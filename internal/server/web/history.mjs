const timeline = document.getElementById("timeline");
const filters = document.getElementById("filters");
const older = document.getElementById("older");
const recording = document.getElementById("recording");
const historyError = document.getElementById("history-error");
const filesContainer = document.getElementById("automation-files");
const filesError = document.getElementById("automations-error");

let currentFilters = {};
let nextCursor = 0;
let newestID = 0;
let loading = false;

async function api(path, options = {}) {
  const response = await fetch(path, { cache: "no-store", ...options });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `Request failed (HTTP ${response.status})`);
  return data;
}

function showError(element, message) {
  element.textContent = message || "";
  element.hidden = !message;
}

function filterValues() {
  const form = new FormData(filters);
  const query = {};
  for (const key of ["device", "file", "kind", "outcome"]) {
    const value = String(form.get(key) || "").trim();
    if (value) query[key] = value;
  }
  for (const key of ["since", "until"]) {
    const value = String(form.get(key) || "");
    if (value) query[key] = new Date(value).toISOString();
  }
  return query;
}

function historyURL(extra = {}) {
  const query = new URLSearchParams({ ...currentFilters, ...extra });
  return `/api/v1/history?${query}`;
}

function formatTime(value) {
  return new Date(value).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" });
}

function entryElement(entry) {
  const li = document.createElement("li");
  li.dataset.id = String(entry.id);
  const details = document.createElement("details");
  const summary = document.createElement("summary");
  const time = document.createElement("time");
  time.dateTime = entry.observed_at;
  time.textContent = formatTime(entry.observed_at);
  const kind = document.createElement("span");
  kind.className = "history-kind";
  kind.textContent = entry.kind.replaceAll(".", " ");
  const message = document.createElement("span");
  message.textContent = entry.summary;
  summary.append(time, kind, message);
  details.append(summary);

  const body = document.createElement("div");
  body.className = "history-detail";
  const pre = document.createElement("pre");
  pre.textContent = JSON.stringify({
    id: entry.id, outcome: entry.outcome || undefined,
    node_id: entry.node_id || undefined, related_nodes: entry.related_nodes,
    file: entry.file || undefined, revision: entry.revision || undefined,
    rule: entry.rule || undefined, run_id: entry.run_id || undefined,
    details: entry.details,
  }, null, 2);
  body.append(pre);
  if (entry.run_id) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "text-button";
    button.textContent = "Show related run";
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        const page = await api(`/api/v1/history?run=${encodeURIComponent(entry.run_id)}&since=1970-01-01T00%3A00%3A00Z&limit=100`);
        const run = document.createElement("ol");
        for (const item of [...page.entries].reverse()) {
          const row = document.createElement("li");
          row.textContent = `${formatTime(item.observed_at)} · ${item.summary}`;
          run.append(row);
        }
        button.replaceWith(run);
      } catch (error) {
        button.disabled = false;
        showError(historyError, error.message);
      }
    });
    body.append(button);
  }
  details.append(body);
  li.append(details);
  return li;
}

function renderPage(page, mode) {
  recording.textContent = page.recording?.state === "healthy"
    ? "Recording"
    : `Recording ${page.recording?.state || "unknown"}: ${page.recording?.last_error || "events may be missing"}`;
  if (mode === "replace") {
    timeline.replaceChildren();
    newestID = 0;
  }
  for (const entry of page.entries || []) {
    if (timeline.querySelector(`[data-id="${entry.id}"]`)) continue;
    const element = entryElement(entry);
    if (mode === "prepend") timeline.prepend(element);
    else timeline.append(element);
    newestID = Math.max(newestID, entry.id);
  }
  if (mode !== "prepend") {
    nextCursor = page.next || 0;
    older.hidden = !nextCursor;
  }
  if (Array.from(timeline.children).some((child) => child.dataset.id)) {
    for (const child of Array.from(timeline.children)) {
      if (!child.dataset.id) child.remove();
    }
  } else if (!timeline.children.length) {
    const empty = document.createElement("li");
    empty.textContent = "No meaningful events in this time window.";
    timeline.append(empty);
  }
}

async function loadHistory(mode = "replace") {
  if (loading) return;
  loading = true;
  showError(historyError, "");
  try {
    const extra = { limit: "50" };
    if (mode === "older") extra.before = String(nextCursor);
    const page = await api(historyURL(extra));
    renderPage(page, mode === "older" ? "append" : "replace");
  } catch (error) {
    recording.textContent = "Recording unavailable";
    showError(historyError, error.message);
  } finally {
    loading = false;
  }
}

async function pollHistory() {
  if (loading) return;
  try {
    const extra = { limit: "100" };
    if (newestID) extra.after = String(newestID);
    const page = await api(historyURL(extra));
    if (page.entries?.length) {
      renderPage(page, newestID ? "prepend" : "replace");
    } else {
      recording.textContent = page.recording?.state === "healthy"
        ? "Recording" : `Recording ${page.recording?.state || "unknown"}`;
    }
  } catch (error) {
    recording.textContent = "Recording unavailable";
    showError(historyError, error.message);
  }
}

async function loadDevices() {
  try {
    const inventory = await api("/api/v1/devices");
    const list = document.getElementById("known-devices");
    for (const device of inventory.devices || []) {
      const option = document.createElement("option");
      option.value = device.node_id;
      option.label = device.label || device.product || `Node ${device.node_id}`;
      list.append(option);
    }
  } catch {
    // Historical queries remain usable while Matter Server is unavailable.
  }
}

async function loadAutomations() {
  showError(filesError, "");
  try {
    const files = await api("/api/v1/automations");
    filesContainer.replaceChildren();
    const fileFilter = filters.elements.namedItem("file");
    const selection = fileFilter.value;
    fileFilter.replaceChildren(new Option("All files", ""));
    for (const file of files) {
      fileFilter.add(new Option(file.name, file.name));
      const card = document.createElement("article");
      card.className = "automation-file";
      const text = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = file.name;
      const status = document.createElement("p");
      status.textContent = `${file.status} · ${file.rules} rules`;
      const detail = document.createElement("small");
      detail.textContent = file.error || (file.revision ? `Revision ${file.revision}` : "");
      text.append(title, status, detail);
      const button = document.createElement("button");
      button.type = "button";
      button.className = "text-button";
      button.textContent = file.paused ? "Resume file" : "Pause file";
      button.addEventListener("click", async () => {
        button.disabled = true;
        try {
          await api(`/api/v1/automations/${encodeURIComponent(file.name)}/pause`, {
            method: "PUT", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ paused: !file.paused }),
          });
          await loadAutomations();
          await pollHistory();
        } catch (error) {
          button.disabled = false;
          showError(filesError, error.message);
        }
      });
      card.append(text, button);
      filesContainer.append(card);
    }
    if (!files.length) filesContainer.textContent = "No automation files loaded.";
    fileFilter.value = selection;
  } catch (error) {
    showError(filesError, error.message);
  }
}

filters.addEventListener("submit", (event) => {
  event.preventDefault();
  try {
    currentFilters = filterValues();
    nextCursor = 0;
    loadHistory();
  } catch {
    showError(historyError, "Enter valid dates.");
  }
});
older.addEventListener("click", () => { if (nextCursor) loadHistory("older"); });
document.getElementById("refresh-automations").addEventListener("click", loadAutomations);
await Promise.all([loadHistory(), loadAutomations(), loadDevices()]);
setInterval(pollHistory, 5000);
