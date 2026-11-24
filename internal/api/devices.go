package api

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	DevicesPath = "/api/v1/devices"
	InspectPath = "/api/v1/devices/inspect"
	EventsPath  = "/api/v1/events"
)

var (
	ErrNotFound  = errors.New("device not found")
	ErrAmbiguous = errors.New("ambiguous device target; use a node ID")
)

// NodeID preserves 64-bit wire IDs and uses strings in the browser-facing API.
type NodeID string

func (id *NodeID) UnmarshalJSON(data []byte) error {
	value := string(data)
	if strings.HasPrefix(value, `"`) {
		if err := json.Unmarshal(data, &value); err != nil {
			return errors.New("invalid node ID")
		}
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return errors.New("invalid node ID")
	}
	*id = NodeID(strconv.FormatUint(number, 10))
	return nil
}

type Inventory struct {
	Connection Connection `json:"connection"`
	Ready      bool       `json:"ready"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Devices    []Device   `json:"devices"`
	Error      string     `json:"error,omitempty"`
}

type Device struct {
	NodeID     NodeID     `json:"node_id"`
	Label      string     `json:"label,omitempty"`
	Vendor     string     `json:"vendor,omitempty"`
	Product    string     `json:"product,omitempty"`
	Available  bool       `json:"available"`
	IsBridge   bool       `json:"is_bridge"`
	ObservedAt time.Time  `json:"observed_at"`
	Endpoints  []Endpoint `json:"endpoints"`
}

type Endpoint struct {
	ID                  uint16        `json:"id"`
	ParentID            *uint16       `json:"parent_id,omitempty"`
	Label               string        `json:"label,omitempty"`
	Labels              []Label       `json:"labels,omitempty"`
	Tags                []SemanticTag `json:"tags,omitempty"`
	DeviceTypes         []DeviceType  `json:"device_types"`
	ServerClusters      []Cluster     `json:"server_clusters"`
	ClientClusters      []Cluster     `json:"client_clusters"`
	DescriptorComplete  bool          `json:"descriptor_complete"`
	Capabilities        []string      `json:"capabilities"`
	Power               *bool         `json:"power,omitempty"`
	Level               *uint8        `json:"level,omitempty"`
	ColorTemperature    *uint16       `json:"color_temperature,omitempty"`
	ColorTemperatureMin *uint16       `json:"color_temperature_min,omitempty"`
	ColorTemperatureMax *uint16       `json:"color_temperature_max,omitempty"`
	Hue                 *uint8        `json:"hue,omitempty"`
	Saturation          *uint8        `json:"saturation,omitempty"`
	ColorX              *uint16       `json:"color_x,omitempty"`
	ColorY              *uint16       `json:"color_y,omitempty"`
	ColorMode           *uint8        `json:"color_mode,omitempty"`
	SwitchPosition      *uint8        `json:"switch_position,omitempty"`
	States              []State       `json:"states,omitempty"`
}

// Label is a standard Matter FixedLabel or UserLabel endpoint tag.
type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SemanticTag is a standard Descriptor TagList entry. Namespace and tag IDs
// retain their meaning even when the optional text label is absent.
type SemanticTag struct {
	Namespace uint8  `json:"namespace"`
	Tag       uint8  `json:"tag"`
	Label     string `json:"label,omitempty"`
}

// State is an interpreted or marked raw application attribute. Value is a JSON
// scalar; Text is its compact display representation. Raw values have no
// inferred unit or enum meaning.
type State struct {
	ClusterID   uint32          `json:"cluster_id"`
	AttributeID uint32          `json:"attribute_id"`
	Name        string          `json:"name"`
	Value       json.RawMessage `json:"value"`
	Text        string          `json:"text"`
	Unit        string          `json:"unit,omitempty"`
	Raw         bool            `json:"raw,omitempty"`
}

type DeviceType struct {
	ID       uint32 `json:"id"`
	Name     string `json:"name,omitempty"`
	Revision uint16 `json:"revision"`
}

type Cluster struct {
	ID               uint32            `json:"id"`
	Name             string            `json:"name,omitempty"`
	FeatureMap       json.RawMessage   `json:"feature_map,omitempty"`
	AcceptedCommands []Command         `json:"accepted_commands"`
	Attributes       []uint32          `json:"attributes"`
	AttributeNames   map[uint32]string `json:"attribute_names,omitempty"`
	Events           []uint32          `json:"events"`
	EventNames       map[uint32]string `json:"event_names,omitempty"`
}

type Command struct {
	ID   uint32 `json:"id"`
	Name string `json:"name,omitempty"`
}

// Event.Data contains a projected application object, never an upstream frame.
type Event struct {
	Type       string          `json:"type"`
	ObservedAt time.Time       `json:"observed_at"`
	NodeID     NodeID          `json:"node_id,omitempty"`
	Data       json.RawMessage `json:"data"`
}

type DeviceSource interface {
	Inventory(context.Context) (Inventory, error)
	Inspect(context.Context, string) (Device, error)
	Watch(context.Context, string) (<-chan Event, error)
}

type ErrorResponse struct {
	Error string `json:"error"`
}
