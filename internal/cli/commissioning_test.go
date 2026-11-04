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

func TestCommissionCLIAddsRequestedNameAfterCommissioning(t *testing.T) {
	var paths []string
	var labelAttempts int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case api.CommissionPath:
			_ = json.NewEncoder(w).Encode(api.Device{NodeID: "42", Product: "Lamp"})
		case api.LabelPath:
			labelAttempts++
			if labelAttempts == 1 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"device not found"}`))
				return
			}
			var request api.LabelRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.Target != "42" || request.Label != "Den Light" {
				t.Error("incorrect label request")
			}
			_ = json.NewEncoder(w).Encode(api.LabelResult{NodeID: "42", Label: "Den Light", ConfirmedAt: time.Now().UTC()})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"device", "commission", "--code", "35325335079", "--name", "Den Light", "--output", "json", "--server.url", s.URL})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths[0] != api.CommissionPath || paths[1] != api.LabelPath || paths[2] != api.LabelPath || !strings.Contains(output.String(), `"label": "Den Light"`) {
		t.Fatalf("unexpected commissioned result: %s, paths: %v", &output, paths)
	}
}

func TestCommissionCLIReportsNameFailureWithoutRetryingCommission(t *testing.T) {
	var commissions int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.CommissionPath {
			commissions++
			_ = json.NewEncoder(w).Encode(api.Device{NodeID: "42"})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"label rejected"}`))
	}))
	defer s.Close()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output, progress bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&progress)
	command.SetArgs([]string{"device", "commission", "--code", "35325335079", "--name", "Den Light", "--server.url", s.URL})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if commissions != 1 || !strings.Contains(progress.String(), "Device added as node 42, but its name was not confirmed") || !strings.Contains(output.String(), "42") {
		t.Fatalf("ambiguous result: output=%s progress=%s commissions=%d", &output, &progress, commissions)
	}
}

func TestCommissionCLIRejectsInvalidOptionsBeforeRequest(t *testing.T) {
	t.Setenv("MATTERCTL_CODE", "35325335079")
	for _, args := range [][]string{
		{}, {"--code", "invalid-secret"}, {"--code", "35325335079", "--network-only=false"},
		{"--code", "35325335079", "--timeout", "0s"}, {"--code", "35325335079", "--timeout", "11m"},
		{"--code", "35325335079", "--code-stdin"}, {"--code-stdin"},
		{"--code", "35325335079", "--name", " ", "--output", "json"},
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
