package automation

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestTwoButtonBilresaRoomBindings(t *testing.T) {
	source, err := os.ReadFile("../../examples/bilresa-two-button-lights.star")
	if err != nil {
		t.Fatal(err)
	}
	source = append(source, []byte("\nbind_room(\"11\", 3, 4, \"21\", 1)\n")...)
	engine, err := Load(t.Context(), "rooms.star", source)
	if err != nil {
		t.Fatal(err)
	}
	if engine.RuleCount() != 8 {
		t.Fatalf("registered %d rules, want 8", engine.RuleCount())
	}
	inventory := api.Inventory{Ready: true, Devices: []api.Device{
		{NodeID: "10", Label: "ROOM_1_BILRESA", Endpoints: []api.Endpoint{{ID: 1}, {ID: 2}}},
		{NodeID: "20", Label: "ROOM_1_LIGHT", Endpoints: []api.Endpoint{{ID: 1}}},
		{NodeID: "11", Endpoints: []api.Endpoint{{ID: 3}, {ID: 4}}},
		{NodeID: "21", Endpoints: []api.Endpoint{{ID: 1}}},
	}}
	if _, err := engine.Feed(t.Context(), sampleEvent("inventory", "", inventory)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		node     api.NodeID
		endpoint uint16
		eventID  uint32
		target   api.NodeID
		kinds    []string
		values   []any
	}{
		{"on click", "10", 1, 3, "20", []string{"power"}, []any{"on"}},
		{"off click", "10", 2, 3, "20", []string{"power"}, []any{"off"}},
		{"on hold", "10", 1, 2, "20", []string{"power", "brightness", "colour_temperature"}, []any{"on", int64(100), int64(5000)}},
		{"off hold", "10", 2, 2, "20", []string{"power", "brightness", "warmest_colour_temperature"}, []any{"on", int64(1), nil}},
		{"second room", "11", 4, 3, "21", []string{"power"}, []any{"off"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := sampleEvent("node_event", test.node, struct {
				EndpointID uint16 `json:"endpoint_id"`
				ClusterID  uint32 `json:"cluster_id"`
				EventID    uint32 `json:"event_id"`
			}{test.endpoint, 59, test.eventID})
			firings, err := engine.Feed(t.Context(), event)
			if err != nil {
				t.Fatal(err)
			}
			if len(firings) != 1 || len(firings[0].Actions) != len(test.kinds) {
				t.Fatalf("firings = %+v", firings)
			}
			for i, action := range firings[0].Actions {
				if action.Kind != test.kinds[i] || action.Value != test.values[i] || action.NodeID != test.target {
					t.Fatalf("action %d = %+v", i, action)
				}
			}
		})
	}
}

const exampleRules = `
den = device("Den Light", endpoint=1)
bilresa = device("BILRESA", endpoint=0)
presence = device("Presence Sensor")

def bind_toggle(button, endpoint, light):
    on(short_press(button, endpoint=endpoint), lambda event: [toggle(light)])

def daytime(event):
    return [power(den, "on"), brightness(den, 90), colour_temperature(den, 5000)]

on(occupied(presence, endpoint=1), daytime)
on(wheel(bilresa, up=7, down=8), lambda event: [adjust_brightness(den, event.steps * 5)])
bind_toggle(bilresa, 9, den)
`

func TestRulesPreviewPresenceWheelAndButton(t *testing.T) {
	engine, err := Load(t.Context(), "rules.star", []byte(exampleRules))
	if err != nil {
		t.Fatal(err)
	}
	if engine.RuleCount() != 3 {
		t.Fatalf("rule count = %d, want 3", engine.RuleCount())
	}
	apply := func(event api.Event) []Firing {
		t.Helper()
		fired, err := engine.Feed(t.Context(), event)
		if err != nil {
			t.Fatal(err)
		}
		return fired
	}
	if got := apply(sampleInventory(false)); len(got) != 0 {
		t.Fatalf("initial snapshot fired %v", got)
	}
	if got := apply(occupancyUpdate(true)); len(got) != 1 || got[0].Rule != 1 || len(got[0].Actions) != 3 || got[0].Actions[0].Kind != "power" || got[0].Actions[1].Value != int64(90) || got[0].Actions[2].Value != int64(5000) {
		t.Fatalf("presence plan = %+v", got)
	}
	if got := apply(occupancyUpdate(true)); len(got) != 0 {
		t.Fatalf("unchanged occupancy fired %v", got)
	}
	if got := apply(switchEvent(7, 1, nil)); len(got) != 0 {
		t.Fatalf("initial press fired %v", got)
	}
	if got := apply(switchEvent(7, 6, map[string]uint8{"totalNumberOfPressesCounted": 3})); len(got) != 1 || got[0].Signal.Steps != 3 || got[0].Actions[0].Value != int64(15) {
		t.Fatalf("up wheel plan = %+v", got)
	}
	if got := apply(switchEvent(8, 6, map[string]uint8{"totalNumberOfPressesCounted": 2})); len(got) != 1 || got[0].Signal.Steps != -2 || got[0].Actions[0].Value != int64(-10) {
		t.Fatalf("down wheel plan = %+v", got)
	}
	if got := apply(switchEvent(9, 3, nil)); len(got) != 1 || got[0].Rule != 3 || got[0].Actions[0].Kind != "toggle" {
		t.Fatalf("button plan = %+v", got)
	}
	apply(occupancyUpdate(false))
	if got := apply(occupancyUpdate(true)); len(got) != 1 {
		t.Fatalf("second occupancy transition = %+v", got)
	}
	if got := apply(sampleInventory(true)); len(got) != 0 {
		t.Fatalf("reconnect snapshot fired %v", got)
	}
	if got := apply(occupancyUpdate(true)); len(got) != 0 {
		t.Fatalf("report equal to reconnect baseline fired %v", got)
	}
}

func TestSwitchPrimitivesSeparateImmediateReleaseFromCompletedGesture(t *testing.T) {
	engine, err := Load(t.Context(), "switch.star", []byte(`
light = device("Den Light", endpoint=1)
button = device("BILRESA")
on(short_release(button, endpoint=9), lambda event: [toggle(light)])
on(single_press(button, endpoint=9), lambda event: [power(light, "on")])
on(double_press(button, endpoint=9), lambda event: [power(light, "off")])
on(long_press(button, endpoint=9), lambda event: [brightness(light, 50)])
on(wheel_step(button, up=7, down=8),
   lambda event: [adjust_brightness(light, event.steps * 5)])
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Feed(t.Context(), sampleInventory(false)); err != nil {
		t.Fatal(err)
	}
	check := func(endpoint uint16, eventID uint32, fields map[string]uint8, wantRule int, wantValue any) {
		t.Helper()
		firings, err := engine.Feed(t.Context(), switchEvent(endpoint, eventID, fields))
		if err != nil {
			t.Fatal(err)
		}
		if wantRule == 0 {
			if len(firings) != 0 {
				t.Fatalf("event %d on endpoint %d unexpectedly fired %+v", eventID, endpoint, firings)
			}
			return
		}
		if len(firings) != 1 || firings[0].Rule != wantRule || len(firings[0].Actions) != 1 || firings[0].Actions[0].Value != wantValue {
			t.Fatalf("event %d on endpoint %d fired %+v, want rule %d value %v", eventID, endpoint, firings, wantRule, wantValue)
		}
	}
	check(9, 1, nil, 0, nil) // InitialPress does not decide a gesture.
	check(9, 3, nil, 1, nil) // ShortRelease toggles before the final count.
	check(9, 6, map[string]uint8{"totalNumberOfPressesCounted": 1}, 2, "on")
	check(9, 6, map[string]uint8{"totalNumberOfPressesCounted": 2}, 3, "off")
	check(9, 2, nil, 4, int64(50))
	check(9, 4, nil, 0, nil) // LongRelease does not also toggle.
	check(7, 3, nil, 5, int64(5))
	check(8, 3, nil, 5, int64(-5))
	check(7, 6, map[string]uint8{"totalNumberOfPressesCounted": 4}, 0, nil)
}

func TestRuleValidationAndTargetResolution(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"no rules", `device("Den Light")`, "did not register"},
		{"invalid trigger", `on(1, lambda e: [])`, "requires a trigger"},
		{"invalid endpoint", `on(short_press(device("2"), endpoint=0), lambda e: [])`, "endpoint"},
		{"invalid action", `den = device("1", endpoint=1)
button = device("2")
on(short_press(button, endpoint=9), lambda e: [brightness(den, 120)])`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, err := Load(t.Context(), "bad.star", []byte(test.source))
			if test.name == "invalid action" {
				if err != nil {
					t.Fatal(err)
				}
				if err := engine.applyInventory(sampleInventoryData(false)); err != nil {
					t.Fatal(err)
				}
				_, err = engine.Feed(t.Context(), switchEvent(9, 3, nil))
				test.want = "brightness must"
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	engine, err := Load(t.Context(), "rules.star", []byte(exampleRules))
	if err != nil {
		t.Fatal(err)
	}
	inventory := sampleInventoryData(false)
	inventory.Devices = append(inventory.Devices, api.Device{NodeID: "4", Label: "Den Light"})
	if err := engine.applyInventory(inventory); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous label error = %v", err)
	}
}

func sampleInventory(occupied bool) api.Event {
	return sampleEvent("inventory", "", sampleInventoryData(occupied))
}

func sampleInventoryData(occupied bool) api.Inventory {
	state, _ := json.Marshal(occupied)
	return api.Inventory{Ready: true, Devices: []api.Device{
		{NodeID: "1", Label: "Den Light", Endpoints: []api.Endpoint{{ID: 1}}},
		{NodeID: "2", Label: "BILRESA", Endpoints: []api.Endpoint{{ID: 7}, {ID: 8}, {ID: 9}}},
		{NodeID: "3", Label: "Presence Sensor", Endpoints: []api.Endpoint{{ID: 1, States: []api.State{{Name: "occupancy", Value: state}}}}},
	}}
}

func occupancyUpdate(occupied bool) api.Event {
	value, _ := json.Marshal(occupied)
	return sampleEvent("attribute_updated", "3", struct {
		Path  string    `json:"path"`
		State api.State `json:"state"`
	}{"1/1030/0", api.State{Name: "occupancy", Value: value}})
}

func switchEvent(endpoint uint16, eventID uint32, fields map[string]uint8) api.Event {
	return sampleEvent("node_event", "2", struct {
		EndpointID uint16           `json:"endpoint_id"`
		ClusterID  uint32           `json:"cluster_id"`
		EventID    uint32           `json:"event_id"`
		Data       map[string]uint8 `json:"data"`
	}{endpoint, 59, eventID, fields})
}

func sampleEvent(kind string, id api.NodeID, value any) api.Event {
	data, _ := json.Marshal(value)
	return api.Event{Type: kind, NodeID: id, Data: data, ObservedAt: time.Date(2026, 11, 3, 16, 40, 20, 0, time.UTC)}
}
