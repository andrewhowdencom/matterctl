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
	case "brightness", "adjust_brightness", "colour_temperature":
		return c.executeLighting(ctx, action)
	default:
		return fmt.Errorf("unsupported automation action %q", action.Kind)
	}
}

func (c *Client) executeLighting(ctx context.Context, action automation.Action) error {
	value, ok := action.Value.(int64)
	if !ok || action.NodeID == "" || action.Endpoint == 0 {
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
	case "colour_temperature":
		if value < 1000 || value > 10000 {
			return errors.New("colour temperature is out of range")
		}
		clusterID, commandID, attributeID, commandName = clusterColorControl, commandMoveToColorTemperature, attributeColorTemperature, "moveToColorTemperature"
		expected = (1_000_000 + value/2) / value
		for _, bound := range []struct {
			attribute uint32
			outside   bool
		}{
			{attributeColorTempPhysicalMin, false}, {attributeColorTempPhysicalMax, true},
		} {
			var limit uint16
			if raw, found := attributes[fmt.Sprintf("%d/%d/%d", action.Endpoint, clusterID, bound.attribute)]; found && json.Unmarshal(raw, &limit) == nil {
				if (!bound.outside && expected < int64(limit)) || (bound.outside && expected > int64(limit)) {
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
		if json.Unmarshal(raw, &got) == nil && absAutomation(got-expected) <= 1 {
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
