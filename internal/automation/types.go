package automation

import (
	"fmt"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"go.starlark.net/starlark"
)

// Signal is a device event after Matter-specific details have been interpreted.
type Signal struct {
	Kind       string     `json:"kind"`
	NodeID     api.NodeID `json:"node_id"`
	Endpoint   uint16     `json:"endpoint"`
	Steps      int        `json:"steps,omitempty"`
	ObservedAt time.Time  `json:"observed_at"`
}

// Action describes a requested operation. This prototype never executes it.
type Action struct {
	Kind     string     `json:"kind"`
	NodeID   api.NodeID `json:"node_id"`
	Endpoint uint16     `json:"endpoint"`
	Value    any        `json:"value,omitempty"`
}

type Firing struct {
	Rule    int      `json:"rule"`
	Signal  Signal   `json:"signal"`
	Actions []Action `json:"actions"`
}

type deviceRef struct {
	selector string
	endpoint uint16
	resolved api.NodeID
}

func (r *deviceRef) String() string        { return fmt.Sprintf("device(%q)", r.selector) }
func (r *deviceRef) Type() string          { return "device" }
func (r *deviceRef) Freeze()               {}
func (r *deviceRef) Truth() starlark.Bool  { return starlark.True }
func (r *deviceRef) Hash() (uint32, error) { return 0, fmt.Errorf("device is not hashable") }

type trigger struct {
	kind     string
	device   *deviceRef
	endpoint uint16
	up, down uint16
}

func (t *trigger) String() string        { return t.kind }
func (t *trigger) Type() string          { return "trigger" }
func (t *trigger) Freeze()               {}
func (t *trigger) Truth() starlark.Bool  { return starlark.True }
func (t *trigger) Hash() (uint32, error) { return 0, fmt.Errorf("trigger is not hashable") }

type actionValue struct {
	kind   string
	device *deviceRef
	value  any
}

func (a *actionValue) String() string        { return a.kind }
func (a *actionValue) Type() string          { return "action" }
func (a *actionValue) Freeze()               {}
func (a *actionValue) Truth() starlark.Bool  { return starlark.True }
func (a *actionValue) Hash() (uint32, error) { return 0, fmt.Errorf("action is not hashable") }
