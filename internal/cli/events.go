package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func eventCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "event", Short: "Observe inventory and Matter device events"}
	var target string
	watch := &cobra.Command{
		Use: "watch", Short: "Watch live changes until interrupted", Args: cobra.NoArgs,
		Example: "  matterctl event watch\n  matterctl event watch --device \"Couch lamp\" --output json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			err = service.Watch(cmd.Context(), target, func(event api.Event) error {
				if format == "json" {
					return encoder.Encode(event)
				}
				if event.Type == "heartbeat" {
					return nil
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s node=%s %s\n", event.ObservedAt.Format(time.RFC3339), event.Type, event.NodeID, event.Data)
				return err
			})
			if errors.Is(cmd.Context().Err(), context.Canceled) {
				return nil
			}
			return err
		},
	}
	watch.Flags().StringVar(&target, "device", "", "Filter by node ID or an unambiguous Matter label")
	command.AddCommand(watch)
	return command
}
