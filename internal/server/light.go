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

func addLightRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("POST "+api.LightPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "control-light", api.LightPath)
		defer span.End()
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		request, err := readLightRequest(w, r)
		_ = controller.SetReadDeadline(time.Time{})
		var result api.LightResult
		if err == nil {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			_ = controller.SetWriteDeadline(time.Now().Add(12 * time.Second))
			if o.light == nil {
				err = api.ErrUnavailable
			} else {
				result, err = o.light.Light(ctx, request)
			}
		}
		var response any = result
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
}

func readLightRequest(w http.ResponseWriter, r *http.Request) (api.LightRequest, error) {
	var request api.LightRequest
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return request, errors.Join(api.ErrInvalidRequest, errors.New("Content-Type must be application/json"))
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return api.LightRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("provide one light JSON object of at most 4096 bytes"))
	}
	return request, request.Validate()
}
