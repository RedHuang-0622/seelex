import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  createThemeController,
  findSkin,
  modeID,
  normalizeThemeManifest,
  resolveMode,
  resolveSkin,
  skinHref,
  skinID,
  SKIN_LINK_ID,
  SKIN_STORAGE_KEY,
  MODE_STORAGE_KEY
} from "./theme.js";

const manifest = normalizeThemeManifest(JSON.parse(
  await readFile(new URL("./themes/manifest.json", import.meta.url), "utf8")
));

// 两轴契约：
//  · 深浅（mode）：styles.css 的 `:root`（浅色）与 `:root[data-theme="dark"]`（深色）
//    提供**中性基座**（面/文本/描边/状态/半透明面/投影/刻度）；
//  · 皮肤（skin）：themes/<skin>.css 只提供**品牌 token**（主信号 + 环境渐变），
//    且必须同时给浅色与深色两个变体（`--skin-*-light` / `--skin-*-dark`）。
const NEUTRAL_TOKENS = [
  "--bg", "--panel", "--surface", "--surface-2", "--surface-3", "--border", "--border-strong",
  "--border-hairline", "--border-field",
  "--surface-blur", "--panel-solid", "--surface-glass", "--overlay", "--surface-opaque",
  "--text", "--muted", "--faint", "--text-bright", "--text-strong", "--text-soft", "--text-mid", "--text-dim",
  "--status-running", "--status-done", "--status-failed", "--status-info", "--status-idle",
  "--tint-running", "--tint-done", "--tint-failed", "--tint-info",
  "--border-running", "--border-done", "--border-failed", "--border-info",
  "--code-bg", "--tick", "--tick-hot"
];

const BRAND_TOKENS = [
  "--skin-accent-light", "--skin-accent-strong-light", "--skin-on-accent-light", "--skin-gradient-light",
  "--skin-accent-dark", "--skin-accent-strong-dark", "--skin-on-accent-dark", "--skin-gradient-dark"
];

test("theme: skin ids are restricted to safe slugs", () => {
  assert.equal(skinID("graphite"), "graphite");
  assert.equal(skinID("Paper-Light"), "paper-light");
  assert.equal(skinID("../secret"), "");
  assert.equal(skinID("a/b"), "");
  assert.equal(skinID("../../etc/passwd"), "");
  assert.equal(skinID(""), "");
  assert.equal(skinID(undefined), "");
});

test("theme: mode ids are restricted to light/dark", () => {
  assert.equal(modeID("light"), "light");
  assert.equal(modeID("Dark"), "dark");
  assert.equal(modeID("Dim"), "");
  assert.equal(modeID(""), "");
  assert.equal(modeID(undefined), "");
});

test("theme: skin hrefs stay inside themes/", () => {
  assert.equal(skinHref({ id: "qoder", file: "" }), "");
  assert.equal(skinHref({ id: "verdigris", file: "themes/verdigris.css" }), "themes/verdigris.css");
  assert.equal(skinHref({ id: "verdigris", file: "/etc/passwd" }), "");
  assert.equal(skinHref({ id: "verdigris", file: "themes/../secret.css" }), "");
  assert.equal(skinHref({ id: "verdigris", file: "https://evil.example/x.css" }), "");
  assert.equal(skinHref({ id: "verdigris", file: "themes/verdigris.css?v=2" }), "");
});

test("theme: manifest drops malformed entries and keeps usable defaults", () => {
  const normalized = normalizeThemeManifest({
    schema_version: 2,
    default_skin: "missing",
    default_mode: "dim",
    skins: [
      { id: "good", name: "好皮肤", file: "themes/good.css", swatches: ["#fff"] },
      { id: "good", name: "重复" },
      { id: "../bad", name: "越界" },
      null,
      { name: "没有 id" }
    ],
    modes: [{ id: "dark", name: "深色" }, { id: "weird" }, null]
  });
  assert.deepEqual(normalized.skins.map(skin => skin.id), ["good"]);
  assert.deepEqual(normalized.modes.map(mode => mode.id), ["dark"]);
  assert.equal(normalized.defaultSkinId, "good");
  assert.equal(normalized.defaultModeId, "dark");
  assert.equal(normalizeThemeManifest({}).skins.length, 0);
  assert.equal(normalizeThemeManifest({}).defaultModeId, "light");
});

test("theme: shipped manifest lists 5 skins and 2 modes pointing at real files", () => {
  assert.equal(manifest.defaultSkinId, "qoder");
  assert.equal(manifest.defaultModeId, "light");
  assert.deepEqual(manifest.skins.map(skin => skin.id), ["qoder", "graphite", "verdigris", "paper", "silver"]);
  assert.deepEqual(manifest.modes.map(mode => mode.id), ["light", "dark"]);
  for (const skin of manifest.skins) {
    assert.ok(skin.name, `${skin.id} 需要名字`);
    if (!skin.file) continue;
    assert.match(skin.file, /^themes\/[a-z0-9-]+\.css$/);
  }
});

// 皮肤契约校验：随包皮肤只覆盖品牌 token，且浅色/深色两个变体齐备。
test("theme: shipped skins cover the whole brand-token contract", async () => {
  for (const skin of manifest.skins) {
    if (!skin.file) continue;
    const css = await readFile(new URL(`./${skin.file}`, import.meta.url), "utf8");
    const code = css.replace(/\/\*[\s\S]*?\*\//g, "");
    for (const token of BRAND_TOKENS) {
      assert.match(css, new RegExp(`${token}:`), `${skin.id} 缺少 ${token}`);
    }
    assert.doesNotMatch(code, /!important/, `${skin.id} 不允许用 !important`);
    // 皮肤包只允许 token 覆盖：出现选择器就说明 token 契约不够用，评审要过问。
    assert.match(code, /^\s*:root\s*\{/m, `${skin.id} 必须只覆盖 :root token`);
    assert.equal((code.match(/:root/g) || []).length, 1, `${skin.id} 只应出现一个 :root 选择器`);
  }
});

// 深浅契约校验：styles.css 必须给出浅色与深色两套中性基座，且把皮肤 token 桥接进品牌位。
test("theme: styles.css ships both mode bases and bridges skin tokens", async () => {
  const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");
  assert.match(css, /:root\s*\{/, "缺少浅色基座");
  assert.match(css, /:root\[data-theme="dark"\]\s*\{/, "缺少深色基座");
  const darkIndex = /:root\[data-theme="dark"\]\s*\{/.exec(css).index;
  const darkBlock = css.slice(darkIndex, css.indexOf("}", darkIndex));
  for (const token of NEUTRAL_TOKENS) {
    assert.match(css, new RegExp(`${token}:`), `浅色基座缺少 ${token}`);
    assert.match(darkBlock, new RegExp(`${token}:`), `深色基座缺少 ${token}`);
  }
  // 品牌位桥接：浅色 / 深色各自从皮肤变体取值。
  assert.match(css, /--accent:\s*var\(--skin-accent-light/);
  assert.match(darkBlock, /--accent:\s*var\(--skin-accent-dark/);
  assert.match(css, /--shell-gradient:\s*var\(--skin-gradient-light/);
  assert.match(darkBlock, /--shell-gradient:\s*var\(--skin-gradient-dark/);
});

// 选中态高亮的「同一份口径」（用户 2026-09-24 点名两处：右栏 状态/工作台/资源管理器 的
// 按钮高亮、以及"圆角配上一个黑色的下外框"）。判据落成四条静态断言——它们不描述理想，
// 只描述这一轮交付的写法；口径改了就该改这里，别让它悄悄漂回去。
test("theme: 选中态高亮只由皮肤派生（无黑块 / 无圆角配黑下条）", async () => {
  const raw = await readFile(new URL("./styles.css", import.meta.url), "utf8");
  const css = raw.replace(/\/\*[\s\S]*?\*\//g, ""); // 注释里可以谈口径，代码里不行

  // 1) 派生 token 只声明一次（浅色基座）：深浅两轴由 --accent / --surface 桥接自动跟随，
  //    皮肤换肤不用各自再声明一遍（否则五套皮肤 × 深浅就是十份事实）。
  assert.equal((css.match(/--hl-fill:/g) || []).length, 1, "--hl-fill 只应在浅色基座声明一次");
  assert.equal((css.match(/--hl-ink:/g) || []).length, 1, "--hl-ink 只应在浅色基座声明一次");
  assert.match(css, /--hl-fill:\s*color-mix\(in srgb,\s*var\(--accent\)/, "高亮底必须由皮肤主信号派生");
  assert.match(css, /--hl-ink:\s*var\(--accent-strong\)/, "高亮字必须取同色系重色");

  // 2) 黑名单一：单边下条（`inset 0 -Npx 0` 压在圆角上就是"圆角 + 黑下框"）。
  assert.doesNotMatch(css, /inset 0 -\d+px 0/, "页签家族不许再画单边下条");

  // 3) 黑名单二：硬编码的"上圆下直"（`Npx Npx 0 0`）——页签老写法，半径必须走 token。
  assert.doesNotMatch(css, /border-radius:\s*\d+px \d+px 0 0/, "页签半径必须走 token");

  // 4) 黑名单三：带圆角的规则块里出现纯黑（描边 / 投影）——那正是用户读到的"黑外框"。
  for (const m of css.matchAll(/([^{}]*)\{([^{}]*)\}/g)) {
    const selector = m[1].trim().split("\n").pop().trim();
    if (/border-radius/.test(m[2]) && /rgba\(0,\s*0,\s*0/.test(m[2])) {
      assert.fail(`带圆角的规则块里出现纯黑：${selector}`);
    }
  }

  // 5) 本轮的几何口径：中栏纸 8px 圆角（子节点由既有 overflow 裁圆）、页签条跟纸同色。
  assert.match(css, /\.workspace\s*\{\s*border-radius:\s*var\(--r-md\)/, "内容纸必须是 --r-md 圆角");
  assert.match(css, /\.conversation-tabs\s*\{\s*background:\s*transparent/, "页签条必须跟纸同色");

  // 6) 过圆角不回潮：全文件胶囊计数是收敛指标（OPTIMIZATION-PLAN P1-1 记 32）。
  const pills = (css.match(/border-radius:\s*999px/g) || []).length;
  assert.ok(pills <= 32, `胶囊半径回潮：${pills} 处 > 32`);
});

test("theme: 换肤/切深浅把「已生效」回流给 token 消费方（外链皮肤等 CSS 落地）", () => {
  const created = [];
  const byId = new Map();
  const head = { appendChild(node) { created.push(node); byId.set(node.id, node); } };
  const doc = {
    documentElement: { dataset: {} },
    head,
    createElement() {
      return {
        id: "", rel: "",
        attrs: {},
        setAttribute(name, value) { this.attrs[name] = value; },
        remove() { byId.delete(this.id); }
      };
    },
    getElementById(id) { return byId.get(id) || null; }
  };
  const store = new Map();
  const storage = {
    getItem(key) { return store.get(key) ?? null; },
    setItem(key, value) { store.set(key, value); }
  };

  const applied = [];
  const controller = createThemeController({ document: doc, storage, manifest, onApplied: () => applied.push(1) });

  // 外链皮肤：换 <link href> 是异步资源，先立即回流一次兜底
  assert.equal(controller.applySkin("silver").id, "silver");
  const link = created[0];
  assert.equal(link.id, SKIN_LINK_ID);
  assert.equal(link.attrs.href, "themes/silver.css");
  assert.equal(applied.length, 1);
  // CSS 真正生效那一刻再回流一次：消费方这时读 token 才是新皮肤的色
  assert.equal(typeof link.onload, "function");
  link.onload();
  assert.equal(applied.length, 2);

  // 切深浅不换皮肤 <link>，只改 <html data-theme>
  applied.length = 0;
  assert.equal(controller.applyMode("dark"), "dark");
  assert.equal(doc.documentElement.dataset.theme, "dark");
  assert.equal(applied.length, 1);
  assert.equal(link.attrs.href, "themes/silver.css");

  // 回流方抛错不能把换肤本身带崩
  const noisy = createThemeController({ document: doc, storage, manifest, onApplied: () => { throw new Error("boom"); } });
  assert.equal(noisy.applySkin("paper").id, "paper");
  assert.equal(noisy.applyMode("light"), "light");
  assert.equal(doc.documentElement.dataset.theme, "light");
});

test("theme: controller keeps skin and mode as independent persisted axes", () => {
  const created = [];
  const byId = new Map();
  const head = { appendChild(node) { created.push(node); byId.set(node.id, node); } };
  const doc = {
    documentElement: { dataset: {} },
    head,
    createElement() {
      return { id: "", rel: "", attrs: {}, setAttribute(name, value) { this.attrs[name] = value; }, remove() { byId.delete(this.id); } };
    },
    getElementById(id) { return byId.get(id) || null; }
  };
  const store = new Map();
  const storage = { getItem(key) { return store.get(key) ?? null; }, setItem(key, value) { store.set(key, value); } };

  const controller = createThemeController({ document: doc, storage, manifest });
  assert.equal(controller.current().skin.id, "qoder");
  assert.equal(controller.current().mode, "light");

  // 轴一：皮肤
  assert.equal(controller.applySkin("verdigris").id, "verdigris");
  assert.equal(store.get(SKIN_STORAGE_KEY), "verdigris");
  assert.equal(doc.documentElement.dataset.theme, undefined);

  // 轴二：深浅
  assert.equal(controller.applyMode("dark"), "dark");
  assert.equal(store.get(MODE_STORAGE_KEY), "dark");
  assert.equal(doc.documentElement.dataset.theme, "dark");
  assert.equal(controller.current().skin.id, "verdigris");
  assert.equal(created[0].attrs.href, "themes/verdigris.css");

  // 未知 id 各自退回默认轴的值
  assert.equal(controller.applySkin("does-not-exist").id, "qoder");
  assert.equal(controller.applyMode("dim"), "light");

  // 两轴的记忆互相独立恢复
  store.set(SKIN_STORAGE_KEY, "paper");
  store.set(MODE_STORAGE_KEY, "dark");
  const restored = createThemeController({
    document: { documentElement: { dataset: {} }, head, createElement: doc.createElement, getElementById: () => null },
    storage,
    manifest
  });
  assert.equal(restored.current().skin.id, "paper");
  assert.equal(restored.current().mode, "dark");
  restored.apply();
  assert.equal(store.get(SKIN_STORAGE_KEY), "paper");
  assert.equal(store.get(MODE_STORAGE_KEY), "dark");

  assert.equal(findSkin(manifest, "nope"), null);
  assert.equal(resolveSkin(manifest, "", "paper").id, "paper");
  assert.equal(resolveSkin(manifest, "verdigris", "paper").id, "verdigris");
  assert.equal(resolveMode(manifest, "", "dark"), "dark");
  assert.equal(resolveMode(manifest, "light", "dark"), "light");
  assert.equal(resolveMode(manifest, "", "dim"), "light");
});
