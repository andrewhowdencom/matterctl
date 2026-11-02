package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/server"
)

// NewCommand constructs an independent command tree and configuration instance.
func NewCommand() (*cobra.Command, error) {
	config := viper.New()
	config.SetEnvPrefix("MATTERCTL")
	config.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	config.AutomaticEnv()

	var configFile string
	var logger *slog.Logger
	root := &cobra.Command{
		Use:           "matterctl",
		Short:         "Manage Matter devices and local automation",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := readConfig(config, configFile); err != nil {
				return err
			}
			var level slog.Level
			if err := level.UnmarshalText([]byte(config.GetString("log.level"))); err != nil {
				return fmt.Errorf("invalid log level: %w", err)
			}
			logger = slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level}))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&configFile, "config", "", "Configuration file (default: $XDG_CONFIG_HOME/matterctl/config.yaml)")
	root.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, or error")
	if err := config.BindPFlag("log.level", root.PersistentFlags().Lookup("log-level")); err != nil {
		return nil, fmt.Errorf("bind log level: %w", err)
	}

	serve := &cobra.Command{
		Use:     "serve",
		Short:   "Serve the matterctl browser interface",
		Example: "  matterctl serve\n  matterctl serve --http.address 127.0.0.1:8081",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return server.Run(cmd.Context(), config.GetString("http.address"), logger)
		},
	}
	serve.Flags().String("http.address", ":8081", "HTTP listen address (MATTERCTL_HTTP_ADDRESS)")
	if err := config.BindPFlag("http.address", serve.Flags().Lookup("http.address")); err != nil {
		return nil, fmt.Errorf("bind HTTP address: %w", err)
	}
	root.AddCommand(serve, &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), root.Version)
			return err
		},
	})
	return root, nil
}

func readConfig(config *viper.Viper, filename string) error {
	if filename != "" {
		config.SetConfigFile(filename)
	} else {
		config.SetConfigName("config")
		config.SetConfigType("yaml")
		config.AddConfigPath(filepath.Join(xdg.ConfigHome, "matterctl"))
		for _, directory := range xdg.ConfigDirs {
			config.AddConfigPath(filepath.Join(directory, "matterctl"))
		}
	}
	if err := config.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if filename == "" && errors.As(err, &missing) {
			return nil
		}
		return fmt.Errorf("read configuration: %w", err)
	}
	return nil
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	version := info.Main.Version
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			version += " (" + setting.Value + ")"
		}
		if setting.Key == "vcs.modified" && setting.Value == "true" {
			version += " dirty"
		}
	}
	return version
}
