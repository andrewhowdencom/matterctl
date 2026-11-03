package matter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestSetLabelWritesAndConfirmsNodeLabel(t *testing.T) {
	n := fixtureNode(t, "1", "Old name")
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
			var args struct {
				NodeID        json.Number `json:"node_id"`
				AttributePath string      `json:"attribute_path"`
				Value         string      `json:"value"`
			}
			if json.Unmarshal(request.Args, &args) != nil || args.NodeID != "1" || args.AttributePath != "0/40/5" {
				t.Error("incorrect label path or node")
			}
			switch request.Command {
			case "write_attribute":
				if args.Value != "Couch lamp" {
					t.Error("incorrect label value")
				}
				if writeResult(ctx, conn, request.MessageID, []any{map[string]any{"Path": map[string]any{"EndpointId": 0, "ClusterId": 40, "AttributeId": 5}, "Status": 0}}) != nil {
					return
				}
			case "read_attribute":
				if writeResult(ctx, conn, request.MessageID, map[string]string{"0/40/5": "Couch lamp"}) != nil {
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
	result, err := c.SetLabel(context.Background(), api.LabelRequest{Target: "1", Label: "Couch lamp"})
	if err != nil || result.NodeID != "1" || result.Endpoint != 0 || result.Label != "Couch lamp" || result.ConfirmedAt.IsZero() {
		t.Fatalf("label was not confirmed: %+v %v", result, err)
	}
	device, err := c.Inspect(context.Background(), "Couch lamp")
	if err != nil || device.Label != "Couch lamp" {
		t.Fatalf("confirmed label cannot address device: %+v %v", device, err)
	}
}

func TestSetLabelRejectsDeviceStatus(t *testing.T) {
	n := fixtureNode(t, "1", "Old name")
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
		_ = writeResult(ctx, conn, request.MessageID, []any{map[string]any{"Path": map[string]any{"EndpointId": 0, "ClusterId": 40, "AttributeId": 5}, "Status": 136}})
		_ = wsjson.Read(ctx, conn, &request)
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	_, err := c.SetLabel(context.Background(), api.LabelRequest{Target: "1", Label: "Couch lamp"})
	if !errors.Is(err, api.ErrDeviceRejected) || !strings.Contains(err.Error(), "136") {
		t.Fatalf("device status was lost: %v", err)
	}
}

func TestSetLabelRequiresBridgedLabelCluster(t *testing.T) {
	n := fixtureNode(t, "1", "Bridge")
	c := initializedClient(t, n)
	e := uint16(1)
	_, err := c.SetLabel(context.Background(), api.LabelRequest{Target: "1", Endpoint: &e, Label: "Lamp"})
	if !errors.Is(err, api.ErrUnsupported) {
		t.Fatalf("ordinary endpoint accepted label: %v", err)
	}
}

func TestSetLabelWritesBridgedEndpointLabel(t *testing.T) {
	n := fixtureNode(t, "1", "Bridge")
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
			var args struct {
				AttributePath string `json:"attribute_path"`
				Value         string `json:"value"`
			}
			if json.Unmarshal(request.Args, &args) != nil || args.AttributePath != "3/57/5" {
				t.Error("incorrect bridged label path")
			}
			switch request.Command {
			case "write_attribute":
				if args.Value != "Desk lamp" {
					t.Error("incorrect bridged label value")
				}
				if writeResult(ctx, conn, request.MessageID, []any{map[string]any{"Path": map[string]any{"EndpointId": 3, "ClusterId": 57, "AttributeId": 5}, "Status": 0}}) != nil {
					return
				}
			case "read_attribute":
				if writeResult(ctx, conn, request.MessageID, map[string]string{"3/57/5": "Desk lamp"}) != nil {
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
	endpoint := uint16(3)
	result, err := c.SetLabel(context.Background(), api.LabelRequest{Target: "1", Endpoint: &endpoint, Label: "Desk lamp"})
	if err != nil || result.Endpoint != 3 || result.Label != "Desk lamp" {
		t.Fatalf("bridged label not confirmed: %+v %v", result, err)
	}
	device, err := c.Inspect(context.Background(), "Desk lamp")
	if err != nil || device.NodeID != "1" {
		t.Fatalf("bridged label did not resolve node: %+v %v", device, err)
	}
}
