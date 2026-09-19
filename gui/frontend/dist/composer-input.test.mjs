import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./composer-input.js", import.meta.url), "utf8");
const { clearSubmittedText, shouldRestoreDraft, isComposingEnter, isSigilInput, composerSubmitPlan } = await import(
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

// app.js 是 DOM 绑定脚本，node 侧按仓库既有口径做源码级断言（见
// chat-view-input-lock.test.mjs）：只钉「提交走哪条 RPC」，行为边界由上面的
// composerSubmitPlan 用例覆盖。
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
