package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func labelCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "label", Short: "Write a Matter NodeLabel on a device or bridged endpoint"}
	var endpoint uint16
	var timeout time.Duration
	set := &cobra.Command{
		Use: "set <target> <label>", Short: "Write and confirm the device's Matter label", Args: cobra.ExactArgs(2),
		Example: "  matterctl device label set 1 \"Couch lamp\"\n  matterctl device label set 2 \"Desk lamp\" --endpoint 3",
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout < time.Second || timeout > 30*time.Second {
				return errors.New("label timeout must be between 1s and 30s")
			}
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			request := api.LabelRequest{Target: args[0], Label: args[1], TimeoutMS: timeout.Milliseconds()}
			if cmd.Flags().Changed("endpoint") {
				request.Endpoint = &endpoint
			}
			result, err := service.SetLabel(cmd.Context(), request)
			if err != nil {
				return err
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Node %s endpoint %d: label %q (confirmed at %s)\n", result.NodeID, result.Endpoint, result.Label, result.ConfirmedAt.Format(time.RFC3339))
			return err
		},
	}
	set.Flags().Uint16Var(&endpoint, "endpoint", 0, "Bridged endpoint ID (omit for the node label)")
	set.Flags().DurationVar(&timeout, "timeout", api.LabelTimeout, "Overall write and confirmation deadline (1s to 30s)")
	command.AddCommand(set)
	return command
}
