import assert from "node:assert/strict";
import test from "node:test";

import {
  CODE_EXTENSIONS,
  PREVIEW_LIMITS,
  base64ToBytes,
  codeLanguageForPath,
  decodeFileText,
  formatPreviewSize,
  needsWholeFile,
  previewKindForPath
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
