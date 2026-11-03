# Preview automation rules

The `automation` commands load a Starlark file and show proposed actions. They
do not send commands to devices. Build a local CLI and check the example:

```sh
task build
./bin/matterctl automation check examples/den.star
./bin/matterctl automation watch examples/den.star
```

`watch` connects to the running matterctl service and displays one line for
each matching rule. You can also record the projected event stream and replay
it while editing rules:

```sh
./bin/matterctl event watch --output json > events.ndjson
# Press Ctrl-C after exercising the devices.
./bin/matterctl automation replay examples/den.star events.ndjson
./bin/matterctl automation replay examples/den.star events.ndjson --output json
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
are choices for this particular BILRESA and light.

Handlers return a list or tuple of planned actions. A handler can call a
function that builds actions for a reusable device pair. Available actions are
`power(light, "on"|"off")`, `toggle(light)`, `brightness(light, percent)`,
`adjust_brightness(light, percentage_points)`, and
`colour_temperature(light, kelvin)`. Each target needs `device(...,
endpoint=...)`. The list order expresses the intended action order, though this
preview has no execution, state reads, retry behavior, or conflict handling.
`--output json` emits one firing object per line for further inspection.

If a configured device disappears, the preview stops rather than retargeting
its label to another node. Restart it after updating the rules or commissioning
state. The live event stream is not a durable history; a recording only contains
events observed while `event watch` was connected.
