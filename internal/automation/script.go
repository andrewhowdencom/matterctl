package automation

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type rule struct {
	trigger *trigger
	handler starlark.Callable
}

// Engine evaluates a Starlark rule file and previews its action plans.
type Engine struct {
	rules       []rule
	references  []*deviceRef
	registering bool
	ready       bool
	devices     map[api.NodeID]api.Device
	occupancy   map[occupancyKey]bool
}

func (e *Engine) RuleCount() int { return len(e.rules) }

// Load executes registration calls. A failed file cannot leave active rules.
func Load(ctx context.Context, filename string, source []byte) (*Engine, error) {
	if len(source) == 0 || len(source) > 1<<20 {
		return nil, fmt.Errorf("rule file must contain 1 byte to 1 MiB")
	}
	e := &Engine{devices: make(map[api.NodeID]api.Device), occupancy: make(map[occupancyKey]bool), registering: true}
	predeclared := starlark.StringDict{
		"device":             starlark.NewBuiltin("device", e.device),
		"occupied":           starlark.NewBuiltin("occupied", triggerBuiltin("occupancy.enter")),
		"short_press":        starlark.NewBuiltin("short_press", triggerBuiltin("button.short_press")),
		"wheel":              starlark.NewBuiltin("wheel", triggerBuiltin("wheel.turn")),
		"on":                 starlark.NewBuiltin("on", e.on),
		"power":              starlark.NewBuiltin("power", actionBuiltin("power")),
		"toggle":             starlark.NewBuiltin("toggle", actionBuiltin("toggle")),
		"brightness":         starlark.NewBuiltin("brightness", actionBuiltin("brightness")),
		"adjust_brightness":  starlark.NewBuiltin("adjust_brightness", actionBuiltin("adjust_brightness")),
		"colour_temperature": starlark.NewBuiltin("colour_temperature", actionBuiltin("colour_temperature")),
	}
	thread := &starlark.Thread{Name: "automation registration"}
	thread.SetMaxExecutionSteps(100_000)
	stop := context.AfterFunc(ctx, func() { thread.Cancel("registration canceled") })
	defer stop()
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, filename, source, predeclared); err != nil {
		return nil, fmt.Errorf("load automation rules: %w", err)
	}
	e.registering = false
	if len(e.rules) == 0 {
		return nil, fmt.Errorf("rule file did not register any handlers")
	}
	return e, nil
}

func (e *Engine) device(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !e.registering {
		return nil, fmt.Errorf("device() is only available while loading rules")
	}
	var selector string
	var endpoint int
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "selector", &selector, "endpoint?", &endpoint); err != nil {
		return nil, err
	}
	if strings.TrimSpace(selector) != selector || selector == "" || endpoint < 0 || endpoint > 65535 {
		return nil, fmt.Errorf("device requires a nonempty exact label or node ID and an optional endpoint from 1 to 65535")
	}
	ref := &deviceRef{selector: selector, endpoint: uint16(endpoint)}
	e.references = append(e.references, ref)
	return ref, nil
}

func triggerBuiltin(kind string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires a device", fn.Name())
		}
		ref, ok := args[0].(*deviceRef)
		if !ok {
			return nil, fmt.Errorf("%s requires a device() value", fn.Name())
		}
		var endpoint, up, down int
		if kind == "wheel.turn" {
			if err := starlark.UnpackArgs(fn.Name(), nil, kwargs, "up", &up, "down", &down); err != nil {
				return nil, err
			}
			if up < 1 || up > 65535 || down < 1 || down > 65535 || up == down {
				return nil, fmt.Errorf("wheel requires two distinct endpoint IDs")
			}
			return &trigger{kind: kind, device: ref, up: uint16(up), down: uint16(down)}, nil
		}
		if err := starlark.UnpackArgs(fn.Name(), nil, kwargs, "endpoint", &endpoint); err != nil {
			return nil, err
		}
		if endpoint < 1 || endpoint > 65535 {
			return nil, fmt.Errorf("%s requires an endpoint from 1 to 65535", fn.Name())
		}
		return &trigger{kind: kind, device: ref, endpoint: uint16(endpoint)}, nil
	}
}

func (e *Engine) on(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !e.registering {
		return nil, fmt.Errorf("on() is only available while loading rules")
	}
	if len(kwargs) != 0 || len(args) != 2 {
		return nil, fmt.Errorf("%s requires a trigger and a handler", fn.Name())
	}
	trigger, ok := args[0].(*trigger)
	if !ok {
		return nil, fmt.Errorf("on requires a trigger")
	}
	handler, ok := args[1].(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("on requires a function")
	}
	if len(e.rules) >= 256 {
		return nil, fmt.Errorf("too many rules")
	}
	e.rules = append(e.rules, rule{trigger: trigger, handler: handler})
	return starlark.None, nil
}

func actionBuiltin(kind string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		want := 2
		if kind == "toggle" {
			want = 1
		}
		if len(kwargs) != 0 || len(args) != want {
			if want == 1 {
				return nil, fmt.Errorf("%s requires a device", fn.Name())
			}
			return nil, fmt.Errorf("%s requires a device and a value", fn.Name())
		}
		ref, ok := args[0].(*deviceRef)
		if !ok || ref.endpoint == 0 {
			return nil, fmt.Errorf("%s requires device() with an endpoint", fn.Name())
		}
		if kind == "toggle" {
			return &actionValue{kind: kind, device: ref}, nil
		}
		if kind == "power" {
			mode, ok := args[1].(starlark.String)
			if !ok || (string(mode) != "on" && string(mode) != "off") {
				return nil, fmt.Errorf("power mode must be on or off")
			}
			return &actionValue{kind: kind, device: ref, value: string(mode)}, nil
		}
		value, ok := args[1].(starlark.Int)
		if !ok {
			return nil, fmt.Errorf("%s requires an integer value", fn.Name())
		}
		number, ok := value.Int64()
		if !ok {
			return nil, fmt.Errorf("%s value is out of range", fn.Name())
		}
		switch kind {
		case "brightness":
			if number < 1 || number > 100 {
				return nil, fmt.Errorf("brightness must be between 1 and 100 percent; use power off for zero")
			}
		case "adjust_brightness":
			if number < -10000 || number > 10000 {
				return nil, fmt.Errorf("brightness adjustment must be between -10000 and 10000 percentage points")
			}
		case "colour_temperature":
			if number < 1000 || number > 10000 {
				return nil, fmt.Errorf("colour temperature must be between 1000 and 10000 kelvin")
			}
		}
		return &actionValue{kind: kind, device: ref, value: number}, nil
	}
}
