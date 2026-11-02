package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
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
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<h1>unimplemented</h1>") {
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
