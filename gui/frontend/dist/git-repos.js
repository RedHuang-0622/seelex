// 多仓库 git 的「仓库子页标签」模型（纯函数 + 一段纯字符串渲染）。
//
// 一个工作区下可能有多棵互不相干的 git 仓库：根自己是仓库、根下若干子目录各自
// 是仓库、甚至仓库里套仓库。后端（Bridge.WorkspaceGitRepos）只给一份有界的相对
// 清单，本模块决定「标签栏长什么样、默认选哪个、选中的那个消失了怎么办」：
//
//   - buildRepoTabs(repos, selectedRel)  标签模型（primary 在前，其余按 rel 字典序）
//   - resolveSelectedRepo(repos, rel)    选中项收敛（不见了 → primary → 第一项 → ""）
//   - renderRepoTabs(model)              标签栏 HTML（只有一个仓库时返回 ""）
//
// 这里不碰 DOM、不碰 localStorage、不碰 Bridge：接线（事件委托、按工作区 ID 记忆、
// 显隐）全在 app.js，node --test 直接覆盖本文件。

// primary 优先、其余按 rel 字典序：标签位置稳定，读数确定（后端也是这个顺序，
// 这里再排一次是为了不依赖输入端顺序）。
function compareRepoRefs(left, right) {
  if (left.primary !== right.primary) return left.primary ? -1 : 1;
  if (left.rel === right.rel) return 0;
  return left.rel < right.rel ? -1 : 1;
}

function repoDisplayName(repo) {
  const name = typeof repo?.name === "string" ? repo.name.trim() : "";
  if (name) return name;
  const rel = typeof repo?.rel === "string" ? repo.rel.trim() : "";
  if (!rel || rel === ".") return ".";
  const parts = rel.split("/").filter(Boolean);
  return parts.length ? parts[parts.length - 1] : rel;
}

// normalizeRepoRefs 把任意输入收敛成合法仓库清单：丢弃没有 rel 的条目、按 rel
// 去重、补齐展示名与分支字符串（缺字段一律给空串，不编默认值）。
export function normalizeRepoRefs(repos) {
  const list = Array.isArray(repos) ? repos : [];
  const seen = new Set();
  const out = [];
  for (const repo of list) {
    if (!repo || typeof repo !== "object") continue;
    const rel = (typeof repo.rel === "string" ? repo.rel.trim() : "").replace(/\/+$/, "");
    if (!rel || seen.has(rel)) continue;
    seen.add(rel);
    out.push({
      rel,
      name: repoDisplayName(repo),
      primary: repo.primary === true,
      branch: typeof repo.branch === "string" ? repo.branch : ""
    });
  }
  return out.sort(compareRepoRefs);
}

// resolveSelectedRepo 收敛选中项：选中的那个还在 → 还是它；不在了（仓库被删/
// 改名/换工作区）→ primary；没有 primary → 清单第一项；清单为空 → ""（= 工作区
// 根视角，向后兼容旧的单一仓库查询）。
export function resolveSelectedRepo(repos, selectedRel) {
  const list = normalizeRepoRefs(repos);
  if (list.length === 0) return "";
  const wanted = typeof selectedRel === "string" ? selectedRel.trim() : "";
  if (wanted && list.some(repo => repo.rel === wanted)) return wanted;
  const primary = list.find(repo => repo.primary);
  return primary ? primary.rel : list[0].rel;
}

// buildRepoTabs 产出标签栏模型：show 只在"多于一个仓库"时为真（单仓库保持原观感，
// 不显示标签栏）；tabs 里恰有一个 active。
export function buildRepoTabs(repos, selectedRel) {
  const list = normalizeRepoRefs(repos);
  const selected = resolveSelectedRepo(list, selectedRel);
  return {
    show: list.length > 1,
    count: list.length,
    selected,
    tabs: list.map(repo => ({
      rel: repo.rel,
      name: repo.name,
      branch: repo.branch,
      primary: repo.primary,
      active: repo.rel === selected
    }))
  };
}

function escapeHtml(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// repoTitle 是标签的悬停提示：相对路径 + 分支（根视角说"工作区根"而不是"."）。
export function repoTitle(tab) {
  const where = tab.rel === "." ? "工作区根" : tab.rel;
  return tab.branch ? `${where} · ${tab.branch}` : where;
}

// renderRepoTabs 渲染标签栏 HTML；show 为假时返回空串（调用方据此隐藏容器）。
export function renderRepoTabs(model) {
  if (!model || model.show !== true || !Array.isArray(model.tabs) || model.tabs.length === 0) return "";
  return model.tabs.map(tab => {
    const active = tab.active === true;
    const branch = tab.branch ? `<span class="git-repo-branch">${escapeHtml(tab.branch)}</span>` : "";
    return `<button class="code-pane-tab git-repo-tab${active ? " is-active" : ""}" type="button" role="tab"`
      + ` data-git-repo="${escapeHtml(tab.rel)}" aria-selected="${active}"`
      + ` title="${escapeHtml(repoTitle(tab))}">${escapeHtml(tab.name)}${branch}</button>`;
  }).join("");
}
