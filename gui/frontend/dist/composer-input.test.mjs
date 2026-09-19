import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./composer-input.js", import.meta.url), "utf8");
const { clearSubmittedText, shouldRestoreDraft, isComposingEnter, isSigilInput, composerSubmitPlan, composerViewSwitch } = await import(
  `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`
);

// ── 提交清空：只移除已发送的那段，不吞提交往返期间的新输入 ──────────

test("clears the box when it still holds exactly the submitted text", () => {
  assert.equal(clearSubmittedText("写测试", "写测试"), "");
  assert.equal(clearSubmittedText("line1\nline2", "line1\nline2"), "");
});

test("keeps text typed during the submit round-trip", () => {
  // 用户在 Submit RPC 往返里又敲了字：追加在末尾 → 只切掉已发送的前缀。
  assert.equal(clearSubmittedText("写测试继续", "写测试"), "继续");
  assert.equal(clearSubmittedText("go test\n再补一句", "go test"), "\n再补一句");
});

test("never clobbers content the user rewrote mid-flight", () => {
  // 提交后用户整段改写（不是简单追加）：保留，宁留原文也不吞新输入。
  assert.equal(clearSubmittedText("另起一行", "写测试"), "另起一行");
  // 用户手动清空后又另写。
  assert.equal(clearSubmittedText("xyz", "abc"), "xyz");
});

test("is a no-op when nothing was submitted", () => {
  assert.equal(clearSubmittedText("草稿", ""), "草稿");
  assert.equal(clearSubmittedText("", ""), "");
  assert.equal(clearSubmittedText(null, ""), "");
  assert.equal(clearSubmittedText(undefined, "abc"), "");
});

// ── 草稿回填：不覆盖未落盘的本地输入 ──────────────────────────────

test("restores only a clean, unfocused draft whose backend copy differs", () => {
  assert.equal(shouldRestoreDraft({
    draft: true, snapshotComposer: "旧草稿", current: "", focused: false, dirty: false
  }), true);
});

test("refuses to restore while the user is editing or has unsaved input", () => {
  const base = { draft: true, snapshotComposer: "旧草稿", current: "", focused: false, dirty: false };
  assert.equal(shouldRestoreDraft({ ...base, focused: true }), false);
  assert.equal(shouldRestoreDraft({ ...base, dirty: true }), false);
});

test("skips restore when there is nothing to restore or copies already agree", () => {
  assert.equal(shouldRestoreDraft({ draft: false, snapshotComposer: "x", current: "", focused: false, dirty: false }), false);
  assert.equal(shouldRestoreDraft({ draft: true, snapshotComposer: "", current: "", focused: false, dirty: false }), false);
  assert.equal(shouldRestoreDraft({ draft: true, snapshotComposer: "同", current: "同", focused: false, dirty: false }), false);
});

// ── IME：合成中的 Enter 不是发送 ────────────────────────────────

test("treats composition-confirm Enter as non-submitting", () => {
  assert.equal(isComposingEnter({ key: "Enter", isComposing: true }), true);
  assert.equal(isComposingEnter({ key: "Enter", keyCode: 229 }), true);
  assert.equal(isComposingEnter({ key: "Enter" }, true), true);
});

test("lets a plain Enter submit", () => {
  assert.equal(isComposingEnter({ key: "Enter", isComposing: false, keyCode: 13 }), false);
  assert.equal(isComposingEnter(null), false);
  assert.equal(isComposingEnter(undefined, false), false);
});

// ── 提交路由：显式钉死目标会话，运行中会话不再"吸"走别人的输入 ─────────

test("普通对话输入走 SubmitToSession + 当前视图会话 ID", () => {
  assert.deepEqual(
    composerSubmitPlan({ text: "帮我跑一遍测试", viewedSessionID: "sess-4" }),
    { rpc: "SubmitToSession", args: ["sess-4", "帮我跑一遍测试"] }
  );
});

test("sigil 输入仍走后端路由器（Submit），不把命令当对话发出去", () => {
  for (const text of ["/compact", "#default", "$code", "@goal-a2a 干活", "@", "#"]) {
    assert.equal(isSigilInput(text), true, `${text} 应判为 sigil`);
    assert.deepEqual(composerSubmitPlan({ text, viewedSessionID: "sess-4" }), { rpc: "Submit", args: [text] });
  }
  assert.equal(isSigilInput("   /compact"), true, "前导空白不影响前缀判定");
  assert.equal(isSigilInput("先看 /compact"), false, "前缀只在开头才拥有输入");
  assert.equal(isSigilInput(""), false);
  assert.equal(isSigilInput(null), false);
});

test("拿不到视图会话 ID 时退回 Submit（不往空 ID 上投）", () => {
  assert.deepEqual(composerSubmitPlan({ text: "hi", viewedSessionID: "" }), { rpc: "Submit", args: ["hi"] });
  assert.deepEqual(composerSubmitPlan({ text: "hi", viewedSessionID: "  " }), { rpc: "Submit", args: ["hi"] });
  assert.deepEqual(composerSubmitPlan({ text: "hi", viewedSessionID: null }), { rpc: "Submit", args: ["hi"] });
  assert.deepEqual(composerSubmitPlan({ text: "hi", viewedSessionID: " sess-9 " }), { rpc: "SubmitToSession", args: ["sess-9", "hi"] });
});

// ── 输入框正文按会话归属：运行中会话里写的字不得被发给空闲会话 ──────────
// 复现（用户报告）：一个会话运行中（A），用户在它的输入框里写了插话/撤回了
// 排队消息，接着切到一个**没在运行**的会话（B）继续干活——输入框正文此前不
// 按会话归属，A 的字跟着视图留了下来，于是按 Enter 时它被当成 B 的内容提交
// （`composerSubmitPlan` 只认当前视图会话）。同一处脏位还挡住 B 自己的草稿
// 回填（shouldRestoreDraft 要求非脏）。

// switchOf 是按 composerViewSwitch 的真实调用序列驱动一次会话切换的小夹具：
// 起点是「已经在 from 会话、输入框里是 current」。
function switchOf({ from, to, current = "", dirty = false, stash = new Map() }) {
  return composerViewSwitch({ fromSessionID: from, toSessionID: to, current, dirty, stash });
}

test("写在一个会话里的未发送正文不会跟着视图切到另一个会话", () => {
  // A 运行中，用户在 A 的输入框里留了一句话（还没发）。
  const leaving = switchOf({ from: "", to: "A", current: "", stash: new Map() });
  const leavingA = switchOf({ from: "A", to: "B", current: "给 A 的插话", dirty: true, stash: leaving.stash });

  assert.equal(leavingA.switched, true, "视图会话变了，必须走一次归属切换");
  assert.equal(leavingA.text, "", "切到空闲会话 B 后输入框必须是空的（A 的字不能留给 B 提交）");
  assert.equal(leavingA.dirty, false, "新会话不继承上一个会话的脏位（否则它自己的草稿回填被挡住）");
  assert.equal(leavingA.stash.get("A"), "给 A 的插话", "A 的正文按会话留存，切回去还在");

  // 切回 A：属于 A 的正文回来，且是「本地内容」（可继续编辑/发送）。
  const backToA = switchOf({ from: "B", to: "A", current: "", stash: leavingA.stash });
  assert.equal(backToA.text, "给 A 的插话");
  assert.equal(backToA.dirty, true, "切回来的本地正文必须置脏：草稿会话的后端旧副本不得覆盖它");
});

test("切到空闲会话后提交的只能是该会话自己的内容", () => {
  // 端到端判据：归属切换 + 提交路由合起来看「这一次 Enter 到底发什么」。
  const stash = switchOf({ from: "", to: "A", current: "" }).stash;
  const atB = switchOf({ from: "A", to: "B", current: "给 A 的插话", dirty: true, stash });

  const afterSwitch = atB.text.trim();
  assert.equal(afterSwitch, "", "切换后的空输入框：回车是 no-op，不会把 A 的字发给 B");
  assert.equal(composerSubmitPlan({ text: "写给 B 的话", viewedSessionID: "B" }).args[0], "B");
});

test("同一会话重复渲染不动输入框（切换只在 ID 真的变了时发生）", () => {
  const same = switchOf({ from: "sess-1", to: "sess-1", current: "正在打字", dirty: true });
  assert.equal(same.switched, false);
  assert.equal(same.text, "正在打字");
  assert.equal(same.dirty, true);
  assert.equal(same.stash.size, 0);
});

test("首次挂载（还没有前置会话）保留已敲进去的内容，不吞字", () => {
  const first = switchOf({ from: "", to: "sess-1", current: "启动竞态里敲的字", dirty: true });
  assert.equal(first.text, "启动竞态里敲的字");
  assert.equal(first.switched, true);
  assert.equal(first.stash.get("sess-1"), "启动竞态里敲的字", "首次挂载的内容归到该会话名下");
});

test("目标会话 ID 缺失时不动输入框（快照未就绪）", () => {
  const missing = switchOf({ from: "sess-1", to: "", current: "写了一半", dirty: true });
  assert.equal(missing.switched, false);
  assert.equal(missing.text, "写了一半");
  assert.equal(missing.dirty, true);
});

test("空正文不留占位，切换后回到「没有本地内容」的干净态", () => {
  const stash = switchOf({ from: "", to: "A", current: "" }).stash;
  const away = switchOf({ from: "A", to: "B", current: "", dirty: false, stash });
  assert.equal(away.stash.has("A"), false, "空正文不占位子");
  assert.equal(away.text, "");
  assert.equal(away.dirty, false);
  const idle = switchOf({ from: "B", to: "D", current: "", dirty: false, stash: away.stash });
  assert.equal(idle.dirty, false, "空闲草稿会话（无本地留存）必须留出回填位");
});

test("留存表有上限（LRU）：正文只是未发送的本地草稿，不随会话数无限长", () => {
  let stash = new Map();
  for (let index = 0; index < 30; index++) {
    stash = switchOf({ from: `sess-${index}`, to: `sess-${index + 1}`, current: `第 ${index} 句`, stash }).stash;
  }
  assert.equal(stash.size, 24, `留存表必须收在上限内，实际 ${stash.size}`);
  assert.equal(stash.has("sess-5"), false, "最早的那几条被 LRU 淘汰");
  assert.equal(stash.has("sess-6"), true, "最近用过的会话仍在表里");
  assert.equal(stash.get("sess-29"), "第 29 句", "最新一条完整保留");
});

// app.js 是 DOM 绑定脚本，node 侧按仓库既有口径做源码级断言（见
// chat-view-input-lock.test.mjs）：只钉「哪条路径 / 哪个规则被接上」，行为边界
// 由上面 composerSubmitPlan / composerViewSwitch 的用例覆盖。
const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("app.js：composer 提交经 composerSubmitPlan 显式钉会话，不裸调会被后端 current 带偏的 Submit", () => {
  assert.ok(
    appSource.includes("composerSubmitPlan({ text, viewedSessionID: client.current()?.session?.id })"),
    "提交必须显式取当前视图会话（await 之前取，不是 RPC 回来时取）"
  );
  assert.ok(appSource.includes("await invoke(plan.rpc, ...plan.args)"), "提交必须走 plan 决定的 RPC");
  assert.ok(
    !appSource.includes('invoke("Submit"'),
    "不得回退到 ambient Submit——它重读后端「当前会话」，正是运行中会话吸走输入的那条路"
  );
});

test("app.js：整份渲染按视图会话对齐输入框正文，且先于草稿回填", () => {
  assert.ok(
    appSource.includes("composerViewSwitch({\n    fromSessionID: composerSessionID,"),
    "输入框正文归属切换必须经 composerViewSwitch（规则不在壳层重写一遍）"
  );
  const syncIndex = appSource.indexOf("syncComposerSession(snapshot);");
  const restoreIndex = appSource.indexOf("restoreComposerDraft(snapshot);");
  const renderIndex = appSource.indexOf("function render(snapshot, options = {})");
  assert.ok(syncIndex > renderIndex && restoreIndex > syncIndex,
    "render 里必须先对齐归属（syncComposerSession）再谈草稿回填（restoreComposerDraft）");
  assert.ok(
    appSource.includes("composerStash = new Map()"),
    "留存表必须是壳层持有的会话级状态（跨渲染保留）"
  );
});
