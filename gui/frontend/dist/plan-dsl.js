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

  // 树状布局（W2）：DAG 归一化为树——拓扑分层 + 主路径树 + 旁路标记。
  layoutPlanTree(nodes, edges);

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

// layoutPlanTree 把 Plan DAG 归一化为树状布局（层级缩进 + 连线渲染）：
//
//  1. 分层：从无入边根节点做 Kahn 拓扑遍历，level = 到根的最长路径层数；
//  2. 主路径：多入边节点（菱形 join）的树父节点取"入边源中层最深者"（并列
//     取边序第一个），其余入边记为旁路（sideRefs → 渲染"旁路"chip）——节点
//     只渲染一次（主路径树 + 旁路引用），不死循环；
//  3. 深度沿主路径递推（visited 防环；Kahn 未访问的环内节点按根处理）；
//  4. 引导字符：父级 "│  " + 自身 "├─ "/"└─ "（层级连线文本）。
//
// 无 edges 时保留 children 嵌套深度（旧契约：嵌套 children 即层级）。
// 副作用：改写每个 node 的 depth/treeParentID/sideRefs/treeGuide，并按
// (depth, 原序) 稳定排序 nodes（树序渲染；reconcile key 不受影响）。
function layoutPlanTree(nodes, edges) {
  const byID = new Map();
  nodes.forEach(node => byID.set(node.id, node));
  // 没有"可用边"（全悬垂）时回退 children 嵌套深度（旧契约）。
  if (!edges.some(edge => byID.has(edge.from) && byID.has(edge.to))) return;
  const indegree = new Map(nodes.map(node => [node.id, 0]));
  const out = new Map(nodes.map(node => [node.id, []]));
  edges.forEach(edge => {
    if (!byID.has(edge.from) || !byID.has(edge.to)) return;
    indegree.set(edge.to, indegree.get(edge.to) + 1);
    out.get(edge.from).push(edge);
  });

  // 1) 拓扑分层（Kahn；level = 到根的最长路径）。
  const level = new Map(nodes.map(node => [node.id, 0]));
  const queue = nodes.filter(node => !indegree.get(node.id)).map(node => node.id);
  const visited = new Set();
  while (queue.length) {
    const id = queue.shift();
    if (visited.has(id)) continue;
    visited.add(id);
    out.get(id).forEach(edge => {
      const remaining = indegree.get(edge.to) - 1;
      indegree.set(edge.to, remaining);
      level.set(edge.to, Math.max(level.get(edge.to), level.get(id) + 1));
      if (remaining === 0) queue.push(edge.to);
    });
  }

  // 2) 主路径父节点 + 旁路入边。
  const treeParentOf = new Map(); // id → 树父节点 id
  const sideRefsOf = new Map();   // id → [旁路边]
  nodes.forEach(node => {
    const incoming = edges.filter(edge => edge.to === node.id && byID.has(edge.from));
    if (!incoming.length) return;
    let best = incoming[0];
    let bestLevel = -1;
    const rest = [];
    incoming.forEach(edge => {
      const fromLevel = level.get(edge.from) ?? -1;
      if (fromLevel > bestLevel) {
        if (bestLevel >= 0) rest.push(best);
        best = edge;
        bestLevel = fromLevel;
      } else {
        rest.push(edge);
      }
    });
    treeParentOf.set(node.id, best.from);
    sideRefsOf.set(node.id, rest);
  });

  // 3) 深度沿主路径递推（visited 防环 → 环内节点按根处理）。
  const depth = new Map();
  const computeDepth = (id, path) => {
    if (depth.has(id)) return depth.get(id);
    if (path.has(id)) return 0;
    path.add(id);
    const parent = treeParentOf.get(id);
    const value = parent ? computeDepth(parent, path) + 1 : 0;
    depth.set(id, value);
    path.delete(id);
    return value;
  };
  nodes.forEach(node => computeDepth(node.id, new Set()));

  // 4) 引导字符（层级连线）：父级 "│  "（父有后继兄弟）/ "   "，自身
  //    "├─ "（非最后子节点）/ "└─ "（最后子节点）。
  const childrenOf = new Map();
  treeParentOf.forEach((parent, id) => {
    if (!childrenOf.has(parent)) childrenOf.set(parent, []);
    childrenOf.get(parent).push(id);
  });
  const guide = new Map();
  nodes.forEach(node => {
    if (!visited.has(node.id)) return; // 环内/不可达：扁平处理（无引导字符）
    const stack = [];
    let id = node.id;
    let parent = treeParentOf.get(id);
    const seen = new Set(); // 防环：主路径链进入环时截断
    while (parent && !seen.has(id)) {
      seen.add(id);
      const siblings = childrenOf.get(parent) || [];
      const isLast = siblings[siblings.length - 1] === id;
      const hasNext = siblings.some(sibling => sibling !== id);
      stack.push({ isLast, hasNext });
      id = parent;
      parent = treeParentOf.get(id);
    }
    const chars = [];
    for (let index = stack.length - 1; index >= 0; index--) {
      const step = stack[index];
      if (index === 0) chars.push(step.isLast ? "└─ " : "├─ ");
      else chars.push(step.hasNext ? "│  " : "   ");
    }
    guide.set(node.id, chars.join(""));
  });

  nodes.forEach(node => {
    if (!visited.has(node.id)) {
      // 环内/不可达节点：按根扁平处理（不参与主路径树，避免死循环）。
      node.depth = 0;
      node.treeParentID = "";
      node.sideRefs = [];
      node.treeGuide = "";
      return;
    }
    node.depth = Math.min(depth.get(node.id) ?? 0, 12);
    node.treeParentID = treeParentOf.get(node.id) || "";
    node.sideRefs = (sideRefsOf.get(node.id) || []).map(edge => ({
      key: edge.key,
      from: edge.from,
      label: edge.label || edge.condition || ""
    }));
    node.treeGuide = guide.get(node.id) || "";
  });
  // 树序渲染：按 (depth, 原序) 稳定排序（reconcile key 不变，仅移动 DOM）。
  const order = new Map(nodes.map((node, index) => [node.key, index]));
  nodes.sort((left, right) => (left.depth - right.depth) || (order.get(left.key) - order.get(right.key)));
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
  const sideRefs = renderSideRefs(node);
  const guide = node.treeGuide ? `<span class="plan-tree-guide" data-plan-tree-guide>${escapeHTML(node.treeGuide)}</span>` : "";
  return `<article class="plan-dsl-node is-${status}" data-plan-node-key="${escapeHTML(node.key)}" data-plan-node-open="${escapeHTML(node.key)}" data-plan-status="${status}" data-plan-tree-depth="${node.depth}" style="--plan-indent:${node.depth * 9}px" tabindex="0" role="button" aria-label="查看节点 ${escapeHTML(node.label)} 的详情">
    <div class="plan-node-connector" aria-hidden="true">${guide}<i></i><span class="plan-dot">${escapeHTML(statusSymbol(node.status))}</span></div>
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
      <div class="plan-node-side${sideRefs ? "" : " hidden"}" data-plan-node-field="side">${sideRefs}</div>
      <div class="plan-node-output${output ? "" : " hidden"}" data-plan-node-field="output" title="${escapeHTML(node.output)}">${escapeHTML(output)}</div>
    </div>
  </article>`;
}

// renderSideRefs 渲染多入边节点的旁路标记（树主路径之外的其他入边来源）。
// DAG 菱形 join 只出现在主路径树中一次，旁路入边以 chip 引用呈现，不复制节点。
// label 来自边条件/标签（未信任输入），必须 escape。
function renderSideRefs(node) {
  if (!node.sideRefs?.length) return "";
  return node.sideRefs.map(ref => {
    const detail = ref.label ? ` · ${escapeHTML(ref.label)}` : "";
    return `<span class="plan-side-ref" data-plan-side-ref="${escapeHTML(ref.from)}" title="旁路来源 ${escapeHTML(ref.from)}${detail}">旁路 ${escapeHTML(ref.from)}${detail}</span>`;
  }).join("");
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
  element.dataset.planTreeDepth = String(node.depth);
  element.setAttribute("aria-label", `查看节点 ${node.label} 的详情`);
  element.style.setProperty("--plan-indent", `${node.depth * 9}px`);
  const connector = element.querySelector(".plan-node-connector");
  const guide = connector?.querySelector("[data-plan-tree-guide]");
  if (node.treeGuide) {
    if (!guide && connector) connector.insertAdjacentHTML("afterbegin", `<span class="plan-tree-guide" data-plan-tree-guide>${escapeHTML(node.treeGuide)}</span>`);
    else if (guide) guide.textContent = node.treeGuide;
  } else {
    guide?.remove();
  }
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
  const side = element.querySelector('[data-plan-node-field="side"]');
  if (side) {
    const markup = renderSideRefs(node);
    if (side.innerHTML !== markup) side.innerHTML = markup;
    side.classList.toggle("hidden", !markup);
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
    // 工作区现场（失败/合并被拒时的恢复入口）前置在上下文快照之前。
    contextContainer.innerHTML = renderNodeWorktree(detail?.worktree) + renderNodeContext(detail?.context);
  }
  if (toolContainer) {
    const toolEvents = normalizeToolEvents(detail?.tool_events);
    toolContainer.innerHTML = renderToolEvents(toolEvents) || '<div class="node-timeline-empty">暂无子代理工具活动</div>';
    const count = document.querySelector("[data-node-detail] [data-node-tool-count]");
    if (count) count.textContent = String(toolEvents.length);
  }
}

// renderNodeWorktree 渲染节点 worktree 现场（详情弹窗"上下文"标签前置
// section）：节点失败/合并被拒时产出文件保留在独立 worktree（未进主仓库
// 但仍在磁盘），Path 是人工恢复入口；分支改动已提交时可 git merge 恢复。
// 全部文本 escape；无现场 → 空串（不渲染）。
export function renderNodeWorktree(worktree) {
  if (!worktree || !worktree.path) return "";
  const branch = worktree.branch || "";
  const recoveryHint = branch
    ? `改动已提交时可 git merge ${escapeHTML(branch)} 合并回 ${escapeHTML(worktree.main_branch || "main")}`
    : `改动未提交时先在 ${escapeHTML(worktree.path)} 内 git add -A && git commit`;
  return `<section class="node-context-section node-worktree-section">
    <h3>工作区现场 (Worktree)</h3>
    <p class="node-context-text">节点失败或合并被拒：子代理产出保留在独立 worktree，未进入主仓库但仍在磁盘，可手动恢复。</p>
    <ul class="node-context-list">
      <li><strong>路径</strong> ${escapeHTML(worktree.path)}</li>
      <li><strong>分支</strong> ${escapeHTML(branch || "—")} — ${recoveryHint}</li>
    </ul>
  </section>`;
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

// ── 子代理树（fork 内存态可视化；数据源 snapshot.runtime.subagent_tree）──

// renderSubagentTree 渲染 fork 子代理树：层级缩进 + 连线字符 + 状态着色 +
// goal/会话摘要；树节点行整行可点开详情弹窗（data-plan-node-open 复用
// 既有节点详情入口；会话记录/上下文经 SubagentSessionDetail 拉取）。
// 全部文本 escape；空树返回 ""（外层隐藏 section）。
export function renderSubagentTree(nodes) {
  if (!Array.isArray(nodes) || !nodes.length) return "";
  const rows = [];
  const walk = (items, depth, bars) => {
    items.forEach((item, index) => {
      if (!isRecord(item)) return;
      const isLast = index === items.length - 1;
      const children = Array.isArray(item.children) ? item.children : [];
      const prefix = depth === 0 ? "" : `${bars}${isLast ? "└─ " : "├─ "}`;
      const nextBars = depth === 0 ? "" : `${bars}${isLast ? "   " : "│  "}`;
      rows.push(renderSubagentTreeRow(item, prefix));
      walk(children, depth + 1, nextBars);
    });
  };
  walk(nodes, 0, "");
  return `<section class="subagent-tree" data-subagent-tree>
    <header class="subagent-tree-head">
      <strong>子代理树</strong>
      <span class="subagent-tree-count">${rows.length} 节点</span>
      <small>fork 子代理 · 内存态实时投影</small>
    </header>
    <div class="subagent-tree-rows" data-subagent-tree-rows>${rows.join("")}</div>
  </section>`;
}

// renderSubagentTreeRow 渲染一个树节点行（id + 状态 + goal/摘要/错误 +
// 紧凑上下文）。
function renderSubagentTreeRow(item, guide) {
  const status = normalizeSubagentStatus(item.status);
  const goal = outputSummary(item.goal);
  const summary = outputSummary(item.summary);
  const error = outputSummary(item.error);
  const label = item.id === "main" ? "主代理" : item.id;
  return `<div class="subagent-tree-row is-${status}" data-subagent-row data-plan-node-open="${escapeHTML(item.id)}" data-subagent-status="${status}">
    <span class="subagent-tree-guide" aria-hidden="true">${escapeHTML(guide)}</span>
    <span class="plan-dot" aria-hidden="true">${escapeHTML(statusSymbol(subagentStatusToken(status)))}</span>
    <div class="subagent-tree-card">
      <header class="subagent-tree-headline">
        <strong title="${escapeHTML(item.id)}">${escapeHTML(label)}</strong>
        <span class="plan-kind">${escapeHTML(statusLabel(subagentStatusToken(status)))}</span>
        ${item.session_id ? `<span class="subagent-session" title="${escapeHTML(item.session_id)}">${escapeHTML(shortSessionID(item.session_id))}</span>` : ""}
        <button class="subagent-tree-detail" type="button" data-plan-node-open="${escapeHTML(item.id)}" title="查看会话记录 / 运行时上下文 / 工具活动">详情</button>
      </header>
      ${goal ? `<div class="subagent-tree-goal" title="${escapeHTML(item.goal)}">${escapeHTML(goal)}</div>` : ""}
      ${renderSubagentTreeContext(item.context)}
      ${summary ? `<div class="subagent-tree-summary" title="${escapeHTML(item.summary)}">${escapeHTML(summary)}</div>` : ""}
      ${error ? `<div class="subagent-tree-error" title="${escapeHTML(item.error)}">${escapeHTML(error)}</div>` : ""}
    </div>
  </div>`;
}

// renderSubagentTreeContext 渲染节点的紧凑上下文（ContextSnapshot 有界投影：
// 消息数/token 估算/发现；全部 escape）。
function renderSubagentTreeContext(context) {
  if (!isRecord(context)) return "";
  const meta = [
    context.message_count != null ? `${context.message_count} 消息` : "",
    context.token_estimate ? `${formatNumber(context.token_estimate)} tok` : ""
  ].filter(Boolean);
  const findings = Array.isArray(context.findings) ? context.findings.map(outputSummary).filter(Boolean) : [];
  if (!meta.length && !findings.length && !context.progress) return "";
  return `<div class="subagent-tree-context">
    ${meta.length ? `<span class="subagent-tree-meta">${escapeHTML(meta.join(" · "))}</span>` : ""}
    ${context.progress ? `<span class="subagent-tree-meta">${escapeHTML(outputSummary(context.progress))}</span>` : ""}
    ${findings.length ? `<ul class="subagent-tree-findings">${findings.map(finding => `<li title="${escapeHTML(finding)}">${escapeHTML(finding)}</li>`).join("")}</ul>` : ""}
  </div>`;
}

// subagentTreeNodeToDSL 把子代理树投影节点转换为详情弹窗可渲染的 DSL 节点
// （fork 节点不在 Plan 快照里时（计划已清除）详情弹窗仍可打开：会话记录/
// 上下文由 SubagentSessionDetail 数据面承载）。
export function subagentTreeNodeToDSL(treeNode) {
  const id = textValue(treeNode?.id);
  const status = normalizeSubagentStatus(treeNode?.status);
  return {
    type: "node",
    key: id,
    id,
    parentKey: textValue(treeNode?.parent_id),
    label: outputSummary(treeNode?.goal) || id || "子代理",
    kind: "agent",
    status: subagentStatusToken(status),
    depth: 0,
    elapsed: "",
    output: textValue(treeNode?.summary),
    events: [],
    toolEvents: [],
    incoming: [],
    outgoing: [],
    mode: "plan"
  };
}

// normalizeSubagentStatus 校验子代理树状态（running/done/failed；非法 → unknown）。
function normalizeSubagentStatus(value) {
  const status = textValue(value).toLowerCase();
  return status === "running" || status === "done" || status === "failed" ? status : "unknown";
}

// subagentStatusToken 把子代理树状态映射到既有节点状态 token（状态徽标/
// 符号复用 plan-dsl 的状态表）。
function subagentStatusToken(status) {
  return ({ running: "running", done: "completed", failed: "failed" })[status] || "unknown";
}

// shortSessionID 截断会话 ID（显示用；完整 ID 在 title 提示里）。
function shortSessionID(sessionID) {
  const value = String(sessionID || "");
  if (value.length <= 16) return value;
  return `${value.slice(0, 8)}…${value.slice(-6)}`;
}
