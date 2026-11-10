package history

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestRecorderTurnsOverflowIntoDurableGap(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	recorder := NewRecorder(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder.input = make(chan observation, 1)
	recorder.Record(api.HistoryEntry{Kind: "test", Summary: "kept"})
	recorder.Record(api.HistoryEntry{Kind: "test", Summary: "lost"})
	if recorder.RecordingStatus().State != "degraded" {
		t.Fatal("queue overflow was not exposed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recorder.Run(ctx)
	page, err := store.Query(t.Context(), api.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Kind != "test" || page.Entries[1].Kind != "observation.gap" {
		t.Fatalf("records after overflow = %#v", page.Entries)
	}
}

func TestRecorderStoresChangedStateOnce(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	recorder := NewRecorder(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	at := time.Now().UTC()
	payload, err := json.Marshal(struct {
		Path   string     `json:"path"`
		Device api.Device `json:"device"`
		State  api.State  `json:"state"`
	}{"1/1030/0", api.Device{NodeID: "2", Label: "Den Presence Sensor"},
		api.State{ClusterID: 1030, Name: "occupancy", Value: json.RawMessage("false"), Text: "unoccupied"}})
	if err != nil {
		t.Fatal(err)
	}
	event := api.Event{Type: "attribute_updated", ObservedAt: at, NodeID: "2", Data: payload}
	recorder.process(context.Background(), observation{event: &event})
	recorder.process(context.Background(), observation{event: &event})
	page, err := store.Query(t.Context(), api.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Summary != "Den Presence Sensor occupancy changed to unoccupied" {
		t.Fatalf("changed-state records = %#v", page.Entries)
	}
}
