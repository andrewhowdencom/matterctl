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

// Each entry describes one standard attribute, independently of device type.
// Only decoded scalar values cross the application API boundary.
func builtInStateDecoders() []stateDecoder {
	return []stateDecoder{
		{clusterOnOff, attributeOnOff, "power", "", decodePower},
		{clusterLevelControl, attributeCurrentLevel, "level", "", decodeByte},
		{clusterSwitch, attributeSwitchCurrentPosition, "switch_position", "", decodeByte},
		{clusterIlluminanceMeasurement, attributeMeasuredIlluminance, "illuminance", "lx", decodeIlluminance},
		{clusterTemperatureMeasurement, attributeMeasuredTemperature, "temperature", "°C", decodeTemperature},
		{clusterOccupancySensing, attributeOccupancy, "occupancy", "", decodeOccupancy},
	}
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
