// 设置弹窗必须「滚得动 + 装得下」。
//
// 现场：设置面板里叠了 存储 / 外观（皮肤 + 深浅两排选择器）/ 终端 三段，比小窗口
// 还高。`.modal` 是 `position: fixed` 的网格遮罩（place-items: center）、自己不滚，
// `.settings-card` 也没有高度上限——卡片一高过视口，上下两端就被切在屏幕外，滚轮
// 没有任何滚动容器可以落，底部那颗「保存并切换」永远点不着。
//
// 口径：给卡片封顶（不超过视口）+ 自己滚，同 .runtime-card 的 max-height +
// overflow:auto。这条测试钉住 width/max-height/overflow 三件套，顺带钉住设置面板
// 确实是「高内容」——内容变矮回到一屏内时，这条护栏该一起改。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");
const rule = (css, selector) => {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  return bare.match(new RegExp(`^${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")} \\{[^}]*\\}`, "m"))?.[0] || "";
};

test("设置弹窗：宽度随窗口收，高度封顶并自己滚", async () => {
  const css = await read("styles.css");
  const card = rule(css, ".settings-card");
  assert.ok(card, "要找得到 .settings-card 规则");

  // 宽度：窄窗口下让出 28px，不把遮罩撑破。
  assert.match(card, /width:\s*min\(500px,\s*calc\(100vw - 28px\)\)/, "宽度要随窗口收缩");
  // 高度：不高于视口（`.modal` 上下各 24px 内边距 → 100vh - 48px），大屏再封 760px。
  assert.match(card, /max-height:\s*min\(760px,\s*calc\(100vh - 48px\)\)/, "高度要封顶到视口内");
  assert.match(card, /overflow-y:\s*auto/, "卡片自己就是滚动容器（否则滚轮无处可落）");
  assert.match(card, /overscroll-behavior:\s*contain/, "滚到底不把滚动传给身后的对话区");
});

test("遮罩不替卡片滚：fixed 网格 + 没有自己的滚动容器", async () => {
  const css = await read("styles.css");
  const overlay = rule(css, ".modal");
  assert.ok(overlay, "要找得到 .modal 规则");
  assert.match(overlay, /position:\s*fixed/, "遮罩是全窗 fixed 层");
  assert.match(overlay, /place-items:\s*center/, "卡片居中（所以溢出的两端无从够到——见上一条测试）");
  assert.doesNotMatch(overlay, /overflow(-y)?:\s*auto/, "遮罩不复制一份滚动：唯一的滚动容器是卡片");
});

test("设置面板确实是「三段 + 动作行」的高内容", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  assert.match(modal[0], /id="settings-modal-title"/, "标题还在");
  for (const marker of ["storage-backend", "theme-picker", "mode-picker", "terminal-scrollback"]) {
    assert.match(modal[0], new RegExp(`id="${marker}"`), `设置面板要保留 ${marker}`);
  }
  assert.match(modal[0], /id="storage-save"/, "底部动作行要在（正是被切掉的那一颗）");
});
