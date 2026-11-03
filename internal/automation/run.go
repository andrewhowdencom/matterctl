package automation

import (
	"context"
	"log/slog"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

// Executor is the device command boundary. A failed action is never retried.
type Executor interface {
	Execute(context.Context, Action) error
}

// Run watches projected events and executes each matching rule's ordered actions.
// An interrupted stream starts again from a fresh inventory baseline.
func Run(ctx context.Context, source api.DeviceSource, executor Executor, programs []Program, logger *slog.Logger) {
	if len(programs) == 0 {
		return
	}
	for ctx.Err() == nil {
		stream, err := source.Watch(ctx, "")
		if err == nil {
			logger.Info("automation stream connected", "programs", len(programs))
			for event := range stream {
				if ctx.Err() != nil {
					return
				}
				if event.Type == "node_added" || event.Type == "endpoint_added" {
					if inventory, refreshErr := source.Inventory(ctx); refreshErr == nil {
						for _, program := range programs {
							if err := program.Engine.applyInventory(inventory); err != nil {
								logger.Warn("automation inventory unavailable", "file", program.Filename, "error", err)
							}
						}
					}
				}
				for _, program := range programs {
					firings, feedErr := program.Engine.Feed(ctx, event)
					if feedErr != nil {
						logger.Warn("automation event rejected", "file", program.Filename, "event", event.Type, "error", feedErr)
						continue
					}
					for _, firing := range firings {
						for _, action := range firing.Actions {
							if ctx.Err() != nil {
								return
							}
							if err := executor.Execute(ctx, action); err != nil {
								logger.Error("automation action failed", "file", program.Filename, "rule", firing.Rule, "signal", firing.Signal.Kind, "action", action.Kind, "node", action.NodeID, "endpoint", action.Endpoint, "error", err)
								break
							}
							logger.Info("automation action confirmed", "file", program.Filename, "rule", firing.Rule, "signal", firing.Signal.Kind, "action", action.Kind, "node", action.NodeID, "endpoint", action.Endpoint)
						}
					}
				}
			}
			if ctx.Err() != nil {
				return
			}
			err = api.ErrUnavailable
		}
		logger.Warn("automation stream interrupted; resubscribing", "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
