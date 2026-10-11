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
