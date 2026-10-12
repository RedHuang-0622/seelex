// 「半径 token 闸」：圆角必须走 :root 的 --r-sm / --r-md / --r-lg / --r-xl / --r-2xl，
// 不许再写裸像素（border-radius 加数值加 px）。
//
// 为什么要有这条闸：styles.css 是 dist 即源的单文件（28 万字节、无 bundler），圆角
// 散落各处直接写死 1px / 2px / … / 999px，于是"同一个胶囊、两处半径"，改一处漏一处；
// 皮肤矩阵（dist/themes/*.css）也补不出第二套半径口径。这条闸把它们钉住。
//
// **它防的是新增**：只数 `border-radius: <数值>px` 形式的声明条数，断言不超过基线
// 49，不做行号快照——行号会漂移，选择器与计数不会。既有那 49 处硬编码是待清理的
// 历史存量，清理是后续条目的事：**不要**为了让这条闸变绿去批量改它们；真正该做的
// 收口动作是清理完之后把下面的基线数字调小。
//
// 手法照搬 gui/frontend/dist/text-button-chrome.test.mjs：自己解析 styles.css，把
// 「选择器 → 声明体」展开（styles.css 里没有嵌套规则），再按声明体逐条计数。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

// 基线（2026-10-12 实测）：1px×2、2px×3、3px×1、4px×2、5px×3、6px×2、8px×5、
// 999px×31，合计 49。这 49 处只许变少，不许变多。
const RADIUS_BASELINE = 49;

// cssRules 剥掉注释后按 { 选择器 → 声明体 } 展开（与 text-button-chrome.test.mjs 同款）。
function cssRules(css) {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (match[1].trim().startsWith("@")) continue;
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

// HARDCODED_RADIUS 只认「整条声明就是裸像素」：`border-radius: 8px`、`border-radius: 999px`。
// 走 token（var(--r-*)）、纯 0、百分比、多值混写（如 `border-radius: 0 0 8px 8px`）都不进
// 计数——它们不是"第二套半径语汇"。
const HARDCODED_RADIUS = /\bborder-radius\s*:\s*[0-9.]+px/g;

test("styles.css 的硬编码像素半径只减不增（防新增裸半径）", async () => {
  const rules = cssRules(await read("styles.css"));
  const hits = [];
  for (const rule of rules) hits.push(...(rule.body.match(HARDCODED_RADIUS) || []));

  assert.ok(
    hits.length <= RADIUS_BASELINE,
    `硬编码 border-radius 又多了：${hits.length} > 基线 ${RADIUS_BASELINE}。\n` +
      `圆角请走 token：--r-sm / --r-md / --r-lg / --r-xl / --r-2xl（见文件顶部「B0 · 视觉基线规则」）。\n` +
      `当前计数：${JSON.stringify(hits.reduce((acc, hit) => ((acc[hit] = (acc[hit] || 0) + 1), acc), {}))}`,
  );
});

test("半径 token 仍由顶部 :root 单一供源", async () => {
  const css = await read("styles.css");
  const base = css.match(/:root \{[\s\S]*?\n\}/);
  assert.ok(base, "要找得到浅色基座的 :root 规则");
  for (const token of ["--r-sm", "--r-md", "--r-lg", "--r-xl", "--r-2xl"]) {
    assert.match(base[0], new RegExp(`${token}:\\s*[0-9.]+px`), `${token} 必须仍在 :root 里定义`);
  }
});

test("styles.css 顶部仍保留 B0 视觉基线规则块（左栏锚点）", async () => {
  const css = await read("styles.css");
  assert.match(css, /B0 · 视觉基线规则/, "基线规则块不能被删");
  // 锚点以选择器名为主：删/改左栏会话行族时要把基线块一起对齐。
  for (const anchor of [".session-row:hover", ".session-row.is-active", "--row-min-h"]) {
    assert.ok(css.includes(anchor), `基线块引用的锚点 ${anchor} 必须仍然存在`);
  }
});

// ── F-2（2026-10-12）附加闸：工作表页签的覆盖只留三段 ────────────────────────
// `.excel-sheet` 原先被定义/覆盖 4 处（基础 + 动效段 + reduced-motion + 末段再覆盖），
// 末段那份（与 .terminal-tab 合并）是纯粹的"更靠后的覆盖"；`.excel-sheets` 曾经是
// 表格底部的独立灰挡板（border-top + --surface-2）。F-2 把批次页签并进表头、删掉
// 重复源，只留三段职责分明：①基础（第 11 节，含选中胶囊）②动效（transition + 位移）
// ③reduced-motion（复位）。这条闸钉住"末段不得再合并覆盖 .excel-sheet"。
test("工作表页签覆盖只剩三段：末段不再把 .excel-sheet 与 .terminal-tab 合并覆盖", async () => {
  const rules = cssRules(await read("styles.css"));

  const merged = rules.filter(
    rule => /\.excel-sheet(?![\w-])/.test(rule.selector) && /\.terminal-tab(?![\w-])/.test(rule.selector)
  );
  assert.deepEqual(merged, [], "末段不得再把 .excel-sheet 与 .terminal-tab 并成一条覆盖；选中态写在基础段");

  const active = rules.find(rule => rule.selector === ".excel-sheet.is-active");
  assert.ok(active, "基础段要有 .excel-sheet.is-active");
  assert.match(active.body, /--hl-fill/, "选中胶囊底色（--hl-fill）应落在基础段，不靠末段覆盖");

  const sheets = rules.find(rule => rule.selector === ".excel-sheets");
  assert.ok(sheets, "批次页签行 .excel-sheets 要有定义");
  assert.doesNotMatch(sheets.body, /border-top\s*:/, "底部灰挡板的分隔线必须已删");
  assert.doesNotMatch(sheets.body, /background\s*:\s*var\(--surface-2\)/, "底部灰挡板的灰底必须已删");
  assert.match(sheets.body, /overflow-x\s*:\s*auto/, "横向滚动源只剩它一条（挂在表头批次行里）");
});
// F-1（2026-10-12）：对话轴从"隐形的线"变成可发现的导航件。
//
// 这条闸钉住的是"默认可见"这一条口径：`.conversation-shell .conversation-wheel`
// 的**基础规则**必须自带一个 ≥ .8 的 opacity（不悬停也看得见），且该基础规则的
// 选择器里不得出现 `:hover`——若把可见性重新交给悬停，这里当场失败。
// 同时确认 `.is-empty` 仍走 opacity 的视觉隐藏（几何量测依赖它），没有被改成
// `display: none`。
test("F-1 对话轴默认可见：基础规则 opacity ≥ .8 且不依赖 :hover", async () => {
  const rules = cssRules(await read("styles.css"));

  const base = rules.find(rule => rule.selector === ".conversation-shell .conversation-wheel");
  assert.ok(base, "要找得到 .conversation-shell .conversation-wheel 基础规则");
  assert.ok(!/:hover/.test(base.selector), "基础规则的选择器不得依赖 :hover（默认态就要可见）");
  const opacity = base.body.match(/opacity\s*:\s*([0-9.]+)/);
  assert.ok(opacity, "基础规则必须声明 opacity");
  assert.ok(
    Number(opacity[1]) >= 0.8,
    `对话轴默认 opacity 必须 ≥ .8（不悬停也能看到），当前 ${opacity[1]}`,
  );

  const empty = rules.find(rule => rule.selector.includes(".conversation-wheel.is-empty"));
  assert.ok(empty, "要找得到 .conversation-wheel.is-empty 空态规则");
  assert.match(empty.body, /opacity\s*:\s*0/, ".is-empty 仍以 opacity 隐藏");
  assert.ok(!/display\s*:\s*none/.test(empty.body), ".is-empty 不得改成 display:none（会让轨道高度量成 0）");
});
// ── F-3「资源管理器」子页：页签同族 / 行密度口径 / 唯一滚动源 ──────────────
// 三页的行壳必须走 --row-min-h（B0 规则 3「行密度走 --row-min-h」），`.code-pane-body`
// 是内容区的唯一滚动源（行容器不许再开一条 overflow：两处滚动会互相抢滚轮）。
const ROW_SHELLS = [
  ".code-pane-body .tree-row",
  ".code-pane-body .git-log-line",
  ".code-pane-body .git-commit-row",
  ".code-pane-body .changes-row",
];
const ROW_CONTAINERS = [".worktree-view", ".git-log-view", ".git-commit-files", ".changes-view"];

// bodyOf 把「选择器组里含这个选择器」的规则体按出现顺序拼起来（后写覆盖先写）。
function bodyOf(rules, selector) {
  return rules
    .filter(rule => rule.selector.split(",").map(part => part.trim()).includes(selector))
    .map(rule => rule.body)
    .join(";");
}

test("子页三页的行壳走 --row-min-h 口径、圆角走 --r-md", async () => {
  const rules = cssRules(await read("styles.css"));
  for (const selector of ROW_SHELLS) {
    const body = bodyOf(rules, selector);
    assert.match(body, /min-height:\s*var\(--row-min-h\)/, `${selector} 的行高要走 --row-min-h 口径`);
    assert.match(body, /border-radius:\s*var\(--r-md\)/, `${selector} 的圆角要走 --r-md token`);
  }
  // 提交列表行还有第二个读者：泳道 SVG 的行高，所以它是"固定行高"。
  assert.match(bodyOf(rules, ".code-pane-body .git-log-line"), /height:\s*var\(--row-min-h\)/);
});

test("子页内容区是唯一滚动源：行容器不许自己再开 overflow", async () => {
  const rules = cssRules(await read("styles.css"));
  assert.match(bodyOf(rules, ".code-pane-body"), /overflow-y:\s*auto/, ".code-pane-body 必须仍是纵向滚动容器");
  for (const selector of ROW_CONTAINERS) {
    assert.doesNotMatch(
      bodyOf(rules, selector),
      /overflow(-x|-y)?\s*:/,
      `${selector} 是行容器：滚动只归 .code-pane-body，别在这里再开一层`,
    );
  }
});

test("子页页签与右栏页签同族：--r-md + 既有选中信号 --hl-fill / --hl-ink", async () => {
  const rules = cssRules(await read("styles.css"));
  assert.match(bodyOf(rules, ".code-pane-tab"), /border-radius:\s*var\(--r-md\)/, "页签半径走 --r-md");
  const active = bodyOf(rules, ".code-pane-tab.is-active");
  assert.match(active, /background:\s*var\(--hl-fill\)/, "选中底 = 同色系薄底");
  assert.match(active, /color:\s*var\(--hl-ink\)/, "选中字 = 同色系重字");
  assert.match(active, /box-shadow:\s*none/, "选中态不描边、不投影（不画第二套黑框）");
  // D3：详情 chip 的「当前项」与子页页签同义，视觉也收进同一套（只留一套选中语义）。
  const chip = bodyOf(rules, ".code-pane-head .file-preview-chip.is-active");
  assert.match(chip, /background:\s*var\(--hl-fill\)/, "chip 当前项 = 同一套薄底");
  assert.match(chip, /color:\s*var\(--hl-ink\)/, "chip 当前项 = 同一套重字");
});

// ── F-3 补充（2026-10-12）：行容器不许被网格拉伸线平分容器高度 ──────────────
// 现场（无窗口 headless Chrome，真 index.html + 真渲染件，面板高 613.86 / 633.86px）：
//   · 提交记录 3 条 → 行距 204.61px（= 面板高 ÷ 行数），泳道 SVG 之间断开 174.61px；
//   · 工作区更改 4 项 → `.changes-row` 自身被拉到 124.56px（`.changes-summary` 115.56px）；
//   · 工作树 4 项 → 被拉伸的是 `.tree-node` 外层（201.95 / 141.97px），`.tree-row` 自身
//     仍 30px —— 症状是行距被摊平、树轨连线断开。
// 根因：行容器是 grid，`align-content` 默认是 stretch —— 行数不足时按拉伸线把行摊平
// 分掉容器高度。左栏会话列表 .stack-list 踩过同一个坑（styles.css 第 4 节「现场 A」），
// 同一处分法：`align-content: start` + `grid-auto-rows: max-content`。
const LIST_GRIDS = [".worktree-view", ".git-log-view", ".changes-view"];

test("子页行容器按内容高度排（不被网格拉伸线平分容器高度）", async () => {
  const rules = cssRules(await read("styles.css"));
  for (const selector of LIST_GRIDS) {
    // 收口写在 F-3 段落里（`.code-pane-body.<view>`），也接受直接写在行容器规则上。
    const body = `${bodyOf(rules, selector)};${bodyOf(rules, `.code-pane-body${selector}`)}`;
    assert.match(
      body,
      /align-content:\s*start/,
      `${selector} 必须 align-content: start：默认的 stretch 会按行数把行摊平分掉容器高度`,
    );
    assert.match(
      body,
      /grid-auto-rows:\s*max-content/,
      `${selector} 的行高只许按内容排（grid-auto-rows: max-content）`,
    );
  }
});

test("提交列表行高与泳道 SVG 的 ROW_HEIGHT 是同一份值", async () => {
  const css = (await read("styles.css")).replace(/\/\*[\s\S]*?\*\//g, "");
  const rowMinH = Number(css.match(/--row-min-h:\s*([0-9.]+)px/)?.[1]);
  const js = await read("git-log-view.js");
  const rowHeight = Number(js.match(/const ROW_HEIGHT = ([0-9]+);/)?.[1]);
  assert.ok(Number.isFinite(rowMinH) && Number.isFinite(rowHeight), "两边都要量得到数");
  assert.equal(
    rowHeight,
    rowMinH,
    `ROW_HEIGHT(${rowHeight}) 必须等于 --row-min-h(${rowMinH}px)：行距 ≠ 行高时泳道会在行间断开`,
  );
});
