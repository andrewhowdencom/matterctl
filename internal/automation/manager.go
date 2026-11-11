package automation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type PauseStore interface {
	SetPaused(context.Context, string, bool) error
	PausedFiles(context.Context) (map[string]bool, error)
}

type ActivityRecorder interface{ Record(api.HistoryEntry) }

type managedFile struct {
	info   api.AutomationFile
	engine *Engine
}

// Manager keeps runtime file status and durable file-level pause state.
type Manager struct {
	mu           sync.RWMutex
	files        []*managedFile
	byName       map[string]*managedFile
	pauses       PauseStore
	recorder     ActivityRecorder
	logger       *slog.Logger
	sequence     atomic.Uint64
	startupError string
}

func DisabledManager(err error) *Manager {
	return &Manager{byName: map[string]*managedFile{}, startupError: err.Error()}
}

func NewManager(ctx context.Context, directory string, pauses PauseStore, recorder ActivityRecorder, logger *slog.Logger) (*Manager, error) {
	m := &Manager{byName: map[string]*managedFile{}, pauses: pauses, recorder: recorder, logger: logger}
	paused, err := pauses.PausedFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("read persisted automation pauses: %w", err)
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read automation directory: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".star") {
			continue
		}
		if len(m.files) == maxPrograms {
			return nil, fmt.Errorf("automation directory contains more than %d programs", maxPrograms)
		}
		file := &managedFile{info: api.AutomationFile{Name: entry.Name(), Paused: paused[entry.Name()]}}
		if !entry.Type().IsRegular() {
			file.info.Error = "not a regular file"
		} else {
			path := filepath.Join(directory, entry.Name())
			source, readErr := readProgram(path)
			if readErr != nil {
				file.info.Error = readErr.Error()
			} else {
				sum := sha256.Sum256(source)
				file.info.Revision = fmt.Sprintf("%x", sum[:6])
				file.engine, readErr = Load(ctx, path, source)
				if readErr != nil {
					file.info.Error = readErr.Error()
				} else {
					file.info.Rules = file.engine.RuleCount()
				}
			}
		}
		file.info.Status = fileStatus(file.info)
		m.files = append(m.files, file)
		m.byName[file.info.Name] = file
		outcome := file.info.Status
		m.record(api.HistoryEntry{Kind: "automation.file", Summary: fmt.Sprintf("%s %s", file.info.Name, outcome),
			File: file.info.Name, Revision: file.info.Revision, Outcome: outcome})
	}
	return m, nil
}

func readProgram(path string) ([]byte, error) {
	// #nosec G304 -- the configured directory is selected by the operator.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	source, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(source) > 1<<20 {
		return nil, errors.New("automation file exceeds 1 MiB")
	}
	return source, nil
}

func fileStatus(info api.AutomationFile) string {
	if info.Error != "" {
		return "failed"
	}
	if info.Paused {
		return "paused"
	}
	return "active"
}

func (m *Manager) Automations(ctx context.Context) ([]api.AutomationFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.startupError != "" {
		return []api.AutomationFile{{Name: "(all automations)", Status: "failed", Error: m.startupError}}, nil
	}
	files := make([]api.AutomationFile, 0, len(m.files))
	for _, file := range m.files {
		files = append(files, file.info)
	}
	return files, nil
}

func (m *Manager) SetAutomationPaused(ctx context.Context, name string, paused bool) (api.AutomationFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startupError != "" {
		return api.AutomationFile{}, api.ErrUnavailable
	}
	file, found := m.byName[name]
	if !found {
		return api.AutomationFile{}, api.ErrNotFound
	}
	if file.info.Paused == paused {
		return file.info, nil
	}
	if err := m.pauses.SetPaused(ctx, name, paused); err != nil {
		return api.AutomationFile{}, fmt.Errorf("persist automation pause: %w", err)
	}
	file.info.Paused = paused
	file.info.Status = fileStatus(file.info)
	action := "resumed"
	if paused {
		action = "paused"
	}
	m.record(api.HistoryEntry{Kind: "automation.pause", Summary: name + " " + action,
		File: name, Revision: file.info.Revision, Outcome: action})
	return file.info, nil
}

func (m *Manager) isPaused(file *managedFile) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return file.info.Paused
}

func (m *Manager) record(entry api.HistoryEntry) {
	if m.recorder == nil {
		return
	}
	if entry.ObservedAt.IsZero() {
		entry.ObservedAt = time.Now().UTC()
	}
	m.recorder.Record(entry)
}

// Run evaluates active and paused files from the same projected event stream.
// Paused files consume inputs to keep baselines current but send no actions.
func (m *Manager) Run(ctx context.Context, source api.DeviceSource, executor Executor) {
	if m.startupError != "" {
		return
	}
	if len(m.files) == 0 {
		return
	}
	for ctx.Err() == nil {
		stream, err := source.Watch(ctx, "")
		if err == nil {
			m.logger.Info("automation stream connected", "files", len(m.files))
			for event := range stream {
				if ctx.Err() != nil {
					return
				}
				if event.Type == "node_added" || event.Type == "endpoint_added" {
					if inventory, refreshErr := source.Inventory(ctx); refreshErr == nil {
						for _, file := range m.files {
							if file.engine != nil {
								if err := file.engine.applyInventory(inventory); err != nil {
									m.logger.Warn("automation inventory unavailable", "file", file.info.Name, "error", err)
								}
							}
						}
					}
				}
				for _, file := range m.files {
					if file.engine == nil {
						continue
					}
					firings, feedErr := file.engine.Feed(ctx, event)
					if feedErr != nil {
						m.record(api.HistoryEntry{Kind: "automation.error", Summary: file.info.Name + ": " + feedErr.Error(),
							File: file.info.Name, Revision: file.info.Revision, NodeID: event.NodeID, Outcome: "failed"})
						m.logger.Warn("automation event rejected", "file", file.info.Name, "event", event.Type, "error", feedErr)
						continue
					}
					for _, firing := range firings {
						m.executeFiring(ctx, executor, file, firing)
					}
				}
			}
			if ctx.Err() != nil {
				return
			}
			err = api.ErrUnavailable
		}
		m.record(api.HistoryEntry{Kind: "automation.stream_gap",
			Summary: "automation event stream interrupted; resubscribing", Outcome: "unknown"})
		m.logger.Warn("automation stream interrupted; resubscribing", "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (m *Manager) executeFiring(ctx context.Context, executor Executor, file *managedFile, firing Firing) {
	runID := fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), m.sequence.Add(1))
	related := []api.NodeID{firing.Signal.NodeID}
	for _, action := range firing.Actions {
		related = append(related, action.NodeID)
	}
	slices.Sort(related)
	related = slices.Compact(related)
	outcome := "planned"
	if len(firing.Actions) == 0 {
		outcome = "no_action"
	}
	if m.isPaused(file) {
		outcome = "paused"
	}
	m.record(api.HistoryEntry{Kind: "automation.rule",
		Summary: fmt.Sprintf("%s rule %d received %s: %s", file.info.Name, firing.Rule, firing.Signal.Kind, outcome),
		NodeID:  firing.Signal.NodeID, Endpoint: &firing.Signal.Endpoint, RelatedNodes: related,
		File: file.info.Name, Revision: file.info.Revision, Rule: firing.Rule, RunID: runID,
		Outcome: outcome, Details: mustActionJSON(firing)})
	if outcome == "paused" || outcome == "no_action" {
		return
	}
	for _, action := range firing.Actions {
		if ctx.Err() != nil {
			return
		}
		if m.isPaused(file) {
			m.record(api.HistoryEntry{Kind: "automation.action",
				Summary: fmt.Sprintf("%s %s on node %s skipped after pause", file.info.Name, action.Kind, action.NodeID),
				NodeID:  action.NodeID, Endpoint: &action.Endpoint, RelatedNodes: related,
				File: file.info.Name, Revision: file.info.Revision, Rule: firing.Rule, RunID: runID,
				Outcome: "skipped", Details: mustActionJSON(action)})
			return
		}
		err := executor.Execute(ctx, action)
		result := "confirmed"
		if err != nil {
			result = "failed"
			if errors.Is(err, api.ErrUnconfirmed) || errors.Is(err, context.DeadlineExceeded) {
				result = "unknown"
			}
		}
		summary := fmt.Sprintf("%s %s on node %s: %s", file.info.Name, action.Kind, action.NodeID, result)
		if err != nil {
			summary += " (" + err.Error() + ")"
		}
		m.record(api.HistoryEntry{Kind: "automation.action", Summary: summary,
			NodeID: action.NodeID, Endpoint: &action.Endpoint, RelatedNodes: related,
			File: file.info.Name, Revision: file.info.Revision, Rule: firing.Rule, RunID: runID,
			Outcome: result, Details: mustActionJSON(action)})
		if err != nil {
			m.logger.Error("automation action failed", "file", file.info.Name, "rule", firing.Rule, "action", action.Kind, "error", err)
			return
		}
	}
}

func mustActionJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
