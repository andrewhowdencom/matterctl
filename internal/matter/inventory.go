package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type watcher struct {
	node api.NodeID
	ch   chan api.Event
}

func (c *Client) Inventory(ctx context.Context) (api.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return api.Inventory{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	inventory := c.inventoryLocked("")
	if !inventory.Ready {
		return inventory, api.ErrUnavailable
	}
	return inventory, nil
}

func (c *Client) Inspect(ctx context.Context, target string) (api.Device, error) {
	if err := ctx.Err(); err != nil {
		return api.Device{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		return api.Device{}, api.ErrUnavailable
	}
	id, err := c.resolveLocked(target)
	if err != nil {
		return api.Device{}, err
	}
	return project(c.nodes[id]), nil
}

// Watch atomically registers a subscriber and queues its initial snapshot.
// A slow subscriber is closed rather than silently losing events.
func (c *Client) Watch(ctx context.Context, target string) (<-chan api.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	var id api.NodeID
	if target != "" {
		if !c.ready {
			c.mu.Unlock()
			return nil, api.ErrUnavailable
		}
		var err error
		id, err = c.resolveLocked(target)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	c.watchSerial++
	key := c.watchSerial
	w := watcher{node: id, ch: make(chan api.Event, 64)}
	w.ch <- newEvent("inventory", id, c.inventoryLocked(id))
	c.watchers[key] = w
	c.mu.Unlock()
	context.AfterFunc(ctx, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if w, ok := c.watchers[key]; ok {
			delete(c.watchers, key)
			close(w.ch)
		}
	})
	return w.ch, nil
}

func (c *Client) resolveLocked(target string) (api.NodeID, error) {
	if target == "" {
		return "", api.ErrNotFound
	}
	if number, err := strconv.ParseUint(target, 10, 64); err == nil {
		id := api.NodeID(strconv.FormatUint(number, 10))
		if _, found := c.nodes[id]; found {
			return id, nil
		}
		return "", api.ErrNotFound
	}
	matches := map[api.NodeID]bool{}
	for id, n := range c.nodes {
		device := project(n)
		if device.Label == target {
			matches[id] = true
		}
		for _, endpoint := range device.Endpoints {
			if endpoint.Label == target {
				matches[id] = true
			}
		}
	}
	if len(matches) > 1 {
		return "", api.ErrAmbiguous
	}
	for id := range matches {
		return id, nil
	}
	return "", api.ErrNotFound
}

func (c *Client) inventoryLocked(filter api.NodeID) api.Inventory {
	connection := c.status.Connection
	if connection.ConnectedAt != nil {
		value := *connection.ConnectedAt
		connection.ConnectedAt = &value
	}
	inventory := api.Inventory{Connection: connection, Ready: c.ready, Devices: []api.Device{}}
	if !c.observedAt.IsZero() {
		value := c.observedAt
		inventory.ObservedAt = &value
	}
	for id, n := range c.nodes {
		if filter == "" || id == filter {
			inventory.Devices = append(inventory.Devices, project(n))
		}
	}
	slices.SortFunc(inventory.Devices, func(a, b api.Device) int {
		x, _ := strconv.ParseUint(string(a.NodeID), 10, 64)
		y, _ := strconv.ParseUint(string(b.NodeID), 10, 64)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	})
	return inventory
}

func newEvent(kind string, id api.NodeID, data any) api.Event {
	raw, err := json.Marshal(data)
	if err != nil {
		kind, raw = "stream_error", json.RawMessage(`{"error":"cannot encode event"}`)
	}
	return api.Event{Type: kind, NodeID: id, ObservedAt: time.Now().UTC(), Data: raw}
}

// A post-command read is an observation of the device, even if no subscription
// report follows. Publish it so clients do not keep displaying older state.
func (c *Client) publishConfirmedAttribute(session *session, id api.NodeID, endpoint uint16, cluster, attribute uint32, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready || c.current != session {
		return
	}
	n, found := c.nodes[id]
	if !found {
		return
	}
	path := fmt.Sprintf("%d/%d/%d", endpoint, cluster, attribute)
	if n.Attributes == nil {
		n.Attributes = map[string]json.RawMessage{}
	}
	n.Attributes[path] = append(json.RawMessage(nil), raw...)
	n.observedAt = time.Now().UTC()
	c.nodes[id] = n
	c.observedAt = n.observedAt
	device := project(n)
	var state *api.State
	if value, known := decodeState(cluster, attribute, raw); known {
		state = &value
	}
	c.publishLocked(newEvent("attribute_updated", id, struct {
		Path          string     `json:"path"`
		AttributeName string     `json:"attribute_name,omitempty"`
		Device        api.Device `json:"device"`
		State         *api.State `json:"state,omitempty"`
	}{path, attributeName(cluster, attribute), device, state}))
}

func (c *Client) publishLocked(event api.Event) {
	if c.observe != nil {
		c.observe(event)
	}
	for key, w := range c.watchers {
		if event.NodeID != "" && w.node != "" && event.NodeID != w.node {
			continue
		}
		select {
		case w.ch <- event:
		default:
			delete(c.watchers, key)
			close(w.ch)
		}
	}
}

func (c *Client) publishInventoryLocked() {
	if c.observe != nil {
		c.observe(newEvent("inventory", "", c.inventoryLocked("")))
	}
	for key, w := range c.watchers {
		event := newEvent("inventory", w.node, c.inventoryLocked(w.node))
		select {
		case w.ch <- event:
		default:
			delete(c.watchers, key)
			close(w.ch)
		}
	}
}

func (c *Client) applySnapshot(s *session, data json.RawMessage, buffered []reply) error {
	var nodes []node
	if json.Unmarshal(data, &nodes) != nil || nodes == nil {
		return errors.New("invalid inventory snapshot")
	}
	now := time.Now().UTC()
	state := make(map[api.NodeID]node, len(nodes))
	for _, n := range nodes {
		if n.NodeID == "" {
			return errors.New("invalid inventory node")
		}
		if _, duplicate := state[n.NodeID]; duplicate {
			return errors.New("duplicate inventory node")
		}
		n.observedAt = now
		state[n.NodeID] = n
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != s {
		return api.ErrUnavailable
	}
	previousNodes, previousObserved := c.nodes, c.observedAt
	c.nodes, c.observedAt = state, now
	for _, frame := range buffered {
		if err := c.applyEventLocked(frame, false); err != nil {
			c.nodes, c.observedAt = previousNodes, previousObserved
			return err
		}
	}
	c.ready = true
	c.publishInventoryLocked()
	for _, frame := range buffered {
		if frame.Event == "node_event" {
			if err := c.applyEventLocked(frame, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) applyEvent(s *session, frame reply) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != s {
		return api.ErrUnavailable
	}
	return c.applyEventLocked(frame, true)
}

func (c *Client) applyEventLocked(frame reply, publish bool) error {
	now := time.Now().UTC()
	var id api.NodeID
	var data any
	switch frame.Event {
	case "node_added", "node_updated":
		var n node
		if json.Unmarshal(frame.Data, &n) != nil || n.NodeID == "" {
			return errors.New("invalid node update")
		}
		n.observedAt = now
		id = n.NodeID
		c.nodes[id] = n
		data = project(n)
	case "node_removed":
		if json.Unmarshal(frame.Data, &id) != nil || id == "" {
			return errors.New("invalid node removal")
		}
		delete(c.nodes, id)
		data = struct {
			NodeID api.NodeID `json:"node_id"`
		}{id}
	case "attribute_updated":
		var update []json.RawMessage
		var path string
		if json.Unmarshal(frame.Data, &update) != nil || len(update) != 3 || json.Unmarshal(update[0], &id) != nil || json.Unmarshal(update[1], &path) != nil {
			return errors.New("invalid attribute update")
		}
		endpointID, clusterID, attributeID, ok := parsePath(path)
		if !ok {
			return errors.New("invalid attribute path")
		}
		n, found := c.nodes[id]
		if !found {
			return errors.New("attribute update for unknown node")
		}
		if n.Attributes == nil {
			n.Attributes = make(map[string]json.RawMessage)
		}
		if string(update[2]) == "null" {
			delete(n.Attributes, path)
		} else {
			n.Attributes[path] = update[2]
		}
		n.observedAt = now
		c.nodes[id] = n
		device := project(n)
		var state *api.State
		for _, endpoint := range device.Endpoints {
			if endpoint.ID != endpointID || !slices.ContainsFunc(endpoint.ServerClusters, func(cluster api.Cluster) bool { return cluster.ID == clusterID }) {
				continue
			}
			if decoded, known := decodeState(clusterID, attributeID, update[2]); known {
				state = &decoded
			} else {
				for i := range endpoint.States {
					candidate := &endpoint.States[i]
					if candidate.ClusterID == clusterID && candidate.AttributeID == attributeID {
						state = candidate
						break
					}
				}
			}
			break
		}
		data = struct {
			Path          string     `json:"path"`
			AttributeName string     `json:"attribute_name,omitempty"`
			Device        api.Device `json:"device"`
			State         *api.State `json:"state,omitempty"`
		}{path, attributeName(clusterID, attributeID), device, state}
	case "endpoint_added", "endpoint_removed":
		var update struct {
			NodeID     api.NodeID `json:"node_id"`
			EndpointID uint16     `json:"endpoint_id"`
		}
		if json.Unmarshal(frame.Data, &update) != nil || update.NodeID == "" {
			return errors.New("invalid endpoint update")
		}
		id = update.NodeID
		n, found := c.nodes[id]
		if !found {
			return errors.New("endpoint update for unknown node")
		}
		if n.addedEndpoints == nil {
			n.addedEndpoints = make(map[uint16]bool)
		}
		if frame.Event == "endpoint_added" {
			n.addedEndpoints[update.EndpointID] = true
		} else {
			delete(n.addedEndpoints, update.EndpointID)
			delete(n.EndpointParents, update.EndpointID)
			for path := range n.Attributes {
				if ep, _, _, ok := parsePath(path); ok && ep == update.EndpointID {
					delete(n.Attributes, path)
				}
			}
		}
		n.observedAt = now
		c.nodes[id] = n
		data = struct {
			EndpointID uint16     `json:"endpoint_id"`
			Device     api.Device `json:"device"`
		}{update.EndpointID, project(n)}
	case "node_event":
		var event struct {
			NodeID        api.NodeID      `json:"node_id"`
			EndpointID    uint16          `json:"endpoint_id"`
			ClusterID     uint32          `json:"cluster_id"`
			EventID       uint32          `json:"event_id"`
			EventNumber   json.Number     `json:"event_number"`
			Timestamp     json.Number     `json:"timestamp"`
			TimestampType uint8           `json:"timestamp_type"`
			Priority      uint8           `json:"priority"`
			Data          json.RawMessage `json:"data"`
		}
		if json.Unmarshal(frame.Data, &event) != nil || event.NodeID == "" {
			return errors.New("invalid device event")
		}
		id = event.NodeID
		var deviceTypes []api.DeviceType
		var label, product string
		if n, found := c.nodes[id]; found {
			device := project(n)
			label, product = device.Label, device.Product
			for _, endpoint := range device.Endpoints {
				if endpoint.ID == event.EndpointID {
					deviceTypes = endpoint.DeviceTypes
					break
				}
			}
		}
		fields := map[string]uint8{}
		name := eventName(event.ClusterID, event.EventID)
		if event.ClusterID == clusterSwitch {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(event.Data, &values); err != nil {
				values = nil
			}
			for _, name := range []string{"newPosition", "previousPosition", "currentPosition", "totalNumberOfPressesCounted", "numberOfPressesCounted"} {
				var value uint8
				if raw, ok := values[name]; ok && json.Unmarshal(raw, &value) == nil {
					fields[name] = value
				}
			}
		}
		data = struct {
			EndpointID    uint16           `json:"endpoint_id"`
			ClusterID     uint32           `json:"cluster_id"`
			ClusterName   string           `json:"cluster_name,omitempty"`
			EventID       uint32           `json:"event_id"`
			EventName     string           `json:"event_name,omitempty"`
			EventNumber   string           `json:"event_number,omitempty"`
			Timestamp     string           `json:"timestamp,omitempty"`
			TimestampType uint8            `json:"timestamp_type"`
			Priority      uint8            `json:"priority"`
			Label         string           `json:"label,omitempty"`
			Product       string           `json:"product,omitempty"`
			DeviceTypes   []api.DeviceType `json:"device_types,omitempty"`
			Data          map[string]uint8 `json:"data"`
		}{event.EndpointID, event.ClusterID, clusterName(event.ClusterID), event.EventID, name, event.EventNumber.String(), event.Timestamp.String(), event.TimestampType, event.Priority, label, product, deviceTypes, fields}
	default:
		return nil
	}
	if frame.Event != "node_event" {
		c.observedAt = now
	}
	if publish {
		c.publishLocked(newEvent(frame.Event, id, data))
	}
	return nil
}
