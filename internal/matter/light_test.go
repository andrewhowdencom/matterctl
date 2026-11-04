package matter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestLightingProjectionUsesAcceptedServerCommands(t *testing.T) {
	n := fixtureNode(t, "1", "Den Light")
	n.Attributes["1/29/1"] = json.RawMessage(`[29,6,8,768]`)
	n.Attributes["1/8/65529"] = json.RawMessage(`[4]`)
	n.Attributes["1/768/65529"] = json.RawMessage(`[6,10]`)
	n.Attributes["1/768/7"] = json.RawMessage(`250`)
	n.Attributes["1/768/16395"] = json.RawMessage(`154`)
	n.Attributes["1/768/16396"] = json.RawMessage(`454`)
	n.Attributes["1/768/0"] = json.RawMessage(`127`)
	n.Attributes["1/768/1"] = json.RawMessage(`254`)
	n.Attributes["1/64/0"] = json.RawMessage(`[{"0":"room","1":"Den"}]`)
	n.Attributes["1/29/1"] = json.RawMessage(`[29,6,8,64,768]`)
	endpoint := project(n).Endpoints[1]
	if len(endpoint.Capabilities) != 6 || endpoint.Capabilities[3] != "brightness.set" || endpoint.Capabilities[4] != "color.hue_saturation" || endpoint.Capabilities[5] != "color.temperature" {
		t.Fatalf("capabilities = %v", endpoint.Capabilities)
	}
	if endpoint.ColorTemperature == nil || *endpoint.ColorTemperature != 4000 || endpoint.Hue == nil || *endpoint.Hue != 127 || len(endpoint.Labels) != 1 || endpoint.Labels[0].Value != "Den" {
		t.Fatalf("color state/labels = %+v", endpoint)
	}
	if endpoint.ColorTemperatureMin == nil || *endpoint.ColorTemperatureMin != 2202 || endpoint.ColorTemperatureMax == nil || *endpoint.ColorTemperatureMax != 6493 {
		t.Fatalf("temperature range = %+v", endpoint)
	}
	n.Attributes["1/768/16396"] = json.RawMessage(`65535`)
	if invalid := project(n).Endpoints[1].ColorTemperatureMin; invalid != nil {
		t.Fatalf("sentinel projected as temperature: %d", *invalid)
	}
}

func TestDescriptorSemanticTagsAreProjectedWithoutLocalNames(t *testing.T) {
	n := fixtureNode(t, "2", "Den Switch")
	n.Attributes["2/29/4"] = json.RawMessage(`[{"1":67,"2":3},{"namespaceID":8,"tag":0,"label":"left"}]`)
	tags := project(n).Endpoints[2].Tags
	if len(tags) != 2 || tags[0].Namespace != 67 || tags[0].Tag != 3 || tags[1].Namespace != 8 || tags[1].Label != "left" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestColorCommandConfirmsReportedHueAndSaturation(t *testing.T) {
	n := fixtureNode(t, "1", "Den Light")
	n.Attributes["1/29/1"] = json.RawMessage(`[29,6,8,768]`)
	n.Attributes["1/768/65529"] = json.RawMessage(`[6]`)
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for wsjson.Read(ctx, conn, &request) == nil {
			switch request.Command {
			case "device_command":
				var args struct {
					ClusterID   uint32 `json:"cluster_id"`
					CommandName string `json:"command_name"`
					Payload     struct {
						Hue        uint8 `json:"hue"`
						Saturation uint8 `json:"saturation"`
					} `json:"payload"`
				}
				if json.Unmarshal(request.Args, &args) != nil || args.ClusterID != 768 || args.CommandName != "moveToHueAndSaturation" || args.Payload.Hue != 0 || args.Payload.Saturation != 254 {
					t.Errorf("command = %+v", args)
					return
				}
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
			case "read_attribute":
				var args struct {
					AttributePath string `json:"attribute_path"`
				}
				if json.Unmarshal(request.Args, &args) != nil {
					return
				}
				value := uint8(0)
				if args.AttributePath == "1/768/1" {
					value = 254
				} else if args.AttributePath != "1/768/0" {
					t.Errorf("read path = %s", args.AttributePath)
					return
				}
				if writeResult(ctx, conn, request.MessageID, map[string]any{args.AttributePath: value}) != nil {
					return
				}
			default:
				t.Errorf("request = %s", request.Command)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	result, err := c.Light(t.Context(), api.LightRequest{Target: "1", Endpoint: 1, Action: "color", Color: "#ff0000"})
	if err != nil || result.NodeID != "1" || result.Action != "color" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	inventory, err := c.Inventory(t.Context())
	if err != nil || inventory.Devices[0].Endpoints[1].Hue == nil || *inventory.Devices[0].Endpoints[1].Hue != 0 || inventory.Devices[0].Endpoints[1].Saturation == nil || *inventory.Devices[0].Endpoints[1].Saturation != 254 {
		t.Fatalf("confirmed color was not projected: %+v, %v", inventory, err)
	}
}

func TestXYOnlyColorCommandUsesAdvertisedMoveToColor(t *testing.T) {
	n := fixtureNode(t, "1", "XY Light")
	n.Attributes["1/29/1"] = json.RawMessage(`[29,6,8,768]`)
	n.Attributes["1/768/65529"] = json.RawMessage(`[7]`)
	wantX, wantY := rgbToMatterXY("#00ff00")
	address := peer(t, func(ctx context.Context, conn *websocket.Conn) {
		if wsjson.Write(ctx, conn, testInfo()) != nil {
			return
		}
		var request testRequest
		if wsjson.Read(ctx, conn, &request) != nil || writeResult(ctx, conn, request.MessageID, []node{n}) != nil {
			return
		}
		for wsjson.Read(ctx, conn, &request) == nil {
			switch request.Command {
			case "device_command":
				var args struct {
					CommandName string `json:"command_name"`
					Payload     struct {
						ColorX uint16 `json:"colorX"`
						ColorY uint16 `json:"colorY"`
					} `json:"payload"`
				}
				if json.Unmarshal(request.Args, &args) != nil || args.CommandName != "moveToColor" || args.Payload.ColorX != wantX || args.Payload.ColorY != wantY {
					t.Errorf("command = %+v", args)
					return
				}
				if writeResult(ctx, conn, request.MessageID, nil) != nil {
					return
				}
			case "read_attribute":
				var args struct {
					AttributePath string `json:"attribute_path"`
				}
				if json.Unmarshal(request.Args, &args) != nil {
					return
				}
				var value uint16
				switch args.AttributePath {
				case "1/768/3":
					value = wantX
				case "1/768/4":
					value = wantY
				default:
					t.Errorf("read path = %s", args.AttributePath)
					return
				}
				if writeResult(ctx, conn, request.MessageID, map[string]any{args.AttributePath: value}) != nil {
					return
				}
			default:
				t.Errorf("request = %s", request.Command)
				return
			}
		}
	})
	c, _, _ := runClient(t, address)
	waitInventory(t, c, func(i api.Inventory, err error) bool { return err == nil && len(i.Devices) == 1 })
	result, err := c.Light(t.Context(), api.LightRequest{Target: "1", Endpoint: 1, Action: "color", Color: "#00ff00"})
	if err != nil || result.NodeID != "1" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}
