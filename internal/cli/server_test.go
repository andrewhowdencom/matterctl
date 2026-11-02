package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestServerInfoCLI(t *testing.T) {
	for _, tt := range []struct {
		name   string
		format string
		code   int
		state  string
	}{
		{"text", "text", 200, "connected"}, {"json", "json", 200, "connected"},
		{"unavailable text", "text", 503, "disconnected"}, {"unavailable json", "json", 503, "disconnected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != api.ServerInfoPath {
					t.Errorf("CLI bypassed service API: %s", r.URL.Path)
				}
				status := api.ServerStatus{Connection: api.Connection{State: tt.state}}
				if tt.code == 200 {
					status.Server = &api.ServerInfo{SDKVersion: "matter-server/test", SchemaVersion: 14}
				} else {
					status.Error = "Matter Server is unavailable"
				}
				w.WriteHeader(tt.code)
				if err := json.NewEncoder(w).Encode(status); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			filename := filepath.Join(t.TempDir(), "config.yaml")
			// Verify service URL configuration reaches the HTTP client through Viper.
			if err := os.WriteFile(filename, []byte("server:\n  url: "+server.URL+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			command, err := NewCommand()
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SetArgs([]string{"--config", filename, "server", "info", "--output", tt.format})
			err = command.ExecuteContext(context.Background())
			if (err != nil) != (tt.code != 200) {
				t.Fatalf("Execute() = %v, status %d", err, tt.code)
			}
			if tt.format == "json" {
				var status api.ServerStatus
				if err := json.Unmarshal(stdout.Bytes(), &status); err != nil || status.Connection.State != tt.state {
					t.Fatalf("invalid JSON output %q: %v", stdout.String(), err)
				}
			} else if !strings.Contains(stdout.String(), tt.state) {
				t.Fatalf("missing connection state: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected logs: %s", &stderr)
			}
		})
	}
}

func TestServerInfoInvalidOptions(t *testing.T) {
	for _, flags := range [][]string{
		{"--output", "xml"}, {"--request.timeout", "0s"}, {"--request.timeout", "-1s"}, {"--server.url", "ws://localhost"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			command, err := NewCommand()
			if err != nil {
				t.Fatal(err)
			}
			command.SetArgs(append([]string{"server", "info"}, flags...))
			if err := command.Execute(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
