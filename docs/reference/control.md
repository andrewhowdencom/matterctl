# Matter labels and power control

## Matter labels

```sh
matterctl device label set <target> <label> [--endpoint <id>] [--timeout 10s]
```

The default writes the node's Basic Information `NodeLabel` (endpoint 0,
cluster 40, attribute 5). For a bridged device with a Bridged Device Basic
Information label, pass its endpoint ID (cluster 57, attribute 5). Ordinary
light endpoints do not have their own writable label. The label can contain up
to 32 UTF-8 bytes without surrounding whitespace; an explicit empty string
clears it. The device may reject
a write even when it advertises the attribute, for example because it does not
grant write access. The service checks the Matter write status and confirms the
value with a fresh read. A confirmed label is immediately usable as a CLI
target. Duplicate labels across nodes require a numeric node ID.

The HTTP request is `POST /api/v1/devices/label` with one JSON object:

```json
{"target":"1","label":"Den Light","timeout_ms":10000}
```

`endpoint` is optional. To clear a label, include `"label":""`; omitting the
field is rejected. The body is limited to 4096 bytes and rejects unknown fields.
A success result contains `node_id`, `endpoint`, `label`, and `confirmed_at`.
The default timeout is 10 seconds and accepts 1 to 30 seconds.

## On/off control

```sh
matterctl device power on <target> [--endpoint <id>] [--timeout 10s]
matterctl device power off <target> [--endpoint <id>] [--timeout 10s]
```

The target is a node ID or an unambiguous Matter label. A compatible endpoint is
selected automatically only when there is exactly one. Otherwise pass an
endpoint ID. The endpoint must advertise the requested
OnOff cluster command, and the device must be available. The service sends one
command through Matter Server and never retries it automatically. After a
successful command response, it performs fresh OnOff attribute reads until the
requested value is observed or the overall deadline expires. Cached state from
before the request cannot confirm success. The reported Matter state is the
confirmation; check the physical light separately.

The default overall timeout is 10 seconds; `--timeout` accepts 1 to 30 seconds.
It covers both command acknowledgement and state confirmation. `--output json`
returns `{node_id,endpoint,power,confirmed_at}` on success.

The HTTP request is `POST /api/v1/devices/power` with a JSON body such as:

```json
{"target":"1","action":"on","timeout_ms":10000}
```

`endpoint` is optional. The body is limited to 4096 bytes and rejects unknown
fields. Results and errors use `Cache-Control: no-store`.

| HTTP status | Meaning |
| --- | --- |
| 200 | Requested power state confirmed by a fresh read |
| 400 | Invalid action, target, endpoint conflict, or timeout |
| 404 | No current node for the target |
| 409 | Name or compatible endpoint is ambiguous; another power request is active |
| 422 | Endpoint does not advertise the requested command |
| 503 | Device is offline, or Matter Server inventory is unavailable |
| 504 | Command was acknowledged but state could not be confirmed before the deadline, or the upstream timed out |
| 502 | Device rejected the command or another upstream failure occurred |

An error states whether the command was acknowledged. A timeout or connection
loss can leave the physical outcome unknown. Inspect the device before deciding
whether another command is appropriate. Upstream error details are not returned;
numeric Matter status codes are preserved for device rejections.
