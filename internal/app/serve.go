// Package app composes the application adapters and their lifetimes.
package app

import (
	"context"
	"log/slog"

	"github.com/andrewhowdencom/matterctl/internal/automation"
	"github.com/andrewhowdencom/matterctl/internal/matter"
	"github.com/andrewhowdencom/matterctl/internal/server"
)

func Serve(ctx context.Context, httpAddress, matterURL, automationDirectory string, logger *slog.Logger) error {
	upstream, err := matter.New(matterURL, logger)
	if err != nil {
		return err
	}
	programs, err := automation.LoadDirectory(ctx, automationDirectory)
	if err != nil {
		logger.Error("automations disabled", "directory", automationDirectory, "error", err)
		programs = nil
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	automationDone := make(chan struct{})
	go func() {
		defer close(done)
		upstream.Run(ctx)
	}()
	go func() {
		defer close(automationDone)
		automation.Run(ctx, upstream, upstream, programs, logger)
	}()
	defer func() {
		cancel()
		<-automationDone
		<-done
	}()
	return server.Run(ctx, httpAddress, logger, server.WithMatter(upstream), server.WithDevices(upstream), server.WithCommissioning(upstream), server.WithPower(upstream), server.WithLabels(upstream))
}
