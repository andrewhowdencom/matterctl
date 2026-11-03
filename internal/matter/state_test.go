package matter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestStateDecodersNormalizeOnlyKnownValues(t *testing.T) {
	tests := []struct {
		name        string
		cluster     uint32
		attribute   uint32
		raw         string
		wantName    string
		wantValue   string
		wantText    string
		wantUnit    string
		wantDecoded bool
	}{
		{"power", clusterOnOff, attributeOnOff, `true`, "power", `true`, "on", "", true},
		{"level", clusterLevelControl, attributeCurrentLevel, `42`, "level", `42`, "42", "", true},
		{"switch", clusterSwitch, attributeSwitchCurrentPosition, `1`, "switch_position", `1`, "1", "", true},
		{"temperature", clusterTemperatureMeasurement, attributeMeasuredTemperature, `-125`, "temperature", `-1.25`, "-1.25°C", "°C", true},
		{"occupancy bitmap", clusterOccupancySensing, attributeOccupancy, `3`, "occupancy", `true`, "occupied", "", true},
		{"occupancy boolean", clusterOccupancySensing, attributeOccupancy, `true`, "occupancy", `true`, "occupied", "", true},
		{"occupancy object", clusterOccupancySensing, attributeOccupancy, `{"occupied":false,"private_key":"secret"}`, "occupancy", `false`, "unoccupied", "", true},
		{"cleared", clusterOccupancySensing, attributeOccupancy, `null`, "occupancy", `null`, "unknown", "", true},
		{"unknown attribute", clusterOccupancySensing, 9, `{"private_key":"secret"}`, "", "", "", "", false},
		{"invalid known value", clusterOccupancySensing, attributeOccupancy, `{"private_key":"secret"}`, "", "", "", "", false},
		{"invalid temperature", clusterTemperatureMeasurement, attributeMeasuredTemperature, `-32768`, "", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, ok := decodeState(tt.cluster, tt.attribute, json.RawMessage(tt.raw))
			if ok != tt.wantDecoded {
				t.Fatalf("decoded=%t, want %t", ok, tt.wantDecoded)
			}
			if !ok {
				return
			}
			if state.Name != tt.wantName || string(state.Value) != tt.wantValue || state.Text != tt.wantText || state.Unit != tt.wantUnit {
				t.Fatalf("unexpected state: %+v", state)
			}
		})
	}
}

func TestSensorStateProjectsAndStreamsWithoutRawAttributes(t *testing.T) {
	n := fixtureNode(t, "1", "Bridge")
	n.EndpointParents[4] = 0
	n.EndpointParents[5] = 0
	n.Attributes["4/29/0"] = json.RawMessage(`[{"deviceType":263,"revision":1}]`)
	n.Attributes["4/29/1"] = json.RawMessage(`[29,1030]`)
	n.Attributes["4/1030/0"] = json.RawMessage(`{"occupied":false,"private_key":"must-not-leak"}`)
	n.Attributes["5/29/0"] = json.RawMessage(`[{"deviceType":770,"revision":1}]`)
	n.Attributes["5/29/1"] = json.RawMessage(`[29,1026]`)
	n.Attributes["5/1026/0"] = json.RawMessage(`2150`)
	n.Attributes["5/1026/9"] = json.RawMessage(`{"private_key":"must-not-leak"}`)
	c := initializedClient(t, n)
	device, err := c.Inspect(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if device.Endpoints[4].DeviceTypes[0].Name != "Occupancy Sensor" || device.Endpoints[5].DeviceTypes[0].Name != "Temperature Sensor" {
		t.Fatalf("sensor types missing: %+v", device.Endpoints)
	}
	if len(device.Endpoints[4].States) != 1 || device.Endpoints[4].States[0].Text != "unoccupied" || len(device.Endpoints[5].States) != 1 || device.Endpoints[5].States[0].Text != "21.50°C" {
		t.Fatalf("sensor values missing: %+v", device.Endpoints)
	}
	encoded, err := json.Marshal(device)
	if err != nil || strings.Contains(string(encoded), "must-not-leak") {
		t.Fatalf("raw attribute leaked through inventory: %s %v", encoded, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	<-events // Initial inventory.
	if err := c.applyEvent(c.current, reply{Event: "attribute_updated", Data: json.RawMessage(`[1,"4/1030/0",{"occupied":true,"private_key":"must-not-leak"}]`)}); err != nil {
		t.Fatal(err)
	}
	event := <-events
	var update struct {
		Path  string     `json:"path"`
		State *api.State `json:"state"`
	}
	if json.Unmarshal(event.Data, &update) != nil || update.Path != "4/1030/0" || update.State == nil || update.State.Text != "occupied" || string(update.State.Value) != "true" || strings.Contains(string(event.Data), "must-not-leak") {
		t.Fatalf("incorrect occupancy event: %s", event.Data)
	}
	if err := c.applyEvent(c.current, reply{Event: "attribute_updated", Data: json.RawMessage(`[1,"5/1026/9",{"private_key":"must-not-leak"}]`)}); err != nil {
		t.Fatal(err)
	}
	event = <-events
	if strings.Contains(string(event.Data), "must-not-leak") || strings.Contains(string(event.Data), `"state"`) {
		t.Fatalf("unknown value leaked: %s", event.Data)
	}
}
