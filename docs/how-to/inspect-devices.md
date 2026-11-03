# Inspect devices and watch changes

With the matterctl service running, list its commissioned devices:

```sh
matterctl device list
matterctl device list --output json
```

Use `./bin/matterctl` for a local development build. From another LAN machine,
add `--server.url http://192.168.86.33:8081`, replacing the address as needed.
The CLI always queries matterctl's HTTP API.

The list shows node and endpoint IDs, Matter labels, advertised device types,
last reported availability, power capabilities, and known power state. The type
comes from the endpoint's Descriptor cluster; it does not depend on the label.
A synchronized empty
inventory is a successful result; the CLI does not discover uncommissioned
devices or import Google Home devices automatically.

Inspect a decimal node ID or an unambiguous Matter label:

```sh
matterctl device inspect 12
matterctl device inspect "Couch lamp"
matterctl device inspect "Couch lamp" --output json
```

Inspection shows every observed endpoint on the node, its parent when known,
device types, server and client clusters, command metadata, features, and
selected state. A light can accept OnOff commands on a server cluster; a switch
with an OnOff client cluster sends commands and does not gain power controls
merely from that cluster's presence. `accepted_commands: null` means unknown,
while `[]` means the server advertised no accepted commands.

Matter labels can differ from Google Home nicknames. Duplicate labels across
nodes are rejected; use the node ID to disambiguate. An endpoint label selects
its containing node for inspection. Use `matterctl device label set <node> <name>` to write a node label.
For a bridged device with its own label attribute, pass `--endpoint <id>`.

Watch live changes:

```sh
matterctl event watch
matterctl event watch --device "Couch lamp"
matterctl event watch --output json
```

The first event contains an inventory snapshot. A device filter selects one
node, including all its endpoints. JSON mode writes one complete JSON object
per line, suitable for scripts. Text mode writes compact lines, for example:

```text
2026-11-03T12:30:00Z inventory node=12 connection=connected ready=true devices=1 available=true endpoints=1 label="Couch lamp" types="Extended Color Light" states="power=off"
2026-11-03T12:30:04Z attribute_updated node=12 device="Couch lamp" attribute=1/6/0 endpoint=1 label="Light" type="Extended Color Light" power=on
2026-11-03T12:31:02Z attribute_updated node=15 attribute=1/1030/0 endpoint=1 type="Occupancy Sensor" occupancy=occupied
2026-11-03T12:31:05Z attribute_updated node=16 attribute=1/1026/0 endpoint=1 type="Temperature Sensor" temperature=21.50°C
2026-11-03T12:31:09Z node_event node=2 device="BILRESA scroll wheel" endpoint=2 cluster=59 event=1 name=initial_press type="Generic Switch" number=42 new_position=1
```

Text mode shows the advertised endpoint type and recognized power, level,
switch-position, occupancy, and temperature values. Inventory and node updates
include a compact summary of up to four distinct types and states, with counts
for types repeated across endpoints. Other attribute
updates show their numeric path without a full device snapshot; `--output json`
contains the full projected event, but unrecognized raw values are deliberately
omitted. Text mode suppresses heartbeat messages. Press Ctrl-C to stop normally.
`node` is the commissioned Matter node ID. On device changes, `device` uses the
node's Matter label when present, or its product name otherwise. The node ID
remains the unique identifier when names are absent or duplicated.

During an upstream disconnection, the stream reports connection changes. It
receives a replacement inventory after reconnect. `device list` returns a
nonzero exit with `ready: false` and the last cached snapshot until synchronization
finishes; `device inspect` does not resolve targets from stale data.

If a stream reports interruption, restart `event watch` to obtain a new
snapshot. Events are live notifications, not a durable history or guaranteed
replay of every physical button press. See the [reference](../reference/devices.md)
for filtering, state freshness, and stream limits.
