package automation

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type automationSource struct{ events <-chan api.Event }

func (s automationSource) Watch(context.Context, string) (<-chan api.Event, error) {
	return s.events, nil
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
