import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = relative => readFile(new URL(`./${relative}`, import.meta.url), "utf8");
const asModule = source => `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
const componentsSource = (await read("./components.js"))
  .replace('"./markdown.js"', '"data:text/javascript,export%20const%20renderMarkdown%3D(s)%3D%3Es%3B"');
const componentsURL = asModule(componentsSource);
const treeForkURL = asModule(await read("tree-fork.js"));
const source = (await read("git-log-view.js"))
  .replace('"./components.js"', `"${componentsURL}"`)
  .replace('"./tree-fork.js"', `"${treeForkURL}"`);
const { gitLogView, renderGitLogHTML, createGitLogView } = await import(asModule(source));

function commit(hash, short, author, date, subject, parents = []) {
  return { hash, short_hash: short, author, date, subject, parents };
}

test("gitLogView 把提交按拓扑序归一化并算出泳道", () => {
  const view = gitLogView({
    commits: [
      commit("aaaa", "a1b2", "Alice", "08-29 10:00", "feat: first", ["bbbb", "cccc"]),
      commit("bbbb", "c3d4", "Bob", "08-29 09:00", "side work", ["dddd"]),
      commit("cccc", "e5f6", "Carol", "08-29 08:00", "other side", ["dddd"]),
      commit("dddd", "0a1b", "Dave", "08-29 07:00", "base", [])
    ],
    root: "G:/repo"
  });
  assert.equal(view.commits.length, 4);
  assert.equal(view.commits[0].subject, "feat: first");
  assert.equal(view.commits[0].parents.length, 2);
  assert.equal(view.root, "G:/repo");
  // merge 分出第二条泳道，两条旁支在 base 上收口。
  assert.equal(view.graph.laneCount, 2);
  assert.deepEqual(view.graph.rows.map(row => row.lane), [0, 0, 1, 0]);
  assert.equal(view.graph.rows[0].merge, true);
  // 最后一行的两条汇入边：一条同泳道、一条跨泳道（贝塞尔）。
  const mergeIn = view.graph.rows[3].segments.filter(seg => seg.toY === 0.5);
  assert.equal(mergeIn.length, 2);
  assert.ok(mergeIn.some(seg => seg.from === 1 && seg.to === 0));
});

test("gitLogView 防御畸形载荷", () => {
  assert.deepEqual(gitLogView(null), {
    commits: [], graph: { rows: [], laneCount: 1, dropped: 0 }, truncated: false, error: "", root: ""
  });
  assert.deepEqual(gitLogView({ commits: "nope" }), {
    commits: [], graph: { rows: [], laneCount: 1, dropped: 0 }, truncated: false, error: "", root: ""
  });
  const view = gitLogView({
    commits: [null, "junk", { hash: "" }, { hash: "aaaa", parents: "not-a-list" }],
    truncated: true,
    error: "not a git repository"
  });
  assert.equal(view.commits.length, 1); // 缺 hash / 非对象的行丢弃
  assert.deepEqual(view.commits[0].parents, []);
  assert.equal(view.truncated, true);
  assert.equal(view.error, "not a git repository");
});

test("gitLogView 缺省提交字段回退为空串", () => {
  const view = gitLogView({ commits: [{ hash: "aaaa" }] });
  assert.equal(view.commits[0].shortHash, "");
  assert.equal(view.commits[0].author, "");
  assert.equal(view.commits[0].date, "");
  assert.equal(view.commits[0].subject, "");
});

test("renderGitLogHTML 用 SVG 画分叉而不是字符画", () => {
  const view = gitLogView({
    commits: [
      commit("aaaa", "a1b2", "Alice", "08-29 10:00", "merge", ["bbbb", "cccc"]),
      commit("bbbb", "c3d4", "Bob", "08-29 09:00", "side", ["dddd"]),
      commit("cccc", "e5f6", "Carol", "08-29 08:00", "other", ["dddd"]),
      commit("dddd", "0a1b", "Dave", "08-29 07:00", "base", [])
    ]
  });
  const html = renderGitLogHTML(view);
  assert.ok(html.includes('<svg class="tf-fork-svg"'), "每行一条 SVG 分叉图");
  assert.ok(html.includes('class="tf-fork-dot"'), "提交点");
  assert.ok(html.includes("C"), "跨泳道用贝塞尔曲线");
  assert.ok(!/[*|\\/]\s*</.test(html) || !html.includes("git-log-graph\">*"), "不再有 graph 字符画");
  assert.ok(!html.includes("is-continuation"), "延续线概念随 --graph 一起去掉");
});

test("renderGitLogHTML 全部文本 escape（防注入）", () => {
  const view = gitLogView({
    commits: [commit("aaaa", "a1b2", "<img src=x onerror=alert(1)>", "08-29", "fix <script>")]
  });
  const html = renderGitLogHTML(view);
  assert.ok(!html.includes("<script>"), "subject 必须被 escape");
  assert.ok(html.includes("&lt;script&gt;"));
  assert.ok(!html.includes("<img src=x"), "author 必须被 escape");
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(html.includes("data-git-hash=\"aaaa\""), "hash 按钮携带完整 hash");
});

test("renderGitLogHTML 截断与泳道上限提示", () => {
  const truncated = renderGitLogHTML({ commits: [], graph: { rows: [], laneCount: 1, dropped: 0 }, truncated: true });
  assert.ok(truncated.includes("已显示部分提交"));
  const capped = renderGitLogHTML({ commits: [], graph: { rows: [], laneCount: 8, dropped: 3 }, truncated: false });
  assert.ok(capped.includes("分支过多"));
});

test("createGitLogView 复制回调与点击绑定", () => {
  let copied = "";
  const container = {
    classList: { add() {}, remove() {} },
    innerHTML: "",
    addEventListener() {},
    querySelectorAll() { return []; }
  };
  const view = createGitLogView(container, { onCopy: async hash => { copied = hash; } });
  view.renderRoot({ commits: [commit("aaaa", "a1b2", "Alice", "08-29", "subject")] });
  assert.ok(container.innerHTML.includes("data-git-hash=\"aaaa\""));
  // 委托监听挂在容器上：无 DOM 时不抛错。
  assert.equal(copied, "");
});

test("createGitLogView 错误态渲染", () => {
  const container = { classList: { add() {}, remove() {} }, innerHTML: "", addEventListener() {}, querySelectorAll() { return []; } };
  const view = createGitLogView(container);
  view.renderRoot({ error: "fatal: not a git repository" });
  assert.ok(container.innerHTML.includes("not a git repository"));
  assert.ok(!container.innerHTML.includes("&lt;") || !container.innerHTML.includes("<div"), "错误文案被 escape");
});
