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

func TestCommissionCLIStdinAndJSONOutput(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request api.CommissionRequest
		if r.Method != http.MethodPost || r.URL.Path != api.CommissionPath || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Code != "35325335079" || !request.NetworkOnly || request.TimeoutMS != 120000 {
			t.Error("incorrect commissioning HTTP request")
		}
		if err := json.NewEncoder(w).Encode(api.Device{NodeID: "42", Label: "Lamp"}); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output, progress bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&progress)
	command.SetIn(strings.NewReader("3532-533-5079\n"))
	command.SetArgs([]string{"device", "commission", "--code-stdin", "--network-only", "--timeout", "2m", "--output", "json", "--server.url", s.URL})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(output.Bytes()) || !strings.Contains(output.String(), `"node_id": "42"`) || !strings.Contains(progress.String(), "Waiting") || !strings.Contains(progress.String(), "completed") {
		t.Fatalf("unexpected output: %s %s", &output, &progress)
	}
	if strings.Contains(output.String()+progress.String(), "3532") {
		t.Fatal("sharing code leaked")
	}
}

func TestCommissionCLIRejectsInvalidOptionsBeforeRequest(t *testing.T) {
	t.Setenv("MATTERCTL_CODE", "35325335079")
	for _, args := range [][]string{
		{}, {"--code", "invalid-secret"}, {"--code", "35325335079", "--network-only=false"},
		{"--code", "35325335079", "--timeout", "0s"}, {"--code", "35325335079", "--timeout", "11m"},
		{"--code", "35325335079", "--code-stdin"}, {"--code-stdin"},
	} {
		command, err := NewCommand()
		if err != nil {
			t.Fatal(err)
		}
		command.SetIn(strings.NewReader(""))
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(append([]string{"device", "commission"}, args...))
		err = command.Execute()
		if err == nil || strings.Contains(err.Error()+output.String(), "invalid-secret") || strings.Contains(err.Error()+output.String(), "35325335079") {
			t.Fatalf("missing or unsafe error: %v %s", err, &output)
		}
	}
}

func TestDiscoverCLI(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != api.DiscoverPath {
			t.Error("unexpected discovery request")
		}
		if err := json.NewEncoder(w).Encode(api.Discovery{ObservedAt: time.Now().UTC(), Devices: []api.Commissionable{{InstanceName: "ADVERTISEMENT", DeviceName: "Lamp"}}}); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"device", "discover", "--server.url", s.URL})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ADVERTISEMENT") || !strings.Contains(output.String(), "Lamp") {
		t.Fatal("missing advertised identity")
	}
}
