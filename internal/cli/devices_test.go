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
	"time"

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

func TestEventWatchTextShowsCompactStateChanges(t *testing.T) {
	observed := time.Date(2026, time.November, 3, 12, 30, 0, 0, time.UTC)
	device := api.Device{NodeID: "12", Label: "Couch lamp", Available: true, Endpoints: []api.Endpoint{{ID: 1, Label: "Light", DeviceTypes: []api.DeviceType{{ID: 269, Name: "Extended Color Light"}}}}}
	data, err := json.Marshal(struct {
		Path   string     `json:"path"`
		Device api.Device `json:"device"`
		State  api.State  `json:"state"`
	}{"1/6/0", device, api.State{ClusterID: 6, AttributeID: 0, Name: "power", Value: json.RawMessage(`true`), Text: "on"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		event api.Event
		want  string
	}{
		{"inventory", api.Event{Type: "inventory", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"connection":{"state":"connected"},"ready":true,"devices":[{"node_id":"12","label":"Couch lamp","available":true,"endpoints":[{"id":1}]}]}`)}, `inventory node=12 connection=connected ready=true devices=1 available=true endpoints=1 label="Couch lamp"`},
		{"connection", api.Event{Type: "connection", ObservedAt: observed, Data: json.RawMessage(`{"connection":{"state":"disconnected","last_error":"connection failed"},"ready":false}`)}, `connection state=disconnected ready=false reason="connection failed"`},
		{"node update", api.Event{Type: "node_updated", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"node_id":"12","label":"Couch lamp","available":true,"endpoints":[{"id":1}]}`)}, `node_updated node=12 available=true endpoints=1 label="Couch lamp"`},
		{"power", api.Event{Type: "attribute_updated", ObservedAt: observed, NodeID: "12", Data: data}, `attribute_updated node=12 device="Couch lamp" attribute=1/6/0 endpoint=1 label="Light" type="Extended Color Light" power=on`},
		{"product fallback", api.Event{Type: "attribute_updated", ObservedAt: observed, NodeID: "2", Data: json.RawMessage(`{"path":"9/59/1","device":{"node_id":"2","product":"BILRESA scroll wheel","endpoints":[{"id":9,"device_types":[{"id":15,"name":"Generic Switch"}]}]},"state":{"name":"switch_position","text":"1"}}`)}, `attribute_updated node=2 device="BILRESA scroll wheel" attribute=9/59/1 endpoint=9 type="Generic Switch" switch_position=1`},
		{"occupancy", api.Event{Type: "attribute_updated", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"path":"2/1030/0","device":{"node_id":"12","endpoints":[{"id":2,"device_types":[{"id":263,"name":"Occupancy Sensor","revision":1}]}]},"state":{"cluster_id":1030,"attribute_id":0,"name":"occupancy","value":true,"text":"occupied"}}`)}, `attribute_updated node=12 attribute=2/1030/0 endpoint=2 type="Occupancy Sensor" occupancy=occupied`},
		{"temperature", api.Event{Type: "attribute_updated", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"path":"3/1026/0","device":{"node_id":"12","endpoints":[{"id":3}]},"state":{"cluster_id":1026,"attribute_id":0,"name":"temperature","value":21.5,"text":"21.50°C","unit":"°C"}}`)}, `attribute_updated node=12 attribute=3/1026/0 endpoint=3 temperature=21.50°C`},
		{"unknown attribute", api.Event{Type: "attribute_updated", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"path":"1/29/1","device":{"node_id":"12","endpoints":[{"id":1}]}}`)}, `attribute_updated node=12 attribute=1/29/1 endpoint=1`},
		{"endpoint", api.Event{Type: "endpoint_added", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"endpoint_id":2,"device":{"node_id":"12","endpoints":[{"id":2,"label":"Switch"}]}}`)}, `endpoint_added node=12 endpoint=2 label="Switch"`},
		{"node event", api.Event{Type: "node_event", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"endpoint_id":2,"cluster_id":59,"event_id":1,"event_name":"initial_press","event_number":"18446744073709551615","data":{"newPosition":1,"numberOfPressesCounted":2}}`)}, `node_event node=12 endpoint=2 cluster=59 event=1 name=initial_press number=18446744073709551615 new_position=1 presses=2`},
		{"node event product fallback", api.Event{Type: "node_event", ObservedAt: observed, NodeID: "2", Data: json.RawMessage(`{"endpoint_id":9,"cluster_id":59,"event_id":2,"event_name":"long_press","event_number":"199340","product":"BILRESA scroll wheel","data":{"newPosition":1}}`)}, `node_event node=2 device="BILRESA scroll wheel" endpoint=9 cluster=59 event=2 name=long_press number=199340 new_position=1`},
		{"inventory with states", api.Event{Type: "inventory", ObservedAt: observed, NodeID: "12", Data: json.RawMessage(`{"connection":{"state":"connected"},"ready":true,"devices":[{"node_id":"12","available":true,"endpoints":[{"id":1,"device_types":[{"id":263,"name":"Occupancy Sensor","revision":1}],"states":[{"cluster_id":1030,"attribute_id":0,"name":"occupancy","value":true,"text":"occupied"}]}]}]}`)}, `inventory node=12 connection=connected ready=true devices=1 available=true endpoints=1 types="Occupancy Sensor" states="occupancy=occupied"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := printEvent(&output, tt.event); err != nil {
				t.Fatal(err)
			}
			want := observed.Format(time.RFC3339) + " " + tt.want + "\n"
			if output.String() != want {
				t.Fatalf("got %q, want %q", output.String(), want)
			}
		})
	}
	var output bytes.Buffer
	if err := printEvent(&output, api.Event{Type: "heartbeat"}); err != nil || output.Len() != 0 {
		t.Fatalf("heartbeat should be hidden in text mode: %v %q", err, output.String())
	}
	switches := api.Device{Available: true, Endpoints: []api.Endpoint{{ID: 0}}}
	for id := uint16(1); id <= 9; id++ {
		switches.Endpoints = append(switches.Endpoints, api.Endpoint{ID: id, DeviceTypes: []api.DeviceType{{ID: 15, Name: "Generic Switch"}}, States: []api.State{{Name: "switch_position", Text: "0"}}})
	}
	if summary := deviceSummary(switches); summary != ` available=true endpoints=10 types="Generic Switch (9 endpoints)" states="switch_position=0 (9 endpoints)"` {
		t.Fatalf("multi-endpoint summary was not compact: %s", summary)
	}
}

func TestDeviceOutputsShowAdvertisedTypeSeparatelyFromLabel(t *testing.T) {
	device := api.Device{NodeID: "1", Label: "Den Light", ObservedAt: time.Now(), Endpoints: []api.Endpoint{{ID: 1, DeviceTypes: []api.DeviceType{{ID: 269, Name: "Extended Color Light", Revision: 1}}, States: []api.State{{Name: "temperature", Text: "21.50°C"}}}}}
	var output bytes.Buffer
	if err := printDevice(&output, device); err != nil || !strings.Contains(output.String(), "Extended Color Light (269, revision 1)") || !strings.Contains(output.String(), "temperature: 21.50°C") {
		t.Fatalf("inspect omitted device type: %v %s", err, &output)
	}
	output.Reset()
	if err := printInventory(&output, api.Inventory{Connection: api.Connection{State: "connected"}, Ready: true, Devices: []api.Device{device}}); err != nil || !strings.Contains(output.String(), "Extended Color Light") || !strings.Contains(output.String(), "DEVICE TYPE") {
		t.Fatalf("list omitted device type: %v %s", err, &output)
	}
}
