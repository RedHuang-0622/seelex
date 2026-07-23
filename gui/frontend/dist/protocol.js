export const SUPPORTED_PROTOCOL_VERSION = 1;

const INCREMENTAL_KINDS = new Set([
  "message.added", "message.delta", "tool.started", "tool.completed",
  "runtime.changed", "interaction.opened", "interaction.closed"
]);

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
  const seq = Number(event.seq || 0);
  if (!seq || (lastSeq && seq > lastSeq + 1)) return refreshResult(snapshot, Math.max(lastSeq, seq));
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
    snapshot.conversation = upsertMessage(snapshot.conversation, payload);
    markRunning(snapshot, event.request_id);
    return true;
  case "message.delta":
    return appendMessageDelta(snapshot, payload);
  case "runtime.changed":
    if (!payload || typeof payload !== "object") return false;
    snapshot.runtime = payload;
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
  if (!payload?.message_id || typeof payload.delta !== "string") return false;
  const index = snapshot.conversation.findIndex(message => message.id === payload.message_id);
  if (index < 0) return false;
  const messages = [...snapshot.conversation];
  messages[index] = { ...messages[index], content: (messages[index].content || "") + payload.delta };
  snapshot.conversation = messages;
  markRunning(snapshot, snapshot.chat?.request_id);
  return true;
}

function upsertMessage(messages, message) {
  const next = [...messages];
  const index = next.findIndex(current => current.id === message.id);
  if (index < 0) next.push(message);
  else next[index] = message;
  return next;
}

function markRunning(snapshot, requestID) {
  if (!requestID) return;
  snapshot.chat = { ...(snapshot.chat || {}), running: true, request_id: requestID };
}

function cloneSnapshot(snapshot, revision) {
  return {
    ...snapshot,
    revision: Math.max(Number(snapshot.revision || 0), Number(revision || 0)),
    conversation: [...snapshot.conversation],
    chat: { ...(snapshot.chat || {}) },
    runtime: { ...(snapshot.runtime || {}) }
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
