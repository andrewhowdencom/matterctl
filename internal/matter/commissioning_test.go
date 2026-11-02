package matter

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestDiscoveryAndCommissioningPreserveSubscription(t *testing.T) {
	n := fixtureNode(t, "18446744069414584320", "Shared lamp")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		for {
			var request testRequest
			if readApplicationRequest(ctx, conn, &request) != nil {
				return
			}
			switch request.Command {
			case "discover":
				if writeResult(ctx, conn, request.MessageID, []map[string]any{{"instance_name": "ADVERTISED", "device_name": "Lamp", "vendor_id": 123, "rotating_id": "private-identifier"}}) != nil {
					return
				}
			case "commission_with_code":
				var args map[string]any
				if json.Unmarshal(request.Args, &args) != nil || args["network_only"] != true || args["code"] != "35325335079" || len(args) != 2 {
					t.Error("unexpected commissioning arguments")
				}
				// Exceeds the ordinary RPC timeout; the long operation must survive.
				timer := time.NewTimer(30 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				if wsjson.Write(ctx, conn, map[string]any{"event": "node_added", "data": n}) != nil {
					return
				}
				if wsjson.Write(ctx, conn, map[string]any{"event": "attribute_updated", "data": []any{n.NodeID, "1/6/0", true}}) != nil {
					return
				}
				if writeResult(ctx, conn, request.MessageID, n) != nil {
					return
				}
			case "server_info":
				if writeResult(ctx, conn, request.MessageID, testInfo()) != nil {
					return
				}
			default:
				t.Error("unexpected upstream operation")
				return
			}
		}
	})
	c, _, _ := runClient(t, address, func(c *Client) { c.requestTimeout = 10 * time.Millisecond })
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && i.Ready })
	discovery, err := c.Discover(context.Background())
	if err != nil || len(discovery.Devices) != 1 || discovery.Devices[0].VendorID == nil || *discovery.Devices[0].VendorID != 123 {
		t.Fatalf("discovery: %+v %v", discovery, err)
	}
	encoded, _ := json.Marshal(discovery)
	if strings.Contains(string(encoded), "private-identifier") {
		t.Fatal("forwarded raw discovery data")
	}
	device, err := c.Commission(context.Background(), api.CommissionRequest{Code: "3532-533-5079", NetworkOnly: true})
	if err != nil || device.NodeID != n.NodeID || !slices.Contains(device.Endpoints[1].Capabilities, "power.on") {
		t.Fatalf("commission: %+v %v", device, err)
	}
	current, err := c.Inspect(context.Background(), string(n.NodeID))
	if err != nil || current.Endpoints[1].Power == nil || !*current.Endpoints[1].Power {
		t.Fatal("commission reply overwrote a newer state report")
	}
	encoded, _ = json.Marshal(device)
	if strings.Contains(string(encoded), "must-not-leak") {
		t.Fatal("commission returned private attributes")
	}
	if _, err := c.ServerInfo(context.Background()); err != nil {
		t.Fatal("commission disrupted the shared session:", err)
	}
}

func TestCommissionFailureReasonsExcludeRequestData(t *testing.T) {
	for _, reason := range []string{"not-discovered", "passcode-mismatch", "invalid-payload", "attestation-rejected", "timeout", "other", "35325335079"} {
		t.Run(reason, func(t *testing.T) {
			address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
				if wsjson.Write(ctx, conn, testInfo()) != nil {
					return
				}
				for {
					var request testRequest
					if readApplicationRequest(ctx, conn, &request) != nil {
						return
					}
					if wsjson.Write(ctx, conn, map[string]any{"message_id": request.MessageID, "error_code": 3, "reason": reason, "details": "35325335079 secret"}) != nil {
						return
					}
				}
			})
			c, _, _ := runClient(t, address)
			waitInventory(t, c, func(_ api.Inventory, err error) bool { return err == nil })
			_, err := c.Commission(context.Background(), api.CommissionRequest{Code: "35325335079", NetworkOnly: true})
			if err == nil || strings.Contains(err.Error(), "35325335079") || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "error code 3") {
				t.Fatalf("unsafe or missing failure: %v", err)
			}
			if safeReason(reason) != "" && !strings.Contains(err.Error(), reason) {
				t.Fatal("lost safe upstream reason")
			}
		})
	}
}

func TestCommissionCancelDoesNotReplayAndLateReplyIsIgnored(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var commissions atomic.Int32
	n := fixtureNode(t, "42", "Lamp")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		for {
			var request testRequest
			if readApplicationRequest(ctx, conn, &request) != nil {
				return
			}
			if request.Command == "commission_with_code" {
				if commissions.Add(1) == 1 {
					close(started)
				}
				select {
				case <-ctx.Done():
					return
				case <-release:
				}
				if writeResult(ctx, conn, request.MessageID, n) != nil {
					return
				}
			} else if writeResult(ctx, conn, request.MessageID, testInfo()) != nil {
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(_ api.Inventory, err error) bool { return err == nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	request := api.CommissionRequest{Code: "35325335079", NetworkOnly: true}
	go func() { _, err := c.Commission(ctx, request); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("commission was not sent")
	}
	if _, err := c.Commission(context.Background(), request); !errors.Is(err, api.ErrBusy) {
		t.Fatal("accepted simultaneous commissioning")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, api.ErrOutcomeUnknown) {
			t.Fatalf("incorrect cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop waiting")
	}
	close(release)
	if _, err := c.ServerInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if commissions.Load() != 1 {
		t.Fatal("commissioning was replayed")
	}
}

func TestCommissionDeadlineAndDisconnectLeaveUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"deadline", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			var attempts atomic.Int32
			address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
				if wsjson.Write(ctx, conn, testInfo()) != nil {
					return
				}
				for {
					var request testRequest
					if readApplicationRequest(ctx, conn, &request) != nil {
						return
					}
					if request.Command == "commission_with_code" {
						attempts.Add(1)
						if mode == "disconnect" {
							return
						}
						// No response: only the caller's deadline ends the wait.
					} else if writeResult(ctx, conn, request.MessageID, testInfo()) != nil {
						return
					}
				}
			})
			c, _, _ := runClient(t, address)
			waitInventory(t, c, func(_ api.Inventory, err error) bool { return err == nil })
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := c.Commission(ctx, api.CommissionRequest{Code: "35325335079", NetworkOnly: true})
			cause := context.DeadlineExceeded
			if mode == "disconnect" {
				cause = api.ErrUnavailable
			}
			if !errors.Is(err, api.ErrOutcomeUnknown) || !errors.Is(err, cause) {
				t.Fatalf("wrong failure: %v", err)
			}
			waitInventory(t, c, func(_ api.Inventory, err error) bool { return err == nil })
			if _, err := c.ServerInfo(context.Background()); err != nil {
				t.Fatal("session did not recover:", err)
			}
			if attempts.Load() != 1 {
				t.Fatal("commission was lost or replayed")
			}
		})
	}
}
