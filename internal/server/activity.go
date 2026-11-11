package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func addActivityRoutes(mux *http.ServeMux, o options, logger *slog.Logger) {
	mux.HandleFunc("GET "+api.HistoryPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "list-history", api.HistoryPath)
		defer span.End()
		query, err := parseHistoryQuery(r)
		var page api.HistoryPage
		if err == nil {
			if o.history == nil {
				err = api.ErrUnavailable
			} else {
				page, err = o.history.History(ctx, query)
			}
		}
		if err != nil {
			jsonResponse(w, logger, span, api.ErrorResponse{Error: err.Error()}, err)
			return
		}
		jsonResponse(w, logger, span, page, nil)
	})
	mux.HandleFunc("GET "+api.HistoryPath+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "inspect-history", api.HistoryPath)
		defer span.End()
		id, parseErr := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var entry api.HistoryEntry
		err := parseErr
		if err != nil {
			err = errors.Join(api.ErrInvalidRequest, err)
		}
		if err == nil && id <= 0 {
			err = api.ErrInvalidRequest
		}
		if err == nil {
			if o.history == nil {
				err = api.ErrUnavailable
			} else {
				entry, err = o.history.HistoryEntry(ctx, id)
			}
		}
		if err != nil {
			jsonResponse(w, logger, span, api.ErrorResponse{Error: err.Error()}, err)
			return
		}
		jsonResponse(w, logger, span, entry, nil)
	})
	mux.HandleFunc("GET "+api.AutomationsPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "list-automations", api.AutomationsPath)
		defer span.End()
		var files []api.AutomationFile
		err := api.ErrUnavailable
		if o.automations != nil {
			files, err = o.automations.Automations(ctx)
		}
		if err != nil {
			jsonResponse(w, logger, span, api.ErrorResponse{Error: err.Error()}, err)
			return
		}
		if files == nil {
			files = []api.AutomationFile{}
		}
		jsonResponse(w, logger, span, files, nil)
	})
	mux.HandleFunc("PUT "+api.AutomationsPath+"/{file}/pause", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := requestSpan(r, o, "pause-automation", api.AutomationsPath)
		defer span.End()
		if !sameOriginCommissionRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Error: "cross-origin automation request rejected"})
			return
		}
		paused, err := readPauseRequest(w, r)
		var file api.AutomationFile
		if err == nil {
			if o.automations == nil {
				err = api.ErrUnavailable
			} else {
				file, err = o.automations.SetAutomationPaused(ctx, r.PathValue("file"), paused)
			}
		}
		if err != nil {
			jsonResponse(w, logger, span, api.ErrorResponse{Error: err.Error()}, err)
			return
		}
		jsonResponse(w, logger, span, file, nil)
	})
}

func parseHistoryQuery(r *http.Request) (api.HistoryQuery, error) {
	values := r.URL.Query()
	query := api.HistoryQuery{Since: time.Now().UTC().Add(-7 * 24 * time.Hour), Limit: 50}
	if raw := values.Get("since"); raw != "" {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return query, errors.Join(api.ErrInvalidRequest, errors.New("since must be RFC3339"))
		}
		query.Since = at
	}
	if raw := values.Get("until"); raw != "" {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return query, errors.Join(api.ErrInvalidRequest, errors.New("until must be RFC3339"))
		}
		query.Until = at
	}
	if raw := values.Get("device"); raw != "" {
		number, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return query, errors.Join(api.ErrInvalidRequest, errors.New("device must be a node ID"))
		}
		query.Device = api.NodeID(strconv.FormatUint(number, 10))
	}
	query.File = values.Get("file")
	query.Kind = values.Get("kind")
	query.Outcome = values.Get("outcome")
	query.RunID = values.Get("run")
	for _, field := range []struct {
		key    string
		target *int64
	}{{"before", &query.Before}, {"after", &query.After}} {
		if raw := values.Get(field.key); raw != "" {
			number, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || number <= 0 {
				return query, errors.Join(api.ErrInvalidRequest, errors.New(field.key+" must be a positive history ID"))
			}
			*field.target = number
		}
	}
	if query.Before > 0 && query.After > 0 {
		return query, errors.Join(api.ErrInvalidRequest, errors.New("before and after cannot be combined"))
	}
	if raw := values.Get("limit"); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < 1 || number > 100 {
			return query, errors.Join(api.ErrInvalidRequest, errors.New("limit must be 1 to 100"))
		}
		query.Limit = number
	}
	if !query.Until.IsZero() && query.Until.Before(query.Since) {
		return query, errors.Join(api.ErrInvalidRequest, errors.New("until precedes since"))
	}
	return query, nil
}

func readPauseRequest(w http.ResponseWriter, r *http.Request) (bool, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false, errors.Join(api.ErrInvalidRequest, errors.New("Content-Type must be application/json"))
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128)
	var request struct {
		Paused *bool `json:"paused"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Paused == nil {
		return false, errors.Join(api.ErrInvalidRequest, errors.New("provide one paused Boolean"))
	}
	return *request.Paused, nil
}
