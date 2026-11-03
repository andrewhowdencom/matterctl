package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestLabelCLIWritesMatterNodeLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request api.LabelRequest
		if r.Method != http.MethodPost || r.URL.Path != api.LabelPath || json.NewDecoder(r.Body).Decode(&request) != nil || request.Target != "1" || request.Label != "Couch lamp" || request.Endpoint != nil {
			t.Error("incorrect CLI label request")
		}
		_ = json.NewEncoder(w).Encode(api.LabelResult{NodeID: "1", Label: "Couch lamp", ConfirmedAt: time.Now().UTC()})
	}))
	defer server.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"device", "label", "set", "1", "Couch lamp", "--server.url", server.URL})
	if err := command.Execute(); err != nil || !strings.Contains(output.String(), `Node 1 endpoint 0: label "Couch lamp" (confirmed`) {
		t.Fatalf("unexpected CLI result: %v %s", err, &output)
	}
}
