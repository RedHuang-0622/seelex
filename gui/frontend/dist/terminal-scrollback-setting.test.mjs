// 回滚行数从"写死在代码里的常量"改成**设置项**（默认 2000），同批还去掉了弹窗遮罩
// 的背景模糊。这条测试钉住 app 层的三处拼接点（index.html 的下拉框、app.js 的注册与
// 读写接线、styles.css 的遮罩规则）；控制器级行为（开标签生效 / 就地生效 / 落盘读回）
// 在 terminal-panel-controller.test.mjs，纯函数口径在 terminal-panel.test.mjs。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("设置面板有回滚行数下拉框：默认 2000，选项都是合法值", async () => {
  const html = await read("index.html");
  const select = html.match(/<select id="terminal-scrollback">([\s\S]*?)<\/select>/);
  assert.ok(select, "设置面板里要有 #terminal-scrollback");
  const body = select[1];
  for (const value of [1000, 2000, 5000, 10000]) {
    assert.match(body, new RegExp(`<option value="${value}"`), `要有 ${value} 行选项`);
  }
  assert.match(body, /<option value="2000" selected>/, "静态默认选中 2000");
});

test("app.js 注册并接线：change 落盘 + 立刻作用到已打开的标签", async () => {
  const source = await read("app.js");
  assert.match(source, /"terminal-scrollback"/, "elements 注册表要有它（否则又是静默失效）");
  assert.match(source, /elements\["terminal-scrollback"\]\?\.addEventListener\("change"/);
  assert.match(source, /terminalPanel\.setScrollback\(event\.target\.value\)/);
  assert.match(source, /terminalPanel\.scrollback\(\)/, "启动时要把当前值填回下拉框");
  // 读回当前值的调用必须在面板构造之后（terminalPanel 是 const，之前引用会 TDZ）
  const panelAt = source.indexOf("const terminalPanel = createTerminalPanel({");
  const syncAt = source.indexOf("syncTerminalScrollbackSelect();");
  assert.ok(panelAt > 0 && syncAt > panelAt, "syncTerminalScrollbackSelect() 要在面板构造之后");
});

test("弹窗遮罩不再做背景模糊，半透明色照旧", async () => {
  const css = await read("styles.css");
  const rule = css.match(/\.modal \{[^}]*\}/);
  assert.ok(rule, "要找得到 .modal 规则");
  assert.doesNotMatch(rule[0], /backdrop-filter/, "弹窗遮罩不做背景模糊");
  assert.match(rule[0], /background: var\(--overlay\)/, "仍靠 --overlay 半透明色压暗背景");
  // 弹窗系的任何规则都不该再引回模糊（.modal-card / .modal-* 一起看）；
  // 先剥掉注释再扫，免得注释里提到的 backdrop-filter 被当成选择器。
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const block of bare.match(/[^{}]+\{[^}]*\}/g) || []) {
    if (/\.modal/.test(block.split("{")[0])) {
      assert.doesNotMatch(block, /backdrop-filter/, `弹窗系规则不该有模糊：${block.split("{")[0].trim()}`);
    }
  }
});
