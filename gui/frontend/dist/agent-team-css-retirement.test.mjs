// 「退场样式回流闸」：Agent Team 面板删面之后，`styles.css` 里那批面板选择器成了
// **永远匹配不到**的死规则——DOM 已经不存在了，选择器再正确也只是白占字节。J-1a 的
// 前端条目按硬约束只删面、没动 `styles.css`（见 gui/frontend/README.md「样式」一条），
// 这道残留由 wi-j1a-css 收口：**先逐条证明再删**——对每个候选前缀在存活资产
// （`dist/**.js` / `index.html` / `dist/themes/*.css`）里数引用，只删"引用数 0"的选择器。
//
// 它防的是**回流**，不是"少删"。删面最容易留下的不是报错，是**看不见的残留**：
//   - 样式侧：有人把面板的类名重新写回 `styles.css`（新规则挂在 `team-rail-*` 上）；
//   - 脚本侧：有人把某个退场类名重新写进模板串（类名复活，样式却没人补）。
// 两边都不报错，只是永远不生效，所以这里把"退场前缀 = 0 命中"钉成断言，**同时钉住保留面**：
//   - 团队看板 `team-board-*`（数据源 `runtime.teamwork_board`，渲染件 `team-board-view.js`
//     自带 `TEAM_BOARD_CSS`）——团队面只剩这一块，绝不删；
//   - `role-session-view.js`（角色会话只读观察面）仍在用的面板类名：`team-block` /
//     `team-floor` / `team-floor-chip` / `team-member-name` / `team-view` / `team-notice` 等；
//   - 团队看板自己的表格件 `team-table` / `team-kv*` 仍在 `styles.css` 里有规则。
//
// 手法照 `style-baseline.test.mjs`：自己读 `styles.css` 解析选择器（按**类名 token** 匹配
// 前缀，不做整文件子串匹配——`team-board-*` 是活体，`scheduled-*` 也不是 `schedule-*`）。
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

// 已退场的类名集合：**精确名** + 整族**前缀**（结尾带 `-`）。
// 名单来源 = wi-j1a-css 在存活侧逐条数引用得到 0 的选择器；加前缀是为了把"同族新成员"也拦住。
// 注意几处刻意**不在**名单里的名字（它们是活体，删了就是把读面/看板带走）：
//   team-block / team-floor / team-floor-chip / team-member-name / team-table / team-kv* /
//   team-panel / team-view / team-notice / team-refresh / team-hire（`[data-team-hire-plugins]`
//   这个属性名仍出现在退场守卫里，故按"有引用就不删"保守保留） / team-member-role（同）。
const RETIRED_EXACT = [
  "team-table-row",
  "team-toolbar",
  "team-empty",
  "team-next",
  "team-stop",
  "team-drift",
  "team-meta-row",
  "team-panel-toolbar",
  "team-kv-list",
  "team-lib-note",
  "team-schedule-order",
  "team-scheduled-note",
  "team-outside-row",
  "team-source-chip",
  "team-member-list",
  "team-member-item",
  "team-member-label",
  "team-member-drop",
  "team-member-add",
  "team-member-empty",
  "team-member-edit",
  "team-member-actions",
  "team-cell-key",
  "team-cell-value",
  "team-field",
  "team-hire-form",
  "team-hire-actions",
  "team-hire-hint",
  "team-hire-submit",
  "team-editor",
];
const RETIRED_PREFIX = [
  "team-rail-",
  "team-editor-",
  "team-order-",
  "team-library-",
  "team-perm-",
  "team-prompt-",
  "team-staff-",
  "team-cell-",
  "team-field-",
  "schedule-", // `scheduled-*`（属性表）不受影响：它不以 `schedule-` 开头
];
const isRetired = (name) => RETIRED_EXACT.includes(name) || RETIRED_PREFIX.some(p => name.startsWith(p));

// 从 styles.css 里取出所有**选择器里的类名 token**（剥注释、只认 `{` 之前的选择器段）。
function classTokens(css) {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, " ");
  const tokens = new Set();
  for (const match of bare.matchAll(/([^{}]+)\{/g)) {
    const selector = match[1].trim();
    if (selector === "" || selector.startsWith("@")) continue;
    for (const cls of selector.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) tokens.add(cls[1]);
  }
  return tokens;
}

// 存活资产（除 styles.css 自己）：退场类名在这里出现 = 类名复活（JS 侧回流）。
async function liveSurfaceFiles() {
  const files = [];
  const walk = async (prefix) => {
    for (const entry of await readdir(new URL(`./${prefix}`, import.meta.url), { withFileTypes: true })) {
      const rel = `${prefix}${entry.name}`;
      if (entry.isDirectory()) await walk(`${rel}/`);
      else files.push(rel);
    }
  };
  await walk("");
  return files.filter(name => name !== "styles.css" && !name.endsWith(".test.mjs"));
}

test("退场的面板选择器不许回流：已退场前缀在 styles.css 的类名里 0 命中", async () => {
  const tokens = classTokens(await read("styles.css"));
  const back = [...tokens].filter(isRetired);
  assert.deepEqual(
    back,
    [],
    `已退场的 Agent Team 面板选择器又回到了 styles.css：${back.join(" / ")}。\n` +
      `面板已不在 DOM 里，这些规则永远匹配不到——要加新样式请用**存活面**的类名` +
      `（team-board-* / role-session-* / 看板自带 TEAM_BOARD_CSS）。`,
  );
});

test("退场类名不许在脚本侧复活：存活资产里 0 命中", async () => {
  const files = await liveSurfaceFiles();
  const hits = [];
  for (const name of files) {
    const text = await read(name);
    for (const token of [...RETIRED_EXACT, ...RETIRED_PREFIX]) {
      if (text.includes(token)) hits.push(`${name} :: ${token}`);
    }
  }
  assert.deepEqual(hits, [], `退场类名被重新引用（类名复活但没有样式）：\n${hits.join("\n")}`);
});

test("保留面①团队看板：styles.css 里 team-board 之外的面板表格件仍在，看板渲染件自带样式不动", async () => {
  const css = await read("styles.css");
  const tokens = classTokens(css);
  for (const kept of ["team-panel", "team-block", "team-table", "team-kv", "team-kv-row", "team-view", "team-notice", "team-refresh", "team-floor", "team-floor-chip", "team-member-name"]) {
    assert.ok(tokens.has(kept), `仍在用的类名 ${kept} 的规则不许被本轮清掉（tokens: ${[...tokens].filter(t => t.startsWith("team-")).sort().join(",")}）`);
  }
  const board = await read("team-board-view.js");
  for (const kept of [".team-table", ".team-kv-row", ".team-board", ".team-dag-", "TEAM_BOARD_CSS"]) {
    assert.ok(board.includes(kept), `团队看板渲染件必须原样在场：${kept}`);
  }
});

test("保留面②角色会话只读观察面：它用的面板类名与其样式都在", async () => {
  const role = await read("role-session-view.js");
  const css = await read("styles.css");
  const tokens = classTokens(css);
  for (const kept of ["team-block", "team-floor-chip", "team-view"]) {
    assert.ok(role.includes(kept), `角色会话只读观察面仍在用 ${kept}，不许删`);
    assert.ok(tokens.has(kept), `${kept} 的样式必须留在 styles.css（读面与看板共用这层类名）`);
  }
  // 读面自己的类名族也必须在场（删面不等于删读面）。
  for (const kept of ["role-session-view", "role-session-row", "role-live-tools", "role-record-table"]) {
    assert.ok(tokens.has(kept), `读面类名 ${kept} 的规则必须留在 styles.css`);
  }
  assert.ok(role.includes("role-live-tools") && role.includes("role-record-table"), "读面的渲染件必须仍在用这两族类名");
});

test("清理是整块摘除：styles.css 结构仍完整（括号配平、无空 @media 残留、无声明混进选择器）", async () => {
  const css = await read("styles.css");
  let depth = 0;
  let min = 0;
  for (const ch of css) {
    if (ch === "{") depth++;
    else if (ch === "}") { depth--; if (depth < min) min = depth; }
  }
  assert.equal(depth, 0, "花括号必须配平（整块摘除不能把邻居吃掉）");
  assert.equal(min, 0, "不许出现多余的 `}`（多一个右括号会让后面整段选择器错位）");
  const emptyAt = [...css.replace(/\/\*[\s\S]*?\*\//g, " ").matchAll(/@[a-z-]+[^{}]*\{\s*\}/g)];
  assert.deepEqual(emptyAt.map(m => m[0]), [], "删到只剩壳的 @media / @supports 要一起摘掉，不留空块");
  // `:root` 与半径 token 是 B0 的地基，本轮只删面板死规则，不许顺手动它们。
  const base = css.match(/:root \{[\s\S]*?\n\}/);
  assert.ok(base, "要找得到 :root 基座");
  assert.match(base[0], /--r-md:\s*[0-9.]+px/, "--r-md 仍在 :root 单一供源（本轮不碰 token）");
});
