import { escapeHtml, icon } from "./components.js";

// ── 工作树（Work Tree）视图 ──────────────────────────────
// 数据源：Bridge.WorkspaceTree(relPath, depth)（后端权威元数据：名称/路径/
// 类型/大小/直接文件计数；只含元数据，不含文件内容）与
// Bridge.WorkspaceFileCount()（递归文件/目录数）。
//
// 交互：
//  - 目录行点击 → 惰性展开（首次经 options.loadDir 拉子级并缓存）；
//  - 文件行显示名称与大小；目录行显示直接文件计数 badge；
//  - Truncated 提示「条目过多已截断」。
//
// 渲染策略：展开态是纯 UI 态（视图实例持有，不写回业务状态）；节点按
// --tree-depth 缩进，全部文本 escape。

const MAX_TREE_DEPTH = 32;

// worktreeView 归一化工作树条目（防御畸形载荷：非数组 → []；非对象或
// 无 name 的条目丢弃；type 只认 dir/file）。
export function worktreeView(entries) {
  if (!Array.isArray(entries)) return [];
  return entries
    .filter(isTreeEntry)
    .map(entry => ({
      name: textValue(entry.name),
      path: textValue(entry.path),
      type: entry.type === "dir" ? "dir" : "file",
      size: finiteNumber(entry.size) ?? 0,
      count: finiteNumber(entry.count) ?? 0
    }))
    .filter(entry => entry.name);
}

// createWorkTreeView 创建视图实例：持有展开/缓存/加载/截断的纯 UI 态。
// options.onOpenFile(fileEntry) 可选：文件行点击打开详情（fileEntry 为
// 归一化条目 {name,path,type,size,count}）。
export function createWorkTreeView(container, options = {}) {
  const state = {
    children: new Map(),  // dirPath -> normalized entries（已加载）
    expanded: new Set(),  // dirPath -> 展开中
    loading: new Set(),   // dirPath -> 首拉进行中
    truncated: new Map(), // dirPath -> 是否截断
    error: "",
    onOpenFile: typeof options.onOpenFile === "function" ? options.onOpenFile : null
  };
  let rootEntries = [];

  function render() {
    if (!container) return;
    container.classList.remove("muted");
    container.classList.add("worktree-view");
    container.innerHTML = renderWorkTreeHTML(rootEntries, state);
    container.querySelectorAll("[data-tree-dir]").forEach(button => {
      button.addEventListener("click", () => {
        toggle(button.dataset.treeDir);
      });
    });
    if (state.onOpenFile) {
      container.querySelectorAll("[data-file-open]").forEach(button => {
        button.addEventListener("click", () => {
          const entry = {
            name: button.dataset.fileName || "",
            path: button.dataset.fileOpen || "",
            type: "file",
            size: finiteNumber(button.dataset.fileSize) ?? 0,
            count: 0
          };
          if (entry.name && entry.path) state.onOpenFile(entry);
        });
      });
    }
  }

  async function toggle(path) {
    if (state.expanded.has(path)) {
      state.expanded.delete(path);
      render();
      return;
    }
    state.expanded.add(path);
    if (!state.children.has(path) && !state.loading.has(path)) {
      state.loading.add(path);
      render();
      try {
        const listing = await options.loadDir(path);
        state.children.set(path, worktreeView(listing?.entries));
        state.truncated.set(path, Boolean(listing?.truncated));
        state.error = "";
      } catch (error) {
        state.error = String(error?.message || error);
      } finally {
        state.loading.delete(path);
      }
    }
    render();
  }

  function renderRoot(entries) {
    rootEntries = worktreeView(entries);
    render();
  }

  function reset() {
    state.children.clear();
    state.expanded.clear();
    state.loading.clear();
    state.truncated.clear();
    state.error = "";
    rootEntries = [];
  }

  return { renderRoot, reset, current: () => rootEntries, state };
}

// renderWorkTreeHTML 渲染整棵展开树（根列表 + 已展开子级递归）。
export function renderWorkTreeHTML(rootEntries, state) {
  const rows = renderLevel(rootEntries, state, 0, 0);
  const error = state.error
    ? `<div class="worktree-error">${escapeHtml(state.error)}</div>`
    : "";
  return `${error}${rows || '<div class="worktree-empty">目录为空</div>'}`;
}

function renderLevel(entries, state, level, depth) {
  if (depth > MAX_TREE_DEPTH) {
    return '<div class="worktree-limit">目录层级过深，已停止展开</div>';
  }
  return entries.map(entry => {
    const isDir = entry.type === "dir";
    const expanded = isDir && state.expanded.has(entry.path);
    const children = isDir && state.children.get(entry.path);
    const loading = isDir && state.loading.has(entry.path);
    const truncated = isDir && state.truncated.get(entry.path);
    const childrenHTML = isDir && expanded && children
      ? renderLevel(children, state, level + 1, depth + 1)
      : "";
    const spinner = isDir && expanded && loading && !children
      ? '<span class="tree-loading" aria-label="加载中"></span>'
      : "";
    return `<div class="tree-node" style="--tree-depth:${level}">
      <div class="tree-row is-${isDir ? "dir" : "file"}">
        <span class="tree-icon" aria-hidden="true">${isDir ? icon("folder", 14) : icon("file", 14)}</span>
        ${isDir
          ? `<button type="button" class="tree-toggle" data-tree-dir="${escapeHtml(entry.path)}" aria-expanded="${expanded}" title="展开/折叠 ${escapeHtml(entry.name)}">${escapeHtml(entry.name)}</button>
             <span class="tree-count" title="直接文件数">${entry.count}</span>${spinner}`
          : `<button type="button" class="tree-file tree-file-open" data-file-open="${escapeHtml(entry.path)}" data-file-name="${escapeHtml(entry.name)}" data-file-size="${entry.size}" title="查看 ${escapeHtml(entry.path)}">${escapeHtml(entry.name)}</button>
             <span class="tree-size">${formatSize(entry.size)}</span>`}
      </div>
      ${isDir && expanded && truncated && children
        ? '<div class="tree-limit">条目过多，已截断</div>'
        : ""}
      ${childrenHTML}
    </div>`;
  }).join("");
}

// formatSize 字节数 → 可读大小（B/KB/MB/GB）。
export function formatSize(bytes) {
  const value = finiteNumber(bytes) ?? 0;
  if (value <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let index = 0;
  let size = value;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${index === 0 ? size : size.toFixed(1)} ${units[index]}`;
}

function isTreeEntry(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function textValue(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function finiteNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}
