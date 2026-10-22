// 用户消息气泡：无边框、无实色，只有一层极淡的半透明底（"玻璃"，2026-07 用户口径定稿）。
//
// 两版试错都记在这里，别再走回头路：
//   ①只删 background 且什么都不补 → 容器视觉上不存在，用户当场说"你把气泡删了"；
//   ②补 1px solid var(--border) → 用户说"填充色仍然是黑色"/"还是个框"。
// 定稿是"不画边框 + 极淡半透明底"：深色皮肤下比背景亮一档，浅色下比背景白一档，
// 轮廓由那一层透明底透出来，不靠线。
//
// 三条断言：①不得有 border；②background 必须是 color-mix(...透明) 而不是实色；
// ③透明度必须"极淡"（≤50%），否则又变成一块实色面板。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

// cssRules 剥掉注释后把「选择器 { 声明体 }」逐条展开（与 style-baseline.test.mjs 同款；
// styles.css 里没有嵌套规则）。
function cssRules(css) {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (match[1].trim().startsWith("@")) continue;
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

test("用户消息气泡：无边框、极淡半透明底", async () => {
  const rules = cssRules(await read("styles.css"));
  const bodies = rules
    .filter((rule) => rule.selector === ".message.user .message-body")
    .map((rule) => rule.body);
  assert.equal(bodies.length, 1, "用户消息气泡规则必须只有一处（单一实现）");
  const body = bodies[0];

  assert.match(body, /width:\s*fit-content/, "先确认取到的是用户消息气泡规则");
  assert.doesNotMatch(body, /(^|;)\s*border\s*:/, "定稿不画边框：线会被读成「还是一个框」");
  assert.doesNotMatch(
    body,
    /(^|;)\s*background(-color)?\s*:\s*(var\(--[a-z0-9-]+\)|#[0-9a-f]{3,8})\s*(;|$)/i,
    "不得铺实色（var()/十六进制）：实色就是「还是填了色」",
  );

  const glass = body.match(/background(-color)?\s*:\s*color-mix\(\s*in srgb,\s*var\(--surface\)\s+(\d+(?:\.\d+)?)%\s*,\s*transparent\s*\)/i);
  assert.ok(glass, "必须有半透明玻璃底：color-mix(in srgb, var(--surface) <n>%, transparent)");
  const alpha = Number(glass[2]);
  assert.ok(alpha > 0 && alpha <= 50, `半透明底必须「极淡」（>0 且 ≤50%），当前 ${alpha}% 会退回成实色面板`);

  assert.match(body, /margin-left:\s*auto/, "用户消息仍右对齐");
  assert.match(body, /border-radius:\s*var\(--r-xl\)/, "气泡圆角保留");
});
