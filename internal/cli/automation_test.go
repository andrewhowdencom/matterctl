package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestAutomationReplayPreviewsWithoutService(t *testing.T) {
	directory := t.TempDir()
	rules := filepath.Join(directory, "rules.star")
	if err := os.WriteFile(rules, []byte(`
den = device("Den Light", endpoint=1)
button = device("2")
on(short_press(button, endpoint=9), lambda event: [toggle(den)])
`), 0o600); err != nil {
		t.Fatal(err)
	}
	recording := filepath.Join(directory, "events.ndjson")
	var events bytes.Buffer
	for _, sample := range []struct {
		kind string
		id   api.NodeID
		data any
	}{
		{"inventory", "", api.Inventory{Ready: true, Devices: []api.Device{
			{NodeID: "1", Label: "Den Light", Endpoints: []api.Endpoint{{ID: 1}}},
			{NodeID: "2", Endpoints: []api.Endpoint{{ID: 9}}},
		}}},
		{"node_event", "2", map[string]any{"endpoint_id": 9, "cluster_id": 59, "event_id": 3}},
	} {
		data, err := json.Marshal(sample.data)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(&events).Encode(api.Event{Type: sample.kind, NodeID: sample.id, Data: data, ObservedAt: time.Date(2026, 11, 3, 16, 40, 20, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(recording, events.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"automation", "replay", rules, recording})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "button.short_press node=2 endpoint=9 -> toggle(node=1 endpoint=1)") {
		t.Fatalf("unexpected preview: %s", output.String())
	}
}
