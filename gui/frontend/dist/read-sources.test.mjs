import assert from "node:assert/strict";
import test from "node:test";

import { collectReadFileSources } from "./read-sources.js";

test("lists only successfully read files and deduplicates paths", () => {
  const sources = collectReadFileSources([
    { role: "tool", tool: { id: "read-1", name: "read_file", arguments: '{"path":"application/core/chat.go"}', status: "running" } },
    { role: "tool_result", tool: { id: "read-1", name: "read_file", result: "package core", status: "success" } },
    { role: "tool", tool: { id: "read-2", name: "read_file", arguments: '{"path":"application/core/chat.go"}', status: "running" } },
    { role: "tool_result", tool: { id: "read-2", name: "read_file", result: "package core", status: "success" } },
    { role: "tool", tool: { id: "read-3", name: "read_file", arguments: '{"path":"missing.go"}', status: "running" } },
    { role: "tool_result", tool: { id: "read-3", name: "read_file", error: "not found", status: "error" } }
  ]);

  assert.deepEqual(sources, [{ name: "chat.go", kind: "read", path: "application/core/chat.go" }]);
});

test("ignores malformed and incomplete read_file calls", () => {
  const sources = collectReadFileSources([
    { role: "tool", tool: { id: "bad", name: "read_file", arguments: "not-json", status: "running" } },
    { role: "tool_result", tool: { id: "bad", name: "read_file", result: "anything", status: "success" } },
    { role: "tool", tool: { id: "pending", name: "read_file", arguments: '{"path":"pending.go"}', status: "running" } },
    { role: "tool_result", tool: { id: "other", name: "grep_search", result: "anything", status: "success" } }
  ]);

  assert.deepEqual(sources, []);
});
