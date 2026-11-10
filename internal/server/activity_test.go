package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type activitySource struct {
	query  api.HistoryQuery
	paused bool
}

func (s *activitySource) History(_ context.Context, query api.HistoryQuery) (api.HistoryPage, error) {
	s.query = query
	return api.HistoryPage{Entries: []api.HistoryEntry{{ID: 7, Kind: "device.state", Summary: "Den Light off"}},
		Recording: api.RecordingStatus{State: "healthy"}}, nil
}
func (*activitySource) HistoryEntry(_ context.Context, id int64) (api.HistoryEntry, error) {
	return api.HistoryEntry{ID: id, Kind: "device.state", Summary: "Den Light off"}, nil
}
func (*activitySource) RecordingStatus() api.RecordingStatus {
	return api.RecordingStatus{State: "healthy"}
}
func (*activitySource) Record(api.HistoryEntry) {}
func (s *activitySource) Automations(context.Context) ([]api.AutomationFile, error) {
	return []api.AutomationFile{{Name: "den.star", Rules: 2, Status: "active", Paused: s.paused}}, nil
}
func (s *activitySource) SetAutomationPaused(_ context.Context, name string, paused bool) (api.AutomationFile, error) {
	s.paused = paused
	return api.AutomationFile{Name: name, Rules: 2, Paused: paused, Status: "paused"}, nil
}

func TestActivityAPIUsesSameHistoryAndPauseContract(t *testing.T) {
	source := &activitySource{}
	handler := handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithHistory(source), WithAutomations(source))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/history?device=1&file=den.star&limit=5", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("history status %d: %s", response.Code, response.Body.String())
	}
	if source.query.Device != "1" || source.query.File != "den.star" || source.query.Limit != 5 ||
		time.Since(source.query.Since) > 8*24*time.Hour {
		t.Fatalf("history query %#v", source.query)
	}
	var page api.HistoryPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("history response %#v, %v", page, err)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/automations/den.star/pause", strings.NewReader(`{"paused":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://elsewhere.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || source.paused {
		t.Fatalf("cross-origin pause status %d, paused=%t", response.Code, source.paused)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/automations/den.star/pause", strings.NewReader(`{"paused":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://example.com")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !source.paused {
		t.Fatalf("same-origin pause status %d, paused=%t", response.Code, source.paused)
	}
}

func TestHistoryQueryRejectsConflictingCursors(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/history?before=1&after=2", nil)
	if _, err := parseHistoryQuery(request); err == nil {
		t.Fatal("accepted conflicting cursors")
	}
}
