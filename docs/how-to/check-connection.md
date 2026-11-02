# Check the Matter Server connection

Start matterctl on the host that runs Matter Server:

```sh
task build
./bin/matterctl serve
```

By default, matterctl listens on port 8081 and connects to
`ws://127.0.0.1:5580/ws`. In another terminal, run:

```sh
./bin/matterctl server info
./bin/matterctl server info --output json
```

For the installed container, `task install` builds and restarts the service;
you can then run the same CLI commands without starting a second `serve`.

To use the CLI from another LAN machine, point it at the matterctl service:

```sh
matterctl server info --server.url http://192.168.86.33:8081
```

Replace the address if the service host's LAN address changes. CLI clients
connect to port 8081; Matter Server can keep its loopback-only binding.

## Diagnose a failed check

If the CLI cannot reach matterctl, check `server.url` and the service:

```sh
sudo systemctl status matterctl.service
curl --fail http://127.0.0.1:8081/healthz
sudo journalctl -u matterctl.service -n 30 --no-pager
```

If `/healthz` succeeds but `server info` fails, matterctl is running and its
upstream request failed. Inspect the connection state and error in the command
output, then check Matter Server:

```sh
sudo systemctl status matter-server.service
sudo journalctl -u matter-server.service -n 30 --no-pager
```

matterctl retries unavailable or incompatible upstream connections automatically.
A request that failed is not retried on your behalf; run `server info` again
after the connection recovers. An incompatible schema range requires a
compatible server or an updated matterctl client implementation.

The CLI deadline can be shortened independently of the service's two-second
upstream request limit:

```sh
matterctl server info --request.timeout 1s
```

See the [reference](../reference/server-info.md) for configuration and response
semantics.
