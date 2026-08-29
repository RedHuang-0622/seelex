import { escapeHtml } from "./components.js";

// ── 提交记录树（Git Log Tree）视图 ─────────────────────────
// 数据源：Bridge.WorkspaceGitLog(limit)（后端权威只读元数据：git log
// --graph 拓扑行 + hash/作者/时间/标题；不含 diff 或文件内容）。
//
// 渲染策略：Graph 前缀（"* "、"| "、"|\ " 等）原样等宽渲染，保留 git 的
// 分支拓扑视觉；提交行展示 短 hash（可点击复制完整 hash）/ 作者 / 时间 /
// 标题；延续线（merge 的 | \ / 等）只画 graph。全部文本 escape。

const MAX_GRAPH_COLS = 48; // graph 前缀宽度上限（防御畸形行撑爆布局）

// gitLogView 归一化 git log 结果（防御畸形载荷：非对象 → 空；lines 非数组
// → []；行内 commit 字段缺省 → 空串；graph 超宽截断）。
export function gitLogView(result) {
  if (!result || typeof result !== "object") {
    return { lines: [], commits: [], truncated: false, error: "", root: "" };
  }
  const lines = Array.isArray(result.lines) ? result.lines : [];
  const normalized = lines
    .filter(isLogLine)
    .map(line => ({
      graph: clampGraph(textValue(line.graph)),
      commit: line.commit && typeof line.commit === "object"
        ? normalizeCommit(line.commit)
        : null
    }));
  const commits = normalized
    .map(line => line.commit)
    .filter(Boolean);
  return {
    lines: normalized,
    commits,
    truncated: Boolean(result.truncated),
    error: textValue(result.error),
    root: textValue(result.root)
  };
}

// createGitLogView 创建视图实例：持有数据面（根加载后缓存）+ 复制回调。
export function createGitLogView(container, options = {}) {
  let current = null;
  const copyHash = options.onCopy || (async hash => {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(hash);
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
    if (current.lines.length === 0) {
      container.innerHTML = '<div class="git-log-empty">仓库暂无提交</div>';
      return;
    }
    container.innerHTML = renderGitLogHTML(current);
    container.querySelectorAll("[data-git-hash]").forEach(button => {
      button.addEventListener("click", async () => {
        const hash = button.dataset.gitHash || "";
        if (!hash) return;
        try {
          await copyHash(hash);
          button.classList.add("is-copied");
          window.setTimeout(() => button.classList.remove("is-copied"), 900);
        } catch (error) {
          // 复制失败静默：hash 已展示，不影响视图。
        }
      });
    });
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

// renderGitLogHTML 渲染全部拓扑行（graph 前缀 + 可选提交信息）。
export function renderGitLogHTML(view) {
  const body = view.lines.map(renderLine).join("");
  const truncated = view.truncated
    ? '<div class="git-log-limit">已显示部分提交，其余省略</div>'
    : "";
  return `${body}${truncated}`;
}

function renderLine(line) {
  const graph = `<span class="git-log-graph" aria-hidden="true">${escapeHtml(line.graph)}</span>`;
  if (!line.commit) {
    return `<div class="git-log-line is-continuation">${graph}</div>`;
  }
  const commit = line.commit;
  return `<div class="git-log-line">
    ${graph}
    <span class="git-log-commit">
      <button type="button" class="git-log-hash" data-git-hash="${escapeHtml(commit.hash)}" title="复制完整 hash">${escapeHtml(commit.shortHash || commit.hash)}</button>
      <span class="git-log-meta">
        <span class="git-log-author">${escapeHtml(commit.author)}</span>
        <span class="git-log-date">${escapeHtml(commit.date)}</span>
      </span>
      <span class="git-log-subject" title="${escapeHtml(commit.subject)}">${escapeHtml(commit.subject)}</span>
    </span>
  </div>`;
}

function normalizeCommit(commit) {
  return {
    hash: textValue(commit.hash),
    shortHash: textValue(commit.short_hash),
    author: textValue(commit.author),
    date: textValue(commit.date),
    subject: textValue(commit.subject)
  };
}

function isLogLine(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function textValue(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function clampGraph(graph) {
  if (graph.length <= MAX_GRAPH_COLS) return graph;
  return graph.slice(0, MAX_GRAPH_COLS) + "…";
}
