# View devices in a browser

Run `matterctl serve` and open `http://localhost:8081/` on the host, or the
host's LAN address on another device. The browser page lists devices
commissioned to this Matter controller. A device that exists only in Google
Home will not appear until it is shared with this controller.

Each card shows the Matter label when available, a device type, availability,
and every supported friendly reading for its endpoints. Lights show power,
brightness, and white temperature when reported. Sensors show occupancy,
temperature, humidity, illuminance, battery level, contact, and leak state when reported.
When a sensor has no interpreted reading, its card shows up to three named raw
Boolean or numeric values. **Device details** includes all such raw readings;
their units and enum meanings may be unknown. A switch shows its latest action
seen by this browser session; after a reload it waits for the next action.
Open **Device details** for node and endpoint IDs, advertised capabilities,
and optional standard Matter endpoint tags. Switches use standard semantic
tags such as Up, Down, Left, and Right when the device advertises them.
Ordinary endpoints do not always have a human-readable name, so their type and ID appear instead. Names may
differ from Google Home nicknames.

Where an endpoint advertises the necessary commands, the card offers a power
toggle, brightness slider, white temperature slider, or hue and saturation
picker. Power with an unknown reported state offers **Turn on**. Slider and
picker changes are sent when released. Each command shows pending and confirmed
feedback, or explains when the outcome is unknown. Color uses the current
brightness setting. Controls are disabled while the device is offline or the
inventory is stale. The page does not save endpoint names or room assignments.

The page updates while open. **Offline** indicates the device's last reported
availability was false. **Last: On** or **Last: Off** identifies a retained
state for an offline device. If Matter Server disconnects or the event stream
stops, the page marks the list as stale and reconnects automatically. The
browser also offers a retry action when matterctl itself cannot be reached.
Only a synchronized, empty inventory displays **No devices yet**.

The page uses the same [inventory and event API](../reference/devices.md) as the
CLI. It shows reported Matter state; it does not perform a fresh read of each
attribute on every page load.
