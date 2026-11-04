const initialState = () => ({ inventory: null, status: "loading", lastActions: {} });

export function validCommissionCode(value) {
  const code = value.trim();
  if (!code || code.length > 1024) return false;
  if (code.startsWith("MT:")) return /^MT:[0-9A-Z.-]+$/.test(code);
  const digits = code.replace(/[ -]/g, "");
  return (digits.length === 11 || digits.length === 21) && /^[0-9]+$/.test(digits);
}

export function validDeviceName(value) {
  return value === "" || (value === value.trim() && !/[\p{Cc}]/u.test(value) && new TextEncoder().encode(value).length <= 32);
}

export async function submitCommission(code, signal, fetchImpl = fetch) {
  const response = await fetchImpl("/api/v1/devices/commission", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code, network_only: true }),
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
    signal,
  });
  let data;
  try {
    data = await response.json();
  } catch {
    data = null;
  }
  return { status: response.status, data };
}

export async function submitLabel(nodeId, name, signal, fetchImpl = fetch) {
  const response = await fetchImpl("/api/v1/devices/label", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ target: nodeId, label: name }),
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
    signal,
  });
  let data;
  try {
    data = await response.json();
  } catch {
    data = null;
  }
  return { status: response.status, data };
}

async function labelAfterCommission(nodeId, name, signal, fetchImpl) {
  const deadline = Date.now() + 5000;
  for (;;) {
    const result = await submitLabel(nodeId, name, signal, fetchImpl);
    if (result.status !== 404 || Date.now() >= deadline || signal.aborted) return result;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
}

export function commissionOutcome(result, naming) {
  if (result.status === 200 && typeof result.data?.node_id === "string" && result.data.node_id) {
    if (naming && naming.status !== 200) {
      return { kind: "naming", message: `Device added as node ${result.data.node_id}, but its name could not be confirmed. Use the device label set command with this node ID to name it later.` };
    }
    if (naming?.status === 200) {
      return { kind: "success", message: `${naming.name} was added as node ${result.data.node_id}.` };
    }
    return { kind: "success", message: `${deviceName(result.data)} was added as node ${result.data.node_id}.` };
  }
  if (result.status === 400) return { kind: "invalid", message: "That sharing code was not accepted. Check it or obtain a fresh code." };
  if (result.status === 403) return { kind: "forbidden", message: "This page is not allowed to add devices from its current address. Open matterctl directly on the local network." };
  if (result.status === 409) return { kind: "busy", message: "Another device is being added. Check the device list before trying again." };
  if (result.status === 503) return { kind: "unavailable", message: "Matter Server is unavailable or synchronizing. Wait for the connection to recover." };
  if (result.status === 502 && typeof result.data?.error === "string" &&
    !result.data.error.includes("commissioning outcome unknown")) {
    return { kind: "rejected", message: "Matter Server rejected this attempt. Check the device and sharing window, then check the device list before trying again." };
  }
  return { kind: "uncertain", message: "The result is unknown. Matter Server may still be adding the device. Check the device list before trying again." };
}

export function beginCommission(code, { name = "", fetchImpl = fetch, timeoutMs = 5 * 60 * 1000 + 15 * 1000, onCommissioned = () => {} } = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const result = submitCommission(code, controller.signal, fetchImpl)
    .then(async (commission) => {
      if (!name || commission.status !== 200 || !commission.data?.node_id) return commissionOutcome(commission);
      onCommissioned(commission.data.node_id);
      let naming;
      try {
        naming = await labelAfterCommission(commission.data.node_id, name, controller.signal, fetchImpl);
        if (naming.status === 200 && (naming.data?.node_id !== commission.data.node_id || naming.data?.label !== name)) naming = { status: 0 };
      } catch {
        naming = { status: 0 };
      }
      return commissionOutcome(commission, { ...naming, name });
    }, () => commissionOutcome({ status: 0 }))
    .finally(() => clearTimeout(timer));
  return { result, stop: () => { clearTimeout(timer); controller.abort(); } };
}

export function applyInventory(state, inventory) {
  const nodes = new Set((inventory.devices || []).map((device) => device.node_id));
  return {
    inventory,
    status: inventory.ready && inventory.connection?.state === "connected" ? "ready" : "stale",
    lastActions: Object.fromEntries(Object.entries(state.lastActions || {}).filter(([key]) => nodes.has(key.split("/")[0]))),
  };
}

export function applyEvent(state, event) {
  if (event.type === "inventory") return applyInventory(state, event.data);
  if (!state.inventory) return state;

  if (event.type === "node_event" && event.data?.cluster_id === 59 && event.node_id) {
    const key = `${event.node_id}/${event.data.endpoint_id}`;
    return { ...state, lastActions: { ...state.lastActions, [key]: { label: switchAction(event.data), at: event.observed_at } } };
  }

  if (event.type === "connection") {
    return {
	  ...state,
      inventory: { ...state.inventory, connection: event.data.connection, ready: event.data.ready },
      status: "stale",
    };
  }

  const devices = state.inventory.devices || [];
  let updated;
  if (event.type === "node_removed") {
    updated = devices.filter((device) => device.node_id !== event.data.node_id);
  } else if (event.type === "node_added" || event.type === "node_updated") {
    updated = replaceDevice(devices, event.data);
  } else if (event.type === "attribute_updated" || event.type === "endpoint_added" || event.type === "endpoint_removed") {
    updated = replaceDevice(devices, event.data.device);
  } else {
    return state;
  }
  const lastActions = event.type === "node_removed"
    ? Object.fromEntries(Object.entries(state.lastActions || {}).filter(([key]) => !key.startsWith(`${event.data.node_id}/`)))
    : state.lastActions;
  return { ...state, lastActions, inventory: { ...state.inventory, devices: updated } };
}

function replaceDevice(devices, incoming) {
  if (!incoming?.node_id) return devices;
  const found = devices.some((device) => device.node_id === incoming.node_id);
  if (!found) return [...devices, incoming];
  return devices.map((device) => device.node_id === incoming.node_id ? incoming : device);
}

export function deviceName(device) {
  return device.label?.trim() || device.product?.trim() || `Node ${device.node_id}`;
}

export function deviceType(device) {
  const endpoints = (device.endpoints || []).filter((endpoint) => endpoint.id !== 0);
  const type = endpoints.flatMap((endpoint) => endpoint.device_types || []).find((item) => item.name);
  return type?.name || device.product?.trim() || (device.is_bridge ? "Matter bridge" : "Matter device");
}

export function deviceRole(device) {
  const endpoints = (device.endpoints || []).filter((endpoint) => endpoint.id !== 0);
  const types = endpoints.flatMap((endpoint) => endpoint.device_types || []).map((type) => type.name?.toLowerCase() || "");
  const clusters = endpoints.flatMap((endpoint) => endpoint.server_clusters || []).map((cluster) => cluster.id);
  if (device.is_bridge) return "bridge";
  if (clusters.includes(59) || types.some((name) => /switch|button|remote/.test(name))) return "switch";
  if (clusters.some((id) => [1024, 1026, 1029, 1030].includes(id)) || types.some((name) => /sensor|detector/.test(name))) return "sensor";
  if (types.some((name) => /outlet|plug/.test(name))) return "outlet";
  if (types.some((name) => /light|lamp/.test(name)) || clusters.includes(768) || clusters.includes(8)) return "light";
  return "device";
}

export function endpointName(endpoint) {
  if (endpoint.label?.trim()) return endpoint.label.trim();
  const named = (endpoint.labels || []).find((label) => ["name", "label", "button"].includes(label.name?.toLowerCase()) && label.value?.trim());
  if (named) return named.value.trim();
  const tags = endpoint.tags || [];
  const custom = tags.find((tag) => tag.namespace === 67 && tag.tag === 8 && tag.label?.trim());
  const column = tags.find((tag) => tag.namespace === 8 && tag.tag === 6 && tag.label?.trim())?.label.trim();
  const direction = tags.find((tag) => tag.namespace === 67 && (tag.tag === 3 || tag.tag === 4));
  if (custom && column && custom.label.toLowerCase() === "rotary" && direction) return `Rotary ${column} ${semanticTagName(direction)} · ${endpoint.id}`;
  if (custom && column && custom.label.toLowerCase() === "button") return `Button ${column} · ${endpoint.id}`;
  if (custom && !["rotary", "button"].includes(custom.label.toLowerCase())) return `${custom.label[0].toUpperCase()}${custom.label.slice(1)} · ${endpoint.id}`;
  const semantic = [...tags.filter((tag) => tag.namespace === 67), ...tags.filter((tag) => tag.namespace !== 67)].map(semanticTagName).find((name) => name);
  if (semantic) return `${semantic} · ${endpoint.id}`;
  const type = (endpoint.device_types || []).find((item) => item.name && !/^(root node|aggregator)$/i.test(item.name));
  return type ? `${type.name} · ${endpoint.id}` : `Endpoint ${endpoint.id}`;
}

export function semanticTagName(tag) {
  if (tag.namespace === 67) {
    const names = ["On", "Off", "Toggle", "Up", "Down", "Next", "Previous", "Select"];
    return tag.tag === 8 ? tag.label?.trim() || "Custom button" : names[tag.tag] || "";
  }
  if (tag.namespace === 8) {
    const name = ["Left", "Right", "Top", "Bottom", "Middle", "Row", "Column"][tag.tag] || "";
    return name && tag.label?.trim() ? `${name} ${tag.label.trim()}` : name;
  }
  return "";
}

export function switchAction(event) {
  const names = {
    initial_press: "Pressed", long_press: "Held", short_release: "Released",
    long_release: "Released after hold", multi_press_ongoing: "Pressed again", multi_press_complete: "Multiple presses",
  };
  const label = names[event.event_name] || (event.event_name || "Action").replaceAll("_", " ");
  const count = event.data?.numberOfPressesCounted;
  return event.event_name === "multi_press_complete" && count > 1 ? `${count} presses` : label;
}

export function endpointReadings(endpoint, lastAction, unavailable = false) {
  const readings = [];
  const add = (name, value) => readings.push({ name, value: unavailable ? `Last: ${value}` : value });
  if (typeof endpoint.power === "boolean") add("Power", endpoint.power ? "On" : "Off");
  else if ((endpoint.server_clusters || []).some((cluster) => cluster.id === 6)) add("Power", "Unknown");
  if (typeof endpoint.level === "number") add("Brightness", `${Math.round(endpoint.level * 100 / 254)}%`);
  if (typeof endpoint.color_temperature === "number") add("White", `${endpoint.color_temperature} K`);
  for (const state of endpoint.states || []) {
    if (["power", "level", "switch_position", "color_temperature"].includes(state.name)) continue;
    if (["occupancy", "temperature", "illuminance", "humidity", "battery"].includes(state.name)) add(state.name[0].toUpperCase() + state.name.slice(1), state.text);
  }
  if ((endpoint.server_clusters || []).some((cluster) => cluster.id === 59) || typeof endpoint.switch_position === "number") {
    add("Last action", lastAction?.label || "Waiting for an action");
  }
  return readings;
}

export function endpointState(endpoint, unavailable = false) {
  let label;
  let kind;
  if (typeof endpoint.power === "boolean") {
    label = endpoint.power ? "On" : "Off";
    kind = unavailable ? "last" : (endpoint.power ? "on" : "off");
  } else if ((endpoint.server_clusters || []).some((cluster) => cluster.id === 6) ||
    (endpoint.capabilities || []).some((capability) => capability.startsWith("power."))) {
    label = "Unknown";
    kind = "unknown";
  } else if (typeof endpoint.switch_position === "number") {
    label = `Position ${endpoint.switch_position}`;
    kind = unavailable ? "last" : "none";
  } else {
    label = "No on/off state";
    kind = "none";
  }
  if (unavailable && kind !== "unknown" && label !== "No on/off state") label = `Last: ${label}`;
  return { label, kind };
}

export async function consumeNDJSON(stream, onEvent, onFrame = () => {}) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let pending = "";
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      onFrame();
      pending += decoder.decode(value, { stream: true });
      if (pending.length > 16 * 1024 * 1024) throw new Error("Event stream frame is too large");
      let end;
      while ((end = pending.indexOf("\n")) !== -1) {
        const line = pending.slice(0, end).trim();
        pending = pending.slice(end + 1);
        if (!line) continue;
        const event = JSON.parse(line);
        if (event.type === "stream_error") throw new Error("Event stream interrupted");
        onEvent(event);
      }
    }
    // EOF is never a clean completion for this continuously open stream.
    throw new Error("Event stream ended");
  } finally {
    reader.releaseLock();
  }
}

function element(tag, className, content) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (content !== undefined) node.textContent = content;
  return node;
}

function emptyState(title, detail, retry = false) {
  const box = element("div", "empty-state");
  box.append(element("h3", "", title), element("p", "", detail));
  if (retry) box.append(retryButton());
  return box;
}

function retryButton() {
  const button = element("button", "retry-button", "Retry connection");
  button.type = "button";
  button.addEventListener("click", () => window.location.reload());
  return button;
}

const icons = {
  light: '<circle cx="12" cy="10" r="5"/><path d="M9 16h6m-5 3h4m-3 2h2"/>',
  switch: '<rect x="4" y="4" width="16" height="16" rx="4"/><path d="M9 8v8m6-8v8"/>',
  sensor: '<circle cx="12" cy="12" r="2"/><path d="M7 7a7 7 0 0 0 0 10m10-10a7 7 0 0 1 0 10M4 4a11 11 0 0 0 0 16m16-16a11 11 0 0 1 0 16"/>',
  outlet: '<rect x="4" y="3" width="16" height="18" rx="4"/><path d="M9 9v3m6-3v3m-6 5h6"/>',
  bridge: '<rect x="3" y="4" width="7" height="7" rx="2"/><rect x="14" y="4" width="7" height="7" rx="2"/><rect x="8.5" y="15" width="7" height="6" rx="2"/><path d="M6.5 11v2h11v-2M12 13v2"/>',
  device: '<rect x="4" y="4" width="16" height="16" rx="4"/><circle cx="12" cy="12" r="3"/>',
};

function actionMessage(result, response) {
  if (result.status === 200) return { kind: "success", message: "Confirmed by the device" };
  if (result.status === 504 || response?.error?.includes("outcome may be unknown") || response?.error?.includes("not confirmed")) {
    return { kind: "uncertain", message: "Outcome unknown. Check the reported state before trying again." };
  }
  if (result.status === 503) return { kind: "error", message: "Device or Matter Server unavailable." };
  if (result.status === 409) return { kind: "error", message: "Another command is in progress." };
  if (result.status === 422) return { kind: "error", message: "This operation is not supported by the endpoint." };
  return { kind: "error", message: "The device did not accept the command." };
}

function card(device, stale, lastActions, ui, sendControl) {
  const offline = device.available === false;
  const unavailable = stale || offline;
  const article = element("article", `device-card${unavailable ? " is-stale" : ""}`);
  article.dataset.nodeId = device.node_id;
  const top = element("div", "card-top");
  const role = deviceRole(device);
  const icon = element("span", `device-icon icon-${role}`);
  icon.setAttribute("aria-hidden", "true");
  icon.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${icons[role]}</svg>`;
  const availability = element("span", `availability${stale ? " stale" : offline ? " offline" : ""}`,
    stale ? (offline ? "Offline · stale" : "Data stale") : offline ? "Offline" : "Available");
  top.append(icon, availability);
  article.append(top, element("h3", "device-name", deviceName(device)), element("p", "device-type", deviceType(device)));

  const list = element("div", "endpoint-list");
  const endpoints = (device.endpoints || []).filter((endpoint) => endpoint.id !== 0 ||
    typeof endpoint.power === "boolean" || typeof endpoint.switch_position === "number" || (endpoint.states || []).length);
  if (!endpoints.length) {
    list.append(element("div", "endpoint-row", "No reported state"));
  } else {
    for (const endpoint of endpoints) {
      const row = element("section", "endpoint-row");
      const key = `${device.node_id}/${endpoint.id}`;
      row.append(element("h4", "endpoint-name", endpointName(endpoint)));
      const last = lastActions[key];
      const readings = endpointReadings(endpoint, last, unavailable);
      if (readings.length) {
        const values = element("dl", "readings");
        for (const reading of readings) {
          values.append(element("dt", "", reading.name), element("dd", "", reading.value));
        }
        row.append(values);
      } else row.append(element("p", "no-state", "No reported state"));
      if (last?.at) row.append(element("p", "action-time", `Last action at ${new Date(last.at).toLocaleTimeString()}`));
      const capabilities = endpoint.capabilities || [];
      const controls = element("div", "endpoint-controls");
      const disabled = unavailable || ui.pending[key];
      const powerOn = capabilities.includes("power.on");
      const powerOff = capabilities.includes("power.off");
      if (powerOn || powerOff) {
        const known = typeof endpoint.power === "boolean";
        const target = known ? !endpoint.power : true;
        const button = element("button", "power-toggle", known ? (endpoint.power ? "On" : "Off") : "Turn on");
        button.type = "button";
        button.disabled = disabled || !(target ? powerOn : powerOff);
        if (known) {
          button.setAttribute("role", "switch");
          button.setAttribute("aria-checked", String(endpoint.power));
          button.setAttribute("aria-label", `${endpointName(endpoint)} power`);
        }
        button.addEventListener("click", () => sendControl(device, endpoint, "power", { action: target ? "on" : "off" }));
        controls.append(button);
      }
      for (const item of [
        { capability: "brightness.set", action: "brightness", name: "Brightness", min: 1, max: 100, value: typeof endpoint.level === "number" ? Math.round(endpoint.level * 100 / 254) : 50, suffix: "%" },
        { capability: "color.temperature", action: "temperature", name: "White temperature", min: Math.max(1000, endpoint.color_temperature_min || 1000), max: Math.min(10000, endpoint.color_temperature_max || 10000), value: endpoint.color_temperature || 4000, suffix: " K" },
      ]) {
        if (!capabilities.includes(item.capability)) continue;
        if (item.min > item.max) continue;
        const label = element("label", "range-control", `${item.name}: `);
        const value = ui.drafts[`${key}/${item.action}`] ?? Math.max(item.min, Math.min(item.max, item.value));
        const output = element("output", "", `${value}${item.suffix}`);
        const slider = element("input");
        slider.type = "range";
        slider.min = String(item.min);
        slider.max = String(item.max);
        slider.value = String(value);
        slider.disabled = disabled;
        slider.addEventListener("focus", () => { ui.activeControl = key; });
        slider.addEventListener("blur", () => {
          ui.activeControl = null;
          if (ui.deferredRender) { ui.deferredRender = false; queueMicrotask(ui.render); }
        });
        slider.addEventListener("input", () => { ui.drafts[`${key}/${item.action}`] = Number(slider.value); output.textContent = `${slider.value}${item.suffix}`; });
        slider.addEventListener("change", () => sendControl(device, endpoint, item.action, { value: Number(slider.value) }));
        label.append(output, slider);
        controls.append(label);
      }
      if (capabilities.includes("color.hue_saturation") || capabilities.includes("color.xy")) {
        const label = element("label", "color-control", "Hue and saturation ");
        const input = element("input");
        input.type = "color";
        input.value = ui.drafts[`${key}/color`] || matterColor(endpoint);
        input.disabled = disabled;
        input.addEventListener("focus", () => { ui.activeControl = key; });
        input.addEventListener("blur", () => {
          ui.activeControl = null;
          if (ui.deferredRender) { ui.deferredRender = false; queueMicrotask(ui.render); }
        });
        input.addEventListener("input", () => { ui.drafts[`${key}/color`] = input.value; });
        input.addEventListener("change", () => sendControl(device, endpoint, "color", { color: input.value }));
        label.append(input);
        controls.append(label);
        controls.append(element("p", "control-help", "Brightness stays as set above."));
      }
      if (controls.childNodes.length) row.append(controls);
      const feedback = ui.feedback[key];
      if (ui.pending[key] || feedback) {
        row.append(element("p", `control-feedback feedback-${ui.pending[key] ? "pending" : feedback.kind}`, ui.pending[key] ? "Waiting for device confirmation…" : feedback.message));
      }
      list.append(row);
    }
  }
  article.append(list);
  const details = element("details", "device-details");
  details.open = ui.open.has(device.node_id);
  details.addEventListener("toggle", () => { if (details.open) ui.open.add(device.node_id); else ui.open.delete(device.node_id); });
  details.append(element("summary", "", "Device details"));
  const facts = element("dl", "detail-facts");
  for (const [name, value] of [["Node ID", device.node_id], ["Matter label", device.label || "None"], ["Product", device.product || "Unknown"], ["Vendor", device.vendor || "Unknown"]]) {
    facts.append(element("dt", "", name), element("dd", "", value));
  }
  details.append(facts);
  for (const endpoint of endpoints) {
    const block = element("div", "endpoint-detail");
    block.append(element("h5", "", `Endpoint ${endpoint.id}`));
    block.append(element("p", "", `Type: ${(endpoint.device_types || []).map((type) => type.name || type.id).join(", ") || "Unknown"}`));
    block.append(element("p", "", `Capabilities: ${(endpoint.capabilities || []).join(", ") || "None advertised"}`));
    if (typeof endpoint.switch_position === "number") block.append(element("p", "", `Last reported position: ${endpoint.switch_position}`));
    for (const label of endpoint.labels || []) block.append(element("p", "", `${label.name}: ${label.value}`));
    for (const tag of endpoint.tags || []) block.append(element("p", "", `Matter tag: ${semanticTagName(tag) || `${tag.namespace}:${tag.tag}`}${tag.label ? ` (${tag.label})` : ""}`));
    details.append(block);
  }
  article.append(details, element("p", "device-id", `Node ${device.node_id}`));
  return article;
}

function matterColor(endpoint) {
  if ((endpoint.color_mode === 1 || typeof endpoint.hue !== "number") && typeof endpoint.color_x === "number" && typeof endpoint.color_y === "number") {
    const x = endpoint.color_x / 65535;
    const y = endpoint.color_y / 65535;
    if (y <= 0) return "#ffffff";
    const X = x / y, Z = (1 - x - y) / y;
    const linear = [3.2406 * X - 1.5372 - .4986 * Z, -0.9689 * X + 1.8758 + .0415 * Z, .0557 * X - .2040 + 1.0570 * Z];
    const maximum = Math.max(...linear, 1);
    const rgb = linear.map((value) => {
      const normalized = Math.max(0, value / maximum);
      return Math.round(255 * (normalized <= .0031308 ? 12.92 * normalized : 1.055 * normalized ** (1 / 2.4) - .055));
    });
    return `#${rgb.map((value) => value.toString(16).padStart(2, "0")).join("")}`;
  }
  if (typeof endpoint.hue !== "number" || typeof endpoint.saturation !== "number") return "#ffffff";
  const h = endpoint.hue / 254 * 360;
  const s = endpoint.saturation / 254;
  const f = (n) => {
    const k = (n + h / 30) % 12;
    return Math.round(255 * (1 - s * Math.max(Math.min(k - 3, 9 - k, 1), -1)));
  };
  return `#${[f(0), f(8), f(4)].map((value) => value.toString(16).padStart(2, "0")).join("")}`;
}

function render(state, ui, sendControl) {
  const connection = document.querySelector("#connection");
  const count = document.querySelector("#device-count");
  const notice = document.querySelector("#notice");
  const grid = document.querySelector("#devices");
  const inventory = state.inventory;
  const devices = inventory?.devices || [];
  const stale = state.status !== "ready";

  const messages = {
    loading: ["connection-loading", "Connecting…"],
    ready: ["", "Live"],
    stale: ["connection-stale", "Matter Server unavailable"],
    unreachable: ["connection-error", "matterctl unreachable"],
  };
  const [className, message] = messages[state.status];
  connection.className = `connection ${className}`.trim();
  connection.querySelector("span:last-child").textContent = message;
  count.textContent = inventory ? String(devices.length) : "—";
  grid.setAttribute("aria-busy", state.status === "loading" ? "true" : "false");

  notice.hidden = !stale || state.status === "loading";
  if (!notice.hidden) {
    const message = state.status === "unreachable"
      ? "Cannot reach matterctl. Any devices below show their last reported state. Reconnecting automatically."
      : "Matter Server is disconnected or synchronizing. Any devices below show their last reported state. Reconnecting automatically.";
    notice.replaceChildren(element("span", "", message));
    if (state.status === "unreachable" && devices.length) notice.append(retryButton());
  }

  if (state.status === "loading") {
    grid.replaceChildren(emptyState("Loading devices", "Waiting for the current Matter inventory."));
  } else if (!inventory || !devices.length && stale) {
    grid.replaceChildren(emptyState("Waiting for devices", "The controller is unavailable or still synchronizing. Try again shortly.", true));
  } else if (!devices.length) {
    grid.replaceChildren(emptyState("No devices yet", "No devices are commissioned to this Matter controller."));
  } else {
    const ordered = [...devices].sort((a, b) => {
      const left = BigInt(a.node_id);
      const right = BigInt(b.node_id);
      return left < right ? -1 : left > right ? 1 : 0;
    });
    grid.replaceChildren(...ordered.map((device) => card(device, stale, state.lastActions || {}, ui, sendControl)));
  }
}

async function start() {
  let state = initialState();
  const ui = { open: new Set(), pending: {}, feedback: {}, drafts: {}, activeControl: null, deferredRender: false };
  let closed = false;
  let commissionPending = false;
  let commissionedNodeID = null;
  let commissionAttempt = 0;
  let commissionOperation;
  let commissionMessage = null;
  const addButton = document.querySelector("#add-device");
  const panel = document.querySelector("#commission-panel");
  const form = document.querySelector("#commission-form");
  const codeInput = document.querySelector("#commission-code");
  const nameInput = document.querySelector("#commission-name");
  const submitButton = document.querySelector("#commission-submit");
  const stopButton = document.querySelector("#commission-stop");
  const closeButton = document.querySelector("#commission-close");
  const revealButton = document.querySelector("#commission-reveal");
  const status = document.querySelector("#commission-status");

  async function sendControl(device, endpoint, kind, parameters) {
    const key = `${device.node_id}/${endpoint.id}`;
    if (ui.pending[key] || state.status !== "ready" || device.available === false) return;
    ui.pending[key] = true;
    delete ui.feedback[key];
    renderAll();
    let result;
    let response;
    try {
      const path = kind === "power" ? "/api/v1/devices/power" : "/api/v1/devices/light";
      const body = kind === "power"
        ? { target: device.node_id, endpoint: endpoint.id, action: parameters.action }
        : { target: device.node_id, endpoint: endpoint.id, action: kind, ...parameters };
      const reply = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), credentials: "same-origin", cache: "no-store", redirect: "error" });
      result = { status: reply.status };
      response = await reply.json().catch(() => null);
    } catch {
      result = { status: 0 };
      response = { error: "outcome may be unknown" };
    }
    delete ui.pending[key];
    ui.feedback[key] = actionMessage(result, response);
    for (const action of ["brightness", "temperature", "color"]) delete ui.drafts[`${key}/${action}`];
    await refresh();
  }

  function renderAll() {
    render(state, ui, sendControl);
    addButton.disabled = state.status !== "ready" || commissionPending;
    submitButton.disabled = state.status !== "ready" || commissionPending;
    nameInput.disabled = commissionPending;
    closeButton.disabled = commissionPending;
    stopButton.hidden = !commissionPending || commissionedNodeID !== null;
    status.hidden = !commissionMessage;
    status.className = `commission-status${commissionMessage ? ` commission-${commissionMessage.kind}` : ""}`;
    status.textContent = commissionMessage?.message || "";
  }
  ui.render = renderAll;

  function clearCode() {
    codeInput.value = "";
    codeInput.type = "password";
    revealButton.textContent = "Show";
    revealButton.setAttribute("aria-pressed", "false");
  }

  addButton.addEventListener("click", () => {
    panel.hidden = false;
    addButton.setAttribute("aria-expanded", "true");
    codeInput.focus();
  });
  closeButton.addEventListener("click", () => {
    panel.hidden = true;
    addButton.setAttribute("aria-expanded", "false");
    commissionMessage = null;
    clearCode();
    nameInput.value = "";
    renderAll();
    addButton.focus();
  });
  revealButton.addEventListener("click", () => {
    const visible = codeInput.type === "password";
    codeInput.type = visible ? "text" : "password";
    revealButton.textContent = visible ? "Hide" : "Show";
    revealButton.setAttribute("aria-pressed", String(visible));
  });
  stopButton.addEventListener("click", () => {
    if (!commissionPending) return;
    commissionAttempt++;
    commissionPending = false;
    commissionOperation.stop();
    commissionMessage = commissionOutcome({ status: 0 });
    renderAll();
    status.focus();
    void refresh();
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (commissionPending || state.status !== "ready") return;
    const code = codeInput.value.trim();
    const name = nameInput.value;
    if (!validCommissionCode(code)) {
      commissionMessage = { kind: "invalid", message: "Enter an 11- or 21-digit manual code, or a complete MT: QR payload." };
      renderAll();
      codeInput.focus();
      return;
    }
    if (!validDeviceName(name)) {
      commissionMessage = { kind: "invalid", message: "Use a name of at most 32 UTF-8 bytes, without leading or trailing spaces or control characters." };
      renderAll();
      nameInput.focus();
      return;
    }
    const attempt = ++commissionAttempt;
    commissionPending = true;
    commissionedNodeID = null;
    const operation = beginCommission(code, { name, onCommissioned: (nodeID) => {
      if (attempt !== commissionAttempt) return;
      commissionedNodeID = nodeID;
      commissionMessage = { kind: "waiting", message: `Device added as node ${nodeID}. Confirming its name…` };
      renderAll();
      status.focus();
    } });
    commissionOperation = operation;
    commissionMessage = { kind: "waiting", message: "Waiting for Matter Server to add the device. This can take up to five minutes." };
    clearCode();
    renderAll();
    stopButton.focus();
    const outcome = await operation.result;
    if (attempt !== commissionAttempt) return;
    commissionPending = false;
    commissionMessage = outcome;
    if (outcome.kind === "success") nameInput.value = "";
    renderAll();
    status.focus();
    await refresh();
  });

  renderAll();

  async function refresh() {
    try {
      const response = await fetch("/api/v1/devices", { cache: "no-store" });
      if (response.status !== 200 && response.status !== 503) throw new Error("Inventory request failed");
      state = applyInventory(state, await response.json());
    } catch {
      state = { ...state, status: "unreachable" };
    }
    renderAll();
  }

  async function listen() {
    const controller = new AbortController();
    let watchdog;
    const resetWatchdog = () => {
      clearTimeout(watchdog);
      watchdog = setTimeout(() => controller.abort(), 45000);
    };
    const close = () => { closed = true; controller.abort(); };
    window.addEventListener("pagehide", close, { once: true });
    try {
      resetWatchdog();
      const response = await fetch("/api/v1/events", { cache: "no-store", signal: controller.signal });
      if (!response.ok || !response.body) throw new Error("Event stream unavailable");
      await consumeNDJSON(response.body, (event) => {
        const next = applyEvent(state, event);
        if (next !== state) {
          state = next;
          if (ui.activeControl && next.status === "ready") ui.deferredRender = true;
          else renderAll();
        }
      }, resetWatchdog);
    } finally {
      clearTimeout(watchdog);
      controller.abort();
      window.removeEventListener("pagehide", close);
    }
  }

  await refresh();
  while (!closed) {
    try {
      await listen();
    } catch {
      if (closed) break;
      state = { ...state, status: state.inventory ? "stale" : "unreachable" };
      renderAll();
    }
    if (closed) break;
    await new Promise((resolve) => setTimeout(resolve, 1500));
    await refresh();
  }
}

if (typeof document !== "undefined") start();
