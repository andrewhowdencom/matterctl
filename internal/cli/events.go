package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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
		Example: "  matterctl event watch --device \"Couch lamp\"\n  matterctl event watch --output json",
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
				return printEvent(cmd.OutOrStdout(), event)
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

func printEvent(output io.Writer, event api.Event) error {
	if event.Type == "heartbeat" {
		return nil
	}
	line := event.ObservedAt.Format(time.RFC3339) + " " + event.Type
	if event.NodeID != "" {
		line += " node=" + string(event.NodeID)
	}
	decode := func(value any) error {
		if err := json.Unmarshal(event.Data, value); err != nil {
			return fmt.Errorf("invalid %s event: %w", event.Type, err)
		}
		return nil
	}
	switch event.Type {
	case "inventory":
		var inventory api.Inventory
		if err := decode(&inventory); err != nil {
			return err
		}
		line += fmt.Sprintf(" connection=%s ready=%t devices=%d", inventory.Connection.State, inventory.Ready, len(inventory.Devices))
		if len(inventory.Devices) == 1 {
			line += deviceSummary(inventory.Devices[0])
		}
	case "connection":
		var update struct {
			Connection api.Connection `json:"connection"`
			Ready      bool           `json:"ready"`
		}
		if err := decode(&update); err != nil {
			return err
		}
		line += fmt.Sprintf(" state=%s ready=%t", update.Connection.State, update.Ready)
		if update.Connection.LastError != "" {
			line += fmt.Sprintf(" reason=%q", update.Connection.LastError)
		}
	case "node_added", "node_updated":
		var device api.Device
		if err := decode(&device); err != nil {
			return err
		}
		line += deviceSummary(device)
	case "node_removed":
		// The node ID in the event envelope identifies the removed device.
	case "attribute_updated":
		var update struct {
			Path          string     `json:"path"`
			AttributeName string     `json:"attribute_name"`
			Device        api.Device `json:"device"`
			State         *api.State `json:"state"`
		}
		if err := decode(&update); err != nil {
			return err
		}
		line += deviceIdentity(update.Device.Label, update.Device.Product)
		if update.AttributeName != "" {
			line += fmt.Sprintf(" attribute=%s path=%s", update.AttributeName, update.Path)
		} else {
			line += fmt.Sprintf(" attribute=%s", update.Path)
		}
		parts := strings.Split(update.Path, "/")
		if len(parts) == 3 {
			endpointID, err := strconv.ParseUint(parts[0], 10, 16)
			if err == nil {
				line += fmt.Sprintf(" endpoint=%d", endpointID)
				for _, endpoint := range update.Device.Endpoints {
					if endpoint.ID != uint16(endpointID) {
						continue
					}
					if endpoint.Label != "" {
						line += fmt.Sprintf(" label=%q", endpoint.Label)
					}
					line += endpointType(endpoint)
					break
				}
			}
		}
		if update.State != nil {
			line += fmt.Sprintf(" %s=%s", update.State.Name, update.State.Text)
		}
	case "endpoint_added", "endpoint_removed":
		var update struct {
			EndpointID uint16     `json:"endpoint_id"`
			Device     api.Device `json:"device"`
		}
		if err := decode(&update); err != nil {
			return err
		}
		line += deviceIdentity(update.Device.Label, update.Device.Product)
		line += fmt.Sprintf(" endpoint=%d", update.EndpointID)
		for _, endpoint := range update.Device.Endpoints {
			if endpoint.ID == update.EndpointID {
				if endpoint.Label != "" {
					line += fmt.Sprintf(" label=%q", endpoint.Label)
				}
				line += endpointType(endpoint)
				break
			}
		}
	case "node_event":
		var update struct {
			EndpointID  uint16           `json:"endpoint_id"`
			ClusterID   uint32           `json:"cluster_id"`
			ClusterName string           `json:"cluster_name"`
			EventID     uint32           `json:"event_id"`
			EventName   string           `json:"event_name"`
			EventNumber string           `json:"event_number"`
			Label       string           `json:"label"`
			Product     string           `json:"product"`
			DeviceTypes []api.DeviceType `json:"device_types"`
			Data        map[string]uint8 `json:"data"`
		}
		if err := decode(&update); err != nil {
			return err
		}
		line += deviceIdentity(update.Label, update.Product)
		line += fmt.Sprintf(" endpoint=%d cluster=%d", update.EndpointID, update.ClusterID)
		if update.ClusterName != "" {
			line += " cluster_name=" + update.ClusterName
		}
		line += fmt.Sprintf(" event=%d", update.EventID)
		if update.EventName != "" {
			line += " name=" + update.EventName
		}
		if len(update.DeviceTypes) != 0 {
			line += fmt.Sprintf(" type=%q", deviceTypeSummary(update.DeviceTypes))
		}
		if update.EventNumber != "" {
			line += " number=" + update.EventNumber
		}
		for _, field := range []struct{ key, name string }{
			{"newPosition", "new_position"},
			{"previousPosition", "previous_position"},
			{"currentPosition", "current_position"},
			{"numberOfPressesCounted", "presses"},
			{"totalNumberOfPressesCounted", "total_presses"},
		} {
			if value, ok := update.Data[field.key]; ok {
				line += fmt.Sprintf(" %s=%d", field.name, value)
			}
		}
	default:
		line += " (details available with --output json)"
	}
	_, err := fmt.Fprintln(output, line)
	return err
}

func deviceSummary(device api.Device) string {
	line := fmt.Sprintf(" available=%t endpoints=%d", device.Available, len(device.Endpoints))
	if device.Label != "" {
		line += fmt.Sprintf(" label=%q", device.Label)
	} else if device.Product != "" {
		line += fmt.Sprintf(" product=%q", device.Product)
	}
	types := make([]string, 0, len(device.Endpoints))
	states := make([]string, 0)
	for _, endpoint := range device.Endpoints {
		if endpoint.ID == 0 && len(device.Endpoints) > 1 {
			continue
		}
		if len(endpoint.DeviceTypes) != 0 {
			types = append(types, deviceTypeSummary(endpoint.DeviceTypes))
		}
		for _, state := range endpoint.States {
			states = append(states, state.Name+"="+state.Text)
		}
	}
	if len(types) != 0 {
		line += fmt.Sprintf(" types=%q", groupedList(types))
	}
	if len(states) != 0 {
		line += fmt.Sprintf(" states=%q", groupedList(states))
	}
	return line
}

func deviceIdentity(label, product string) string {
	if label != "" {
		return fmt.Sprintf(" device=%q", label)
	}
	if product != "" {
		return fmt.Sprintf(" device=%q", product)
	}
	return ""
}

func endpointType(endpoint api.Endpoint) string {
	if len(endpoint.DeviceTypes) == 0 {
		return ""
	}
	return fmt.Sprintf(" type=%q", deviceTypeSummary(endpoint.DeviceTypes))
}

func groupedList(items []string) string {
	const limit = 4
	counts := make(map[string]int, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		if counts[item] == 0 {
			order = append(order, item)
		}
		counts[item]++
	}
	parts := make([]string, 0, min(len(order), limit))
	for _, item := range order[:min(len(order), limit)] {
		if counts[item] == 1 {
			parts = append(parts, item)
		} else {
			parts = append(parts, fmt.Sprintf("%s (%d endpoints)", item, counts[item]))
		}
	}
	if len(order) <= limit {
		return strings.Join(parts, ",")
	}
	return fmt.Sprintf("%s,+%d more", strings.Join(parts, ","), len(order)-limit)
}
