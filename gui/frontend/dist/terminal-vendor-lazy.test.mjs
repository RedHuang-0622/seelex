// 终端 vendor（xterm.js 277KB / addon-fit.js）从"每次启动同步加载"改为"第一次
// 真的开终端时按需注入"：
//   ① index.html 不再有这两条 <script>（xterm.css 仍同步——它只是一段样式）；
//   ② terminal-panel.js 在 newTerminal 里、createTerminal() 之前 await 注入；
//   ③ 调用方自己注入了工厂时不碰 vendor（测试/宿主行为不变）；
//   ④ 顺带把 scrollback 从上不封顶的 5000 收到 2000（按标签按会话各一份）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("index.html 不再同步加载 xterm 脚本，但样式仍在", async () => {
  const html = await read("index.html");
  assert.doesNotMatch(html, /src="\.\/vendor\/xterm\/xterm\.js"/);
  assert.doesNotMatch(html, /src="\.\/vendor\/xterm\/addon-fit\.js"/);
  assert.match(html, /href="\.\/vendor\/xterm\/xterm\.css"/, "xterm.css 只是样式，继续同步");
});

test("按需注入只发生在首次开终端，且不改变既有同步语义", async () => {
  const source = await read("terminal-panel.js");
  // 门控：只有两个工厂都是默认实现时才注入。
  assert.match(source, /const ownsVendors = !options\.createTerminal && !options\.createFit;/);
  assert.match(source, /ensureVendorScript\(XTERM_SRC/);
  assert.match(source, /ensureVendorScript\(FIT_SRC/);
  // 已就绪走同步路径（newTerminal 保持同步函数），未就绪才 then 到 openTerminal。
  assert.doesNotMatch(source, /async function newTerminal\(\)/);
  assert.match(source, /function newTerminal\(\) \{/);
  const gateAt = source.indexOf("if (!terminalVendorsReady())");
  const thenAt = source.indexOf("loadTerminalVendors().then(openTerminal");
  const openAt = source.indexOf("function openTerminal()");
  const createAt = source.indexOf("term = createTerminal();");
  for (const [name, at] of [["门控", gateAt], ["then 分支", thenAt], ["openTerminal", openAt], ["createTerminal", createAt]]) {
    assert.ok(at > 0, `${name} 应存在`);
  }
  assert.ok(gateAt < thenAt && thenAt < openAt && openAt < createAt, "顺序：门控 → 注入后开 → 建实例");
});

test("终端回滚缓冲有上限（不再 5000 行 × 每标签 × 每会话）", async () => {
  const source = await read("terminal-panel.js");
  assert.match(source, /scrollback: 2000/);
  assert.doesNotMatch(source, /scrollback: 5000/);
});
