import assert from "node:assert/strict";
import test from "node:test";

import { createRuntimeEventBinder } from "./runtime-events.js";

test("waits for the Wails event runtime and binds listeners exactly once", async () => {
  const listeners = new Map();
  const registrations = [];
  const events = [];
  const snapshots = [];
  const errors = [];
  const client = {
    async handleEvent(event) { events.push(event); },
    acceptSnapshot(snapshot, scrollMode) { snapshots.push([snapshot, scrollMode]); }
  };
  const bind = createRuntimeEventBinder({ client, onError: error => errors.push(error) });

  assert.equal(bind(undefined), false);
  const runtime = {
    EventsOn(name, listener) {
      registrations.push(name);
      listeners.set(name, listener);
    }
  };
  assert.equal(bind(runtime), true);
  assert.equal(bind(runtime), true);
  assert.deepEqual(registrations, ["seelex:event", "seelex:ready"]);

  await listeners.get("seelex:event")({ kind: "tool.completed" });
  listeners.get("seelex:ready")({ revision: 3 });
  assert.deepEqual(events, [{ kind: "tool.completed" }]);
  assert.deepEqual(snapshots, [[{ revision: 3 }, "bottom"]]);
  assert.deepEqual(errors, []);
});

test("routes invalid ready snapshots to the frontend error boundary", () => {
  const listeners = new Map();
  const failure = new Error("invalid snapshot");
  const errors = [];
  const bind = createRuntimeEventBinder({
    client: {
      handleEvent() {},
      acceptSnapshot() { throw failure; }
    },
    onError: error => errors.push(error)
  });
  assert.equal(bind({ EventsOn: (name, listener) => listeners.set(name, listener) }), true);
  listeners.get("seelex:ready")({});
  assert.deepEqual(errors, [failure]);
});
