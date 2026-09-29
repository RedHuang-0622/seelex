import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = relative => readFile(new URL(`./${relative}`, import.meta.url), "utf8");
const asModule = source => `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
const componentsSource = (await read("./components.js"))
  .replace('"./markdown.js"', '"data:text/javascript,export%20const%20renderMarkdown%3D(s)%3D%3Es%3B"');
const componentsURL = asModule(componentsSource);
const treeForkURL = asModule(await read("tree-fork.js"));
// 提交详情/文件内容两层视图依赖的两个模块：各自用最小替身。
//   - file-preview：只替"渲染入口"（真实现由 file-preview.test.mjs 覆盖），这样本
//     文件能断言"面板把字节交给了共用渲染器"，而不是在假 DOM 里重造一套渲染；
//   - workspace-changes：Kind → 中文标签的唯一词表在那里，这里只钉"标签从那里取"。
const filePreviewStub = asModule(`
export const PREVIEW_LIMITS = { markdown: 111, code: 222, text: 333, unsupported: 444 };
export function previewKindForPath(path) {
  return String(path).endsWith(".md") ? "markdown" : "code";
}
export function renderNotice(panel, message) {
  globalThis.__gitLogNotices.push({ panel, message });
}
export async function renderReadOnlyContent(panel, payload, options) {
  globalThis.__gitLogRenders.push({ panel, payload, options });
  return options?.kind || "";
}
`);
const workspaceChangesStub = asModule('export function kindLabel(kind) { return `K:${kind}`; }');
const source = (await read("./git-log-view.js"))
  .replace('"./components.js"', `"${componentsURL}"`)
  .replace('"./tree-fork.js"', `"${treeForkURL}"`)
  .replace('"./file-preview.js"', `"${filePreviewStub}"`)
  .replace('"./workspace-changes.js"', `"${workspaceChangesStub}"`);
const {
  gitLogView,
  gitCommitDetailView,
  renderGitLogHTML,
  renderGitCommitDetailHTML,
  commitFileStatText,
  createGitLogView
} = await import(asModule(source));

function commit(hash, short, author, date, subject, parents = []) {
  return { hash, short_hash: short, author, date, subject, parents };
}

function fileEntry(path, extra = {}) {
  return {
    path,
    kind: "modified",
    status: "M",
    letter: "M",
    additions: 1,
    deletions: 0,
    ...extra
  };
}

// ── 列表层（原有行为不能退化）──────────────────────────────

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

test("renderGitLogHTML 提交行与标题都能下钻（鼠标整行 / 键盘按钮）", () => {
  const html = renderGitLogHTML(gitLogView({ commits: [commit("aaaa", "a1b2", "Alice", "08-29", "feat")] }));
  assert.ok(html.includes('class="git-log-line" data-git-commit="aaaa"'), "整行可点");
  assert.ok(html.includes('<button type="button" class="git-log-subject" data-git-commit="aaaa"'), "标题是可聚焦按钮");
});

test("renderGitLogHTML 截断与泳道上限提示", () => {
  const truncated = renderGitLogHTML({ commits: [], graph: { rows: [], laneCount: 1, dropped: 0 }, truncated: true });
  assert.ok(truncated.includes("已显示部分提交"));
  const capped = renderGitLogHTML({ commits: [], graph: { rows: [], laneCount: 8, dropped: 3 }, truncated: false });
  assert.ok(capped.includes("分支过多"));
});

// ── 提交详情层 ─────────────────────────────────────────────

test("gitCommitDetailView 归一化提交详情", () => {
  const view = gitCommitDetailView({
    hash: "aaaa",
    short_hash: "a1b2",
    author: "Alice",
    date: "08-29 10:00",
    subject: "feat: detail",
    files: [
      fileEntry("src/main.go"),
      null,
      { path: "" },
      fileEntry("docs/guide.md", { kind: "added", status: "A", letter: "A", additions: 20 }),
      fileEntry("gone.txt", { kind: "deleted", status: "D", letter: "D", additions: 0, deletions: 3 }),
      fileEntry("new name.txt", { kind: "renamed", status: "R062", letter: "R", old_path: "old name.txt", additions: 1 }),
      fileEntry("bin.dat", { kind: "modified", binary: true, additions: 0, deletions: 0 })
    ],
    total: 30,
    filtered: 2,
    truncated: true,
    root: "G:/repo"
  });
  assert.equal(view.hash, "aaaa");
  assert.equal(view.shortHash, "a1b2");
  assert.equal(view.total, 30);
  assert.equal(view.filtered, 2);
  assert.equal(view.truncated, true);
  assert.equal(view.error, "");
  assert.equal(view.root, "G:/repo");
  // 缺 path / 非对象的条目丢弃，其余保序。
  assert.deepEqual(view.files.map(item => item.path),
    ["src/main.go", "docs/guide.md", "gone.txt", "new name.txt", "bin.dat"]);
  const renamed = view.files[3];
  assert.equal(renamed.oldPath, "old name.txt");
  assert.equal(renamed.dir, "");
  assert.equal(renamed.previewable, true);
  // 删除的条目没有字节可读 → 不可下钻；二进制标记保留。
  assert.equal(view.files[2].previewable, false);
  assert.equal(view.files[4].binary, true);
  // 目录与文件名拆开（展示时目录淡显）。
  assert.equal(gitCommitDetailView({ files: [fileEntry("a/b/c.go")] }).files[0].dir, "a/b/");
  assert.equal(gitCommitDetailView({ files: [fileEntry("a/b/c.go")] }).files[0].name, "c.go");
});

test("gitCommitDetailView 防御畸形载荷与缺省计数", () => {
  assert.deepEqual(gitCommitDetailView(null), {
    hash: "", shortHash: "", author: "", date: "", subject: "",
    files: [], total: 0, filtered: 0, truncated: false, error: "", root: ""
  });
  assert.deepEqual(gitCommitDetailView([1, 2]), gitCommitDetailView(undefined));
  const view = gitCommitDetailView({ files: [fileEntry("a.txt")], error: "fatal: not a git repository" });
  // 文件数缺失时退回实际条目数；负数/非数归零。
  assert.equal(view.total, 1);
  assert.equal(view.filtered, 0);
  assert.equal(view.error, "fatal: not a git repository");
  assert.equal(gitCommitDetailView({ files: [fileEntry("a.txt")], total: -5, filtered: "x" }).total, 1);
});

test("renderGitCommitDetailHTML 渲染头部、文件清单与行数", () => {
  const detail = gitCommitDetailView({
    hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    short_hash: "a1b2c3d",
    author: "Alice",
    date: "08-29 10:00",
    subject: "feat: <b>detail</b>",
    files: [
      fileEntry("src/main.go", { additions: 3, deletions: 1 }),
      fileEntry("docs/guide.md", { kind: "added", status: "A", letter: "A", additions: 20, deletions: 0 }),
      fileEntry("new name.txt", { kind: "renamed", status: "R062", letter: "R", old_path: "old name.txt", additions: 1, deletions: 0 }),
      fileEntry("gone.txt", { kind: "deleted", status: "D", letter: "D", additions: 0, deletions: 3 }),
      fileEntry("bin.dat", { kind: "modified", binary: true, additions: 0, deletions: 0 })
    ],
    total: 5
  });
  const html = renderGitCommitDetailHTML(detail);
  assert.ok(html.includes('class="git-back" data-git-back="list"'), "返回提交列表");
  assert.ok(html.includes("← 提交列表"));
  assert.ok(html.includes("&lt;b&gt;detail&lt;/b&gt;"), "提交标题 escape");
  assert.ok(!html.includes("<b>detail</b>"));
  assert.ok(html.includes('data-git-hash="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"'), "完整 hash 在复制按钮上");
  assert.ok(html.includes("a1b2c3d") && html.includes("Alice") && html.includes("5 个文件"));
  // 可下钻的行：路径按钮带 hash + 路径两个凭据。
  assert.ok(html.includes('data-git-file="src/main.go" data-git-commit="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"'));
  assert.ok(html.includes(">M</span>"), "状态字母来自后端");
  assert.ok(html.includes("K:modified"), "中文标签来自共用词表（workspace-changes.kindLabel）");
  assert.ok(html.includes("+3 -1"));
  assert.ok(html.includes("+20"), "只增不减时只显示 +");
  assert.ok(html.includes("← old name.txt"), "重命名带原路径");
  // 删除的行不是按钮（没有字节可读）：不可下钻，且说明原因。
  assert.ok(html.includes('class="git-commit-path is-plain"'), "删除行不可点");
  assert.ok(!html.includes('data-git-file="gone.txt"'));
  assert.ok(html.includes("该提交里已删除，无内容可看"));
  assert.ok(html.includes("二进制"), "二进制文件不显示 +0 -0");
  assert.ok(!html.includes("+0 -0"));
});

test("renderGitCommitDetailHTML 渲染空清单、截断/过滤提示与错误态", () => {
  const empty = renderGitCommitDetailHTML(gitCommitDetailView({ hash: "aaaa", total: 0 }));
  assert.ok(empty.includes("这次提交没有改动文件"));

  const noisy = renderGitCommitDetailHTML(gitCommitDetailView({
    hash: "aaaa",
    files: [fileEntry("a.txt")],
    total: 4,
    truncated: true,
    filtered: 2
  }));
  assert.ok(noisy.includes("文件过多，已截断"));
  assert.ok(noisy.includes("另有 2 条未展示"));
  assert.ok(noisy.includes("4 个文件"), "头部说的是过滤后的全部文件数");

  const failed = renderGitCommitDetailHTML(gitCommitDetailView({ error: "fatal: <not a repo>" }));
  assert.ok(failed.includes("fatal: &lt;not a repo&gt;"));
  assert.ok(!failed.includes("<not a repo>"));
  assert.ok(failed.includes('data-git-back="list"'), "错误态仍能回到列表");
});

test("renderGitCommitDetailHTML 路径与标题全部 escape", () => {
  const html = renderGitCommitDetailHTML(gitCommitDetailView({
    hash: "aaaa",
    subject: "evil",
    files: [fileEntry('<img src=x onerror=alert(1)>.go', { old_path: "<script>" })]
  }));
  assert.ok(!html.includes("<img src=x"), "路径必须被 escape");
  assert.ok(!html.includes("<script>"), "原路径必须被 escape");
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;.go"));
});

test("commitFileStatText 只显示非零方向，二进制直说", () => {
  assert.equal(commitFileStatText({ additions: 3, deletions: 1 }), "+3 -1");
  assert.equal(commitFileStatText({ additions: 3, deletions: 0 }), "+3");
  assert.equal(commitFileStatText({ additions: 0, deletions: 4 }), "-4");
  assert.equal(commitFileStatText({ additions: 0, deletions: 0 }), "");
  assert.equal(commitFileStatText({ binary: true, additions: 0, deletions: 0 }), "二进制");
});

// ── 控制器：三层下钻 ───────────────────────────────────────

function fakeClassList() {
  const names = new Set();
  return {
    add: (...list) => list.forEach(name => names.add(name)),
    remove: (...list) => list.forEach(name => names.delete(name)),
    has: name => names.has(name)
  };
}

function fakeContainer() {
  const listeners = new Map();
  const body = { tag: "file-body", innerHTML: "", classList: fakeClassList() };
  return {
    classList: fakeClassList(),
    innerHTML: "",
    body,
    addEventListener(type, handler) {
      listeners.set(type, handler);
    },
    querySelector(selector) {
      return selector === ".git-file-body" ? body : null;
    },
    // click 按"哪个选择器命中"投递一次委托点击，返回监听器的 promise（异步层可等）。
    click(selector, dataset = {}) {
      const handler = listeners.get("click");
      assert.ok(handler, "容器上必须挂着点击委托");
      return handler({ target: { closest: candidate => (candidate === selector ? { dataset } : null) } });
    }
  };
}

const flush = () => new Promise(resolve => setTimeout(resolve, 0));

test("createGitLogView 点开提交 → 文件清单 → 文件内容，返回键逐层退回", async () => {
  globalThis.__gitLogRenders = [];
  globalThis.__gitLogNotices = [];
  const container = fakeContainer();
  const loadedCommits = [];
  const loadedFiles = [];
  const view = createGitLogView(container, {
    loadCommit: async (hash, limit) => {
      loadedCommits.push({ hash, limit });
      return {
        hash,
        short_hash: "a1b2",
        author: "Alice",
        date: "08-29 10:00",
        subject: "feat: detail",
        files: [fileEntry("src/main.go", { additions: 3, deletions: 1 })],
        total: 1
      };
    },
    loadFile: async (hash, path, limit) => {
      loadedFiles.push({ hash, path, limit });
      return { path, size: 4, truncated: false, text_like: true, base64: "Y29kZQ==" };
    }
  });
  view.renderRoot({ commits: [commit("aaaa", "a1b2", "Alice", "08-29", "feat: first")] });
  assert.ok(container.innerHTML.includes("git-log-line"), "起始在列表层");

  await container.click("[data-git-commit]", { gitCommit: "aaaa" });
  assert.deepEqual(loadedCommits, [{ hash: "aaaa", limit: 200 }], "展示预算显式下发");
  assert.ok(container.innerHTML.includes("git-commit-files"), "第二层是文件清单");
  assert.ok(container.innerHTML.includes("← 提交列表"));
  assert.ok(container.innerHTML.includes('data-git-file="src/main.go"'));

  await container.click("[data-git-file]", { gitFile: "src/main.go", gitCommit: "aaaa" });
  // 限额按预览类型取自共用表（stub 里 code=222）。
  assert.deepEqual(loadedFiles, [{ hash: "aaaa", path: "src/main.go", limit: 222 }]);
  assert.ok(container.innerHTML.includes("← 文件清单"), "第三层是文件内容");
  assert.ok(container.innerHTML.includes("git-file-body"));
  assert.ok(container.innerHTML.includes("M K:modified"), "文件头带上那一行的状态（字母 + 共用词表标签）");
  assert.equal(globalThis.__gitLogRenders.length, 1, "正文交给共用只读渲染器");
  assert.equal(globalThis.__gitLogRenders[0].panel, container.body);
  assert.equal(globalThis.__gitLogRenders[0].payload.base64, "Y29kZQ==");
  assert.equal(globalThis.__gitLogRenders[0].options.path, "src/main.go");
  assert.equal(globalThis.__gitLogRenders[0].options.kind, "code");

  container.click("[data-git-back]");
  assert.ok(container.innerHTML.includes("git-commit-files"), "退回文件清单");
  container.click("[data-git-back]");
  assert.ok(container.innerHTML.includes("git-log-line"), "再退回提交列表");
});

test("createGitLogView 迟到的下钻结果不覆盖新画面", async () => {
  const container = fakeContainer();
  let releaseFirst = null;
  const view = createGitLogView(container, {
    loadCommit: hash => {
      if (hash === "aaaa") return new Promise(resolve => { releaseFirst = () => resolve({ hash, subject: "first" }); });
      return Promise.resolve({ hash, subject: "second", files: [fileEntry("b.txt")], total: 1 });
    },
    loadFile: async () => ({ base64: "", size: 0, truncated: false, text_like: true })
  });
  view.renderRoot({ commits: [commit("aaaa", "a1", "A", "08-29", "one"), commit("bbbb", "b1", "B", "08-29", "two")] });

  const first = container.click("[data-git-commit]", { gitCommit: "aaaa" });
  await container.click("[data-git-commit]", { gitCommit: "bbbb" });
  assert.equal(view.detail().hash, "bbbb");
  const snapshot = container.innerHTML;
  releaseFirst();
  await first;
  await flush();
  assert.equal(view.detail().hash, "bbbb", "先点的那次不能把后点的挤掉");
  assert.equal(container.innerHTML, snapshot);
});

test("createGitLogView 下钻失败渲染成错误块（不外抛、不弹 toast）", async () => {
  const container = fakeContainer();
  const view = createGitLogView(container, {
    loadCommit: async () => ({ error: "fatal: not a git repository" }),
    loadFile: async () => {
      throw new Error("worktree: no workspace bound to current session");
    }
  });
  view.renderRoot({ commits: [commit("aaaa", "a1", "A", "08-29", "one")] });

  await container.click("[data-git-commit]", { gitCommit: "aaaa" });
  assert.ok(container.innerHTML.includes("git-commit-error"));
  assert.ok(container.innerHTML.includes("not a git repository"));

  // 详情里没有文件行（error 载荷）→ 直接给一份可下钻的清单再点文件。
  const fileView = createGitLogView(container, {
    loadCommit: async () => ({ hash: "aaaa", subject: "x", files: [fileEntry("a.go")], total: 1 }),
    loadFile: async () => {
      throw new Error("worktree: no workspace bound to current session");
    }
  });
  fileView.renderRoot({ commits: [commit("aaaa", "a1", "A", "08-29", "one")] });
  await container.click("[data-git-commit]", { gitCommit: "aaaa" });
  await container.click("[data-git-file]", { gitFile: "a.go", gitCommit: "aaaa" });
  assert.ok(container.innerHTML.includes("git-commit-error"));
  assert.ok(container.innerHTML.includes("no workspace bound"));
});

test("createGitLogView 没有下钻回调时不产生死链接", async () => {
  const container = fakeContainer();
  const view = createGitLogView(container); // 未接 loadCommit/loadFile
  view.renderRoot({ commits: [commit("aaaa", "a1", "A", "08-29", "one")] });
  await container.click("[data-git-commit]", { gitCommit: "aaaa" });
  assert.equal(view.detail(), null, "没有加载器就不进详情层");
  assert.ok(container.innerHTML.includes("git-log-line"));
});

test("createGitLogView 根刷新不把用户从已打开的提交里踢出去，reset 才清空", async () => {
  const container = fakeContainer();
  const view = createGitLogView(container, {
    loadCommit: async hash => ({ hash, subject: "detail", files: [], total: 0 }),
    loadFile: async () => ({ base64: "", size: 0, truncated: false, text_like: true })
  });
  view.renderRoot({ commits: [commit("aaaa", "a1", "A", "08-29", "one")] });
  await container.click("[data-git-commit]", { gitCommit: "aaaa" });
  assert.ok(container.innerHTML.includes("← 提交列表"));

  view.renderRoot({ commits: [commit("bbbb", "b1", "B", "08-29", "two")] });
  assert.equal(view.detail().hash, "aaaa", "列表数据换代不等于离开详情");

  view.reset();
  assert.equal(view.detail(), null);
  assert.equal(view.file(), null);
  assert.equal(view.current(), null);
  assert.equal(container.innerHTML, "");
  assert.ok(container.classList.has("muted"));
});

test("createGitLogView 复制回调与错误态渲染", async () => {
  let copied = "";
  const container = fakeContainer();
  const view = createGitLogView(container, { onCopy: async hash => { copied = hash; } });
  view.renderRoot({ commits: [commit("aaaa", "a1b2", "Alice", "08-29", "subject")] });
  assert.ok(container.innerHTML.includes("data-git-hash=\"aaaa\""));
  await container.click("[data-git-hash]", { gitHash: "aaaa" });
  assert.equal(copied, "aaaa");

  view.renderRoot({ error: "fatal: not a git repository" });
  assert.ok(container.innerHTML.includes("not a git repository"));
  assert.ok(!container.innerHTML.includes("&lt;") || !container.innerHTML.includes("<div"), "错误文案被 escape");
});
