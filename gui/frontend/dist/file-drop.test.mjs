import test from "node:test";
import assert from "node:assert/strict";

const {
  FILE_DRAG_MIME,
  normalizeDropPath,
  fileDragPayload,
  dropPaths,
  fileQueueText,
  appendDropText,
  dropPlan
} = await import("./file-drop.js");

// file-drop.test.mjs — 档案夹 → 会话（消息队列）的拖放纯逻辑。
//
// 现场口径（用户报告）：需要"文件从档案夹中抽取发送"的交互逻辑 —— 拖进来的必须
// 是**工作区内的相对路径**，落到空草稿上就直接排队发出、落到正在写的草稿上只追加
// 引用（不替用户发出去）。

test("normalizeDropPath 只接受工作区内的相对路径", () => {
  assert.equal(normalizeDropPath(" src/app.js "), "src/app.js");
  assert.equal(normalizeDropPath(".\\src\\app.js"), "src/app.js");
  assert.equal(normalizeDropPath("./src/app.js"), "src/app.js");
  assert.equal(normalizeDropPath("/etc/passwd"), "");
  assert.equal(normalizeDropPath("C:\\Windows\\system.ini"), "");
  assert.equal(normalizeDropPath("c:/Windows"), "");
  assert.equal(normalizeDropPath("src/../../etc/passwd"), "");
  assert.equal(normalizeDropPath(""), "");
  assert.equal(normalizeDropPath("   "), "");
  assert.equal(normalizeDropPath(null), "");
  assert.equal(normalizeDropPath("x".repeat(513)), "");
});

test("fileDragPayload 给非法路径返回 null（该行不该启动拖拽）", () => {
  assert.deepEqual(fileDragPayload("src/app.js"), { mime: FILE_DRAG_MIME, path: "src/app.js" });
  assert.equal(fileDragPayload("/etc/passwd"), null);
});

test("dropPaths 优先读自定义 mime，text/plain 兜底，裸文件名不接受", () => {
  assert.deepEqual(
    dropPaths({ mime: "src/a.js", text: "src/b.js" }),
    ["src/a.js", "src/b.js"]
  );
  assert.deepEqual(dropPaths({ text: "src/a.js\nsrc/b.js\nsrc/a.js" }), ["src/a.js", "src/b.js"]);
  // 系统文件拖入：只有文件名，不是工作树相对路径 → 整条丢弃（发出去只会读到不存在的文件）。
  assert.deepEqual(dropPaths({ files: [{ name: "报告.pdf" }] }), []);
  // 带路径字段的条目仍然可用。
  assert.deepEqual(dropPaths({ files: [{ name: "a.js", path: "src/a.js" }] }), ["src/a.js"]);
  assert.deepEqual(dropPaths({}), []);
  assert.deepEqual(dropPaths({ mime: "/abs/path", text: "../up.js" }), []);
});

test("fileQueueText 渲染成一条可执行的读取指令", () => {
  assert.equal(fileQueueText(["src/a.js"]), "读取工作树文件：\n- src/a.js");
  assert.equal(fileQueueText(["src/a.js", "src/b.js"]), "读取以下 2 个工作树文件：\n- src/a.js\n- src/b.js");
  assert.equal(fileQueueText([]), "");
});

test("appendDropText 只追加，不替换用户正在写的草稿", () => {
  assert.equal(appendDropText("", "读取工作树文件：\n- a.js"), "读取工作树文件：\n- a.js");
  assert.equal(appendDropText("看看这个  ", "读取工作树文件：\n- a.js"), "看看这个\n\n读取工作树文件：\n- a.js");
  assert.equal(appendDropText("看看这个", ""), "看看这个");
});

test("dropPlan：草稿空 → 排队发出去；草稿非空 → 只追加引用", () => {
  assert.deepEqual(dropPlan({ paths: ["src/a.js"], draftText: "" }), {
    mode: "queue",
    paths: ["src/a.js"],
    text: "读取工作树文件：\n- src/a.js"
  });
  assert.deepEqual(dropPlan({ paths: ["src/a.js"], draftText: "   " }), {
    mode: "queue",
    paths: ["src/a.js"],
    text: "读取工作树文件：\n- src/a.js"
  });
  assert.deepEqual(dropPlan({ paths: ["src/a.js"], draftText: "顺便解释一下" }), {
    mode: "insert",
    paths: ["src/a.js"],
    text: "读取工作树文件：\n- src/a.js",
    draft: "顺便解释一下\n\n读取工作树文件：\n- src/a.js"
  });
  // 去重 + 非法路径丢弃；一条可用路径都不剩 → null（什么都不做，不报错、不空发）。
  assert.deepEqual(dropPlan({ paths: ["src/a.js", "src/a.js", "/etc/passwd"], draftText: "" }).paths, ["src/a.js"]);
  assert.equal(dropPlan({ paths: ["/etc/passwd", "../x"], draftText: "" }), null);
  assert.equal(dropPlan({ paths: [], draftText: "在写" }), null);
});
