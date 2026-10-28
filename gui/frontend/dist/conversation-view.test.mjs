import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  canPatchMessageBody,
  conversationRenderProbe,
  settleConversationTailAtSessionBoundary,
  shouldRepaintConversationBoundary,
  shouldAutoLoadOlder,
  shouldFollowTail
} from "./conversation-view.js";

// 这一份钉的是「顶部 sentinel 自动翻更早页」的准入纪律：**用户停在尾部时一律不翻**。
//
// 背景（2026-09-27 定位）：可见窗口是"从 history_offset 起的连续一段"，翻更早页会把
// 窗口整体后退，尾部那一段随之离开 DOM。sentinel 的 IntersectionObserver 只看几何
// （rootMargin 160px），于是"容器从 display:none 重新显示 / 侧栏压缩 / 布局抖动"都会
// 在用户根本没滚到顶时触发它——最新消息就这样被静默移出窗口。这条纪律是唯一判据，
// 因此单独抽成纯函数在这里钉住；显式的「加载更早」按钮走宿主命令，不受此限制。

const tailState = {
  hasLoader: true,
  canLoadMore: true,
  loadingOlder: false,
  sentinelArmed: true,
  followsTail: true
};

test("停在尾部时不自动翻更早页（最新消息不得被移出窗口）", () => {
  assert.equal(shouldAutoLoadOlder(tailState), false);
  // 关键回归：sentinel 被几何变化"叫醒"（armed）也不能翻——尾部才是判据。
  assert.equal(shouldAutoLoadOlder({ ...tailState, sentinelArmed: true, followsTail: true }), false);
});

test("离开尾部且有更早历史时才自动翻页", () => {
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false }), true);
});

test("没有更早历史 / 正在翻页 / 未武装 / 无加载器时都不翻", () => {
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, canLoadMore: false }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, loadingOlder: true }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, sentinelArmed: false }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, hasLoader: false }), false);
});

test("缺省/畸形状态按不翻处理（绝不因为状态缺失就去翻页）", () => {
  assert.equal(shouldAutoLoadOlder(undefined), false);
  assert.equal(shouldAutoLoadOlder({}), false);
});

test("流式正文只允许 patch 稳定消息的 body；外壳变化仍走 keyed 替换", () => {
  const before = `<article class="message assistant" data-conversation-key="message:a"><div class="message-head"><strong>AGENT</strong></div><div class="message-body" data-message-body="true">旧</div><!--/seelex-message-body--><div class="message-actions"></div></article>`;
  const next = `<article class="message assistant" data-conversation-key="message:a"><div class="message-head"><strong>AGENT</strong></div><div class="message-body" data-message-body="true">新正文</div><!--/seelex-message-body--><div class="message-actions"></div></article>`;
  const shellChanged = next.replace("<strong>AGENT</strong>", "<strong>EXEC</strong>");
  assert.equal(canPatchMessageBody(before, next), true);
  assert.equal(canPatchMessageBody(before, shellChanged), false);
  assert.equal(canPatchMessageBody("", next), false);
});

test("用户接管滚动后 auto/preserve 都不得抢回尾部", () => {
  assert.equal(shouldFollowTail("auto", true), true);
  assert.equal(shouldFollowTail("auto", false), false);
  assert.equal(shouldFollowTail("bottom", false), true);
  assert.equal(shouldFollowTail("preserve", true), false);
  assert.equal(shouldFollowTail("anchor", true), false);
});

test("会话边界在布局完成后按新高度重新贴尾", () => {
  const style = { display: "grid", scrollBehavior: "smooth" };
  const container = {
    style,
    scrollTop: 120,
    scrollHeight: 920,
    clientHeight: 640
  };
  settleConversationTailAtSessionBoundary(container, true);
  assert.equal(style.display, "grid");
  assert.equal(container.scrollTop, 920, "恢复后必须落到新会话真实尾部");
});

test("会话切换或空壳首次收到正文都算渲染边界", () => {
  assert.equal(shouldRepaintConversationBoundary({
    previousSessionID: "session-a",
    nextSessionID: "session-b",
    hadContent: true,
    hasContent: true
  }), true, "切换到另一会话必须刷新合成层");
  assert.equal(shouldRepaintConversationBoundary({
    previousSessionID: "session-b",
    nextSessionID: "session-b",
    hadContent: false,
    hasContent: true
  }), true, "restoring 空壳收到正文时也必须刷新");
  assert.equal(shouldRepaintConversationBoundary({
    previousSessionID: "session-b",
    nextSessionID: "session-b",
    hadContent: true,
    hasContent: true
  }), false, "普通流式/增量更新不得反复 display 抖动");
});

test("会话渲染探针区分 snapshot 为空、前端丢弃与布局隐藏", () => {
  const base = {
    targetSessionID: "target",
    viewSessionID: "target",
    snapshotCount: 12,
    renderedCount: 12,
    visibleCount: 12
  };
  assert.equal(conversationRenderProbe(base).verdict, "ok");
  assert.equal(conversationRenderProbe({ ...base, loading: true }).verdict, "loading");
  assert.equal(conversationRenderProbe({ ...base, viewSessionID: "old" }).verdict, "target-not-loaded");
  assert.equal(conversationRenderProbe({ ...base, snapshotCount: 0, renderedCount: 0, visibleCount: 0 }).verdict, "snapshot-empty");
  assert.equal(conversationRenderProbe({ ...base, renderedCount: 0, visibleCount: 0 }).verdict, "frontend-dropped");
  assert.equal(conversationRenderProbe({ ...base, visibleCount: 0 }).verdict, "layout-hidden");
  assert.equal(conversationRenderProbe({ ...base, hiddenRowCount: 2 }).verdict, "rows-transparent");
  const sized = conversationRenderProbe({ ...base, maxRowHeight: 486, containerHeight: 640 });
  assert.equal(sized.verdict, "ok");
  assert.equal(sized.maxRowHeight, 486);
  assert.equal(sized.containerHeight, 640);
  assert.equal(conversationRenderProbe({ ...base, maxRowHeight: 2000, containerHeight: 640 }).verdict, "tall-row");
  assert.equal(conversationRenderProbe({ ...base, maxGap: 360, containerHeight: 640 }).verdict, "layout-gap");
});

test("会话容器暴露 aria-busy / stream phase，局部 patch 不替换整条 article", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /setAttribute\?\.\("aria-busy",\s*options\.streaming \? "true" : "false"\)/);
  assert.match(source, /dataset\.streamPhase = streamPhase/);
  assert.match(source, /patchMessageBody\(node, replacement/);
  const patched = source.slice(source.indexOf("const patched"), source.indexOf("const uiState = captureUIState(node);"));
  assert.doesNotMatch(patched, /node\.replaceWith/, "可 patch 的稳定消息不得走整条 replaceWith");
});

test("会话整批装载不播放逐行入场，只有后续增量行才动画", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(
    source,
    /animateEnter:\s*!repaintBoundary\s*&&[\s\S]{0,160}newItemCount\s*<=\s*2/,
    "会话边界 / 历史批量装载必须关闭逐行入场，只有小批增量才动画",
  );
  assert.match(
    source,
    /if\s*\(buffers\.animateEnter\s*!==\s*false\s*&&[\s\S]{0,80}markEntering\(replacement,\s*"is-entering-row"\)/,
    "历史会话批量行不得停在入场动画的 opacity:0 初始帧",
  );
  assert.match(
    source,
    /item\.meta\?\.kind\s*!==\s*"loading"[\s\S]{0,80}markEntering\(replacement,\s*"is-entering-row"\)/,
    "会话切换骨架必须首帧可见，不能等待入场动画",
  );
});

test("连续工具、Think 与 LLM 收尾只做局部滚动和阻尼落位", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /item\.meta\?\.motion === "tool-update"/);
  assert.match(source, /animateToolDomainUpdate\(replacement\)/);
  assert.match(source, /translateY\(10px\)/);
  assert.match(source, /animateThinkingUpdate\(assistant\)/);
  assert.match(source, /previousStreaming && !options\.streaming && !repaintBoundary/);
  assert.match(source, /settleCompletedTurn\(container\)/);
  assert.doesNotMatch(source, /animateSpring\(container, "settle"/, "回合收尾不得推动整个对话容器");
});

test("消息编辑器复用 copyText 通道，并用稳定 operation_id 调一次原子 editFork", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /data-edit-message/);
  assert.match(source, /operationID:\s*newEditOperationID\(\)/);
  assert.match(source, /options\.editFork\(\{/);
  assert.match(source, /expected_revision:\s*currentEditor\.expectedRevision/);
  assert.match(source, /options\.copyText\(editor\.original\)/);
  assert.doesNotMatch(source, /navigator\.clipboard/);
  assert.match(source, /编辑内容已保留/);
});

test("app 只调用原子 EditUserMessageAndFork，不在前端拼 Fork + Submit", async () => {
  const source = await readFile(new URL("./app.js", import.meta.url), "utf8");
  assert.match(source, /invoke\("EditUserMessageAndFork", parentID, request\)/);
  assert.doesNotMatch(source, /invoke\("ForkSession",\s*parentID[\s\S]{0,200}invoke\("SubmitToSession"/);
});

test("编辑分叉请求字段使用后端 JSON 契约的 snake_case", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /target_event_seq:\s*currentEditor\.eventSeq/);
  assert.match(source, /new_text:\s*text/);
  assert.match(source, /expected_revision:\s*currentEditor\.expectedRevision/);
  assert.match(source, /operation_id:\s*currentEditor\.operationID/);
  const request = source.slice(source.indexOf("options.editFork({"), source.indexOf("});", source.indexOf("options.editFork({")));
  assert.doesNotMatch(request, /targetEventSeq:|expectedRevision:|operationId:/);
});
