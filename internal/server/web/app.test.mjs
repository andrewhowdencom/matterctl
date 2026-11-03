import { test } from "node:test";
import assert from "node:assert/strict";
import { applyEvent, applyInventory, consumeNDJSON, deviceName, deviceType, endpointState } from "./app.mjs";

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
