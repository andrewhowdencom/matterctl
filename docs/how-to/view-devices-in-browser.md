# View devices in a browser

Run `matterctl serve` and open `http://localhost:8081/` on the host, or the
host's LAN address on another device. The browser page lists devices
commissioned to this Matter controller. A device that exists only in Google
Home will not appear until it is shared with this controller.

Each card shows the Matter label when available, a device type, node ID,
availability, and reported endpoint state. An On/Off endpoint shows **On** or
**Off**; **Unknown** means its On/Off state has not been reported. An event-only
button can appear without an On/Off state. Multi-endpoint devices show their
states separately. Names may differ from Google Home nicknames.

The page updates while open. **Offline** indicates the device's last reported
availability was false. **Last: On** or **Last: Off** identifies a retained
state for an offline device. If Matter Server disconnects or the event stream
stops, the page marks the list as stale and reconnects automatically. The
browser also offers a retry action when matterctl itself cannot be reached.
Only a synchronized, empty inventory displays **No devices yet**.

The page uses the same [inventory and event API](../reference/devices.md) as the
CLI. It shows reported Matter state; it does not perform a fresh read of each
attribute on every page load.
