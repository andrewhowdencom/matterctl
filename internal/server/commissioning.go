package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func addCommissioningRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("GET "+api.DiscoverPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "discover-devices", api.DiscoverPath)
		defer span.End()
		ctx, cancel := context.WithTimeout(ctx, api.DiscoveryTimeout)
		defer cancel()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(api.DiscoveryTimeout + 10*time.Second))
		var result api.Discovery
		err := api.ErrUnavailable
		if o.commissioning != nil {
			result, err = o.commissioning.Discover(ctx)
		}
		var response any = result
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
	mux.HandleFunc("POST "+api.CommissionPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "commission-device", api.CommissionPath)
		defer span.End()
		if !sameOriginCommissionRequest(r) {
			const message = "cross-origin commissioning request rejected"
			span.SetAttributes(attribute.Int("http.response.status_code", http.StatusForbidden))
			span.SetStatus(codes.Error, message)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			if err := json.NewEncoder(w).Encode(api.ErrorResponse{Error: message}); err != nil {
				logger.Debug("write commissioning rejection", "error", err)
			}
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
		request, err := readCommissionRequest(w, r)
		_ = controller.SetReadDeadline(time.Time{})
		var device api.Device
		if err == nil {
			ctx, cancel := context.WithTimeout(ctx, request.Timeout())
			defer cancel()
			_ = controller.SetWriteDeadline(time.Now().Add(request.Timeout() + 10*time.Second))
			if o.commissioning == nil {
				err = api.ErrUnavailable
			} else {
				device, err = o.commissioning.Commission(ctx, request)
			}
		}
		var response any = device
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
}

// Browsers supply Origin on JSON POSTs. Requests without it remain available to
// non-browser clients such as the CLI; their existing API contract is unchanged.
func sameOriginCommissionRequest(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	origin := origins[0]
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.String() != origin {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return parsed.Scheme == scheme && strings.EqualFold(parsed.Host, r.Host)
}

func readCommissionRequest(w http.ResponseWriter, r *http.Request) (api.CommissionRequest, error) {
	var request api.CommissionRequest
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return request, errors.Join(api.ErrInvalidRequest, errors.New("Content-Type must be application/json"))
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return api.CommissionRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("provide one commissioning JSON object of at most 4096 bytes"))
	}
	return request, request.Validate()
}
