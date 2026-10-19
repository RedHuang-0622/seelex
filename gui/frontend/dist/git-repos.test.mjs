// 多仓库 git 的仓库标签模型单测（纯函数，node --test 直接跑）。
//
// 覆盖：标签模型（primary 在前 + 字典序）、单选（恰有一个 active）、选中项消失
// 回落（→ primary → 第一项）、单仓库不出标签栏、空清单向后兼容（"" = 工作区根）。
import assert from "node:assert/strict";
import test from "node:test";

import {
  buildRepoTabs,
  normalizeRepoRefs,
  renderRepoTabs,
  repoTitle,
  resolveSelectedRepo
} from "./git-repos.js";

const threeRepos = [
  { rel: "vendor/lib", name: "lib", branch: "dev" },
  { rel: ".", name: "project", primary: true, branch: "main" },
  { rel: "tools/cli", name: "cli", branch: "feat/x" }
];

test("标签模型：primary 排第一，其余按 rel 字典序", () => {
  const model = buildRepoTabs(threeRepos, ".");
  assert.equal(model.show, true);
  assert.equal(model.count, 3);
  assert.deepEqual(model.tabs.map(tab => tab.rel), [".", "tools/cli", "vendor/lib"]);
  assert.deepEqual(model.tabs.map(tab => tab.primary), [true, false, false]);
});

test("单选：恰有一个 active，且就是选中的那个仓库", () => {
  const model = buildRepoTabs(threeRepos, "vendor/lib");
  assert.equal(model.selected, "vendor/lib");
  assert.equal(model.tabs.filter(tab => tab.active).length, 1);
  assert.equal(model.tabs.find(tab => tab.active).rel, "vendor/lib");
});

test("选中项消失回落：先回 primary，没有 primary 回第一项", () => {
  // 选中的仓库被删/改名 → 回 primary（"."）。
  assert.equal(resolveSelectedRepo(threeRepos, "gone/repo"), ".");
  // 没有 primary → 回字典序第一项。
  const noPrimary = [
    { rel: "b", name: "b" },
    { rel: "a", name: "a" }
  ];
  assert.equal(resolveSelectedRepo(noPrimary, "gone"), "a");
  const model = buildRepoTabs(noPrimary, "gone");
  assert.equal(model.selected, "a");
  assert.equal(model.tabs.find(tab => tab.active).rel, "a");
});

test("空清单回落到空串（工作区根视角，旧的单仓库查询不变）", () => {
  assert.equal(resolveSelectedRepo([], "vendor/lib"), "");
  const model = buildRepoTabs([], "vendor/lib");
  assert.equal(model.show, false);
  assert.equal(model.selected, "");
  assert.deepEqual(model.tabs, []);
});

test("单仓库不出标签栏（保持原观感）", () => {
  const single = [{ rel: ".", name: "project", primary: true, branch: "main" }];
  const model = buildRepoTabs(single, "");
  assert.equal(model.count, 1);
  assert.equal(model.show, false);
  assert.equal(model.selected, ".");
  assert.equal(renderRepoTabs(model), "", "只有一个仓库时标签栏必须是空串");
});

test("标签栏渲染：每枚标签带仓库凭据、单选态与分支提示", () => {
  const html = renderRepoTabs(buildRepoTabs(threeRepos, "tools/cli"));
  assert.equal((html.match(/data-git-repo="/g) || []).length, 3);
  assert.equal((html.match(/aria-selected="true"/g) || []).length, 1);
  assert.match(html, /data-git-repo="tools\/cli" aria-selected="true"/);
  assert.match(html, /class="git-repo-branch">feat\/x</);
  assert.match(html, /title="工作区根 · main"/);
  // 复用既有页签样式（.code-pane-tab），不另造一套外观。
  assert.match(html, /class="code-pane-tab git-repo-tab is-active"/);
});

test("仓库引用收敛：去重、丢脏值、补齐展示名", () => {
  const refs = normalizeRepoRefs([
    { rel: "a", name: "a" },
    { rel: "a", name: "dup" },
    { rel: "  " },
    null,
    { rel: "pkg/tool/" },
    { rel: "pkg/x", branch: 5 }
  ]);
  assert.deepEqual(refs.map(ref => ref.rel), ["a", "pkg/tool", "pkg/x"]);
  assert.equal(refs[1].name, "tool", "缺展示名时用 basename");
  assert.equal(refs[1].branch, "", "分支缺失给空串，不编默认值");
  assert.equal(refs[2].branch, "");
});

test("根视角的悬停提示说人话：工作区根而不是点", () => {
  assert.equal(repoTitle({ rel: ".", branch: "main" }), "工作区根 · main");
  assert.equal(repoTitle({ rel: "vendor/lib", branch: "" }), "vendor/lib");
});
