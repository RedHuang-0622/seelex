import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  createThemeController,
  findTheme,
  normalizeThemeManifest,
  resolveTheme,
  THEME_STORAGE_KEY,
  themeHref,
  themeID,
  themeMode
} from "./theme.js";

const manifest = normalizeThemeManifest(JSON.parse(
  await readFile(new URL("./themes/manifest.json", import.meta.url), "utf8")
));

test("theme: ids are restricted to safe slugs", () => {
  assert.equal(themeID("graphite"), "graphite");
  assert.equal(themeID("Paper-Light"), "paper-light");
  assert.equal(themeID("../secret"), "");
  assert.equal(themeID("a/b"), "");
  assert.equal(themeID("../../etc/passwd"), "");
  assert.equal(themeID(""), "");
  assert.equal(themeID(undefined), "");
});

test("theme: skin hrefs stay inside themes/", () => {
  assert.equal(themeHref({ id: "graphite", file: "" }), "");
  assert.equal(themeHref({ id: "verdigris", file: "themes/verdigris.css" }), "themes/verdigris.css");
  assert.equal(themeHref({ id: "verdigris", file: "/etc/passwd" }), "");
  assert.equal(themeHref({ id: "verdigris", file: "themes/../secret.css" }), "");
  assert.equal(themeHref({ id: "verdigris", file: "https://evil.example/x.css" }), "");
  assert.equal(themeHref({ id: "verdigris", file: "themes/verdigris.css?v=2" }), "");
});

test("theme: manifest drops malformed entries and keeps a usable default", () => {
  const normalized = normalizeThemeManifest({
    schema_version: 1,
    default: "missing",
    themes: [
      { id: "good", name: "好皮肤", mode: "light", file: "themes/good.css", swatches: ["#fff"] },
      { id: "good", name: "重复" },
      { id: "../bad", name: "越界" },
      null,
      { name: "没有 id" }
    ]
  });
  assert.deepEqual(normalized.themes.map(theme => theme.id), ["good"]);
  assert.equal(normalized.defaultId, "good");
  assert.equal(normalized.themes[0].mode, "light");
  assert.equal(normalizeThemeManifest({}).themes.length, 0);
  assert.equal(themeMode({ mode: "weird" }), "dark");
});

test("theme: shipped manifest points at real skin files", () => {
  assert.equal(manifest.defaultId, "graphite");
  assert.deepEqual(manifest.themes.map(theme => theme.id), ["graphite", "verdigris", "paper"]);
  for (const theme of manifest.themes) {
    assert.ok(theme.name, `${theme.id} 需要名字`);
    if (!theme.file) continue;
    assert.match(theme.file, /^themes\/[a-z0-9-]+\.css$/);
  }
});

// 皮肤契约校验：随包皮肤必须覆盖契约里的全部 token，否则切肤会漏色。
test("theme: shipped skins cover the whole token contract", async () => {
  const required = [
    "--bg", "--panel", "--surface", "--surface-2", "--surface-3", "--border", "--border-strong",
    "--surface-blur", "--panel-solid", "--surface-glass", "--overlay", "--surface-opaque",
    "--text", "--muted", "--faint", "--text-bright", "--text-strong", "--text-soft", "--text-mid", "--text-dim",
    "--accent", "--accent-strong", "--on-accent",
    "--status-running", "--status-done", "--status-failed", "--status-info", "--status-idle",
    "--tint-running", "--tint-done", "--tint-failed", "--tint-info",
    "--border-running", "--border-done", "--border-failed", "--border-info",
    "--code-bg", "--tick", "--tick-hot"
  ];
  for (const theme of manifest.themes) {
    if (!theme.file) continue;
    const css = await readFile(new URL(`./${theme.file}`, import.meta.url), "utf8");
    const code = css.replace(/\/\*[\s\S]*?\*\//g, "");
    for (const token of required) {
      assert.match(css, new RegExp(`${token}:`), `${theme.id} 缺少 ${token}`);
    }
    assert.doesNotMatch(code, /!important/, `${theme.id} 不允许用 !important`);
    // 皮肤包只允许 token 覆盖：出现选择器就说明 token 契约不够用，评审要过问。
    assert.match(code, /^\s*:root\s*\{/m, `${theme.id} 必须只覆盖 :root token`);
  }
});

test("theme: controller switches skins, mode and persistence", () => {
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

  const controller = createThemeController({ document: doc, storage, manifest });
  assert.equal(controller.current().id, "graphite");
  assert.equal(controller.apply("verdigris").id, "verdigris");
  assert.equal(doc.documentElement.dataset.theme, "dark");
  const link = created[0];
  assert.equal(link.attrs.href, "themes/verdigris.css");
  assert.equal(store.get(THEME_STORAGE_KEY), "verdigris");

  assert.equal(controller.apply("paper").id, "paper");
  assert.equal(doc.documentElement.dataset.theme, "light");
  assert.equal(link.attrs.href, "themes/paper.css");

  // 未知 id 退回默认皮肤；默认皮肤移除皮肤链接。
  assert.equal(controller.apply("does-not-exist").id, "graphite");
  assert.equal(store.get(THEME_STORAGE_KEY), "graphite");

  // 上次选择在构造时恢复。
  store.set(THEME_STORAGE_KEY, "paper");
  const restored = createThemeController({ document: { documentElement: { dataset: {} }, head, createElement: doc.createElement, getElementById: () => null }, storage, manifest });
  assert.equal(restored.current().id, "paper");
  assert.equal(findTheme(manifest, "nope"), null);
  assert.equal(resolveTheme(manifest, "", "paper").id, "paper");
  assert.equal(resolveTheme(manifest, "verdigris", "paper").id, "verdigris");
});
