// elements 注册表覆盖度：`elements` 是 `Object.fromEntries([...ids].map(id => [id, getElementById(id)]))`
// 的白名单，app.js 里所有 `elements["x"]` 都必须在这张表里。漏一个的直接后果不是报错，而是
// `if (elements["x"])` 静默为假 —— 2026-09-27 的实例正是顶栏两个诊断徽标（perf-badge / live-diag）
// 因此从不渲染：数据在采、窗口在抖，用户却看不到任何指标。反过来注册表里的 id 又必须真的在
// index.html 里存在，否则拿到的是 null。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("elements 注册表：用到的 key 必须都注册，注册的 id 必须都在 index.html 里", async () => {
  const [app, html] = await Promise.all([read("app.js"), read("index.html")]);

  const block = app.match(/const elements = Object\.fromEntries\(\[([\s\S]*?)\]\.map/);
  assert.ok(block, "找不到 elements 注册表字面量");
  const registry = [...block[1].matchAll(/"([^"]+)"/g)].map(match => match[1]);
  assert.ok(registry.length > 100, `注册表规模异常：${registry.length}`);

  const used = [...new Set([...app.matchAll(/elements\["([^"]+)"\]/g)].map(match => match[1]))];
  const missing = used.filter(key => !registry.includes(key));
  assert.deepEqual(missing, [], `这些 key 被 elements[...] 用到却没注册：${missing.join(", ")}`);

  const ids = new Set([...html.matchAll(/id="([^"]+)"/g)].map(match => match[1]));
  const dangling = registry.filter(id => !ids.has(id));
  assert.deepEqual(dangling, [], `注册表里的这些 id 在 index.html 里不存在：${dangling.join(", ")}`);
});

test("顶栏诊断徽标宿主必须在注册表里（否则徽标静默不落 DOM）", async () => {
  const app = await read("app.js");
  for (const id of ["perf-badge-host", "live-diag-host"]) {
    assert.match(app, new RegExp(`"${id}"`), `${id} 必须注册进 elements`);
    assert.match(app, new RegExp(`elements\\["${id}"\\]\\.appendChild`), `${id} 必须被挂载`);
  }
});
