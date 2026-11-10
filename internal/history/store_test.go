package history

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestHistoryPersistsAndFiltersRelatedDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	first, err := store.Append(ctx, api.HistoryEntry{
		ObservedAt: now.Add(-time.Minute), Kind: "device.state", Summary: "Den occupied",
		NodeID: "2", RelatedNodes: []api.NodeID{"1", "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append(ctx, api.HistoryEntry{
		ObservedAt: now, Kind: "automation.action", Summary: "Den Light off",
		NodeID: "1", RelatedNodes: []api.NodeID{"1", "2"}, File: "den.star",
		Outcome: "confirmed", RunID: "run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPaused(ctx, "den.star", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	paused, err := store.PausedFiles(ctx)
	if err != nil || !paused["den.star"] {
		t.Fatalf("pause did not survive restart: %v, %v", paused, err)
	}
	page, err := store.Query(ctx, api.HistoryQuery{Device: "1", Since: now.Add(-time.Hour), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].ID != second.ID || page.Next != second.ID {
		t.Fatalf("first page = %#v", page)
	}
	page, err = store.Query(ctx, api.HistoryQuery{Device: "1", Before: page.Next, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].ID != first.ID || page.Next != 0 {
		t.Fatalf("older page = %#v", page)
	}
	page, err = store.Query(ctx, api.HistoryQuery{RunID: "run-1", Limit: 10})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ID != second.ID {
		t.Fatalf("run query = %#v, %v", page, err)
	}
	page, err = store.Query(ctx, api.HistoryQuery{After: first.ID, Limit: 10})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ID != second.ID {
		t.Fatalf("follow query = %#v, %v", page, err)
	}
}

func TestPauseRejectsInvalidFileName(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for _, name := range []string{"../den.star", "den.txt", ""} {
		if err := store.SetPaused(context.Background(), name, true); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
