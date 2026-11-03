package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func powerCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "power", Short: "Set and confirm a device endpoint's on/off state"}
	for _, action := range []string{"on", "off", "toggle"} {
		var endpoint uint16
		var timeout time.Duration
		action := action
		child := &cobra.Command{
			Use: action + " <target>", Short: "Turn a device " + action + " and confirm its reported state", Args: cobra.ExactArgs(1),
			Example: "  matterctl device power " + action + " 1\n  matterctl device power " + action + " 1 --endpoint 1",
			RunE: func(cmd *cobra.Command, args []string) error {
				if timeout < time.Second || timeout > 30*time.Second {
					return errors.New("power timeout must be between 1s and 30s")
				}
				service, format, err := configuredClient(config)
				if err != nil {
					return err
				}
				request := api.PowerRequest{Target: args[0], Action: action, TimeoutMS: timeout.Milliseconds()}
				if cmd.Flags().Changed("endpoint") {
					request.Endpoint = &endpoint
				}
				result, err := service.Power(cmd.Context(), request)
				if err != nil {
					return err
				}
				if format == "json" {
					return printJSON(cmd.OutOrStdout(), result)
				}
				state := "off"
				if result.Power {
					state = "on"
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Node %s endpoint %d: %s (confirmed at %s)\n", result.NodeID, result.Endpoint, state, result.ConfirmedAt.Format(time.RFC3339))
				return err
			},
		}
		child.Flags().Uint16Var(&endpoint, "endpoint", 0, "Endpoint ID (required when multiple endpoints support power)")
		child.Flags().DurationVar(&timeout, "timeout", api.PowerTimeout, "Overall command and confirmation deadline (1s to 30s)")
		command.AddCommand(child)
	}
	return command
}
