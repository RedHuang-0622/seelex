import { escapeHtml } from "./components.js";
import { MAX_FORK_LANES, commitGraphRowHTML, forkGraphWidth, layoutCommitGraph } from "./tree-fork.js";

// ── 提交记录（Git Log）视图 ─────────────────────────────
// 数据源：Bridge.WorkspaceGitLog(limit)（后端权威只读元数据：按 git 拓扑序
// （新 → 旧）的提交行 + 每个提交的父提交 hash；不含 diff 或文件内容）。
//
// 渲染策略：分支拓扑不再贴 `git --graph` 的字符画（`* | \ /` 既撑不出真实
// 分叉，也不能随皮肤换色），而是把 parents 算成泳道
// （tree-fork.layoutCommitGraph），逐行用一条 SVG 画直线/合并贝塞尔 + 提交点。
// 提交行展示 短 hash（可点击复制完整 hash）/ 作者 / 时间 / 标题；全部文本 escape。

// MAX_FORK_LANES 之外的泳道不再画线（记 dropped，前端给一行提示）。
const LANE_WIDTH = 16;
const ROW_HEIGHT = 22;

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

// createGitLogView 创建视图实例：持有数据面（根加载后缓存）+ 复制回调。
// hash 复制用容器委托（一条监听），提交行重绘不再逐行绑事件。
export function createGitLogView(container, options = {}) {
  let current = null;
  const copyHash = options.onCopy || (async hash => {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(hash);
    }
  });
  container?.addEventListener("click", async event => {
    const button = event.target?.closest?.("[data-git-hash]");
    const hash = button?.dataset?.gitHash || "";
    if (!hash) return;
    try {
      await copyHash(hash);
      button.classList.add("is-copied");
      window.setTimeout(() => button.classList.remove("is-copied"), 900);
    } catch (error) {
      // 复制失败静默：hash 已展示，不影响视图。
    }
  });

  function render() {
    if (!container) return;
    container.classList.remove("muted");
    container.classList.add("git-log-view");
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

  function renderRoot(result) {
    current = gitLogView(result);
    render();
  }

  function reset() {
    current = null;
    if (container) {
      container.classList.add("muted");
      container.classList.remove("git-log-view");
      container.innerHTML = "";
    }
  }

  return { renderRoot, reset, current: () => current };
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

function renderLine(commit, row, columnWidth) {
  const graph = `<span class="git-log-graph" aria-hidden="true">${commitGraphRowHTML(row, { laneWidth: LANE_WIDTH, rowHeight: ROW_HEIGHT })}</span>`;
  return `<div class="git-log-line">
    ${graph}
    <span class="git-log-commit">
      <button type="button" class="git-log-hash" data-git-hash="${escapeHtml(commit.hash)}" title="复制完整 hash">${escapeHtml(commit.shortHash || commit.hash)}</button>
      <span class="git-log-meta" title="${escapeHtml(`${commit.author} · ${commit.date}`)}">
        <span class="git-log-date">${escapeHtml(commit.date)}</span>
      </span>
      <span class="git-log-subject" title="${escapeHtml(commit.subject)}">${escapeHtml(commit.subject)}</span>
    </span>
  </div>`;
}

function emptyGraph() {
  return { rows: [], laneCount: 1, dropped: 0 };
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

function textValue(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}
