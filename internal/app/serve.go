// Package app composes the application adapters and their lifetimes.
package app

import (
	"context"
	"log/slog"

	"github.com/andrewhowdencom/matterctl/internal/automation"
	"github.com/andrewhowdencom/matterctl/internal/history"
	"github.com/andrewhowdencom/matterctl/internal/matter"
	"github.com/andrewhowdencom/matterctl/internal/server"
)

func Serve(ctx context.Context, httpAddress, matterURL, automationDirectory, historyPath string, logger *slog.Logger) error {
	store, err := history.Open(historyPath)
	historyErr := err
	var recorder *history.Recorder
	if err != nil {
		logger.Error("history unavailable; automations disabled until pause state can be read", "error", err)
		recorder = history.NewUnavailableRecorder(err, logger)
	} else {
		defer func() { _ = store.Close() }()
		recorder = history.NewRecorder(store, logger)
	}
	upstream, err := matter.New(matterURL, logger, matter.WithObserver(recorder.Observe))
	if err != nil {
		return err
	}
	var manager *automation.Manager
	if store == nil {
		manager = automation.DisabledManager(historyErr)
	} else {
		manager, err = automation.NewManager(ctx, automationDirectory, store, recorder, logger)
		if err != nil {
			logger.Error("automations disabled", "directory", automationDirectory, "error", err)
			manager = automation.DisabledManager(err)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	historyCtx, stopHistory := context.WithCancel(context.Background())
	done := make(chan struct{})
	automationDone := make(chan struct{})
	historyDone := make(chan struct{})
	go func() {
		defer close(historyDone)
		recorder.Run(historyCtx)
	}()
	go func() {
		defer close(done)
		upstream.Run(ctx)
	}()
	go func() {
		defer close(automationDone)
		manager.Run(ctx, upstream, upstream)
	}()
	defer func() {
		cancel()
		<-automationDone
		<-done
		stopHistory()
		<-historyDone
	}()
	return server.Run(ctx, httpAddress, logger, server.WithMatter(upstream), server.WithDevices(upstream), server.WithCommissioning(upstream), server.WithPower(upstream), server.WithLight(upstream), server.WithLabels(upstream), server.WithHistory(recorder), server.WithAutomations(manager))
}
