import test from "node:test";
import assert from "node:assert/strict";

const {
  ATTACHMENT_LIMIT,
  ATTACHMENT_MAX_BYTES,
  attachmentMime,
  attachmentIsImage,
  attachmentFiles,
  attachmentReject,
  attachmentName,
  attachmentAdd,
  attachmentRemove,
  attachmentChipHTML,
  attachmentStripHTML,
  attachmentThumbUrl
} = await import("./composer-attachments.js");

// composer-attachments.test.mjs — 输入框图片附件（拖拽 / 粘贴）的纯逻辑判据。
//
// 现场口径（用户要求）：截图拖进输入框 / 粘贴进输入框，输入框里给**不打开的预览**
// （缩略图），点开才用系统默认图片查看器看原图。这里钉的是"收不收、叫什么、怎么排、
// 怎么画"这四件不碰 DOM 的事。

test("attachmentMime 归一 MIME 写法", () => {
  assert.equal(attachmentMime("image/png"), "image/png");
  assert.equal(attachmentMime("IMAGE/PNG;charset=binary"), "image/png");
  assert.equal(attachmentMime("image/jpg"), "image/jpeg");
  assert.equal(attachmentMime("  "), "");
  assert.equal(attachmentMime(null), "");
});

test("attachmentIsImage 只认白名单里的图片类型", () => {
  for (const mime of ["image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp"]) {
    assert.equal(attachmentIsImage(mime), true, mime);
  }
  // svg 刻意不在白名单：它靠扩展名接管程序、且不属于端点认的那几种。
  assert.equal(attachmentIsImage("image/svg+xml"), false);
  assert.equal(attachmentIsImage("text/plain"), false);
  assert.equal(attachmentIsImage("application/pdf"), false);
});

test("attachmentReject 先判类型、再判大小、最后判数量", () => {
  assert.equal(attachmentReject({ type: "image/png", size: 1024 }), "");
  assert.match(attachmentReject({ type: "text/plain", size: 10 }), /只支持/);
  // 超大且非图片：先说"不是图片"，因为那才是根因。
  assert.match(attachmentReject({ type: "text/plain", size: ATTACHMENT_MAX_BYTES + 1 }), /只支持/);
  assert.match(attachmentReject({ type: "image/png", size: ATTACHMENT_MAX_BYTES + 1 }), /MB 上限/);
  const full = Array.from({ length: ATTACHMENT_LIMIT }, (_, index) => ({ ref: `media:${index}` }));
  assert.match(attachmentReject({ type: "image/png", size: 10 }, full), /最多附/);
  // size 缺失（某些拖拽来源不带 size）不阻塞：真实字节由后端配额把关。
  assert.equal(attachmentReject({ type: "image/png" }), "");
});

test("attachmentName 把扩展名按 MIME 归一（截图粘贴常常没有名字）", () => {
  const now = new Date(2026, 9, 30, 8, 5, 9).getTime();
  assert.equal(attachmentName({ name: "shot.png", type: "image/png" }, now), "shot.png");
  // 没有扩展名 → 补上；扩展名与 MIME 不符 → 按 MIME 改。
  assert.equal(attachmentName({ name: "clipboard", type: "image/png" }, now), "clipboard.png");
  assert.equal(attachmentName({ name: "shot.txt", type: "image/jpeg" }, now), "shot.jpg");
  // 完全没有名字 → 时间戳兜底（可读、不含路径字符）。
  assert.equal(attachmentName({ name: "", type: "image/png" }, now), "screenshot-20261030-080509.png");
  // 路径与非法字符都不进名字（名字不是路径）。
  assert.equal(attachmentName({ name: "C:\\shots\\a<b>.png", type: "image/png" }, now), "a_b_.png");
  assert.equal(attachmentName({ name: "../../etc/passwd", type: "image/png" }, now), "passwd.png");
});

test("attachmentAdd 按 ref 去重并且不改原数组", () => {
  const first = attachmentAdd([], { ref: "media:a", name: "a.png" });
  assert.equal(first.length, 1);
  const again = attachmentAdd(first, { ref: "media:a", name: "a.png" });
  assert.equal(again.length, 1, "同一张图粘贴两次不该出现两条（模型会看两遍）");
  assert.notEqual(again, first, "返回新数组，不原地改");
  assert.equal(attachmentAdd(first, { ref: "" }).length, 1, "没有 ref 的条目不进列表");
  const full = Array.from({ length: ATTACHMENT_LIMIT }, (_, index) => ({ ref: `media:${index}` }));
  assert.equal(attachmentAdd(full, { ref: "media:extra" }).length, ATTACHMENT_LIMIT);
});

test("attachmentRemove 只摘掉指定 ref", () => {
  const list = [{ ref: "media:a" }, { ref: "media:b" }];
  const left = attachmentRemove(list, "media:a");
  assert.deepEqual(left.map(item => item.ref), ["media:b"]);
  assert.equal(list.length, 2, "不改原数组");
  assert.equal(attachmentRemove(list, "media:none").length, 2);
  assert.equal(attachmentRemove(list, "").length, 2);
});

test("attachmentChipHTML 画缩略图 + 移除按钮，且属性都转义", () => {
  const html = attachmentChipHTML({ ref: "media:a", name: 'a"b.png', thumbUrl: "blob:seelex/1" });
  assert.match(html, /data-attachment="media:a"/);
  assert.match(html, /data-attachment-open="media:a"/);
  assert.match(html, /data-attachment-remove="media:a"/);
  assert.match(html, /src="blob:seelex\/1"/);
  assert.match(html, /alt=""/);
  assert.ok(!html.includes('a"b.png'), "名字必须转义后再进属性");
  assert.match(html, /a&quot;b\.png/);
  // 没有 ref 的条目不画（没有 ref 就打不开原图，占位没有意义）。
  assert.equal(attachmentChipHTML({ name: "a.png" }), "");
  // 缺缩略图时退化成一个占位方块，"点开原图"那条路还在。
  const blank = attachmentChipHTML({ ref: "media:b", name: "b.png" });
  assert.match(blank, /is-blank/);
  assert.match(blank, /data-attachment-open="media:b"/);
});

test("attachmentStripHTML 画整条，空列表整块收起", () => {
  assert.equal(attachmentStripHTML([]), "");
  assert.equal(attachmentStripHTML(null), "");
  const html = attachmentStripHTML([{ ref: "media:a" }, { ref: "media:b" }]);
  assert.equal(html.match(/data-attachment=/g).length, 2);
});

test("attachmentFiles 只挑图片文件，保持顺序", () => {
  const png = { name: "a.png", type: "image/png" };
  const txt = { name: "notes.txt", type: "text/plain" };
  const jpeg = { name: "b.jpg", type: "image/jpeg" };
  assert.deepEqual(attachmentFiles([txt, png, jpeg]), [png, jpeg]);
  assert.deepEqual(attachmentFiles(null), []);
  // FileList 形状（类数组）也要认。
  assert.deepEqual(attachmentFiles({ 0: png, 1: txt, length: 2 }), [png]);
});

test("attachmentThumbUrl 用本地字节生成缩略图，工厂失败不炸", () => {
  assert.equal(attachmentThumbUrl({ size: 1 }, () => "blob:x"), "blob:x");
  assert.equal(attachmentThumbUrl(null, () => "blob:x"), "");
  assert.equal(attachmentThumbUrl({ size: 1 }, null), "");
  assert.equal(attachmentThumbUrl({ size: 1 }, () => { throw new Error("boom"); }), "");
});
