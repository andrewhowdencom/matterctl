package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestDeviceCommandsUseAPIAndPreserveLargeIDs(t *testing.T) {
	device := api.Device{NodeID: "18446744069414584320", Label: "Couch lamp", Available: true, Endpoints: []api.Endpoint{{ID: 1, ServerClusters: []api.Cluster{{ID: 6, Name: "OnOff", AcceptedCommands: []api.Command{{ID: 1, Name: "on"}}}}, Capabilities: []string{"power.on"}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any = device
		if r.URL.Path == api.DevicesPath {
			result = api.Inventory{Connection: api.Connection{State: "connected"}, Ready: true, Devices: []api.Device{device}}
		} else if r.URL.Path != api.InspectPath || r.URL.Query().Get("target") != "Couch lamp" {
			t.Errorf("invalid API request")
		}
		if err := json.NewEncoder(w).Encode(result); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{{"device", "list"}, {"device", "list", "--output", "json"}, {"device", "inspect", "Couch lamp"}, {"device", "inspect", "Couch lamp", "--output", "json"}} {
		command, err := NewCommand()
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetArgs(append(args, "--server.url", server.URL))
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "18446744069414584320") || !strings.Contains(output.String(), "Couch lamp") {
			t.Fatalf("missing identity: %s", &output)
		}
		if args[len(args)-1] == "json" && !json.Valid(output.Bytes()) {
			t.Fatalf("invalid JSON: %s", &output)
		}
	}
}

type cancelingWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelingWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	if bytes.Contains(data, []byte("\n")) {
		w.cancel()
	}
	return n, err
}

func TestEventWatchProducesNDJSONAndStopsNormallyOnCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != api.EventsPath || r.URL.Query().Get("device") != "Couch lamp" {
			t.Errorf("invalid watch request")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		if err := json.NewEncoder(w).Encode(api.Event{Type: "inventory", Data: json.RawMessage(`{"ready":true,"devices":[]}`)}); err != nil {
			t.Error(err)
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancelCause := context.WithCancelCause(context.Background())
	cancel := func() { cancelCause(errors.New("interrupt signal received")) }
	defer cancel()
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	output := &cancelingWriter{cancel: cancel}
	command.SetOut(output)
	command.SetArgs([]string{"event", "watch", "--device", "Couch lamp", "--output", "json", "--server.url", server.URL})
	if err := command.ExecuteContext(ctx); err != nil {
		t.Fatalf("interrupt returned error: %v", err)
	}
	var event api.Event
	if json.Unmarshal(output.Bytes(), &event) != nil || event.Type != "inventory" {
		t.Fatalf("invalid NDJSON: %s", output.String())
	}
}
