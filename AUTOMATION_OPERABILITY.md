# Automation operability proposal

Status: proposed.

## Problem

Automation rules are loaded from Starlark files when the service starts. A rule
change requires a service restart, and live execution outcomes appear only in
the service log. `automation check`, `watch`, and `replay` help inspect a proposed
rule, but they do not show which version is active or what happened when the
service ran it. This makes routine changes and diagnosis harder than they need
to be for a small, local controller.

## User outcome

After changing a rule, I can check and activate it without restarting the
service. When an automation does or does not act as expected, I can see which
rules are active, which recent signals reached them, what actions they planned,
and what happened to those actions.

## Proposed behavior

- Show whether automation processing is active and connected to the device
  event stream. List the active rule files, their rule counts, when they were
  loaded, and any most recent load error.
- Keep a bounded, recent history of normalized signals, including ones that
  matched no rule. For each match, show the file and rule, planned actions,
  targets, timestamp, evaluation errors, and execution outcomes. Distinguish a
  confirmed Matter report from a physical observation; distinguish a failed or
  uncertain action from later actions that were skipped.
- Provide an explicit reload operation for the configured rule directory. Check
  every candidate file before replacing the active set. If checking fails, keep
  the previous rules running and report the error. On success, activate the new
  set together so each incoming event is handled by one rule set. Do not replay
  old events or fire merely because an initial inventory snapshot contains an
  occupied device.
- Make status, recent outcomes, and reload available through the service and a
  readable CLI command. The browser can use the same service data if a browser
  view is useful.

## Acceptance examples

1. A presence rule runs and powers a light on. The recent history shows the
   signal, rule, requested action, and confirmed reported state.
2. A device is unavailable or an action outcome is uncertain. The history shows
   that outcome and identifies any subsequent actions the rule skipped.
3. A switch event arrives but matches no rule. The recent history shows that it
   reached the automation engine and that no rule matched.
4. An edited file contains an error. Reload reports the error, and the previous
   active rules continue to respond to new events.
5. A corrected file reloads without a service restart. Status identifies the
   new active set, and reloading while a room is already occupied does not
   trigger a new arrival action.

This proposal addresses the effort of changing and diagnosing automations.
The ease of expressing a new automation is a separate part of the project's
complexity problem and needs its own evidence.
