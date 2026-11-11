package automation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type pauseMemory struct{ values map[string]bool }

func (p *pauseMemory) SetPaused(_ context.Context, file string, paused bool) error {
	p.values[file] = paused
	return nil
}
func (p *pauseMemory) PausedFiles(context.Context) (map[string]bool, error) {
	copy := map[string]bool{}
	for key, value := range p.values {
		copy[key] = value
	}
	return copy, nil
}

type historyMemory struct {
	mu      sync.Mutex
	entries []api.HistoryEntry
}

func (r *historyMemory) Record(entry api.HistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

func TestManagerKeepsHealthyFileAndDurablePause(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "good.star"),
		[]byte(`light = device("1", endpoint=1)
sensor = device("2")
on(occupied(sensor, endpoint=1), lambda event: [power(light, "on")])
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bad.star"), []byte("not valid starlark !!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	pauses := &pauseMemory{values: map[string]bool{}}
	records := &historyMemory{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewManager(t.Context(), directory, pauses, records, logger)
	if err != nil {
		t.Fatal(err)
	}
	files, err := manager.Automations(t.Context())
	if err != nil || len(files) != 2 || files[0].Status != "failed" || files[1].Status != "active" {
		t.Fatalf("files = %#v, %v", files, err)
	}
	if _, err := manager.SetAutomationPaused(t.Context(), "good.star", true); err != nil {
		t.Fatal(err)
	}
	manager, err = NewManager(t.Context(), directory, pauses, records, logger)
	if err != nil {
		t.Fatal(err)
	}
	files, _ = manager.Automations(t.Context())
	if files[1].Status != "paused" {
		t.Fatalf("pause did not survive restart: %#v", files[1])
	}
}

func TestManagerPauseStopsRemainingActions(t *testing.T) {
	pauses := &pauseMemory{values: map[string]bool{}}
	records := &historyMemory{}
	manager := &Manager{
		byName: map[string]*managedFile{}, pauses: pauses, recorder: records,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	file := &managedFile{info: api.AutomationFile{Name: "den.star", Status: "active", Revision: "abc"}}
	manager.files = []*managedFile{file}
	manager.byName[file.info.Name] = file
	calls := 0
	executor := automationExecutor(func(ctx context.Context, _ Action) error {
		calls++
		if calls == 1 {
			_, err := manager.SetAutomationPaused(ctx, "den.star", true)
			return err
		}
		return errors.New("second action should not run")
	})
	manager.executeFiring(t.Context(), executor, file, Firing{
		Rule: 1, Signal: Signal{Kind: "button.single_press", NodeID: "2", Endpoint: 1},
		Actions: []Action{{Kind: "power", NodeID: "1", Endpoint: 1}, {Kind: "brightness", NodeID: "1", Endpoint: 1}},
	})
	if calls != 1 {
		t.Fatalf("executed %d actions after pause", calls)
	}
	if !slices.ContainsFunc(records.entries, func(entry api.HistoryEntry) bool {
		return entry.Kind == "automation.action" && entry.Outcome == "skipped"
	}) {
		t.Fatalf("missing skipped action record: %#v", records.entries)
	}
}
