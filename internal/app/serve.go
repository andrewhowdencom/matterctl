// Package app composes the application adapters and their lifetimes.
package app

import (
	"context"
	"log/slog"

	"github.com/andrewhowdencom/matterctl/internal/matter"
	"github.com/andrewhowdencom/matterctl/internal/server"
)

func Serve(ctx context.Context, httpAddress, matterURL string, logger *slog.Logger) error {
	upstream, err := matter.New(matterURL, logger)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		upstream.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()
	return server.Run(ctx, httpAddress, logger, server.WithMatter(upstream))
}
