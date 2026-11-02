package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		env     string
		flags   []string
		wantErr bool
	}{
		{name: "file", file: "log:\n  level: debug\n"},
		{name: "invalid file value", file: "log:\n  level: invalid\n", wantErr: true},
		{name: "environment overrides file", file: "log:\n  level: invalid\n", env: "debug"},
		{name: "flag overrides environment", file: "log:\n  level: invalid\n", env: "invalid", flags: []string{"--log-level", "warn"}},
		{name: "malformed file", file: "log: [", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MATTERCTL_LOG_LEVEL", tt.env)
			filename := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(filename, []byte(tt.file), 0o600); err != nil {
				t.Fatal(err)
			}
			command, err := NewCommand()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			args := append([]string{"--config", filename, "version"}, tt.flags...)
			command.SetArgs(args)
			err = command.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, want error = %v", err, tt.wantErr)
			}
		})
	}
}

func TestExplicitMissingConfigurationFails(t *testing.T) {
	command, err := NewCommand()
	if err != nil {
		t.Fatal(err)
	}
	command.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml"), "version"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected an error for an explicitly requested missing config")
	}
}
