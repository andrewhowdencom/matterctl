package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type occupancyKey struct {
	node     api.NodeID
	endpoint uint16
}

// Feed consumes one projected event. Inventory establishes a baseline without
// firing edge-triggered rules; later reports may produce preview plans.
func (e *Engine) Feed(ctx context.Context, event api.Event) ([]Firing, error) {
	switch event.Type {
	case "inventory":
		var inventory api.Inventory
		if err := json.Unmarshal(event.Data, &inventory); err != nil {
			return nil, fmt.Errorf("decode inventory: %w", err)
		}
		return nil, e.applyInventory(inventory)
	case "connection":
		var update struct {
			Ready bool `json:"ready"`
		}
		if err := json.Unmarshal(event.Data, &update); err != nil {
			return nil, fmt.Errorf("decode connection: %w", err)
		}
		if !update.Ready {
			e.ready = false
			e.occupancy = make(map[occupancyKey]bool)
		}
		return nil, nil
	case "node_removed":
		if _, known := e.devices[event.NodeID]; known {
			delete(e.devices, event.NodeID)
			for _, ref := range e.references {
				if ref.resolved == event.NodeID {
					e.ready = false
					return nil, fmt.Errorf("configured node %s was removed; restart the preview after updating rules", event.NodeID)
				}
			}
		}
		return nil, nil
	case "attribute_updated":
		if !e.ready {
			return nil, nil
		}
		var update struct {
			Path  string     `json:"path"`
			State *api.State `json:"state"`
		}
		if err := json.Unmarshal(event.Data, &update); err != nil {
			return nil, fmt.Errorf("decode attribute update: %w", err)
		}
		if update.State == nil || update.State.Name != "occupancy" {
			return nil, nil
		}
		parts := strings.Split(update.Path, "/")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid attribute path %q", update.Path)
		}
		endpoint, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid attribute path %q", update.Path)
		}
		var occupied bool
		if err := json.Unmarshal(update.State.Value, &occupied); err != nil {
			return nil, fmt.Errorf("invalid occupancy state: %w", err)
		}
		key := occupancyKey{node: event.NodeID, endpoint: uint16(endpoint)}
		previous, known := e.occupancy[key]
		e.occupancy[key] = occupied
		if !known || previous || !occupied {
			return nil, nil
		}
		return e.fire(ctx, Signal{Kind: "occupancy.enter", NodeID: event.NodeID, Endpoint: uint16(endpoint), ObservedAt: event.ObservedAt})
	case "node_event":
		if !e.ready {
			return nil, nil
		}
		var update struct {
			EndpointID uint16           `json:"endpoint_id"`
			ClusterID  uint32           `json:"cluster_id"`
			EventID    uint32           `json:"event_id"`
			Data       map[string]uint8 `json:"data"`
		}
		if err := json.Unmarshal(event.Data, &update); err != nil {
			return nil, fmt.Errorf("decode node event: %w", err)
		}
		if update.ClusterID != 59 {
			return nil, nil
		}
		signal := Signal{NodeID: event.NodeID, Endpoint: update.EndpointID, ObservedAt: event.ObservedAt}
		switch update.EventID {
		case 3: // Switch ShortRelease; InitialPress and completion are not clicks.
			signal.Kind = "button.short_press"
		case 6: // Switch MultiPressComplete carries the whole gesture count.
			steps := update.Data["totalNumberOfPressesCounted"]
			if steps == 0 {
				return nil, nil
			}
			signal.Kind, signal.Steps = "wheel.turn", int(steps)
		default:
			return nil, nil
		}
		return e.fire(ctx, signal)
	default:
		return nil, nil
	}
}

func (e *Engine) applyInventory(inventory api.Inventory) error {
	e.ready = false
	e.occupancy = make(map[occupancyKey]bool)
	if !inventory.Ready {
		return nil
	}
	devices := make(map[api.NodeID]api.Device, len(inventory.Devices))
	for _, device := range inventory.Devices {
		devices[device.NodeID] = device
		for _, endpoint := range device.Endpoints {
			for _, state := range endpoint.States {
				if state.Name != "occupancy" {
					continue
				}
				var occupied bool
				if json.Unmarshal(state.Value, &occupied) == nil {
					e.occupancy[occupancyKey{device.NodeID, endpoint.ID}] = occupied
				}
			}
		}
	}
	for _, ref := range e.references {
		id := ref.resolved
		if id != "" {
			if _, present := devices[id]; !present {
				return fmt.Errorf("device %q (node %s) is absent; refusing to retarget", ref.selector, id)
			}
		} else {
			var err error
			id, err = resolveDevice(ref.selector, devices)
			if err != nil {
				return err
			}
		}
		if ref.endpoint != 0 {
			found := false
			for _, endpoint := range devices[id].Endpoints {
				found = found || endpoint.ID == ref.endpoint
			}
			if !found {
				return fmt.Errorf("device %q has no endpoint %d", ref.selector, ref.endpoint)
			}
		}
		ref.resolved = id
	}
	for _, rule := range e.rules {
		t := rule.trigger
		device := devices[t.device.resolved]
		if t.kind == "wheel.turn" {
			if !hasEndpoint(device, t.up) || !hasEndpoint(device, t.down) {
				return fmt.Errorf("wheel source %q requires endpoints %d and %d", t.device.selector, t.up, t.down)
			}
		} else if !hasEndpoint(device, t.endpoint) {
			return fmt.Errorf("trigger source %q has no endpoint %d", t.device.selector, t.endpoint)
		}
	}
	e.devices = devices
	e.ready = true
	return nil
}

func hasEndpoint(device api.Device, id uint16) bool {
	for _, endpoint := range device.Endpoints {
		if endpoint.ID == id {
			return true
		}
	}
	return false
}

func resolveDevice(selector string, devices map[api.NodeID]api.Device) (api.NodeID, error) {
	if number, err := strconv.ParseUint(selector, 10, 64); err == nil {
		id := api.NodeID(strconv.FormatUint(number, 10))
		if _, ok := devices[id]; ok {
			return id, nil
		}
		return "", fmt.Errorf("node %s is absent from inventory", id)
	}
	var found api.NodeID
	for id, device := range devices {
		if device.Label != selector {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("device label %q is ambiguous", selector)
		}
		found = id
	}
	if found == "" {
		return "", fmt.Errorf("device label %q is absent from inventory", selector)
	}
	return found, nil
}

func (e *Engine) fire(ctx context.Context, signal Signal) ([]Firing, error) {
	var firings []Firing
	for index, rule := range e.rules {
		trigger := rule.trigger
		if trigger.device.resolved != signal.NodeID || trigger.kind != signal.Kind {
			continue
		}
		matched := trigger.endpoint == signal.Endpoint
		if signal.Kind == "wheel.turn" {
			matched = trigger.up == signal.Endpoint || trigger.down == signal.Endpoint
			if trigger.down == signal.Endpoint {
				signal.Steps = -positive(signal.Steps)
			} else {
				signal.Steps = positive(signal.Steps)
			}
		}
		if !matched {
			continue
		}
		actions, err := e.plan(ctx, rule.handler, signal)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", index+1, err)
		}
		firings = append(firings, Firing{Rule: index + 1, Signal: signal, Actions: actions})
	}
	return firings, nil
}

func positive(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (e *Engine) plan(ctx context.Context, handler starlark.Callable, signal Signal) ([]Action, error) {
	thread := &starlark.Thread{Name: "automation handler"}
	thread.SetMaxExecutionSteps(10_000)
	stop := context.AfterFunc(ctx, func() { thread.Cancel("handler canceled") })
	defer stop()
	value := starlarkstruct.FromStringDict(starlark.String("event"), starlark.StringDict{
		"kind": starlark.String(signal.Kind), "node_id": starlark.String(signal.NodeID),
		"endpoint": starlark.MakeInt(int(signal.Endpoint)), "steps": starlark.MakeInt(signal.Steps),
	})
	result, err := starlark.Call(thread, handler, starlark.Tuple{value}, nil)
	if err != nil {
		return nil, fmt.Errorf("evaluate handler: %w", err)
	}
	var values []starlark.Value
	switch items := result.(type) {
	case *starlark.List:
		for item := range items.Elements() {
			values = append(values, item)
		}
	case starlark.Tuple:
		values = items
	default:
		return nil, fmt.Errorf("handler must return a list or tuple of actions")
	}
	if len(values) > 32 {
		return nil, fmt.Errorf("handler returned more than 32 actions")
	}
	actions := make([]Action, 0, len(values))
	for _, value := range values {
		planned, ok := value.(*actionValue)
		if !ok {
			return nil, fmt.Errorf("handler returned %s instead of an action", value.Type())
		}
		if planned.device.resolved == "" {
			return nil, fmt.Errorf("action target %q is unresolved", planned.device.selector)
		}
		actions = append(actions, Action{Kind: planned.kind, NodeID: planned.device.resolved, Endpoint: planned.device.endpoint, Value: planned.value})
	}
	return actions, nil
}
