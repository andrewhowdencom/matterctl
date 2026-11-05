package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/automation"
)

const automationActionTimeout = 10 * time.Second

// Execute applies one already resolved automation action. Each action is
// independently acknowledged and confirmed; a failed action is never replayed.
func (c *Client) Execute(ctx context.Context, action automation.Action) error {
	endpoint := action.Endpoint
	switch action.Kind {
	case "power":
		mode, ok := action.Value.(string)
		if !ok || (mode != "on" && mode != "off") {
			return errors.New("invalid automation power action")
		}
		_, err := c.Power(ctx, api.PowerRequest{Target: string(action.NodeID), Endpoint: &endpoint, Action: mode})
		return err
	case "toggle":
		_, err := c.Power(ctx, api.PowerRequest{Target: string(action.NodeID), Endpoint: &endpoint, Action: "toggle"})
		return err
	case "brightness", "adjust_brightness", "colour_temperature", "adjust_colour_temperature", "warmest_colour_temperature", "adjust_colour":
		return c.executeLighting(ctx, action)
	default:
		return fmt.Errorf("unsupported automation action %q", action.Kind)
	}
}

func (c *Client) executeLighting(ctx context.Context, action automation.Action) error {
	value, ok := action.Value.(int64)
	if (!ok && action.Kind != "warmest_colour_temperature") || action.NodeID == "" || action.Endpoint == 0 {
		return errors.New("invalid automation lighting action")
	}
	if !c.controlling.CompareAndSwap(false, true) {
		return api.ErrBusy
	}
	defer c.controlling.Store(false)
	ctx, cancel := context.WithTimeout(ctx, automationActionTimeout)
	defer cancel()
	c.mu.Lock()
	if !c.ready || c.current == nil {
		c.mu.Unlock()
		return api.ErrUnavailable
	}
	n, present := c.nodes[action.NodeID]
	session := c.current
	var device api.Device
	var attributes map[string]json.RawMessage
	if present {
		device = project(n)
		attributes = maps.Clone(n.Attributes)
	}
	c.mu.Unlock()
	if !present {
		return api.ErrNotFound
	}
	if !device.Available {
		return api.ErrOffline
	}
	var clusterID, commandID uint32
	var attributeID uint32
	var commandName string
	var expected int64
	var expectedHue uint8
	var expectedSaturation uint8
	var payload any
	switch action.Kind {
	case "brightness", "adjust_brightness":
		clusterID, commandID, attributeID, commandName = clusterLevelControl, commandMoveToLevelWithOnOff, attributeCurrentLevel, "moveToLevelWithOnOff"
		if action.Kind == "brightness" && (value < 1 || value > 100) {
			return errors.New("brightness is out of range")
		}
		if action.Kind == "adjust_brightness" {
			if value < -10000 || value > 10000 {
				return errors.New("brightness adjustment is out of range")
			}
			current, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterLevelControl, attributeCurrentLevel)
			if err != nil {
				return fmt.Errorf("read current brightness: %w", err)
			}
			var level uint8
			if json.Unmarshal(current, &level) != nil {
				return errors.New("current brightness is unknown")
			}
			value += (int64(level)*100 + 127) / 254
		}
		value = max(1, min(100, value))
		expected = max(1, (value*254+50)/100)
		for _, bound := range []struct {
			attribute uint32
			minimum   bool
		}{
			{attributeMinLevel, true}, {attributeMaxLevel, false},
		} {
			var limit uint8
			if raw, found := attributes[fmt.Sprintf("%d/%d/%d", action.Endpoint, clusterID, bound.attribute)]; found && json.Unmarshal(raw, &limit) == nil {
				if bound.minimum {
					expected = max(expected, int64(limit))
				} else {
					expected = min(expected, int64(limit))
				}
			}
		}
		payload = struct {
			Level           int64 `json:"level"`
			TransitionTime  int   `json:"transitionTime"`
			OptionsMask     int   `json:"optionsMask"`
			OptionsOverride int   `json:"optionsOverride"`
		}{expected, 0, 0, 0}
	case "colour_temperature", "adjust_colour_temperature", "warmest_colour_temperature":
		clusterID, commandID, attributeID, commandName = clusterColorControl, commandMoveToColorTemperature, attributeColorTemperature, "moveToColorTemperature"
		switch action.Kind {
		case "warmest_colour_temperature":
			raw, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterColorControl, attributeColorTempPhysicalMax)
			if err != nil {
				return fmt.Errorf("read warmest colour temperature: %w", err)
			}
			var limit uint16
			if json.Unmarshal(raw, &limit) != nil || limit == 0 {
				return errors.New("warmest colour temperature is unavailable")
			}
			expected = int64(limit)
		case "adjust_colour_temperature":
			if value < -10000 || value > 10000 {
				return errors.New("colour temperature adjustment is out of range")
			}
			current, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterColorControl, attributeColorTemperature)
			if err != nil {
				return fmt.Errorf("read current colour temperature: %w", err)
			}
			var mireds uint16
			if json.Unmarshal(current, &mireds) != nil || mireds == 0 {
				return errors.New("current colour temperature is unknown")
			}
			kelvin := max(int64(1000), min(int64(10000), (1_000_000+int64(mireds)/2)/int64(mireds)+value))
			expected = (1_000_000 + kelvin/2) / kelvin
		default:
			if value < 1000 || value > 10000 {
				return errors.New("colour temperature is out of range")
			}
			expected = (1_000_000 + value/2) / value
		}
		for _, bound := range []struct {
			attribute uint32
			outside   bool
		}{
			{attributeColorTempPhysicalMin, false}, {attributeColorTempPhysicalMax, true},
		} {
			var limit uint16
			if raw, found := attributes[fmt.Sprintf("%d/%d/%d", action.Endpoint, clusterID, bound.attribute)]; found && json.Unmarshal(raw, &limit) == nil {
				if action.Kind == "adjust_colour_temperature" {
					if bound.outside {
						expected = min(expected, int64(limit))
					} else {
						expected = max(expected, int64(limit))
					}
				} else if (!bound.outside && expected < int64(limit)) || (bound.outside && expected > int64(limit)) {
					return fmt.Errorf("colour temperature %d K is outside device range", value)
				}
			}
		}
		payload = struct {
			ColorTemperatureMireds int64 `json:"colorTemperatureMireds"`
			TransitionTime         int   `json:"transitionTime"`
			OptionsMask            int   `json:"optionsMask"`
			OptionsOverride        int   `json:"optionsOverride"`
		}{expected, 0, 0, 0}
	case "adjust_colour":
		clusterID, commandID, attributeID, commandName = clusterColorControl, commandMoveToHueAndSaturation, attributeCurrentHue, "moveToHueAndSaturation"
		if value < -3600 || value > 3600 {
			return errors.New("colour adjustment is out of range")
		}
		current, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterColorControl, attributeCurrentHue)
		if err != nil {
			return fmt.Errorf("read current hue: %w", err)
		}
		var hue uint8
		if json.Unmarshal(current, &hue) != nil {
			return errors.New("current hue is unknown")
		}
		expectedHue = hueAfterDegrees(hue, value)
		expected = int64(expectedHue)
		current, err = c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterColorControl, attributeCurrentSaturation)
		if err != nil {
			return fmt.Errorf("read current saturation: %w", err)
		}
		if json.Unmarshal(current, &expectedSaturation) != nil {
			return errors.New("current saturation is unknown")
		}
		if expectedSaturation < 32 {
			expectedSaturation = 254
		} else if expectedSaturation > 254 {
			expectedSaturation = 254
		}
		payload = struct {
			Hue             uint8 `json:"hue"`
			Saturation      uint8 `json:"saturation"`
			TransitionTime  int   `json:"transitionTime"`
			OptionsMask     int   `json:"optionsMask"`
			OptionsOverride int   `json:"optionsOverride"`
		}{expectedHue, expectedSaturation, 0, 0, 0}
	}
	if !supportsAutomationCommand(device, action.Endpoint, clusterID, commandID) {
		return api.ErrUnsupported
	}
	args := struct {
		NodeID      json.Number `json:"node_id"`
		EndpointID  uint16      `json:"endpoint_id"`
		ClusterID   uint32      `json:"cluster_id"`
		CommandName string      `json:"command_name"`
		Payload     any         `json:"payload"`
	}{json.Number(action.NodeID), action.Endpoint, clusterID, commandName, payload}
	var acknowledgement json.RawMessage
	if err := c.callOnSessionWithTimeout(ctx, "device_command", args, &acknowledgement, automationActionTimeout, session); err != nil {
		var remote remoteError
		if errors.As(err, &remote) && remote.status != nil {
			return errors.Join(api.ErrDeviceRejected, err)
		}
		return fmt.Errorf("automation command was not acknowledged; outcome may be unknown: %w", err)
	}
	for {
		raw, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterID, attributeID)
		if err != nil {
			return errors.Join(api.ErrUnconfirmed, err)
		}
		var got int64
		confirmed := json.Unmarshal(raw, &got) == nil && absAutomation(got-expected) <= 1
		if action.Kind == "adjust_colour" {
			confirmed = json.Unmarshal(raw, &got) == nil && got >= 0 && got <= 254 && hueDistance(uint8(got), uint8(expected)) <= 2
			if confirmed {
				saturation, err := c.readAutomationAttribute(ctx, session, action.NodeID, action.Endpoint, clusterColorControl, attributeCurrentSaturation)
				if err != nil {
					return errors.Join(api.ErrUnconfirmed, err)
				}
				var gotSaturation uint8
				confirmed = json.Unmarshal(saturation, &gotSaturation) == nil && absAutomation(int64(gotSaturation)-int64(expectedSaturation)) <= 2
				if confirmed {
					c.publishConfirmedAttribute(session, action.NodeID, action.Endpoint, clusterColorControl, attributeCurrentSaturation, saturation)
				}
			}
		}
		if confirmed {
			c.publishConfirmedAttribute(session, action.NodeID, action.Endpoint, clusterID, attributeID, raw)
			return nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(api.ErrUnconfirmed, ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *Client) readAutomationAttribute(ctx context.Context, session *session, id api.NodeID, endpoint uint16, cluster, attribute uint32) (json.RawMessage, error) {
	path := fmt.Sprintf("%d/%d/%d", endpoint, cluster, attribute)
	args := struct {
		NodeID        json.Number `json:"node_id"`
		AttributePath string      `json:"attribute_path"`
	}{json.Number(id), path}
	var values map[string]json.RawMessage
	if err := c.callOnSessionWithTimeout(ctx, "read_attribute", args, &values, automationActionTimeout, session); err != nil {
		return nil, err
	}
	raw, found := values[path]
	if !found || string(raw) == "null" {
		return nil, fmt.Errorf("attribute %s is unavailable", path)
	}
	return raw, nil
}

func supportsAutomationCommand(device api.Device, endpoint uint16, clusterID, commandID uint32) bool {
	for _, e := range device.Endpoints {
		if e.ID != endpoint {
			continue
		}
		for _, cluster := range e.ServerClusters {
			if cluster.ID == clusterID {
				return slices.ContainsFunc(cluster.AcceptedCommands, func(command api.Command) bool { return command.ID == commandID })
			}
		}
	}
	return false
}

func absAutomation(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func hueAfterDegrees(hue uint8, degrees int64) uint8 {
	turn := degrees % 360
	steps := (absAutomation(turn)*255 + 180) / 360
	if turn < 0 {
		steps = -steps
	}
	// The modulo result is always in the Matter hue range 0..254.
	return uint8((int64(hue)%255 + steps + 255) % 255) // #nosec G115
}
