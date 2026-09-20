import { escapeHtml } from "./components.js";

// ── 工作区更改（Work Tree Changes）视图 ─────────────────────
// 数据源：Bridge.WorkspaceChanges(limit)（后端权威只读元数据：状态字符/路径/
// 重命名原路径/统计；不含 diff、补丁或文件内容）。
//
// 交互：点击文件行 → 打开文件详情（options.onOpenFile，与工作树共用同一个
// 抽屉）；已删除的文件没有可预览的字节，行不可点（title 说明原因）。
//
// 渲染策略：状态字母与中文标签都从后端的 Kind 派生——porcelain 的 XY 语义只在
// 后端解释一次，前端不重推 git 规则；未知 Kind 退回「变更」且不猜字母（宁可
// 少说，不编造 git 语义）。全部文本 escape。

// KIND_LABELS：后端 dto.Change* → 中文标签。
const KIND_LABELS = {
  modified: "修改",
  added: "新增",
  deleted: "删除",
  renamed: "重命名",
  copied: "复制",
  type_changed: "类型变化",
  conflicted: "冲突",
  untracked: "未跟踪"
};

// STATUS_LETTERS：Kind → 单字母状态标记（git 习惯用法，纯展示）。
const STATUS_LETTERS = {
  modified: "M",
  added: "A",
  deleted: "D",
  renamed: "R",
  copied: "C",
  type_changed: "T",
  conflicted: "U",
  untracked: "?"
};

// workspaceChangesView 归一化改动结果（防御畸形载荷：非对象 → 空视图；entries
// 非数组 → []；缺 path 或非对象的行丢弃；计数取有限整数，负数与 NaN 归零）。
export function workspaceChangesView(result) {
  if (!result || typeof result !== "object" || Array.isArray(result)) {
    return emptyView("");
  }
  const entries = (Array.isArray(result.entries) ? result.entries : [])
    .map(normalizeChangeEntry)
    .filter(Boolean);
  return {
    entries,
    branch: textValue(result.branch),
    counts: {
      staged: finiteCount(result.staged),
      unstaged: finiteCount(result.unstaged),
      untracked: finiteCount(result.untracked),
      conflicted: finiteCount(result.conflicted)
    },
    total: finiteCount(result.total) || entries.length,
    filtered: finiteCount(result.filtered),
    truncated: Boolean(result.truncated),
    error: textValue(result.error),
    root: textValue(result.root)
  };
}

// kindLabel / statusLetter：未知 Kind 退回「变更」与空字母（不猜）。
export function kindLabel(kind) {
  return KIND_LABELS[textValue(kind)] || "变更";
}

export function statusLetter(kind) {
  return STATUS_LETTERS[textValue(kind)] || "";
}

// createWorkspaceChangesView 创建视图实例：持有数据面与打开文件回调。
// 行点击走容器委托（一条监听）：列表每次刷新整块重绘，逐行绑监听会留下孤儿闭包。
export function createWorkspaceChangesView(container, options = {}) {
  let current = null;
  const onOpenFile = typeof options.onOpenFile === "function" ? options.onOpenFile : null;

  container?.addEventListener("click", event => {
    const button = event.target?.closest?.("[data-change-path]");
    const path = button?.dataset?.changePath || "";
    if (!path || !onOpenFile) return;
    onOpenFile({
      name: button.dataset.changeName || path.split("/").pop() || path,
      path,
      type: "file",
      size: 0,
      count: 0
    });
  });

  function render() {
    if (!container) return;
    container.classList.remove("muted");
    container.classList.add("changes-view");
    if (!current) {
      container.innerHTML = '<div class="changes-empty">暂无改动信息</div>';
      return;
    }
    if (current.error) {
      container.innerHTML = `<div class="changes-error">${escapeHtml(current.error)}</div>`;
      return;
    }
    if (current.entries.length === 0) {
      container.innerHTML = '<div class="changes-empty">工作区干净：没有未提交改动</div>';
      return;
    }
    container.innerHTML = renderWorkspaceChangesHTML(current);
  }

  function renderRoot(result) {
    current = workspaceChangesView(result);
    render();
  }

  function reset() {
    current = null;
    if (container) {
      container.classList.add("muted");
      container.classList.remove("changes-view");
      container.innerHTML = "";
    }
  }

  return { renderRoot, reset, current: () => current };
}

// renderWorkspaceChangesHTML 渲染统计行 + 改动行 + 截断/过滤提示。
export function renderWorkspaceChangesHTML(view) {
  const rows = (view?.entries || []).map(renderChangeRow).join("");
  const notes = [];
  if (view?.truncated) notes.push("条目过多，已截断");
  if (view?.filtered) notes.push(`另有 ${view.filtered} 条未展示（敏感路径或工作区之外）`);
  const notice = notes.length ? `<div class="changes-limit">${escapeHtml(notes.join("；"))}</div>` : "";
  return `${renderSummary(view)}${rows}${notice}`;
}

// renderSummary 渲染「分支 · 暂存 x · 未暂存 y · 未跟踪 z」统计行；没有分支与
// 计数时不渲染空壳。
function renderSummary(view) {
  const counts = view?.counts || {};
  const segments = [];
  if (counts.staged) segments.push(`暂存 ${counts.staged}`);
  if (counts.unstaged) segments.push(`未暂存 ${counts.unstaged}`);
  if (counts.untracked) segments.push(`未跟踪 ${counts.untracked}`);
  if (counts.conflicted) segments.push(`冲突 ${counts.conflicted}`);
  if (!view?.branch && segments.length === 0) return "";
  const branch = view?.branch
    ? `<span class="changes-branch" title="当前分支">${escapeHtml(view.branch)}</span>`
    : "";
  const summary = segments.length
    ? `<span class="changes-counts" title="按过滤后的全部条目统计">${escapeHtml(segments.join(" · "))}</span>`
    : "";
  return `<div class="changes-summary">${branch}${summary}</div>`;
}

// renderChangeRow 渲染一行改动：状态字母 + 路径（目录淡显）+ 重命名原路径 +
// 暂存标记。删除的文件不可点开详情（没有字节可读），用 span + title 说明。
function renderChangeRow(entry) {
  const label = kindLabel(entry.kind);
  const letter = statusLetter(entry.kind);
  const status = `<span class="changes-status" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}">${escapeHtml(letter)}</span>`;
  const dir = entry.dir ? `<span class="changes-dir">${escapeHtml(entry.dir)}</span>` : "";
  const name = `<span class="changes-name">${escapeHtml(entry.name)}</span>`;
  const path = entry.previewable
    ? `<button type="button" class="changes-path" data-change-path="${escapeHtml(entry.path)}" data-change-name="${escapeHtml(entry.name)}" title="查看 ${escapeHtml(entry.path)} 的详情">${dir}${name}</button>`
    : `<span class="changes-path is-plain" title="${escapeHtml(`${entry.path}（已删除，无内容可预览）`)}">${dir}${name}</span>`;
  const old = entry.oldPath
    ? `<span class="changes-old" title="原路径">← ${escapeHtml(entry.oldPath)}</span>`
    : "";
  const staged = entry.staged ? '<span class="changes-badge" title="暂存侧有改动">已暂存</span>' : "";
  return `<div class="changes-row is-${escapeHtml(entry.kind)}">
    ${status}${path}${old}${staged}
  </div>`;
}

function emptyView(error) {
  return {
    entries: [],
    branch: "",
    counts: { staged: 0, unstaged: 0, untracked: 0, conflicted: 0 },
    total: 0,
    filtered: 0,
    truncated: false,
    error,
    root: ""
  };
}

// normalizeChangeEntry 归一化一条改动：缺 path 的行丢弃；重命名原路径只保留
// 字符串；staged 取布尔真值；删除的文件不带可预览标记。
function normalizeChangeEntry(entry) {
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
    index: textValue(entry.index),
    worktree: textValue(entry.worktree),
    staged: Boolean(entry.staged),
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
