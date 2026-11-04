package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/automation"
)

func (c *Client) Light(ctx context.Context, request api.LightRequest) (api.LightResult, error) {
	if err := request.Validate(); err != nil {
		return api.LightResult{}, err
	}
	device, err := c.Inspect(ctx, request.Target)
	if err != nil {
		return api.LightResult{}, err
	}
	if !device.Available {
		return api.LightResult{}, api.ErrOffline
	}
	var capability string
	switch request.Action {
	case "brightness":
		capability = "brightness.set"
	case "temperature":
		capability = "color.temperature"
	case "color":
		capability = "color.hue_saturation"
	}
	allowed := false
	for _, endpoint := range device.Endpoints {
		if endpoint.ID == request.Endpoint {
			for _, item := range endpoint.Capabilities {
				if item == capability || request.Action == "color" && item == "color.xy" {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		return api.LightResult{}, api.ErrUnsupported
	}
	if request.Action == "color" {
		err = c.setColor(ctx, device.NodeID, request.Endpoint, request.Color)
	} else {
		kind := request.Action
		if kind == "temperature" {
			kind = "colour_temperature"
		}
		err = c.Execute(ctx, automation.Action{Kind: kind, NodeID: device.NodeID, Endpoint: request.Endpoint, Value: request.Value})
	}
	if err != nil {
		return api.LightResult{}, err
	}
	return api.LightResult{NodeID: device.NodeID, Endpoint: request.Endpoint, Action: request.Action, ConfirmedAt: time.Now().UTC()}, nil
}

func (c *Client) setColor(ctx context.Context, id api.NodeID, endpoint uint16, color string) error {
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
	device := project(c.nodes[id])
	session := c.current
	c.mu.Unlock()
	if !device.Available {
		return api.ErrOffline
	}
	useHue := supportsAutomationCommand(device, endpoint, clusterColorControl, commandMoveToHueAndSaturation)
	useXY := supportsAutomationCommand(device, endpoint, clusterColorControl, commandMoveToColor)
	if !useHue && !useXY {
		return api.ErrUnsupported
	}
	hue, saturation := rgbToMatterHSV(color)
	x, y := rgbToMatterXY(color)
	commandName := "moveToHueAndSaturation"
	var payload any = struct {
		Hue             uint8 `json:"hue"`
		Saturation      uint8 `json:"saturation"`
		TransitionTime  int   `json:"transitionTime"`
		OptionsMask     int   `json:"optionsMask"`
		OptionsOverride int   `json:"optionsOverride"`
	}{hue, saturation, 0, 0, 0}
	if !useHue {
		commandName = "moveToColor"
		payload = struct {
			ColorX          uint16 `json:"colorX"`
			ColorY          uint16 `json:"colorY"`
			TransitionTime  int    `json:"transitionTime"`
			OptionsMask     int    `json:"optionsMask"`
			OptionsOverride int    `json:"optionsOverride"`
		}{x, y, 0, 0, 0}
	}
	args := struct {
		NodeID   json.Number `json:"node_id"`
		Endpoint uint16      `json:"endpoint_id"`
		Cluster  uint32      `json:"cluster_id"`
		Command  string      `json:"command_name"`
		Payload  any         `json:"payload"`
	}{json.Number(id), endpoint, clusterColorControl, commandName, payload}
	var acknowledgement json.RawMessage
	if err := c.callOnSessionWithTimeout(ctx, "device_command", args, &acknowledgement, automationActionTimeout, session); err != nil {
		var remote remoteError
		if errors.As(err, &remote) && remote.status != nil {
			return errors.Join(api.ErrDeviceRejected, err)
		}
		return fmt.Errorf("color command was not acknowledged; outcome may be unknown: %w", err)
	}
	for {
		first, second := attributeCurrentHue, attributeCurrentSaturation
		if !useHue {
			first, second = attributeCurrentX, attributeCurrentY
		}
		h, err := c.readAutomationAttribute(ctx, session, id, endpoint, clusterColorControl, first)
		if err != nil {
			return errors.Join(api.ErrUnconfirmed, err)
		}
		s, err := c.readAutomationAttribute(ctx, session, id, endpoint, clusterColorControl, second)
		if err != nil {
			return errors.Join(api.ErrUnconfirmed, err)
		}
		if useHue {
			var gotH, gotS uint8
			if json.Unmarshal(h, &gotH) == nil && json.Unmarshal(s, &gotS) == nil && hueDistance(gotH, hue) <= 2 && absAutomation(int64(gotS)-int64(saturation)) <= 2 {
				c.publishConfirmedAttribute(session, id, endpoint, clusterColorControl, attributeCurrentHue, h)
				c.publishConfirmedAttribute(session, id, endpoint, clusterColorControl, attributeCurrentSaturation, s)
				return nil
			}
		} else {
			var gotX, gotY uint16
			if json.Unmarshal(h, &gotX) == nil && json.Unmarshal(s, &gotY) == nil && absAutomation(int64(gotX)-int64(x)) <= 200 && absAutomation(int64(gotY)-int64(y)) <= 200 {
				c.publishConfirmedAttribute(session, id, endpoint, clusterColorControl, attributeCurrentX, h)
				c.publishConfirmedAttribute(session, id, endpoint, clusterColorControl, attributeCurrentY, s)
				return nil
			}
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

func rgbToMatterXY(color string) (uint16, uint16) {
	r, _ := strconv.ParseUint(color[1:3], 16, 8)
	g, _ := strconv.ParseUint(color[3:5], 16, 8)
	b, _ := strconv.ParseUint(color[5:7], 16, 8)
	linear := func(v uint64) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	rf, gf, bf := linear(r), linear(g), linear(b)
	x := 0.4124*rf + 0.3576*gf + 0.1805*bf
	y := 0.2126*rf + 0.7152*gf + 0.0722*bf
	z := 0.0193*rf + 0.1192*gf + 0.9505*bf
	total := x + y + z
	if total == 0 {
		return 20493, 21561
	}
	return uint16(math.Round(x / total * 65535)), uint16(math.Round(y / total * 65535))
}

func hueDistance(a, b uint8) int64 {
	d := absAutomation(int64(a) - int64(b))
	if d > 127 {
		return 255 - d
	}
	return d
}

func rgbToMatterHSV(color string) (uint8, uint8) {
	r, _ := strconv.ParseUint(color[1:3], 16, 8)
	g, _ := strconv.ParseUint(color[3:5], 16, 8)
	b, _ := strconv.ParseUint(color[5:7], 16, 8)
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	maximum := math.Max(rf, math.Max(gf, bf))
	minimum := math.Min(rf, math.Min(gf, bf))
	delta := maximum - minimum
	hue := 0.0
	if delta > 0 {
		switch maximum {
		case rf:
			hue = math.Mod((gf-bf)/delta, 6)
		case gf:
			hue = (bf-rf)/delta + 2
		default:
			hue = (rf-gf)/delta + 4
		}
		hue /= 6
		if hue < 0 {
			hue++
		}
	}
	saturation := 0.0
	if maximum > 0 {
		saturation = delta / maximum
	}
	return uint8(math.Round(hue * 254)), uint8(math.Round(saturation * 254))
}
