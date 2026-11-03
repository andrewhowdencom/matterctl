package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestHTTPStartupFailureStopsBackgroundConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	result := make(chan error, 1)
	go func() {
		result <- Serve(context.Background(), listener.Addr().String(), "ws://127.0.0.1:1/ws", t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected occupied HTTP address error")
		}
	case <-time.After(time.Second):
		t.Fatal("startup failure did not stop and join the upstream goroutine")
	}
}
