package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func requestSpan(r *http.Request, o options, operation, path string) (context.Context, trace.Span) {
	ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	return o.tracer.Start(ctx, operation, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
		attribute.String("http.request.method", r.Method), attribute.String("http.path", path),
	))
}

func responseCode(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, api.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, api.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, api.ErrAmbiguous):
		return http.StatusConflict
	case errors.Is(err, api.ErrBusy):
		return http.StatusConflict
	case errors.Is(err, api.ErrInvalidRequest):
		return http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func jsonResponse(w http.ResponseWriter, logger *slog.Logger, span trace.Span, data any, err error) {
	code := responseCode(err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.SetAttributes(attribute.Int("http.response.status_code", code))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Debug("write API response", "error", err)
	}
}

func addDeviceRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("GET "+api.DevicesPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "list-devices", api.DevicesPath)
		defer span.End()
		inventory := api.Inventory{Connection: api.Connection{State: "disconnected"}, Devices: []api.Device{}}
		err := api.ErrUnavailable
		if o.devices != nil {
			inventory, err = o.devices.Inventory(ctx)
		}
		if err != nil {
			inventory.Error = err.Error()
		}
		jsonResponse(w, logger, span, inventory, err)
	})
	mux.HandleFunc("GET "+api.InspectPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "inspect-device", api.InspectPath)
		defer span.End()
		var device api.Device
		err := api.ErrUnavailable
		if o.devices != nil {
			device, err = o.devices.Inspect(ctx, r.URL.Query().Get("target"))
		}
		var response any = device
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
	mux.HandleFunc("GET "+api.EventsPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "watch-events", api.EventsPath)
		defer span.End()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var events <-chan api.Event
		err := api.ErrUnavailable
		if o.devices != nil {
			events, err = o.devices.Watch(ctx, r.URL.Query().Get("device"))
		}
		if err != nil {
			jsonResponse(w, logger, span, api.ErrorResponse{Error: err.Error()}, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		span.SetAttributes(attribute.Int("http.response.status_code", http.StatusOK))
		controller := http.NewResponseController(w)
		write := func(event api.Event) bool {
			failed := func() bool {
				err := errors.New("event stream write failed")
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return false
			}
			if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return failed()
			}
			if err := json.NewEncoder(w).Encode(event); err != nil {
				return failed()
			}
			if err := controller.Flush(); err != nil {
				return failed()
			}
			// Idle streams remain open; each actual write has its own deadline.
			if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return failed()
			}
			return true
		}
		ticker := time.NewTicker(o.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					span.SetStatus(codes.Error, "event stream interrupted")
					write(api.Event{Type: "stream_error", ObservedAt: time.Now().UTC(), Data: json.RawMessage(`{"error":"event stream interrupted; restart watch to resynchronize"}`)})
					return
				}
				if !write(event) {
					return
				}
			case now := <-ticker.C:
				if !write(api.Event{Type: "heartbeat", ObservedAt: now.UTC(), Data: json.RawMessage(`{}`)}) {
					return
				}
			}
		}
	})
}
