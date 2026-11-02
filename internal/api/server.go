// Package api defines the application API shared by HTTP handlers and clients.
package api

import (
	"context"
	"errors"
	"time"
)

const ServerInfoPath = "/api/v1/server/info"

var ErrUnavailable = errors.New("upstream Matter Server is unavailable")

// ServerInfo deliberately excludes fabric identifiers and arbitrary upstream data.
type ServerInfo struct {
	SDKVersion                string `json:"sdk_version"`
	SchemaVersion             int    `json:"schema_version"`
	MinSupportedSchemaVersion int    `json:"min_supported_schema_version"`
	BluetoothEnabled          bool   `json:"bluetooth_enabled"`
	WiFiCredentialsSet        bool   `json:"wifi_credentials_set"`
	ThreadCredentialsSet      bool   `json:"thread_credentials_set"`
}

type Connection struct {
	State       string     `json:"state"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type ServerStatus struct {
	Connection Connection  `json:"connection"`
	Server     *ServerInfo `json:"server,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type InfoSource interface {
	ServerInfo(context.Context) (ServerStatus, error)
}
