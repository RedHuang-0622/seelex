import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./plan-dsl.js", import.meta.url), "utf8");
const { planToDSL, renderPlanDSL, renderNodeDetail, renderNodeContext, renderSubagentTree, subagentTreeNodeToDSL, workItemToDetailNode } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

function parallelPlan(status = "queued", progress = 0) {
  return {
    name: "parallel build",
    entry_node_id: "start",
    status: "running",
    progress,
    elapsed: "1.2s",
    nodes: [
      { id: "start", label: "Start", kind: "auto", status: "completed" },
      { id: "left", label: "Left branch", kind: "auto", status, output: "left output" },
      { id: "right", label: "Right branch", kind: "manual", status: "pending" },
      { id: "join", label: "Join", status: "pending" }
    ],
    edges: [
      { from: "start", to: "left" },
      { from: "start", to: "right", label: "fork" },
      { from: "left", to: "join" },
      { from: "right", to: "join", condition: { when: "approved" } }
    ]
  };
}

test("converts a parallel plan JSON graph into a stable DSL", () => {
  const queued = planToDSL(parallelPlan("queued", 0.25));
  const running = planToDSL(parallelPlan("running", 0.5));

  assert.equal(queued.schema, "seelex.plan/v1");
  assert.equal(queued.key, "start");
  assert.equal(queued.progressPercent, 25);
  assert.deepEqual(queued.nodes.map(node => node.key), running.nodes.map(node => node.key));
  assert.deepEqual(queued.edges.map(edge => edge.key), running.edges.map(edge => edge.key));
  assert.equal(running.nodes.find(node => node.id === "left").status, "running");
  assert.equal(running.nodes.find(node => node.id === "start").outgoing.length, 2);
  assert.equal(running.edges.find(edge => edge.to === "left").status, "active");
  assert.equal(running.edges.find(edge => edge.to === "join" && edge.from === "right").condition, '{"when":"approved"}');
});

test("flattens nested children while retaining hierarchy and unique keys", () => {
  const dsl = planToDSL({
    name: "nested",
    status: "pending",
    nodes: [{
      id: "fork", label: "Fork", status: "pending", children: [
        { id: "child", status: "queued" },
        { id: "child", status: "pending", children: [{ label: "leaf", status: "pending" }] }
      ]
    }]
  });

  assert.deepEqual(dsl.nodes.map(node => node.key), ["fork", "child", "child#2", "child#2/1"]);
  assert.deepEqual(dsl.nodes.map(node => node.depth), [0, 1, 1, 2]);
  assert.equal(dsl.nodes[3].parentKey, "child#2");
});

test("normalizes malformed fields without hiding dangling edges", () => {
  const dsl = planToDSL({
    name: 123,
    status: "future-state",
    progress: 9,
    nodes: [null, { label: "No ID", status: "future-node", depth: 99 }],
    edges: [{ from: "missing", to: "also-missing" }, null, {}]
  });

  assert.equal(dsl.name, "123");
  assert.equal(dsl.status, "unknown");
  assert.equal(dsl.progress, 1);
  assert.equal(dsl.nodes[0].status, "unknown");
  assert.equal(dsl.nodes[0].depth, 12);
  assert.equal(dsl.edges.length, 1);
  assert.equal(dsl.edges[0].dangling, true);
});

test("renders escaped cards, status dots, progress, and dependency edges", () => {
  const plan = parallelPlan("completed", 1);
  plan.name = '<img src=x onerror="boom">';
  plan.nodes[1].label = "<script>alert(1)</script>";
  plan.nodes[1].output = "done <b>not markup</b>";
  const html = renderPlanDSL(planToDSL(plan));

  assert.match(html, /data-plan-board/);
  assert.match(html, /aria-valuenow="100"/);
  assert.match(html, /plan-dsl-node is-completed/);
  assert.match(html, /plan-edge is-completed/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /<b>not markup<\/b>/);
});

test("maps queued, running, and completed JSON updates without stale state", () => {
  const states = ["queued", "running", "completed"].map(status => {
    const dsl = planToDSL(parallelPlan(status, status === "completed" ? 0.5 : 0.25));
    return {
      key: dsl.nodes.find(node => node.id === "left").key,
      status: dsl.nodes.find(node => node.id === "left").status,
      html: renderPlanDSL(dsl)
    };
  });

  assert.deepEqual(states.map(state => state.key), ["left", "left", "left"]);
  assert.deepEqual(states.map(state => state.status), ["queued", "running", "completed"]);
  assert.match(states[0].html, /plan-dsl-node is-queued/);
  assert.match(states[1].html, /plan-dsl-node is-running/);
  assert.match(states[2].html, /plan-dsl-node is-completed/);
});

test("returns null for missing Plan JSON", () => {
  assert.equal(planToDSL(null), null);
  assert.equal(planToDSL([]), null);
  assert.equal(renderPlanDSL(null), "");
});

test("extracts node event timeline for the detail page", () => {
  const plan = {
    name: "with events",
    status: "running",
    nodes: [{
      id: "agent-1", label: "Audit module", kind: "agent", status: "running",
      events: [
        { status: "queued", at: "2026-08-02T10:00:00Z" },
        { status: "running", at: "2026-08-02T10:00:05Z", output: "started" },
        { status: "running", at: "2026-08-02T10:00:20Z" },
        { status: "completed", at: "2026-08-02T10:01:00Z", output: "done reading module" }
      ]
    }]
  };
  const dsl = planToDSL(plan);
  const node = dsl.nodes[0];
  assert.equal(node.events.length, 4);
  assert.deepEqual(node.events.map(event => event.status), ["queued", "running", "running", "completed"]);
  assert.equal(node.events[3].output, "done reading module");
  assert.equal(node.events[0].at, "2026-08-02T10:00:00Z");

  const html = renderPlanDSL(dsl);
  assert.match(html, /data-plan-node-open="agent-1"/);
  assert.match(html, /has-events/);
});

test("renders the three detail data surfaces with escaped content", () => {
  const plan = {
    name: "detail",
    status: "completed",
    nodes: [{
      id: "n1", label: "<script>alert(1)</script>", kind: "agent", status: "completed", elapsed: "12s",
      output: "ok <b>not markup</b>",
      events: [
        { status: "queued", at: "2026-08-02T10:00:00Z" },
        { status: "completed", at: "2026-08-02T10:00:12Z", output: "done" }
      ]
    }]
  };
  const dsl = planToDSL(plan);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });

  assert.match(detail, /data-node-detail/);
  assert.match(detail, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(detail, /<script>/);
  assert.match(detail, />TASKLIST</); // 模式徽标（completed 状态 → tasklist）
  // 只剩三块数据面：会话记录 / 上下文 / 功能打点。
  assert.deepEqual([...detail.matchAll(/data-node-tab="([^"]+)"/g)].map(match => match[1]), ["conversation", "context", "instrumentation"]);
  assert.deepEqual([...detail.matchAll(/data-node-panel="([^"]+)"/g)].map(match => match[1]), ["conversation", "context", "instrumentation"]);
  assert.match(detail, /data-node-conversation/);
  assert.match(detail, /data-node-context/);
  assert.match(detail, /data-node-instrumentation/);
  assert.match(detail, /加载会话记录…/);
  assert.match(detail, /加载上下文快照…/);
});

test("drops the removed live/timeline/tools/output/worktree surfaces from the detail modal", () => {
  const dsl = planToDSL({ name: "quiet", status: "pending", nodes: [{ id: "n1", status: "pending", output: "final text" }] });
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.doesNotMatch(detail, /data-node-tab="(live|timeline|tools|output)"/);
  assert.doesNotMatch(detail, /data-node-panel="(live|timeline|tools|output)"/);
  assert.doesNotMatch(detail, /data-node-live-feed|node-live-row|data-node-tool-events|data-node-tool-count|node-detail-output|node-worktree-section/);
  // 事件计数仍是身份元信息（不再有事件时间线标签）。
  assert.match(detail, /<dt>事件<\/dt>/);
  assert.doesNotMatch(detail, /final text/);
});

test("renders Dify-style branch flow inside nodes (outgoing targets, conditions, parallel forks)", () => {
  const plan = parallelPlan("pending", 0);
  const dsl = planToDSL(plan);
  const start = dsl.nodes.find(node => node.id === "start");
  assert.equal(start.outgoing.length, 2); // fork ×2 → 并行分支
  const html = renderPlanDSL(dsl);
  // 分支行：目标节点名 + 条件/标签；并行箭头 ⇉
  assert.match(html, /data-plan-node-field="branches"/);
  assert.match(html, /plan-branch-row/);
  assert.match(html, /⇉/);
  assert.match(html, /data-branch-target="left">Left branch</);
  assert.match(html, /branch-condition">fork</);
  // 节点仍带完整 outgoing 对象（targetLabel 供树语义使用）
  assert.equal(start.outgoing[0].targetLabel, "Left branch");
  assert.equal(start.outgoing[1].label, "fork"); // 条件边在 left→join / right→join 上
  const rightToJoin = dsl.edges.find(edge => edge.from === "right" && edge.to === "join");
  assert.equal(rightToJoin.condition, '{"when":"approved"}');
  // 依赖（incoming）仍在
  assert.match(html, /plan-dependency/);
});

test("labels tasklist gate vs plan-run mode from authoritative status", () => {
  const running = planToDSL(parallelPlan("running", 0.5));
  assert.equal(running.mode, "plan");
  assert.match(renderPlanDSL(running), /plan-mode is-plan/);
  assert.match(renderPlanDSL(running), />PLAN RUN</);

  const pending = planToDSL({ ...parallelPlan("pending", 0), status: "pending" });
  assert.equal(pending.mode, "tasklist");
  const html = renderPlanDSL(pending);
  assert.match(html, /plan-mode is-tasklist/);
  assert.match(html, />TASKLIST</);
  assert.match(html, /task_check_node/);

  const completed = planToDSL({ ...parallelPlan("completed", 1), status: "completed" });
  assert.equal(completed.mode, "tasklist");
  assert.match(renderPlanDSL(completed), />TASKLIST</);
});

test("renders tasklist checkpoints and subagent tool events in the function instrumentation table", () => {
  const plan = {
    name: "instrumented", status: "pending", progress: 0.5,
    nodes: [
      {
        id: "check", label: "Check source", kind: "auto", status: "completed",
        events: [{ status: "completed", at: "2026-08-04T08:00:00Z", output: "read <main>" }]
      },
      {
        id: "worker", label: "Worker", kind: "agent", status: "running",
        tool_events: [{ id: "tool-1", node_id: "worker", name: "read_file", status: "running", arguments: "main.go", started_at: "2026-08-04T08:01:00Z" }]
      }
    ]
  };
  const dsl = planToDSL(plan);
  const board = renderPlanDSL(dsl);
  assert.match(board, /功能打点/);
  assert.match(board, /task_check_node/);
  assert.match(board, /read_file/);
  assert.match(board, /read &lt;main&gt;/);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, /data-node-tab="instrumentation"/);
  assert.match(detail, /task_check_node/);
});

test("merges worktree lifecycle status and subagent tool activity into the instrumentation table", () => {
  const plan = {
    name: "worktree flow", status: "running", progress: 0.5,
    nodes: [{
      id: "worker", label: "Worker", kind: "agent", status: "rebasing",
      tool_events: [{
        id: "subtool-1", node_id: "worker", name: "bash", status: "error",
        arguments: "<unsafe>", error: "conflict <main>", started_at: "2026-08-04T08:00:00Z", duration: 250000000
      }]
    }]
  };
  const dsl = planToDSL(plan);
  assert.equal(dsl.nodes[0].status, "rebasing");
  assert.equal(dsl.nodes[0].toolEvents[0].name, "bash");
  assert.match(renderPlanDSL(dsl), /plan-dsl-node is-rebasing/);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, />REBASING</);
  // 工具活动已并入「功能打点」：操作名 + 失败证据 + 状态着色都在同一张表里。
  assert.match(detail, /instrumentation-row is-error/);
  assert.match(detail, /<strong>bash<\/strong>/);
  assert.match(detail, /conflict &lt;main&gt;/);
  assert.doesNotMatch(detail, /<unsafe>/);
  assert.doesNotMatch(detail, /data-node-tool-id=/);
});

test("renders the context tab placeholder in the node detail modal", () => {
  const dsl = planToDSL({ name: "ctx", status: "running", nodes: [{ id: "agent-1", kind: "agent", status: "running" }] });
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, /data-node-tab="context"/);
  assert.match(detail, /data-node-panel="context"/);
  assert.match(detail, /data-node-context/);
  assert.match(detail, /加载上下文快照/);
});

test("renders subagent context snapshot with escaped content", () => {
  const html = renderNodeContext({
    goal: "<script>alert(1)</script>",
    progress: "60%",
    message_count: 12,
    token_estimate: 1234,
    findings: ["found <b>race</b>"],
    decisions: [{ what: "use mutex", why: "because <i>reason</i>" }],
    constraints: ["no merge"],
    pending_work: ["run tests"]
  });
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /found &lt;b&gt;race&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<b>race<\/b>/);
  assert.match(html, /use mutex — because &lt;i&gt;reason&lt;\/i&gt;/);
  assert.match(html, /60%/);
  assert.match(html, /12/);
  assert.match(html, /1,234/);
  assert.match(html, /no merge/);
  assert.match(html, /run tests/);
});

test("renders empty context placeholder for missing or empty snapshots", () => {
  assert.match(renderNodeContext(null), /无上下文快照/);
  assert.match(renderNodeContext(undefined), /无上下文快照/);
  assert.match(renderNodeContext({}), /上下文快照为空/);
});

// ── 工作区现场已从详情弹窗下线（renderNodeWorktree 与其在「上下文」标签里
// 的前置 section 一并删除；worktree 现场仍留在后端载荷，但前端不再有渲染分支）

test("no longer exports the worktree detail renderer", async () => {
  const module = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
  assert.equal(module.renderNodeWorktree, undefined);
});

test("keeps the detail modal free of worktree recovery markup", () => {
  const dsl = planToDSL({ name: "recovery", status: "failed", nodes: [{ id: "worker", status: "failed" }] });
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.doesNotMatch(detail, /node-worktree-section|工作区现场|git merge|git add -A/);
  assert.match(detail, /data-node-detail-error/); // 失败原文区仍在（执行失败）
});

// ── Plan 树状布局（W2：DAG → 树）──────────────────────────────────

test("lays out a parallel DAG as a tree: topological levels, main path, side refs", () => {
  const dsl = planToDSL(parallelPlan("pending", 0));
  const byID = Object.fromEntries(dsl.nodes.map(node => [node.id, node]));
  assert.equal(byID.start.depth, 0);
  assert.equal(byID.left.depth, 1);
  assert.equal(byID.right.depth, 1);
  assert.equal(byID.join.depth, 2); // 主路径 start → left → join（左分支层更深/边序优先）
  assert.equal(byID.join.treeParentID, "left");
  assert.deepEqual(byID.join.sideRefs.map(ref => ref.from), ["right"]);
  assert.equal(byID.start.treeParentID, "");
  assert.equal(byID.left.sideRefs.length, 0);
  // 树序渲染顺序稳定：按 (depth, 原序) 排序。
  assert.deepEqual(dsl.nodes.map(node => node.id), ["start", "left", "right", "join"]);
  // 树序与状态更新正交：同图不同状态产出相同 key 序列（reconcile 稳定）。
  const running = planToDSL(parallelPlan("running", 0.5));
  assert.deepEqual(running.nodes.map(node => node.key), dsl.nodes.map(node => node.key));
});

test("renders tree guide characters and side-ref chips for diamond joins", () => {
  const dsl = planToDSL(parallelPlan("pending", 0));
  const byID = Object.fromEntries(dsl.nodes.map(node => [node.id, node]));
  assert.equal(byID.left.treeGuide, "├─ ");
  assert.equal(byID.right.treeGuide, "└─ ");
  assert.equal(byID.join.treeGuide, "│  └─ ");
  const html = renderPlanDSL(dsl);
  assert.match(html, /data-plan-tree-guide/);
  assert.match(html, />├─ </);
  assert.match(html, />│\s+└─ </); // 引导字符白空格原样保留
  assert.match(html, /data-plan-side-ref="right"/);
  assert.match(html, />旁路 right · {&quot;when&quot;:&quot;approved&quot;}/); // chip 文本（来源 + 条件标签）
  assert.match(html, /data-plan-tree-depth="2"/);
});

test("keeps children-nesting depth and no tree guide when a plan has no edges", () => {
  const dsl = planToDSL({ name: "nested", status: "pending", nodes: [{ id: "fork", status: "pending", children: [{ id: "child", status: "queued" }] }] });
  assert.deepEqual(dsl.nodes.map(node => node.depth), [0, 1]);
  assert.equal(dsl.nodes[1].treeGuide ?? "", "");
  assert.equal(dsl.nodes[1].treeParentID ?? "", "");
  const html = renderPlanDSL(dsl);
  assert.doesNotMatch(html, /data-plan-tree-guide/);
});

test("breaks DAG cycles without infinite loops and renders every node once", () => {
  const dsl = planToDSL({
    name: "cycle", status: "running",
    nodes: [{ id: "a", status: "pending" }, { id: "b", status: "pending" }],
    edges: [{ from: "a", to: "b" }, { from: "b", to: "a" }]
  });
  assert.equal(dsl.nodes.length, 2);
  // 环内节点按根处理（level 0），深度有界、无 treeParent 死循环。
  assert.deepEqual(dsl.nodes.map(node => node.depth), [0, 0]);
  assert.ok(dsl.nodes.every(node => node.treeParentID === "" && node.treeGuide === ""));
  const html = renderPlanDSL(dsl);
  assert.match(html, /data-plan-node-open="a"/);
  assert.match(html, /data-plan-node-open="b"/);
});

test("supports multiple roots and dangling edges without breaking the tree", () => {
  const dsl = planToDSL({
    name: "multi", status: "running",
    nodes: [{ id: "r1", status: "pending" }, { id: "r2", status: "pending" }, { id: "x", status: "pending" }],
    edges: [{ from: "r1", to: "x" }, { from: "ghost", to: "r2" }]
  });
  assert.equal(dsl.nodes.find(node => node.id === "r1").depth, 0);
  assert.equal(dsl.nodes.find(node => node.id === "r2").depth, 0); // 悬垂入边不算层级
  assert.equal(dsl.nodes.find(node => node.id === "x").depth, 1);
  assert.equal(dsl.nodes.find(node => node.id === "x").treeParentID, "r1");
  assert.doesNotThrow(() => renderPlanDSL(dsl));
});

// ── 子代理树（fork 内存态可视化）──────────────────────────────────

test("renders the subagent tree with nesting guides, status colors, and escaped content", () => {
  const html = renderSubagentTree([
    {
      id: "main", status: "running", children: [
        { id: "s1", status: "running", goal: "audit <main>", children: [
          { id: "s1a", status: "failed", goal: "deep", summary: "boom <b>", error: "oops",
            context: { goal: "deep", progress: "60%", message_count: 12, token_estimate: 4321, findings: ["found <b>race</b>"] } }
        ] },
        { id: "s2", status: "done", goal: "safe", summary: "ok", session_id: "sess_0123456789abcdef" }
      ]
    }
  ]);
  assert.match(html, /data-subagent-tree/);
  assert.match(html, /data-subagent-status="running"/);
  assert.match(html, /data-subagent-status="done"/);
  assert.match(html, /data-subagent-status="failed"/);
  assert.match(html, /data-plan-node-open="s1a"/);
  // 每个树节点行都有显式「详情」入口（T3：运行时上下文查看的可发现性）。
  assert.match(html, /data-plan-node-open="s1"/);
  assert.match(html, /data-plan-node-open="s2"/);
  assert.match(html, />详情<\/button>/);
  assert.match(html, /audit &lt;main&gt;/);
  assert.doesNotMatch(html, /<main>/);
  assert.match(html, /boom &lt;b&gt;/);
  assert.doesNotMatch(html, /<b>boom<\/b>/);
  assert.match(html, />├─ </); // s1 引导字符
  assert.match(html, />│\s+└─ </); // s1a 深层引导（│ + └─）
  assert.match(html, />└─ </); // s2 引导字符
  assert.match(html, /主代理/);
  assert.match(html, /sess_012…abcdef/);
  assert.match(html, />DONE</);
  // 紧凑上下文（消息数/token/进度/findings）挂在节点上且全部 escape。
  assert.match(html, /12 消息/);
  assert.match(html, /4,321 tok/);
  assert.match(html, /60%/);
  assert.match(html, /found &lt;b&gt;race&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<b>race<\/b>/);
  assert.match(html, /data-subagent-tree-context|subagent-tree-context/);
  assert.equal(renderSubagentTree(null), "");
  assert.equal(renderSubagentTree([]), "");
  assert.equal(renderSubagentTree(undefined), "");
});

test("renders compact tree context only when present and escapes all text", () => {
  const html = renderSubagentTree([{
    id: "main", status: "running", children: [
      { id: "plain", status: "done", goal: "g" },
      { id: "ctx", status: "running", goal: "g2",
        context: { progress: "<script>alert(1)</script>", findings: ["<img src=x>"] } }
    ]
  }]);
  // 无 context 的节点行内不渲染上下文块（行边界内断言）。
  const plainRow = html.slice(html.indexOf('data-plan-node-open="plain"'), html.indexOf('data-plan-node-open="ctx"'));
  assert.doesNotMatch(plainRow, /subagent-tree-context/);
  // 有 context 的节点渲染且 escape。
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;img src=x&gt;/);
  assert.doesNotMatch(html, /<img src=x>/);
});

test("converts a subagent tree node into a detail DSL node", () => {
  const done = subagentTreeNodeToDSL({ id: "s1", status: "done", goal: "goal <text>", summary: "sum", parent_id: "main" });
  assert.equal(done.key, "s1");
  assert.equal(done.id, "s1");
  assert.equal(done.status, "completed");
  assert.equal(done.label, "goal <text>");
  assert.equal(done.output, "sum");
  assert.equal(done.parentKey, "main");
  assert.equal(subagentTreeNodeToDSL({ id: "x", status: "failed" }).status, "failed");
  assert.equal(subagentTreeNodeToDSL({ id: "y", status: "running" }).status, "running");
  assert.equal(subagentTreeNodeToDSL({ id: "q", status: "queued" }).status, "queued");
  assert.equal(subagentTreeNodeToDSL({ id: "i", status: "interrupted" }).status, "interrupted");
  assert.equal(subagentTreeNodeToDSL({ id: "z" }).status, "unknown"); // 未知状态 → unknown 徽标
  assert.equal(subagentTreeNodeToDSL({ status: "done" }).key, ""); // 缺 id → 空 key
});

test("converts a work table row into a detail DSL node for the fallback entry", () => {
  // 工作表格行先到（树未到）时的兜底身份：source_id 是后端节点 id。
  const row = workItemToDetailNode({
    id: "subagent:s1", kind: "subagent", status: "done", source_id: "s1",
    task: "调研 <作者>", description: "结论", elapsed: "2m"
  });
  assert.equal(row.key, "s1");
  assert.equal(row.id, "s1");
  assert.equal(row.kind, "subagent");
  assert.equal(row.status, "completed"); // done → completed（详情状态词汇）
  assert.equal(row.label, "调研 <作者>"); // 渲染层统一 escape
  assert.equal(row.output, "结论");
  assert.equal(row.elapsed, "2m");
  assert.deepEqual(row.events, []);

  // 缺 source_id 时退回行 id（后端按同一 key 查详情），缺 task 时退回描述。
  const fallback = workItemToDetailNode({ id: "task:7", kind: "task", status: "doing", description: "描述" });
  assert.equal(fallback.key, "task:7");
  assert.equal(fallback.label, "描述");
  assert.equal(fallback.status, "running");

  // 关键词汇映射：失败/中断保留语义，未知不猜。
  assert.equal(workItemToDetailNode({ source_id: "a", status: "failed" }).status, "failed");
  assert.equal(workItemToDetailNode({ source_id: "b", status: "error" }).status, "failed");
  assert.equal(workItemToDetailNode({ source_id: "c", status: "interrupted" }).status, "interrupted");
  assert.equal(workItemToDetailNode({ source_id: "d", status: "weird" }).status, "unknown");
  assert.equal(workItemToDetailNode({ source_id: "e" }).status, "unknown");

  // 纯身份兜底（既不在 Plan DSL 也不在树、表格行也查不到时）。
  const identity = workItemToDetailNode({ id: "orphan", source_id: "orphan" });
  assert.equal(identity.key, "orphan");
  assert.equal(identity.label, "orphan");
  assert.equal(identity.kind, "auto");
});
