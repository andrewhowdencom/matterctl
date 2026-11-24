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
		{"boolean state clear", clusterBooleanState, attributeStateValue, `false`, "boolean_state", `false`, "false", "", true},
		{"boolean state active", clusterBooleanState, attributeStateValue, `true`, "boolean_state", `true`, "true", "", true},
		{"illuminance", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `10001`, "illuminance", `10`, "10.0 lx", "lx", true},
		{"illuminance below range", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `0`, "illuminance", `0`, "below measurable range", "lx", true},
		{"illuminance invalid", clusterIlluminanceMeasurement, attributeMeasuredIlluminance, `65535`, "illuminance", `null`, "unknown", "lx", true},
		{"temperature", clusterTemperatureMeasurement, attributeMeasuredTemperature, `-125`, "temperature", `-1.25`, "-1.25°C", "°C", true},
		{"humidity", clusterRelativeHumidityMeasurement, attributeMeasuredTemperature, `4567`, "humidity", `45.67`, "45.7%", "%", true},
		{"battery", clusterPowerSource, attributeBatteryPercentRemaining, `155`, "battery", `77.5`, "77.5%", "%", true},
		{"invalid humidity", clusterRelativeHumidityMeasurement, attributeMeasuredTemperature, `10001`, "", "", "", "", false},
		{"invalid battery", clusterPowerSource, attributeBatteryPercentRemaining, `201`, "", "", "", "", false},
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

func TestRawApplicationStateExposesOnlyNamedPrimitiveValues(t *testing.T) {
	tests := []struct {
		name      string
		cluster   uint32
		attribute uint32
		raw       string
		allowed   bool
	}{
		{"alarm bitmap", 128, 3, `3`, true},
		{"measurement range", 1026, 1, `-1000`, true},
		{"unknown standard attribute", 128, 999, `3`, false},
		{"global attribute", 128, attributeFeatureMap, `3`, false},
		{"infrastructure cluster", clusterBasicInformation, 2, `12`, false},
		{"vendor cluster", 0xFFF10000, 0, `12`, false},
		{"string", 80, 0, `"private-value"`, false},
		{"structure", 128, 3, `{"private_key":"must-not-leak"}`, false},
		{"null", 128, 3, `null`, false},
		{"invalid", 128, 3, `3 4`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, ok := decodeRawApplicationState(tt.cluster, tt.attribute, json.RawMessage(tt.raw))
			if ok != tt.allowed {
				t.Fatalf("decoded=%t, want %t", ok, tt.allowed)
			}
			if ok && (!state.Raw || state.Name != attributeName(tt.cluster, tt.attribute) || string(state.Value) != tt.raw || state.Text != tt.raw) {
				t.Fatalf("incorrect raw value: %+v", state)
			}
		})
	}
}

func TestBooleanSensorAndRawAlarmUpdatesProjectTogether(t *testing.T) {
	n := fixtureNode(t, "27", "Leak Sensor")
	n.EndpointParents[4] = 0
	n.Attributes["4/29/0"] = json.RawMessage(`[{"deviceType":67,"revision":1}]`)
	n.Attributes["4/29/1"] = json.RawMessage(`[29,69,128]`)
	n.Attributes["4/69/0"] = json.RawMessage(`false`)
	n.Attributes["4/128/3"] = json.RawMessage(`0`)
	n.Attributes["4/128/65533"] = json.RawMessage(`1`)
	c := initializedClient(t, n)
	device, err := c.Inspect(context.Background(), "27")
	if err != nil {
		t.Fatal(err)
	}
	states := device.Endpoints[4].States
	if len(states) != 2 || states[0].Name != "boolean_state" || string(states[0].Value) != "false" || states[0].Raw || states[1].Name != "BooleanStateConfiguration.AlarmsActive" || states[1].Text != "0" || !states[1].Raw {
		t.Fatalf("sensor projection = %+v", states)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := c.Watch(ctx, "27")
	if err != nil {
		t.Fatal(err)
	}
	<-events
	if err := c.applyEvent(c.current, reply{Event: "attribute_updated", Data: json.RawMessage(`[27,"4/128/3",3]`)}); err != nil {
		t.Fatal(err)
	}
	event := <-events
	var update struct {
		State *api.State `json:"state"`
	}
	if json.Unmarshal(event.Data, &update) != nil || update.State == nil || !update.State.Raw || update.State.Text != "3" {
		t.Fatalf("raw sensor update = %s", event.Data)
	}
}

func TestRawFallbackCoversFutureSensorWithoutAddingLightConfiguration(t *testing.T) {
	n := fixtureNode(t, "31", "Pressure Sensor")
	n.Attributes["1/6/16385"] = json.RawMessage(`20`)
	if states := project(n).Endpoints[1].States; len(states) != 2 {
		t.Fatalf("ordinary light configuration was exposed: %+v", states)
	}
	n.Attributes["1/29/0"] = json.RawMessage(`[{"deviceType":773,"revision":1}]`)
	n.Attributes["1/29/1"] = json.RawMessage(`[29,1027]`)
	n.Attributes["1/1027/0"] = json.RawMessage(`1003`)
	states := project(n).Endpoints[1].States
	if len(states) != 1 || !states[0].Raw || states[0].Name != "PressureMeasurement.MeasuredValue" || states[0].Text != "1003" {
		t.Fatalf("standard pressure sensor value was omitted: %+v", states)
	}
	n.Attributes["1/1027/0"] = json.RawMessage(`{"invalid":true}`)
	if states := project(n).Endpoints[1].States; len(states) != 0 {
		t.Fatalf("structured measurement was exposed: %+v", states)
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
	n.Attributes["5/1026/0"] = json.RawMessage(`-32768`)
	if states := project(n).Endpoints[5].States; len(states) != 0 {
		t.Fatalf("invalid interpreted measurement became a raw reading: %+v", states)
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
