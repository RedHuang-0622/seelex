const NODE_STATUSES = new Set([
  "pending", "queued", "running", "completed", "failed", "aborted",
  "skipped", "canceled", "panicked"
]);

const PLAN_STATUSES = new Set(["pending", "running", "completed", "failed", "aborted"]);
const FAILURE_STATUSES = new Set(["failed", "aborted", "canceled", "panicked"]);
const DONE_STATUSES = new Set(["completed", "skipped"]);

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
        status: edgeDisplayStatus(source?.status, target?.status),
        dangling: !source || !target
      };
      edges.push(edge);
      if (source) source.outgoing.push(edge.key);
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
  const branchCount = node.outgoing.length;
  const hasDetail = node.events.length > 0 || Boolean(node.output) || node.elapsed;
  return `<article class="plan-dsl-node is-${status}" data-plan-node-key="${escapeHTML(node.key)}" data-plan-status="${status}" style="--plan-indent:${node.depth * 9}px">
    <div class="plan-node-connector" aria-hidden="true"><i></i><span class="plan-dot">${escapeHTML(statusSymbol(node.status))}</span></div>
    <div class="plan-node-card">
      <header class="plan-node-head">
        <strong data-plan-node-field="label" title="${escapeHTML(node.label)}">${escapeHTML(node.label)}</strong>
        <span class="plan-kind" data-plan-node-field="kind">${escapeHTML(node.kind)}</span>
        <span class="plan-branch${branchCount > 1 ? "" : " hidden"}" data-plan-node-field="branch">fork ×${branchCount}</span>
        <span class="plan-dur${node.elapsed ? "" : " hidden"}" data-plan-node-field="elapsed">${escapeHTML(node.elapsed)}</span>
        ${hasDetail ? `<button type="button" class="plan-detail-btn${node.events.length ? " has-events" : ""}" data-plan-node-detail="${escapeHTML(node.key)}" title="${escapeHTML(node.events.length ? `${node.events.length} 个事件；点击查看详情` : "查看节点详情")}" aria-label="节点详情">…</button>` : ""}
      </header>
      <div class="plan-node-deps${node.incoming.length ? "" : " hidden"}" data-plan-node-field="deps">${renderDependencies(node)}</div>
      <div class="plan-node-output${output ? "" : " hidden"}" data-plan-node-field="output" title="${escapeHTML(node.output)}">${escapeHTML(output)}</div>
    </div>
  </article>`;
}

function updateNode(element, node) {
  const status = statusToken(node.status);
  element.className = `plan-dsl-node is-${status}`;
  element.dataset.planStatus = status;
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
  if (targetStatus === "running" || targetStatus === "queued" || sourceStatus === "running") return "active";
  return "pending";
}

function normalizeStatus(value, allowed) {
  const status = textValue(value).toLowerCase();
  return allowed.has(status) ? status : "unknown";
}

function statusToken(status) {
  return /^[a-z][a-z0-9-]*$/.test(status || "") ? status : "unknown";
}

function statusLabel(status) {
  return ({ pending: "PENDING", queued: "QUEUED", running: "RUNNING", completed: "DONE", failed: "FAILED", aborted: "ABORTED", skipped: "SKIPPED", canceled: "CANCELED", panicked: "PANICKED", active: "ACTIVE" })[status] || "UNKNOWN";
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
  return ({ pending: "·", queued: "○", running: "●", completed: "✓", skipped: "↷", failed: "!", aborted: "×", canceled: "×", panicked: "!" })[status] || "?";
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

// renderNodeDetail 渲染节点详情页（弹窗内容）：身份信息 + 事件时间线。
// 时间线是权威 JSON（node.events），运行中节点经心跳事件刷新"最后活跃"。
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
    </dl>
    ${node.output ? `<div class="node-detail-output"><strong>最终输出</strong><pre>${escapeHTML(node.output)}</pre></div>` : ""}
    <div class="node-detail-timeline">
      <div class="node-timeline-title">时间线</div>
      ${timeline ? `<ol class="node-timeline">${timeline}</ol>` : '<div class="node-timeline-empty">暂无事件；任务清单模式下由 task_check_node 打点驱动</div>'}
    </div>
  </div>`;
}

// formatEventTime 把 ISO 时间戳格式化为本地 HH:MM:SS（非法/空 → "—"）。
function formatEventTime(iso) {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString([], { hour12: false });
}
