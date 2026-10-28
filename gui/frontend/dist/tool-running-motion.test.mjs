import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const fileReferenceURL = new URL("./file-reference.js", import.meta.url).href;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`)
  .replace('"./file-reference.js"', `"${fileReferenceURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { renderConversationComponent } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

function toolHTML(status, extra = {}) {
  return renderConversationComponent([
    {
      id: `tool-${status}`,
      role: "tool",
      tool: { id: `call-${status}`, name: "read_file", arguments: "{}", status, ...extra }
    }
  ], { running: status === "running" }).html;
}

function textOf(html) {
  return html.replace(/<[^>]*>/g, " ").replace(/\s+/g, " ").trim();
}

test("running 工具卡只有一条局部 1.8s shimmer activity，并保留工具名与可读状态", () => {
  const html = toolHTML("running");
  assert.match(html, /class="tool-run is-running"/);
  assert.equal((html.match(/class="tool-run-activity"/g) || []).length, 1);
  assert.match(html, /data-motion-purpose="feedback"/);
  assert.match(html, /data-motion-trigger="state-change"/);
  assert.match(html, /data-motion-phase="running"/);
  assert.match(textOf(html), /read_file/);
  assert.match(textOf(html), /正在运行/);
});

test("success/error/canceled 立即停止工具 shimmer", () => {
  for (const [status, className] of [["success", "is-success"], ["error", "is-error"], ["canceled", "is-cancelled"]]) {
    const html = toolHTML(status, status === "error" ? { error: "boom" } : {});
    assert.match(html, new RegExp(`class="tool-run ${className}`), `${status} 应使用 ${className}`);
    assert.doesNotMatch(html, /tool-run-activity/, `${status} 后不得残留 running activity`);
    assert.match(html, /正在运行|已完成|失败|已取消/);
  }
});

test("对话尾部 spinner 改成可读状态与 aria-busy；无图标旋转", async () => {
  const html = renderConversationComponent([], { running: true }).html;
  assert.match(html, /aria-busy="true"/);
  assert.match(html, /role="status"/);
  assert.match(html, /正在生成回复|等待模型响应/);
  assert.doesNotMatch(html, /runtime-spinner/);
});

test("tool CSS 的 activity 层只靠 transform 扫动，reduced-motion 为静态线", async () => {
  const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");
  assert.match(css, /\.tool-run-activity\s*\{[^}]*position:\s*absolute/s);
  assert.match(css, /\.tool-run-activity\s*\{[^}]*overflow:\s*hidden/s);
  assert.match(css, /\.tool-run-activity\s*>\s*i\s*\{[^}]*transform:/s);
  assert.match(css, /\.tool-run:not\(\.is-running\)\s+\.tool-run-activity/);
  assert.match(css, /@media\s*\(prefers-reduced-motion:\s*reduce\)[\s\S]*?\.tool-run-activity\s*>\s*i\s*\{[^}]*animation:\s*none/s);
});
