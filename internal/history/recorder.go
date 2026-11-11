package history

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type observation struct {
	event *api.Event
	entry *api.HistoryEntry
}

// Recorder receives projected observations without waiting for disk I/O on
// the Matter reader. A full queue becomes an explicit recording gap.
type Recorder struct {
	store  *Store
	logger *slog.Logger
	input  chan observation
	mu     sync.Mutex
	lost   int
	since  time.Time
	status api.RecordingStatus

	states       map[string]string
	availability map[api.NodeID]bool
	gapStart     time.Time
}

func NewRecorder(store *Store, logger *slog.Logger) *Recorder {
	return &Recorder{
		store: store, logger: logger, input: make(chan observation, 4096),
		status: api.RecordingStatus{State: "healthy"},
		states: map[string]string{}, availability: map[api.NodeID]bool{},
	}
}

func NewUnavailableRecorder(err error, logger *slog.Logger) *Recorder {
	now := time.Now().UTC()
	return &Recorder{logger: logger, status: api.RecordingStatus{
		State: "degraded", Since: &now, LastError: err.Error(),
	}}
}

func (r *Recorder) Observe(event api.Event)       { r.offer(observation{event: &event}) }
func (r *Recorder) Record(entry api.HistoryEntry) { r.offer(observation{entry: &entry}) }

func (r *Recorder) offer(item observation) {
	if r.store == nil {
		return
	}
	select {
	case r.input <- item:
	default:
		r.markLost(time.Now().UTC(), "recording queue overflow")
	}
}

func (r *Recorder) markLost(at time.Time, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lost++
	if r.since.IsZero() {
		r.since = at
	}
	r.status = api.RecordingStatus{State: "degraded", Since: &r.since, LastError: reason}
}

func (r *Recorder) RecordingStatus() api.RecordingStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.status
	if result.Since != nil {
		value := *result.Since
		result.Since = &value
	}
	return result
}

func (r *Recorder) Run(ctx context.Context) {
	if r.store == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			// Drain observations already accepted before shutdown.
			for {
				select {
				case item := <-r.input:
					r.process(context.Background(), item)
				default:
					return
				}
			}
		case item := <-r.input:
			r.process(ctx, item)
		}
	}
}

func (r *Recorder) process(ctx context.Context, item observation) {
	entries := []api.HistoryEntry{}
	if item.entry != nil {
		entries = append(entries, *item.entry)
	}
	if item.event != nil {
		entries = append(entries, r.project(*item.event)...)
	}
	for _, entry := range entries {
		r.append(ctx, entry)
	}
}

func (r *Recorder) append(ctx context.Context, entry api.HistoryEntry) {
	r.mu.Lock()
	lost, since := r.lost, r.since
	r.mu.Unlock()
	if lost > 0 {
		gap := api.HistoryEntry{ObservedAt: time.Now().UTC(), Kind: "observation.gap",
			Summary: fmt.Sprintf("%d history observations could not be recorded", lost),
			Outcome: "unknown", Details: mustJSON(struct {
				StartedAt time.Time `json:"started_at"`
				Lost      int       `json:"lost"`
			}{since, lost})}
		if _, err := r.store.Append(ctx, gap); err != nil {
			r.markLost(entry.ObservedAt, "history storage unavailable")
			r.logger.Warn("history write failed", "error", err)
			return
		}
		r.mu.Lock()
		r.lost -= lost
		if r.lost == 0 {
			r.since = time.Time{}
			r.status = api.RecordingStatus{State: "healthy"}
		}
		r.mu.Unlock()
	}
	if _, err := r.store.Append(ctx, entry); err != nil {
		r.markLost(entry.ObservedAt, "history storage unavailable")
		r.logger.Warn("history write failed", "error", err)
	}
}

func (r *Recorder) History(ctx context.Context, query api.HistoryQuery) (api.HistoryPage, error) {
	if r.store == nil {
		return api.HistoryPage{Recording: r.RecordingStatus()}, api.ErrUnavailable
	}
	page, err := r.store.Query(ctx, query)
	page.Recording = r.RecordingStatus()
	return page, err
}

func (r *Recorder) HistoryEntry(ctx context.Context, id int64) (api.HistoryEntry, error) {
	if r.store == nil {
		return api.HistoryEntry{}, api.ErrUnavailable
	}
	return r.store.Get(ctx, id)
}

func (r *Recorder) project(event api.Event) []api.HistoryEntry {
	at := event.ObservedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	switch event.Type {
	case "inventory":
		var inventory api.Inventory
		if json.Unmarshal(event.Data, &inventory) != nil {
			return nil
		}
		var entries []api.HistoryEntry
		for _, device := range inventory.Devices {
			r.availability[device.NodeID] = device.Available
			for _, endpoint := range device.Endpoints {
				for _, state := range endpoint.States {
					key := fmt.Sprintf("%s/%d/%d/%d", device.NodeID, endpoint.ID, state.ClusterID, state.AttributeID)
					r.states[key] = string(state.Value)
					ep := endpoint.ID
					entries = append(entries, api.HistoryEntry{ObservedAt: at, Kind: "device.baseline",
						Summary: fmt.Sprintf("%s %s was %s at synchronization", deviceName(device), state.Name, state.Text),
						NodeID:  device.NodeID, DeviceLabel: device.Label, Endpoint: &ep,
						Details: mustJSON(state)})
				}
			}
		}
		if !r.gapStart.IsZero() {
			entries = append(entries, gapEntry(r.gapStart, at, "Matter Server observations unavailable"))
			r.gapStart = time.Time{}
		}
		return entries
	case "connection":
		var update struct {
			Connection api.Connection `json:"connection"`
			Ready      bool           `json:"ready"`
		}
		if json.Unmarshal(event.Data, &update) != nil {
			return nil
		}
		if !update.Ready && r.gapStart.IsZero() {
			r.gapStart = at
		}
		return []api.HistoryEntry{{ObservedAt: at, Kind: "connection",
			Summary: "Matter Server " + update.Connection.State, Outcome: update.Connection.State}}
	case "attribute_updated":
		var update struct {
			Path   string     `json:"path"`
			Device api.Device `json:"device"`
			State  *api.State `json:"state"`
		}
		if json.Unmarshal(event.Data, &update) != nil || update.State == nil {
			return nil
		}
		key := string(event.NodeID) + "/" + update.Path
		value := string(update.State.Value)
		if r.states[key] == value {
			return nil
		}
		r.states[key] = value
		ep := endpointFromPath(update.Path)
		return []api.HistoryEntry{{ObservedAt: at, Kind: "device.state",
			Summary: fmt.Sprintf("%s %s changed to %s", deviceName(update.Device), update.State.Name, update.State.Text),
			NodeID:  event.NodeID, DeviceLabel: update.Device.Label, Endpoint: ep,
			Details: mustJSON(struct {
				Path  string    `json:"path"`
				State api.State `json:"state"`
			}{update.Path, *update.State})}}
	case "node_event":
		var update struct {
			EndpointID uint16           `json:"endpoint_id"`
			ClusterID  uint32           `json:"cluster_id"`
			EventName  string           `json:"event_name"`
			EventID    uint32           `json:"event_id"`
			Label      string           `json:"label"`
			Product    string           `json:"product"`
			Data       map[string]uint8 `json:"data"`
		}
		if json.Unmarshal(event.Data, &update) != nil || update.ClusterID != 59 {
			return nil
		}
		name := update.EventName
		if name == "" {
			name = fmt.Sprintf("Switch event %d", update.EventID)
		}
		label := update.Label
		if label == "" {
			label = update.Product
		}
		if label == "" {
			label = "Node " + string(event.NodeID)
		}
		return []api.HistoryEntry{{ObservedAt: at, Kind: "device.switch",
			Summary: label + " " + name, NodeID: event.NodeID, DeviceLabel: update.Label,
			Endpoint: &update.EndpointID, Details: mustJSON(update.Data)}}
	case "node_added", "node_updated":
		var device api.Device
		if json.Unmarshal(event.Data, &device) != nil {
			return nil
		}
		previous, known := r.availability[device.NodeID]
		r.availability[device.NodeID] = device.Available
		var entries []api.HistoryEntry
		status := "offline"
		if device.Available {
			status = "available"
		}
		if !known || previous != device.Available {
			entries = append(entries, api.HistoryEntry{ObservedAt: at, Kind: "device.availability",
				Summary: deviceName(device) + " " + status, NodeID: device.NodeID,
				DeviceLabel: device.Label, Outcome: status})
		}
		for _, endpoint := range device.Endpoints {
			for _, state := range endpoint.States {
				key := fmt.Sprintf("%s/%d/%d/%d", device.NodeID, endpoint.ID, state.ClusterID, state.AttributeID)
				if r.states[key] == string(state.Value) {
					continue
				}
				r.states[key] = string(state.Value)
				ep := endpoint.ID
				entries = append(entries, api.HistoryEntry{ObservedAt: at, Kind: "device.state",
					Summary: fmt.Sprintf("%s %s is %s", deviceName(device), state.Name, state.Text),
					NodeID:  device.NodeID, DeviceLabel: device.Label, Endpoint: &ep, Details: mustJSON(state)})
			}
		}
		return entries
	case "node_removed":
		delete(r.availability, event.NodeID)
		return []api.HistoryEntry{{ObservedAt: at, Kind: "device.removed",
			Summary: "Node " + string(event.NodeID) + " removed", NodeID: event.NodeID}}
	default:
		return nil
	}
}

func gapEntry(from, to time.Time, reason string) api.HistoryEntry {
	return api.HistoryEntry{ObservedAt: to, Kind: "observation.gap", Summary: reason,
		Outcome: "unknown", Details: mustJSON(struct {
			StartedAt time.Time `json:"started_at"`
			EndedAt   time.Time `json:"ended_at"`
		}{from, to})}
}

func deviceName(device api.Device) string {
	if device.Label != "" {
		return device.Label
	}
	if device.Product != "" {
		return device.Product
	}
	return "Node " + string(device.NodeID)
}

func endpointFromPath(path string) *uint16 {
	first, _, _ := strings.Cut(path, "/")
	number, err := strconv.ParseUint(first, 10, 16)
	if err != nil {
		return nil
	}
	value := uint16(number)
	return &value
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
