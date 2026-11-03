# Browser interface requirements

This is the specification for the first browser screen. It lets someone open matterctl on
the home LAN and see the devices commissioned to its Matter Server fabric and
their reported state. It does not discover every Matter device on the LAN or
automatically import devices that exist only in Google Home.

## Device list: first increment

### User outcome

On opening `/`, I can tell which devices matterctl knows about, whether each is
available, and whether a device with a reported On/Off state is on or off. The
list continues to work when a second device is added without changing the page.

### What the page shows

- Show one entry per commissioned node, identified internally by its string
  `node_id`. Use the Matter label as the primary name when present, then the
  product name, then `Node <id>`. Show the node ID as secondary text so duplicate
  or missing names are distinguishable. Matter labels may differ from Google
  Home names.
- Show the advertised device type when available in a human-readable form.
  Where a node has multiple meaningful endpoints, show their labels or endpoint
  IDs and states within that node's entry. Do not collapse several power states
  into one misleading node-wide On/Off value.
- For an endpoint with a known `power` value, show **On** or **Off**. If it has
  a server-side OnOff cluster but no reported `power` value, show **Unknown**.
  Devices without an On/Off state, such as an event-only button, do not get an
  **Off** label merely because `power` is absent.
- Show **Offline** separately when the device's last reported `available` is
  false. Any retained power value is explicitly marked as last reported, not
  presented as the device's current state.
- Make the page usable on a phone and a desktop. Names, state, and availability
  remain readable without opening an inspector or understanding Matter cluster
  IDs.

### Loading and changes

- Read the initial list from `GET /api/v1/devices`. Show a loading state until
  that request completes. A ready inventory with `devices: []` gets an empty
  state saying that no devices are commissioned to this controller.
- Keep the list current while the page is open as devices are added, removed,
  renamed, or report new state or availability. The existing unfiltered
  `GET /api/v1/events` stream provides an initial or replacement inventory and
  node updates. Reconcile by `node_id` and endpoint ID, not by display name.
- When Matter Server disconnects, inventory is still synchronizing, or the
  event stream is interrupted, show a clear connection or stale-data notice.
  Previously displayed states must not look live. Recover by obtaining a fresh
  inventory snapshot; a blank page must not be mistaken for an empty network.
- When the HTTP service itself cannot be reached, show an error with a retry
  action. Do not show the commissioned-devices empty state for this case.

### Acceptance examples

| Situation | Expected result |
| --- | --- |
| Den Light is the only ready, available node and endpoint 1 reports `power: true` | One entry named Den Light, with node ID 1 and **On** |
| Den Light reports `power: false` | The same entry changes to **Off** without a page reload |
| A second node is commissioned | A second, separate entry appears without a code or configuration change |
| An OnOff endpoint has no readable power value | **Unknown**, not **Off** |
| A button has no OnOff server state | Its entry remains visible without an invented On/Off state |
| A device becomes unavailable | **Offline** is visible; retained power is described as last reported |
| Matter Server disconnects after a successful load | Existing entries are visibly stale until a replacement inventory arrives |
| A synchronized snapshot contains no nodes | A dedicated no-devices message appears |

## Existing application contract

The browser uses matterctl's HTTP API, shared with the CLI. The inventory
response already contains `ready`, `connection`, `devices`, `available`, node and
endpoint labels, endpoint device types, optional `power`, and observation times.
The event stream supplies inventory replacements and projected node changes.
See the [device API reference](docs/reference/devices.md) for the exact fields
and event shapes. These are cached Matter reports; an observation timestamp does
not prove every attribute was freshly read at that moment.

## Later decisions

The viewing increment above is implemented. On/off controls, device inspection,
brightness, colour, button event history, and automation controls can be
specified separately. The existing power API can support controls when their
interaction and confirmation feedback are defined.

## Add device: commissioning increment

On the device page, **Add device** opens an inline form for a fresh manual
sharing code or pasted `MT:` payload from an already-networked Matter device.
The form is available only while inventory is ready. It submits once to
`POST /api/v1/devices/commission` with `network_only: true`; it does not retain
the code, follow redirects, or retry automatically. There is no camera scan or
discovery list. Success identifies the new node and refreshes the inventory.
The waiting state offers **Stop waiting**. Stopping, a connection failure, or a
timeout can leave the outcome unknown; the page directs the user to inspect the
device list before another attempt. Input errors, another active commission,
upstream rejection, and Matter Server unavailability have distinct feedback.
