import assert from "node:assert/strict";
import test from "node:test";

import {
  CODE_EXTENSIONS,
  PREVIEW_LIMITS,
  base64ToBytes,
  closePreviewTab,
  codeLanguageForPath,
  decodeFileText,
  formatPreviewSize,
  needsWholeFile,
  normalizePreviewTab,
  openPreviewTab,
  previewKindForPath,
  previewTabLabel,
  renderPreviewTabsHTML
} from "./file-preview.js";

test("previewKindForPath dispatches by extension", () => {
  const cases = {
    "README.md": "markdown",
    "docs/guide.markdown": "markdown",
    "src/main.go": "code",
    "src/app.tsx": "code",
    "notes.txt": "text",
    "app.log": "text",
    "manual.pdf": "pdf",
    "report.docx": "word",
    "old.doc": "word-legacy",
    "photo.png": "image",
    "pic.SVG": "image",
    "data.bin": "unsupported",
    "Makefile": "code",
    "Dockerfile": "code",
    "noext": "unsupported"
  };
  for (const [path, kind] of Object.entries(cases)) {
    assert.equal(previewKindForPath(path), kind, path);
  }
});

test("codeLanguageForPath maps extensions to hljs languages", () => {
  assert.equal(codeLanguageForPath("main.go"), "go");
  assert.equal(codeLanguageForPath("a/b.ts"), "typescript");
  assert.equal(codeLanguageForPath("x.json"), "json");
  assert.equal(codeLanguageForPath("Dockerfile"), "dockerfile");
  assert.equal(codeLanguageForPath("Makefile"), "makefile");
  assert.equal(codeLanguageForPath("plain.txt"), "");
  assert.equal(codeLanguageForPath("README.md"), "markdown");
});

test("CODE_EXTENSIONS covers common source types", () => {
  for (const ext of [".go", ".js", ".ts", ".py", ".java", ".rs", ".c", ".cpp", ".sh", ".sql", ".html", ".css"]) {
    assert.ok(CODE_EXTENSIONS.has(ext), ext);
  }
});

test("needsWholeFile marks paginated renderers", () => {
  assert.equal(needsWholeFile("pdf"), true);
  assert.equal(needsWholeFile("word"), true);
  assert.equal(needsWholeFile("image"), true);
  assert.equal(needsWholeFile("code"), false);
  assert.equal(needsWholeFile("markdown"), false);
});

test("decodeFileText handles utf8 and utf16 BOM", () => {
  assert.equal(decodeFileText(new TextEncoder().encode("hello 世界")), "hello 世界");
  const utf16 = new Uint8Array([0xFF, 0xFE, 0x2D, 0x00, 0x4E, 0x00, 0x16, 0x00]);
  assert.equal(decodeFileText(utf16), "-N\u0016");
  // GBK "编码"（E7 BC 96 E7 A0 81 → GBK: B1 E0 C2 EB）
  const gbk = new Uint8Array([0xB1, 0xE0, 0xC2, 0xEB]);
  assert.equal(decodeFileText(gbk), "编码");
  // windows-1252 fallback for stray bytes（永不抛错）
  const latin = new Uint8Array([0xE9, 0x20, 0x74, 0x65, 0x78, 0x74]);
  assert.equal(decodeFileText(latin), "é text");
});

test("formatPreviewSize renders readable units", () => {
  assert.equal(formatPreviewSize(0), "");
  assert.equal(formatPreviewSize(-1), "");
  assert.equal(formatPreviewSize(512), "512 B");
  assert.equal(formatPreviewSize(2048), "2.0 KB");
  assert.equal(formatPreviewSize(5 * 1024 * 1024), "5.0 MB");
  assert.equal(formatPreviewSize(Number.NaN), "");
});

test("base64ToBytes round trips", () => {
  const bytes = new Uint8Array([0, 1, 2, 250, 251, 252, 253, 254, 255]);
  const encoded = Buffer.from(bytes).toString("base64");
  assert.deepEqual(Array.from(base64ToBytes(encoded)), Array.from(bytes));
});

test("preview limits are bounded for text renderers", () => {
  assert.ok(PREVIEW_LIMITS.markdown <= 8 << 20);
  assert.ok(PREVIEW_LIMITS.code <= 8 << 20);
  assert.ok(PREVIEW_LIMITS.pdf <= 64 << 20);
});

// ── 多文件详情标签（上标 chip）────────────────────────────

test("previewTabLabel takes the basename for posix and windows paths", () => {
  assert.equal(previewTabLabel("src/main.go"), "main.go");
  assert.equal(previewTabLabel("a\\b\\c.txt"), "c.txt");
  assert.equal(previewTabLabel("README.md"), "README.md");
  assert.equal(previewTabLabel(""), "");
});

test("normalizePreviewTab drops entries without a path and fills the label", () => {
  assert.equal(normalizePreviewTab(null), null);
  assert.equal(normalizePreviewTab("src/main.go"), null);
  assert.equal(normalizePreviewTab({ name: "x" }), null);
  assert.deepEqual(normalizePreviewTab({ path: "src/main.go" }), { path: "src/main.go", name: "main.go" });
  assert.deepEqual(
    normalizePreviewTab({ path: "src/main.go", name: "main" }),
    { path: "src/main.go", name: "main" }
  );
});

test("openPreviewTab appends new files and dedups by path", () => {
  const first = openPreviewTab([], { path: "a.go", name: "a.go" });
  assert.equal(first.added, true);
  assert.deepEqual(first.path, "a.go");
  assert.deepEqual(first.tabs, [{ path: "a.go", name: "a.go" }]);

  const second = openPreviewTab(first.tabs, { path: "b/c.txt" });
  assert.equal(second.added, true);
  assert.deepEqual(second.tabs.map(item => item.path), ["a.go", "b/c.txt"]);

  // 再次打开同一个文件：不重复追加（只是激活），列表与顺序不变。
  const again = openPreviewTab(second.tabs, { path: "a.go", name: "a.go" });
  assert.equal(again.added, false);
  assert.deepEqual(again.tabs.map(item => item.path), ["a.go", "b/c.txt"]);

  // 非法条目原样返回。
  const invalid = openPreviewTab(second.tabs, {});
  assert.equal(invalid.path, "");
  assert.equal(invalid.added, false);
  assert.equal(invalid.tabs.length, 2);
});

test("closePreviewTab activates the right neighbour, then the left, then empties", () => {
  const tabs = [{ path: "a" }, { path: "b" }, { path: "c" }];
  // 关闭中间：激活右邻居 c。
  assert.deepEqual(closePreviewTab(tabs, "b"), { tabs: [{ path: "a" }, { path: "c" }], active: "c" });
  // 关闭末位：回落到左邻居 a。
  assert.deepEqual(closePreviewTab([{ path: "a" }, { path: "b" }], "b"), { tabs: [{ path: "a" }], active: "a" });
  // 关闭最后一个：列表清空，active 为空串（容器生命周期结束）。
  assert.deepEqual(closePreviewTab([{ path: "a" }], "a"), { tabs: [], active: "" });
  // 关闭不存在的路径：原样返回。
  assert.deepEqual(closePreviewTab(tabs, "zzz"), { tabs, active: "" });
});

test("closePreviewTab keeps the current detail when closing a different one", () => {
  const tabs = [{ path: "a" }, { path: "b" }, { path: "c" }];
  // 当前看 a，关掉 c：a 保持激活（关旁边的文件不打断正在看的）。
  assert.deepEqual(closePreviewTab(tabs, "c", "a"), { tabs: [{ path: "a" }, { path: "b" }], active: "a" });
  // 当前看 b，关掉 b：切到右邻居 c。
  assert.deepEqual(closePreviewTab(tabs, "b", "b"), { tabs: [{ path: "a" }, { path: "c" }], active: "c" });
});

test("renderPreviewTabsHTML marks the active chip, escapes text and carries a close button", () => {
  const html = renderPreviewTabsHTML([
    { path: "src/a.go", name: "<b>a</b>" },
    { path: "src/b.ts", name: "b.ts" }
  ], "src/b.ts");
  assert.ok(html.includes('data-preview-tab="src/a.go"'));
  assert.ok(html.includes('data-preview-tab="src/b.ts"'));
  assert.ok(html.includes('data-preview-tab-close="src/a.go"'));
  assert.ok(html.includes("file-preview-chip is-active"));
  // 文本 escape，不注入原始标签。
  assert.ok(!html.includes("<b>a</b>"));
  assert.ok(html.includes("&lt;b&gt;a&lt;/b&gt;"));
  // 空列表渲染为空串。
  assert.equal(renderPreviewTabsHTML([], ""), "");
});
