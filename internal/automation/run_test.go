package automation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type automationSource struct{ events <-chan api.Event }

func (s automationSource) Watch(context.Context, string) (<-chan api.Event, error) {
	return s.events, nil
}

func TestRunContinuesRoomAfterOneLightFails(t *testing.T) {
	source, err := os.ReadFile("../../examples/bilresa-scroll-wheel-lights.star")
	if err != nil {
		t.Fatal(err)
	}
	source = append(source, []byte("\nbind_room(\"30\", [(\"31\", 1), (\"32\", 1)])\n")...)
	engine, err := Load(t.Context(), "rooms.star", source)
	if err != nil {
		t.Fatal(err)
	}
	switchEndpoints := []api.Endpoint{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}, {ID: 6}, {ID: 7}, {ID: 8}, {ID: 9}}
	inventory := api.Inventory{Ready: true, Devices: []api.Device{
		{NodeID: "2", Label: "Den Scroll Wheel", Endpoints: switchEndpoints},
		{NodeID: "1", Label: "Den Light", Endpoints: []api.Endpoint{{ID: 1}}},
		{NodeID: "30", Endpoints: switchEndpoints},
		{NodeID: "31", Endpoints: []api.Endpoint{{ID: 1}}},
		{NodeID: "32", Endpoints: []api.Endpoint{{ID: 1}}},
	}}
	stream := make(chan api.Event, 2)
	stream <- sampleEvent("inventory", "", inventory)
	stream <- sampleEvent("node_event", "30", struct {
		EndpointID uint16 `json:"endpoint_id"`
		ClusterID  uint32 `json:"cluster_id"`
		EventID    uint32 `json:"event_id"`
	}{7, 59, 3})
	close(stream)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var targets []api.NodeID
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, automationSource{stream}, automationExecutor(func(_ context.Context, action Action) error {
			targets = append(targets, action.NodeID)
			if len(targets) == 2 {
				cancel()
			}
			if action.NodeID == "31" {
				return errors.New("light unavailable")
			}
			return nil
		}), []Program{{Filename: "rooms.star", Engine: engine}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("automation runner did not continue to the second light")
	}
	if len(targets) != 2 || targets[0] != "31" || targets[1] != "32" {
		t.Fatalf("targets = %v", targets)
	}
}
func (s automationSource) Inventory(context.Context) (api.Inventory, error) {
	return api.Inventory{}, nil
}
func (s automationSource) Inspect(context.Context, string) (api.Device, error) {
	return api.Device{}, nil
}

type automationExecutor func(context.Context, Action) error

func (f automationExecutor) Execute(ctx context.Context, action Action) error { return f(ctx, action) }

func TestRunExecutesOrderedActionsOnlyAfterPresenceEdge(t *testing.T) {
	engine, err := Load(t.Context(), "rules.star", []byte(exampleRules))
	if err != nil {
		t.Fatal(err)
	}
	stream := make(chan api.Event, 3)
	stream <- sampleInventory(false)
	stream <- occupancyUpdate(true)
	close(stream)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var kinds []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, automationSource{stream}, automationExecutor(func(_ context.Context, action Action) error {
			kinds = append(kinds, action.Kind)
			if len(kinds) == 3 {
				cancel()
			}
			return nil
		}), []Program{{Filename: "rules.star", Engine: engine}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("automation runner did not execute the presence plan")
	}
	if len(kinds) != 3 || kinds[0] != "power" || kinds[1] != "brightness" || kinds[2] != "colour_temperature" {
		t.Fatalf("wrong action order: %v", kinds)
	}
}
