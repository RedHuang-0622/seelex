import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");

function cssRules(source) {
  const bare = source.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (match[1].trim().startsWith("@")) continue;
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

function px(value) {
  const text = String(value).trim();
  if (text === "auto" || text === "0") return 0;
  const spaces = {
    "var(--sp-1)": 4,
    "var(--sp-2)": 8,
    "var(--sp-3)": 12,
    "var(--sp-4)": 16,
    "var(--sp-5)": 24
  };
  if (spaces[text] !== undefined) return spaces[text];
  const match = text.match(/^(-?\d+(?:\.\d+)?)px$/);
  return match ? Number(match[1]) : null;
}

function marginSides(body) {
  const sides = { top: 0, right: 0, bottom: 0, left: 0 };
  for (const match of body.matchAll(/\bmargin(?:-(top|right|bottom|left))?\s*:\s*([^;]+)/g)) {
    const property = match[1];
    const values = match[2].trim().split(/\s+/);
    if (property) {
      sides[property] = px(values[0]);
      continue;
    }
    const expanded = values.length === 1
      ? [values[0], values[0], values[0], values[0]]
      : values.length === 2
        ? [values[0], values[1], values[0], values[1]]
        : values.length === 3
          ? [values[0], values[1], values[2], values[1]]
          : values;
    sides.top = px(expanded[0]);
    sides.right = px(expanded[1]);
    sides.bottom = px(expanded[2]);
    sides.left = px(expanded[3]);
  }
  return sides;
}

test("圆角容器不得用单侧厚外边距代替布局间距", () => {
  const failures = [];
  for (const rule of cssRules(css)) {
    if (!/border-radius\s*:\s*(?!0(?:px)?\s*(?:;|$))/.test(rule.body)) continue;
    const sides = marginSides(rule.body);
    const pairs = [["top", "bottom"], ["left", "right"]];
    for (const [first, opposite] of pairs) {
      if (sides[first] > 8 && sides[opposite] === 0) {
        failures.push(`${rule.selector}: margin-${first}=${sides[first]}px, margin-${opposite}=0`);
      }
      if (sides[opposite] > 8 && sides[first] === 0) {
        failures.push(`${rule.selector}: margin-${opposite}=${sides[opposite]}px, margin-${first}=0`);
      }
    }
  }
  assert.deepEqual(failures, [], `发现圆角 + 单侧厚 margin：\n${failures.join("\n")}`);
});

test("轨迹使用左表右详情的 Network 主从布局", () => {
  const rules = cssRules(css);
  const main = rules.find(rule => rule.selector === ".trajectory-main");
  assert.ok(main, "必须存在 .trajectory-main");
  assert.match(main.body, /grid-template-columns:/);
  assert.match(main.body, /minmax\(/);
  assert.match(main.body, /border-radius:\s*var\(--r-md\)/);
  assert.match(css, /\.trajectory-results\s*\{[^}]*border-right:/s);
  assert.match(css, /\.trajectory-inspector\s*\{[^}]*overflow:\s*auto/s);
});

test("目标状态信号收进条目内部：不得使用单侧粗边或整块状态底", () => {
  const forbiddenSelectors = [
    ".goal-frame",
    ".goal-board-card",
    ".goal-governance",
    ".goal-history-row",
    ".goal-detail-frame"
  ];
  const failures = [];
  for (const rule of cssRules(css)) {
    const selectors = rule.selector.split(",").map(selector => selector.trim());
    if (!selectors.some(selector => forbiddenSelectors.some(base => selector === base || selector.startsWith(`${base}.`)))) continue;
    if (/border-left(?:-width|-color)?\s*:\s*(?!0(?:px)?\s*(?:;|$))/.test(rule.body)) {
      failures.push(`${rule.selector}: ${rule.body.match(/border-left[^;]*;/)?.[0]?.trim() || "单侧状态轨"}`);
    }
  }
  assert.deepEqual(failures, [], `目标卡片不得拼接左侧状态轨：\n${failures.join("\n")}`);

  const governance = cssRules(css).find(rule => rule.selector === ".goal-governance");
  assert.ok(governance, "必须保留目标治理区");
  assert.match(governance.body, /background:\s*transparent/, "治理区不得铺整块状态底");

  const status = cssRules(css).find(rule => rule.selector === ".goal-gov-status");
  assert.ok(status, "治理状态必须使用行内状态件");
  assert.match(status.body, /border:\s*1px solid var\(--border\)/);
  assert.match(status.body, /border-radius:\s*var\(--r-sm\)/);
});
