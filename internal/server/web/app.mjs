const initialState = () => ({ inventory: null, status: "loading" });

export function applyInventory(state, inventory) {
  return {
    inventory,
    status: inventory.ready && inventory.connection?.state === "connected" ? "ready" : "stale",
  };
}

export function applyEvent(state, event) {
  if (event.type === "inventory") return applyInventory(state, event.data);
  if (!state.inventory) return state;

  if (event.type === "connection") {
    return {
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
  return { ...state, inventory: { ...state.inventory, devices: updated } };
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

function card(device, stale) {
  const offline = device.available === false;
  const unavailable = stale || offline;
  const article = element("article", `device-card${stale ? " is-stale" : ""}`);
  const top = element("div", "card-top");
  const icon = element("span", "device-icon");
  icon.setAttribute("aria-hidden", "true");
  icon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18h6M10 21h4M8.5 15.5a6 6 0 1 1 7 0c-.9.6-1.5 1.4-1.5 2.5h-4c0-1.1-.6-1.9-1.5-2.5Z"/></svg>';
  const availability = element("span", `availability${stale ? " stale" : offline ? " offline" : ""}`,
    stale ? (offline ? "Offline · stale" : "Data stale") : offline ? "Offline" : "Available");
  top.append(icon, availability);
  article.append(top, element("h3", "device-name", deviceName(device)), element("p", "device-type", deviceType(device)));

  const list = element("div", "endpoint-list");
  const endpoints = (device.endpoints || []).filter((endpoint) => endpoint.id !== 0 ||
    typeof endpoint.power === "boolean" || typeof endpoint.switch_position === "number");
  if (!endpoints.length) {
    list.append(element("div", "endpoint-row", "No reported state"));
  } else {
    for (const endpoint of endpoints) {
      const row = element("div", "endpoint-row");
      const name = endpoint.label?.trim() || `Endpoint ${endpoint.id}`;
      const state = endpointState(endpoint, unavailable);
      row.append(element("span", "endpoint-name", name), element("span", `state state-${state.kind}`, state.label));
      list.append(row);
    }
  }
  article.append(list, element("p", "device-id", `Node ${device.node_id}`));
  return article;
}

function render(state) {
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
    grid.replaceChildren(...ordered.map((device) => card(device, stale)));
  }
}

async function start() {
  let state = initialState();
  let closed = false;
  render(state);

  async function refresh() {
    try {
      const response = await fetch("/api/v1/devices", { cache: "no-store" });
      if (response.status !== 200 && response.status !== 503) throw new Error("Inventory request failed");
      state = applyInventory(state, await response.json());
    } catch {
      state = { ...state, status: "unreachable" };
    }
    render(state);
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
        state = applyEvent(state, event);
        render(state);
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
      render(state);
    }
    if (closed) break;
    await new Promise((resolve) => setTimeout(resolve, 1500));
    await refresh();
  }
}

if (typeof document !== "undefined") start();
