package matter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func testInfo() api.ServerInfo {
	return api.ServerInfo{SDKVersion: "matter-server/test", SchemaVersion: 14, MinSupportedSchemaVersion: 11, BluetoothEnabled: true}
}

type testRequest struct {
	MessageID string          `json:"message_id"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
}

func peer(t *testing.T, run func(context.Context, *websocket.Conn)) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		run(ctx, conn)
	}))
	t.Cleanup(func() { cancel(); server.Close() })
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func runClient(t *testing.T, address string, configure ...func(*Client)) (*Client, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	c, err := New(address, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c.retryMin = 5 * time.Millisecond
	c.retryMax = 20 * time.Millisecond
	for _, configure := range configure {
		configure(c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("connection lifecycle did not stop")
		}
	})
	return c, cancel, done
}

func waitStatus(t *testing.T, c *Client, predicate func(api.ServerStatus) bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if predicate(c.Status()) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("status did not converge: %+v", c.Status())
		case <-tick.C:
		}
	}
}

func connected(status api.ServerStatus) bool { return status.Connection.State == "connected" }

func writeResult(ctx context.Context, conn *websocket.Conn, id string, result any) error {
	return wsjson.Write(ctx, conn, map[string]any{"message_id": id, "result": result})
}

func TestServerInfoRoundTrip(t *testing.T) {
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := wsjson.Write(ctx, conn, testInfo()); err != nil {
			return
		}
		for {
			var request testRequest
			if err := wsjson.Read(ctx, conn, &request); err != nil {
				return
			}
			if request.Command != "server_info" || request.MessageID == "" {
				t.Errorf("unexpected request: %+v", request)
			}
			info := testInfo()
			info.WiFiCredentialsSet = true
			if err := writeResult(ctx, conn, request.MessageID, info); err != nil {
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, connected)
	status, err := c.ServerInfo(context.Background())
	if err != nil || status.Server == nil || !status.Server.WiFiCredentialsSet {
		t.Fatalf("expected fresh server_info response, got %+v, %v", status, err)
	}
	// Returned snapshots cannot mutate the client's internal state.
	snapshot := c.Status()
	snapshot.Server.SDKVersion = "changed"
	*snapshot.Connection.ConnectedAt = time.Time{}
	if got := c.Status(); got.Server.SDKVersion == "changed" || got.Connection.ConnectedAt.IsZero() {
		t.Fatalf("mutable snapshot leaked into client: %+v", got)
	}
}

func TestConcurrentCallsCorrelateOutOfOrderReplies(t *testing.T) {
	const count = 12
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := wsjson.Write(ctx, conn, testInfo()); err != nil {
			return
		}
		requests := make([]testRequest, count)
		for i := range requests {
			if err := wsjson.Read(ctx, conn, &requests[i]); err != nil {
				return
			}
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"event": "attribute_updated", "data": []any{1, "1/6/0", true}}); err != nil {
			return
		}
		if err := writeResult(ctx, conn, "unknown-request", nil); err != nil {
			return
		}
		for i := count - 1; i >= 0; i-- {
			if err := writeResult(ctx, conn, requests[i].MessageID, requests[i].Args); err != nil {
				return
			}
		}
		var ignored testRequest
		_ = wsjson.Read(ctx, conn, &ignored)
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, connected)
	results := make(chan error, count)
	for i := range count {
		go func() {
			var result int
			err := c.call(context.Background(), "echo", i, &result)
			if err == nil && result != i {
				err = errors.New("reply was delivered to the wrong request")
			}
			results <- err
		}()
	}
	for range count {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

func TestCanceledCallDoesNotCancelSessionOrAnotherRequest(t *testing.T) {
	slowSeen := make(chan struct{})
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := wsjson.Write(ctx, conn, testInfo()); err != nil {
			return
		}
		var slowID string
		for {
			var request testRequest
			if err := wsjson.Read(ctx, conn, &request); err != nil {
				return
			}
			if request.Command == "slow" {
				slowID = request.MessageID
				close(slowSeen)
				continue
			}
			if request.Command == "server_info" && slowID != "" {
				if err := writeResult(ctx, conn, slowID, "late reply"); err != nil {
					return
				}
			}
			if err := writeResult(ctx, conn, request.MessageID, testInfo()); err != nil {
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, connected)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		var ignored any
		result <- c.call(ctx, "slow", nil, &ignored)
	}()
	select {
	case <-slowSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not receive slow request")
	}
	// A completed second write/reply is a barrier: the slow request is now
	// waiting for its reply, not inside the WebSocket write operation.
	var barrier any
	if err := c.call(context.Background(), "barrier", nil, &barrier); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := c.ServerInfo(context.Background()); err != nil {
		t.Fatalf("session failed after cancellation/late reply: %v", err)
	}
	c.mu.Lock()
	s := c.current
	c.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) != 0 {
		t.Fatalf("canceled request leaked: %d pending", len(s.pending))
	}
}

func TestDisconnectFailsPendingCallAndReconnectDoesNotReplay(t *testing.T) {
	var connections, requests atomic.Int32
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		number := connections.Add(1)
		info := testInfo()
		if number > 1 {
			info.SDKVersion = "reconnected"
		}
		if err := wsjson.Write(ctx, conn, info); err != nil {
			return
		}
		for {
			var request testRequest
			if err := wsjson.Read(ctx, conn, &request); err != nil {
				return
			}
			requests.Add(1)
			if number == 1 {
				return // Drop the connection before answering its pending request.
			}
			if err := writeResult(ctx, conn, request.MessageID, info); err != nil {
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, connected)
	if _, err := c.ServerInfo(context.Background()); !errors.Is(err, api.ErrUnavailable) {
		t.Fatalf("expected unavailable pending call, got %v", err)
	}
	waitStatus(t, c, func(status api.ServerStatus) bool {
		return connected(status) && status.Server.SDKVersion == "reconnected"
	})
	if requests.Load() != 1 {
		t.Fatalf("request replayed during reconnect: count=%d", requests.Load())
	}
	if _, err := c.ServerInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("unexpected request count: %d", requests.Load())
	}
}

func TestIncompatibleHandshakeNeverBecomesReady(t *testing.T) {
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		info := testInfo()
		info.SchemaVersion, info.MinSupportedSchemaVersion = 16, 15
		if err := wsjson.Write(ctx, conn, info); err != nil {
			return
		}
		var request testRequest
		if err := wsjson.Read(ctx, conn, &request); err == nil {
			t.Error("client sent a command to an incompatible server")
		}
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, func(status api.ServerStatus) bool { return status.Connection.State == "incompatible" })
	if status := c.Status(); status.Server != nil || status.Connection.ConnectedAt != nil {
		t.Fatalf("incompatible server was advertised as ready: %+v", status)
	}
}

func TestRequestDeadlineAndShutdown(t *testing.T) {
	requestSeen := make(chan struct{}, 2)
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := wsjson.Write(ctx, conn, testInfo()); err != nil {
			return
		}
		for {
			var request testRequest
			if err := wsjson.Read(ctx, conn, &request); err != nil {
				return
			}
			requestSeen <- struct{}{}
		}
	})
	c, cancel, done := runClient(t, address)
	waitStatus(t, c, connected)
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if _, err := c.ServerInfo(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed-out request was not received")
	}
	result := make(chan error, 1)
	go func() { _, err := c.ServerInfo(context.Background()); result <- err }()
	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("second request not received")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, api.ErrUnavailable) {
			t.Fatalf("pending request after shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending request did not stop")
	}
	<-done
	if got := c.Status(); got.Connection.State != "disconnected" || got.Server != nil {
		t.Fatalf("stale status after shutdown: %+v", got)
	}
}

func TestUpstreamErrorDoesNotExposeDetails(t *testing.T) {
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := wsjson.Write(ctx, conn, testInfo()); err != nil {
			return
		}
		var request testRequest
		if err := wsjson.Read(ctx, conn, &request); err != nil {
			return
		}
		if err := wsjson.Write(ctx, conn, map[string]any{
			"message_id": request.MessageID, "error_code": 7,
			"message": "secret-pairing-code", "details": "secret-network-key",
		}); err != nil {
			return
		}
		_ = wsjson.Read(ctx, conn, &request)
	})
	c, _, _ := runClient(t, address)
	waitStatus(t, c, connected)
	_, err := c.ServerInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "error code 7") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected error disclosure: %v", err)
	}
}

func TestSchemaCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name string
		min  int
		max  int
		ok   bool
	}{
		{"installed", 11, 14, true}, {"older supported", 11, 11, true},
		{"newer backward compatible", 11, 15, true}, {"too old", 10, 10, false},
		{"too new minimum", 15, 16, false}, {"missing versions", 0, 0, false},
		{"inverted range", 14, 11, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := testInfo()
			info.MinSupportedSchemaVersion, info.SchemaVersion = tt.min, tt.max
			if err := validateInfo(info); (err == nil) != tt.ok {
				t.Fatalf("validateInfo() = %v, want valid=%v", err, tt.ok)
			}
		})
	}
}

func TestSilentHandshakeTimesOutAndBackoffIsCancelable(t *testing.T) {
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		var ignored any
		_ = wsjson.Read(ctx, conn, &ignored)
	})
	c, cancel, done := runClient(t, address, func(c *Client) {
		c.connectTimeout = 30 * time.Millisecond
		c.retryMin, c.retryMax = time.Minute, time.Minute
	})
	waitStatus(t, c, func(status api.ServerStatus) bool {
		return status.Connection.State == "disconnected" && status.Connection.LastError == "server handshake failed"
	})
	if c.Status().Server != nil {
		t.Fatal("silent peer was advertised as ready")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for the reconnect timer")
	}
}
