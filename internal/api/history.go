package api

import (
	"context"
	"encoding/json"
	"time"
)

const (
	HistoryPath     = "/api/v1/history"
	AutomationsPath = "/api/v1/automations"
)

// HistoryEntry is a durable, projected household observation. RelatedNodes
// includes trigger and target devices so either device can select the run.
type HistoryEntry struct {
	ID           int64           `json:"id"`
	ObservedAt   time.Time       `json:"observed_at"`
	Kind         string          `json:"kind"`
	Summary      string          `json:"summary"`
	NodeID       NodeID          `json:"node_id,omitempty"`
	RelatedNodes []NodeID        `json:"related_nodes,omitempty"`
	DeviceLabel  string          `json:"device_label,omitempty"`
	Endpoint     *uint16         `json:"endpoint,omitempty"`
	File         string          `json:"file,omitempty"`
	Revision     string          `json:"revision,omitempty"`
	Rule         int             `json:"rule,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
	Outcome      string          `json:"outcome,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
}

type HistoryQuery struct {
	Since   time.Time
	Until   time.Time
	Device  NodeID
	File    string
	Kind    string
	Outcome string
	RunID   string
	Before  int64
	After   int64
	Limit   int
}

type RecordingStatus struct {
	State     string     `json:"state"`
	Since     *time.Time `json:"since,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

type HistoryPage struct {
	Entries   []HistoryEntry  `json:"entries"`
	Next      int64           `json:"next,omitempty"`
	Recording RecordingStatus `json:"recording"`
}

type AutomationFile struct {
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
	Rules    int    `json:"rules"`
	Status   string `json:"status"`
	Paused   bool   `json:"paused"`
	Error    string `json:"error,omitempty"`
}

type HistorySource interface {
	History(context.Context, HistoryQuery) (HistoryPage, error)
	HistoryEntry(context.Context, int64) (HistoryEntry, error)
	RecordingStatus() RecordingStatus
	Record(HistoryEntry)
}

type AutomationSource interface {
	Automations(context.Context) ([]AutomationFile, error)
	SetAutomationPaused(context.Context, string, bool) (AutomationFile, error)
}
