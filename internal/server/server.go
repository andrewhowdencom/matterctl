package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type options struct {
	matter        api.InfoSource
	devices       api.DeviceSource
	commissioning api.CommissioningSource
	power         api.PowerSource
	labels        api.LabelSource
	heartbeat     time.Duration
	tracer        trace.Tracer
}

func WithDevices(source api.DeviceSource) Option {
	return func(o *options) { o.devices = source }
}

func WithCommissioning(source api.CommissioningSource) Option {
	return func(o *options) { o.commissioning = source }
}

func WithPower(source api.PowerSource) Option {
	return func(o *options) { o.power = source }
}

func WithLabels(source api.LabelSource) Option {
	return func(o *options) { o.labels = source }
}

type Option func(*options)

func WithMatter(source api.InfoSource) Option {
	return func(o *options) { o.matter = source }
}

func WithTracer(tracer trace.Tracer) Option {
	return func(o *options) { o.tracer = tracer }
}

// Run serves the browser interface until the context is canceled.
func Run(ctx context.Context, address string, logger *slog.Logger, opts ...Option) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", address, err)
	}
	return serve(ctx, listener, logger, opts...)
}

func handler(logger *slog.Logger, opts ...Option) http.Handler {
	options := options{tracer: otel.Tracer("github.com/andrewhowdencom/matterctl/internal/server"), heartbeat: 15 * time.Second}
	for _, option := range opts {
		option(&options)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.WriteString(w, page); err != nil {
			logger.Debug("write page", "error", err)
		}
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"status":"ok"}`+"\n"); err != nil {
			logger.Debug("write health response", "error", err)
		}
	})
	mux.HandleFunc("GET "+api.ServerInfoPath, func(w http.ResponseWriter, r *http.Request) {
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := options.tracer.Start(ctx, "server-info", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
			attribute.String("http.request.method", r.Method), attribute.String("http.path", api.ServerInfoPath),
		))
		defer span.End()
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		status := api.ServerStatus{Connection: api.Connection{State: "disconnected"}}
		var err error
		if options.matter == nil {
			err = api.ErrUnavailable
		} else {
			status, err = options.matter.ServerInfo(ctx)
		}
		code := http.StatusOK
		if err != nil {
			status.Error = err.Error()
			code = http.StatusBadGateway
			if errors.Is(err, api.ErrUnavailable) {
				code = http.StatusServiceUnavailable
			} else if errors.Is(err, context.DeadlineExceeded) {
				code = http.StatusGatewayTimeout
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Int("http.response.status_code", code))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(status); err != nil {
			logger.Debug("write server information response", "error", err)
		}
	})
	addDeviceRoutes(mux, options, logger)
	addCommissioningRoutes(mux, options, logger)
	addPowerRoutes(mux, options, logger)
	addLabelRoutes(mux, options, logger)
	return mux
}

func serve(ctx context.Context, listener net.Listener, logger *slog.Logger, opts ...Option) error {
	server := &http.Server{
		Handler:           handler(logger, opts...),
		BaseContext:       func(net.Listener) context.Context { return ctx },
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
