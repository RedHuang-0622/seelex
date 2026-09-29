import { escapeHtml } from "./components.js";
import {
  PREVIEW_LIMITS,
  previewKindForPath,
  renderNotice,
  renderReadOnlyContent
} from "./file-preview.js";
import { MAX_FORK_LANES, commitGraphRowHTML, forkGraphWidth, layoutCommitGraph } from "./tree-fork.js";
import { kindLabel } from "./workspace-changes.js";

// ── 提交记录（Git Log）视图 ─────────────────────────────
// 数据源：Bridge.WorkspaceGitLog(limit)（后端权威只读元数据：按 git 拓扑序
// （新 → 旧）的提交行 + 每个提交的父提交 hash；不含 diff 或文件内容）。
//
// 三层视图，共用一个容器与一条容器委托监听：
//   1. 提交列表（本模块原有面）：泳道分叉 + 短 hash / 时间 / 标题；
//   2. 提交详情：点开某一条提交 → 这次提交改了哪些文件（状态字母 / 路径 /
//      重命名原路径 / ±行数）。数据源 Bridge.WorkspaceGitCommitDetail；
//   3. 文件内容：点开清单里的一行 → 该文件**在那个提交时**的内容。
//      数据源 Bridge.WorkspaceGitCommitFileContent（git 对象库，只读）。
//
// 渲染策略：分支拓扑不再贴 `git --graph` 的字符画（`* | \ /` 既撑不出真实
// 分叉，也不能随皮肤换色），而是把 parents 算成泳道
// （tree-fork.layoutCommitGraph），逐行用一条 SVG 画直线/合并贝塞尔 + 提交点。
// 提交行展示 短 hash（可点击复制完整 hash）/ 时间 / 标题；全部文本 escape。
//
// 为什么文件内容不塞进「文件详情」抽屉：那个抽屉按路径认身份、且带编辑面（Ctrl+S
// 直接写工作区文件）。历史版本与工作区同名文件在抽屉里是同一枚 chip（会串台），
// 更糟的是一条保存就能把某个历史版本写进工作区。历史版本是另一种来源，它只读、
// 也只在本面板里看——渲染分派则与抽屉共用 file-preview.renderReadOnlyContent，
// "怎么渲染一份字节"仍然只有一处实现。

// MAX_FORK_LANES 之外的泳道不再画线（记 dropped，前端给一行提示）。
const LANE_WIDTH = 16;
const ROW_HEIGHT = 22;
// COMMIT_FILES_LIMIT 是提交详情一次列出的改动条数（展示预算：面板一屏翻不到
// 200 条；后端另有 1000 的硬钳制与解析预算）。
const COMMIT_FILES_LIMIT = 200;

// gitLogView 归一化 git log 结果（防御畸形载荷：非对象 → 空；commits 非数组
// → []；行内缺 hash 的条目丢弃；parents 只保留字符串）。
export function gitLogView(result) {
  if (!result || typeof result !== "object") {
    return { commits: [], graph: emptyGraph(), truncated: false, error: "", root: "" };
  }
  const commits = (Array.isArray(result.commits) ? result.commits : [])
    .map(normalizeCommit)
    .filter(Boolean);
  return {
    commits,
    // 泳道布局只吃 id/parents（纯函数，见 tree-fork.js）。
    graph: layoutCommitGraph(commits.map(item => ({ id: item.hash, parents: item.parents })), { maxLanes: MAX_FORK_LANES }),
    truncated: Boolean(result.truncated),
    error: textValue(result.error),
    root: textValue(result.root)
  };
}

// gitCommitDetailView 归一化提交详情（防御畸形载荷：非对象 → 空；files 非数组
// → []；缺 path 的条目丢弃；计数取有限整数）。
export function gitCommitDetailView(result) {
  if (!result || typeof result !== "object" || Array.isArray(result)) return emptyDetail();
  const files = (Array.isArray(result.files) ? result.files : [])
    .map(normalizeCommitFile)
    .filter(Boolean);
  return {
    hash: textValue(result.hash),
    shortHash: textValue(result.short_hash),
    author: textValue(result.author),
    date: textValue(result.date),
    subject: textValue(result.subject),
    files,
    // 头部说的是"这次提交改了多少个文件"（含被 limit 截断的部分），不是本屏行数。
    total: finiteCount(result.total) || files.length,
    filtered: finiteCount(result.filtered),
    truncated: Boolean(result.truncated),
    error: textValue(result.error),
    root: textValue(result.root)
  };
}

// createGitLogView 创建视图实例：持有数据面（根加载后缓存）+ 复制回调 +
// 两层下钻的加载回调（loadCommit/loadFile 缺省时对应层不可下钻，行不做成按钮）。
//
// 全部点击走容器委托（一条监听）：三层视图每次整块重绘，逐行绑监听会留下孤儿闭包。
export function createGitLogView(container, options = {}) {
  let current = null;
  // detail: { hash, loading, error, data }；file: { hash, path, kind, loading, error, payload }
  let detail = null;
  let file = null;
  // loadToken 是下钻代次：换层/返回/根重置都递增，迟到的异步结果一律丢弃
  // （否则"点开 A 又马上点 B"会让 A 的结果覆盖 B 的画面）。
  let loadToken = 0;
  // cleanups 收集当前文件内容视图的资源回收任务（blob URL、PDF 渲染等）。
  let cleanups = [];

  const copyHash = options.onCopy || (async hash => {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(hash);
    }
  });
  const loadCommit = typeof options.loadCommit === "function" ? options.loadCommit : null;
  const loadFile = typeof options.loadFile === "function" ? options.loadFile : null;

  container?.addEventListener("click", async event => {
    // 返回键优先：详情/内容两层都靠它回上一层。
    if (event.target?.closest?.("[data-git-back]")) {
      if (file) closeFile();
      else closeDetail();
      return;
    }
    const hashButton = event.target?.closest?.("[data-git-hash]");
    if (hashButton) {
      const hash = hashButton.dataset.gitHash || "";
      try {
        await copyHash(hash);
        hashButton.classList.add("is-copied");
        window.setTimeout(() => hashButton.classList.remove("is-copied"), 900);
      } catch (error) {
        // 复制失败静默：hash 已展示，不影响视图。
      }
      return;
    }
    const fileRow = event.target?.closest?.("[data-git-file]");
    if (fileRow) {
      // 返回加载 promise：调用方（测试/宿主）可以等这一层真正落定。
      return openFile(fileRow.dataset.gitCommit || "", fileRow.dataset.gitFile || "");
    }
    const commitRow = event.target?.closest?.("[data-git-commit]");
    if (commitRow) return openCommit(commitRow.dataset.gitCommit || "");
  });

  function runCleanups() {
    for (const task of cleanups.splice(0)) {
      try {
        task();
      } catch (error) {
        // 回收失败不影响视图切换。
      }
    }
  }

  function addCleanup(task) {
    if (typeof task === "function") cleanups.push(task);
  }

  function render() {
    if (!container) return;
    container.classList.remove("muted");
    container.classList.add("git-log-view");
    if (file) {
      renderFileLevel();
      return;
    }
    if (detail) {
      renderDetailLevel();
      return;
    }
    if (!current) {
      container.innerHTML = '<div class="git-log-empty">暂无提交记录</div>';
      return;
    }
    if (current.error) {
      container.innerHTML = `<div class="git-log-error">${escapeHtml(current.error)}</div>`;
      return;
    }
    if (current.commits.length === 0) {
      container.innerHTML = '<div class="git-log-empty">仓库暂无提交</div>';
      return;
    }
    container.innerHTML = renderGitLogHTML(current);
  }

  // renderDetailLevel 渲染「这次提交改了哪些文件」：加载态是头部 + 忙碌行，落定后
  // 整块交给 renderGitCommitDetailHTML（正常态与错误态同一份纯渲染，视图不重写一遍）。
  function renderDetailLevel() {
    const view = detail.data;
    if (detail.loading) {
      container.innerHTML = `${renderDetailHead(detail.hash, null)}<div class="git-commit-busy">正在读取提交详情…</div>`;
      return;
    }
    container.innerHTML = renderGitCommitDetailHTML(view || { hash: detail.hash, error: detail.error });
  }

  // renderFileLevel 渲染「某文件在那个提交时的内容」：返回键 + 文件身份 + 正文
  // 容器。正文由共用的只读渲染器写进 .git-file-body（异步分派 markdown/代码/
  // 图片/PDF…），由 openFile 在写完之后调用 renderFileBody——本函数保持同步，
  // 渲染位置只有一处（不在两条路径上各写一遍）。
  function renderFileLevel() {
    const entry = (detail?.data?.files || []).find(item => item.path === file.path) || null;
    const head = renderFileHead(file, entry);
    if (file.loading || !file.payload) {
      container.innerHTML = `${head}${file.loading
        ? '<div class="git-commit-busy">正在读取文件内容…</div>'
        : renderErrorBlock(file.error)}`;
      return;
    }
    container.innerHTML = `${head}<div class="git-file-body"></div>`;
  }

  async function renderFileBody(token) {
    const body = container?.querySelector?.(".git-file-body");
    if (!body || !file?.payload) return;
    try {
      await renderReadOnlyContent(body, file.payload, {
        path: file.path,
        kind: file.kind,
        isCurrent: () => token === loadToken && Boolean(file),
        addCleanup
      });
    } catch (error) {
      if (token !== loadToken) return;
      renderNotice(body, `无法预览：${error?.message || String(error)}`);
    }
  }

  async function openCommit(hash) {
    if (!loadCommit || !hash) return;
    const token = ++loadToken;
    runCleanups();
    file = null;
    detail = { hash, loading: true, error: "", data: null };
    render();
    let payload = null;
    let failure = "";
    try {
      payload = await loadCommit(hash, COMMIT_FILES_LIMIT);
    } catch (error) {
      failure = error?.message || String(error);
    }
    if (token !== loadToken) return;
    const view = payload ? gitCommitDetailView(payload) : null;
    detail = { hash, loading: false, error: failure || view?.error || "", data: view };
    render();
  }

  async function openFile(hash, path) {
    if (!loadFile || !hash || !path) return;
    const token = ++loadToken;
    runCleanups();
    const kind = previewKindForPath(path);
    file = { hash, path, kind, loading: true, error: "", payload: null };
    render();
    let payload = null;
    let failure = "";
    try {
      // 限额按预览类型给（与文件详情抽屉同一张表）：后端还有自己的硬上限。
      payload = await loadFile(hash, path, PREVIEW_LIMITS[kind] || PREVIEW_LIMITS.text);
    } catch (error) {
      failure = error?.message || String(error);
    }
    if (token !== loadToken) return;
    file = { hash, path, kind, loading: false, error: failure, payload };
    render();
    // 正文渲染是异步的（图片/PDF/Word 还要等组件），等它落定再让调用方继续：
    // 「点开一个文件」这件事的完成点是内容真的画出来了。
    if (file.payload) await renderFileBody(token);
  }

  function closeFile() {
    if (!file) return;
    loadToken++;
    runCleanups();
    file = null;
    render();
  }

  function closeDetail() {
    if (!detail) return;
    loadToken++;
    runCleanups();
    file = null;
    detail = null;
    render();
  }

  function renderRoot(result) {
    current = gitLogView(result);
    // 根数据换了一代（chat 结束、手动刷新、工作区切换）不等于用户想离开已打开的
    // 提交：详情/内容各自持有自己那一次读取的事实，这里只换列表数据。
    if (!detail) render();
  }

  function reset() {
    loadToken++;
    runCleanups();
    current = null;
    detail = null;
    file = null;
    if (container) {
      container.classList.add("muted");
      container.classList.remove("git-log-view");
      container.innerHTML = "";
    }
  }

  return {
    renderRoot,
    reset,
    current: () => current,
    detail: () => detail,
    file: () => file
  };
}

// renderGitLogHTML 渲染整张提交图（逐行 SVG + 提交信息）。
export function renderGitLogHTML(view) {
  const rows = view?.graph?.rows || [];
  const laneCount = view?.graph?.laneCount || 1;
  const columnWidth = forkGraphWidth(laneCount, { laneWidth: LANE_WIDTH });
  const body = (view?.commits || []).map((commit, index) =>
    renderLine(commit, rows[index], columnWidth)).join("");
  const notes = [];
  if (view?.truncated) notes.push("已显示部分提交，其余省略");
  if (view?.graph?.dropped) notes.push("分支过多，部分连线未绘制");
  const notice = notes.length ? `<div class="git-log-limit">${escapeHtml(notes.join("；"))}</div>` : "";
  return `${body}${notice}`;
}

// renderGitCommitDetailHTML 渲染提交详情（头部 + 文件清单）。纯函数：给定一份
// 归一化后的详情就渲染完整一屏，供 node --test 直接断言。
export function renderGitCommitDetailHTML(detail) {
  if (detail?.error) return `${renderDetailHead(detail.hash, detail)}${renderErrorBlock(detail.error)}`;
  if (!detail || (detail.files || []).length === 0) {
    return `${renderDetailHead(detail?.hash || "", detail)}<div class="git-commit-empty">这次提交没有改动文件</div>`;
  }
  return `${renderDetailHead(detail.hash, detail)}${renderGitCommitFilesHTML(detail)}`;
}

// renderDetailHead 渲染详情头部：返回键 + 提交标题 + 一行事实（短 hash · 作者 ·
// 时间 · 文件数）。hash 是复制按钮（完整 hash 在 data 属性里）。
function renderDetailHead(hash, detail) {
  const full = textValue(detail?.hash) || textValue(hash);
  const short = textValue(detail?.shortHash) || full;
  const parts = [];
  if (detail?.author) parts.push(textValue(detail.author));
  if (detail?.date) parts.push(textValue(detail.date));
  if (detail && !detail.error) parts.push(`${detail.total} 个文件`);
  if (detail?.filtered) parts.push(`另有 ${detail.filtered} 条未展示`);
  return `<div class="git-commit-head">
    <button type="button" class="git-back" data-git-back="list" title="返回提交列表">← 提交列表</button>
    <span class="git-commit-title" title="${escapeHtml(textValue(detail?.subject))}">${escapeHtml(textValue(detail?.subject) || "(无提交标题)")}</span>
    <span class="git-commit-meta">
      <button type="button" class="git-log-hash" data-git-hash="${escapeHtml(full)}" title="复制完整 hash">${escapeHtml(short)}</button>
      ${parts.length ? `<span class="git-commit-facts">${escapeHtml(parts.join(" · "))}</span>` : ""}
    </span>
  </div>`;
}

// renderGitCommitFilesHTML 渲染文件清单：一行一处改动（状态字母 / 路径 /
// 重命名原路径 / ±行数）。被删除的文件在那个提交里没有字节可读，行不做按钮
// （title 说明原因），与「工作区更改」面板同一条口径。
function renderGitCommitFilesHTML(detail) {
  const rows = (detail.files || []).map(file => renderCommitFileRow(file, detail.hash)).join("");
  const notes = [];
  if (detail.truncated) notes.push("文件过多，已截断");
  if (detail.filtered) notes.push(`另有 ${detail.filtered} 条未展示（敏感路径或工作区之外）`);
  const notice = notes.length ? `<div class="git-log-limit">${escapeHtml(notes.join("；"))}</div>` : "";
  return `<div class="git-commit-files">${rows}</div>${notice}`;
}

function renderCommitFileRow(file, hash) {
  const label = kindLabel(file.kind);
  const status = `<span class="git-commit-status" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}">${escapeHtml(file.letter || "")}</span>`;
  const dir = file.dir ? `<span class="git-commit-dir">${escapeHtml(file.dir)}</span>` : "";
  const name = `<span class="git-commit-name">${escapeHtml(file.name)}</span>`;
  const path = file.previewable
    ? `<button type="button" class="git-commit-path" data-git-file="${escapeHtml(file.path)}" data-git-commit="${escapeHtml(hash)}" title="查看 ${escapeHtml(file.path)} 在该提交时的内容">${dir}${name}</button>`
    : `<span class="git-commit-path is-plain" title="${escapeHtml(`${file.path}（该提交里已删除，无内容可看）`)}">${dir}${name}</span>`;
  const old = file.oldPath
    ? `<span class="git-commit-old" title="原路径">← ${escapeHtml(file.oldPath)}</span>`
    : "";
  const stat = commitFileStatText(file);
  const statHTML = stat ? `<span class="git-commit-stat" title="本次改动的行数">${escapeHtml(stat)}</span>` : "";
  return `<div class="git-commit-row is-${escapeHtml(file.kind)}">${status}${path}${old}${statHTML}</div>`;
}

// renderFileHead 渲染文件内容视图的头部：返回键 + 路径 + 这次改动的状态与行数
// （状态来自用户在清单里点的那一行——同一份事实，不从别处重推）。
function renderFileHead(state, entry) {
  const facts = [];
  if (entry) {
    const label = kindLabel(entry.kind);
    facts.push(textValue(entry.letter) ? `${entry.letter} ${label}` : label);
    const stat = commitFileStatText(entry);
    if (stat) facts.push(stat);
  }
  return `<div class="git-commit-head">
    <button type="button" class="git-back" data-git-back="files" title="返回文件清单">← 文件清单</button>
    <span class="git-file-title" title="${escapeHtml(`${state.path} @ ${state.hash}`)}">${escapeHtml(state.path)}</span>
    <span class="git-commit-meta">${facts.length ? `<span class="git-commit-facts">${escapeHtml(facts.join(" · "))}</span>` : ""}</span>
  </div>`;
}

function renderErrorBlock(message) {
  return `<div class="git-commit-error">${escapeHtml(message)}</div>`;
}

function renderLine(commit, row, columnWidth) {
  const graph = `<span class="git-log-graph" aria-hidden="true">${commitGraphRowHTML(row, { laneWidth: LANE_WIDTH, rowHeight: ROW_HEIGHT })}</span>`;
  return `<div class="git-log-line" data-git-commit="${escapeHtml(commit.hash)}">
    ${graph}
    <span class="git-log-commit">
      <button type="button" class="git-log-hash" data-git-hash="${escapeHtml(commit.hash)}" title="复制完整 hash">${escapeHtml(commit.shortHash || commit.hash)}</button>
      <span class="git-log-meta" title="${escapeHtml(`${commit.author} · ${commit.date}`)}">
        <span class="git-log-date">${escapeHtml(commit.date)}</span>
      </span>
      <button type="button" class="git-log-subject" data-git-commit="${escapeHtml(commit.hash)}" title="查看这次提交改了哪些文件">${escapeHtml(commit.subject)}</button>
    </span>
  </div>`;
}

// commitFileStatText 渲染一处改动的行数：二进制由 git 直接标出（数不了行数），
// 数字为 0 的一侧不显示（+0 -3 里的 +0 只是噪音）。
export function commitFileStatText(file) {
  if (file?.binary) return "二进制";
  const parts = [];
  if (file?.additions) parts.push(`+${file.additions}`);
  if (file?.deletions) parts.push(`-${file.deletions}`);
  return parts.join(" ");
}

function emptyGraph() {
  return { rows: [], laneCount: 1, dropped: 0 };
}

function emptyDetail() {
  return {
    hash: "", shortHash: "", author: "", date: "", subject: "",
    files: [], total: 0, filtered: 0, truncated: false, error: "", root: ""
  };
}

function normalizeCommit(commit) {
  if (!commit || typeof commit !== "object" || Array.isArray(commit)) return null;
  const hash = textValue(commit.hash).trim();
  if (!hash) return null;
  return {
    hash,
    shortHash: textValue(commit.short_hash),
    author: textValue(commit.author),
    date: textValue(commit.date),
    parents: Array.isArray(commit.parents)
      ? commit.parents.filter(parent => typeof parent === "string" && parent)
      : [],
    subject: textValue(commit.subject)
  };
}

// normalizeCommitFile 归一化一处改动：缺 path 的条目丢弃；Kind 缺失退回
// modified（后端一定会给，退回只为防御畸形载荷）；删除的条目不可下钻（没有
// 字节可读）。目录/文件名拆开只为展示时目录淡显。
function normalizeCommitFile(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) return null;
  const path = textValue(entry.path).trim();
  if (!path) return null;
  const kind = textValue(entry.kind) || "modified";
  const cut = path.lastIndexOf("/");
  return {
    path,
    oldPath: textValue(entry.old_path),
    kind,
    status: textValue(entry.status),
    letter: textValue(entry.letter),
    additions: finiteCount(entry.additions),
    deletions: finiteCount(entry.deletions),
    binary: Boolean(entry.binary),
    previewable: kind !== "deleted",
    dir: cut >= 0 ? path.slice(0, cut + 1) : "",
    name: cut >= 0 ? path.slice(cut + 1) : path
  };
}

function textValue(value) {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function finiteCount(value) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? Math.floor(number) : 0;
}
