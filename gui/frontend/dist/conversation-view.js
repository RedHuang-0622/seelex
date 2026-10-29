import { createConversationWheel } from "./conversation-wheel.js";
import { createEmbedFrameBuffer } from "./embed-buffering.js";
import { hydrateMessageImages } from "./message-images.js";
import { markEntering, prefersReducedMotion } from "./motion.js";
import { animateSpring } from "./spring-motion.js";

const BOTTOM_THRESHOLD = 72;

// shouldAutoLoadOlder 判定"顶部 sentinel 的自动触发"是否真的应当翻更早一页。
//
// 抽成纯函数是为了让这条纪律可测（view 本身要真 DOM + 轮轴）：它是"看不到最新消息"
// 那类问题的唯一判据——**用户停在尾部时一律不自动翻页**。
export function shouldAutoLoadOlder(state) {
  if (!state?.hasLoader) return false;
  if (state.followsTail) return false;
  if (!state.canLoadMore || state.loadingOlder) return false;
  return Boolean(state.sentinelArmed);
}

// conversationRenderProbe 区分“后端没给内容”和“前端拿到却没显示”。它只生成
// 诊断读数，不参与布局或状态：F12 里看 window.__SEELEX_CONVERSATION_PROBE__，
// 或直接看 #conversation[data-conversation-probe]。
export function conversationRenderProbe({
  targetSessionID = "",
  viewSessionID = "",
  snapshotCount = 0,
  renderedCount = 0,
  visibleCount = 0,
  maxRowHeight = 0,
  containerHeight = 0,
  maxRowKind = "",
  maxGap = 0,
  gapAfterKind = "",
  hiddenRowCount = 0,
  loading = false
} = {}) {
  const targetMatched = !targetSessionID || targetSessionID === viewSessionID;
  let verdict = "ok";
  if (loading) verdict = "loading";
  else if (!targetMatched) verdict = "target-not-loaded";
  else if (snapshotCount <= 0) verdict = "snapshot-empty";
  else if (renderedCount <= 0) verdict = "frontend-dropped";
  else if (visibleCount <= 0) verdict = "layout-hidden";
  else if (hiddenRowCount > 0) verdict = "rows-transparent";
  else if (maxGap > Math.max(containerHeight * 0.4, 300)) verdict = "layout-gap";
  else if (maxRowHeight > Math.max(containerHeight * 1.6, 1000)) verdict = "tall-row";
  return {
    at: Date.now(),
    verdict,
    targetSessionID,
    viewSessionID,
    targetMatched,
    snapshotCount,
    renderedCount,
    visibleCount,
    maxRowHeight: Math.max(Number(maxRowHeight) || 0, 0),
    containerHeight: Math.max(Number(containerHeight) || 0, 0),
    maxRowKind: String(maxRowKind || ""),
    maxGap: Math.max(Number(maxGap) || 0, 0),
    gapAfterKind: String(gapAfterKind || ""),
    hiddenRowCount: Math.max(Math.trunc(Number(hiddenRowCount)) || 0, 0)
  };
}

export function createConversationView(container, options = {}) {
  const htmlByKey = new Map();
  let payloads = new Map();
  let followsTail = true;
  let canLoadMore = false;
  let loadingOlder = false;
  let sentinelArmed = true;
  let currentRevision = 0;
  let activeSessionID = "";
  let activeHadContent = false;
  let previousStreaming = false;
  let editor = null;
  const sentinel = document.createElement("div");
  sentinel.className = "conversation-sentinel";
  sentinel.dataset.conversationSentinel = "top";
  sentinel.setAttribute("aria-hidden", "true");
  container.prepend(sentinel);
  // 右侧「会话内全量用户输入索引」：索引本身由宿主（app.js）经
  // invoke("SessionInputIndex", sessionID) 拉回后写入（inputIndex(...)）；
  // 点击到尚未加载的早期输入时，轮轴经 locateInput 让宿主先回读那一页。
  const wheel = createConversationWheel(container, { locateInput: options.locateInput });

  // 沙箱渲染块的帧缓冲（embed-buffering.js）。这里只做接线：换行前 adopt、换行后
  // attach、行被移出时 release。**缓冲算法一概不写进 reconcile** —— 闪屏的根因是
  // "行 html 一变就整行重建、iframe 跟着重建"，判据（领养/挂载/延迟/卸载）在
  // embed-buffering.js 里是纯函数，可离线单测。
  const embedBuffer = options.embedBuffer || createEmbedFrameBuffer({
    mountRoot: options.embedMountRoot || null
  });

  // loadOlder 翻更早的一页。
  //
  // **只在用户真的离开尾部时**才自动翻：可见窗口是"从 history_offset 起的连续一段"
  // （后端 history_window），翻更早页会把窗口整体后退，最新的那一段随之离开 DOM。
  // 用户停在尾部时他看的就是最新内容，此时任何自动触发（IntersectionObserver 在容器
  // display:none 后重新显示、布局变化把 sentinel 带进 rootMargin、侧栏压缩改变可用
  // 高度）都会把最新消息静默移出窗口——这正是"看不到最新消息"最容易复现的一条。
  // 显式的「加载更早」按钮走宿主命令（invoke LoadMoreHistory），不受这里限制。
  async function loadOlder() {
    if (!shouldAutoLoadOlder({
      canLoadMore,
      followsTail,
      loadingOlder,
      sentinelArmed,
      hasLoader: typeof options.loadMore === "function"
    })) return;
    sentinelArmed = false;
    loadingOlder = true;
    try { await options.loadMore(); }
    finally { loadingOlder = false; }
  }

  if (typeof IntersectionObserver === "function") {
    const observer = new IntersectionObserver(entries => {
      const visible = entries.some(entry => entry.isIntersecting);
      if (!visible) sentinelArmed = true;
      else void loadOlder();
    }, { root: container, rootMargin: "160px 0px 0px" });
    observer.observe(sentinel);
  }
  container.addEventListener("scroll", () => {
    followsTail = isNearBottom(container);
    wheel.updateViewport();
    if (container.scrollTop > 240) sentinelArmed = true;
    else if (typeof IntersectionObserver !== "function") void loadOlder();
  }, { passive: true });
  container.addEventListener("click", event => {
    const editButton = event.target.closest?.("[data-edit-message]");
    if (editButton) {
      event.preventDefault();
      if (editButton.disabled || editButton.dataset.editDisabled === "true") {
        options.notify?.(editButton.title || "这条消息不能编辑");
        return;
      }
      openMessageEditor(editButton);
      return;
    }
    return handleAction(event, () => payloads, options);
  });

  function openMessageEditor(button) {
    const key = String(button.dataset.editMessage || "");
    const article = button.closest?.(".message");
    if (!key || !article) return;
    captureEditorText();
    closeMessageEditor();
    const original = payloads.get(`message-copy:${key}`) || "";
    editor = {
      key,
      original,
      text: original,
      eventSeq: Number(button.dataset.eventSeq || 0),
      expectedRevision: currentRevision,
      operationID: newEditOperationID(),
      loading: false,
      status: ""
    };
    mountMessageEditor(article);
  }

  function closeMessageEditor() {
    container.querySelectorAll(".message-edit-panel").forEach(panel => panel.remove());
    editor = null;
  }

  function captureEditorText() {
    if (!editor) return;
    const panel = container.querySelector(".message-edit-panel");
    const textarea = panel?.querySelector("textarea");
    if (textarea) editor.text = textarea.value;
  }

  function mountMessageEditor(article) {
    if (!editor || !article) return;
    article.querySelectorAll(".message-edit-panel").forEach(panel => panel.remove());
    const panel = document.createElement("section");
    panel.className = "message-edit-panel";
    panel.dataset.editKey = editor.key;
    panel.innerHTML = `<div class="message-edit-head"><strong>编辑并分叉</strong><span>原会话保持不变，新输入会提交到子会话</span></div>
      <textarea aria-label="编辑用户消息原文"></textarea>
      <div class="message-edit-actions">
        <span class="message-edit-status" role="status"></span>
        <button type="button" class="message-action" data-edit-cancel>取消</button>
        <button type="button" class="message-action" data-edit-copy>重新复制原文</button>
        <button type="button" class="message-edit-submit" data-edit-submit>${editor.loading ? "正在创建分支…" : "编辑并分叉发送"}</button>
      </div>`;
    const textarea = panel.querySelector("textarea");
    textarea.value = editor.text;
    const status = panel.querySelector(".message-edit-status");
    if (status) status.textContent = editor.status;
    panel.querySelector("[data-edit-cancel]")?.addEventListener("click", closeMessageEditor);
    panel.querySelector("[data-edit-copy]")?.addEventListener("click", async () => {
      try {
        await options.copyText(editor.original);
        options.notify?.("已复制原文");
      } catch (error) {
        options.notify?.(error);
      }
    });
    panel.querySelector("[data-edit-submit]")?.addEventListener("click", () => {
      void submitMessageEditor(panel, textarea);
    });
    const submit = panel.querySelector("[data-edit-submit]");
    if (submit) submit.disabled = Boolean(editor.loading);
    article.appendChild(panel);
    textarea.focus();
    textarea.setSelectionRange(textarea.value.length, textarea.value.length);
  }

  async function submitMessageEditor(panel, textarea) {
    if (!editor || editor.loading) return;
    const currentEditor = editor;
    const text = String(textarea.value || "").trim();
    const status = panel.querySelector(".message-edit-status");
    const submit = panel.querySelector("[data-edit-submit]");
    if (!text) {
      if (status) status.textContent = "编辑后的输入不能为空";
      textarea.focus();
      return;
    }
    if (typeof options.editFork !== "function") {
      if (status) status.textContent = "当前宿主未装配编辑分叉能力";
      return;
    }
    currentEditor.text = textarea.value;
    currentEditor.loading = true;
    currentEditor.status = "";
    if (submit) {
      submit.disabled = true;
      submit.textContent = "正在创建分支…";
    }
    if (status) status.textContent = "";
    try {
      const result = await options.editFork({
        target_event_seq: currentEditor.eventSeq,
        new_text: text,
        expected_revision: currentEditor.expectedRevision,
        operation_id: currentEditor.operationID
      });
      options.notify?.(`已创建分支 ${shortSessionID(result?.child_session_id || result?.ChildSessionID || "")}`);
      closeMessageEditor();
    } catch (error) {
      currentEditor.loading = false;
      currentEditor.status = `提交失败：${error?.message || String(error)}；编辑内容已保留`;
      if (submit) {
        submit.disabled = false;
        submit.textContent = "编辑并分叉发送";
      }
      const article = container.querySelector(`[data-conversation-key="${CSS.escape(currentEditor.key)}"]`);
      if (article) mountMessageEditor(article);
      else if (status) status.textContent = currentEditor.status;
    }
  }

  return {
    render(model, options = {}) {
      captureEditorText();
      const before = scrollState(container, followsTail);
      const anchor = captureScrollAnchor(container);
      payloads = model.payloads;
      currentRevision = Number(options.revision || 0);
      canLoadMore = Boolean(options.hasMoreHistory);
      const sessionID = String(container.dataset?.sessionId || "");
      const hasContent = model.items.length > 0;
      const repaintBoundary = shouldRepaintConversationBoundary({
        previousSessionID: activeSessionID,
        nextSessionID: sessionID,
        hadContent: activeHadContent,
        hasContent
      });
      const sessionChanged = activeSessionID !== "" && sessionID !== "" && sessionID !== activeSessionID;
      if (sessionChanged) {
        // message IDs are only unique inside one session. Reusing keyed rows across
        // sessions can preserve the previous conversation's DOM, payloads and
        // expanded states under colliding IDs. A session boundary is therefore a
        // full row rebuild, never a keyed patch.
        htmlByKey.clear();
        for (const node of container.querySelectorAll?.("[data-conversation-key]") || []) {
          embedBuffer.release(node);
          node.remove();
        }
      }
      const streamPhase = String(options.streamPhase || (options.streaming ? "receiving" : "idle"));
      const existingKeys = new Set(
        [...container.querySelectorAll?.("[data-conversation-key]") || []]
          .map(node => String(node.dataset?.conversationKey || ""))
          .filter(Boolean)
      );
      const newItemCount = model.items.filter(item => !existingKeys.has(item.key)).length;
      // 流式标记：会话还在跑的时候一帧都不许新建（见 embed-buffering.js 的
      // shouldDeferMount）。chat-view.js 传的是 chat.running。
      embedBuffer.setStreaming(Boolean(options.streaming));
      container.setAttribute?.("aria-busy", options.streaming ? "true" : "false");
      container.dataset.streamPhase = streamPhase;
      reconcile(container, model.items, htmlByKey, payloads, {
        streaming: Boolean(options.streaming),
        streamPhase,
        embedBuffer,
        animateEnter: !repaintBoundary && existingKeys.size > 0 && newItemCount > 0 && newItemCount <= 2
      });
      // 用户消息随行图片：结构渲染完就把 ref 注水成可显示的缩略图。注水按 ref
      // 缓存（message-images.js），流式期间每帧调一次也不会重复打 RPC；第一次
      // 进会话/换会话时同理。
      hydrateMessageImages(container);
      const renderedRows = [...container.querySelectorAll?.("[data-conversation-key]") || []];
      const rowMetrics = renderedRows.map(node => {
        const rect = node.getBoundingClientRect?.();
        const style = globalThis.getComputedStyle?.(node);
        return {
          node,
          key: String(node.dataset?.conversationKey || ""),
          kind: String(node.dataset?.wheelKind || node.className || "row"),
          top: Number(rect?.top) || 0,
          bottom: Number(rect?.bottom) || 0,
          height: Number(rect?.height) || 0,
          painted: !style || (Number(style.opacity || 1) > 0.01 && style.visibility !== "hidden")
        };
      });
      const tallestRow = rowMetrics.reduce((max, row) => row.height > max.height ? row : max, { height: 0, kind: "", key: "" });
      let largestGap = { gap: 0, afterKind: "", afterKey: "" };
      for (let index = 1; index < rowMetrics.length; index += 1) {
        const previous = rowMetrics[index - 1];
        const gap = rowMetrics[index].top - previous.bottom;
        if (gap > largestGap.gap) {
          largestGap = { gap, afterKind: previous.kind, afterKey: previous.key };
        }
      }
      const probe = conversationRenderProbe({
        targetSessionID: String(container.dataset?.targetSessionId || ""),
        viewSessionID: sessionID,
        snapshotCount: Number(options.conversationCount || 0),
        renderedCount: renderedRows.length,
        visibleCount: rowMetrics.filter(row => row.height > 0 && row.painted).length,
        maxRowHeight: tallestRow.height,
        containerHeight: Number(container.clientHeight) || 0,
        maxRowKind: tallestRow.kind,
        maxGap: largestGap.gap,
        gapAfterKind: largestGap.afterKind,
        hiddenRowCount: rowMetrics.filter(row => row.height > 0 && !row.painted).length,
        loading: Boolean(options.loading)
      });
      probe.maxRowKey = tallestRow.key;
      probe.gapAfterKey = largestGap.afterKey;
      const previousProbe = globalThis.__SEELEX_CONVERSATION_PROBE__;
      globalThis.__SEELEX_CONVERSATION_PROBE__ = {
        current: probe,
        history: [...(previousProbe?.history || []), probe].slice(-20)
      };
      container.dataset.conversationProbe = `${probe.verdict}:${probe.snapshotCount}/${probe.renderedCount}/${probe.visibleCount}`;
      if (previousStreaming && !options.streaming && !repaintBoundary) settleCompletedTurn(container);
      previousStreaming = Boolean(options.streaming) && !repaintBoundary;
      restoreScroll(container, before, options.scrollMode || "auto", anchor);
      activeSessionID = sessionID;
      activeHadContent = hasContent;
      if (editor) {
        const article = container.querySelector(`[data-conversation-key="${CSS.escape(editor.key)}"]`);
        if (article) mountMessageEditor(article);
        else if (!editor.loading) closeMessageEditor();
      }
      // 几何来自 DOM：加载更早历史/增量新消息/换行后线表自动重建。
      wheel.refresh();
      followsTail = isNearBottom(container);
      if (repaintBoundary && shouldFollowTail(options.scrollMode || "auto", before.followsTail)) {
        const settleTail = () => settleConversationTailAtSessionBoundary(container, true);
        if (typeof globalThis.requestAnimationFrame === "function") {
          globalThis.requestAnimationFrame(() => globalThis.requestAnimationFrame(settleTail));
        } else {
          settleTail();
        }
      }
      return {
        repaintBoundary,
        followTail: shouldFollowTail(options.scrollMode || "auto", before.followsTail),
        probe
      };
    },
    payload(key) { return payloads.get(key) || ""; },
    // inputIndex 写入全量用户输入索引（后端 SessionInputIndex 载荷；null =
    // 退回「窗口内用户行」临时模式）。返回本次刻度表。
    inputIndex(payload) { return wheel.setIndex(payload); },
    // wheelRounds 返回当前刻度表（宿主定位编排/诊断用）。
    wheelRounds() { return wheel.rounds(); }
  };
}

function newEditOperationID() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  return `edit-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function shortSessionID(sessionID) {
  const value = String(sessionID || "");
  return value.length > 14 ? `${value.slice(0, 7)}…${value.slice(-5)}` : value;
}

async function handleAction(event, getPayloads, options) {
  const copyButton = event.target.closest("[data-copy]");
  if (copyButton) {
    const value = getPayloads().get(copyButton.dataset.copy) || "";
    try {
      await options.copyText(value);
      options.notify("已复制");
    } catch (error) { options.notify(error); }
    return;
  }
  const expandButton = event.target.closest("[data-expand]");
  if (expandButton) {
    const panel = expandButton.closest(".io-panel");
    const key = expandButton.dataset.expand;
    const value = getPayloads().get(key) || "";
    const pre = panel?.querySelector("pre");
    const label = expandButton.querySelector("span");
    if (panel?.classList.contains("expanded")) {
      panel.classList.remove("expanded");
      pre?.replaceChildren(document.createTextNode(expandButton.dataset.original || ""));
      if (label) label.textContent = `+${expandButton.dataset.hiddenChars || "0"} chars`;
      expandButton.title = "展开完整内容";
      return;
    }
    expandButton.dataset.original = pre?.textContent || "";
    expandButton.dataset.hiddenChars = (label?.textContent || "").replace(/\D/g, "");
    pre?.replaceChildren(document.createTextNode(value));
    panel?.classList.add("expanded");
    if (label) label.textContent = "收回";
    expandButton.title = "收回完整内容";
    return;
  }
  // 快照截断输出的"加载完整输出"：按 result_ref 从后端分页拉全文
  // （复用 read_tool_result 通道），拉回后替换预览并展开；再点收回。
  const loadButton = event.target.closest("[data-load-ref]");
  if (loadButton) {
    const panel = loadButton.closest(".io-panel");
    const ref = loadButton.dataset.loadRef;
    const details = panel?.querySelector("details.io-collapse");
    const pre = panel?.querySelector("pre");
    if (details?.open && panel?.classList.contains("expanded")) {
      details.open = false;
      panel.classList.remove("expanded");
      const label = loadButton.querySelector("span");
      if (label) label.textContent = "加载完整输出";
      loadButton.title = "加载完整输出";
      return;
    }
    if (!panel || panel.classList.contains("loading-full")) return;
    panel.classList.add("loading-full");
    const label = loadButton.querySelector("span");
    if (label) label.textContent = "加载中…";
    loadButton.disabled = true;
    try {
      const page = await options.loadResultRef(ref, 0, options.resultPageLimit || 12000);
      const text = page?.content || "";
      if (pre) pre.replaceChildren(document.createTextNode(text));
      if (details) details.open = true;
      panel.classList.add("expanded");
      panel.dataset.fullRef = ref;
      if (label) label.textContent = page?.has_more ? "加载更多" : "收回完整输出";
      loadButton.title = "收回完整输出";
      loadButton.dataset.fullLoaded = "1";
      if (page?.has_more && page.next_offset > 0 && typeof options.loadResultRef === "function") {
        loadButton.dataset.nextOffset = String(page.next_offset);
      }
    } catch (error) {
      if (label) label.textContent = "加载失败";
      options.notify?.(error);
    } finally {
      panel.classList.remove("loading-full");
      loadButton.disabled = false;
    }
    return;
  }
}

function reconcile(container, items, htmlByKey, payloads, buffers = {}) {
  const embedBuffer = buffers.embedBuffer;
  const streaming = Boolean(buffers.streaming);
  const existing = new Map([...container.children]
    .filter(node => node.dataset.conversationKey)
    .map(node => [node.dataset.conversationKey, node]));
  const desired = new Set();
  const sentinel = container.querySelector("[data-conversation-sentinel]");
  let cursor = sentinel ? sentinel.nextElementSibling : container.firstElementChild;
  for (const item of items) {
    desired.add(item.key);
    let node = existing.get(item.key);
    const hadNode = Boolean(node);
    let fresh = false;
    if (!node || htmlByKey.get(item.key) !== item.html) {
      const replacement = elementFromHTML(item.html);
      const patched = Boolean(node && canPatchMessageBody(htmlByKey.get(item.key), item.html));
      if (patched) {
        // 流式正文更新只替换稳定消息的 body；外壳、操作按钮、reasoning 展开态
        // 和焦点所在的 article 身份都保留，绝不按 token 重建整条消息。
        patchMessageBody(node, replacement, payloads, embedBuffer, { streaming });
      } else if (node) {
        const wasCursor = node === cursor;
        const uiState = captureUIState(node);
        // 换行**之前**：让缓冲区把旧行里内容没变的沙箱帧领养进新行（srcdoc 一致就
        // 原样搬过去，不重新加载），内容变了的帧继续占着可视位等新帧 load 完。
        embedBuffer?.adopt(node, replacement, { streaming });
        node.replaceWith(replacement);
        restoreUIState(replacement, uiState, payloads);
        if (wasCursor) cursor = replacement;
      } else {
        // 新出现的条目（新消息 / 一次新的工具过程）入场一次：keyed reconcile
        // 只在**首次插入**时挂类，后续流式替换不重播（否则工具面板会一直闪）。
        if (buffers.animateEnter !== false && item.meta?.kind !== "loading") {
          markEntering(replacement, "is-entering-row");
        }
        embedBuffer?.adopt(null, replacement, { streaming });
      }
      if (!patched) node = replacement;
      if (hadNode && !patched) {
        if (item.meta?.motion === "tool-update") animateToolDomainUpdate(replacement);
        if (item.meta?.kind === "message" && item.meta.hasReasoning) animateThinkingUpdate(replacement);
      }
      fresh = true;
      htmlByKey.set(item.key, item.html);
    }
    if (node !== cursor) container.insertBefore(node, cursor);
    // 换行**之后**（行进文档）才发起离屏挂载：流式期间只留同高占位。
    if (fresh) embedBuffer?.attach(node, { streaming });
    cursor = node.nextElementSibling;
  }
  while (cursor) {
    const next = cursor.nextElementSibling;
    htmlByKey.delete(cursor.dataset.conversationKey);
    // 行被移出对话流：撤在途挂载、摘监听、清 pending 帧（不留在离屏容器里）。
    embedBuffer?.release(cursor);
    cursor.remove();
    cursor = next;
  }
  for (const key of [...htmlByKey.keys()]) if (!desired.has(key)) htmlByKey.delete(key);
}

function animateToolDomainUpdate(node) {
  if (!node || prefersReducedMotion()) return;
  const target = node.querySelector?.(".tool-call-summary") || node;
  const animation = animateSpring(target, "settle", [
    { opacity: .72, transform: "translateY(10px)" },
    { opacity: 1, transform: "translateY(0)" }
  ], { fill: "none" });
  animation?.finished?.then(() => animation.cancel()).catch(() => {});
}

function animateThinkingUpdate(node) {
  if (!node || prefersReducedMotion()) return;
  const target = node.querySelector?.(".reasoning-summary");
  if (!target) return;
  const animation = animateSpring(target, "settle", [
    { opacity: .78, transform: "translateY(6px)" },
    { opacity: 1, transform: "translateY(0)" }
  ], { fill: "none" });
  animation?.finished?.then(() => animation.cancel()).catch(() => {});
}

function settleCompletedTurn(container) {
  if (!container || prefersReducedMotion()) return;
  const assistant = [...container.querySelectorAll(".message.assistant")].at(-1);
  if (!assistant) return;
  const body = assistant.querySelector(".message-body") || assistant;
  animateSpring(body, "settle", [
    { opacity: .86, transform: "translateY(4px)" },
    { opacity: 1, transform: "translateY(0)" }
  ], { fill: "none" });
  animateThinkingUpdate(assistant);
}

function messageBodyBounds(html) {
  const value = String(html || "");
  const start = value.indexOf('<div class="message-body" data-message-body="true">');
  if (start < 0) return null;
  const end = value.indexOf("<!--/seelex-message-body-->", start);
  if (end < 0) return null;
  return { start, end, endMarker: end + "<!--/seelex-message-body-->".length };
}

// canPatchMessageBody 只比较 body 以外的稳定外壳。同一 message key 的 content 增长
// 可以走局部 patch；角色、元数据、操作按钮或邻接状态改变时仍走 keyed 替换。
export function canPatchMessageBody(previousHtml, nextHtml) {
  const previous = messageBodyBounds(previousHtml);
  const next = messageBodyBounds(nextHtml);
  if (!previous || !next) return false;
  const previousStart = String(previousHtml).slice(0, previous.start);
  const nextStart = String(nextHtml).slice(0, next.start);
  const previousEnd = String(previousHtml).slice(previous.endMarker);
  const nextEnd = String(nextHtml).slice(next.endMarker);
  return previousStart === nextStart && previousEnd === nextEnd;
}

function patchMessageBody(node, replacement, payloads, embedBuffer, options = {}) {
  const currentBody = node.querySelector?.(".message-body");
  const nextBody = replacement?.querySelector?.(".message-body");
  if (!currentBody || !nextBody) return false;
  const uiState = captureUIState(node);
  embedBuffer?.adopt(node, replacement, { streaming: Boolean(options.streaming) });
  currentBody.replaceChildren(...nextBody.childNodes);
  restoreUIState(node, uiState, payloads);
  return true;
}

function captureUIState(node) {
  return {
    details: [...node.querySelectorAll("details")].map(details => details.open),
    expanded: new Set([...node.querySelectorAll(".io-panel.expanded")].map(panel => panel.dataset.payload))
  };
}

function restoreUIState(node, state, payloads) {
  [...node.querySelectorAll("details")].forEach((details, index) => {
    if (state.details[index] !== undefined) details.open = state.details[index];
  });
  for (const panel of node.querySelectorAll(".io-panel")) {
    if (!state.expanded.has(panel.dataset.payload)) continue;
    const pre = panel.querySelector("pre");
    const toggle = panel.querySelector("[data-expand]");
    const label = toggle?.querySelector("span");
    if (toggle) toggle.dataset.hiddenChars = (label?.textContent || "").replace(/\D/g, "");
    if (toggle) toggle.dataset.original = pre?.textContent || "";
    pre?.replaceChildren(document.createTextNode(payloads.get(panel.dataset.payload) || ""));
    panel.classList.add("expanded");
    if (toggle) {
      if (label) label.textContent = "收回";
      toggle.title = "收回完整内容";
    }
  }
}

function elementFromHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html.trim();
  return template.content.firstElementChild;
}

function scrollState(container, followsTail) {
  return {
    top: container.scrollTop,
    height: container.scrollHeight,
    followsTail: followsTail || isNearBottom(container)
  };
}

// captureScrollAnchor 记住视口顶部第一条可见消息（key + 相对视口位置）。
// 加载更早历史时预置的新页会让内容整体下移，靠这条锚点把用户正在读的那一条
// 按回原处——比「按 scrollHeight 增量」稳：窗口整体后退（尾部被截）时高度几乎
// 不变，增量算法会失效并把用户甩到别的位置。
export function captureScrollAnchor(container) {
  const containerTop = container.getBoundingClientRect().top;
  for (const node of container.children) {
    if (!node.dataset || !node.dataset.conversationKey) continue;
    const rect = node.getBoundingClientRect();
    if (rect.bottom <= containerTop + 1) continue;
    return { key: node.dataset.conversationKey, top: rect.top - containerTop };
  }
  return null;
}

// restoreScrollAnchor 把锚点消息放回原来的视口位置；锚点已不在窗口内（被
// 截掉的尾页/会话切换）时返回 false，调用方退回粗略的 scrollHeight 增量。
export function restoreScrollAnchor(container, anchor) {
  if (!anchor || !anchor.key) return false;
  const node = container.querySelector(`[data-conversation-key="${CSS.escape(anchor.key)}"]`);
  if (!node) return false;
  const containerTop = container.getBoundingClientRect().top;
  const delta = node.getBoundingClientRect().top - containerTop - anchor.top;
  if (!delta) return true;
  setScrollInstantly(container, container.scrollTop + delta);
  return true;
}

export function shouldFollowTail(mode, followsTail) {
  return mode === "bottom" || (mode === "auto" && Boolean(followsTail));
}

// setScrollInstantly 立即设置滚动位置：.conversation 声明了
// scroll-behavior: smooth，直接赋值会变成动画（锚点恢复、轮轴拖拽都会「飘」）。
function setScrollInstantly(container, top) {
  const previous = container.style.scrollBehavior;
  container.style.scrollBehavior = "auto";
  container.scrollTop = top;
  container.style.scrollBehavior = previous;
}

function restoreScroll(container, before, mode, anchor) {
  if (mode === "anchor") {
    if (restoreScrollAnchor(container, anchor)) return;
    setScrollInstantly(container, before.top + Math.max(container.scrollHeight - before.height, 0));
    return;
  }
  if (shouldFollowTail(mode, before.followsTail)) {
    setScrollInstantly(container, container.scrollHeight);
  }
}

// settleConversationTailAtSessionBoundary runs after the rebuilt session has
// completed layout, so it uses the new scrollHeight rather than the previous
// session's height or an intermediate markdown/embed measurement.
export function settleConversationTailAtSessionBoundary(container, followTail) {
  if (!container) return;
  setScrollInstantly(container, followTail ? container.scrollHeight : container.scrollTop);
}

export function shouldRepaintConversationBoundary({
  previousSessionID = "",
  nextSessionID = "",
  hadContent = false,
  hasContent = false
} = {}) {
  const sessionChanged = previousSessionID !== "" && nextSessionID !== "" && previousSessionID !== nextSessionID;
  return sessionChanged || (!hadContent && hasContent);
}

function isNearBottom(container) {
  return container.scrollHeight - container.scrollTop - container.clientHeight <= BOTTOM_THRESHOLD;
}
