// Collects the files that the agent actually read in the current conversation.
// A read_file start alone is not evidence: only a successful matching result is
// displayed, so failed paths and in-flight calls never appear as sources.
export function collectReadFileSources(conversation = [], cachedFiles = []) {
  const pending = new Map();
  const sources = new Map();

  for (const cached of cachedFiles) {
    const path = typeof cached?.path === "string" ? cached.path.trim() : "";
    if (!path || sources.has(path)) continue;
    sources.set(path, { name: fileName(path), kind: "read", path });
  }

  for (const message of conversation) {
    const tool = message?.tool;
    if (tool?.name !== "read_file" || !tool.id) continue;

    if (message.role === "tool") {
      const path = readPath(tool.arguments);
      if (path) pending.set(tool.id, path);
      continue;
    }

    if (message.role !== "tool_result" || !isSuccessful(tool)) continue;
    const path = pending.get(tool.id);
    if (!path || sources.has(path)) continue;
    sources.set(path, { name: fileName(path), kind: "read", path });
  }

  return [...sources.values()];
}

function readPath(argumentsJSON) {
  if (typeof argumentsJSON !== "string" || !argumentsJSON.trim()) return "";
  try {
    const input = JSON.parse(argumentsJSON);
    return typeof input?.path === "string" ? input.path.trim() : "";
  } catch {
    return "";
  }
}

function isSuccessful(tool) {
  return !tool.error && tool.status !== "error" && tool.status !== "failed";
}

function fileName(path) {
  const parts = path.split(/[\\/]/);
  return parts.at(-1) || path;
}
