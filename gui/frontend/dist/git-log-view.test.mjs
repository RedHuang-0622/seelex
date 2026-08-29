import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', '"data:text/javascript,export%20const%20renderMarkdown%3D(s)%3D%3Es%3B"');
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./git-log-view.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { gitLogView, renderGitLogHTML, createGitLogView } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

function commit(hash, short, author, date, subject) {
  return { hash, short_hash: short, author, date, subject };
}

test("gitLogView 归一化提交行与延续线", () => {
  const view = gitLogView({
    lines: [
      { graph: "*", commit: commit("aaaa", "a1b2", "Alice", "08-29 10:00", "feat: first") },
      { graph: "|\\", commit: null },
      { graph: "| *", commit: commit("bbbb", "c3d4", "Bob", "08-29 09:00", "side work") },
      { graph: "|/", commit: null }
    ],
    commits: [commit("aaaa", "a1b2", "Alice", "08-29 10:00", "feat: first")]
  });
  assert.equal(view.lines.length, 4);
  assert.equal(view.lines[0].graph, "*");
  assert.equal(view.lines[0].commit.subject, "feat: first");
  assert.equal(view.lines[1].graph, "|\\");
  assert.equal(view.lines[1].commit, null);
  assert.equal(view.lines[2].commit.subject, "side work");
  assert.equal(view.lines[3].commit, null);
  assert.equal(view.commits.length, 2); // 从行内提交重建索引
});

test("gitLogView 防御畸形载荷", () => {
  assert.deepEqual(gitLogView(null), { lines: [], commits: [], truncated: false, error: "", root: "" });
  assert.deepEqual(gitLogView({ lines: "nope" }), { lines: [], commits: [], truncated: false, error: "", root: "" });
  const view = gitLogView({ lines: [{ graph: 42, commit: null }, { graph: "*", commit: null }, null, "junk"], truncated: true, error: "not a git repository" });
  assert.equal(view.lines.length, 2); // 非法行丢弃
  assert.equal(view.lines[0].graph, "42");
  assert.equal(view.truncated, true);
  assert.equal(view.error, "not a git repository");
});

test("gitLogView 缺省提交字段回退为空串", () => {
  const view = gitLogView({ lines: [{ graph: "*", commit: { hash: "aaaa" } }] });
  assert.equal(view.lines[0].commit.shortHash, "");
  assert.equal(view.lines[0].commit.author, "");
  assert.equal(view.lines[0].commit.date, "");
  assert.equal(view.lines[0].commit.subject, "");
});

test("gitLogView graph 前缀超宽截断", () => {
  const wide = "|".repeat(60);
  const view = gitLogView({ lines: [{ graph: wide, commit: null }] });
  assert.ok(view.lines[0].graph.length <= 49);
  assert.ok(view.lines[0].graph.endsWith("…"));
});

test("renderGitLogHTML 全部文本 escape（防注入）", () => {
  const view = gitLogView({
    lines: [
      { graph: "*", commit: commit("aaaa", "a1b2", "<img src=x onerror=alert(1)>", "08-29", "fix <script>") },
      { graph: "|", commit: null }
    ],
    error: ""
  });
  const html = renderGitLogHTML(view);
  assert.ok(!html.includes("<script>"), "subject 必须被 escape");
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(!html.includes("<img src=x"), "author 必须被 escape");
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(html.includes("data-git-hash=\"aaaa\""), "hash 按钮携带完整 hash");
  assert.ok(html.includes("is-continuation"), "延续线有标记类");
});

test("renderGitLogHTML 截断提示", () => {
  const html = renderGitLogHTML({ lines: [], truncated: true });
  assert.ok(html.includes("已显示部分提交"));
});

test("createGitLogView 复制回调与点击绑定", () => {
  let copied = "";
  const container = {
    classList: { add() {}, remove() {} },
    innerHTML: "",
    querySelectorAll() { return []; }
  };
  const view = createGitLogView(container, { onCopy: async hash => { copied = hash; } });
  view.renderRoot({ lines: [{ graph: "*", commit: commit("aaaa", "a1b2", "Alice", "08-29", "subject") }] });
  assert.ok(container.innerHTML.includes("data-git-hash=\"aaaa\""));
  // 无 DOM 时不抛错（querySelectorAll 返回空）。
  assert.equal(copied, "");
});

test("createGitLogView 错误态渲染", () => {
  const container = { classList: { add() {}, remove() {} }, innerHTML: "", querySelectorAll() { return []; } };
  const view = createGitLogView(container);
  view.renderRoot({ error: "fatal: not a git repository" });
  assert.ok(container.innerHTML.includes("not a git repository"));
  assert.ok(!container.innerHTML.includes("&lt;") || !container.innerHTML.includes("<div"), "错误文案被 escape");
});
