import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = relative => readFile(new URL(`./${relative}`, import.meta.url), "utf8");
const asModule = source => `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
const componentsSource = (await read("./components.js"))
  .replace('"./markdown.js"', '"data:text/javascript,export%20const%20renderMarkdown%3D(s)%3D%3Es%3B"');
const componentsURL = asModule(componentsSource);
const source = (await read("workspace-changes.js"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { workspaceChangesView, renderWorkspaceChangesHTML, createWorkspaceChangesView, kindLabel, statusLetter } =
  await import(asModule(source));

function change(path, kind, extra = {}) {
  return { path, kind, status: " M", index: " ", worktree: "M", staged: false, ...extra };
}

// 无 DOM 的容器替身：只记录 innerHTML 与监听器（视图不依赖真实 DOM API）。
function fakeContainer() {
  return {
    innerHTML: "",
    listeners: [],
    classList: { add() {}, remove() {} },
    addEventListener(type, handler) { this.listeners.push({ type, handler }); }
  };
}

test("workspaceChangesView 归一化改动并拆出目录/文件名", () => {
  const view = workspaceChangesView({
    branch: "main",
    entries: [
      change("src/main.go", "modified"),
      change("notes.md", "untracked", { status: "??", index: "?", worktree: "?", staged: false }),
      change("sub/renamed.txt", "renamed", { status: "R ", index: "R", worktree: " ", staged: true, old_path: "sub/inner.txt" })
    ],
    total: 3,
    staged: 1,
    unstaged: 1,
    untracked: 1,
    root: "G:/repo"
  });
  assert.equal(view.branch, "main");
  assert.equal(view.entries.length, 3);
  assert.deepEqual(view.counts, { staged: 1, unstaged: 1, untracked: 1, conflicted: 0 });
  assert.equal(view.entries[0].dir, "src/");
  assert.equal(view.entries[0].name, "main.go");
  assert.equal(view.entries[1].dir, "");
  assert.equal(view.entries[1].name, "notes.md");
  assert.equal(view.entries[2].oldPath, "sub/inner.txt");
  assert.equal(view.entries[2].staged, true);
  assert.equal(view.root, "G:/repo");
});

test("workspaceChangesView 防御畸形载荷", () => {
  assert.deepEqual(workspaceChangesView(null), {
    entries: [],
    branch: "",
    counts: { staged: 0, unstaged: 0, untracked: 0, conflicted: 0 },
    total: 0,
    filtered: 0,
    truncated: false,
    error: "",
    root: ""
  });
  const view = workspaceChangesView({
    entries: [null, "junk", { kind: "modified" }, { path: "  " }, change("kept.txt", "added")],
    staged: -3,
    untracked: "not-a-number",
    truncated: true,
    error: "fatal: not a git repository"
  });
  assert.equal(view.entries.length, 1); // 非对象 / 缺 path / 空白 path 的行丢弃
  assert.equal(view.entries[0].path, "kept.txt");
  // 负数与非数字计数归零：不做"看起来像计数"的猜测。
  assert.deepEqual(view.counts, { staged: 0, unstaged: 0, untracked: 0, conflicted: 0 });
  assert.equal(view.total, 1); // 缺 total 时回落到实际条目数
  assert.equal(view.truncated, true);
  assert.equal(view.error, "fatal: not a git repository");
});

test("kindLabel / statusLetter 未知分类不猜语义", () => {
  assert.equal(kindLabel("modified"), "修改");
  assert.equal(kindLabel("untracked"), "未跟踪");
  assert.equal(kindLabel("who-knows"), "变更");
  assert.equal(statusLetter("conflicted"), "U");
  assert.equal(statusLetter("who-knows"), "");
});

test("renderWorkspaceChangesHTML 渲染统计行、状态字母与暂存标记", () => {
  const view = workspaceChangesView({
    branch: "feature/x",
    entries: [
      change("src/main.go", "modified"),
      change("sub/renamed.txt", "renamed", { staged: true, old_path: "sub/inner.txt" }),
      change("gone.txt", "deleted", { status: " D", worktree: "D" })
    ],
    staged: 1,
    unstaged: 2
  });
  const html = renderWorkspaceChangesHTML(view);
  assert.ok(html.includes('class="changes-summary"'), "统计行");
  assert.ok(html.includes("feature/x"));
  assert.ok(html.includes("暂存 1") && html.includes("未暂存 2"));
  assert.ok(html.includes('data-change-path="src/main.go"'), "行携带可打开路径");
  assert.ok(html.includes("changes-dir"), "目录淡显");
  assert.ok(html.includes("← sub/inner.txt"), "重命名原路径");
  assert.ok(html.includes("已暂存"));
  // 已删除的文件没有字节可读：不给可点按钮，但仍列出路径。
  assert.ok(!html.includes('data-change-path="gone.txt"'));
  assert.ok(html.includes("gone.txt"));
  assert.ok(html.includes("is-deleted"));
});

test("renderWorkspaceChangesHTML 全部文本 escape（防注入）", () => {
  const view = workspaceChangesView({
    branch: "<img src=x onerror=alert(1)>",
    entries: [change("<script>alert(1)</script>.txt", "modified", { old_path: "<b>old</b>" })]
  });
  const html = renderWorkspaceChangesHTML(view);
  assert.ok(!html.includes("<script>"));
  assert.ok(!html.includes("<img src=x"));
  assert.ok(!html.includes("<b>old</b>"));
  assert.ok(html.includes("&lt;script&gt;"));
});

test("renderWorkspaceChangesHTML 截断与过滤提示不静默", () => {
  const truncated = renderWorkspaceChangesHTML(
    workspaceChangesView({ entries: [change("a.txt", "modified")], truncated: true, filtered: 2 })
  );
  assert.ok(truncated.includes("条目过多，已截断"));
  assert.ok(truncated.includes("另有 2 条未展示"));
});

test("createWorkspaceChangesView 空/错误/正常三态与点击委托", () => {
  const container = fakeContainer();
  const opened = [];
  const view = createWorkspaceChangesView(container, { onOpenFile: entry => opened.push(entry) });

  view.renderRoot({ entries: [] });
  assert.ok(container.innerHTML.includes("工作区干净"), "空态不装作出错");

  view.renderRoot({ error: "fatal: not a git repository (or any of the parent directories): .git" });
  assert.ok(container.innerHTML.includes("not a git repository"));

  view.renderRoot({ entries: [change("src/main.go", "modified")] });
  assert.ok(container.innerHTML.includes('data-change-path="src/main.go"'));

  // 点击委托挂在容器上（一条监听）：命中 data-change-path 才回调。
  const handler = container.listeners[0].handler;
  assert.equal(container.listeners.length, 1);
  handler({ target: { closest: () => ({ dataset: { changePath: "src/main.go", changeName: "main.go" } }) } });
  assert.deepEqual(opened, [{ name: "main.go", path: "src/main.go", type: "file", size: 0, count: 0 }]);
  handler({ target: { closest: () => null } });
  assert.equal(opened.length, 1, "未命中行不触发回调");

  view.reset();
  assert.equal(container.innerHTML, "");
  assert.equal(view.current(), null);
});
