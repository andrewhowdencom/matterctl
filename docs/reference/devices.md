# Device inventory and events

## CLI commands

```text
matterctl device list [--output text|json]
matterctl device inspect <target> [--output text|json]
matterctl event watch [--device <target>] [--output text|json]
```

Commands use the existing `server.url`, `request.timeout`, and `output`
[settings](server-info.md). Node IDs are decimal and retain their full 64-bit
value. Labels are exact, case-sensitive Matter node or bridged endpoint labels.
Numeric targets select node IDs. A label matching several nodes is ambiguous.
Inspecting an endpoint label returns its containing node and all endpoints.
These commands do not change devices or commission new ones.

`device list` reads the synchronized service cache. It reports connection state,
readiness, and the last inventory update time. Inspection includes availability,
the last node update time, labels, vendor/product names, and endpoint metadata.
Times indicate when matterctl applied updates; they are not fresh reads of every
attribute. Availability is the upstream's last reported device availability.

An empty ready inventory is successful. If synchronization is incomplete or the
upstream has disconnected, listing prints the stale diagnostic snapshot and
exits nonzero; inspection fails. Connection readiness does not guarantee device
reachability or delivery of every event. `server info` separately checks current
upstream responsiveness with a round trip.

## Inventory HTTP API

### GET /api/v1/devices

Returns an inventory envelope with `connection`, `ready`, `observed_at` when
known, and a `devices` array. Arrays remain empty arrays for empty inventory.
HTTP 200 means a complete snapshot has been applied to the current connection.
HTTP 503 returns `ready: false`, a diagnostic `error`, and the previous snapshot
when available. The initial snapshot deadline is ten seconds; failures trigger
reconnection and a new subscription.

Each device has a string `node_id`, optional `label`, `vendor`, and `product`,
plus `available`, `is_bridge`, `observed_at`, and `endpoints`. An endpoint has:

| Field | Meaning |
| --- | --- |
| `id` / optional `parent_id` | Numeric endpoint ID and directly reported parent |
| `label` | Optional bridged-device label |
| `device_types` | Advertised type IDs and revisions |
| `server_clusters` / `client_clusters` | Capability roles, kept separate |
| `descriptor_complete` | Whether type, server-list, and client-list metadata were present |
| `capabilities` | Friendly power actions supported by advertised server-side commands |
| `power`, `level`, `switch_position` | Selected cached state, omitted when unknown |

Clusters expose their ID, a known name when available, accepted commands,
attribute/event IDs, and a validated feature map when present. Unknown names
remain numeric IDs. `accepted_commands: null` means metadata is unavailable;
`accepted_commands: []` is a known empty list. This phase names the OnOff
commands `off`, `on`, and `toggle`; other commands retain their IDs.

The API projects selected fields rather than returning raw node dumps.
Unselected attribute values, including commissioning/security data, are not
exposed. Attribute IDs can still be inspected.

### GET /api/v1/devices/inspect?target=TARGET

Returns a device object. URL-encode labels, for example:

```sh
curl --get --data-urlencode 'target=Couch lamp' \
  http://127.0.0.1:8081/api/v1/devices/inspect
```

| HTTP status | Meaning |
| --- | --- |
| 200 | Device found in ready inventory |
| 404 | No matching node or label |
| 409 | Label matches several nodes |
| 503 | Inventory is unavailable or synchronizing |

All inventory responses use JSON and `Cache-Control: no-store`.

## Event stream

### GET /api/v1/events?device=TARGET

The optional `device` query selects a node once, by ID or label. All endpoints
of that node are included. Connection changes are always included. Node removal
or a later label change does not redirect an existing subscription to another
node. A filtered stream requires ready inventory; the unfiltered stream can
start while upstream is unavailable and observe recovery.

Successful streams use `application/x-ndjson`. Each line contains `type`, a
service-side `observed_at`, optional string `node_id`, and `data`:

| Type | Data |
| --- | --- |
| `inventory` | Initial or replacement snapshot, filtered when requested |
| `connection` | Upstream connection state and inventory readiness |
| `node_added`, `node_updated` | Projected device object |
| `node_removed` | String node ID |
| `attribute_updated` | Attribute path and updated projected device |
| `endpoint_added`, `endpoint_removed` | Endpoint ID and updated projected device |
| `node_event` | Endpoint/cluster/event IDs, counter/timestamp strings, timestamp type, priority, and selected event fields |
| `heartbeat` | Empty object, every 15 seconds |
| `stream_error` | Stream interrupted; restart to obtain a new snapshot |

Standard Switch event fields include position and press-count values when
reported. Other event payloads are omitted; their metadata is still observable.
There is no durable event history. Upstream events during a disconnection cannot
be replayed; upstream backpressure can also discard some device events. Cached
state is replaced after reconnect, not inferred from missed events.

The initial snapshot and subscriber registration are atomic. Slow subscribers
have a queue of 64 events; overflow closes that subscriber without blocking
others. A terminal `stream_error` is sent when possible. EOF, network truncation,
and stalled streams cause a nonzero CLI exit and an explicit resynchronization
message. Ctrl-C exits normally, including signal-specific cancellation causes.

`request.timeout` bounds HTTP establishment, not total watch duration. Each
server-side write has a ten-second deadline. A stream with no frames for 45
seconds is canceled by the CLI. Frames and inventory HTTP responses are limited
to 16 MiB by the client. A failed stream is not automatically reconnected;
restart watch to receive a new initial snapshot. Existing streams remain open
across upstream reconnects and receive a replacement inventory.

Upstream wire formats are documented in the
[Matter Server v1.5.5 API](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/websockets_api.md).
