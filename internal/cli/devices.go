package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/client"
)

func configuredClient(config *viper.Viper) (*client.Client, string, error) {
	format := config.GetString("output")
	if format != "text" && format != "json" {
		return nil, "", fmt.Errorf("output must be text or json")
	}
	service, err := client.New(config.GetString("server.url"), client.WithTimeout(config.GetDuration("request.timeout")))
	return service, format, err
}

func deviceCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "device", Short: "Discover, commission, and inspect Matter devices"}
	command.AddCommand(discoverCommand(config), commissionCommand(config))
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "List devices and their endpoint capabilities", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			inventory, requestErr := service.Inventory(cmd.Context())
			if inventory.Connection.State != "" {
				if format == "json" {
					err = printJSON(cmd.OutOrStdout(), inventory)
				} else {
					err = printInventory(cmd.OutOrStdout(), inventory)
				}
				if err != nil {
					return err
				}
			}
			return requestErr
		},
	}, &cobra.Command{
		Use: "inspect <target>", Short: "Inspect a node ID or an unambiguous Matter label", Args: cobra.ExactArgs(1),
		Example: "  matterctl device inspect 12\n  matterctl device inspect \"Couch lamp\" --output json",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			device, err := service.Inspect(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), device)
			}
			return printDevice(cmd.OutOrStdout(), device)
		},
	})
	return command
}

func printJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printInventory(output io.Writer, inventory api.Inventory) error {
	if _, err := fmt.Fprintf(output, "Connection: %s; inventory ready: %t\n", inventory.Connection.State, inventory.Ready); err != nil {
		return err
	}
	if inventory.ObservedAt != nil {
		if _, err := fmt.Fprintf(output, "Inventory observed at: %s\n", inventory.ObservedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if len(inventory.Devices) == 0 {
		_, err := fmt.Fprintln(output, "No devices in the current snapshot.")
		return err
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NODE\tLABEL\tAVAILABLE\tENDPOINT\tENDPOINT LABEL\tCAPABILITIES\tPOWER"); err != nil {
		return err
	}
	for _, device := range inventory.Devices {
		if len(device.Endpoints) == 0 {
			if _, err := fmt.Fprintf(w, "%s\t%q\t%t\t?\t\tunknown\t\n", device.NodeID, device.Label, device.Available); err != nil {
				return err
			}
		}
		for _, endpoint := range device.Endpoints {
			capabilities := strings.Join(endpoint.Capabilities, ",")
			if capabilities == "" {
				capabilities = "not advertised"
			}
			power := "unknown"
			if endpoint.Power != nil {
				if *endpoint.Power {
					power = "on"
				} else {
					power = "off"
				}
			}
			if _, err := fmt.Fprintf(w, "%s\t%q\t%t\t%d\t%q\t%s\t%s\n", device.NodeID, device.Label, device.Available, endpoint.ID, endpoint.Label, capabilities, power); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

func printDevice(output io.Writer, device api.Device) error {
	if _, err := fmt.Fprintf(output, "Node: %s\nLabel: %q\nVendor: %q\nProduct: %q\nAvailable: %t\nBridge: %t\nObserved at: %s\n", device.NodeID, device.Label, device.Vendor, device.Product, device.Available, device.IsBridge, device.ObservedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	for _, endpoint := range device.Endpoints {
		if _, err := fmt.Fprintf(output, "\nEndpoint %d %q (descriptor complete: %t)\n", endpoint.ID, endpoint.Label, endpoint.DescriptorComplete); err != nil {
			return err
		}
		if endpoint.ParentID != nil {
			if _, err := fmt.Fprintf(output, "  Parent: %d\n", *endpoint.ParentID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(output, "  Device types: %v\n  Capabilities: %s\n", endpoint.DeviceTypes, strings.Join(endpoint.Capabilities, ", ")); err != nil {
			return err
		}
		if endpoint.Power != nil {
			if _, err := fmt.Fprintf(output, "  Power: %t\n", *endpoint.Power); err != nil {
				return err
			}
		}
		if endpoint.Level != nil {
			if _, err := fmt.Fprintf(output, "  Level: %d\n", *endpoint.Level); err != nil {
				return err
			}
		}
		if endpoint.SwitchPosition != nil {
			if _, err := fmt.Fprintf(output, "  Switch position: %d\n", *endpoint.SwitchPosition); err != nil {
				return err
			}
		}
		for _, role := range []struct {
			name     string
			clusters []api.Cluster
		}{{"Server", endpoint.ServerClusters}, {"Client", endpoint.ClientClusters}} {
			for _, cluster := range role.clusters {
				if _, err := fmt.Fprintf(output, "  %s cluster %d %s\n", role.name, cluster.ID, cluster.Name); err != nil {
					return err
				}
				if role.name == "Server" {
					commands := "unknown"
					if cluster.AcceptedCommands != nil {
						encoded, err := json.Marshal(cluster.AcceptedCommands)
						if err != nil {
							return err
						}
						commands = string(encoded)
					}
					if _, err := fmt.Fprintf(output, "    Accepted commands: %s\n    Attributes: %v\n    Events: %v\n", commands, cluster.Attributes, cluster.Events); err != nil {
						return err
					}
					if cluster.FeatureMap != nil {
						if _, err := fmt.Fprintf(output, "    Features: %s\n", cluster.FeatureMap); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
