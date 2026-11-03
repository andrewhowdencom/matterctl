package matter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func fixtureNode(t *testing.T, id api.NodeID, label string) node {
	t.Helper()
	encoded, err := json.Marshal(label)
	if err != nil {
		t.Fatal(err)
	}
	return node{NodeID: id, Available: true, IsBridge: true, EndpointParents: map[uint16]uint16{1: 0, 2: 0, 3: 1}, observedAt: time.Now().UTC(), Attributes: map[string]json.RawMessage{
		"0/40/5": encoded, "0/40/1": json.RawMessage(`"Vendor"`), "0/40/3": json.RawMessage(`"Bridge"`),
		"0/62/0": json.RawMessage(`{"private_key":"must-not-leak"}`),
		"1/29/0": json.RawMessage(`[{"deviceType":256,"revision":2}]`), "1/29/1": json.RawMessage(`[29,6,8]`), "1/29/2": json.RawMessage(`[]`),
		"1/6/65529": json.RawMessage(`[0,1,2]`), "1/6/0": json.RawMessage(`false`), "1/6/65532": json.RawMessage(`{"lighting":true}`),
		"1/8/0":  json.RawMessage(`42`),
		"2/29/0": json.RawMessage(`[{"deviceType":15,"revision":1}]`), "2/29/1": json.RawMessage(`[29,59]`), "2/29/2": json.RawMessage(`[6]`),
		"2/59/1": json.RawMessage(`0`), "2/6/65529": json.RawMessage(`[0,1,2]`),
		"3/29/0": json.RawMessage(`[{"deviceType":256,"revision":2}]`), "3/29/1": json.RawMessage(`[29,57,6]`), "3/29/2": json.RawMessage(`[]`),
		"3/57/5": json.RawMessage(`"Couch lamp"`), "3/6/65529": json.RawMessage(`[]`),
	}}
}

func TestProjectionReadsNumericDescriptorDeviceTypeFields(t *testing.T) {
	n := fixtureNode(t, "1", "Den Light")
	n.Attributes["1/29/0"] = json.RawMessage(`[{"0":269,"1":1}]`)
	device := project(n)
	light := device.Endpoints[1]
	if !light.DescriptorComplete || !slices.Equal(light.DeviceTypes, []api.DeviceType{{ID: 269, Name: "Extended Color Light", Revision: 1}}) {
		t.Fatalf("numeric Matter descriptor fields were lost: %+v", light.DeviceTypes)
	}
	n.Attributes["1/29/0"] = json.RawMessage(`[{"unknown":269}]`)
	device = project(n)
	if device.Endpoints[1].DescriptorComplete || len(device.Endpoints[1].DeviceTypes) != 0 {
		t.Fatalf("malformed device type became an invented ID: %+v", device.Endpoints[1])
	}
	n.Attributes["1/29/0"] = json.RawMessage(`[{"deviceType":260,"revision":4}]`)
	device = project(n)
	if !slices.Equal(device.Endpoints[1].DeviceTypes, []api.DeviceType{{ID: 260, Name: "Dimmer Switch", Revision: 4}}) {
		t.Fatalf("dimmer switch type was not named: %+v", device.Endpoints[1].DeviceTypes)
	}
}

func initializedClient(t *testing.T, nodes ...node) *Client {
	t.Helper()
	c, err := New("ws://localhost/ws", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c.current = &session{}
	c.status.Connection.State = "connected"
	data, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if nodes == nil {
		data = json.RawMessage(`[]`)
	}
	if err := c.applySnapshot(c.current, data, nil); err != nil {
		t.Fatal(err)
	}
	return c
}

func waitInventory(t *testing.T, c *Client, check func(api.Inventory, error) bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		inventory, err := c.Inventory(context.Background())
		if check(inventory, err) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("inventory did not converge: %+v %v", inventory, err)
		case <-tick.C:
		}
	}
}

func TestProjectionPreservesRolesUnknownCommandsAndPrivateAttributes(t *testing.T) {
	n := fixtureNode(t, "18446744069414584320", "Bridge")
	device := project(n)
	if device.NodeID != n.NodeID || len(device.Endpoints) != 4 {
		t.Fatalf("unexpected projection: %+v", device)
	}
	light, button, bridged := device.Endpoints[1], device.Endpoints[2], device.Endpoints[3]
	if !slices.Equal(light.Capabilities, []string{"power.off", "power.on", "power.toggle"}) || light.Power == nil || *light.Power || light.Level == nil || *light.Level != 42 {
		t.Fatalf("light capabilities/state: %+v", light)
	}
	if len(button.Capabilities) != 0 || button.Power != nil || len(button.ClientClusters) != 1 || button.ClientClusters[0].ID != 6 {
		t.Fatalf("client OnOff cluster was treated as a server: %+v", button)
	}
	if bridged.ParentID == nil || *bridged.ParentID != 1 || bridged.Label != "Couch lamp" || bridged.ServerClusters[0].AcceptedCommands == nil {
		t.Fatalf("bridged endpoint: %+v", bridged)
	}
	if light.ServerClusters[1].AcceptedCommands != nil {
		t.Fatal("unknown command metadata became a known empty list")
	}
	data, err := json.Marshal(device)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-not-leak") || !strings.Contains(string(data), `"node_id":"18446744069414584320"`) {
		t.Fatalf("invalid public projection: %s", data)
	}
}

func TestIDsAndTargetResolution(t *testing.T) {
	for _, wire := range []string{`18446744069414584320`, `"18446744069414584320"`} {
		var id api.NodeID
		if err := json.Unmarshal([]byte(wire), &id); err != nil || id != "18446744069414584320" {
			t.Fatalf("lost precision: %s %v", id, err)
		}
	}
	c := initializedClient(t, fixtureNode(t, "18446744069414584320", "Bridge"))
	for _, target := range []string{"18446744069414584320", "Bridge", "Couch lamp"} {
		device, err := c.Inspect(context.Background(), target)
		if err != nil || device.NodeID != "18446744069414584320" {
			t.Fatalf("resolve %q: %+v %v", target, device, err)
		}
	}
	for _, target := range []string{"", "missing"} {
		if _, err := c.Inspect(context.Background(), target); !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("target %q: %v", target, err)
		}
	}
	other := fixtureNode(t, "2", "Bridge")
	c.mu.Lock()
	c.nodes[other.NodeID] = other
	c.mu.Unlock()
	if _, err := c.Inspect(context.Background(), "Bridge"); !errors.Is(err, api.ErrAmbiguous) {
		t.Fatalf("duplicate labels: %v", err)
	}
}

func TestSnapshotAndImmediatelyFollowingEventsAreOrdered(t *testing.T) {
	n := fixtureNode(t, "1", "Bridge")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil {
			return
		}
		if request.Command != "start_listening" {
			t.Errorf("missing subscription: %+v", request)
			return
		}
		// Deliberately interleave an update before the snapshot reply as well.
		if wsjson.Write(ctx, conn, reply{Event: "attribute_updated", Data: json.RawMessage(`[1,"1/6/0",true]`)}) != nil {
			return
		}
		if writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		if wsjson.Write(ctx, conn, reply{Event: "attribute_updated", Data: json.RawMessage(`[1,"1/8/0",80]`)}) != nil {
			return
		}
		_ = readApplicationRequest(ctx, conn, &request)
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(inventory api.Inventory, err error) bool {
		if err != nil || len(inventory.Devices) != 1 {
			return false
		}
		endpoint := inventory.Devices[0].Endpoints[1]
		return endpoint.Power != nil && *endpoint.Power && endpoint.Level != nil && *endpoint.Level == 80
	})
}

func TestLiveUpdatesRemovalAndEventSanitization(t *testing.T) {
	c := initializedClient(t, fixtureNode(t, "1", "Bridge"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "Couch lamp")
	if err != nil {
		t.Fatal(err)
	}
	<-events
	apply := func(kind, data string) api.Event {
		t.Helper()
		if err := c.applyEvent(c.current, reply{Event: kind, Data: json.RawMessage(data)}); err != nil {
			t.Fatal(err)
		}
		select {
		case event := <-events:
			return event
		case <-time.After(time.Second):
			t.Fatal("event missing")
			return api.Event{}
		}
	}
	apply("attribute_updated", `[1,"1/6/0",null]`)
	device, err := c.Inspect(ctx, "1")
	if err != nil || device.Endpoints[1].Power != nil {
		t.Fatal("removed state remained cached")
	}
	event := apply("node_event", `{"node_id":1,"endpoint_id":2,"cluster_id":59,"event_id":1,"event_number":18446744073709551615,"timestamp":18446744073709551614,"data":{"newPosition":1,"private_key":"must-not-leak"}}`)
	if strings.Contains(string(event.Data), "must-not-leak") || !strings.Contains(string(event.Data), `"event_number":"18446744073709551615"`) || !strings.Contains(string(event.Data), `"event_name":"initial_press"`) || !strings.Contains(string(event.Data), `"name":"Generic Switch"`) || !strings.Contains(string(event.Data), `"newPosition":1`) {
		t.Fatalf("bad event: %s", event.Data)
	}
	event = apply("node_event", `{"node_id":1,"endpoint_id":2,"cluster_id":999,"event_id":1,"data":["must-not-leak"]}`)
	if strings.Contains(string(event.Data), "must-not-leak") {
		t.Fatal("arbitrary event payload leaked")
	}
	apply("endpoint_removed", `{"node_id":1,"endpoint_id":3}`)
	device, err = c.Inspect(ctx, "1")
	if err != nil || len(device.Endpoints) != 3 {
		t.Fatalf("endpoint not removed: %+v %v", device, err)
	}
	n := fixtureNode(t, "1", "Bridge")
	n.Available = false
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	apply("node_updated", string(data))
	device, err = c.Inspect(ctx, "1")
	if err != nil || device.Available {
		t.Fatalf("availability update lost: %+v %v", device, err)
	}
	apply("node_removed", `1`)
	inventory, err := c.Inventory(ctx)
	if err != nil || len(inventory.Devices) != 0 {
		t.Fatalf("node not removed: %+v %v", inventory, err)
	}
}

func TestReconnectKeepsStaleInventoryUntilReplacementSnapshot(t *testing.T) {
	n := fixtureNode(t, "1", "Old bridge")
	drop, release := make(chan struct{}), make(chan struct{})
	var connections atomic.Int32
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		connection := connections.Add(1)
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil {
			return
		}
		if connection == 1 {
			if writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
				return
			}
			select {
			case <-drop:
			case <-ctx.Done():
			}
			return
		}
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		if writeResult(ctx, conn, request.MessageID, []node{}) != nil {
			return
		}
		_ = readApplicationRequest(ctx, conn, &request)
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(inventory api.Inventory, err error) bool { return err == nil && len(inventory.Devices) == 1 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	<-events
	close(drop)
	waitInventory(t, c, func(inventory api.Inventory, err error) bool {
		return errors.Is(err, api.ErrUnavailable) && !inventory.Ready && len(inventory.Devices) == 1
	})
	if _, err := c.Inspect(ctx, "1"); !errors.Is(err, api.ErrUnavailable) {
		t.Fatalf("stale inventory resolved a target: %v", err)
	}
	close(release)
	waitInventory(t, c, func(inventory api.Inventory, err error) bool {
		return err == nil && inventory.Ready && len(inventory.Devices) == 0
	})
	if connections.Load() < 2 {
		t.Fatal("did not resubscribe after reconnect")
	}
	for {
		select {
		case event := <-events:
			if event.Type == "inventory" {
				var inv api.Inventory
				if json.Unmarshal(event.Data, &inv) == nil && inv.Ready && len(inv.Devices) == 0 {
					return
				}
			}
		case <-time.After(time.Second):
			t.Fatal("watcher did not receive replacement snapshot")
		}
	}
}

func TestSlowWatchersCloseWithoutBlockingOthersAndCancellationCleansUp(t *testing.T) {
	c := initializedClient(t)
	slowCtx, stopSlow := context.WithCancel(context.Background())
	defer stopSlow()
	fastCtx, stopFast := context.WithCancel(context.Background())
	defer stopFast()
	slow, err := c.Watch(slowCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	fast, err := c.Watch(fastCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	<-fast
	for range 100 {
		c.mu.Lock()
		c.publishLocked(newEvent("node_removed", "1", api.ErrorResponse{}))
		c.mu.Unlock()
		select {
		case _, ok := <-fast:
			if !ok {
				t.Fatal("fast subscriber was closed")
			}
		case <-time.After(time.Second):
			t.Fatal("publisher blocked")
		}
	}
	count := 0
	for range slow {
		count++
	}
	if count != 64 {
		t.Fatalf("expected bounded queued events then closure, got %d", count)
	}
	stopFast()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		c.mu.Lock()
		remaining := len(c.watchers)
		c.mu.Unlock()
		if remaining == 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("canceled subscriber leaked")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestInvalidSnapshotDoesNotReplaceLastGoodState(t *testing.T) {
	c := initializedClient(t, fixtureNode(t, "1", "Old bridge"))
	c.ready = false
	data, err := json.Marshal([]node{fixtureNode(t, "2", "New bridge")})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.applySnapshot(c.current, data, []reply{{Event: "attribute_updated", Data: json.RawMessage(`[999,"1/6/0",true]`)}}); err == nil {
		t.Fatal("accepted inconsistent buffered update")
	}
	inventory, err := c.Inventory(context.Background())
	if !errors.Is(err, api.ErrUnavailable) || len(inventory.Devices) != 1 || inventory.Devices[0].NodeID != "1" {
		t.Fatalf("last good inventory replaced: %+v %v", inventory, err)
	}
	if err := c.applySnapshot(c.current, json.RawMessage(`null`), nil); err == nil {
		t.Fatal("null inventory treated as empty snapshot")
	}
}

func TestWatchFiltersNodesAndKeepsConnectionEvents(t *testing.T) {
	c := initializedClient(t, fixtureNode(t, "1", "One"), fixtureNode(t, "2", "Two"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	initial := <-events
	var inventory api.Inventory
	if json.Unmarshal(initial.Data, &inventory) != nil || len(inventory.Devices) != 1 || inventory.Devices[0].NodeID != "1" {
		t.Fatalf("unfiltered initial snapshot: %s", initial.Data)
	}
	c.mu.Lock()
	c.publishLocked(newEvent("node_removed", "2", api.ErrorResponse{}))
	c.publishLocked(newEvent("node_removed", "1", api.ErrorResponse{}))
	c.mu.Unlock()
	if event := <-events; event.NodeID != "1" {
		t.Fatalf("filter leaked another node: %+v", event)
	}
	c.setDisconnected("disconnected", "test disconnection")
	if event := <-events; event.Type != "connection" {
		t.Fatalf("filter hid connection event: %+v", event)
	}
}

func TestInitialSnapshotDeadlinePreventsReadyInventory(t *testing.T) {
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil {
			return
		}
		_ = wsjson.Read(ctx, conn, &request)
	})
	c, cancel, _ := runClient(t, address, func(c *Client) {
		c.syncTimeout = 30 * time.Millisecond
		c.retryMin, c.retryMax = time.Minute, time.Minute
	})
	defer cancel()
	waitStatus(t, c, func(status api.ServerStatus) bool {
		return status.Connection.LastError == "inventory synchronization timed out"
	})
	if inventory, err := c.Inventory(context.Background()); !errors.Is(err, api.ErrUnavailable) || inventory.Ready {
		t.Fatalf("unsynchronized inventory became ready: %+v %v", inventory, err)
	}
}
