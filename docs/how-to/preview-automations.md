# Configure and preview automation rules

Place `.star` files in `${XDG_CONFIG_HOME:-$HOME/.config}/matterctl/automations`.
`matterctl serve` loads every file in lexical order at startup and executes
matching actions. The directory may be absent if no automations are enabled.
Each file registers independent rules; edits take effect after a service
restart. The configured directory can be changed with
`--automation.directory` or `MATTERCTL_AUTOMATION_DIRECTORY`.

The BILRESA example uses node 2's endpoint 9 short press to toggle Den Light
and wheel endpoints 7 and 8 to change its brightness by five percentage points
per reported step. To enable it on the installed host:

```sh
task automation:example
task install
```

`task install` checks the saved programs, builds the image, mounts the host's
XDG automation directory read-only inside the container, and restarts the
service. The container reads the files at `/config/matterctl/automations`.
`task build` alone updates only the local CLI binary. After changing a saved
program, run `./bin/matterctl automation check` and `sudo systemctl restart
matterctl.service`. To remove the example, delete its saved file and restart.

The `automation check`, `replay`, and `watch` commands inspect proposed actions
without sending commands. Build a local CLI and check or preview a file:

```sh
task build
./bin/matterctl automation check
./bin/matterctl automation check examples/bilresa-den.star
./bin/matterctl automation watch examples/bilresa-den.star
```

`check` with no file validates every saved program. `watch` connects to the
running service and displays one line for each matching rule. You can also
record the projected event stream and replay it while editing rules:

```sh
./bin/matterctl event watch --output json > events.ndjson
# Press Ctrl-C after exercising the devices.
./bin/matterctl automation replay examples/bilresa-den.star events.ndjson
./bin/matterctl automation replay examples/bilresa-den.star events.ndjson --output json
```

Record from the start of a watch session: its first event is an inventory
snapshot that resolves device names and establishes occupancy state. The rule
file's labels must exactly match Matter labels. Use a node ID if a device has
no label or several nodes share the same label. `check` validates Starlark
syntax and rule registration; `watch` and `replay` also resolve devices and
endpoints against inventory. These commands do not verify that a target
advertises the commands needed to carry out an action.

The example registers three rules:

```python
on(occupied(presence, endpoint=1), daytime)
on(wheel(bilresa, up=7, down=8),
   lambda event: [adjust_brightness(den, event.steps * 5)])
bind_toggle(bilresa, 9, den)
```

`occupied` fires when a reported occupancy state changes from false to true.
An initial snapshot, including one that is already occupied, sets the baseline
without firing. `short_press` fires on the Matter Switch ShortRelease event.
`wheel` uses the Switch MultiPressComplete count: endpoint `up` produces a
positive `event.steps`, and endpoint `down` produces a negative count. The
count is switch activations, not degrees of rotation or an absolute brightness
level. The endpoint mapping and five percentage points per step in the example
are choices for this particular BILRESA and light. The additional
[`den.star`](../../examples/den.star) example also shows a presence trigger
and a three-action daytime function.

Handlers return a list or tuple of planned actions. A handler can call a
function that builds actions for a reusable device pair. Available actions are
`power(light, "on"|"off")`, `toggle(light)`, `brightness(light, percent)`,
`adjust_brightness(light, percentage_points)`, and
`colour_temperature(light, kelvin)`. Brightness is 1–100 percent; use
`power(light, "off")` to turn a light off. Each target needs `device(...,
endpoint=...)`. The list order expresses the intended action order, though this
is not a transaction: if an action fails, later actions in that rule are
skipped and earlier actions are not rolled back. The live runner checks
advertised commands and reads back power, level, or color temperature after
each command. It does not retry an action with an uncertain outcome or resolve
conflicting rules. Runtime outcomes appear in the service log. `--output json`
emits one preview firing object per line for further inspection.

If a configured device disappears, its engine stops acting rather than
retargeting its label to another node. Restart the service after updating the
rules or commissioning state. If a script fails to load, the service keeps its
API running but disables all automations and logs the error. `task install`
checks scripts before restarting the service. The live event stream is not a
durable history; a recording only contains events observed while `event watch`
was connected.
