import assert from "node:assert/strict";
import test from "node:test";
import { createActiveChatSnapshotSync } from "./active-chat-sync.js";

test("polls a running chat once and stops when an authoritative snapshot is idle", async () => {
  const timers = [];
  const cancelled = [];
  let refreshes = 0;
  const sync = createActiveChatSnapshotSync({
    refresh: async () => { refreshes += 1; },
    schedule(callback, delay) { timers.push({ callback, delay }); return timers.length - 1; },
    cancel(id) { cancelled.push(id); }
  });

  sync.observe({ chat: { running: true } });
  assert.equal(timers.length, 1);
  assert.equal(timers[0].delay, 1000);
  await timers[0].callback();
  assert.equal(refreshes, 1);

  sync.observe({ chat: { running: false } });
  assert.deepEqual(cancelled, []);
});

test("retries a running chat after a snapshot failure and stops permanently on close", async () => {
  const timers = [];
  const cancelled = [];
  let errors = 0;
  const sync = createActiveChatSnapshotSync({
    refresh: async () => { throw new Error("Bridge unavailable"); },
    onError() { errors += 1; },
    schedule(callback) { timers.push(callback); return timers.length - 1; },
    cancel(id) { cancelled.push(id); }
  });

  sync.observe({ chat: { running: true } });
  await timers[0]();
  assert.equal(errors, 1);
  assert.equal(timers.length, 2, "a failed reconciliation retries while chat is active");
  sync.stop();
  assert.deepEqual(cancelled, [1]);
});
