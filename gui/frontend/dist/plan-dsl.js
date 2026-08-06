const NODE_STATUSES = new Set([
  "pending", "queued", "running", "worktree_creating", "rebasing", "merging", "completed", "failed", "aborted",
  "skipped", "canceled", "panicked"
]);

const PLAN_STATUSES = new Set(["pending", "running", "completed", "failed", "aborted"]);
const FAILURE_STATUSES = new Set(["failed", "aborted", "canceled", "panicked"]);
const DONE_STATUSES = new Set(["completed", "skipped"]);
const ACTIVE_STATUSES = new Set(["queued", "running", "worktree_creating", "rebasing", "merging"]);

export function planToDSL(plan) {
  if (!isRecord(plan)) return null;

  const nodes = [];
  const nodeByID = new Map();
  const keyCounts = new Map();

  const visit = (input, parentKey = "", inheritedDepth = 0) => {
    if (!Array.isArray(input)) return;
    input.forEach((rawNode, index) => {
      if (!isRecord(rawNode)) return;
      const id = textValue(rawNode.id);
      const baseKey = id || `${parentKey || "root"}/${index + 1}`;
      const key = uniqueKey(baseKey, keyCounts);
      const explicitDepth = finiteNumber(rawNode.depth);
      const node = {
        type: "node",
        key,
        id,
        parentKey,
        label: textValue(rawNode.label, id || `Step ${nodes.length + 1}`),
        kind: textValue(rawNode.kind, "auto"),
        status: normalizeStatus(rawNode.status, NODE_STATUSES),
        depth: Math.max(0, Math.min(explicitDepth ?? inheritedDepth, 12)),
        elapsed: textValue(rawNode.elapsed),
        output: textValue(rawNode.output),
        events: normalizeNodeEvents(rawNode.events),
        toolEvents: normalizeToolEvents(rawNode.tool_events),
        incoming: [],
        outgoing: []
      };
      nodes.push(node);
      if (id && !nodeByID.has(id)) nodeByID.set(id, node);
      visit(rawNode.children, key, node.depth + 1);
    });
  };
  visit(plan.nodes);

  const edgeCounts = new Map();
  const edges = [];
  if (Array.isArray(plan.edges)) {
    plan.edges.forEach((rawEdge, index) => {
      if (!isRecord(rawEdge)) return;
      const from = textValue(rawEdge.from);
      const to = textValue(rawEdge.to);
      if (!from && !to) return;
      const label = textValue(rawEdge.label);
      const condition = structuredText(rawEdge.condition);
      const source = nodeByID.get(from);
      const target = nodeByID.get(to);
      const baseKey = `${from || "?"}->${to || "?"}${label ? `:${label}` : ""}`;
      const edge = {
        type: "edge",
        key: uniqueKey(baseKey || `edge-${index + 1}`, edgeCounts),
        from,
        to,
        label,
        condition,
        targetLabel: target?.label || "", // 节点内嵌分支的目标名（Dify 树语义）
        status: edgeDisplayStatus(source?.status, target?.status),
        dangling: !source || !target
      };
      edges.push(edge);
      // Dify 式图语义：节点持有完整上下游边对象（非 key 摘要）——
      // 渲染时节点内嵌分支（outgoing 目标/条件/并行）与依赖（incoming）。
      if (source) source.outgoing.push(edge);
      if (target) target.incoming.push(edge);
    });
  }

  const progress = clamp(finiteNumber(plan.progress) ?? 0, 0, 1);
  const entryNodeID = textValue(plan.entry_node_id);
  const name = textValue(plan.name, entryNodeID || "Plan");
  const status = normalizeStatus(plan.status, PLAN_STATUSES);
  // 门禁模式：plan_run 在途（status=running）时节点打勾来自执行事件投影，徽标
  // 显示 PLAN RUN；其余状态是 tasklist 门禁——主代理串行执行，节点打勾来自
  // task_check_node，徽标显示 TASKLIST。
  const mode = status === "running" ? "plan" : "tasklist";
  return {
    schema: "seelex.plan/v1",
    type: "plan",
    key: entryNodeID || name || "plan",
    name,
    entryNodeID,
    mode,
    status,
    progress,
    progressPercent: Math.round(progress * 100),
    elapsed: textValue(plan.elapsed),
    nodes,
    edges
  };
}

export function renderPlanDSL(dsl) {
  if (!dsl) return "";
  const status = statusToken(dsl.status);
  return `<section class="plan-board is-${status}" data-plan-board data-plan-key="${escapeHTML(dsl.key)}" data-plan-status="${status}">
    <header class="plan-board-header">
      <span class="plan-board-icon" aria-hidden="true">◇</span>
      <strong data-plan-field="name">${escapeHTML(dsl.name)}</strong>
      <span class="plan-mode is-${statusToken(dsl.mode)}" data-plan-field="mode" title="${modeTitle(dsl.mode)}">${escapeHTML(modeLabel(dsl.mode))}</span>
      <span class="plan-status is-${status}" data-plan-field="status">${escapeHTML(statusLabel(dsl.status))}</span>
      <span class="plan-board-meta" data-plan-field="progress-label">${dsl.progressPercent}%</span>
      <span class="plan-board-elapsed${dsl.elapsed ? "" : " hidden"}" data-plan-field="elapsed">${escapeHTML(dsl.elapsed)}</span>
    </header>
    <div class="plan-board-progress" role="progressbar" aria-label="Plan progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${dsl.progressPercent}">
      <div class="plan-board-bar" data-plan-field="progress-bar" style="width:${dsl.progressPercent}%"></div>
    </div>
    <div class="plan-dsl-summary"><span>${dsl.nodes.length} nodes</span><span>${dsl.edges.length} edges</span><span data-plan-field="entry">entry: ${escapeHTML(dsl.entryNodeID || "—")}</span></div>
    <div class="plan-dsl-nodes" data-plan-nodes>${dsl.nodes.map(renderNode).join("") || '<div class="plan-dsl-empty">No plan nodes</div>'}</div>
    ${renderPlanInstrumentation(dsl)}
    <section class="plan-edge-section${dsl.edges.length ? "" : " hidden"}" data-plan-edge-section>
      <div class="plan-edge-title">Dependencies</div>
      <div class="plan-edge-list" data-plan-edges>${dsl.edges.map(renderEdge).join("")}</div>
    </section>
  </section>`;
}

export function reconcilePlanDSL(container, dsl) {
  if (!container) return;
  if (!dsl) {
    container.className = "plan-view muted";
    container.removeAttribute("data-plan-key");
    container.textContent = "暂无执行计划";
    return;
  }

  container.className = "plan-view";
  const board = container.querySelector("[data-plan-board]");
  if (!board || board.dataset.planKey !== dsl.key) {
    container.innerHTML = renderPlanDSL(dsl);
    container.dataset.planKey = dsl.key;
    return;
  }

  container.dataset.planKey = dsl.key;
  updateBoard(board, dsl);
}

function updateBoard(board, dsl) {
  const status = statusToken(dsl.status);
  board.className = `plan-board is-${status}`;
  board.dataset.planStatus = status;
  setText(board, "name", dsl.name);
  setMode(board, dsl.mode);
  setText(board, "status", statusLabel(dsl.status));
  const statusElement = board.querySelector('[data-plan-field="status"]');
  if (statusElement) statusElement.className = `plan-status is-${status}`;
  setText(board, "progress-label", `${dsl.progressPercent}%`);
  setText(board, "elapsed", dsl.elapsed);
  const elapsed = board.querySelector('[data-plan-field="elapsed"]');
  elapsed?.classList.toggle("hidden", !dsl.elapsed);
  setText(board, "entry", `entry: ${dsl.entryNodeID || "—"}`);

  const progress = board.querySelector(".plan-board-progress");
  progress?.setAttribute("aria-valuenow", String(dsl.progressPercent));
  const bar = board.querySelector('[data-plan-field="progress-bar"]');
  if (bar) bar.style.width = `${dsl.progressPercent}%`;

  const summary = board.querySelector(".plan-dsl-summary");
  if (summary) {
    const spans = summary.querySelectorAll("span");
    if (spans[0]) spans[0].textContent = `${dsl.nodes.length} nodes`;
    if (spans[1]) spans[1].textContent = `${dsl.edges.length} edges`;
  }

  const nodeList = board.querySelector("[data-plan-nodes]");
  if (nodeList) reconcileKeyedList(nodeList, dsl.nodes, "planNodeKey", renderNode, updateNode);
  const instrumentation = board.querySelector("[data-plan-instrumentation]");
  if (instrumentation) instrumentation.outerHTML = renderPlanInstrumentation(dsl);
  const edgeSection = board.querySelector("[data-plan-edge-section]");
  edgeSection?.classList.toggle("hidden", dsl.edges.length === 0);
  const edgeList = board.querySelector("[data-plan-edges]");
  if (edgeList) reconcileKeyedList(edgeList, dsl.edges, "planEdgeKey", renderEdge, updateEdge);
}

function reconcileKeyedList(list, items, datasetKey, renderItem, updateItem) {
  const existing = new Map(Array.from(list.children)
    .filter(element => element.dataset?.[datasetKey])
    .map(element => [element.dataset[datasetKey], element]));
  const used = new Set();

  items.forEach(item => {
    let element = existing.get(item.key);
    if (!element) element = elementFromHTML(list.ownerDocument, renderItem(item));
    if (!element) return;
    updateItem(element, item);
    list.append(element);
    used.add(item.key);
  });

  existing.forEach((element, key) => {
    if (!used.has(key)) element.remove();
  });

  const empty = list.querySelector(":scope > .plan-dsl-empty");
  if (items.length) empty?.remove();
  else if (!empty) list.insertAdjacentHTML("beforeend", '<div class="plan-dsl-empty">No plan nodes</div>');
}

function renderNode(node) {
  const status = statusToken(node.status);
  const output = outputSummary(node.output);
  const branches = renderOutgoing(node);
  return `<article class="plan-dsl-node is-${status}" data-plan-node-key="${escapeHTML(node.key)}" data-plan-node-open="${escapeHTML(node.key)}" data-plan-status="${status}" style="--plan-indent:${node.depth * 9}px" tabindex="0" role="button" aria-label="查看节点 ${escapeHTML(node.label)} 的详情">
    <div class="plan-node-connector" aria-hidden="true"><i></i><span class="plan-dot">${escapeHTML(statusSymbol(node.status))}</span></div>
    <div class="plan-node-card">
      <header class="plan-node-head">
        <strong data-plan-node-field="label" title="${escapeHTML(node.label)}">${escapeHTML(node.label)}</strong>
        <span class="plan-kind" data-plan-node-field="kind">${escapeHTML(node.kind)}</span>
        <span class="plan-branch${node.outgoing.length > 1 ? "" : " hidden"}" data-plan-node-field="branch">fork ×${node.outgoing.length}</span>
        <span class="plan-dur${node.elapsed ? "" : " hidden"}" data-plan-node-field="elapsed">${escapeHTML(node.elapsed)}</span>
        <span data-plan-node-detail-slot>${renderDetailButton(node)}</span>
      </header>
      <div class="plan-node-deps${node.incoming.length ? "" : " hidden"}" data-plan-node-field="deps">${renderDependencies(node)}</div>
      ${branches ? `<div class="plan-node-branches" data-plan-node-field="branches">${branches}</div>` : ""}
      <div class="plan-node-output${output ? "" : " hidden"}" data-plan-node-field="output" title="${escapeHTML(node.output)}">${escapeHTML(output)}</div>
    </div>
  </article>`;
}

// renderOutgoing 渲染节点下游分支（Dify 树语义核心）：每个 outgoing 边一行
// 箭头 + 目标节点名 + 条件标签；并行分支（同层多出边）标 ⚡，条件边显示条件。
function renderOutgoing(node) {
  if (!node.outgoing?.length) return "";
  return node.outgoing.map(edge => {
    const targetLabel = edge.targetLabel || edge.to || "?";
    const parallel = node.outgoing.length > 1;
    const condition = edge.condition || edge.label;
    return `<div class="plan-branch-row is-${statusToken(edge.status)}" data-plan-edge-key="${escapeHTML(edge.key)}" title="${escapeHTML(condition || `流向 ${targetLabel}`)}">
      <span class="branch-arrow" aria-hidden="true">${parallel ? "⚡→" : "→"}</span>
      <span class="branch-target" data-branch-target="${escapeHTML(edge.to || "")}">${escapeHTML(targetLabel)}</span>
      ${condition ? `<span class="branch-condition">${escapeHTML(condition)}</span>` : ""}
    </div>`;
  }).join("");
}

function updateNode(element, node) {
  const status = statusToken(node.status);
  element.className = `plan-dsl-node is-${status}`;
  element.dataset.planStatus = status;
  element.dataset.planNodeOpen = node.key;
  element.setAttribute("aria-label", `查看节点 ${node.label} 的详情`);
  element.style.setProperty("--plan-indent", `${node.depth * 9}px`);
  const dot = element.querySelector(".plan-dot");
  if (dot) dot.textContent = statusSymbol(node.status);
  setNodeText(element, "label", node.label);
  const label = element.querySelector('[data-plan-node-field="label"]');
  if (label) label.title = node.label;
  setNodeText(element, "kind", node.kind);
  setNodeText(element, "branch", `fork ×${node.outgoing.length}`);
  element.querySelector('[data-plan-node-field="branch"]')?.classList.toggle("hidden", node.outgoing.length <= 1);
  setNodeText(element, "elapsed", node.elapsed);
  element.querySelector('[data-plan-node-field="elapsed"]')?.classList.toggle("hidden", !node.elapsed);
  const detailSlot = element.querySelector("[data-plan-node-detail-slot]");
  if (detailSlot) detailSlot.innerHTML = renderDetailButton(node);

  const deps = element.querySelector('[data-plan-node-field="deps"]');
  if (deps) {
    const markup = renderDependencies(node);
    if (deps.innerHTML !== markup) deps.innerHTML = markup;
    deps.classList.toggle("hidden", node.incoming.length === 0);
  }
  const output = element.querySelector('[data-plan-node-field="output"]');
  if (output) {
    const summary = outputSummary(node.output);
    output.textContent = summary;
    output.title = node.output;
    output.classList.toggle("hidden", !summary);
  }
  const branches = element.querySelector('[data-plan-node-field="branches"]');
  if (branches) {
    const markup = renderOutgoing(node);
    if (branches.innerHTML !== markup) branches.innerHTML = markup;
    branches.classList.toggle("hidden", !markup);
  }
}

function renderDependencies(node) {
  return node.incoming.map(edge => `<span class="plan-dependency is-${statusToken(edge.status)}" title="${escapeHTML(edge.condition || edge.label)}">${escapeHTML(edge.from || "?")} →</span>`).join("");
}

function renderEdge(edge) {
  const status = statusToken(edge.status);
  const detail = edge.label || edge.condition;
  return `<div class="plan-edge is-${status}${edge.dangling ? " is-dangling" : ""}" data-plan-edge-key="${escapeHTML(edge.key)}" data-plan-status="${status}">
    <span data-plan-edge-field="from">${escapeHTML(edge.from || "?")}</span><i aria-hidden="true">→</i><span data-plan-edge-field="to">${escapeHTML(edge.to || "?")}</span>
    <small class="${detail ? "" : "hidden"}" data-plan-edge-field="detail">${escapeHTML(detail)}</small>
  </div>`;
}

function updateEdge(element, edge) {
  const status = statusToken(edge.status);
  element.className = `plan-edge is-${status}${edge.dangling ? " is-dangling" : ""}`;
  element.dataset.planStatus = status;
  setEdgeText(element, "from", edge.from || "?");
  setEdgeText(element, "to", edge.to || "?");
  const detail = edge.label || edge.condition;
  setEdgeText(element, "detail", detail);
  element.querySelector('[data-plan-edge-field="detail"]')?.classList.toggle("hidden", !detail);
}

function edgeDisplayStatus(sourceStatus, targetStatus) {
  if (FAILURE_STATUSES.has(sourceStatus) || FAILURE_STATUSES.has(targetStatus)) return "failed";
  if (DONE_STATUSES.has(targetStatus)) return "completed";
  if (ACTIVE_STATUSES.has(targetStatus) || ACTIVE_STATUSES.has(sourceStatus)) return "active";
  return "pending";
}

function normalizeStatus(value, allowed) {
  const status = textValue(value).toLowerCase();
  return allowed.has(status) ? status : "unknown";
}

function statusToken(status) {
  return /^[a-z][a-z0-9_-]*$/.test(status || "") ? status : "unknown";
}

function statusLabel(status) {
  return ({ pending: "PENDING", queued: "QUEUED", running: "RUNNING", worktree_creating: "WORKTREE", rebasing: "REBASING", merging: "MERGING", completed: "DONE", failed: "FAILED", aborted: "ABORTED", skipped: "SKIPPED", canceled: "CANCELED", panicked: "PANICKED", active: "ACTIVE", success: "SUCCESS", error: "ERROR" })[status] || "UNKNOWN";
}

function modeLabel(mode) {
  return mode === "plan" ? "PLAN RUN" : "TASKLIST";
}

function modeTitle(mode) {
  return mode === "plan"
    ? "plan_run 执行中：节点打勾来自执行事件投影"
    : "tasklist 门禁：主代理串行执行，节点打勾来自 task_check_node";
}

function setMode(root, mode) {
  const element = root.querySelector('[data-plan-field="mode"]');
  if (!element) return;
  element.textContent = modeLabel(mode);
  element.title = modeTitle(mode);
  element.className = `plan-mode is-${statusToken(mode)}`;
}

function statusSymbol(status) {
  return ({ pending: "·", queued: "○", running: "●", worktree_creating: "◇", rebasing: "↻", merging: "⋈", completed: "✓", skipped: "↷", failed: "!", aborted: "×", canceled: "×", panicked: "!", success: "✓", error: "!" })[status] || "?";
}

function renderDetailButton(node) {
  const eventCount = (node.events?.length || 0) + (node.toolEvents?.length || 0);
  const title = eventCount > 0 ? `${eventCount} 个活动；点击查看详情` : "查看节点详情";
  return `<span class="plan-detail-btn${eventCount ? " has-events" : ""}" title="${escapeHTML(title)}" aria-hidden="true">详情</span>`;
}

function renderPlanInstrumentation(dsl) {
  const rows = instrumentationRows(dsl.nodes, dsl.mode);
  const source = dsl.mode === "tasklist" ? "task_check_node 与节点完成事件" : "子代理生命周期与工具事件";
  return `<section class="plan-instrumentation" data-plan-instrumentation>
    <header class="instrumentation-head">
      <strong>功能打点</strong><span class="instrumentation-count">${rows.length}</span>
      <small>${escapeHTML(source)}</small>
    </header>
    ${renderInstrumentationTable(rows, "暂无打点；任务清单完成节点或子代理开始调用工具后会显示在这里。")}
  </section>`;
}

function nodeInstrumentationRows(node, mode) {
  return instrumentationRows([node], mode);
}

function instrumentationRows(nodes, mode) {
  const rows = [];
  let order = 0;
  for (const node of nodes || []) {
    const source = node.label || node.id || "node";
    for (const event of node.events || []) {
      const status = event.status || "unknown";
      rows.push({
        source,
        operation: mode === "tasklist" && status === "completed" ? "task_check_node" : "node.lifecycle",
        status,
        at: event.at,
        detail: event.output || "",
        order: order += 1
      });
    }
    for (const event of node.toolEvents || []) {
      rows.push({
        source,
        operation: event.name || "tool",
        status: event.status || "unknown",
        at: event.startedAt,
        detail: event.error || event.result || event.arguments || "",
        order: order += 1
      });
    }
  }
  return rows.sort((left, right) => {
    const delta = Date.parse(right.at || "") - Date.parse(left.at || "");
    return Number.isFinite(delta) && delta !== 0 ? delta : right.order - left.order;
  });
}

function renderInstrumentationTable(rows, emptyText) {
  if (!rows.length) return `<div class="instrumentation-empty">${escapeHTML(emptyText)}</div>`;
  const visible = rows.slice(0, 24);
  return `<div class="instrumentation-table" role="table" aria-label="功能打点表">
    <div class="instrumentation-row is-head" role="row"><span>节点</span><span>函数 / 阶段</span><span>状态</span><span>时间</span><span>证据</span></div>
    ${visible.map(row => `<div class="instrumentation-row is-${statusToken(row.status)}" role="row">
      <span title="${escapeHTML(row.source)}">${escapeHTML(row.source)}</span>
      <strong>${escapeHTML(row.operation)}</strong>
      <span>${escapeHTML(statusLabel(row.status))}</span>
      <time datetime="${escapeHTML(row.at || "")}">${escapeHTML(formatEventTime(row.at))}</time>
      <small title="${escapeHTML(row.detail)}">${escapeHTML(outputSummary(row.detail) || "—")}</small>
    </div>`).join("")}
  </div>`;
}

function outputSummary(value) {
  const normalized = String(value || "").replace(/\s+/g, " ").trim();
  return normalized.length > 180 ? `${normalized.slice(0, 177)}…` : normalized;
}

// normalizeNodeEvents 归一化节点事件时间线（详情页数据源）：
// 保序、字段清洗；时间戳保留原始 ISO 串，状态经 NODE_STATUSES 校验。
function normalizeNodeEvents(rawEvents) {
  if (!Array.isArray(rawEvents)) return [];
  const events = [];
  for (const raw of rawEvents) {
    if (!isRecord(raw)) continue;
    const status = normalizeStatus(raw.status, NODE_STATUSES);
    if (status === "unknown") continue;
    events.push({
      status,
      at: textValue(raw.at),
      output: textValue(raw.output)
    });
  }
  return events;
}

function normalizeToolEvents(rawEvents) {
  if (!Array.isArray(rawEvents)) return [];
  return rawEvents.filter(isRecord).map(raw => ({
    id: textValue(raw.id),
    nodeID: textValue(raw.node_id),
    name: textValue(raw.name, "tool"),
    arguments: textValue(raw.arguments),
    result: textValue(raw.result),
    error: textValue(raw.error),
    status: textValue(raw.status, "unknown").toLowerCase(),
    startedAt: textValue(raw.started_at),
    duration: finiteNumber(raw.duration) ?? 0
  }));
}

function setText(root, field, value) {
  const element = root.querySelector(`[data-plan-field="${field}"]`);
  if (element) element.textContent = value;
}

function setNodeText(root, field, value) {
  const element = root.querySelector(`[data-plan-node-field="${field}"]`);
  if (element) element.textContent = value;
}

function setEdgeText(root, field, value) {
  const element = root.querySelector(`[data-plan-edge-field="${field}"]`);
  if (element) element.textContent = value;
}

function elementFromHTML(ownerDocument, markup) {
  const template = ownerDocument.createElement("template");
  template.innerHTML = markup.trim();
  return template.content.firstElementChild;
}

function uniqueKey(base, counts) {
  const count = counts.get(base) || 0;
  counts.set(base, count + 1);
  return count === 0 ? base : `${base}#${count + 1}`;
}

function finiteNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function clamp(value, minimum, maximum) {
  return Math.min(Math.max(value, minimum), maximum);
}

function isRecord(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function textValue(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function structuredText(value) {
  if (value === undefined || value === null || value === "") return "";
  if (typeof value === "string") return value;
  try { return JSON.stringify(value); }
  catch { return String(value); }
}

export function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

// renderNodeDetail 渲染节点详情页（弹窗内容）：身份信息 + 会话记录
// （子代理对话流，detail.conversation）+ 事件时间线 + 最终输出。
// 会话记录由 app.js 经 invoke("SubagentSessionDetail") 异步拉取后
// 调用 setNodeDetailConversation 注入（运行中 2s 轮询刷新）。
export function renderNodeDetail(node) {
  const status = statusToken(node.status);
  const timeline = (node.events || []).map(event => {
    const time = formatEventTime(event.at);
    const output = outputSummary(event.output);
    return `<li class="node-event is-${statusToken(event.status)}" data-node-event-status="${statusToken(event.status)}">
      <span class="node-event-dot" aria-hidden="true">${escapeHTML(statusSymbol(event.status))}</span>
      <time class="node-event-time" datetime="${escapeHTML(event.at)}">${escapeHTML(time)}</time>
      <span class="node-event-status">${escapeHTML(statusLabel(event.status))}</span>
      ${output ? `<span class="node-event-output" title="${escapeHTML(event.output)}">${escapeHTML(output)}</span>` : ""}
    </li>`;
  }).join("");
  const toolEvents = renderToolEvents(node.toolEvents || []);
  const instrumentation = renderInstrumentationTable(
    nodeInstrumentationRows(node, node.mode),
    "暂无节点打点；子代理运行或任务清单勾选后会显示在这里。"
  );
  return `<div class="node-detail" data-node-detail>
    <div class="node-detail-head">
      <h2 class="node-detail-title">${escapeHTML(node.label || node.id || "节点详情")}</h2>
      <span class="plan-status is-${status}">${escapeHTML(statusLabel(node.status))}</span>
      <span class="plan-mode is-${statusToken(node.mode)}">${escapeHTML(modeLabel(node.mode))}</span>
    </div>
    <dl class="node-detail-meta">
      <div><dt>ID</dt><dd>${escapeHTML(node.id || "—")}</dd></div>
      <div><dt>Kind</dt><dd>${escapeHTML(node.kind || "auto")}</dd></div>
      <div><dt>耗时</dt><dd>${escapeHTML(node.elapsed || "—")}</dd></div>
      <div><dt>事件</dt><dd>${node.events ? node.events.length : 0}</dd></div>
      <div><dt>工具</dt><dd data-node-tool-count>${node.toolEvents ? node.toolEvents.length : 0}</dd></div>
    </dl>
    <div class="node-detail-tabs">
      <button class="node-tab is-active" data-node-tab="conversation" type="button">会话记录</button>
      <button class="node-tab" data-node-tab="context" type="button">上下文</button>
      <button class="node-tab" data-node-tab="instrumentation" type="button">功能打点</button>
      <button class="node-tab" data-node-tab="timeline" type="button">事件时间线</button>
      <button class="node-tab" data-node-tab="tools" type="button">工具活动</button>
      ${node.output ? `<button class="node-tab" data-node-tab="output" type="button">最终输出</button>` : ""}
    </div>
    <div class="node-tab-panel is-active" data-node-panel="conversation">
      <div class="node-conversation" data-node-conversation>
        <div class="node-timeline-empty">加载会话记录…</div>
      </div>
    </div>
    <div class="node-tab-panel" data-node-panel="context">
      <div class="node-context" data-node-context>
        <div class="node-timeline-empty">加载上下文快照…</div>
      </div>
    </div>
    <div class="node-tab-panel" data-node-panel="instrumentation" data-node-instrumentation>
      ${instrumentation}
    </div>
    <div class="node-tab-panel" data-node-panel="timeline">
      ${timeline ? `<ol class="node-timeline">${timeline}</ol>` : '<div class="node-timeline-empty">暂无事件；任务清单模式下由 task_check_node 打点驱动</div>'}
    </div>
    <div class="node-tab-panel" data-node-panel="tools" data-node-tool-events>
      ${toolEvents || '<div class="node-timeline-empty">暂无子代理工具活动</div>'}
    </div>
    ${node.output ? `<div class="node-tab-panel" data-node-panel="output"><div class="node-detail-output"><pre>${escapeHTML(node.output)}</pre></div></div>` : ""}
  </div>`;
}

// setNodeDetailConversation 渲染子代理会话记录 + 结构化上下文快照
// （详情弹窗会话记录 / 上下文标签；数据来自 invoke SubagentSessionDetail）。
export function setNodeDetailConversation(detail) {
  const container = document.querySelector("[data-node-detail] [data-node-conversation]");
  const toolContainer = document.querySelector("[data-node-detail] [data-node-tool-events]");
  const contextContainer = document.querySelector("[data-node-detail] [data-node-context]");
  if (!container && !toolContainer && !contextContainer) return;
  const messages = (detail?.conversation || []);
  if (container && messages.length === 0) {
    container.innerHTML = '<div class="node-timeline-empty">该节点无会话记录（确定性节点或会话未落盘）</div>';
  } else if (container) {
    container.innerHTML = messages.map(msg => {
      const role = msg.role === "user" ? "user" : (msg.role === "assistant" ? "assistant" : "tool");
      const body = msg.tool && msg.tool.name
        ? `<span class="node-msg-tool">🛠 ${escapeHTML(msg.tool.name)}</span>${msg.content ? `<div>${escapeHTML(msg.content)}</div>` : ""}`
        : `<div>${escapeHTML(msg.content || "")}</div>`;
      return `<div class="node-msg is-${role}"><span class="node-msg-role">${role}</span>${body}</div>`;
    }).join("");
  }
  if (container && detail?.running) {
    container.insertAdjacentHTML("beforeend", '<div class="node-timeline-empty">执行中，每 2 秒刷新…</div>');
  }
  if (contextContainer) {
    contextContainer.innerHTML = renderNodeContext(detail?.context);
  }
  if (toolContainer) {
    const toolEvents = normalizeToolEvents(detail?.tool_events);
    toolContainer.innerHTML = renderToolEvents(toolEvents) || '<div class="node-timeline-empty">暂无子代理工具活动</div>';
    const count = document.querySelector("[data-node-detail] [data-node-tool-count]");
    if (count) count.textContent = String(toolEvents.length);
  }
}

// renderNodeContext 渲染子代理结构化上下文快照（详情弹窗"上下文"标签）。
// 数据来自后端 SubagentSessionDetail.context（权威导出：Goal/Progress/
// Findings/Decisions/Constraints/PendingWork/MessageCount/TokenEstimate）；
// 全部文本 escape，缺字段显示占位。
export function renderNodeContext(context) {
  if (!context || typeof context !== "object") {
    return '<div class="node-timeline-empty">该节点无上下文快照（非 agent 节点或会话尚未导出）</div>';
  }
  const meta = [
    ["消息数", context.message_count != null ? String(context.message_count) : "—"],
    ["token 估算", context.token_estimate ? formatNumber(context.token_estimate) : "—"]
  ].filter(pair => pair[1] !== "—").map(([label, value]) => `<span><strong>${escapeHTML(label)}</strong>${escapeHTML(value)}</span>`).join("");
  const sections = [
    ["目标 Goal", context.goal],
    ["进度 Progress", context.progress],
    ["重要发现 Findings", context.findings],
    ["关键决策 Decisions", Array.isArray(context.decisions) ? context.decisions.map(decision => `${decision.what || ""}${decision.why ? ` — ${decision.why}` : ""}`) : null],
    ["约束 Constraints", context.constraints],
    ["待办 Pending", context.pending_work]
  ].filter(([, value]) => value && (Array.isArray(value) ? value.length : String(value).trim()));
  return `<div class="node-context-meta">${meta}</div>${sections.map(([title, value]) => `
    <section class="node-context-section">
      <h3>${escapeHTML(title)}</h3>
      ${Array.isArray(value)
        ? `<ul class="node-context-list">${value.map(entry => `<li title="${escapeHTML(entry)}">${escapeHTML(entry)}</li>`).join("")}</ul>`
        : `<p class="node-context-text">${escapeHTML(value)}</p>`}
    </section>`).join("") || '<div class="node-timeline-empty">上下文快照为空</div>'}
  </div>`;
}

function formatNumber(value) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

function renderToolEvents(events) {
  if (!events.length) return "";
  return `<ol class="node-tool-list">${events.map(event => {
    const evidence = event.error || event.result || event.arguments;
    const evidenceKind = event.error ? "ERROR" : (event.result ? "RESULT" : "INPUT");
    return `<li class="node-tool-event is-${statusToken(event.status)}" data-node-tool-id="${escapeHTML(event.id)}">
      <div class="node-tool-head">
        <span class="node-event-dot" aria-hidden="true">${escapeHTML(statusSymbol(event.status))}</span>
        <strong>${escapeHTML(event.name)}</strong>
        <span>${escapeHTML(statusLabel(event.status))}</span>
        <time datetime="${escapeHTML(event.startedAt)}">${escapeHTML(formatEventTime(event.startedAt))}</time>
        <small>${escapeHTML(formatDuration(event.duration))}</small>
      </div>
      ${evidence ? `<div class="node-tool-evidence"><span>${evidenceKind}</span><pre>${escapeHTML(evidence)}</pre></div>` : ""}
    </li>`;
  }).join("")}</ol>`;
}

function formatDuration(value) {
  const nanoseconds = Number(value || 0);
  if (!Number.isFinite(nanoseconds) || nanoseconds <= 0) return "";
  const milliseconds = nanoseconds / 1e6;
  if (milliseconds < 1000) return `${Math.round(milliseconds)}ms`;
  return `${(milliseconds / 1000).toFixed(2)}s`;
}

// bindNodeDetailTabs 切换详情弹窗标签（会话记录 / 事件时间线 / 输出）。
export function bindNodeDetailTabs(root) {
  root.querySelectorAll("[data-node-tab]").forEach(button => {
    button.addEventListener("click", () => {
      root.querySelectorAll("[data-node-tab]").forEach(b => b.classList.toggle("is-active", b === button));
      root.querySelectorAll("[data-node-panel]").forEach(panel =>
        panel.classList.toggle("is-active", panel.dataset.nodePanel === button.dataset.nodeTab));
    });
  });
}

// formatEventTime 把 ISO 时间戳格式化为本地 HH:MM:SS（非法/空 → "—"）。
function formatEventTime(iso) {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString([], { hour12: false });
}
