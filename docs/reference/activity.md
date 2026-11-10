# Household history and automation status

The CLI and browser use these service endpoints. All timestamps are UTC in
JSON. Text output uses the client's local time. Node IDs are strings; history
IDs are positive integers.

## History

`GET /api/v1/history` returns `{entries, next, recording}`. Entries are
newest first by default. The response has at most 100 entries; `next` is
the last ID on a page when older entries remain. Request the next page with
`before=next`. With `after=ID`, entries are oldest first so clients can
follow newly stored records.

| Query | Meaning |
| --- | --- |
| `since`, `until` | RFC3339 bounds; `since` defaults to seven days ago |
| `device` | Matter node ID, including related trigger/target runs |
| `file` | Exact automation file name |
| `kind`, `outcome` | Exact history kind or result |
| `run` | Exact causal automation run ID |
| `before`, `after` | Exclusive history ID cursor; cannot be combined |
| `limit` | Page size from 1 to 100; default 50 |

`GET /api/v1/history/{id}` returns one entry or 404. Each entry has a
durable `id`, `observed_at`, `kind`, `summary`, and optional
`node_id`, `related_nodes`, `device_label`, `endpoint`, `file`,
`revision`, `rule`, `run_id`, `outcome`, and projected `details`.
The service stores no raw Matter frames, pairing codes, network credentials,
or HTTP request bodies in history.

Kinds include `device.baseline`, `device.state`, `device.switch`,
`device.availability`, `device.removed`, `manual.power`,
`manual.light`, `automation.file`, `automation.rule`,
`automation.action`, `automation.error`, `automation.pause`,
`automation.stream_gap`, `connection`, and `observation.gap`.
`recording.state` is `healthy` or `degraded`; degraded responses
include the start time and a short error. Known gaps do not account for
events Matter Server may have dropped before matterctl observed them.

## Automations

`GET /api/v1/automations` returns an array of `{name, revision, rules,
status, paused, error}` objects. `status` is `active`, `paused`, or
`failed`. A malformed file is reported as failed while healthy files can
continue running. If the pause database is unavailable at startup, the
list reports a global failure and no automation executes.

`PUT /api/v1/automations/{file}/pause` accepts exactly one JSON object:

```json
{"paused": true}
```

`false` resumes the whole file. The response is the updated file object.
Unknown files return 404. The operation is idempotent and acknowledges a
change only after pause state is durably saved. Browser requests from another
Origin are rejected; LAN clients without an Origin header can use the API.
