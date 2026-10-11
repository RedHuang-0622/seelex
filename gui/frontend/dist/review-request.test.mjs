import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  MAX_REVIEW_FILES_PER_REQUEST,
  REVIEW_SKIP_REASONS,
  decideReviewApply,
  normalizeReviewRequest,
  reviewRequestEntries,
  reviewRequestKey
} from "./review-request.js";
import { PREVIEW_CHIP_VISIBLE_LIMIT } from "./file-preview.js";

function snapshotWith(request, sessionID = "sess-a") {
  return { session: { id: sessionID }, review_request: request };
}

test("MAX_REVIEW_FILES_PER_REQUEST 与后端契约同数（10）且覆盖 chip 可见预算", () => {
  assert.equal(MAX_REVIEW_FILES_PER_REQUEST, 10);
  // chip 条可见枚数 + 溢出计数必须能容纳满额 10 份（否则后端允许的批处理在前端会丢 chip）。
  assert.ok(PREVIEW_CHIP_VISIBLE_LIMIT >= 1);
  assert.ok(PREVIEW_CHIP_VISIBLE_LIMIT < MAX_REVIEW_FILES_PER_REQUEST);
  assert.deepEqual([...REVIEW_SKIP_REASONS].sort(), ["binary", "duplicate", "missing", "sandbox", "sensitive"]);
});

test("normalizeReviewRequest 收下契约形状并丢弃半份契约", () => {
  // 缺省 / null / 空 files / 缺 request_id 一律视为"没有待审阅请求"。
  assert.equal(normalizeReviewRequest({}), null);
  assert.equal(normalizeReviewRequest({ review_request: null }), null);
  assert.equal(normalizeReviewRequest({ review_request: {} }), null);
  assert.equal(normalizeReviewRequest({ review_request: { request_id: "r1", files: [] } }), null);
  assert.equal(normalizeReviewRequest({ review_request: { request_id: "  ", files: [{ path: "a.go" }] } }), null);
  // files 里的非法条目被丢掉；整条都非法时视作没有请求。
  assert.equal(normalizeReviewRequest({ review_request: { request_id: "r1", files: [{ path: "" }, "x", null] } }), null);

  const request = normalizeReviewRequest(snapshotWith({
    request_id: "r1",
    files: [
      { path: "src/a.go", note: "看这里" },
      { path: "src/b.go", start_line: 3, end_line: 9 },
      // 单端 / 倒序区间视为"不指定"（与后端同口径）。
      { path: "src/c.go", start_line: 5 },
      { path: "src/d.go", start_line: 9, end_line: 3 }
    ],
    skipped: [{ path: "x.bin", reason: "binary" }, { path: "", reason: "sandbox" }]
  }));
  assert.equal(request.requestID, "r1");
  assert.deepEqual(request.files, [
    { path: "src/a.go", note: "看这里" },
    { path: "src/b.go", startLine: 3, endLine: 9 },
    { path: "src/c.go" },
    { path: "src/d.go" }
  ]);
  // 跳过清单只留双端合法的（回契约的一半：哪几份没进来、为什么）。
  assert.deepEqual(request.skipped, [{ path: "x.bin", reason: "binary" }]);
});

test("reviewRequestKey 以会话为界：同 request_id 跨会话不算已应用", () => {
  const request = normalizeReviewRequest(snapshotWith({ request_id: "r1", files: [{ path: "a.go" }] }));
  assert.equal(reviewRequestKey(snapshotWith({ request_id: "r1", files: [{ path: "a.go" }] }, "sess-a"), request), "sess-a::r1");
  assert.equal(reviewRequestKey(snapshotWith({ request_id: "r1", files: [{ path: "a.go" }] }, "sess-b"), request), "sess-b::r1");
  assert.equal(reviewRequestKey({ review_request: null }, null), "");
});

test("decideReviewApply：新召唤应用、补放不抢、编辑中延迟且不推进水位", () => {
  const snapshot = snapshotWith({ request_id: "r1", files: [{ path: "src/a.go" }] });

  // 新的一次召唤 → 应用。
  const fresh = decideReviewApply(snapshot, { appliedKey: "", editing: false });
  assert.equal(fresh.apply, true);
  assert.equal(fresh.reason, "new");
  assert.equal(fresh.key, "sess-a::r1");
  assert.deepEqual(fresh.request.files, [{ path: "src/a.go" }]);

  // 同一份快照的补放（key 已在水位上）→ 不重复开 tab、不抢当前文件。
  const replay = decideReviewApply(snapshot, { appliedKey: "sess-a::r1", editing: false });
  assert.equal(replay.apply, false);
  assert.equal(replay.reason, "replay");
  assert.equal(replay.key, "sess-a::r1");

  // 用户正在编辑 → 本轮不抢，且**不推进水位**（下一次快照会再试）。
  const deferred = decideReviewApply(snapshot, { appliedKey: "", editing: true });
  assert.equal(deferred.apply, false);
  assert.equal(deferred.reason, "editing");
  assert.equal(deferred.key, "");

  // 没有请求 → 不动（缺省快照里的 review_request 不存在）。
  const absent = decideReviewApply({ session: { id: "sess-a" } }, { appliedKey: "sess-a::r1" });
  assert.equal(absent.apply, false);
  assert.equal(absent.reason, "absent");
  assert.equal(absent.key, "sess-a::r1");
});

test("reviewRequestEntries 只带 path(+note)：去重与读盘交给抽屉", () => {
  const request = normalizeReviewRequest(snapshotWith({
    request_id: "r1",
    files: [{ path: "src/a.go", note: "为什么看它" }, { path: "src/b.go" }]
  }));
  assert.deepEqual(reviewRequestEntries(request), [
    { path: "src/a.go", note: "为什么看它" },
    { path: "src/b.go" }
  ]);
  assert.deepEqual(reviewRequestEntries(null), []);
});

test("app.js 在快照渲染路径上按 decideReviewApply 消费 review_request", async () => {
  const source = await readFile(new URL("./app.js", import.meta.url), "utf8");
  assert.match(source, /import \{ decideReviewApply, reviewRequestEntries \} from "\.\/review-request\.js"/);
  // render(snapshot) 里确实调用了 applyReviewRequest（快照即真相源，不靠一次性事件）。
  assert.match(source, /applyReviewRequest\(snapshot\)/);
  // 保守默认：打开抽屉**不**调 revealView("code")（不抢用户当前子页）——openReviewRequest 里没有它。
  const body = source.slice(source.indexOf("function openReviewRequest"), source.indexOf("function openFilePreview"));
  assert.ok(!body.includes("revealView("), "openReviewRequest 不得切换子页");
  assert.match(body, /openDeferred\(reviewRequestEntries\(request\)/);
});
