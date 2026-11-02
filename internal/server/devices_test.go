package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/client"
)

type deviceSourceStub struct {
	inventory func(context.Context) (api.Inventory, error)
	inspect   func(context.Context, string) (api.Device, error)
	watch     func(context.Context, string) (<-chan api.Event, error)
}

func (s deviceSourceStub) Inventory(ctx context.Context) (api.Inventory, error) {
	if s.inventory != nil {
		return s.inventory(ctx)
	}
	return api.Inventory{}, api.ErrUnavailable
}
func (s deviceSourceStub) Inspect(ctx context.Context, target string) (api.Device, error) {
	if s.inspect != nil {
		return s.inspect(ctx, target)
	}
	return api.Device{}, api.ErrNotFound
}
func (s deviceSourceStub) Watch(ctx context.Context, target string) (<-chan api.Event, error) {
	if s.watch != nil {
		return s.watch(ctx, target)
	}
	return nil, api.ErrNotFound
}

func TestInventoryAndInspectHTTPContracts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range []struct {
		name string
		err  error
		code int
	}{
		{"ready", nil, 200}, {"resynchronizing", api.ErrUnavailable, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := deviceSourceStub{inventory: func(context.Context) (api.Inventory, error) {
				return api.Inventory{Connection: api.Connection{State: "connected"}, Ready: tt.err == nil, Devices: []api.Device{{NodeID: "18446744069414584320", Label: "Couch lamp"}}}, tt.err
			}}
			response := httptest.NewRecorder()
			handler(logger, WithDevices(source)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, api.DevicesPath, nil))
			if response.Code != tt.code || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("invalid response: %d %s", response.Code, response.Body)
			}
			var inventory api.Inventory
			if json.Unmarshal(response.Body.Bytes(), &inventory) != nil || len(inventory.Devices) != 1 || inventory.Devices[0].NodeID != "18446744069414584320" {
				t.Fatalf("invalid inventory: %s", response.Body)
			}
		})
	}
	for _, tt := range []struct {
		err  error
		code int
	}{{api.ErrNotFound, 404}, {api.ErrAmbiguous, 409}, {api.ErrUnavailable, 503}} {
		source := deviceSourceStub{inspect: func(context.Context, string) (api.Device, error) { return api.Device{}, tt.err }}
		response := httptest.NewRecorder()
		handler(logger, WithDevices(source)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, api.InspectPath+"?target=lamp", nil))
		if response.Code != tt.code || !strings.Contains(response.Body.String(), tt.err.Error()) {
			t.Fatalf("error response: %d %s", response.Code, response.Body)
		}
	}
}

func TestEventStreamOutlivesRequestAndServerWriteTimeoutsAndCleansUp(t *testing.T) {
	canceled := make(chan struct{})
	source := deviceSourceStub{watch: func(ctx context.Context, target string) (<-chan api.Event, error) {
		if target != "Couch lamp" {
			t.Errorf("filter lost: %q", target)
		}
		events := make(chan api.Event, 1)
		events <- api.Event{Type: "inventory", ObservedAt: time.Now(), Data: json.RawMessage(`{"ready":true,"devices":[]}`)}
		go func() { <-ctx.Done(); close(canceled) }()
		return events, nil
	}}
	server := httptest.NewUnstartedServer(handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithDevices(source), func(o *options) { o.heartbeat = 60 * time.Millisecond }))
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	service, err := client.New(server.URL, client.WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	heartbeats := 0
	started := time.Now()
	err = service.Watch(ctx, "Couch lamp", func(event api.Event) error {
		if event.Type == "heartbeat" {
			heartbeats++
			if heartbeats == 3 {
				cancel()
			}
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || heartbeats != 3 || time.Since(started) < 100*time.Millisecond {
		t.Fatalf("stream ended early: heartbeats=%d error=%v", heartbeats, err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation did not release subscription")
	}
}

func TestEventStreamClosureIsReportedAsGap(t *testing.T) {
	source := deviceSourceStub{watch: func(context.Context, string) (<-chan api.Event, error) {
		events := make(chan api.Event, 1)
		events <- api.Event{Type: "inventory", Data: json.RawMessage(`{}`)}
		close(events)
		return events, nil
	}}
	server := httptest.NewServer(handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithDevices(source)))
	defer server.Close()
	service, err := client.New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	err = service.Watch(context.Background(), "", func(api.Event) error { seen++; return nil })
	if err == nil || !strings.Contains(err.Error(), "interrupted") || seen != 1 {
		t.Fatalf("gap was hidden: seen=%d err=%v", seen, err)
	}
}

func TestDeviceRoutesAreReadOnly(t *testing.T) {
	h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{api.DevicesPath, api.InspectPath, api.EventsPath} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s returned %d", path, response.Code)
		}
	}
}
