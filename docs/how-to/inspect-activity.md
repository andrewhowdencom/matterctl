# Inspect household activity and pause automations

Open `http://localhost:8081/history` on the host, or use the host's LAN
address. The timeline starts with meaningful activity across the house from
the last seven days. Use the time, device, automation, kind, and outcome
filters to narrow it. Leave the time fields blank to use the seven-day
default. Select **Older entries** to page backward; records are not
automatically deleted. The open page checks for new records every few seconds.

The timeline includes changed Matter state, Switch actions, manual matterctl
controls, automation decisions and outcomes, file status changes, and known
observation gaps. Expand an entry for its IDs and details. **Show related run**
shows the rule decision and action results with the same run ID. A device
filter includes automation runs that use the device as a trigger or target.

If Den Light unexpectedly switches off while someone is still in the room,
find the light state change and nearby presence-sensor report. Check whether
the rule planned an off action and whether Matter reported the requested
state. A confirmation is a read of reported Matter state, not proof of the
physical light. An absent sensor record is not proof that the sensor saw no
one: a connection or recording gap may have hidden events. Arbitrary
Starlark conditions are not explained beyond a rule planning no actions.

The **Automations** section shows each loaded `.star` file, its rule count,
revision, and active, paused, or failed status. **Pause file** stops future
actions from every rule in that file. Inputs continue to be observed and
recorded. An action already sent may still finish. **Resume file** waits for
new triggers and does not replay events received while paused. Pause state
survives a service restart.

The CLI uses the same service API:

```sh
matterctl history list
matterctl history list --device 1 --since 2026-11-09T00:00:00Z
matterctl history list --automation den-presence.star --outcome failed
matterctl history list --before 1234 --output json
matterctl history inspect 1234
matterctl history follow
matterctl automation list
matterctl automation pause den-presence.star
matterctl automation resume den-presence.star
```

`history list` prints 50 entries per page by default and shows the cursor
for older entries. Use `--since` to reach older records. `history follow`
prints records added after it starts. The CLI can run on another LAN machine
with `--server.url`; it does not open the history database itself.

## Storage and backup

Local runs use `$XDG_STATE_HOME/matterctl/history.db` (normally
`~/.local/state/matterctl/history.db`). The installed service uses
`/var/lib/matterctl/history.db` in a writable Podman mount. There is no
automatic age or size limit. Check free disk space as the household history
grows.

For a consistent host backup, stop `matterctl.service`, copy the whole
`/var/lib/matterctl` directory, then start the service again. Restore that
directory with ownership `65532:65532` before starting the service. Paused
file state lives in the same database.

```sh
sudo systemctl stop matterctl.service
sudo cp -a /var/lib/matterctl /path/to/your/backup/
sudo systemctl start matterctl.service
```

Stopping the service pauses local automation during the backup. A database
write failure after startup does not stop already loaded automations; the
history view reports degraded recording and later marks a gap. If persisted
pause state cannot be read at startup, automations stay inactive until the
database is repaired.
