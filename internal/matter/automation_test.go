package matter

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/automation"
)

func TestExecuteAutomationUsesAdvertisedCommandsAndFreshReads(t *testing.T) {
	n := fixtureNode(t, "1", "Den Light")
	n.Attributes["1/29/1"] = json.RawMessage(`[29,6,8,768]`)
	n.Attributes["1/8/65529"] = json.RawMessage(`[4]`)
	n.Attributes["1/768/65529"] = json.RawMessage(`[10,6]`)
	n.Attributes["1/768/7"] = json.RawMessage(`250`)
	n.Attributes["1/768/16395"] = json.RawMessage(`154`)
	n.Attributes["1/768/16396"] = json.RawMessage(`454`)
	var mu sync.Mutex
	var commands []struct {
		cluster uint32
		name    string
		value   int64
	}
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		power, level, mireds, hue, saturation := false, int64(42), int64(250), int64(50), int64(0)
		for wsjson.Read(ctx, conn, &request) == nil {
			switch request.Command {
			case "read_attribute":
				var args struct {
					AttributePath string `json:"attribute_path"`
				}
				if json.Unmarshal(request.Args, &args) != nil {
					t.Error("invalid read request")
					return
				}
				var value any
				switch args.AttributePath {
				case "1/6/0":
					value = power
				case "1/8/0":
					value = level
				case "1/768/7":
					value = mireds
				case "1/768/16396":
					value = 454
				case "1/768/0":
					value = hue
				case "1/768/1":
					value = saturation
				default:
					t.Errorf("unexpected read path %s", args.AttributePath)
					return
				}
				if writeResult(ctx, conn, request.MessageID, map[string]any{args.AttributePath: value}) != nil {
					return
				}
			case "device_command":
				var args struct {
					ClusterID   uint32                     `json:"cluster_id"`
					CommandName string                     `json:"command_name"`
					Payload     map[string]json.RawMessage `json:"payload"`
				}
				if json.Unmarshal(request.Args, &args) != nil {
					t.Error("invalid command request")
					return
				}
				var value int64
				switch args.CommandName {
				case "toggle":
					power = !power
				case "moveToLevelWithOnOff":
					if json.Unmarshal(args.Payload["level"], &value) != nil {
						t.Error("missing level")
						return
					}
					level = value
				case "moveToColorTemperature":
					if json.Unmarshal(args.Payload["colorTemperatureMireds"], &value) != nil {
						t.Error("missing mireds")
						return
					}
					mireds = value
				case "moveToHueAndSaturation":
					if json.Unmarshal(args.Payload["hue"], &value) != nil || json.Unmarshal(args.Payload["saturation"], &saturation) != nil {
						t.Error("missing hue or saturation")
						return
					}
					hue = value
				default:
					t.Errorf("unexpected command %s", args.CommandName)
					return
				}
				mu.Lock()
				commands = append(commands, struct {
					cluster uint32
					name    string
					value   int64
				}{args.ClusterID, args.CommandName, value})
				mu.Unlock()
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
			default:
				t.Errorf("unexpected request %s", request.Command)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	for _, action := range []automation.Action{
		{Kind: "toggle", NodeID: "1", Endpoint: 1},
		{Kind: "brightness", NodeID: "1", Endpoint: 1, Value: int64(50)},
		{Kind: "adjust_brightness", NodeID: "1", Endpoint: 1, Value: int64(10)},
		{Kind: "colour_temperature", NodeID: "1", Endpoint: 1, Value: int64(5000)},
		{Kind: "adjust_colour_temperature", NodeID: "1", Endpoint: 1, Value: int64(150)},
		{Kind: "adjust_colour_temperature", NodeID: "1", Endpoint: 1, Value: int64(10000)},
		{Kind: "warmest_colour_temperature", NodeID: "1", Endpoint: 1},
		{Kind: "adjust_colour", NodeID: "1", Endpoint: 1, Value: int64(12)},
	} {
		if err := c.Execute(t.Context(), action); err != nil {
			t.Fatalf("execute %s: %v", action.Kind, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 8 || commands[0].cluster != 6 || commands[0].name != "toggle" || commands[1].value != 127 || commands[2].value != 152 || commands[3].cluster != 768 || commands[3].value != 200 || commands[4].value != 194 || commands[5].value != 154 || commands[6].value != 454 || commands[7].name != "moveToHueAndSaturation" || commands[7].value != 59 {
		t.Fatalf("unexpected Matter commands: %+v", commands)
	}
}

func TestHueAfterDegreesWraps(t *testing.T) {
	for _, test := range []struct {
		hue     uint8
		degrees int64
		want    uint8
	}{
		{50, 12, 59},
		{250, 12, 4},
		{4, -12, 250},
		{50, 360, 50},
		{50, -720, 50},
	} {
		if got := hueAfterDegrees(test.hue, test.degrees); got != test.want {
			t.Errorf("hueAfterDegrees(%d, %d) = %d, want %d", test.hue, test.degrees, got, test.want)
		}
	}
}

func TestAutomationRejectsUnavailableCapabilitiesBeforeCommand(t *testing.T) {
	c := initializedClient(t, fixtureNode(t, "1", "Lamp"))
	err := c.Execute(t.Context(), automation.Action{Kind: "brightness", NodeID: "1", Endpoint: 1, Value: int64(50)})
	if !errors.Is(err, api.ErrUnsupported) {
		t.Fatalf("missing Level command: %v", err)
	}
}
