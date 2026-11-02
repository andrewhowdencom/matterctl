package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/client"
)

func serverCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "server", Short: "Inspect the upstream Matter Server connection"}
	command.AddCommand(&cobra.Command{
		Use:     "info",
		Short:   "Show Matter Server connection, version, and capabilities",
		Args:    cobra.NoArgs,
		Example: "  matterctl server info\n  matterctl server info --output json\n  matterctl server info --server.url http://192.168.86.33:8081",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format := config.GetString("output")
			if format != "text" && format != "json" {
				return errors.New("output must be text or json")
			}
			timeout := config.GetDuration("request.timeout")
			if timeout <= 0 {
				return errors.New("request.timeout must be a positive duration")
			}
			service, err := client.New(config.GetString("server.url"), client.WithTimeout(timeout))
			if err != nil {
				return err
			}
			status, requestErr := service.ServerInfo(cmd.Context())
			if status.Connection.State != "" {
				if err := printServerInfo(cmd.OutOrStdout(), status, format); err != nil {
					return err
				}
			}
			return requestErr
		},
	})
	return command
}

func printServerInfo(output io.Writer, status api.ServerStatus, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(w, "Connection:\t%s\n", status.Connection.State); err != nil {
		return err
	}
	if status.Connection.ConnectedAt != nil {
		if _, err := fmt.Fprintf(w, "Connected at:\t%s\n", status.Connection.ConnectedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if info := status.Server; info != nil {
		if _, err := fmt.Fprintf(w, "Matter Server:\t%s\nAPI schema:\t%d (minimum %d)\nBluetooth enabled:\t%t\nWi-Fi credentials configured:\t%t\nThread credentials configured:\t%t\n",
			info.SDKVersion, info.SchemaVersion, info.MinSupportedSchemaVersion, info.BluetoothEnabled, info.WiFiCredentialsSet, info.ThreadCredentialsSet); err != nil {
			return err
		}
	}
	if status.Connection.LastError != "" {
		if _, err := fmt.Fprintf(w, "Connection error:\t%s\n", status.Connection.LastError); err != nil {
			return err
		}
	}
	if status.Error != "" {
		if _, err := fmt.Fprintf(w, "Request error:\t%s\n", status.Error); err != nil {
			return err
		}
	}
	return w.Flush()
}
