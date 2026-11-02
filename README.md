# matterctl

A Go CLI and browser interface for local automation through OHF Matter Server.
The initial `serve` command displays **unimplemented**. Matter connectivity is
not implemented yet.

## Development

Prerequisites: Go 1.26.2 or newer, [Task](https://taskfile.dev/), and
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
```

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| HTTP address | `--http.address` | `MATTERCTL_HTTP_ADDRESS` | `:8081` |
| Log level | `--log-level` | `MATTERCTL_LOG_LEVEL` | `info` |

For example, `matterctl serve --http.address 127.0.0.1:8081` restricts the
development server to localhost. Logs go to stderr using `log/slog`.

## Install on the host

```sh
task install
```

This validates the project, builds `localhost/matterctl:latest` in the
**rootful** Podman store, installs `deploy/matterctl.container` at
`/etc/containers/systemd/matterctl.container`, reloads systemd, and starts or
restarts `matterctl.service`. Run it again after changes to rebuild and deploy.
The Quadlet's `[Install]` section activates the service at boot; do not run
`systemctl enable` on the generated unit.

The image runs the binary as UID/GID `65532:65532`, with a read-only filesystem
and no Linux capabilities. It uses host networking so it can later reach
Matter Server's loopback-only API at `ws://127.0.0.1:5580/ws`. The installed
service listens on port 8081 and does not require Matter Server to be running
for this placeholder page.

```sh
sudo systemctl status matterctl.service
sudo journalctl -u matterctl.service -f
curl http://127.0.0.1:8081/
sudo systemctl stop matterctl.service
```

To change deployed settings, edit the environment entries in the Quadlet
source, then run `task install`. A subsequent install overwrites direct edits
to the installed Quadlet. The image contains no host configuration files.
