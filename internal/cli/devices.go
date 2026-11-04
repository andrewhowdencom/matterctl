package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
	command.AddCommand(discoverCommand(config), commissionCommand(config), powerCommand(config), labelCommand(config))
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
	})
	var verbose bool
	inspect := &cobra.Command{
		Use: "inspect <target>", Short: "Inspect a node ID or an unambiguous Matter label", Args: cobra.ExactArgs(1),
		Example: "  matterctl device inspect 12\n  matterctl device inspect \"Couch lamp\" --verbose\n  matterctl device inspect \"Couch lamp\" --output json",
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
			return printDevice(cmd.OutOrStdout(), device, verbose)
		},
	}
	inspect.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show per-cluster commands, attributes, events, and features in text output")
	command.AddCommand(inspect)
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
	if _, err := fmt.Fprintln(w, "NODE\tLABEL\tAVAILABLE\tENDPOINT\tENDPOINT LABEL\tDEVICE TYPE\tCAPABILITIES\tPOWER"); err != nil {
		return err
	}
	for _, device := range inventory.Devices {
		if len(device.Endpoints) == 0 {
			if _, err := fmt.Fprintf(w, "%s\t%q\t%t\t?\t\tunknown\tunknown\t\n", device.NodeID, device.Label, device.Available); err != nil {
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
			if _, err := fmt.Fprintf(w, "%s\t%q\t%t\t%d\t%q\t%s\t%s\t%s\n", device.NodeID, device.Label, device.Available, endpoint.ID, endpoint.Label, deviceTypeSummary(endpoint.DeviceTypes), capabilities, power); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

func printDevice(output io.Writer, device api.Device, verbose bool) error {
	availability := "unavailable"
	if device.Available {
		availability = "available"
	}
	name := ""
	if device.Label != "" {
		name = " " + fmt.Sprintf("%q", device.Label)
	}
	if _, err := fmt.Fprintf(output, "Node %s%s (%s)\n", device.NodeID, name, availability); err != nil {
		return err
	}
	if device.Vendor != "" || device.Product != "" {
		if _, err := fmt.Fprintf(output, "  Product: %s\n", strings.Join(nonempty(device.Vendor, device.Product), " / ")); err != nil {
			return err
		}
	}
	if device.IsBridge {
		if _, err := fmt.Fprintln(output, "  Bridge: yes"); err != nil {
			return err
		}
	}
	if !device.ObservedAt.IsZero() {
		if _, err := fmt.Fprintf(output, "  Observed: %s\n", device.ObservedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if len(device.Endpoints) == 0 {
		_, err := fmt.Fprintln(output, "  No endpoints observed.")
		return err
	}
	for _, endpoint := range device.Endpoints {
		label := ""
		if endpoint.Label != "" {
			label = " " + fmt.Sprintf("%q", endpoint.Label)
		}
		if _, err := fmt.Fprintf(output, "\nEndpoint %d%s — %s\n", endpoint.ID, label, deviceTypeSummary(endpoint.DeviceTypes)); err != nil {
			return err
		}
		if endpoint.ParentID != nil {
			if _, err := fmt.Fprintf(output, "  Parent: %d\n", *endpoint.ParentID); err != nil {
				return err
			}
		}
		if !endpoint.DescriptorComplete {
			if _, err := fmt.Fprintln(output, "  Descriptor: incomplete"); err != nil {
				return err
			}
		}
		states := endpointStateSummary(endpoint)
		if _, err := fmt.Fprintf(output, "  State: %s\n  Power actions: %s\n  Serves: %s\n  Uses: %s\n", states, powerActionSummary(endpoint), clusterSummary(endpoint.ServerClusters, endpoint.DescriptorComplete), clusterSummary(endpoint.ClientClusters, endpoint.DescriptorComplete)); err != nil {
			return err
		}
		if verbose {
			if _, err := fmt.Fprintf(output, "  Device types: %s\n", deviceTypeDetails(endpoint.DeviceTypes)); err != nil {
				return err
			}
			if err := printClusterDetails(output, "Server", endpoint.ServerClusters); err != nil {
				return err
			}
			if err := printClusterDetails(output, "Client", endpoint.ClientClusters); err != nil {
				return err
			}
		}
	}
	return nil
}

func nonempty(values ...string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func powerActionSummary(endpoint api.Endpoint) string {
	if len(endpoint.Capabilities) != 0 {
		items := make([]string, 0, len(endpoint.Capabilities))
		for _, capability := range endpoint.Capabilities {
			items = append(items, strings.TrimPrefix(capability, "power."))
		}
		return strings.Join(items, ", ")
	}
	if !endpoint.DescriptorComplete {
		return "unknown"
	}
	for _, cluster := range endpoint.ServerClusters {
		if cluster.ID == 6 && cluster.AcceptedCommands == nil {
			return "unknown"
		}
	}
	return "none advertised"
}

func endpointStateSummary(endpoint api.Endpoint) string {
	states := make([]string, 0, len(endpoint.States))
	for _, state := range endpoint.States {
		states = append(states, state.Name+"="+state.Text)
	}
	if len(states) == 0 { // Older servers may only supply the selected state fields.
		if endpoint.Power != nil {
			power := "off"
			if *endpoint.Power {
				power = "on"
			}
			states = append(states, "power="+power)
		}
		if endpoint.Level != nil {
			states = append(states, fmt.Sprintf("level=%d", *endpoint.Level))
		}
		if endpoint.SwitchPosition != nil {
			states = append(states, fmt.Sprintf("switch_position=%d", *endpoint.SwitchPosition))
		}
	}
	if len(states) == 0 {
		return "unknown"
	}
	return strings.Join(states, ", ")
}

func clusterSummary(clusters []api.Cluster, complete bool) string {
	if len(clusters) == 0 {
		if !complete {
			return "unknown"
		}
		return "none advertised"
	}
	items := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.Name != "" {
			items = append(items, cluster.Name)
		} else {
			items = append(items, fmt.Sprint(cluster.ID))
		}
	}
	return strings.Join(items, ", ")
}

func clusterNameAndID(cluster api.Cluster) string {
	if cluster.Name == "" {
		return fmt.Sprint(cluster.ID)
	}
	return fmt.Sprintf("%s (%d)", cluster.Name, cluster.ID)
}

func printClusterDetails(output io.Writer, role string, clusters []api.Cluster) error {
	if len(clusters) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "  %s clusters:\n", role); err != nil {
		return err
	}
	for _, cluster := range clusters {
		if _, err := fmt.Fprintf(output, "    %s\n", clusterNameAndID(cluster)); err != nil {
			return err
		}
		if role == "Client" {
			continue
		}
		commands := "unknown"
		if cluster.AcceptedCommands != nil {
			commands = "none"
			if len(cluster.AcceptedCommands) != 0 {
				items := make([]string, 0, len(cluster.AcceptedCommands))
				for _, command := range cluster.AcceptedCommands {
					if command.Name == "" {
						items = append(items, fmt.Sprint(command.ID))
					} else {
						items = append(items, fmt.Sprintf("%s (%d)", command.Name, command.ID))
					}
				}
				commands = strings.Join(items, ", ")
			}
		}
		if _, err := fmt.Fprintf(output, "      Commands: %s\n      Attributes: %s\n      Events: %s\n", commands, namedIDs(cluster.Attributes, cluster.AttributeNames), namedIDs(cluster.Events, cluster.EventNames)); err != nil {
			return err
		}
		if cluster.FeatureMap != nil {
			if _, err := fmt.Fprintf(output, "      Features: %s\n", featureSummary(cluster.FeatureMap)); err != nil {
				return err
			}
		}
	}
	return nil
}

func featureSummary(raw json.RawMessage) string {
	var bits uint32
	if json.Unmarshal(raw, &bits) == nil {
		return fmt.Sprintf("0x%X", bits)
	}
	var features map[string]bool
	if json.Unmarshal(raw, &features) == nil {
		keys := make([]string, 0, len(features))
		for key := range features {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]string, 0, len(keys))
		for _, key := range keys {
			items = append(items, fmt.Sprintf("%s=%t", key, features[key]))
		}
		if len(items) == 0 {
			return "none"
		}
		return strings.Join(items, ", ")
	}
	return "unknown"
}

func namedIDs(ids []uint32, names map[uint32]string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := names[id]; name != "" {
			items = append(items, fmt.Sprintf("%d (%s)", id, name))
		} else {
			items = append(items, fmt.Sprint(id))
		}
	}
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func deviceTypeSummary(types []api.DeviceType) string {
	if len(types) == 0 {
		return "unknown"
	}
	names := make([]string, 0, len(types))
	for _, deviceType := range types {
		if deviceType.Name != "" {
			names = append(names, deviceType.Name)
		} else {
			names = append(names, fmt.Sprint(deviceType.ID))
		}
	}
	return strings.Join(names, ", ")
}

func deviceTypeDetails(types []api.DeviceType) string {
	if len(types) == 0 {
		return "unknown"
	}
	parts := make([]string, 0, len(types))
	for _, deviceType := range types {
		if deviceType.Name != "" {
			parts = append(parts, fmt.Sprintf("%s (%d, revision %d)", deviceType.Name, deviceType.ID, deviceType.Revision))
		} else {
			parts = append(parts, fmt.Sprintf("%d (revision %d)", deviceType.ID, deviceType.Revision))
		}
	}
	return strings.Join(parts, ", ")
}
