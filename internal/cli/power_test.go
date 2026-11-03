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

func TestPowerCLIReportsConfirmedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request api.PowerRequest
		if r.Method != http.MethodPost || r.URL.Path != api.PowerPath || json.NewDecoder(r.Body).Decode(&request) != nil || request.Target != "1" || request.Action != "on" || request.Endpoint == nil || *request.Endpoint != 1 || request.TimeoutMS != 2000 {
			t.Error("incorrect CLI power request")
		}
		_ = json.NewEncoder(w).Encode(api.PowerResult{NodeID: "1", Endpoint: 1, Power: true, ConfirmedAt: time.Now().UTC()})
	}))
	defer server.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"device", "power", "on", "1", "--endpoint", "1", "--timeout", "2s", "--server.url", server.URL})
	if err := command.Execute(); err != nil || !strings.Contains(output.String(), "Node 1 endpoint 1: on (confirmed") {
		t.Fatalf("unexpected CLI result: %v %s", err, &output)
	}
}
