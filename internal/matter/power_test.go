package matter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestPowerAcknowledgementNeedsFreshConfirmedRead(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	var commands, reads atomic.Int32
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || request.Command != "start_listening" || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for {
			if wsjson.Read(ctx, conn, &request) != nil {
				return
			}
			switch request.Command {
			case "device_command":
				var args struct {
					NodeID      json.Number `json:"node_id"`
					EndpointID  uint16      `json:"endpoint_id"`
					ClusterID   uint32      `json:"cluster_id"`
					CommandName string      `json:"command_name"`
				}
				if json.Unmarshal(request.Args, &args) != nil || args.NodeID != "1" || args.EndpointID != 1 || args.ClusterID != 6 || args.CommandName != "on" {
					t.Error("incorrect device command")
				}
				commands.Add(1)
				if wsjson.Write(ctx, conn, map[string]any{"event": "attribute_updated", "data": []any{1, "1/6/0", true}}) != nil {
					return
				}
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
				if wsjson.Write(ctx, conn, map[string]any{"event": "attribute_updated", "data": []any{1, "1/6/0", true}}) != nil {
					return
				}
			case "read_attribute":
				var args struct {
					AttributePath string `json:"attribute_path"`
				}
				if json.Unmarshal(request.Args, &args) != nil || args.AttributePath != "1/6/0" {
					t.Error("incorrect read path")
				}
				value := reads.Add(1) > 1
				if writeResult(ctx, conn, request.MessageID, map[string]bool{"1/6/0": value}) != nil {
					return
				}
			default:
				t.Errorf("unexpected command %s", request.Command)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	result, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on", TimeoutMS: 2000})
	if err != nil || result.NodeID != "1" || result.Endpoint != 1 || !result.Power || result.ConfirmedAt.IsZero() || commands.Load() != 1 || reads.Load() < 2 {
		t.Fatalf("power was not confirmed by a fresh read: %+v, %v; commands=%d reads=%d", result, err, commands.Load(), reads.Load())
	}
}

func TestPowerRejectsUnsupportedOrOfflineBeforeSending(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	c := initializedClient(t, n)
	e := uint16(2)
	if _, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Endpoint: &e, Action: "on"}); !errors.Is(err, api.ErrUnsupported) {
		t.Fatalf("unsupported endpoint: %v", err)
	}
	n.Attributes["2/29/1"] = json.RawMessage(`[29,6]`)
	n.Attributes["2/6/65529"] = json.RawMessage(`[0,1]`)
	c.mu.Lock()
	c.nodes[n.NodeID] = n
	c.mu.Unlock()
	if _, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on"}); !errors.Is(err, api.ErrAmbiguous) || !strings.Contains(err.Error(), "[1 2]") {
		t.Fatalf("multiple compatible endpoints were not listed: %v", err)
	}
	c.mu.Lock()
	n.Available = false
	c.nodes[n.NodeID] = n
	c.mu.Unlock()
	if _, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on"}); !errors.Is(err, api.ErrOffline) {
		t.Fatalf("offline node: %v", err)
	}
}

func TestPowerTimeoutAfterAcknowledgementIsUnconfirmed(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for {
			if wsjson.Read(ctx, conn, &request) != nil {
				return
			}
			switch request.Command {
			case "device_command":
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
			case "read_attribute":
				if writeResult(ctx, conn, request.MessageID, map[string]bool{"1/6/0": false}) != nil {
					return
				}
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Power(ctx, api.PowerRequest{Target: "1", Action: "on"})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, api.ErrUnconfirmed) {
		t.Fatalf("wrong unconfirmed result: %v", err)
	}
	// No replay occurs after the caller's deadline.
	time.Sleep(20 * time.Millisecond)
}

func TestPowerDeviceRejectionPreservesStatusWithoutDetails(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		if wsjson.Read(ctx, conn, &request) != nil {
			return
		}
		if wsjson.Write(ctx, conn, map[string]any{"message_id": request.MessageID, "error_code": 7, "status": 129, "details": "private device data"}) != nil {
			return
		}
		_ = wsjson.Read(ctx, conn, &request)
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	_, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on"})
	if !errors.Is(err, api.ErrDeviceRejected) || !strings.Contains(err.Error(), "129") || strings.Contains(err.Error(), "private") {
		t.Fatalf("device rejection was not safely classified: %v", err)
	}
}

func TestPowerConfirmsFromReadWithoutSubscriptionReport(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for {
			if wsjson.Read(ctx, conn, &request) != nil {
				return
			}
			switch request.Command {
			case "device_command":
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
			case "read_attribute":
				if writeResult(ctx, conn, request.MessageID, map[string]bool{"1/6/0": true}) != nil {
					return
				}
			default:
				t.Errorf("unexpected request %s", request.Command)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	result, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on"})
	if err != nil || !result.Power {
		t.Fatalf("fresh read was not enough to confirm command: %+v %v", result, err)
	}
}

func TestPowerDisconnectDoesNotReplayAcrossSessions(t *testing.T) {
	n := fixtureNode(t, "1", "Lamp")
	var commands atomic.Int32
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for {
			if wsjson.Read(ctx, conn, &request) != nil {
				return
			}
			if request.Command == "device_command" {
				commands.Add(1)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	_, err := c.Power(context.Background(), api.PowerRequest{Target: "1", Action: "on", TimeoutMS: 1000})
	if err == nil || !strings.Contains(err.Error(), "outcome may be unknown") {
		t.Fatalf("disconnect looked successful: %v", err)
	}
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	if commands.Load() != 1 {
		t.Fatalf("power command was replayed %d times", commands.Load())
	}
}
