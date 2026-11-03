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
	ID                 uint16       `json:"id"`
	ParentID           *uint16      `json:"parent_id,omitempty"`
	Label              string       `json:"label,omitempty"`
	DeviceTypes        []DeviceType `json:"device_types"`
	ServerClusters     []Cluster    `json:"server_clusters"`
	ClientClusters     []Cluster    `json:"client_clusters"`
	DescriptorComplete bool         `json:"descriptor_complete"`
	Capabilities       []string     `json:"capabilities"`
	Power              *bool        `json:"power,omitempty"`
	Level              *uint8       `json:"level,omitempty"`
	SwitchPosition     *uint8       `json:"switch_position,omitempty"`
}

type DeviceType struct {
	ID       uint32 `json:"id"`
	Name     string `json:"name,omitempty"`
	Revision uint16 `json:"revision"`
}

type Cluster struct {
	ID               uint32          `json:"id"`
	Name             string          `json:"name,omitempty"`
	FeatureMap       json.RawMessage `json:"feature_map,omitempty"`
	AcceptedCommands []Command       `json:"accepted_commands"`
	Attributes       []uint32        `json:"attributes"`
	Events           []uint32        `json:"events"`
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
