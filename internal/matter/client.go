// Package matter implements the OHF Matter Server WebSocket protocol.
package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

const (
	minSchema = 11
	maxSchema = 14
)

type Client struct {
	url         string
	logger      *slog.Logger
	tracer      trace.Tracer
	mu          sync.Mutex
	current     *session
	status      api.ServerStatus
	serial      uint64
	nodes       map[api.NodeID]node
	ready       bool
	observedAt  time.Time
	watchers    map[uint64]watcher
	watchSerial uint64

	connectTimeout time.Duration
	syncTimeout    time.Duration
	requestTimeout time.Duration
	retryMin       time.Duration
	retryMax       time.Duration
}

type Option func(*Client)

func WithTracer(tracer trace.Tracer) Option {
	return func(c *Client) { c.tracer = tracer }
}

func New(address string, logger *slog.Logger, options ...Option) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("invalid matter.url: require an absolute ws:// or wss:// URL without user information or a fragment")
	}
	c := &Client{
		url: address, logger: logger,
		tracer:         otel.Tracer("github.com/andrewhowdencom/matterctl/internal/matter"),
		status:         api.ServerStatus{Connection: api.Connection{State: "disconnected"}},
		connectTimeout: 5 * time.Second, requestTimeout: 2 * time.Second,
		syncTimeout: 10 * time.Second,
		nodes:       make(map[api.NodeID]node), watchers: make(map[uint64]watcher),
		retryMin: 250 * time.Millisecond, retryMax: 30 * time.Second,
	}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

// Run owns the session lifecycle. Call it once and cancel ctx to stop it.
// Reconnection never replays requests from an earlier session.
func (c *Client) Run(ctx context.Context) {
	delay := c.retryMin
	for ctx.Err() == nil {
		c.setDisconnected("connecting", "")
		connected, state, reason := c.connect(ctx)
		if ctx.Err() != nil {
			break
		}
		c.setDisconnected(state, reason)
		c.logger.Warn("Matter Server connection unavailable", "reason", reason)
		if connected {
			delay = c.retryMin
		}
		// Retry jitter is scheduling noise, not a security value.
		pause := delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1)) // #nosec G404
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		delay = min(delay*2, c.retryMax)
	}
	c.setDisconnected("disconnected", "")
	c.mu.Lock()
	for key, w := range c.watchers {
		delete(c.watchers, key)
		close(w.ch)
	}
	c.mu.Unlock()
	c.logger.Info("Matter Server connection stopped")
}

func (c *Client) connect(ctx context.Context) (bool, string, string) {
	connectCtx, cancel := context.WithTimeout(ctx, c.connectTimeout)
	connectCtx, span := c.tracer.Start(connectCtx, "connect-matter-server", trace.WithSpanKind(trace.SpanKindClient))
	header := make(http.Header)
	propagation.TraceContext{}.Inject(connectCtx, propagation.HeaderCarrier(header))
	conn, _, err := websocket.Dial(connectCtx, c.url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		cancel()
		span.RecordError(errors.New("connection failed"))
		span.SetStatus(codes.Error, "connection failed")
		span.End()
		return false, "disconnected", "connection failed"
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(8 << 20)
	var info api.ServerInfo
	err = wsjson.Read(connectCtx, conn, &info)
	cancel()
	if err != nil {
		span.RecordError(errors.New("server handshake failed"))
		span.SetStatus(codes.Error, "server handshake failed")
		span.End()
		return false, "disconnected", "server handshake failed"
	}
	if err := validateInfo(info); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return false, "incompatible", err.Error()
	}
	span.End()
	s := &session{conn: conn, done: make(chan struct{}), pending: make(map[string]chan reply)}
	syncCtx, syncCancel := context.WithTimeout(ctx, c.syncTimeout)
	defer syncCancel()
	_, syncSpan := c.tracer.Start(syncCtx, "matter.start_listening", trace.WithSpanKind(trace.SpanKindClient))
	defer syncSpan.End()
	c.mu.Lock()
	c.serial++
	s.snapshotID = strconv.FormatUint(c.serial, 10)
	c.mu.Unlock()
	if err := wsjson.Write(syncCtx, conn, map[string]string{"message_id": s.snapshotID, "command": "start_listening"}); err != nil {
		syncSpan.SetStatus(codes.Error, "inventory subscription failed")
		return false, "disconnected", "inventory subscription failed"
	}
	now := time.Now().UTC()
	c.mu.Lock()
	c.current = s
	c.ready = false
	c.status = api.ServerStatus{Connection: api.Connection{State: "connected", ConnectedAt: &now}, Server: &info}
	c.mu.Unlock()
	c.logger.Info("Matter Server connected", "schema", info.SchemaVersion)
	// Only this goroutine reads the socket; individual RPC deadlines cannot
	// cancel the reader or discard unrelated in-flight responses.
	reason := s.read(ctx, syncCtx, c, syncSpan)
	c.mu.Lock()
	synchronized := c.ready
	c.current = nil
	c.ready = false
	c.status = api.ServerStatus{Connection: api.Connection{State: "disconnected", LastError: reason}}
	close(s.done)
	c.mu.Unlock()
	return synchronized, "disconnected", reason
}

func validateInfo(info api.ServerInfo) error {
	if info.SDKVersion == "" || info.MinSupportedSchemaVersion <= 0 || info.SchemaVersion < info.MinSupportedSchemaVersion {
		return errors.New("invalid server information")
	}
	if info.SchemaVersion < minSchema || info.MinSupportedSchemaVersion > maxSchema {
		return fmt.Errorf("incompatible schema: server %d-%d, client %d-%d", info.MinSupportedSchemaVersion, info.SchemaVersion, minSchema, maxSchema)
	}
	return nil
}

func (c *Client) setDisconnected(state, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = api.ServerStatus{Connection: api.Connection{State: state, LastError: reason}}
	c.ready = false
	c.publishLocked(newEvent("connection", "", struct {
		Connection api.Connection `json:"connection"`
		Ready      bool           `json:"ready"`
	}{c.status.Connection, false}))
}

// Status returns a snapshot; metadata from disconnected sessions is not retained.
func (c *Client) Status() api.ServerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.status
	if status.Server != nil {
		info := *status.Server
		status.Server = &info
	}
	if status.Connection.ConnectedAt != nil {
		connectedAt := *status.Connection.ConnectedAt
		status.Connection.ConnectedAt = &connectedAt
	}
	return status
}

// ServerInfo verifies responsiveness with a correlated server_info round trip.
func (c *Client) ServerInfo(ctx context.Context) (api.ServerStatus, error) {
	before := c.Status()
	var info api.ServerInfo
	if err := c.call(ctx, "server_info", nil, &info); err != nil {
		return c.Status(), err
	}
	if err := validateInfo(info); err != nil {
		return c.Status(), err
	}
	status := c.Status()
	if status.Connection.State != "connected" || before.Connection.ConnectedAt == nil || status.Connection.ConnectedAt == nil || !before.Connection.ConnectedAt.Equal(*status.Connection.ConnectedAt) {
		return status, api.ErrUnavailable
	}
	status.Server = &info
	return status, nil
}

func (c *Client) call(ctx context.Context, command string, args any, result any) (err error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	ctx, span := c.tracer.Start(ctx, "matter."+command, trace.WithSpanKind(trace.SpanKindClient))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	s := c.current
	c.serial++
	id := strconv.FormatUint(c.serial, 10)
	c.mu.Unlock()
	if s == nil {
		return api.ErrUnavailable
	}
	response := make(chan reply, 1)
	s.mu.Lock()
	s.pending[id] = response
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	request := struct {
		MessageID string `json:"message_id"`
		Command   string `json:"command"`
		Args      any    `json:"args,omitempty"`
	}{id, command, args}
	select {
	case <-s.done:
		return api.ErrUnavailable
	default:
	}
	if err := wsjson.Write(ctx, s.conn, request); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return api.ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return api.ErrUnavailable
	case response := <-response:
		if response.ErrorCode != nil {
			// Upstream messages/details can contain arbitrary request data.
			return fmt.Errorf("upstream Matter Server returned error code %d", *response.ErrorCode)
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return errors.New("invalid Matter Server response")
		}
		return nil
	}
}

type reply struct {
	MessageID string          `json:"message_id"`
	Result    json.RawMessage `json:"result"`
	ErrorCode *int            `json:"error_code"`
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
}

type session struct {
	conn       *websocket.Conn
	done       chan struct{}
	mu         sync.Mutex
	pending    map[string]chan reply
	snapshotID string
}

func (s *session) read(ctx, syncCtx context.Context, c *Client, syncSpan trace.Span) string {
	synchronized := false
	buffered := []reply{}
	bufferBytes := 0
	for {
		var response reply
		readCtx := ctx
		if !synchronized {
			readCtx = syncCtx
		}
		if err := wsjson.Read(readCtx, s.conn, &response); err != nil {
			if !synchronized && errors.Is(syncCtx.Err(), context.DeadlineExceeded) {
				return "inventory synchronization timed out"
			}
			return "connection closed"
		}
		if response.Event == "server_shutdown" {
			return "server shutting down"
		}
		if response.MessageID == s.snapshotID {
			if synchronized {
				continue
			}
			if response.ErrorCode != nil || c.applySnapshot(s, response.Result, buffered) != nil {
				syncSpan.SetStatus(codes.Error, "inventory synchronization failed")
				return "inventory synchronization failed"
			}
			syncSpan.End()
			synchronized, buffered = true, nil
			continue
		}
		if response.MessageID == "" {
			if response.Event == "" {
				continue
			}
			if !synchronized {
				bufferBytes += len(response.Data)
				if len(buffered) >= 128 || bufferBytes > 8<<20 {
					return "inventory synchronization overflow"
				}
				buffered = append(buffered, response)
			} else if err := c.applyEvent(s, response); err != nil {
				return "invalid inventory update"
			}
			continue
		}
		s.mu.Lock()
		pending := s.pending[response.MessageID]
		delete(s.pending, response.MessageID)
		s.mu.Unlock()
		if pending != nil {
			pending <- response
		}
	}
}
