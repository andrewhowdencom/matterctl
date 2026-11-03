package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func addPowerRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("POST "+api.PowerPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "power-device", api.PowerPath)
		defer span.End()
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		request, err := readPowerRequest(w, r)
		_ = controller.SetReadDeadline(time.Time{})
		var result api.PowerResult
		if err == nil {
			ctx, cancel := context.WithTimeout(ctx, request.Timeout())
			defer cancel()
			_ = controller.SetWriteDeadline(time.Now().Add(request.Timeout() + 2*time.Second))
			if o.power == nil {
				err = api.ErrUnavailable
			} else {
				result, err = o.power.Power(ctx, request)
			}
		}
		var response any = result
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
}

func readPowerRequest(w http.ResponseWriter, r *http.Request) (api.PowerRequest, error) {
	var request api.PowerRequest
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return request, errors.Join(api.ErrInvalidRequest, errors.New("Content-Type must be application/json"))
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return api.PowerRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("provide one power JSON object of at most 4096 bytes"))
	}
	return request, request.Validate()
}
