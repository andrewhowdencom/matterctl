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

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type lightStub func(context.Context, api.LightRequest) (api.LightResult, error)

func (f lightStub) Light(ctx context.Context, request api.LightRequest) (api.LightResult, error) {
	return f(ctx, request)
}

func TestLightHTTPAcceptsSupportedRequestAndRejectsMalformed(t *testing.T) {
	called := false
	h := handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithLight(lightStub(func(_ context.Context, request api.LightRequest) (api.LightResult, error) {
		called = true
		if request.Target != "1" || request.Endpoint != 2 || request.Action != "color" || request.Color != "#40aaff" {
			t.Errorf("request = %+v", request)
		}
		return api.LightResult{NodeID: "1", Endpoint: 2, Action: "color"}, nil
	})))
	request := httptest.NewRequest(http.MethodPost, api.LightPath, strings.NewReader(`{"target":"1","endpoint":2,"action":"color","color":"#40aaff"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	var result api.LightResult
	if response.Code != 200 || !called || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Endpoint != 2 {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, api.LightPath, strings.NewReader(`{"target":"1","endpoint":2,"action":"color","color":"oops"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("invalid response = %d", response.Code)
	}
}
