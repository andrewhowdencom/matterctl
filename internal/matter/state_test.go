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
		{"illuminance", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `10001`, "illuminance", `10`, "10.0 lx", "lx", true},
		{"illuminance below range", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `0`, "illuminance", `0`, "below measurable range", "lx", true},
		{"illuminance invalid", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `65535`, "illuminance", `null`, "unknown", "lx", true},
		{"temperature", clusterTemperatureMeasurement, attributeMeasuredTemperature, `-125`, "temperature", `-1.25`, "-1.25°C", "°C", true},
		{"occupancy bitmap", clusterOccupancySensing, attributeOccupancy, `3`, "occupancy", `true`, "occupied", "", true},
		{"occupancy boolean", clusterOccupancySensing, attributeOccupancy, `true`, "occupancy", `true`, "occupied", "", true},
		{"occupancy object", clusterOccupancySensing, attributeOccupancy, `{"occupied":false,"private_key":"secret"}`, "occupancy", `false`, "unoccupied", "", true},
		{"cleared", clusterOccupancySensing, attributeOccupancy, `null`, "occupancy", `null`, "unknown", "", true},
		{"unknown attribute", clusterOccupancySensing, 9, `{"private_key":"secret"}`, "", "", "", "", false},
		{"invalid known value", clusterOccupancySensing, attributeOccupancy, `{"private_key":"secret"}`, "", "", "", "", false},
		{"invalid temperature", clusterTemperatureMeasurement, attributeMeasuredTemperature, `-32768`, "", "", "", "", false},
		{"invalid illuminance", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `-1`, "", "", "", "", false},
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

func TestAttributeNamesAreScopedByCluster(t *testing.T) {
	if got := attributeName(clusterSwitch, attributeSwitchCurrentPosition); got != "Switch.CurrentPosition" {
		t.Fatalf("Switch attribute name = %q", got)
	}
	if got := attributeName(clusterBasicInformation, attributeSwitchCurrentPosition); got != "BasicInformation.VendorName" {
		t.Fatalf("attribute ID was not scoped to its cluster: %q", got)
	}
	if got := attributeName(clusterIlluminanceMeasurement, attributeMeasuredIlluminance); got != "IlluminanceMeasurement.MeasuredValue" {
		t.Fatalf("illuminance name = %q", got)
	}
	if got := attributeName(clusterIlluminanceMeasurement, attributeFeatureMap); got != "IlluminanceMeasurement.FeatureMap" {
		t.Fatalf("global attribute name = %q", got)
	}
	if got := attributeName(clusterIlluminanceMeasurement, 9999); got != "" {
		t.Fatalf("invented name for unknown attribute: %q", got)
	}
}

func TestStandardCatalogNamesTypesClustersCommandsAndEvents(t *testing.T) {
	if got := deviceTypeName(262); got != "Light Sensor" {
		t.Fatalf("light sensor type = %q", got)
	}
	if got := clusterName(1024); got != "IlluminanceMeasurement" {
		t.Fatalf("illuminance cluster = %q", got)
	}
	if got := commandName(clusterOnOff, 1); got != "On" {
		t.Fatalf("OnOff command = %q", got)
	}
	if got := commandName(84, 0); got != "ChangeToMode" {
		t.Fatalf("inherited mode command = %q", got)
	}
	if got := eventName(clusterSwitch, 1); got != "initial_press" {
		t.Fatalf("Switch event = %q", got)
	}
	if got := eventName(clusterOccupancySensing, 0); got != "occupancy_changed" {
		t.Fatalf("occupancy event = %q", got)
	}
	if got := eventName(56, 0); got != "dst_table_empty" {
		t.Fatalf("event acronym = %q", got)
	}
	if got := deviceTypeName(0xFFF10000); got != "" {
		t.Fatalf("invented vendor device type = %q", got)
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
		Path          string     `json:"path"`
		AttributeName string     `json:"attribute_name"`
		State         *api.State `json:"state"`
	}
	if json.Unmarshal(event.Data, &update) != nil || update.Path != "4/1030/0" || update.AttributeName != "OccupancySensing.Occupancy" || update.State == nil || update.State.Text != "occupied" || string(update.State.Value) != "true" || strings.Contains(string(event.Data), "must-not-leak") {
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

func TestLightSensorStateProjectsAndStreams(t *testing.T) {
	n := fixtureNode(t, "3", "Presence Multi-Sensor FP300")
	n.Attributes["2/29/0"] = json.RawMessage(`[{"deviceType":262,"revision":1}]`)
	n.Attributes["2/29/1"] = json.RawMessage(`[29,1024]`)
	n.Attributes["2/1024/0"] = json.RawMessage(`1`)
	c := initializedClient(t, n)
	device, err := c.Inspect(context.Background(), "3")
	if err != nil {
		t.Fatal(err)
	}
	light := device.Endpoints[2]
	if light.DeviceTypes[0].Name != "Light Sensor" || len(light.States) != 1 || light.States[0].Text != "1.0 lx" {
		t.Fatalf("light sensor projection = %+v", light)
	}
	n.Attributes["2/1024/0"] = json.RawMessage(`65535`)
	if invalid := project(n).Endpoints[2].States; len(invalid) != 0 {
		t.Fatalf("invalid illuminance projected: %+v", invalid)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "3")
	if err != nil {
		t.Fatal(err)
	}
	<-events // Initial inventory.
	if err := c.applyEvent(c.current, reply{Event: "attribute_updated", Data: json.RawMessage(`[3,"2/1024/0",10001]`)}); err != nil {
		t.Fatal(err)
	}
	event := <-events
	var update struct {
		AttributeName string     `json:"attribute_name"`
		State         *api.State `json:"state"`
	}
	if err := json.Unmarshal(event.Data, &update); err != nil || update.AttributeName != "IlluminanceMeasurement.MeasuredValue" || update.State == nil || update.State.Text != "10.0 lx" {
		t.Fatalf("light sensor event = %s: %v", event.Data, err)
	}
}
