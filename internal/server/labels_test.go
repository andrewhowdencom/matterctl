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

type labelStub func(context.Context, api.LabelRequest) (api.LabelResult, error)

func (f labelStub) SetLabel(ctx context.Context, request api.LabelRequest) (api.LabelResult, error) {
	return f(ctx, request)
}

func TestSetLabelHTTPAllowsExplicitClearAndRejectsMissingLabel(t *testing.T) {
	server := handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithLabels(labelStub(func(_ context.Context, request api.LabelRequest) (api.LabelResult, error) {
		return api.LabelResult{NodeID: "1", Label: request.Label}, nil
	})))
	for _, tc := range []struct {
		body string
		want int
	}{{`{"target":"1","label":""}`, 200}, {`{"target":"1"}`, 400}, {`{"target":"1","label":null}`, 400}, {`{"target":"1","label":"x","extra":1}`, 400}} {
		request := httptest.NewRequest(http.MethodPost, api.LabelPath, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("%s: status %d, want %d", tc.body, response.Code, tc.want)
		}
		if tc.want == 200 {
			var result api.LabelResult
			if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Label != "" {
				t.Fatalf("clear response: %s", response.Body.String())
			}
		}
	}
}
