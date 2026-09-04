export const SUPPORTED_PROTOCOL_VERSION = 1;

const INCREMENTAL_KINDS = new Set([
  "message.added", "message.delta", "tool.started", "tool.completed",
  "subagent.changed", "subagent.tool.started", "subagent.tool.completed",
  "chat.changed", "runtime.changed", "worktable.changed", "task.changed",
  "interaction.opened", "interaction.closed"
]);

// PROCESS_KINDS 是进程级事件（G2/M2）：不带会话归属（session_id 必空），
// 投递给全部订阅。白名单之外的 kind 一律视为会话级：session_id 必须与当前
// 视图快照的 session.id 精确匹配——空 sid 不再作为会话事件的通配。
const PROCESS_KINDS = new Set(["resync.required", "app.exit_requested"]);

const MAX_FRONTEND_NODE_TOOL_EVENTS = 100;

export function belongsToView(event, snapshot) {
  if (!event || typeof event !== "object") return false;
  if (PROCESS_KINDS.has(event.kind)) {
    return !event.session_id;
  }
  const eventSession = event.session_id;
  // 过渡期：草稿/存量事件可能尚缺 sid（投递端已按订阅键过滤），G4 早分配
  // SID 后服务端保证会话类事件必带 sid，此处再收紧为缺失即拒。
  if (!eventSession) return true;
  const viewSession = (snapshot && snapshot.session && snapshot.session.id) || "";
  return eventSession === viewSession;
}

export function validateSnapshot(snapshot) {
  if (!snapshot || typeof snapshot !== "object") throw new Error("GUI snapshot 无效");
  assertProtocol(snapshot.protocol_version, "Snapshot");
  if (!Array.isArray(snapshot.conversation)) throw new Error("GUI snapshot 缺少 conversation");
  return snapshot;
}

export function applyEvent(snapshot, event, lastSeq = 0, snapshotRevisionFloor = 0) {
  if (!event || typeof event !== "object") return refreshResult(snapshot, lastSeq);
  try {
    assertProtocol(event.protocol_version, "Event");
  } catch (error) {
    return { snapshot, lastSeq, needsRefresh: false, error };
  }
  const seq = Number(event.delivery_seq || 0);
  // G2/M2：会话级事件带 sid 时必须归属当前视图会话；进程级事件不得带 sid。
  // 投递端已按订阅键过滤，此处只做防御性硬校验：不匹配的事件丢弃但推进
  // 水位（服务端不会给本订阅投递它，避免异常路径造成后续事件连环跳号）。
  if (!belongsToView(event, snapshot)) {
    return { snapshot, lastSeq: seq, needsRefresh: false, dropped: true };
  }
  // 连续性只按 delivery_seq（本订阅内的投递序号）判定：会话归属由 application
  // 在投递端过滤，全局 seq 因此必然跳号，跳号不代表丢事件；缺口才是。
  if (!seq || (lastSeq && seq > lastSeq + 1)) {
    // 刻意不推进 lastSeq：宿主会先按 delivery_seq 增量补取这段缺口（C4），
    // 补得齐就不必整份重拉快照。gapSeq 是补不齐时重拉后要落到的水位。
    return { snapshot, lastSeq, needsRefresh: true, gap: true, gapSeq: Math.max(lastSeq, seq) };
  }
  if (lastSeq && seq <= lastSeq) return { snapshot, lastSeq, needsRefresh: false };
  if (!snapshot || !INCREMENTAL_KINDS.has(event.kind)) return refreshResult(snapshot, seq);
  const revision = Number(event.revision || 0);
  if (revision && revision <= Number(snapshotRevisionFloor || 0)) {
    return { snapshot, lastSeq: seq, needsRefresh: false };
  }

  const payload = decodePayload(event.payload);
  const next = cloneSnapshot(snapshot, event.revision);
  const applied = applyIncremental(next, event, payload);
  return applied
    ? { snapshot: next, lastSeq: seq, needsRefresh: false, changed: event.kind }
    : refreshResult(snapshot, seq);
}

function applyIncremental(snapshot, event, payload) {
  switch (event.kind) {
  case "message.added":
  case "tool.started":
  case "tool.completed":
    if (!payload?.id) return false;
    // 窗口游标（total_messages / history_offset / has_more_history）与截断都是
    // 后端 view_state 的投影：reducer 只 upsert 消息，下一次 snapshot.changed
    // 会带回权威窗口，客户端不再复刻这份规则。
    snapshot.conversation = upsertMessage(snapshot.conversation, payload);
    flushPendingDeltasForSnapshot(snapshot, payload.id);
    return true;
  case "message.delta":
    return appendMessageDelta(snapshot, payload);
  case "chat.changed":
    // 运行态只有后端能说：ChatState 整体替换，客户端不自造 running/request_id。
    if (!payload || typeof payload !== "object") return false;
    snapshot.chat = payload;
    return true;
  case "subagent.changed":
    return applySubagentChanged(snapshot, payload);
  case "subagent.tool.started":
  case "subagent.tool.completed":
    return applySubagentToolEvent(snapshot, payload);
  case "runtime.changed":
    if (!payload || typeof payload !== "object") return false;
    snapshot.runtime = payload;
    return true;
  case "worktable.changed":
    if (!payload || !Array.isArray(payload.items)) return false;
    snapshot.runtime.work_table = payload.items;
    // batches 是可选增量（批次头：标签/时间/各类计数）；缺失时保留既有
    // 批次（task.changed 单行增量不携带 batches）。
    if (Array.isArray(payload.batches)) {
      snapshot.runtime.work_table_batches = payload.batches;
    }
    return true;
  case "task.changed":
    if (!payload?.task || !payload.task_id || typeof payload.task !== "object") return false;
    {
      const tasks = Array.isArray(snapshot.runtime.work_table) ? [...snapshot.runtime.work_table] : [];
      const index = tasks.findIndex(item => item?.id === payload.task_id);
      if (index < 0) tasks.push(payload.task);
      else tasks[index] = payload.task;
      snapshot.runtime.work_table = tasks;
    }
    return true;
  case "interaction.opened":
    snapshot.interaction = payload || null;
    return true;
  case "interaction.closed":
    snapshot.interaction = null;
    return true;
  default:
    return false;
  }
}

function appendMessageDelta(snapshot, payload) {
  if (!payload?.message_id) return false;
  const hasDelta = typeof payload.delta === "string";
  const hasReasoning = typeof payload.reasoning_content === "string";
  if (!hasDelta && !hasReasoning) return false;
  const index = snapshot.conversation.findIndex(message => message.id === payload.message_id);
  if (index < 0) {
    // 增量先于 added 到达（跨进程乱序/丢 added 后重放）：缓冲等待，不触发
    // 整份刷新——否则高频流式会退化为刷新风暴，表现为“当前视图内容不及时”。
    if (!Array.isArray(snapshot._pending_deltas)) snapshot._pending_deltas = [];
    snapshot._pending_deltas.push({ ...payload });
    return true;
  }
  const messages = [...snapshot.conversation];
  const next = { ...messages[index] };
  if (hasDelta) next.content = (messages[index].content || "") + payload.delta;
  if (hasReasoning) next.reasoning_content = payload.reasoning_content;
  messages[index] = next;
  snapshot.conversation = messages;
  return true;
}

function upsertMessage(messages, message) {
  const next = [...messages];
  const index = next.findIndex(current => current.id === message.id);
  if (index < 0) next.push(message);
  else next[index] = message;
  return next;
}

// flushPendingDeltasForSnapshot 在 added/tool 消息落地后把此前缓冲的 delta 应用到该
// 消息（不丢增量、不整份刷新）。
function flushPendingDeltasForSnapshot(snapshot, messageID) {
  const index = (snapshot.conversation || []).findIndex(current => current.id === messageID);
  if (index < 0) return;
  const target = { ...snapshot.conversation[index] };
  const pending = snapshot._pending_deltas;
  if (!Array.isArray(pending)) return;
  const kept = [];
  for (const delta of pending) {
    if (delta.message_id !== messageID) {
      kept.push(delta);
      continue;
    }
    if (typeof delta.delta === "string") {
      target.content = (target.content || "") + delta.delta;
    }
    if (typeof delta.reasoning_content === "string") {
      target.reasoning_content = delta.reasoning_content;
    }
  }
  snapshot.conversation[index] = target;
  if (kept.length === 0) delete snapshot._pending_deltas;
  else snapshot._pending_deltas = kept;
}

function applySubagentChanged(snapshot, payload) {
  if (!payload?.node_id || !payload.node || !snapshot.runtime?.plan) return false;
  const replaced = mapPlanNodePath(snapshot.runtime.plan.nodes || [], payload.node_id, () => clonePlanNode(payload.node));
  if (!replaced.changed) return false;
  const next = { ...snapshot.runtime.plan, nodes: replaced.nodes };
  if (typeof payload.plan_status === "string" && payload.plan_status) {
    next.status = payload.plan_status;
  }
  const progress = Number(payload.progress);
  if (Number.isFinite(progress)) next.progress = Math.min(Math.max(progress, 0), 1);
  snapshot.runtime.plan = next;
  return true;
}

function applySubagentToolEvent(snapshot, payload) {
  if (!payload?.id || !payload.node_id || !snapshot.runtime?.plan) return false;
  const replaced = mapPlanNodePath(snapshot.runtime.plan.nodes || [], payload.node_id, node => {
    const toolEvents = [...(node.tool_events || [])];
    const index = toolEvents.findIndex(current => current.id === payload.id);
    if (index < 0) toolEvents.push(payload);
    else toolEvents[index] = payload;
    return { ...node, tool_events: toolEvents.slice(-MAX_FRONTEND_NODE_TOOL_EVENTS) };
  });
  if (!replaced.changed) return false;
  snapshot.runtime.plan = { ...snapshot.runtime.plan, nodes: replaced.nodes };
  return true;
}

// mapPlanNodePath 沿命中节点路径复制（结构共享）：未命中分支原样复用，
// 只重建命中节点所在链，避免每次事件深拷贝整棵 plan（内存 P1 优化）。
function mapPlanNodePath(nodes, nodeID, update) {
  let changed = false;
  const next = (nodes || []).map(node => {
    if (node.id === nodeID) {
      changed = true;
      return update(node);
    }
    const nested = mapPlanNodePath(node.children || [], nodeID, update);
    if (!nested.changed) return node;
    changed = true;
    return { ...node, children: nested.nodes };
  });
  return { nodes: next, changed };
}

function cloneSnapshot(snapshot, revision) {
  const clone = {
    ...snapshot,
    revision: Math.max(Number(snapshot.revision || 0), Number(revision || 0)),
    conversation: [...snapshot.conversation],
    chat: { ...(snapshot.chat || {}) },
    runtime: { ...(snapshot.runtime || {}) }
  };
  if (Array.isArray(snapshot._pending_deltas)) {
    clone._pending_deltas = [...snapshot._pending_deltas];
  }
  return clone;
}

function clonePlanNode(node) {
  return {
    ...node,
    events: Array.isArray(node.events) ? node.events.map(event => ({ ...event })) : [],
    tool_events: Array.isArray(node.tool_events) ? node.tool_events.map(event => ({ ...event })) : [],
    children: Array.isArray(node.children) ? node.children.map(clonePlanNode) : []
  };
}

function decodePayload(payload) {
  if (typeof payload !== "string") return payload;
  try { return JSON.parse(payload); }
  catch { return payload; }
}

function assertProtocol(version, source) {
  if (Number(version) !== SUPPORTED_PROTOCOL_VERSION) {
    throw new Error(`${source} 协议版本 ${version ?? "缺失"} 不受支持，GUI 仅支持 v${SUPPORTED_PROTOCOL_VERSION}`);
  }
}

function refreshResult(snapshot, lastSeq) {
  return { snapshot, lastSeq, needsRefresh: true };
}
