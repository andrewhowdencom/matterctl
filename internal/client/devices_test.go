package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestInventoryAndInspectUseServiceAndEncodeLabels(t *testing.T) {
	const target = "Couch lamp / café?x=1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		if r.URL.Path == api.DevicesPath {
			result = api.Inventory{Connection: api.Connection{State: "connected"}, Ready: true, Devices: []api.Device{}}
		} else {
			if r.URL.Path != api.InspectPath || r.URL.Query().Get("target") != target {
				t.Errorf("invalid target request: %s", r.URL.Path)
			}
			result = api.Device{NodeID: "18446744069414584320", Label: target, Endpoints: []api.Endpoint{}}
		}
		if err := json.NewEncoder(w).Encode(result); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	service, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := service.Inventory(context.Background())
	if err != nil || inventory.Devices == nil || len(inventory.Devices) != 0 {
		t.Fatalf("empty snapshot: %+v %v", inventory, err)
	}
	device, err := service.Inspect(context.Background(), target)
	if err != nil || device.NodeID != "18446744069414584320" {
		t.Fatalf("inspect: %+v %v", device, err)
	}
}

func TestWatchEstablishmentAndIdleDeadlines(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(map[bool]string{false: "establishment", true: "idle"}[headers], func(t *testing.T) {
			canceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if headers {
					w.Header().Set("Content-Type", "application/x-ndjson")
					if err := http.NewResponseController(w).Flush(); err != nil {
						t.Error(err)
					}
				}
				<-r.Context().Done()
				close(canceled)
			}))
			defer server.Close()
			service, err := New(server.URL, WithTimeout(50*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			service.streamIdleTimeout = 30 * time.Millisecond
			err = service.Watch(context.Background(), "", func(api.Event) error { return nil })
			if headers {
				if err == nil || !strings.Contains(err.Error(), "stalled") {
					t.Fatalf("idle deadline: %v", err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("establishment deadline: %v", err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("request not canceled")
			}
		})
	}
}

func TestTruncatedEventStreamFailsAndCallbackFailureCancels(t *testing.T) {
	for _, callbackFails := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			if err := json.NewEncoder(w).Encode(api.Event{Type: "inventory", Data: json.RawMessage(`{}`)}); err != nil {
				t.Error(err)
			}
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
			}
			if callbackFails {
				<-r.Context().Done()
			}
		}))
		service, err := New(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		stop := errors.New("output failed")
		err = service.Watch(context.Background(), "", func(api.Event) error {
			if callbackFails {
				return stop
			}
			return nil
		})
		server.Close()
		if callbackFails {
			if !errors.Is(err, stop) {
				t.Fatalf("callback failure: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "ended") {
			t.Fatalf("truncation was hidden: %v", err)
		}
	}
}
