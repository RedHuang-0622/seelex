import test from "node:test";
import assert from "node:assert/strict";
import {
  EMBED_ACTION_ATTR,
  EMBED_BRIDGE_TAG,
  EMBED_GESTURE_WINDOW_MS,
  buildEmbedBridgeScript
} from "./html-embed.js";
import {
  createEmbedActionGate,
  normalizeEmbedPayload,
  parseEmbedAction,
  resolveEmbedAction
} from "./embed-bridge.js";

test("embed bridge: only tagged embed messages are parsed", () => {
  assert.deepEqual(parseEmbedAction({ source: EMBED_BRIDGE_TAG, action: "copy-text", payload: { text: "a" } }), {
    action: "copy-text",
    payload: { text: "a" }
  });
  // 载荷缺省是 null，不是 undefined（判据下游只处理这两种形状）。
  assert.deepEqual(parseEmbedAction({ source: EMBED_BRIDGE_TAG, action: " open-source " }), {
    action: "open-source",
    payload: null
  });
  for (const bad of [
    null,
    "copy-text",
    { source: "other-frame", action: "copy-text" },
    { action: "copy-text" },
    { source: EMBED_BRIDGE_TAG },
    { source: EMBED_BRIDGE_TAG, action: "" },
    { source: EMBED_BRIDGE_TAG, action: 42 }
  ]) {
    assert.equal(parseEmbedAction(bad), null, JSON.stringify(bad));
  }
});

test("embed bridge: payload shape and size are bounded per action", () => {
  assert.deepEqual(normalizeEmbedPayload("copy-text", { text: " 你好 " }), { ok: true, payload: { text: "你好" } });
  // 裸字符串也认：`data-seelex-payload="解释这个节点"` 不是合法 JSON。
  assert.deepEqual(normalizeEmbedPayload("ask-agent", "解释这个节点"), { ok: true, payload: { text: "解释这个节点" } });
  // open-source 不收文本，给了也当没有。
  assert.deepEqual(normalizeEmbedPayload("open-source", { text: "忽略" }), { ok: true, payload: null });

  assert.equal(normalizeEmbedPayload("nope", { text: "x" }).reason, "action-not-allowlisted");
  assert.equal(normalizeEmbedPayload("copy-text", { text: "   " }).reason, "payload-missing-text");
  assert.equal(normalizeEmbedPayload("copy-text", { text: 7 }).reason, "payload-missing-text");
  assert.equal(normalizeEmbedPayload("copy-text", null).reason, "payload-missing-text");
  // 超限拒绝而不是截断：把要发给 agent 的话从中间砍掉比不支持更危险。
  assert.equal(normalizeEmbedPayload("ask-agent", { text: "x".repeat(4001) }).reason, "payload-too-long");
  assert.equal(normalizeEmbedPayload("ask-agent", { text: "x".repeat(4000) }).ok, true);
});

test("embed bridge: resolution is the ordered gate chain", () => {
  const asked = { source: EMBED_BRIDGE_TAG, action: "ask-agent", payload: { text: "解释节点 A" } };

  // 非白名单动作：连载荷都不看。
  assert.equal(resolveEmbedAction({ data: { source: EMBED_BRIDGE_TAG, action: "rm-rf" } }).reason, "action-not-allowlisted");
  assert.equal(resolveEmbedAction({ data: {} }).reason, "not-an-embed-message");

  // 有后果的动作：块没自愿（围栏没写 interactive=1）时直接拒。
  assert.equal(resolveEmbedAction({ data: asked, interactive: false }).reason, "block-not-interactive");
  // 自愿了但没给限流器：不放行（driving 动作不许"忘了接限流"地通过）。
  assert.equal(resolveEmbedAction({ data: asked, interactive: true }).reason, "gate-missing");
  assert.equal(resolveEmbedAction({ data: asked, interactive: true, allow: () => true }).ok, true);
  assert.equal(resolveEmbedAction({ data: asked, interactive: true, allow: () => false }).reason, "rate-limited");

  // 只读动作不需要自愿、也不消耗限流额度。
  let calls = 0;
  const counted = () => { calls += 1; return true; };
  assert.equal(resolveEmbedAction({ data: { source: EMBED_BRIDGE_TAG, action: "copy-text", payload: "x" }, allow: counted }).ok, true);
  assert.equal(calls, 0);

  // 限流种子是「动作 + 正文」：同一动作的不同正文各算一次。
  const seeds = [];
  resolveEmbedAction({ data: asked, interactive: true, allow: seed => { seeds.push(seed); return true; } });
  assert.deepEqual(seeds, [`ask-agent\u0000解释节点 A`]);
});

test("embed bridge: gate limits bursts and de-duplicates replays", () => {
  let clock = 0;
  const gate = createEmbedActionGate({ limit: 3, windowMs: 10000, dedupeMs: 1500, now: () => clock });

  assert.equal(gate.allow("a"), true);
  assert.equal(gate.allow("b"), true);
  assert.equal(gate.allow("c"), true);
  // 窗口内第四次：拒（块内脚本把动作循环发出去的情况）。
  assert.equal(gate.allow("d"), false);
  // 同一正文的复读机：即使在额度内也拒（连点/重放）。
  assert.equal(gate.allow("a"), false);

  clock = 10001;
  assert.equal(gate.allow("d"), true);
  clock = 10200;
  assert.equal(gate.allow("a"), true);
});

test("embed bridge: the injected script carries the protocol and the gesture gate", () => {
  const script = buildEmbedBridgeScript();
  assert.match(script, /seelex-embed/);
  assert.match(script, new RegExp(EMBED_ACTION_ATTR));
  assert.match(script, /data-seelex-payload/);
  assert.match(script, /window\.seelex = \{ emit: emit, actions: ACTIONS \}/);
  assert.match(script, /event\.isTrusted/);
  assert.match(script, new RegExp(String(EMBED_GESTURE_WINDOW_MS)));
  assert.match(script, /"ask-agent"/);
  // 只有一个闭合标签：脚本里不能出现会提前收尾的 `</script>`。
  assert.equal((script.match(/<\/script>/g) || []).length, 1);
  assert.ok(script.startsWith("<script>"));
});
