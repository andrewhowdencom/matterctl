# Discovery and commissioning

The browser and CLI can request an optional device name. After commissioning
returns the new node ID, matterctl writes the name as the node's Matter
`NodeLabel` and confirms it with a fresh read. If naming fails, the device
remains commissioned and the caller sees its node ID to retry `device label
set`. The CLI prompts for a name on a terminal in text mode, or accepts
`--name <name>` in any mode. JSON mode does not prompt. The browser form has an
optional name field. Names must follow the 32 UTF-8 byte Matter label limit.

Both commands call the running matterctl HTTP service. They require its current
Matter Server session to have synchronized inventory. `--server.url` and
`--output text|json` work as on other device commands.

The browser device page also has **Add device** for network-only commissioning
with a fresh manual sharing code or pasted `MT:` payload. The button is disabled
while inventory is unavailable or synchronizing. The browser does not scan for
devices or use the camera. The API uses a five-minute default deadline; the
browser allows 15 more seconds for a response and refreshes inventory after a
result. **Stop waiting** cancels the browser request, but the outcome may
remain unknown; check the device list before trying again.

## Commands

| Command | Behavior |
| --- | --- |
| `device discover` | A fresh scan of commissionable network advertisements; does not commission anything |
| `device commission --code-stdin --network-only` | Read one sharing code through stdin until EOF and wait for network-only commissioning |
| `device commission --code '<code>'` | Same operation, with the code in process arguments; prefer stdin |

Commissioning accepts an 11- or 21-digit manual code (spaces and hyphens are
removed), or a single `MT:` QR payload. Matter Server validates the payload and
checksum. Pairing codes have no configuration or environment setting and are not
retained in matterctl state, logs, traces, or event broadcasts. The upstream must
receive the code to commission the device; its own logging is managed separately.

`--network-only` defaults to true; false is rejected. Fresh-device Bluetooth
commissioning and Wi-Fi/Thread credential provisioning are not exposed.

The commissioning `--timeout` is an overall wait, from 1 second to 10 minutes,
default 5 minutes. Discovery has a 30-second deadline. These operations use their
own deadlines rather than the ordinary `request.timeout` default of 5 seconds;
the HTTP client allows two additional seconds to receive a timeout response.
Caller cancellation can shorten either wait.

Progress on stderr reports waiting and confirmed completion. The installed
upstream provides a final result/error, without detailed stage events; matterctl
does not infer intermediate commissioning stages. A successful result on stdout
uses the same device projection as `device inspect`. Inventory and watch updates
continue through the existing subscription during commissioning.

## HTTP API

| Method and path | Result |
| --- | --- |
| `GET /api/v1/devices/discover` | `{ "observed_at": "<RFC3339 timestamp>", "devices": [...] }` |
| `POST /api/v1/devices/commission` | Commissioned `Device` object, with node ID encoded as a decimal string |

Discovery entries contain `instance_name`, `device_name`, and optional
`vendor_id`, `product_id`, `device_type`, `long_discriminator`, and
`commissioning_mode`. They are advertisements, not inventory nodes or stable
friendly names. Unknown upstream fields and rotating identifiers are dropped.

Commission requests require `Content-Type: application/json`, one object, and a
maximum body size of 4096 bytes. Unknown fields are rejected. Example shape:

```json
{"code":"<fresh-sharing-code>","network_only":true,"timeout_ms":300000}
```

`timeout_ms` is optional (default 300000), or an integer from 1000 to 600000.
Codes belong only in the request body, never in URLs. Clients must not follow
redirects or retry a commission request automatically. The supplied Go client
does neither. All responses use `Cache-Control: no-store`.
Browser requests to the commission endpoint must have an Origin matching the
request's scheme and Host, or receive HTTP 403. Requests without Origin remain
valid for non-browser clients.

Errors use `{ "error": "..." }` and a non-success status:

| HTTP status | Meaning |
| --- | --- |
| 400 | Invalid input, code shape, timeout, or unsupported commissioning mode |
| 409 | Another commissioning request is currently waiting on this service |
| 502 | Upstream rejection, invalid response, or another upstream failure |
| 503 | Upstream unavailable or inventory not synchronized |
| 504 | Operation deadline exceeded |

Upstream error codes and the allowlisted commissioning reasons are safe to
display: `not-discovered`, `passcode-mismatch`, `invalid-payload`,
`attestation-rejected`, `timeout`, and `other`. Arbitrary upstream details are
discarded. Local timeout, disconnection, or a malformed success response after
sending a commission request can leave the outcome unknown. Client transport
failure also cannot establish whether the server received or completed it.
An error does not prove the device was left unchanged. Inspect inventory before
retrying. Canceling the HTTP request stops waiting; the upstream protocol does
not guarantee cancellation of commissioning itself. A service restart also loses
any in-flight wait; commissioning is never replayed after reconnect or restart.

The service rejects overlapping commissioning waits but does not claim that
cancellation stopped an upstream attempt. After an uncertain result, the caller
must check the device and inventory before explicitly trying again.

The adapter uses `discover` and `commission_with_code` with `network_only:true`,
available in the supported schema range 11–14. The reference wire types are in
[Matter Server v1.5.5](https://github.com/matter-js/matterjs-server/blob/v1.5.5/packages/ws-client/src/models/model.ts).
