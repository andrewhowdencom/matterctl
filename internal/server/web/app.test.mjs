import { test } from "node:test";
import assert from "node:assert/strict";
import { applyEvent, applyInventory, beginCommission, commissionOutcome, consumeNDJSON, deviceName, deviceType, endpointState, submitCommission, validCommissionCode, validDeviceName } from "./app.mjs";

const denLight = {
  node_id: "1",
  label: "Den Light",
  available: true,
  endpoints: [{ id: 1, power: true, device_types: [{ name: "Extended Color Light" }], server_clusters: [{ id: 6 }] }],
};

test("names, types, and endpoint states reflect reported values", () => {
  assert.equal(deviceName(denLight), "Den Light");
  assert.equal(deviceType(denLight), "Extended Color Light");
  assert.equal(deviceName({ node_id: "2", product: "Lamp" }), "Lamp");
  assert.equal(deviceName({ node_id: "3" }), "Node 3");
  assert.deepEqual(endpointState(denLight.endpoints[0]), { label: "On", kind: "on" });
  assert.deepEqual(endpointState({ ...denLight.endpoints[0], power: false }), { label: "Off", kind: "off" });
  assert.deepEqual(endpointState(denLight.endpoints[0], true), { label: "Last: On", kind: "last" });
  assert.deepEqual(endpointState({ id: 1, server_clusters: [{ id: 6 }] }), { label: "Unknown", kind: "unknown" });
  assert.deepEqual(endpointState({ id: 1, client_clusters: [{ id: 6 }] }), { label: "No on/off state", kind: "none" });
  assert.deepEqual(endpointState({ id: 1, switch_position: 2 }), { label: "Position 2", kind: "none" });
});

test("snapshot and events add, update, and remove distinct devices", () => {
  let state = applyInventory({}, { ready: true, connection: { state: "connected" }, devices: [denLight] });
  assert.equal(state.status, "ready");
  state = applyEvent(state, { type: "node_added", data: { node_id: "2", label: "Other lamp", endpoints: [] } });
  assert.equal(state.inventory.devices.length, 2);
  state = applyEvent(state, { type: "attribute_updated", data: { device: { ...denLight, label: "Renamed", endpoints: [{ id: 1, power: false }] } } });
  assert.equal(state.inventory.devices[0].label, "Renamed");
  assert.equal(state.inventory.devices[0].endpoints[0].power, false);
  state = applyEvent(state, { type: "endpoint_added", data: { device: { ...denLight, endpoints: [{ id: 1, power: true }, { id: 2, power: false }] } } });
  assert.equal(state.inventory.devices[0].endpoints.length, 2);
  state = applyEvent(state, { type: "node_removed", data: { node_id: "2" } });
  assert.deepEqual(state.inventory.devices.map((device) => device.node_id), ["1"]);
  state = applyEvent(state, { type: "connection", data: { connection: { state: "disconnected" }, ready: false } });
  assert.equal(state.status, "stale");
  assert.equal(state.inventory.devices.length, 1);
  state = applyEvent(state, { type: "inventory", data: { ready: true, connection: { state: "connected" }, devices: [] } });
  assert.equal(state.status, "ready");
  assert.deepEqual(state.inventory.devices, []);
});

test("NDJSON stream handles split frames and reports interruption", async () => {
  const encoder = new TextEncoder();
  const seen = [];
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('{"type":"inventory","data":{"ready":true}}\n{"type":"node_'));
      controller.enqueue(encoder.encode('added","data":{"node_id":"2"}}\n'));
      controller.close();
    },
  });
  await assert.rejects(consumeNDJSON(stream, (event) => seen.push(event)), /ended/);
  assert.deepEqual(seen.map((event) => event.type), ["inventory", "node_added"]);

  const interrupted = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('{"type":"stream_error","data":{}}\n'));
      controller.close();
    },
  });
  await assert.rejects(consumeNDJSON(interrupted, () => {}), /interrupted/);
});

test("commission codes accept supported shapes without validating their checksum", () => {
  for (const code of ["35325335079", "3532-533-5079", "  3532 533 5079  ", "123456789012345678901", "MT:ABC123.-"]) {
    assert.equal(validCommissionCode(code), true, code);
  }
  for (const code of ["", "3532533507", "3532533507x", "mt:ABC123", "MT:", "MT:abc", "1".repeat(1025)]) {
    assert.equal(validCommissionCode(code), false, code);
  }
});

test("optional device names follow Matter label limits", () => {
  for (const name of ["", "Den Light", "é".repeat(16)]) assert.equal(validDeviceName(name), true, name);
  for (const name of [" Den Light", "Den Light ", "line\nbreak", "é".repeat(17)]) assert.equal(validDeviceName(name), false, name);
});

test("commissioning writes a supplied name after the node is known", async () => {
  const calls = [];
  let labelAttempts = 0;
  const fetchImpl = async (path, options) => {
    calls.push({ path, body: JSON.parse(options.body) });
    if (path.endsWith("/commission")) return { status: 200, json: async () => ({ node_id: "42", product: "Lamp" }) };
    if (++labelAttempts === 1) return { status: 404, json: async () => ({ error: "device not found" }) };
    return { status: 200, json: async () => ({ node_id: "42", label: "Den Light" }) };
  };
  const operation = beginCommission("35325335079", { name: "Den Light", fetchImpl });
  assert.deepEqual(await operation.result, { kind: "success", message: "Den Light was added as node 42." });
  assert.deepEqual(calls.map((call) => call.path), ["/api/v1/devices/commission", "/api/v1/devices/label", "/api/v1/devices/label"]);
  assert.deepEqual(calls[1].body, { target: "42", label: "Den Light" });
});

test("a rejected name reports the commissioned node without retrying", async () => {
  const calls = [];
  const fetchImpl = async (path) => {
    calls.push(path);
    if (path.endsWith("/commission")) return { status: 200, json: async () => ({ node_id: "42" }) };
    return { status: 422 };
  };
  const outcome = await beginCommission("35325335079", { name: "Den Light", fetchImpl }).result;
  assert.equal(outcome.kind, "naming");
  assert.match(outcome.message, /Device added as node 42/);
  assert.equal(calls.length, 2);
});

test("commission request posts one code on the same origin without following redirects", async () => {
  const calls = [];
  const result = await submitCommission("35325335079", undefined, async (path, options) => {
    calls.push({ path, options });
    return { status: 200, json: async () => ({ node_id: "18446744069414584320", label: "Lamp" }) };
  });
  assert.deepEqual(result, { status: 200, data: { node_id: "18446744069414584320", label: "Lamp" } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/v1/devices/commission");
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.headers["Content-Type"], "application/json");
  assert.equal(calls[0].options.redirect, "error");
  assert.equal(calls[0].options.credentials, "same-origin");
  assert.deepEqual(JSON.parse(calls[0].options.body), { code: "35325335079", network_only: true });
});

test("commission outcomes distinguish success, input, busy, unavailable, rejection, and uncertainty", () => {
  assert.deepEqual(commissionOutcome({ status: 200, data: { node_id: "9", label: "Lamp" } }), { kind: "success", message: "Lamp was added as node 9." });
  assert.equal(commissionOutcome({ status: 400 }).kind, "invalid");
  assert.equal(commissionOutcome({ status: 403 }).kind, "forbidden");
  assert.equal(commissionOutcome({ status: 409 }).kind, "busy");
  assert.equal(commissionOutcome({ status: 503 }).kind, "unavailable");
  assert.equal(commissionOutcome({ status: 502, data: { error: "passcode-mismatch" } }).kind, "rejected");
  assert.equal(commissionOutcome({ status: 502, data: { error: "commissioning outcome unknown" } }).kind, "uncertain");
  assert.equal(commissionOutcome({ status: 502, data: { error: 42 } }).kind, "uncertain");
  assert.equal(commissionOutcome({ status: 504 }).kind, "uncertain");
  assert.equal(commissionOutcome({ status: 200, data: {} }).kind, "uncertain");
});

test("stopping a commission wait aborts the request and reports uncertainty", async () => {
  let aborted = false;
  const fetchImpl = (_path, { signal }) => new Promise((_resolve, reject) => {
    signal.addEventListener("abort", () => { aborted = true; reject(new Error("aborted")); }, { once: true });
  });
  const operation = beginCommission("35325335079", { fetchImpl });
  operation.stop();
  assert.equal((await operation.result).kind, "uncertain");
  assert.equal(aborted, true);
});

test("a stalled commission wait reaches the browser deadline", async () => {
  const fetchImpl = (_path, { signal }) => new Promise((_resolve, reject) => {
    signal.addEventListener("abort", () => reject(new Error("timed out")), { once: true });
  });
  const operation = beginCommission("35325335079", { fetchImpl, timeoutMs: 1 });
  assert.equal((await operation.result).kind, "uncertain");
});
