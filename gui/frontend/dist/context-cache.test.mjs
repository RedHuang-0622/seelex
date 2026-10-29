import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { contextCacheStatus, formatContextCacheChars } from "./context-cache.js";

test("context cache formats only L1 reuse facts", () => {
  assert.equal(formatContextCacheChars(999), "999");
  assert.equal(formatContextCacheChars(20503), "20.5k");
  assert.equal(formatContextCacheChars(1250000), "1.3m");

  const hit = contextCacheStatus({
    available: true,
    status: "hit",
    hits: 3,
    misses: 1,
    rebuilt: 1,
    reused_events: 7,
    reused_chars: 20503,
    last_reused_chars: 9000,
    last_changed: [],
    prefix_key: { retained_from: 12, compact_seq: 4, tools_epoch: 2 }
  });
  assert.equal(hit.value, "L1 75% · 复用 20.5k 字符");
  assert.match(hit.title, /非 provider usage/);
  assert.match(hit.title, /retained_from=12/);
});

test("context cache names invalidation and empty state", () => {
  const miss = contextCacheStatus({
    available: true,
    status: "miss",
    hits: 0,
    misses: 1,
    rebuilt: 1,
    reused_chars: 0,
    last_changed: ["compact_seq", "retained_from"]
  });
  assert.equal(miss.value, "L1 0% · 复用 0 字符");
  assert.match(miss.title, /changed=compact_seq,retained_from/);
  assert.deepEqual(contextCacheStatus(null), {
    value: "尚未建立",
    title: "当前会话尚未发生 L1 前缀复用判定"
  });
});

test("status panel wires the L1 cache row", () => {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const app = fs.readFileSync(path.join(here, "app.js"), "utf8");
  assert.match(app, /import \{ contextCacheStatus \} from "\.\/context-cache\.js"/);
  assert.match(app, /const contextCache = contextCacheStatus\(snapshot\.runtime\?\.context_cache\)/);
  assert.match(app, /\["L1 缓存", contextCache\.value, contextCache\.title\]/);
});
