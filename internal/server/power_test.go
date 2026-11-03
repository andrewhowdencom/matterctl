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

type powerStub func(context.Context, api.PowerRequest) (api.PowerResult, error)

func (f powerStub) Power(ctx context.Context, request api.PowerRequest) (api.PowerResult, error) {
	return f(ctx, request)
}

func TestPowerHTTPPassesEndpointAndDeadline(t *testing.T) {
	stub := powerStub(func(ctx context.Context, request api.PowerRequest) (api.PowerResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second || request.Target != "1" || request.Endpoint == nil || *request.Endpoint != 1 || request.Action != "on" {
			t.Error("incorrect power request or deadline")
		}
		return api.PowerResult{NodeID: "1", Endpoint: 1, Power: true, ConfirmedAt: time.Now().UTC()}, nil
	})
	request := httptest.NewRequest(http.MethodPost, api.PowerPath, strings.NewReader(`{"target":"1","endpoint":1,"action":"on","timeout_ms":2000}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithPower(stub)).ServeHTTP(response, request)
	var result api.PowerResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.Power {
		t.Fatalf("unexpected power response: %d %s", response.Code, response.Body.String())
	}
}

func TestPowerHTTPRejectsUnsupportedAction(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, api.PowerPath, strings.NewReader(`{"target":"1","action":"blink"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithPower(powerStub(func(context.Context, api.PowerRequest) (api.PowerResult, error) {
		t.Fatal("invalid request reached power source")
		return api.PowerResult{}, nil
	}))).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid action status: %d", response.Code)
	}
}
