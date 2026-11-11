package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func historyCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "history", Short: "Inspect durable household activity"}
	var since, until, device, file, kind, outcome, runID string
	var before int64
	var limit int
	list := &cobra.Command{
		Use: "list", Short: "List whole-house history (last seven days by default)", Args: cobra.NoArgs,
		Example: "  matterctl history list\n  matterctl history list --device 1 --since 2026-11-09T00:00:00Z\n  matterctl history list --output json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			query := api.HistoryQuery{File: file, Kind: kind, Outcome: outcome, RunID: runID, Before: before, Limit: limit}
			if since != "" {
				query.Since, err = time.Parse(time.RFC3339, since)
				if err != nil {
					return fmt.Errorf("since must be RFC3339: %w", err)
				}
			}
			if until != "" {
				query.Until, err = time.Parse(time.RFC3339, until)
				if err != nil {
					return fmt.Errorf("until must be RFC3339: %w", err)
				}
			}
			if device != "" {
				if _, err = strconv.ParseUint(device, 10, 64); err != nil {
					return fmt.Errorf("device must be a node ID: %w", err)
				}
				query.Device = api.NodeID(device)
			}
			page, err := service.History(cmd.Context(), query)
			if err != nil {
				return err
			}
			return printHistoryPage(cmd.OutOrStdout(), page, format)
		},
	}
	list.Flags().StringVar(&since, "since", "", "Start time in RFC3339 (default: seven days ago)")
	list.Flags().StringVar(&until, "until", "", "End time in RFC3339")
	list.Flags().StringVar(&device, "device", "", "Filter by Matter node ID")
	list.Flags().StringVar(&file, "automation", "", "Filter by automation file name")
	list.Flags().StringVar(&kind, "kind", "", "Filter by history kind")
	list.Flags().StringVar(&outcome, "outcome", "", "Filter by outcome")
	list.Flags().StringVar(&runID, "run", "", "Show every record in a causal automation run")
	list.Flags().Int64Var(&before, "before", 0, "Continue with entries before this history ID")
	list.Flags().IntVar(&limit, "limit", 50, "Entries per page (1–100)")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use: "inspect <id>", Short: "Inspect one history record", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("history ID must be positive")
			}
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			entry, err := service.HistoryEntry(cmd.Context(), id)
			if err != nil {
				return err
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), entry)
			}
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), "%d %s %s\n%s\n", entry.ID, entry.ObservedAt.Local().Format(time.RFC3339), entry.Kind, entry.Summary); err != nil {
				return err
			}
			data, err := json.MarshalIndent(entry, "", "  ")
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintln(cmd.OutOrStdout(), string(data)); err != nil {
				return err
			}
			if entry.RunID != "" {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Related run: matterctl history list --run %s --since 1970-01-01T00:00:00Z\n", entry.RunID)
			}
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "follow", Short: "Follow newly persisted history until interrupted", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			page, err := service.History(cmd.Context(), api.HistoryQuery{Limit: 1})
			if err != nil {
				return err
			}
			var last int64
			if len(page.Entries) != 0 {
				last = page.Entries[0].ID
			}
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					query := api.HistoryQuery{After: last, Limit: 100}
					page, err := service.History(cmd.Context(), query)
					if err != nil {
						return err
					}
					for _, entry := range page.Entries {
						if err := printHistoryEntry(cmd.OutOrStdout(), entry, format); err != nil {
							return err
						}
						last = entry.ID
					}
				}
			}
		},
	})
	return command
}

func printHistoryPage(output io.Writer, page api.HistoryPage, format string) error {
	if format == "json" {
		return printJSON(output, page)
	}
	if page.Recording.State != "healthy" {
		if _, err := fmt.Fprintf(output, "Recording: %s (%s)\n", page.Recording.State, page.Recording.LastError); err != nil {
			return err
		}
	}
	if len(page.Entries) == 0 {
		_, err := fmt.Fprintln(output, "No history in this time window.")
		return err
	}
	for _, entry := range page.Entries {
		if err := printHistoryEntry(output, entry, format); err != nil {
			return err
		}
	}
	if page.Next != 0 {
		_, err := fmt.Fprintf(output, "Older entries: --before %d\n", page.Next)
		return err
	}
	return nil
}

func printHistoryEntry(output io.Writer, entry api.HistoryEntry, format string) error {
	if format == "json" {
		return json.NewEncoder(output).Encode(entry)
	}
	_, err := fmt.Fprintf(output, "%d %s %-22s %s\n", entry.ID, entry.ObservedAt.Local().Format(time.RFC3339), entry.Kind, entry.Summary)
	return err
}
