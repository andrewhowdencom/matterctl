# matterctl

A Go CLI and browser interface for local automation through OHF Matter Server.
`serve` connects to Matter Server and exposes an HTTP API. The CLI can inspect
the connection, discover and commission shared devices, inspect their capabilities,
set and confirm on/off state, write Matter labels, and watch live events.
The browser page lists commissioned devices and their reported state, updating
as Matter Server sends changes. It can also add an already-networked Matter
device using a fresh sharing code.

See the [implementation plan](PLAN.md) for the agreed direction, proposed
commands, and milestones toward controlling the first light.
See [browser requirements](WEB_REQUIREMENTS.md) for the first device-list screen.
See [user documentation](docs/index.md) for connection checks and the API
reference, and [architecture](ARCHITECTURE.md) for application boundaries.
See [automation setup](docs/how-to/preview-automations.md) for Starlark rules,
the BILRESA example, and the preview CLI.
See [SkyConnect Thread setup](docs/how-to/setup-skyconnect-thread.md) for the
host's OpenThread border router deployment and credential import.

## Development

Prerequisites: Go 1.26.2 or newer, Node.js 20 or newer for browser tests,
a C compiler for race tests,
[Task](https://taskfile.dev/), and
[golangci-lint v2](https://golangci-lint.run/docs/welcome/install/) with support
for the installed Go version. Deployment also requires Podman with Quadlet,
systemd, and sudo access.

```sh
task setup
task validate
task run
# Or:
task build
./bin/matterctl serve
```

Open <http://localhost:8081/>. The default address `:8081` accepts connections
from the LAN. Port 8080 is already used by another service on the target host.

```sh
./bin/matterctl --help
./bin/matterctl serve --help
./bin/matterctl version
./bin/matterctl server info
./bin/matterctl server info --output json
./bin/matterctl device list
./bin/matterctl device discover
./bin/matterctl device commission --code-stdin --network-only --name "Den Light"
./bin/matterctl device label set 1 "Den Light"
./bin/matterctl device power on "Den Light"
./bin/matterctl device power on 1 --endpoint 1
./bin/matterctl device power off 1 --endpoint 1
./bin/matterctl device inspect "Couch lamp"
./bin/matterctl event watch --output json
./bin/matterctl automation check examples/den.star
./bin/matterctl automation watch examples/den.star
```

## Configuration

Settings use Viper, with precedence: flags, environment, configuration file,
then defaults. The optional default file is
`$XDG_CONFIG_HOME/matterctl/config.yaml` (normally
`~/.config/matterctl/config.yaml`); XDG system configuration directories are
also searched. Use `--config PATH` to require a specific file.

```yaml
http:
  address: ":8081"
log:
  level: info
matter:
  url: "ws://127.0.0.1:5580/ws"
server:
  url: "http://127.0.0.1:8081"
request:
  timeout: 5s
output: text
```

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| HTTP address | `--http.address` | `MATTERCTL_HTTP_ADDRESS` | `:8081` |
| Log level | `--log-level` | `MATTERCTL_LOG_LEVEL` | `info` |
| Upstream URL (`serve`) | `--matter.url` | `MATTERCTL_MATTER_URL` | `ws://127.0.0.1:5580/ws` |
| Service URL (CLI) | `--server.url` | `MATTERCTL_SERVER_URL` | `http://127.0.0.1:8081` |
| Ordinary CLI request deadline | `--request.timeout` | `MATTERCTL_REQUEST_TIMEOUT` | `5s` |
| CLI output | `--output` | `MATTERCTL_OUTPUT` | `text` |
| Automation files | `--automation.directory` | `MATTERCTL_AUTOMATION_DIRECTORY` | `$XDG_CONFIG_HOME/matterctl/automations` |

For example, `matterctl serve --http.address 127.0.0.1:8081` restricts the
development server to localhost. Logs go to stderr using `log/slog`.

Discovery waits up to 30 seconds. Commissioning has its own `--timeout` (default
5 minutes, maximum 10 minutes). Pairing codes use `--code-stdin` or `--code`;
they are never loaded from Viper configuration or environment variables.
See [share an existing Matter light](docs/how-to/commission-device.md) for the
Google Home workflow and hidden terminal input.

## Install on the host

```sh
task install
```

This validates the project and saved automations, builds
`localhost/matterctl:latest` in the **rootful** Podman store, installs a
Quadlet generated from `deploy/matterctl.container` at
`/etc/containers/systemd/matterctl.container`, reloads systemd, and starts or
restarts `matterctl.service`. Run it again after changes to rebuild and deploy.
It mounts `${XDG_CONFIG_HOME:-$HOME/.config}/matterctl/automations` read-only
for service startup. Use `task automation:example` to save the BILRESA and Den
Light example before installation.
The Quadlet's `[Install]` section activates the service at boot; do not run
`systemctl enable` on the generated unit.

The image runs the binary as UID/GID `65532:65532`, with a read-only filesystem
and no Linux capabilities. It uses host networking to reach
Matter Server's loopback-only API at `ws://127.0.0.1:5580/ws`. The installed
service listens on port 8081. The container filesystem remains read-only.
The browser device list and `/healthz` remain available when Matter Server is
down; `server info` and `device list` report
upstream failures and exit nonzero. Event streams report connection changes and
receive a replacement inventory after reconnect. The local API has no authentication.
Browser commissioning requires a live Matter Server connection. Anyone with
access to this local HTTP service can use its commissioning API.

```sh
sudo systemctl status matterctl.service
sudo journalctl -u matterctl.service -f
curl http://127.0.0.1:8081/
./bin/matterctl server info
sudo systemctl stop matterctl.service
```

To change deployed settings, edit the environment entries in the Quadlet
source, then run `task install`. A subsequent install overwrites direct edits
to the installed Quadlet. The image contains no host configuration files.
