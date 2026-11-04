const initialState = () => ({ inventory: null, status: "loading" });

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

  function renderAll() {
    render(state);
    addButton.disabled = state.status !== "ready" || commissionPending;
    submitButton.disabled = state.status !== "ready" || commissionPending;
    nameInput.disabled = commissionPending;
    closeButton.disabled = commissionPending;
    stopButton.hidden = !commissionPending || commissionedNodeID !== null;
    status.hidden = !commissionMessage;
    status.className = `commission-status${commissionMessage ? ` commission-${commissionMessage.kind}` : ""}`;
    status.textContent = commissionMessage?.message || "";
  }

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
        state = applyEvent(state, event);
        renderAll();
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
