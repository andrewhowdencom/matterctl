package server

import (
	"bytes"
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

func addLabelRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("POST "+api.LabelPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "set-device-label", api.LabelPath)
		defer span.End()
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		request, err := readLabelRequest(w, r)
		_ = controller.SetReadDeadline(time.Time{})
		var result api.LabelResult
		if err == nil {
			ctx, cancel := context.WithTimeout(ctx, request.Timeout())
			defer cancel()
			_ = controller.SetWriteDeadline(time.Now().Add(request.Timeout() + 2*time.Second))
			if o.labels == nil {
				err = api.ErrUnavailable
			} else {
				result, err = o.labels.SetLabel(ctx, request)
			}
		}
		var response any = result
		if err != nil {
			response = api.ErrorResponse{Error: err.Error()}
		}
		jsonResponse(w, logger, span, response, err)
	})
}

func readLabelRequest(w http.ResponseWriter, r *http.Request) (api.LabelRequest, error) {
	var request api.LabelRequest
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return request, errors.Join(api.ErrInvalidRequest, errors.New("Content-Type must be application/json"))
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	var body json.RawMessage
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		return api.LabelRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("provide one label JSON object of at most 4096 bytes"))
	}
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	if strict.Decode(&request) != nil {
		return api.LabelRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("invalid label request fields"))
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil || len(fields["label"]) == 0 || fields["label"][0] != '"' {
		return api.LabelRequest{}, errors.Join(api.ErrInvalidRequest, errors.New("label field is required; use an empty string to clear it"))
	}
	return request, request.Validate()
}
