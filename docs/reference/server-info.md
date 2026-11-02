# Server information

## CLI

```text
matterctl server info [--server.url URL] [--request.timeout DURATION] [--output text|json]
```

The command queries the matterctl service, which sends a fresh `server_info`
request over its existing Matter Server WebSocket session. It reports:

- Connection state and the current session's connection time.
- SDK/server version and API schema range.
- Whether Bluetooth is enabled and Wi-Fi/Thread credentials are configured.
- Connection or request failures, when present.

It never returns network credentials or pairing codes. It does not list devices
or change Matter Server configuration.

| Setting | Flag | Environment | Default |
| --- | --- | --- | --- |
| matterctl HTTP service | `--server.url` | `MATTERCTL_SERVER_URL` | `http://127.0.0.1:8081` |
| CLI deadline | `--request.timeout` | `MATTERCTL_REQUEST_TIMEOUT` | `5s` |
| Output | `--output` | `MATTERCTL_OUTPUT` | `text` |

Flags override environment variables, configuration files, and defaults, in
that order. Nested file keys are `server.url` and `request.timeout`; `output`
is a top-level key. URLs may include a service base path; user information,
queries, and fragments are rejected. Only HTTP and HTTPS URLs are accepted.

Exit status is zero only when the information request succeeds. If the service
returns an error response, its diagnostic object is still printed, including
in JSON mode, followed by an error on stderr and a nonzero exit. Transport
failures that yield no diagnostic object print only an error on stderr.

## Service configuration

```text
matterctl serve [--http.address ADDRESS] [--matter.url URL]
```

`matter.url` defaults to `ws://127.0.0.1:5580/ws` and can be configured through
`--matter.url`, `MATTERCTL_MATTER_URL`, or the nested configuration file key.
Only absolute WS/WSS URLs without user information or fragments are accepted.

The initial handshake has a five-second deadline. The client supports schema
versions 11 through 14 and requires overlap with the server's advertised range.
Reconnect delays increase exponentially with jitter up to 30 seconds. The HTTP
listener remains available during connection attempts and reconnects.

## HTTP API

### GET /api/v1/server/info

Returns JSON with `Content-Type: application/json` and `Cache-Control: no-store`.
For example:

```json
{
  "connection": {
    "state": "connected",
    "connected_at": "2026-11-02T17:37:43Z"
  },
  "server": {
    "sdk_version": "matter-server/1.5.5 (matter.js/0.18.0)",
    "schema_version": 14,
    "min_supported_schema_version": 11,
    "bluetooth_enabled": true,
    "wifi_credentials_set": false,
    "thread_credentials_set": false
  }
}
```

Connection states are `connecting`, `connected`, `disconnected`, and
`incompatible`. `connected` describes the WebSocket session; an individual
information request can still fail or time out. `connected_at` is present only
for an active compatible session. `last_error`, when present, describes the
last connection failure. The top-level `error` describes the current request
failure. `server` is absent when no compatible session is connected.

| HTTP status | Meaning |
| --- | --- |
| 200 | Fresh server information received and validated |
| 503 | No compatible connected upstream session, or the session was lost |
| 504 | The upstream request exceeded its two-second deadline |
| 502 | Upstream rejection or invalid response |
| 405 | Unsupported HTTP method |

Upstream protocol errors expose their numeric error code, without arbitrary
message or details payloads. Neither this endpoint nor the CLI caches successful
responses for later requests.

### GET /healthz

Returns HTTP 200 with `{"status":"ok"}` while the HTTP server is running.
This is process liveness, independent of Matter Server connectivity. The browser
placeholder at `/` also remains available while the upstream is disconnected.

## Upstream protocol

This phase uses the initial server information frame and `server_info` request
from the [Matter Server v1.5.5 API](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/websockets_api.md).
