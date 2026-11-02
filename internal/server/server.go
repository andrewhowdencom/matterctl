package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Run serves the browser interface until the context is canceled.
func Run(ctx context.Context, address string, logger *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", address, err)
	}
	return serve(ctx, listener, logger)
}

func serve(ctx context.Context, listener net.Listener, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.WriteString(w, page); err != nil {
			logger.Debug("write page", "error", err)
		}
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(listener)
	}()
	logger.Info("HTTP server started", "address", listener.Addr().String())

	select {
	case err := <-result:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("HTTP server stopping")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			closeErr := server.Close()
			return errors.Join(fmt.Errorf("shut down HTTP server: %w", err), closeErr)
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}
}

const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>matterctl</title>
  <style>
    body { margin: 0; min-height: 100vh; display: grid; place-items: center;
           font-family: system-ui, sans-serif; background: #f5f4ef; color: #25251f; }
    main { text-align: center; }
    p { color: #66665c; }
    h1 { font-size: 2rem; font-weight: 500; }
  </style>
</head>
<body>
  <main><p>matterctl</p><h1>unimplemented</h1></main>
</body>
</html>
`
