import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  MOTION_DRAG_GUARD_MS,
  MOTION_ENTER_MS,
  MOTION_EXIT_MS,
  MOTION_FEEDBACK_MS,
  MOTION_ONE_SHOT_MS,
  MOTION_SLOW_MS,
  MOTION_STATE_MS
} from "./motion.js";

const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");

function rootBlock() {
  const match = css.match(/:root\s*\{[\s\S]*?\n\}/);
  assert.ok(match, "styles.css 必须保留浅色 :root token 基座");
  return match[0];
}

function cssRules(source) {
  const bare = source.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

function motionDeclarations() {
  return cssRules(css)
    .flatMap(rule => [...rule.body.matchAll(/\b(transition|animation)\s*:\s*([^;]+)/g)]
      .map(match => ({ selector: rule.selector, property: match[1], value: match[2].trim() })));
}

test("motion token：宪法时长、缓动、stagger 只由 :root 单一供源", () => {
  const root = rootBlock();
  const expected = {
    "--duration-micro": "100ms",
    "--duration-fast": "150ms",
    "--duration-normal": "250ms",
    "--duration-slow": "400ms",
    "--duration-slower": "600ms",
    "--duration-shimmer": "1.8s",
    "--motion-stagger": "40ms",
    "--motion-sequence-max": "500ms"
  };
  for (const [token, value] of Object.entries(expected)) {
    assert.match(root, new RegExp(`${token}:\\s*${value.replace(".", "\\.")}\\s*;`), `${token} 必须为 ${value}`);
  }
  for (const token of ["--ease-out", "--ease-in", "--ease-in-out", "--ease-linear"]) {
    assert.match(root, new RegExp(`${token}:\\s*[^;]+;`), `${token} 必须在 :root 定义`);
  }
});

test("motion token：旧 --dur-* 只作为新 token 的 alias", () => {
  const root = rootBlock();
  const aliases = {
    "--dur-fast": "--duration-fast",
    "--dur-med": "--duration-normal",
    "--dur-slow": "--duration-slow"
  };
  for (const [legacy, canonical] of Object.entries(aliases)) {
    assert.match(root, new RegExp(`${legacy}:\\s*var\\(${canonical}\\)\\s*;`), `${legacy} 只能是 ${canonical} 的 alias`);
    assert.doesNotMatch(root, new RegExp(`${legacy}:\\s*[0-9.]+m?s\\s*;`), `${legacy} 不得保留第二份数值`);
  }
});

test("motion contract：不出现通用 spinner、3D 旋转或布局属性动画", () => {
  assert.doesNotMatch(css, /runtime-spin/, "对话尾部不得保留通用 spinner keyframe");
  assert.doesNotMatch(css, /rotate(?:X|Y|Z)\s*\(|rotate3d\s*\(/i, "禁止 3D/翻转式状态动画");
  assert.doesNotMatch(css, /perspective\s*:/i, "禁止为普通状态变化引入 3D perspective");
  for (const declaration of motionDeclarations()) {
    assert.doesNotMatch(
      declaration.value,
      /\b(width|height|top|left|right|bottom|margin|padding|display)\b/,
      `${declaration.selector} 不得以 ${declaration.property} 动画布局/离散属性`
    );
  }
});

test("motion contract：所有 transition/animation 使用 token，不写任意时长或裸 easing", () => {
  for (const declaration of motionDeclarations()) {
    if (declaration.value === "none") continue;
    assert.doesNotMatch(
      declaration.value,
      /\b\d+(?:\.\d+)?m?s\b/,
      `${declaration.selector} 的 ${declaration.property} 含硬编码时长：${declaration.value}`
    );
    const withoutTokens = declaration.value.replace(/var\([^)]*\)/g, "");
    assert.doesNotMatch(
      withoutTokens,
      /(^|[\s,])(ease(?:-in|-out|-in-out)?|linear)(?=$|[\s,])/,
      `${declaration.selector} 的 ${declaration.property} 含裸 easing：${declaration.value}`
    );
  }
});

test("motion contract：工具文字 shimmer 是唯一 1.8s、linear、transform 的局部循环", () => {
  const shimmer = css.match(/@keyframes\s+tool-running-text-shimmer\s*\{([\s\S]*?)\n\}/);
  assert.ok(shimmer, "必须存在唯一工具文字 shimmer");
  assert.match(shimmer[1], /transform\s*:\s*translateX\(/);
  assert.doesNotMatch(shimmer[1], /\b(top|left|width|height|background-position)\s*:/);
  assert.match(css, /animation:\s*tool-running-text-shimmer\s+var\(--duration-shimmer\)\s+var\(--ease-linear\)/);
  assert.equal((css.match(/@keyframes\s+tool-running-text-shimmer/g) || []).length, 1);
  assert.doesNotMatch(css, /\.tool-run-activity/);
});

test("motion contract：reduced-motion 保留静态活动线与焦点终态", () => {
  const reduced = css.match(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{([\s\S]*?)\n\}/g)?.join("\n") || "";
  assert.match(reduced, /\.chat-chip-name\[data-tool-label\]::after/);
  assert.match(reduced, /animation:\s*none/);
  assert.match(css, /:focus-visible/);
  assert.doesNotMatch(reduced, /outline:\s*none/, "reduced-motion 不得移除焦点可见性");
  assert.match(css, /\.trajectory-row:focus-visible\s*\{[^}]*outline:\s*1px solid var\(--focus-ring\)/s);
});

test("motion contract：keyframes 只动画白名单属性，不移动布局", () => {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const marker = "@keyframes";
  let from = 0;
  while (true) {
    const start = bare.indexOf(marker, from);
    if (start < 0) break;
    const open = bare.indexOf("{", start);
    assert.ok(open > start, "@keyframes 必须有声明体");
    let depth = 0;
    let end = -1;
    for (let index = open; index < bare.length; index += 1) {
      if (bare[index] === "{") depth += 1;
      if (bare[index] === "}") depth -= 1;
      if (depth === 0) {
        end = index;
        break;
      }
    }
    assert.ok(end > open, "@keyframes 声明体必须闭合");
    const name = bare.slice(start + marker.length, open).trim();
    const body = bare.slice(open + 1, end);
    assert.doesNotMatch(
      body,
      /\b(width|height|top|left|right|bottom|margin|padding|display)\s*:/,
      `${name} 不得动画布局/离散属性`
    );
    from = end + 1;
  }
});

test("motion contract：JS 兜底时长与 CSS token 同名同值，交互节奏固定", async () => {
  assert.deepEqual(
    {
      MOTION_FEEDBACK_MS,
      MOTION_STATE_MS,
      MOTION_ENTER_MS,
      MOTION_SLOW_MS,
      MOTION_ONE_SHOT_MS
    },
    {
      MOTION_FEEDBACK_MS: 100,
      MOTION_STATE_MS: 150,
      MOTION_ENTER_MS: 250,
      MOTION_SLOW_MS: 400,
      MOTION_ONE_SHOT_MS: 500
    }
  );
  assert.equal(MOTION_EXIT_MS, MOTION_STATE_MS);
  assert.equal(MOTION_DRAG_GUARD_MS, 300);
  assert.match(css, /\.app-shell button:active[\s\S]*?transition-duration:\s*var\(--duration-micro\)/);
  assert.doesNotMatch(css, /@keyframes\s+(tooltip-in|view-enter|view-exit|card-draw|pane-draw)/);
  const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
  assert.match(app, /animateSpring\(el, "state"/);
  assert.match(app, /animateSpring\(card, "enter"/);
  assert.match(app, /animateSpring\(card, "exit"/);
  assert.match(app, /animateSpring\(panel, "enter"/);
  assert.match(app, /animateSpring\(panel, "exit"/);
});

test("reduced-motion 判据只在 motion.js 保留一份", async () => {
  for (const name of ["app.js", "trajectory-view.js", "components.js", "conversation-view.js", "chat-view.js"]) {
    const source = await readFile(new URL(`./${name}`, import.meta.url), "utf8");
    assert.doesNotMatch(source, /matchMedia[^\n]*prefers-reduced-motion/, `${name} 不得复制 reduced-motion 判据`);
  }
});
