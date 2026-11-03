# matterctl implementation plan

Status: implemented and technically validated, 2026-11-02. Real-device
commissioning, Matter label writing, and named on/off commands succeeded.
Physical light response and continued Google Home operation await observation.

## First milestone

Use matterctl to commission one existing Matter light, inspect its capabilities,
give it a usable friendly name, and turn it on and off through the CLI. Observe
its state and events through the same service that will eventually support the
browser UI.

The milestone is complete when a real light responds to both commands and the
CLI confirms the device's reported state. A successful build or a command
acknowledgement alone does not complete it.

## Agreed decisions

| Area | Decision |
| --- | --- |
| Execution | CLI requests go through the running matterctl service. The service owns the Matter Server connection. |
| Browser UI | Uses the same application API as the CLI. The first screen is a read-only device list. |
| Command structure | Categories followed by verbs, including `device power on` and `device power off`. |
| Capabilities | Determine supported operations from endpoint capabilities, features, and accepted commands. Device types provide context and presentation. |
| Friendly names | Use writable Matter NodeLabel attributes; use node IDs when names are unavailable or ambiguous. |
| Addressing | Friendly names are normal CLI targets. Node and endpoint IDs remain available for inspection and disambiguation. |
| Completion | Wait for an observation of the requested power state, within a bounded timeout. |
| Access | No authentication for the local service. Keep Matter Server's API restricted to loopback. |
| Scope | Focus on the user's home-control workflows. Add broader capability families when needed. |

## System boundaries

```text
CLI -------------------+
                       |
Browser UI ------------+--> matterctl service --> OHF Matter Server --> devices
                            HTTP API              WebSocket API
                            LAN, port 8081         127.0.0.1:5580/ws
```

Matter Server remains responsible for controller identity, commissioning,
protocol interactions, and device subscriptions. matterctl owns its application
API, target resolution, CLI output, and interpretation of control
results. Future automation rules will also belong to matterctl.

Keep the CLI and HTTP handlers thin. Share application operations for inventory,
target resolution, and power control. Put the upstream WebSocket protocol and
label writes behind small interfaces that can be exercised with realistic
fixtures. Introduce packages as these responsibilities appear; a public Go
library or plugin framework is not a milestone requirement.

The service must expose upstream connection health separately from HTTP process
health. An HTTP listener can be running while Matter Server is unreachable.

## Proposed first-release commands

The category/verb structure is agreed. The supporting command spellings below
are the working design to use when implementing help and API documentation.

```sh
matterctl serve
matterctl server info

matterctl device list
matterctl device inspect <target>
matterctl device discover
matterctl device commission --code '<sharing-code>' --network-only

matterctl device label set <node> "Couch lamp"

matterctl device power on "Couch lamp"
matterctl device power off "Couch lamp"

matterctl event watch
matterctl event watch --device "Couch lamp"
```

Numeric node targets and `--endpoint` remain supported. An endpoint is an
independently addressable part of a node: for example, an outlet in a multi-outlet
plug or a light exposed by a bridge. With one compatible endpoint, the service
can resolve a node-level name automatically. With several possible endpoints,
require an endpoint ID.

Provide human-readable output and a consistent `--output json` option for
non-streaming commands. Use newline-delimited JSON for machine-readable event
streams. CLI connection settings use the existing Viper conventions; the CLI's
default service URL is `http://127.0.0.1:8081` and can be overridden for LAN use.

## Friendly-name resolution

Read the node's Matter label and applicable bridged endpoint labels. Display
node and endpoint identifiers in inventory and inspection output. Names are not
guaranteed to match Google Home nicknames.

Write a node's Basic Information `NodeLabel`, or a bridged endpoint's own
`NodeLabel` when that cluster is present. Confirm a successful write with a
fresh read before reporting success. Matter Server and the device own label
persistence; matterctl keeps no service-owned alias state.

Resolve a name only when it identifies one node. Reject ambiguity across nodes
and keep numeric node and endpoint IDs available for disambiguation. A label on
an ordinary light endpoint is unsupported because it has no bridged label
attribute.

## Power-control contract

- Resolve the target and confirm that its endpoint accepts the requested
  On/Off cluster command. A device-type name alone is insufficient.
- Keep observation active before issuing the command, so an immediate state
  report is not missed. Track upstream request IDs and responses independently
  from asynchronous events.
- Confirm success using a post-request report or a fresh read performed after
  command acknowledgement. An old cached value cannot confirm a new request.
- Use a configurable overall timeout of 10 seconds for initial power
  control. Propagate the remaining deadline through the CLI and service.
- Distinguish device rejection, unsupported capability, unavailable upstream,
  ambiguous target, and state-confirmation timeout. Return nonzero on an
  unconfirmed result and explain whether the command was acknowledged.
- A timeout or disconnection can leave the outcome unknown; do not claim that
  the light stayed unchanged or automatically replay an uncertain operation.
- Show availability and observation freshness with cached state. Reconnect and
  reload inventory after an upstream disconnection before accepting new control
  requests. An offline target's stale state cannot confirm success.

The confirmation contract verifies reported Matter state. The final manual test
also checks that the physical light follows it.

## Delivery sequence

Each phase should be a reviewable pull request, with user documentation updated
alongside the behavior. Dependencies run from top to bottom; fixture-based
tests can be written before a physical device is commissioned.

### 1. Connect the service to Matter Server

- [x] Add the upstream WebSocket session, response correlation, cancellation,
  schema compatibility check, and bounded reconnect behavior.
- [x] Add the CLI-to-service API client and `server info`.
- [x] Report upstream connection state and version without exposing secrets.
- [x] Verify initial connection, disconnect, reconnect, and shutdown using a
  controlled upstream peer, then read information from the installed server.

Validated on 2026-11-02 with `task validate` and `task install`. Live CLI checks
through localhost and the host's LAN address reported Matter Server 1.5.5,
schema 14. The container's CLI also succeeded; Matter Server remained bound to
IPv4 loopback. Failure and recovery cases were exercised with controlled peers.

### 2. Inspect inventory and observe changes

- [x] Load the initial snapshot with `start_listening` and process updates.
- [x] Model nodes, endpoint hierarchy, device types, cluster roles, and supported
  operations without requiring a physical device type for every operation.
- [x] Implement `device list`, `device inspect`, and `event watch`.
- [x] Test empty inventory, multiple endpoints, a bridge, an event-only switch,
  unsupported commands, unavailable devices, and resynchronization.
- [x] Confirm that client event streams are canceled and released cleanly.

Phase 2 passed `task validate` and a live read-only subscription check on
2026-11-02. Matter Server's inventory was synchronized and empty. The CLI
reported a missing target correctly, streamed its initial inventory, and exited
cleanly on Ctrl-C. Controlled peers covered populated devices, bridges, switch
events, reconnect replacement, and slow-subscriber cleanup. Labels can already
select a node for inspection; label writing is covered in phase 4.
`task install` deployed the container; the same checks passed through localhost,
the LAN address, and the container CLI. Matter Server remained loopback-only.

### 3. Commission the first existing light

- [x] Implement discovery and network-only commissioning through Matter Server.
- [x] Surface commissioning progress, completion, and useful upstream errors.
  Do not retain pairing codes in state or logs.
- [x] Use a fresh Google Home sharing code when performing the real test.
- [x] Inspect the returned light's actual endpoints and On/Off support.
- [ ] Verify the device remains usable through Google Home.

The first real commission on 2026-11-02 added a Govee H60A6 light (reported
vendor Shenzhen Qianyan Technology) as node 1. Endpoint 1 advertises On/Off,
LevelControl, and ColorControl. Google Home operation after sharing still needs
manual confirmation.

Phase 3 adds `device discover` and `device commission --code-stdin --network-only`,
with safe error reasons, bounded waits, and no automatic retries. The CLI reports
waiting and final completion; the installed API has no detailed commissioning
stage events. Controlled peers cover capability projection, code privacy, late
replies, cancellation, and continuing inventory updates. HTTP tests cover long
operation deadlines, rejected bodies, redirects, and cancellation. Physical
sharing and inspection succeeded; Google Home verification remains pending.
See [the sharing how-to](docs/how-to/commission-device.md).

On 2026-11-02, `task validate` passed lint, race tests, and the build, and
`task install` deployed the commands. Live discovery succeeded through localhost,
the LAN address, and the container CLI, with zero advertisements found. Inventory
was synchronized and empty; event streaming and Ctrl-C cleanup still passed.
Matter Server remained bound to `127.0.0.1:5580`.

### 4. Resolve friendly names and write Matter labels

- [x] Read applicable Matter labels and resolve unambiguous name targets.
- [x] Add a device label write with a checked Matter status and fresh-read
  confirmation for node and bridged endpoint labels.
- [x] Remove application alias management and its container state mount.
- [x] Validate a label write and named target against the real Govee light.

The Govee H60A6 is node 1. Its root endpoint advertises Basic Information
`NodeLabel`; endpoint 1 is an ordinary light endpoint, so the node label is
written at `0/40/5`. Matter labels are owned by the device and may differ from
Google Home's display name.

The light accepted `Govee H60A6`, confirmed by a fresh read. Named inspection
and power commands succeeded. The label still resolved after restarting
matterctl without any service-owned alias state.
On 2026-11-03, the user identified node 1 as the Den Light and explicitly
approved writing `Den Light` as its Matter label. The write and fresh read
succeeded. Its endpoint 1 independently advertises device type 269,
Extended Color Light, revision 1; a projection bug that displayed type 0 was
corrected.

### 5. Implement confirmed on/off control

- [x] Add `device power on` and `device power off` with supported endpoint
  selection and fresh-read confirmation.
- [x] Complete the remaining failure and reconnect cases in the contract above.
- [x] Test acknowledgement before/after state reports, a missing report followed
  by a successful fresh read, stale cached state, device rejection, and timeout.
- [x] Test upstream disconnection during a command without automatic replay.
- [x] Verify both CLI commands against the real light using node and endpoint IDs.
- [x] Verify named on/off after a confirmed Matter label write.

On 2026-11-02, `device power on 1 --endpoint 1` returned a fresh-read
confirmation. Subsequently, `on`, `off`, and `on` commands each returned
fresh-read confirmation for the same endpoint through an application alias
that has since been removed at the user's request. Physical illumination remains
to be confirmed by the user.

### 6. Validate and document the complete workflow

- [x] Run `task validate` and deploy using `task install`.
- [x] Check localhost and LAN CLI access to the same service.
- [x] Verify reconnect recovery and process shutdown without changing Matter
  Server's loopback binding.
- [x] Add a how-to for sharing and controlling the first light, plus a command
  reference covering output, deadlines, and failure meanings.
- [x] Record the actual device model, observed capabilities, and validation
  results without storing sharing codes or network credentials.

`task validate` passed lint, race tests, and build. `task install` deployed the
service without its former writable alias mount. A service restart resynchronized
node 1 and retained its device-owned label. The Matter Server API still listened
only on `127.0.0.1:5580` in the earlier network check.

## Deferred work

- Browser controls beyond the read-only device list.
- Button-to-light automation, schedules, scenes, rules, and activity history.
- `power toggle`, which requires particular care around retries and unknown
  outcomes; the first milestone uses explicit on/off operations.
- Brightness, color, coverings, locks, and other friendly capability families.
- Sharing onward to another controller and decommissioning.
- General attribute writes, raw command invocation, bindings, ACLs, fabric
  administration, firmware, Thread diagnostics, cameras, and ICD management.
- Fresh-device Bluetooth commissioning and Wi-Fi/Thread credential management.
- Automatic synchronization with Google Home nicknames.
- Direct CLI connections to Matter Server, bypassing matterctl's service.
- Authentication and remote-access features beyond the agreed local deployment.

## References

- [Matter Server v1.5.5 WebSocket API](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/websockets_api.md)
- [Matter Server v1.5.5 CLI and subscription behavior](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/cli.md)
- [Matter Server's node-label writes](https://github.com/matter-js/matterjs-server/blob/v1.5.5/packages/dashboard/src/util/node-label.ts)
- [Matter Basic Information labels](https://github.com/project-chip/connectedhomeip/blob/master/data_model/1.4/clusters/BasicInformationCluster.xml)
- [Matter bridged-device labels](https://github.com/project-chip/connectedhomeip/blob/master/data_model/1.4/clusters/bridge-clusters-BridgedDeviceBasicInformationCluster.xml)
