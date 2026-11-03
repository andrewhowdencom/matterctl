package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeAndShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, listener, logger)
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read response: %v; close response: %v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<h1 id=\"page-title\">Your devices") {
		t.Fatalf("unexpected response: status=%d body=%s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", got)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		if closeErr := connection.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("listener still accepts connections after shutdown")
	}
}

func TestWebAssets(t *testing.T) {
	h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range []struct {
		path, contentType, contains string
	}{
		{"/", "text/html; charset=utf-8", `<script type="module" src="/app.mjs">`},
		{"/app.css", "text/css; charset=utf-8", ".device-card"},
		{"/app.mjs", "text/javascript; charset=utf-8", "/api/v1/events"},
	} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != tt.contentType ||
			response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), tt.contains) {
			t.Errorf("GET %s: status=%d headers=%v body=%s", tt.path, response.Code, response.Header(), response.Body)
		}
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing-page", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown path returned %d", response.Code)
	}
}

func TestRunReportsOccupiedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Run(context.Background(), listener.Addr().String(), logger); err == nil {
		t.Fatal("expected an error for an occupied address")
	}
}
