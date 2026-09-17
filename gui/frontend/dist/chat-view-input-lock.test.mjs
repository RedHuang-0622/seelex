import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// 输入区锁（chat-view.js `renderControls`）的行为边界。锁有两种形态：
//   - restoring（目标会话恢复中）：整块 composer 只读壳；
//   - switching（本端已发起切换、渲染层还没拿到 restoring 快照）：**只锁输入**，
//     不得连带锁死当前会话的「停止」与历史栏。
//
// 语义背景与修法：docs/devlog/2026-09-17-submit-during-cold-restore-repro.md。
// 前端锁只是呈现；restoring 期间的权威判据在应用层（ErrSessionRestoring，
// application/core/session_submit_restoring_test.go）。

function dataURL(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
}

const chatSource = (await readFile(new URL("./chat-view.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${dataURL("export function renderConversationModel(messages, chat) { return { messages, chat }; }")}"`)
  .replace('"./protocol.js"', `"${dataURL("export function historyWindowed() { return false; }")}"`);
const { createChatView } = await import(dataURL(chatSource));

function makeClassList() {
  const set = new Set();
  return {
    add: (...names) => names.forEach(name => set.add(name)),
    remove: (...names) => names.forEach(name => set.delete(name)),
    toggle: (name, force) => {
      const on = force === undefined ? !set.has(name) : Boolean(force);
      if (on) set.add(name); else set.delete(name);
      return on;
    },
    contains: name => set.has(name)
  };
}

function makeElement() {
  const element = {
    textContent: "",
    placeholder: "",
    title: "",
    disabled: false,
    value: "",
    attributes: {},
    classList: makeClassList(),
    setAttribute(name, value) { element.attributes[name] = String(value); }
  };
  return element;
}

const ELEMENT_NAMES = [
  "empty-state", "composer-status", "send-button", "prompt", "composer",
  "stop-button", "connection-dot", "history-bar", "load-history", "latest-history"
];

function makeView() {
  const elements = {};
  for (const name of ELEMENT_NAMES) elements[name] = makeElement();
  const renders = [];
  const view = createChatView(elements, { render: (model, options) => renders.push({ model, options }) });
  return { view, elements, renders };
}

function snapshot({ status = "idle", running = false, queued = 0, hasMoreHistory = false, conversation } = {}) {
  return {
    session: { id: "s1", status },
    chat: { running, queued_count: queued },
    conversation: conversation || [{ id: "m1", role: "user", content: "hi" }],
    has_more_history: hasMoreHistory
  };
}

test("restoring 锁整块 composer（输入、停止、历史栏）", () => {
  const { view, elements } = makeView();
  view.renderControls(snapshot({ status: "restoring", running: true, hasMoreHistory: true }));
  assert.equal(elements.prompt.disabled, true);
  assert.equal(elements["send-button"].disabled, true);
  assert.equal(elements["composer-status"].textContent, "正在恢复会话…");
  assert.equal(elements.prompt.placeholder, "会话内容恢复中…");
  assert.equal(elements["stop-button"].classList.contains("hidden"), true);
  assert.equal(elements["history-bar"].classList.contains("hidden"), true);
});

test("switching 只锁输入，不锁停止 / 历史栏", () => {
  const { view, elements } = makeView();
  view.renderControls(snapshot({ status: "running", running: true, queued: 2, hasMoreHistory: true }), true);
  assert.equal(elements.prompt.disabled, true);
  assert.equal(elements["send-button"].disabled, true);
  assert.equal(elements["composer-status"].textContent, "正在切换会话…");
  assert.equal(elements.prompt.placeholder, "正在切换会话…");
  assert.equal(elements["send-button"].title, "正在切换会话");
  assert.equal(elements["send-button"].attributes["aria-label"], "正在切换会话");
  // 关键：切换窗口里当前运行回合的停止与历史回看必须继续可用。
  assert.equal(elements["stop-button"].classList.contains("hidden"), false);
  assert.equal(elements["history-bar"].classList.contains("hidden"), false);
});

test("switching 结束（快照已到位）后恢复可发送与运行态文案", () => {
  const { view, elements } = makeView();
  view.renderControls(snapshot({ status: "running", running: true }), true);
  view.renderControls(snapshot({ status: "running", running: true }));
  assert.equal(elements.prompt.disabled, false);
  assert.equal(elements["send-button"].disabled, false);
  assert.equal(elements["composer-status"].textContent, "执行中");
  assert.equal(elements.prompt.placeholder, "继续输入，Enter 加入队列");
});

test("空闲且非切换时可发送", () => {
  const { view, elements } = makeView();
  view.renderControls(snapshot({ status: "idle" }));
  assert.equal(elements.prompt.disabled, false);
  assert.equal(elements["send-button"].disabled, false);
  assert.equal(elements["composer-status"].textContent, "就绪");
});

test("单调 OR：restoring 与 switching 同时成立时锁不提前解除", () => {
  const { view, elements } = makeView();
  // 两者同时成立 → 取射程更大的 restoring（整块只读壳）。
  view.renderControls(snapshot({ status: "restoring", running: true, hasMoreHistory: true }), true);
  assert.equal(elements.prompt.disabled, true);
  assert.equal(elements["composer-status"].textContent, "正在恢复会话…");
  // restoring 已清、switching 仍在途 → 仍锁输入：不得因为快照回到 running 就解锁。
  view.renderControls(snapshot({ status: "running", running: true }), true);
  assert.equal(elements.prompt.disabled, true);
  assert.equal(elements["composer-status"].textContent, "正在切换会话…");
  // 两个条件都清 → 才解锁。
  view.renderControls(snapshot({ status: "running", running: true }), false);
  assert.equal(elements.prompt.disabled, false);
  assert.equal(elements["composer-status"].textContent, "执行中");
});

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("app.js：切换在途标志贯穿 composer 的每个渲染落点与生命周期", () => {
  // app.js 是 DOM 绑定脚本，node 侧按仓库既有口径做源码级断言（见
  // agent-team-refresh.test.mjs / queue-edit.test.mjs）：只钉「哪条渲染路径
  // 带没带 switching」，行为边界由上面的 renderControls 用例覆盖。
  assert.ok(
    appSource.includes("chatView.render(snapshot, options.scrollMode, Boolean(state.resumingSessionID))"),
    "整份渲染必须把 resumingSessionID 作为 switching 传入"
  );
  const calls = appSource.match(/chatView\.renderControls\(([^\n]*)\)/g) || [];
  assert.ok(calls.length >= 3, `期望至少 3 个 renderControls 落点，实际 ${calls.length}`);
  assert.ok(
    calls.some(call => call.includes("Boolean(state.resumingSessionID)")),
    "增量渲染落点必须传 switching"
  );
  assert.ok(
    calls.some(call => call === "chatView.renderControls(client.current(), true)"),
    "置位 switching 后必须显式重渲 composer（不等下一次快照）"
  );
  assert.ok(
    calls.some(call => call === "chatView.renderControls(latest)"),
    "切换结束后必须按权威快照重算解锁"
  );

  const setIndex = appSource.indexOf("state.resumingSessionID = sessionID;");
  const invokeIndex = appSource.indexOf('await invoke("ResumeSession", sessionID);');
  const clearIndex = appSource.indexOf('state.resumingSessionID = "";');
  assert.ok(setIndex >= 0 && invokeIndex > setIndex, "置位必须发生在 ResumeSession 之前");
  assert.ok(clearIndex > invokeIndex, "清位必须发生在切换结束之后");
  assert.ok(
    /finally\s*\{[\s\S]{0,200}state\.resumingSessionID = "";/.test(appSource),
    "清位必须在 finally（失败回滚也不留锁）"
  );
});

test("render 在 switching 窗口不闪「暂无消息」空态并锁输入", () => {
  const { view, elements, renders } = makeView();
  view.render(snapshot({ status: "idle", conversation: [] }), "bottom", true);
  assert.equal(elements["empty-state"].classList.contains("hidden"), true);
  assert.equal(elements.prompt.disabled, true);
  assert.equal(renders.length, 1);

  // 对照：同一份空会话、非切换 → 空态可见。
  const plain = makeView();
  plain.view.render(snapshot({ status: "idle", conversation: [] }), "bottom", false);
  assert.equal(plain.elements["empty-state"].classList.contains("hidden"), false);
  assert.equal(plain.elements.prompt.disabled, false);
});
