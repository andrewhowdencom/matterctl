package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type infoSourceFunc func(context.Context) (api.ServerStatus, error)

func (f infoSourceFunc) ServerInfo(ctx context.Context) (api.ServerStatus, error) { return f(ctx) }

func TestServerInfoHTTPStatusAndHealth(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state string
		err   error
		code  int
	}{
		{"connected", "connected", nil, http.StatusOK},
		{"disconnected", "disconnected", api.ErrUnavailable, http.StatusServiceUnavailable},
		{"incompatible", "incompatible", api.ErrUnavailable, http.StatusServiceUnavailable},
		{"deadline", "connected", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"upstream rejected", "connected", errors.New("upstream rejected request"), http.StatusBadGateway},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := infoSourceFunc(func(ctx context.Context) (api.ServerStatus, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Error("HTTP handler did not bound its upstream request")
				}
				status := api.ServerStatus{Connection: api.Connection{State: tt.state}}
				if tt.err == nil {
					status.Server = &api.ServerInfo{SDKVersion: "test", SchemaVersion: 14, MinSupportedSchemaVersion: 11}
				}
				return status, tt.err
			})
			h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithMatter(source))
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, api.ServerInfoPath, nil))
			if response.Code != tt.code {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, tt.code, response.Body)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected headers: %v", response.Header())
			}
			var status api.ServerStatus
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if status.Connection.State != tt.state || (status.Error != "") != (tt.err != nil) {
				t.Fatalf("unexpected status: %+v", status)
			}
			alive := httptest.NewRecorder()
			h.ServeHTTP(alive, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if alive.Code != http.StatusOK {
				t.Fatalf("HTTP liveness depends on upstream availability: %d", alive.Code)
			}
		})
	}
}

func TestServerInfoExtractsTraceContext(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	source := infoSourceFunc(func(ctx context.Context) (api.ServerStatus, error) {
		if got := trace.SpanContextFromContext(ctx).TraceID().String(); got != traceID {
			t.Errorf("trace ID = %s, want %s", got, traceID)
		}
		return api.ServerStatus{Connection: api.Connection{State: "disconnected"}}, api.ErrUnavailable
	})
	h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithMatter(source))
	request := httptest.NewRequest(http.MethodGet, api.ServerInfoPath, nil)
	request.Header.Set("Traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), request)
}

func TestServerInfoRouteRejectsMutations(t *testing.T) {
	h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, api.ServerInfoPath, nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST returned %d", response.Code)
	}
}
