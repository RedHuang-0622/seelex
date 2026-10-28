import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");

test("置顶会话不再拼接左侧内阴影，并只保留一个前导图标", () => {
  assert.doesNotMatch(css, /\.session-row\.is-pinned\s+\.session-title\s*\{[^}]*box-shadow\s*:\s*inset\s+2px\s+0\s+0/s);
  assert.doesNotMatch(css, /\.session-row\.is-active[^{]*\{[^}]*box-shadow\s*:\s*inset\s+2px\s+0\s+0/s);
  assert.doesNotMatch(
    app,
    /<span class="entry-name">\$\{pinned \? `<span class="session-pin-mark"[^`]*` : ""\}\$\{icon\("message", 13\)\}/s,
    "置顶行不得同时出现星标和消息图标"
  );
});

test("置顶状态有可见/可读的单一信号，并暴露稳定机读属性", () => {
  assert.match(app, /data-session-pinned="\$\{pinned \? "true" : "false"\}"/);
  assert.ok(app.includes('const pinLabel = pinned ? " · 已置顶" : "";'));
  assert.ok(app.includes('aria-label="${escapeHtml(`${label}${pinLabel} · ${tip}`)}"'));
  assert.ok(app.includes('${pinned ? `<span class="session-pin-mark" aria-hidden="true">${icon("star", 13)}</span>` : icon("message", 13)}'));
  assert.match(css, /\.session-row\.is-pinned\s+\.entry-name\s*\{[^}]*color:\s*var\(--text-strong\)/s);
  assert.match(css, /\.session-pin-mark\s*\{[^}]*color:\s*var\(--accent\)/s);
});
