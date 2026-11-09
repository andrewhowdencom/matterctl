# Configure and preview automation rules

Place `.star` files in `${XDG_CONFIG_HOME:-$HOME/.config}/matterctl/automations`.
`matterctl serve` loads every file in lexical order at startup and executes
matching actions. The directory may be absent if no automations are enabled.
Each file registers independent rules; edits take effect after a service
restart. The configured directory can be changed with
`--automation.directory` or `MATTERCTL_AUTOMATION_DIRECTORY`.

The scroll-wheel BILRESA example binds Den Scroll Wheel to Den Light. The three
LED groups select colour, white temperature, and brightness. In any group, a
single wheel click toggles each light, a double click sets night mode at 1%
and the light's warmest colour temperature, and a long press sets it to 100%
at 4000 K. Each wheel step changes hue by 12 degrees, white
temperature by 150 K, or brightness by five percentage points. Group 3 retains
the existing brightness mapping on endpoints 7, 8, and 9. The current Den
Scroll Wheel inventory reports groups 1 and 2 on endpoints 1–3 and 4–6.
Confirm endpoint numbers with `matterctl event watch --device "Den Scroll Wheel"`
when binding another switch. To enable the example on the installed host:

```sh
task automation:example
task install
```

`task install` checks the saved programs, builds the image, mounts the host's
XDG automation directory read-only inside the container, and restarts the
service. The container reads the files at `/config/matterctl/automations`.
`task build` alone updates only the local CLI binary. An existing saved copy
is not replaced when the repository example changes. After changing a saved
program, run `./bin/matterctl automation check` and `sudo systemctl restart
matterctl.service`. To remove the example, delete its saved file and restart.
When replacing the earlier `bilresa-den.star`, remove that saved file before
restarting so both rules do not respond to the same wheel events.

To control a whole room, pass a list of `(light selector, endpoint)` pairs to
`bind_room` in [`bilresa-scroll-wheel-lights.star`](../../examples/bilresa-scroll-wheel-lights.star):

```python
bind_room("ROOM_BILRESA", [("CEILING_LIGHT", 1), ("TABLE_LIGHT", 1)])
```

Each switch and light selector may be an exact Matter label or node ID. A
separate rule is registered for each light, so one light failing does not stop
the remaining lights receiving the same event. Commands run sequentially, and
an individual single-click toggle reverses each light's current power state.
If the lights start in mixed on/off states, a single click keeps them mixed;
double click or long press brings them into the same power state. Colour
scrolling requires each light to advertise hue-and-saturation control;
temperature and
brightness scrolling likewise require their corresponding Matter commands.
Relative temperature is limited to each light's reported physical range.

For the separate two button BILRESA, copy
[`bilresa-two-button-lights.star`](../../examples/bilresa-two-button-lights.star)
to the automation directory after replacing the room placeholders. Each
`bind_room(button_selector, on_endpoint, off_endpoint, lights)` call connects
one switch to a list of lights. Additional calls can connect more rooms. Inspect
the two button switch and observe its events to identify which endpoint is On
and which is Off; do not use the scroll wheel example's endpoints. A short
release powers all listed lights on or off. Holding On powers them on at 100%
and 4000 K. Holding Off powers them on at 1% and each light's advertised
warmest colour temperature. The warmest action reads the light's physical
maximum mired value
when triggered. If the light does not support 4000 K or does not advertise its
warm limit, that temperature action reports an error in the service log.

To configure it on the installed host, first run `./bin/matterctl device list`
to find node IDs and light endpoints. Use `./bin/matterctl event watch` while
pressing each button to find its Switch event endpoint. Then:

```sh
automation_dir="${XDG_CONFIG_HOME:-$HOME/.config}/matterctl/automations"
mkdir -p "$automation_dir"
cp examples/bilresa-two-button-lights.star "$automation_dir/"
${EDITOR:-vi} "$automation_dir/bilresa-two-button-lights.star"
task install
```

Replace `ROOM_1_BILRESA`, `ROOM_1_LIGHT`, and the sample endpoint numbers in
the saved file before `task install`. Add one `bind_room(...)` call per room.
`task install` validates the saved rules, rebuilds the service, and restarts it.

The `automation check`, `replay`, and `watch` commands inspect proposed actions
without sending commands. Build a local CLI and check or preview a file:

```sh
task build
./bin/matterctl automation check
./bin/matterctl automation check examples/bilresa-scroll-wheel-lights.star
./bin/matterctl automation watch examples/bilresa-scroll-wheel-lights.star
```

`check` with no file validates every saved program. `watch` connects to the
running service and displays one line for each matching rule. You can also
record the projected event stream and replay it while editing rules:

```sh
./bin/matterctl event watch --output json > events.ndjson
# Press Ctrl-C after exercising the devices.
./bin/matterctl automation replay examples/bilresa-scroll-wheel-lights.star events.ndjson
./bin/matterctl automation replay examples/bilresa-scroll-wheel-lights.star events.ndjson --output json
```

Record from the start of a watch session: its first event is an inventory
snapshot that resolves device names and establishes occupancy state. The rule
file's labels must exactly match Matter labels. Use a node ID if a device has
no label or several nodes share the same label. `check` validates Starlark
syntax and rule registration; `watch` and `replay` also resolve devices and
endpoints against inventory. These commands do not verify that a target
advertises the commands needed to carry out an action.

For Den Light and the FP300 presence sensor, save
[`den-presence.star`](../../examples/den-presence.star) in the automation
directory. It powers the light on when occupancy begins and off when occupancy
clears. It does not change brightness or colour. The sensor's own occupancy
hold time determines how long the light remains on after the room is vacated.
This rule can turn the light off after a manual switch action if the sensor
subsequently reports unoccupied.

```sh
automation_dir="${XDG_CONFIG_HOME:-$HOME/.config}/matterctl/automations"
install -D -m 0644 examples/den-presence.star "$automation_dir/den-presence.star"
task install
```

The separate [`den.star`](../../examples/den.star) example registers three rules:

```python
on(occupied(presence, endpoint=1), daytime)
on(wheel_step(bilresa, up=7, down=8),
   lambda event: [adjust_brightness(den, event.steps * 5)])
bind_toggle(bilresa, 9, den)
```

`occupied` fires when a reported occupancy state changes from false to true;
`unoccupied` fires when it changes from true to false. An initial snapshot,
including one that is already occupied, sets the baseline without firing.
`short_release` fires as soon as the Matter Switch ShortRelease
event arrives; `short_press` is a compatible alias. It can also fire during a
double press. `single_press` and `double_press` distinguish final counts of one
and two using MultiPressComplete, so they wait for the gesture to finish. The
scroll-wheel room example uses these completed gestures so a double click does
not cause a preceding toggle.
`long_press` fires when the Switch LongPress event reaches the controller.
`wheel_step` reacts to each ShortRelease on its two direction endpoints, before
MultiPressComplete. Its `event.steps` is +1 for `up` and -1 for `down`.
The older `wheel` trigger instead waits for MultiPressComplete and uses its
reported count. That count is switch activations, not degrees of rotation or
an absolute brightness level. The endpoint mapping and five percentage points
per step in the example are choices for this particular BILRESA and light.
Fast wheel input can still queue light commands because the live runner confirms
each action before starting the next. `den.star` also shows a presence trigger
and a three-action daytime function.

Handlers return a list or tuple of planned actions. A handler can call a
function that builds actions for a reusable device pair. Available actions are
`power(light, "on"|"off")`, `toggle(light)`, `brightness(light, percent)`,
`adjust_brightness(light, percentage_points)`,
`colour_temperature(light, kelvin)`,
`adjust_colour_temperature(light, kelvin_delta)`,
`adjust_colour(light, hue_degrees)`, and
`warmest_colour_temperature(light)`. Brightness is 1–100 percent; use
`power(light, "off")` to turn a light off. Each target needs `device(...,
endpoint=...)`. `adjust_colour` turns around the hue circle and raises a
near-white saturation to full when entering colour mode. The list order
expresses the intended action order, though this is not a transaction: if an
action fails, later actions in that rule are skipped and earlier actions are
not rolled back. The live runner checks
advertised commands and reads back power, level, colour temperature, or hue after
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
