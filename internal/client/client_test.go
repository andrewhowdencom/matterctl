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

	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestServerInfoRequestAndTracePropagation(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/prefix"+api.ServerInfoPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Traceparent"), traceID) {
			t.Error("W3C trace context was not injected")
		}
		status := api.ServerStatus{Connection: api.Connection{State: "connected"}, Server: &api.ServerInfo{SDKVersion: "test", SchemaVersion: 14}}
		if err := json.NewEncoder(w).Encode(status); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	c, err := New(server.URL + "/prefix")
	if err != nil {
		t.Fatal(err)
	}
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	}))
	status, err := c.ServerInfo(ctx)
	if err != nil || status.Server == nil || status.Server.SDKVersion != "test" {
		t.Fatalf("ServerInfo() = %+v, %v", status, err)
	}
}

func TestServerInfoErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		code int
		body string
	}{
		{"upstream unavailable", 503, `{"connection":{"state":"disconnected","last_error":"connection failed"},"error":"Matter Server is unavailable"}`},
		{"wrong service", 200, `<html>unimplemented</html>`},
		{"missing server metadata", 200, `{"connection":{"state":"connected"}}`},
		{"disconnected success", 200, `{"connection":{"state":"disconnected"}}`},
		{"oversized body", 200, strings.Repeat(" ", 64<<10) + `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			c, err := New(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.ServerInfo(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestServerInfoDeadlineCancelsHTTP(t *testing.T) {
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	c, err := New(server.URL, WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ServerInfo(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP handler did not observe cancellation")
	}
}

func TestInvalidServiceURLsDoNotEchoSecrets(t *testing.T) {
	for _, address := range []string{"relative", "ws://localhost", "http://user:secret@example.com", "http://localhost?token=secret", "http://localhost#secret", "://"} {
		t.Run(address, func(t *testing.T) {
			if _, err := New(address); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
