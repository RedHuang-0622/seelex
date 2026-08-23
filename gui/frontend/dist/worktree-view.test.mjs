import assert from "node:assert/strict";
import test from "node:test";

import { formatSize, renderWorkTreeHTML, worktreeView } from "./worktree-view.js";

function sampleState(overrides = {}) {
  return {
    children: new Map(),
    expanded: new Set(),
    loading: new Set(),
    truncated: new Map(),
    error: "",
    ...overrides
  };
}

test("normalizes tree entries and drops malformed rows", () => {
  const rows = worktreeView([
    { name: "src", path: "src", type: "dir", count: 2 },
    { name: "README.md", path: "README.md", type: "file", size: 1024 },
    { name: "", path: "", type: "file" },
    { name: "link", path: "link", type: "symlink", size: 1 },
    null,
    "bad"
  ]);
  assert.deepEqual(rows, [
    { name: "src", path: "src", type: "dir", size: 0, count: 2 },
    { name: "README.md", path: "README.md", type: "file", size: 1024, count: 0 },
    { name: "link", path: "link", type: "file", size: 1, count: 0 }
  ]);
});

test("renders root entries and escapes text", () => {
  const html = renderWorkTreeHTML(worktreeView([
    { name: "<script>", path: "<script>", type: "file", size: 5 }
  ]), sampleState());
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(html.includes("tree-row is-file"));
  assert.ok(html.includes("tree-size"));
});

test("renders expanded directory children recursively with counts", () => {
  const entries = worktreeView([
    { name: "src", path: "src", type: "dir", count: 1 },
    { name: "README.md", path: "README.md", type: "file", size: 10 }
  ]);
  const children = worktreeView([{ name: "main.go", path: "src/main.go", type: "file", size: 20 }]);
  const state = sampleState({
    expanded: new Set(["src"]),
    children: new Map([["src", children]])
  });
  const html = renderWorkTreeHTML(entries, state);
  assert.ok(html.includes("tree-count"));
  assert.ok(html.includes("main.go"));
  assert.ok(html.includes("--tree-depth:1"));
  assert.ok(!html.includes("tree-loading"));
});

test("shows loading spinner before children arrive and limit marker when truncated", () => {
  const entries = worktreeView([{ name: "big", path: "big", type: "dir", count: 0 }]);
  const loading = renderWorkTreeHTML(entries, sampleState({
    expanded: new Set(["big"]),
    loading: new Set(["big"])
  }));
  assert.ok(loading.includes("tree-loading"));

  const truncated = renderWorkTreeHTML(entries, sampleState({
    expanded: new Set(["big"]),
    children: new Map([["big", []]]),
    truncated: new Map([["big", true]])
  }));
  assert.ok(truncated.includes("已截断"));
});

test("renders error and empty states", () => {
  const withError = renderWorkTreeHTML([], sampleState({ error: "boom <bad>" }));
  assert.ok(withError.includes("worktree-error"));
  assert.ok(withError.includes("boom &lt;bad&gt;"));

  const empty = renderWorkTreeHTML([], sampleState());
  assert.ok(empty.includes("目录为空"));
});

test("formats file sizes", () => {
  assert.equal(formatSize(0), "");
  assert.equal(formatSize(512), "512 B");
  assert.equal(formatSize(2048), "2.0 KB");
  assert.equal(formatSize(5 * 1024 * 1024), "5.0 MB");
});
