import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// file-reference-wiring.test.mjs — J-1b 的**结构性**证据：三个入口（`@文件` / `+` 加号 /
// 拖拽）必须共用同一份归一化与同一枚 chip，两处宿主（composer 草稿区 / 对话正文）必须
// 是同一个 chip 组件。纯逻辑由 file-reference.test.mjs 覆盖，这里只钉"接线"：
//   - 归一化只有一份：app.js 不自己判路径（只经 file-drop.js / file-reference.js）；
//   - 候选只有一份：`@` 内联面板与 `+` 选择器都调 fileReferenceCandidates；
//   - 插入只有一份：所有入口都经 applyFileReference；
//   - chip 只有一枚：草稿条与正文都出自 fileReferenceChipHTML（正文经 markdown 的
//     fileRefs 通道）；
//   - 正文里只有路径文本：Submit 签名不变（引用就是正文）。

const read = name => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("app.js 不另写路径判据：三入口只经 file-drop.js / file-reference.js", async () => {
  const app = await read("app.js");
  assert.match(app, /from "\.\/file-reference\.js"/, "app.js 必须引文件引用模块");
  assert.match(app, /from "\.\/file-drop\.js"/, "拖拽入口仍走 file-drop.js");
  // 唯一归一化实现的调用点：拖拽载荷（file-drop 内部）与引用插入。app.js 自己不许
  // 出现第二套判据（形如 `path.startsWith("/")` / `includes("..")` 的本地校验）。
  assert.doesNotMatch(app, /normalizeDropPath\(/, "归一化只在 file-drop.js / file-reference.js 里被调用");
  assert.doesNotMatch(app, /normalizeFilePath\(/, "归一化只在 file-reference.js 里被调用");
});

test("候选只有一份：`@` 内联面板与 `+` 选择器都走 fileReferenceCandidates", async () => {
  const app = await read("app.js");
  const calls = [...app.matchAll(/fileReferenceCandidates\(fileRefIndex, query/g)];
  assert.equal(calls.length, 2, "两个入口（@ 内联面板 / + 选择器）各一处，且都查同一份索引");
  assert.match(app, /function renderFileReferenceSuggestions/, "@ 内联面板的候选渲染");
  assert.match(app, /function renderFilePickerResults/, "+ 选择器的候选渲染");
  // `@` 的候选**不调 Bridge**：后端 Suggestions 表里没有 `@`（它不是前缀）。
  const block = app.slice(app.indexOf("async function updateInlineSuggestions"), app.indexOf("function hideInlineSuggestions"));
  assert.match(block, /fileReferenceQuery\(elements\.prompt\.value, cursor\)/,
    "`@` 判据落在**光标所在的词**上（句子中间的 `@词` 也要给候选）");
  const branch = block.slice(block.indexOf("const reference = fileReferenceQuery"), block.indexOf("const input = elements.prompt.value.trimStart();"));
  assert.doesNotMatch(branch, /invoke\(/, "`@` 分支不调后端建议面（候选只来自已加载的工作区树）");
});

test("`@` 是光标处的标记：句子中间也给候选，Tab 按记录下来的触发字符插入", async () => {
  const app = await read("app.js");
  // 判据取自 file-reference.js 的真实行为（不是字符串匹配）：光标停在一句话中间的
  // `@词` 里命中，停在词外或粘连词（email）不命中。
  const { fileReferenceQuery } = await import(new URL("./file-reference.js", import.meta.url));
  assert.deepEqual(fileReferenceQuery("看看 @src/app.js 的实现", 12), { query: "src/app.js", start: 3, end: 14 });
  assert.equal(fileReferenceQuery("看看 src/app.js 的实现", 8), null, "没有 `@` 就不弹文件候选");
  assert.equal(fileReferenceQuery("mailto:user@example.com", 24), null, "粘连词（email）不误认成引用");
  // 接线侧：光标判据排在 sigil 的"整条输入"判据**前面**，且 Tab / 上下键按
  // state.inlineTrigger 走 —— 否则句子中间的 `@` 会被"首字符不是 sigil"挡掉，或
  // Tab 把触发字符算成正文首字（插错位置）。
  assert.match(app, /const reference = fileReferenceQuery\(elements\.prompt\.value, cursor\);/);
  assert.match(app, /state\.inlineTrigger = FILE_REF_TRIGGER;/);
  assert.match(app, /acceptSuggestion\(state\.inlineSuggestions\[state\.inlineSelected\], state\.inlineTrigger\)/);
  assert.doesNotMatch(app, /acceptSuggestion\([^)]*trimStart\(\)\[0\]/, "不得再从正文首字符现推触发字符");
});

test("插入只有一份：所有入口都经 applyFileReference", async () => {
  const app = await read("app.js");
  assert.match(app, /function acceptFileReference\(/, "唯一插入点");
  assert.match(app, /applyFileReference\(elements\.prompt\.value/, "插入走同一套 apply");
  // 草稿条的 × 删除走同一份识别的删除函数。
  assert.match(app, /removeFileReference\(elements\.prompt\.value/);
  // 三个入口的接线都在场：`@`（内联面板 Accept）/ `+`（file-button → picker）/ 拖拽
  // （file-drop 的 dropPlan，未改）。
  assert.match(app, /elements\["file-button"\]\?\.addEventListener/);
  assert.match(app, /trigger === FILE_REF_TRIGGER/);
  assert.match(app, /dropPlan\(\{ paths: dropPaths/);
});

test("同一枚 chip 的两种宿主：草稿条 + 对话正文（后者只对 user 消息开启）", async () => {
  const [app, components] = await Promise.all([read("app.js"), read("components.js")]);
  assert.match(app, /fileReferenceStripHTML\(elements\.prompt\.value, \{ host: "composer" \}\)/, "草稿宿主");
  assert.match(components, /role === "user" \? markdown\(content, \{ fileRefs: true \}\)/, "正文宿主只认用户消息");
  assert.match(app, /\.message-body \[data-file-ref\]/, "正文 chip 点击开右栏详情");
  // 正文 chip 只是渲染层识别：引用进正文靠的就是 Submit 的正文文本（签名不变）。
  assert.doesNotMatch(app, /fileReferenceStripHTML\([^)]*host: "message"/, "正文宿主由 markdown 渲染，不在 app.js 里另造一套");
});

test("index.html：`+` 加号、草稿 chip 条与选择器面板都在场", async () => {
  const html = await read("index.html");
  for (const id of ["file-button", "composer-file-refs", "composer-file-picker", "composer-file-search", "composer-file-results"]) {
    assert.ok(html.includes(`id="${id}"`), `缺 ${id}`);
  }
  assert.match(html, /<div class="composer-tools">[\s\S]*id="file-button"/, "加号落在 composer 工具条里");
  assert.doesNotMatch(html, /data-trigger="@"/, "命令面板不再有 `@ Team` 触发位（J-1a 的残留一并清掉）");
});

test("styles.css：chip 的皮就地写进所属段落（不追加到文件末尾）", async () => {
  const css = await read("styles.css");
  const chipRule = css.indexOf("\n.file-ref-chip {");
  assert.ok(chipRule > 0, "chip 必须有基础规则（抹掉组件库按钮皮，见 text-button-chrome.test.mjs）");
  assert.ok(css.indexOf(".composer-tools {") < chipRule, "chip 基础段紧跟 composer 工具条那一段（就地写）");
  assert.ok(chipRule < css.length - 4000, "chip 规则不得写在文件最末尾");
  assert.match(css, /\.composer-file-refs \{/, "草稿 chip 条");
  assert.match(css, /\.composer-file-picker \{/, "选择器面板");
  // 正文宿主的就地微调落在 message 段（在那一段里，不是文件末尾）。
  const messageTweak = css.indexOf(".message-body .file-ref-chip");
  assert.ok(messageTweak > 0 && messageTweak < chipRule, "正文 chip 的微调写在 message 段");
  // B0：半径走 token，不新增裸像素半径（只看 chip 这一段自己的声明）。
  const blockEnd = css.indexOf(".composer-file-results {");
  assert.ok(blockEnd > chipRule, "chip 段落要完整（含选择器面板）");
  assert.doesNotMatch(css.slice(chipRule, blockEnd), /border-radius:\s*[0-9.]+px/);
});
