import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { fileQueueText, normalizeDropPath } from "./file-drop.js";
import {
  FILE_REF_CANDIDATE_LIMIT,
  FILE_REF_TRIGGER,
  applyFileReference,
  fileReferenceCandidates,
  fileReferenceChipHTML,
  fileReferenceIndex,
  fileReferencePaths,
  fileReferenceQuery,
  fileReferenceStripHTML,
  fileReferenceTokens,
  normalizeFilePath,
  removeFileReference
} from "./file-reference.js";

// markdown.js 依赖 html-embed.js（浏览器面）与 file-reference.js（引用 chip），所以照
// markdown.test.mjs 的手法：把两个相对 specifier 都换成可解析的 URL 再动态 import。
const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const fileReferenceURL = new URL("./file-reference.js", import.meta.url).href;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`)
  .replace('"./file-reference.js"', `"${fileReferenceURL}"`);
const { renderMarkdown } = await import(`data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`);

// file-reference.test.mjs — J-1b「路径引用」的纯逻辑：三入口同一载体（同一归一化 +
// 同一枚 chip），引用只传路径、正文里就是路径文本，非法路径一律拒绝。

test("三入口共用同一份归一化：normalizeFilePath 就是 file-drop 的 normalizeDropPath", () => {
  assert.equal(normalizeFilePath, normalizeDropPath, "不得另写第二份路径判据");
  // 非法路径一律拒绝（绝对路径 / .. / 超长 / 空）——三入口同一判据。
  for (const raw of ["/etc/passwd", "C:\\Windows\\system.ini", "c:/Windows", "src/../../etc/passwd", "", "   ", null, "x".repeat(513)]) {
    assert.equal(normalizeFilePath(raw), "", `${JSON.stringify(raw)} 必须被拒`);
  }
  assert.equal(normalizeFilePath(".\\gui\\frontend\\dist\\app.js"), "gui/frontend/dist/app.js");
});

test("fileReferenceIndex 只收可引用路径，目录在前、去重、有上限", () => {
  const index = fileReferenceIndex([
    { path: "gui/frontend/dist", type: "dir" },
    { path: "README.md", type: "file" },
    { path: "/etc/passwd", type: "file" },
    { path: "README.md", type: "file" },
    { path: "src/../escape.js", type: "file" },
    { path: "application/core", type: "dir" }
  ]);
  assert.deepEqual(index, [
    { path: "gui/frontend/dist", dir: true },
    { path: "application/core", dir: true },
    { path: "README.md", dir: false }
  ]);
  assert.equal(fileReferenceIndex(Array.from({ length: 12 }, (_, i) => ({ path: `f${i}.js`, type: "file" })), 3).length, 3);
  assert.deepEqual(fileReferenceIndex(null), []);
});

test("fileReferenceCandidates 按子串命中（大小写不敏感）并截到上限", () => {
  const index = fileReferenceIndex([
    { path: "gui/frontend/dist/conversation-wheel.js", type: "file" },
    { path: "gui/frontend/dist/conversation-view.js", type: "file" },
    { path: "gui/frontend/dist/work-table.js", type: "file" }
  ]);
  assert.deepEqual(fileReferenceCandidates(index, "CONVERSA").map(item => item.path), [
    "gui/frontend/dist/conversation-wheel.js",
    "gui/frontend/dist/conversation-view.js"
  ]);
  assert.deepEqual(fileReferenceCandidates(index, "  work-table  ").map(item => item.path), ["gui/frontend/dist/work-table.js"]);
  assert.deepEqual(fileReferenceCandidates(index, "没有这个").map(item => item.path), []);
  assert.equal(fileReferenceCandidates(index, "").length, 3);
  const wide = fileReferenceIndex(Array.from({ length: 40 }, (_, i) => ({ path: `src/f${i}.js`, type: "file" })));
  assert.equal(fileReferenceCandidates(wide, "src/").length, FILE_REF_CANDIDATE_LIMIT);
});

test("fileReferenceQuery 只认开启该词的 `@`（空白截断）", () => {
  assert.deepEqual(fileReferenceQuery("@src/ap", 7), { query: "src/ap", start: 0, end: 7 });
  assert.deepEqual(fileReferenceQuery("看看 @src/app.js 吧", 13), { query: "src/app.js", start: 3, end: 14 });
  assert.equal(fileReferenceQuery("user@example.com", 8), null, "邮件里的 @ 不是引用");
  assert.equal(fileReferenceQuery("先看 app.js", 9), null);
});

test("applyFileReference 把 `@` 词整枚换成 `@路径 `，不在词里就只插入（不吞句子）", () => {
  assert.deepEqual(applyFileReference("@sr", 3, "src/app.js"), {
    ok: true, path: "src/app.js", cursor: 12, text: "@src/app.js "
  });
  // 光标停在词中间（紧跟 `@sr`）：整枚 token 被替换，不留 `@sr` 尾巴。
  assert.deepEqual(applyFileReference("看 @sr 里的函数", 4, "src/app.js"), {
    ok: true, path: "src/app.js", cursor: 14, text: "看 @src/app.js 里的函数"
  });
  // `+` 选择器在普通句子末尾插入：草稿一个字都不丢。
  assert.equal(applyFileReference("先看这个", 4, "src/app.js").text, "先看这个@src/app.js ");
  // 非法路径：什么都不做（ok=false），不猜、不回退到文件名。
  const rejected = applyFileReference("看看", 2, "../etc/passwd");
  assert.equal(rejected.ok, false);
  assert.equal(rejected.text, "看看");
});

test("fileReferenceTokens 识别 `@路径` 与拖拽落的裸路径，且拒绝不像路径的词", () => {
  const at = fileReferenceTokens("读一下 @src/app.js 和 @README.md");
  assert.deepEqual(at.map(token => [token.path, token.form, token.dir]), [
    ["src/app.js", "at", false],
    ["README.md", "at", false]
  ]);
  // 目录引用（选择器点目录留下的尾斜杠）仍然认，路径本身归一化后不带斜杠。
  const dir = fileReferenceTokens("@gui/frontend/ 里");
  assert.deepEqual(dir.map(token => [token.path, token.dir]), [["gui/frontend", true]]);
  // 拖拽入口的载体（dropPlan 的正文形状）：列表项里的裸路径。
  const bare = fileReferenceTokens("读取工作树文件：\n- src/app.js\n- gui/frontend/dist/app.js");
  assert.deepEqual(bare.map(token => [token.path, token.form]), [
    ["src/app.js", "bare"],
    ["gui/frontend/dist/app.js", "bare"]
  ]);
  // 保守：提到某人 / 普通词 / 邮件 / 绝对路径都不认。
  assert.deepEqual(fileReferenceTokens("@someone 来一下"), []);
  assert.deepEqual(fileReferenceTokens("and/or 二选一"), []);
  assert.deepEqual(fileReferenceTokens("user@example.com"), []);
  assert.deepEqual(fileReferenceTokens("看 /etc/passwd"), []);
  // 句末句号不并进路径。
  assert.deepEqual(fileReferenceTokens("见 @src/app.js.").map(token => token.end), [13]);
  assert.deepEqual(fileReferencePaths("读 @src/app.js 与 @src/app.js"), ["src/app.js"]);
});

test("同一枚 chip 两种宿主：composer（可删）与正文（可点开），标签就是路径文本", () => {
  const composerChip = fileReferenceChipHTML("src/app.js", { host: "composer" });
  const messageChip = fileReferenceChipHTML("src/app.js", { host: "message" });
  for (const html of [composerChip, messageChip]) {
    assert.match(html, /class="file-ref-chip is-(composer|message)"/);
    assert.match(html, /data-file-ref="src\/app\.js"/);
    assert.match(html, /<span class="file-ref-path">src\/app\.js<\/span>/, "chip 标签就是路径（agent 看到的是路径）");
  }
  assert.match(composerChip, /data-file-ref-remove/, "草稿区那枚可删");
  assert.doesNotMatch(messageChip, /data-file-ref-remove/, "正文那枚不可删（已发出的正文不可改）");
  // 不携带任何内容字节：chip 里只有路径标记、路径文本与数据属性。
  assert.equal(/base64|content|bytes/.test(messageChip), false);
  assert.equal(fileReferenceChipHTML("/etc/passwd", { host: "message" }), "", "非法路径渲染不出 chip");
  assert.match(fileReferenceStripHTML("读 @src/a.js 与 @docs/ 里的东西", { host: "composer" }), /data-file-ref-dir="1"/);
  assert.equal(fileReferenceStripHTML("没有引用", { host: "composer" }), "");
});

test("removeFileReference 只摘掉那一枚引用，其余草稿原样保留", () => {
  assert.equal(removeFileReference("看 @src/app.js 谢谢", "src/app.js"), "看 谢谢");
  assert.equal(removeFileReference("读取工作树文件：\n- src/app.js\n- src/b.js", "src/app.js"), "读取工作树文件：\n- \n- src/b.js");
  assert.equal(removeFileReference("没有这枚", "src/app.js"), "没有这枚");
});

test("正文 chip：user 消息开启 fileRefs 后路径被渲染成 chip，正文文本一个字不改", () => {
  const content = "看一下 @src/app.js 这个文件，另外 src/b.js 也要看";
  const html = renderMarkdown(content, { fileRefs: true });
  assert.match(html, /data-file-ref="src\/app\.js"/, "`@文件` 渲染成 chip");
  assert.match(html, /data-file-ref="src\/b\.js"/, "拖拽/裸路径形态同样渲染成 chip");
  assert.equal(/<base64|<img|data:application/.test(html), false, "chip 不搬内容字节");
  // 没开启的渲染路径（assistant 正文等）保持原样：chip 只认用户附的文件。
  assert.doesNotMatch(renderMarkdown(content), /file-ref-chip/);
  // 代码块/行内代码里的路径不参与识别（那里是代码，不是引用）。
  assert.doesNotMatch(renderMarkdown("```\n@src/app.js\n```", { fileRefs: true }), /file-ref-chip/);
  assert.doesNotMatch(renderMarkdown("`src/app.js`", { fileRefs: true }), /file-ref-chip/);
});

test("提交正文样例：三个入口落进正文的都是同形状的「路径文本」，没有内容字节", () => {
  // 这就是"chip 进正文"的载体：**正文里就是路径**（chip 只是渲染层识别）。三入口各走
  // 一遍，落到的都是这一形状——agent 收到的、能看到的就是这些路径。
  const at = applyFileReference("看看这个", 4, "gui/frontend/dist/app.js").text;
  const plus = applyFileReference("看看这个", 4, "docs/gui/modules/shell-and-interactions.md").text;
  const drag = fileQueueText(["src/a.js"]);
  assert.equal(at, "看看这个@gui/frontend/dist/app.js ");
  assert.equal(plus, "看看这个@docs/gui/modules/shell-and-interactions.md ");
  assert.equal(drag, "读取工作树文件：\n- src/a.js");
  for (const body of [at, plus, drag]) {
    assert.doesNotMatch(body, /base64|data:application|;base64,/, "正文里不得出现内容字节");
    assert.ok(body.length < 120, "正文里只有路径——没有把文件内容搬进来");
  }
});

test("FILE_REF_TRIGGER 是前端侧唯一的引用标记（后端不认它是前缀）", () => {
  assert.equal(FILE_REF_TRIGGER, "@");
});
