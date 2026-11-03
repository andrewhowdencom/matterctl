package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/automation"
)

func automationCommand(config *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "automation", Short: "Preview Starlark automation rules without controlling devices"}
	command.AddCommand(&cobra.Command{
		Use: "check <rules.star>", Short: "Parse a rule file and count its handlers", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := loadAutomation(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			format, err := automationFormat(config)
			if err != nil {
				return err
			}
			if format == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Rules int `json:"rules"`
				}{engine.RuleCount()})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d rules registered\n", engine.RuleCount())
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "replay <rules.star> <events.ndjson>", Short: "Preview action plans from recorded event frames", Args: cobra.ExactArgs(2),
		Example: "  matterctl automation replay rules.star events.ndjson\n  matterctl automation replay rules.star - < events.ndjson",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := loadAutomation(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			format, err := automationFormat(config)
			if err != nil {
				return err
			}
			input := cmd.InOrStdin()
			if args[1] != "-" {
				file, err := os.Open(args[1])
				if err != nil {
					return fmt.Errorf("open event recording: %w", err)
				}
				defer func() { _ = file.Close() }()
				input = file
			}
			return replayAutomation(cmd.Context(), input, cmd.OutOrStdout(), engine, format)
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "watch <rules.star>", Short: "Preview action plans from the live event stream", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := loadAutomation(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			err = service.Watch(cmd.Context(), "", func(event api.Event) error {
				return previewEvent(cmd.Context(), cmd.OutOrStdout(), engine, event, format)
			})
			if errors.Is(cmd.Context().Err(), context.Canceled) {
				return nil
			}
			return err
		},
	})
	return command
}

func loadAutomation(ctx context.Context, filename string) (*automation.Engine, error) {
	// #nosec G304 -- the CLI reads the rule file explicitly selected by its caller.
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open rule file: %w", err)
	}
	defer func() { _ = file.Close() }()
	source, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, fmt.Errorf("read rule file: %w", err)
	}
	return automation.Load(ctx, filename, source)
}

func automationFormat(config *viper.Viper) (string, error) {
	format := config.GetString("output")
	if format != "text" && format != "json" {
		return "", fmt.Errorf("output must be text or json")
	}
	return format, nil
}

func replayAutomation(ctx context.Context, input io.Reader, output io.Writer, engine *automation.Engine, format string) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var event api.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Type == "" || event.Data == nil {
			return fmt.Errorf("invalid event on line %d", line)
		}
		if err := previewEvent(ctx, output, engine, event, format); err != nil {
			return fmt.Errorf("event on line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read event recording: %w", err)
	}
	return nil
}

func previewEvent(ctx context.Context, output io.Writer, engine *automation.Engine, event api.Event, format string) error {
	firings, err := engine.Feed(ctx, event)
	if err != nil {
		return err
	}
	for _, firing := range firings {
		if format == "json" {
			if err := json.NewEncoder(output).Encode(firing); err != nil {
				return err
			}
			continue
		}
		parts := make([]string, 0, len(firing.Actions))
		for _, action := range firing.Actions {
			detail := fmt.Sprintf("%s(node=%s endpoint=%d", action.Kind, action.NodeID, action.Endpoint)
			if action.Value != nil {
				detail += fmt.Sprintf(" value=%v", action.Value)
			}
			parts = append(parts, detail+")")
		}
		steps := ""
		if firing.Signal.Kind == "wheel.turn" {
			steps = fmt.Sprintf(" steps=%+d", firing.Signal.Steps)
		}
		if _, err := fmt.Fprintf(output, "%s rule=%d %s node=%s endpoint=%d%s -> %s\n",
			firing.Signal.ObservedAt.Format(time.RFC3339), firing.Rule, firing.Signal.Kind,
			firing.Signal.NodeID, firing.Signal.Endpoint, steps, strings.Join(parts, ", ")); err != nil {
			return err
		}
	}
	return nil
}
