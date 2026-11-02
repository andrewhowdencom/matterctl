# matterctl implementation plan

Status: agreed, 2026-11-02. The build and deployment bootstrap is complete.
Phase 1 is implemented and validated. The remaining phases are planned.

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
| Browser UI | Eventually uses the same application API as the CLI. Its current placeholder can remain during this milestone. |
| Command structure | Categories followed by verbs, including `device power on` and `device power off`. |
| Capabilities | Determine supported operations from endpoint capabilities, features, and accepted commands. Device types provide context and presentation. |
| Friendly names | Use Matter labels first, with persistent aliases held centrally by the service where needed. |
| Addressing | Friendly names are normal CLI targets. Node and endpoint IDs remain available for inspection and disambiguation. |
| Completion | Wait for an observation of the requested power state, within a bounded timeout. |
| Access | No authentication for the local service. Keep Matter Server's API restricted to loopback. |
| Scope | Focus on the user's home-control workflows. Add broader capability families when needed. |

## System boundaries

```text
CLI -------------------+
                       |
Future browser UI -----+--> matterctl service --> OHF Matter Server --> devices
                            HTTP API              WebSocket API
                            LAN, port 8081         127.0.0.1:5580/ws
```

Matter Server remains responsible for controller identity, commissioning,
protocol interactions, and device subscriptions. matterctl owns its application
API, target resolution, aliases, CLI output, and interpretation of control
results. Future automation rules will also belong to matterctl.

Keep the CLI and HTTP handlers thin. Share application operations for inventory,
target resolution, and power control. Put the upstream WebSocket protocol and
alias persistence behind small interfaces that can be exercised with realistic
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

matterctl device alias list
matterctl device alias set "Couch lamp" <node> --endpoint <endpoint>
matterctl device alias remove "Couch lamp"

matterctl device power on "Couch lamp"
matterctl device power off "Couch lamp"

matterctl event watch
matterctl event watch --device "Couch lamp"
```

Numeric node targets and `--endpoint` remain supported. An endpoint is an
independently addressable part of a node: for example, an outlet in a multi-outlet
plug or a light exposed by a bridge. With one compatible endpoint, the service
can resolve a node-level name automatically. With several possible endpoints,
require an endpoint or an alias that selects one.

Provide human-readable output and a consistent `--output json` option for
non-streaming commands. Use newline-delimited JSON for machine-readable event
streams. CLI connection settings use the existing Viper conventions; the CLI's
default service URL is `http://127.0.0.1:8081` and can be overridden for LAN use.

## Friendly-name resolution

Read the node's Matter label and applicable bridged endpoint labels. Preserve
their source and display the underlying node/endpoint identifiers in inventory
and inspection output. Names are not guaranteed to match Google Home nicknames.

An explicit alias identifies a stable node/endpoint target. Store aliases once
in the service's persistent state; every CLI and browser client uses that same
mapping. Creating an alias does not rename the device or change another
controller's labels.

Resolve a name only when it identifies one valid target. Reject ambiguity,
including an alias colliding with another target's label, and list the candidate
identifiers. Do not silently choose the first device or retarget an alias when
a device is removed or recommissioned. Allow aliases to be edited or removed
even when their target is offline.

The state directory must survive container replacement. Add a writable bind
mount, owned by the container's application user, while retaining the read-only
container filesystem. Keep this state separate from Matter Server's controller
identity and pairing data. Choose the smallest storage implementation that
supports atomic updates and restart persistence.

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

- [ ] Load the initial snapshot with `start_listening` and process updates.
- [ ] Model nodes, endpoint hierarchy, device types, cluster roles, and supported
  operations without requiring a physical device type for every operation.
- [ ] Implement `device list`, `device inspect`, and `event watch`.
- [ ] Test empty inventory, multiple endpoints, a bridge, an event-only switch,
  unsupported commands, unavailable devices, and resynchronization.
- [ ] Confirm that client event streams are canceled and released cleanly.

### 3. Commission the first existing light

- [ ] Implement discovery and network-only commissioning through Matter Server.
- [ ] Surface commissioning progress, completion, and useful upstream errors.
  Do not retain pairing codes in state or logs.
- [ ] Use a fresh Google Home sharing code when performing the real test.
- [ ] Inspect the returned light's actual endpoints and On/Off support.
- [ ] Verify the device remains usable through Google Home.

The read-only inventory check on 2026-11-02 returned zero commissioned nodes.
Real commissioning therefore precedes end-to-end light-control validation.
The selected light must support Matter directly or through an appropriate
Matter bridge; appearing in Google Home alone does not establish that.

### 4. Resolve friendly names and persist aliases

- [ ] Read applicable Matter labels and resolve unambiguous name targets.
- [ ] Implement central alias management and the container state mount.
- [ ] Test duplicate names, multiple endpoints, stale aliases, and atomic writes.
- [ ] Verify that two CLI clients see the same aliases and that aliases survive
  service restart and container replacement.

### 5. Implement confirmed on/off control

- [ ] Implement `device power on` and `device power off` with the contract above.
- [ ] Test acknowledgement before/after state reports, a missing report followed
  by a successful fresh read, stale cached state, device rejection, and timeout.
- [ ] Test upstream disconnection during a command without automatic replay.
- [ ] Verify both CLI commands against the real light using its friendly name.

### 6. Validate and document the complete workflow

- [ ] Run `task validate` and deploy using `task install`.
- [ ] Check localhost and LAN CLI access to the same service and names.
- [ ] Verify reconnect recovery, process shutdown, and persistent state after
  restart without changing Matter Server's loopback binding.
- [ ] Add a how-to for sharing and controlling the first light, plus a command
  reference covering output, deadlines, and failure meanings.
- [ ] Record the actual device model, observed capabilities, and validation
  results without storing sharing codes or network credentials.

## Deferred work

- Browser controls beyond the current placeholder page.
- Button-to-light automation, schedules, scenes, rules, and activity history.
- `power toggle`, which requires particular care around retries and unknown
  outcomes; the first milestone uses explicit on/off operations.
- Brightness, color, coverings, locks, and other friendly capability families.
- Device-label writes, sharing onward to another controller, and decommissioning.
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
