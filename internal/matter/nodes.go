package matter

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type node struct {
	NodeID          api.NodeID                 `json:"node_id"`
	Available       bool                       `json:"available"`
	IsBridge        bool                       `json:"is_bridge"`
	Attributes      map[string]json.RawMessage `json:"attributes"`
	EndpointParents map[uint16]uint16          `json:"endpoint_parents"`
	observedAt      time.Time
	addedEndpoints  map[uint16]bool
}

func attribute[T any](n node, endpoint uint16, cluster, id uint32) (T, bool) {
	var value T
	raw, ok := n.Attributes[fmt.Sprintf("%d/%d/%d", endpoint, cluster, id)]
	if !ok || string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
		return value, false
	}
	return value, true
}

func textAttribute(n node, endpoint uint16, cluster, id uint32) string {
	value, _ := attribute[string](n, endpoint, cluster, id)
	return strings.TrimSpace(value)
}

func parsePath(path string) (uint16, uint32, uint32, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	endpoint, e1 := strconv.ParseUint(parts[0], 10, 16)
	cluster, e2 := strconv.ParseUint(parts[1], 10, 32)
	attr, e3 := strconv.ParseUint(parts[2], 10, 32)
	return uint16(endpoint), uint32(cluster), uint32(attr), e1 == nil && e2 == nil && e3 == nil
}

func project(n node) api.Device {
	device := api.Device{
		NodeID: n.NodeID, Label: textAttribute(n, 0, clusterBasicInformation, attributeNodeLabel),
		Vendor: textAttribute(n, 0, clusterBasicInformation, attributeVendorName), Product: textAttribute(n, 0, clusterBasicInformation, attributeProductName),
		Available: n.Available, IsBridge: n.IsBridge, ObservedAt: n.observedAt,
		Endpoints: []api.Endpoint{},
	}
	endpoints := map[uint16]bool{}
	for path := range n.Attributes {
		if id, _, _, ok := parsePath(path); ok {
			endpoints[id] = true
		}
	}
	for id := range n.EndpointParents {
		endpoints[id] = true
	}
	for id := range n.addedEndpoints {
		endpoints[id] = true
	}
	for _, id := range sortedKeys(endpoints) {
		endpoint := api.Endpoint{
			ID: id, Label: textAttribute(n, id, clusterBridgedDeviceInformation, attributeBridgedNodeLabel),
			DeviceTypes: []api.DeviceType{}, ServerClusters: []api.Cluster{}, ClientClusters: []api.Cluster{}, Capabilities: []string{},
		}
		if parent, ok := n.EndpointParents[id]; ok {
			endpoint.ParentID = &parent
		}
		if raw, found := n.Attributes[fmt.Sprintf("%d/%d/%d", id, clusterDescriptor, attributeTagList)]; found {
			var tags []struct {
				NamespaceID   *uint8 `json:"namespaceID"`
				NamespaceId   *uint8 `json:"namespaceId"`
				WireNamespace *uint8 `json:"1"`
				Tag           *uint8 `json:"tag"`
				WireTag       *uint8 `json:"2"`
				Label         string `json:"label"`
				WireLabel     string `json:"3"`
			}
			if json.Unmarshal(raw, &tags) == nil {
				for _, tag := range tags {
					namespace, value := tag.NamespaceID, tag.Tag
					if namespace == nil {
						namespace = tag.NamespaceId
					}
					if namespace == nil {
						namespace = tag.WireNamespace
					}
					if value == nil {
						value = tag.WireTag
					}
					if namespace == nil || value == nil {
						continue
					}
					label := tag.Label
					if label == "" {
						label = tag.WireLabel
					}
					endpoint.Tags = append(endpoint.Tags, api.SemanticTag{Namespace: *namespace, Tag: *value, Label: label})
				}
			}
		}
		// matter.js uses the numeric struct field IDs on the wire. Accept the
		// named representation too, as used by other Matter controllers.
		types, typesKnown := attribute[[]struct {
			DeviceType *uint32 `json:"deviceType"`
			Revision   *uint16 `json:"revision"`
			WireType   *uint32 `json:"0"`
			WireRev    *uint16 `json:"1"`
		}](n, id, clusterDescriptor, attributeDeviceTypeList)
		for _, dt := range types {
			deviceType, revision := dt.DeviceType, dt.Revision
			if deviceType == nil {
				deviceType = dt.WireType
			}
			if revision == nil {
				revision = dt.WireRev
			}
			if deviceType == nil || revision == nil {
				typesKnown = false
				continue
			}
			endpoint.DeviceTypes = append(endpoint.DeviceTypes, api.DeviceType{ID: *deviceType, Name: deviceTypeName(*deviceType), Revision: *revision})
		}
		servers, serversKnown := attribute[[]uint32](n, id, clusterDescriptor, attributeServerList)
		clients, clientsKnown := attribute[[]uint32](n, id, clusterDescriptor, attributeClientList)
		endpoint.DescriptorComplete = typesKnown && serversKnown && clientsKnown
		if !serversKnown {
			observed := map[uint32]bool{}
			for path := range n.Attributes {
				if ep, cluster, _, ok := parsePath(path); ok && ep == id {
					observed[cluster] = true
				}
			}
			servers = sortedKeys(observed)
		}
		for _, clusterID := range uniqueSorted(servers) {
			cluster := projectCluster(n, id, clusterID)
			endpoint.ServerClusters = append(endpoint.ServerClusters, cluster)
			if clusterID == clusterFixedLabel || clusterID == clusterUserLabel {
				var values []struct {
					Name      string `json:"label"`
					Value     string `json:"value"`
					WireName  string `json:"0"`
					WireValue string `json:"1"`
				}
				if raw, found := n.Attributes[fmt.Sprintf("%d/%d/%d", id, clusterID, attributeLabelList)]; found && json.Unmarshal(raw, &values) == nil {
					for _, value := range values {
						name, text := value.Name, value.Value
						if name == "" {
							name = value.WireName
						}
						if text == "" {
							text = value.WireValue
						}
						if name != "" && text != "" {
							endpoint.Labels = append(endpoint.Labels, api.Label{Name: name, Value: text})
						}
					}
				}
			}
			if clusterID == clusterOnOff {
				for _, command := range cluster.AcceptedCommands {
					if name := powerName(command.ID); name != "" {
						endpoint.Capabilities = append(endpoint.Capabilities, "power."+name)
					}
				}
				if power, ok := attribute[bool](n, id, clusterOnOff, attributeOnOff); ok {
					endpoint.Power = &power
				}
			}
			if clusterID == clusterLevelControl {
				for _, command := range cluster.AcceptedCommands {
					if command.ID == commandMoveToLevelWithOnOff {
						endpoint.Capabilities = append(endpoint.Capabilities, "brightness.set")
					}
				}
			}
			if clusterID == clusterLevelControl {
				if level, ok := attribute[uint8](n, id, clusterLevelControl, attributeCurrentLevel); ok {
					endpoint.Level = &level
				}
			}
			if clusterID == clusterColorControl {
				for _, command := range cluster.AcceptedCommands {
					switch command.ID {
					case commandMoveToColorTemperature:
						endpoint.Capabilities = append(endpoint.Capabilities, "color.temperature")
					case commandMoveToHueAndSaturation:
						endpoint.Capabilities = append(endpoint.Capabilities, "color.hue_saturation")
					case commandMoveToColor:
						endpoint.Capabilities = append(endpoint.Capabilities, "color.xy")
					}
				}
				if value, ok := attribute[uint16](n, id, clusterColorControl, attributeColorTemperature); ok && value > 0 {
					kelvinValue := 1_000_000 / uint32(value)
					if kelvinValue <= 65535 {
						kelvin := uint16(kelvinValue)
						endpoint.ColorTemperature = &kelvin
					}
				}
				if value, ok := attribute[uint16](n, id, clusterColorControl, attributeColorTempPhysicalMax); ok && value >= 100 && value <= 1000 {
					kelvinValue := 1_000_000 / uint32(value)
					if kelvinValue <= 65535 {
						kelvin := uint16(kelvinValue)
						endpoint.ColorTemperatureMin = &kelvin
					}
				}
				if value, ok := attribute[uint16](n, id, clusterColorControl, attributeColorTempPhysicalMin); ok && value >= 100 && value <= 1000 {
					kelvinValue := 1_000_000 / uint32(value)
					if kelvinValue <= 65535 {
						kelvin := uint16(kelvinValue)
						endpoint.ColorTemperatureMax = &kelvin
					}
				}
				if value, ok := attribute[uint8](n, id, clusterColorControl, attributeCurrentHue); ok {
					endpoint.Hue = &value
				}
				if value, ok := attribute[uint8](n, id, clusterColorControl, attributeCurrentSaturation); ok {
					endpoint.Saturation = &value
				}
				if value, ok := attribute[uint16](n, id, clusterColorControl, attributeCurrentX); ok {
					endpoint.ColorX = &value
				}
				if value, ok := attribute[uint16](n, id, clusterColorControl, attributeCurrentY); ok {
					endpoint.ColorY = &value
				}
				if value, ok := attribute[uint8](n, id, clusterColorControl, attributeColorMode); ok {
					endpoint.ColorMode = &value
				}
			}
			if clusterID == clusterSwitch {
				if position, ok := attribute[uint8](n, id, clusterSwitch, attributeSwitchCurrentPosition); ok {
					endpoint.SwitchPosition = &position
				}
			}
		}
		for _, clusterID := range uniqueSorted(clients) {
			endpoint.ClientClusters = append(endpoint.ClientClusters, api.Cluster{ID: clusterID, Name: clusterName(clusterID)})
		}
		for path, raw := range n.Attributes {
			ep, clusterID, attributeID, ok := parsePath(path)
			if !ok || ep != id || string(raw) == "null" {
				continue
			}
			if !slices.ContainsFunc(endpoint.ServerClusters, func(cluster api.Cluster) bool { return cluster.ID == clusterID }) {
				continue
			}
			if state, known := decodeState(clusterID, attributeID, raw); known && string(state.Value) != "null" {
				endpoint.States = append(endpoint.States, state)
			}
		}
		slices.SortFunc(endpoint.States, func(a, b api.State) int {
			if a.ClusterID != b.ClusterID {
				return int(a.ClusterID) - int(b.ClusterID)
			}
			return int(a.AttributeID) - int(b.AttributeID)
		})
		device.Endpoints = append(device.Endpoints, endpoint)
	}
	return device
}

func projectCluster(n node, endpoint uint16, id uint32) api.Cluster {
	cluster := api.Cluster{ID: id, Name: clusterName(id), Attributes: []uint32{}, Events: []uint32{}}
	if commands, known := attribute[[]uint32](n, endpoint, id, attributeAcceptedCommands); known {
		cluster.AcceptedCommands = []api.Command{}
		for _, command := range uniqueSorted(commands) {
			cluster.AcceptedCommands = append(cluster.AcceptedCommands, api.Command{ID: command, Name: commandName(id, command)})
		}
	}
	attrs, _ := attribute[[]uint32](n, endpoint, id, attributeAttributeList)
	for path := range n.Attributes {
		if ep, cl, attr, ok := parsePath(path); ok && ep == endpoint && cl == id {
			attrs = append(attrs, attr)
		}
	}
	cluster.Attributes = uniqueSorted(attrs)
	for _, attributeID := range cluster.Attributes {
		if name := attributeMemberName(id, attributeID); name != "" {
			if cluster.AttributeNames == nil {
				cluster.AttributeNames = make(map[uint32]string)
			}
			cluster.AttributeNames[attributeID] = name
		}
	}
	events, _ := attribute[[]uint32](n, endpoint, id, attributeEventList)
	cluster.Events = uniqueSorted(events)
	for _, eventID := range cluster.Events {
		if name := standardEvents[memberKey{id, eventID}]; name != "" {
			if cluster.EventNames == nil {
				cluster.EventNames = make(map[uint32]string)
			}
			cluster.EventNames[eventID] = name
		}
	}
	// FeatureMap is either a bitmask or matter.js's decoded boolean object.
	if raw, ok := attribute[json.RawMessage](n, endpoint, id, attributeFeatureMap); ok {
		var bits uint32
		var features map[string]bool
		if json.Unmarshal(raw, &bits) == nil || json.Unmarshal(raw, &features) == nil {
			cluster.FeatureMap = slices.Clone(raw)
		}
	}
	return cluster
}

func sortedKeys[K ~uint16 | ~uint32](values map[K]bool) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func uniqueSorted(values []uint32) []uint32 {
	copy := append([]uint32{}, values...)
	slices.Sort(copy)
	return slices.Compact(copy)
}

func powerName(id uint32) string {
	if id > 2 {
		return ""
	}
	return strings.ToLower(commandName(clusterOnOff, id))
}
