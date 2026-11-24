package matter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type stateDecoder struct {
	clusterID   uint32
	attributeID uint32
	name        string
	unit        string
	decode      func(json.RawMessage) (any, string, bool)
}

// Each entry describes one interpreted standard attribute, independently of
// device type. Other safe sensor scalars use the marked raw fallback below.
func builtInStateDecoders() []stateDecoder {
	return []stateDecoder{
		{clusterOnOff, attributeOnOff, "power", "", decodePower},
		{clusterLevelControl, attributeCurrentLevel, "level", "", decodeByte},
		{clusterSwitch, attributeSwitchCurrentPosition, "switch_position", "", decodeByte},
		{clusterBooleanState, attributeStateValue, "boolean_state", "", decodeBooleanState},
		{clusterIlluminanceMeasurement, attributeMeasuredIlluminance, "illuminance", "lx", decodeIlluminance},
		{clusterTemperatureMeasurement, attributeMeasuredTemperature, "temperature", "°C", decodeTemperature},
		{clusterRelativeHumidityMeasurement, attributeMeasuredTemperature, "humidity", "%", decodeHumidity},
		{clusterPowerSource, attributeBatteryPercentRemaining, "battery", "%", decodeBattery},
		{clusterOccupancySensing, attributeOccupancy, "occupancy", "", decodeOccupancy},
	}
}

func decodeBooleanState(raw json.RawMessage) (any, string, bool) {
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return nil, "", false
	}
	return value, fmt.Sprint(value), true
}

// Unknown application readings retain the standard attribute name and wire
// scalar. They carry no inferred units or enum meaning. Infrastructure values,
// vendor attributes, strings, arrays, and structures stay inside the adapter.
func decodeRawApplicationState(clusterID, attributeID uint32, raw json.RawMessage) (api.State, bool) {
	if !standardApplicationClusters[clusterID] || attributeID >= 0xFFF8 {
		return api.State{}, false
	}
	raw = bytes.TrimSpace(raw)
	name := attributeName(clusterID, attributeID)
	if name == "" || len(raw) > 64 || !json.Valid(raw) {
		return api.State{}, false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return api.State{}, false
	}
	switch value.(type) {
	case bool, json.Number:
		return api.State{ClusterID: clusterID, AttributeID: attributeID, Name: name, Value: append(json.RawMessage(nil), raw...), Text: string(raw), Raw: true}, true
	default:
		return api.State{}, false
	}
}

func decodeHumidity(raw json.RawMessage) (any, string, bool) {
	var hundredths uint16
	if json.Unmarshal(raw, &hundredths) != nil || hundredths > 10000 {
		return nil, "", false
	}
	value := float64(hundredths) / 100
	return value, fmt.Sprintf("%.1f%%", value), true
}

func decodeBattery(raw json.RawMessage) (any, string, bool) {
	var halves uint8
	if json.Unmarshal(raw, &halves) != nil || halves > 200 {
		return nil, "", false
	}
	value := float64(halves) / 2
	return value, fmt.Sprintf("%.1f%%", value), true
}

func decodeState(clusterID, attributeID uint32, raw json.RawMessage) (api.State, bool) {
	for _, decoder := range builtInStateDecoders() {
		if decoder.clusterID != clusterID || decoder.attributeID != attributeID {
			continue
		}
		state := api.State{ClusterID: clusterID, AttributeID: attributeID, Name: decoder.name, Unit: decoder.unit}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			state.Value, state.Text = json.RawMessage("null"), "unknown"
			return state, true
		}
		value, display, ok := decoder.decode(raw)
		if !ok {
			return api.State{}, false
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return api.State{}, false
		}
		state.Value, state.Text = encoded, display
		return state, true
	}
	return api.State{}, false
}

func interpretedAttribute(clusterID, attributeID uint32) bool {
	for _, decoder := range builtInStateDecoders() {
		if decoder.clusterID == clusterID && decoder.attributeID == attributeID {
			return true
		}
	}
	return false
}

func decodePower(raw json.RawMessage) (any, string, bool) {
	var on bool
	if json.Unmarshal(raw, &on) != nil {
		return nil, "", false
	}
	if on {
		return on, "on", true
	}
	return on, "off", true
}

func decodeByte(raw json.RawMessage) (any, string, bool) {
	var value uint8
	if json.Unmarshal(raw, &value) != nil {
		return nil, "", false
	}
	return value, fmt.Sprint(value), true
}

func decodeTemperature(raw json.RawMessage) (any, string, bool) {
	var centiCelsius int16
	if json.Unmarshal(raw, &centiCelsius) != nil || centiCelsius < -27315 || centiCelsius > 32766 {
		return nil, "", false
	}
	value := float64(centiCelsius) / 100
	return value, fmt.Sprintf("%.2f°C", value), true
}

func decodeIlluminance(raw json.RawMessage) (any, string, bool) {
	var measured uint16
	if json.Unmarshal(raw, &measured) != nil {
		return nil, "", false
	}
	if measured == 0xFFFF {
		return nil, "unknown", true
	}
	if measured == 0 {
		return 0, "below measurable range", true
	}
	// Matter encodes illuminance logarithmically: measured = 10000*log10(lx)+1.
	lux := math.Pow(10, float64(measured-1)/10000)
	return lux, fmt.Sprintf("%.1f lx", lux), true
}

func decodeOccupancy(raw json.RawMessage) (any, string, bool) {
	var bitmap uint8
	occupied := false
	if json.Unmarshal(raw, &bitmap) == nil {
		occupied = bitmap&1 != 0
	} else {
		if json.Unmarshal(raw, &occupied) != nil {
			var flags struct {
				Occupied *bool `json:"occupied"`
			}
			if json.Unmarshal(raw, &flags) != nil || flags.Occupied == nil {
				return nil, "", false
			}
			occupied = *flags.Occupied
		}
	}
	if occupied {
		return occupied, "occupied", true
	}
	return occupied, "unoccupied", true
}
