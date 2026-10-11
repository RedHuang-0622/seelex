import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const source = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const { renderMarkdown, rewriteMermaidFences } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

test("renders explicitly marked html fences as sandboxed embeds", () => {
  const html = renderMarkdown([
    "趋势如下：",
    "",
    "```seelex-html title=\"任务耗时\" height=300",
    "<svg viewBox=\"0 0 10 10\"><rect width=\"10\" height=\"10\"/></svg>",
    "```"
  ].join("\n"));
  assert.match(html, /class="html-embed"/);
  assert.match(html, /sandbox="allow-scripts"/);
  assert.match(html, /--embed-height:300px/);
  assert.match(html, /任务耗时/);
  assert.doesNotMatch(html, /<pre><code class="language-seelex-html"/);
});

test("keeps plain html fences as source blocks", () => {
  const html = renderMarkdown(["```html", "<div>raw</div>", "```"].join("\n"));
  assert.match(html, /<pre><code class="language-html">/);
  assert.doesNotMatch(html, /<iframe/);
});

test("renders common block and inline markdown", () => {
  const html = renderMarkdown(`# Title

**bold** and *italic* with [docs](https://example.com).

> quoted

- first
- [x] done

\`\`\`go
fmt.Println("ok")
\`\`\``);

  assert.match(html, /<h1>Title<\/h1>/);
  assert.match(html, /<strong>bold<\/strong>/);
  assert.match(html, /<em>italic<\/em>/);
  assert.match(html, /href="https:\/\/example\.com"/);
  assert.match(html, /<blockquote><p>quoted<\/p><\/blockquote>/);
  assert.match(html, /<ul><li>first<\/li><li class="task-item">/);
  assert.match(html, /<code class="language-go">fmt\.Println\(&quot;ok&quot;\)<\/code>/);
});

test("renders tables and preserves alignment", () => {
  const html = renderMarkdown(`| Name | State |
| :--- | ---: |
| GUI | ready |`);

  assert.match(html, /<table>/);
  assert.match(html, /<th style="text-align:left">Name<\/th>/);
  assert.match(html, /<td style="text-align:right">ready<\/td>/);
});

test("escapes raw html and rejects dangerous links", () => {
  const html = renderMarkdown(`<script>alert(1)</script>

[bad](javascript:alert(1))`);

  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /href=/);
  assert.match(html, /\[bad\]\(javascript:alert\(1\)\)/);
});

test("keeps code content inert", () => {
  const html = renderMarkdown("`<img src=x onerror=alert(1)>`");
  assert.equal(html, "<p><code>&lt;img src=x onerror=alert(1)&gt;</code></p>");
});

test("wraps closed think blocks in collapsed reasoning details", () => {
  const html = renderMarkdown(`<think>
**检查**输入与约束。
</think>
最终回答`);

  assert.match(html, /^<details class="reasoning-block">/);
  assert.doesNotMatch(html, /<details[^>]* open/);
  assert.match(html, /<strong>检查<\/strong>输入与约束。/);
  assert.match(html, /<\/details>\n?<p>最终回答<\/p>$/);
});

test("keeps an unfinished think block open while streaming", () => {
  const html = renderMarkdown("<think>正在检查\n第二步");

  assert.match(html, /<details open class="reasoning-block is-streaming">/);
  assert.match(html, /<span class="reasoning-state">LIVE<\/span>/);
  assert.match(html, /正在检查<br>第二步/);
});

test("does not interpret think tags inside fenced code", () => {
  const html = renderMarkdown("```xml\n<think>literal</think>\n```");

  assert.doesNotMatch(html, /reasoning-block/);
  assert.match(html, /&lt;think&gt;literal&lt;\/think&gt;/);
});

// ── mermaid 围栏（F-5 D3/D4）──────────────────────────────
// 语言标记为 mermaid 的围栏走**图表容器**：容器里始终先渲染一份源码代码块，
// 画布初始为空且 hidden。库未就绪/渲染失败时退回这份源码块——不白屏。
test("renders mermaid fences as diagram containers with the source block kept for fallback", () => {
  const html = renderMarkdown(["```mermaid", "graph TD;", "A-->B;", "```"].join("\n"));

  assert.match(html, /class="mermaid-diagram" data-mermaid-diagram/);
  assert.match(html, /class="mermaid-canvas" data-mermaid-canvas hidden/);
  assert.match(html, /<pre class="mermaid-source"><code class="language-mermaid">graph TD;\nA--&gt;B;<\/code><\/pre>/);
  // 不再是"裸代码块"这一种形态（否则就没有可渲染的分支）。
  assert.doesNotMatch(html, /<pre><code class="language-mermaid">/);
  // 源码里的 HTML 仍然转义：容器不是执行面。
  const dangerous = renderMarkdown(["```mermaid", "<img src=x onerror=alert(1)>", "```"].join("\n"));
  assert.match(dangerous, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(dangerous, /<img /);
});

test("rewriteMermaidFences converts marked's mermaid code block into the same container", () => {
  const markedHTML = '<p>前</p>\n<pre><code class="language-mermaid">graph LR;\nX--&gt;Y;\n</code></pre>\n<p>后</p>';
  const html = rewriteMermaidFences(markedHTML);

  assert.match(html, /<p>前<\/p>/);
  assert.match(html, /data-mermaid-diagram/);
  assert.match(html, /<code class="language-mermaid">graph LR;\nX--&gt;Y;\n<\/code>/);
  assert.doesNotMatch(html, /<pre><code class="language-mermaid">/);
  // 非 mermaid 的代码块原样保留。
  const plain = '<pre><code class="language-go">fmt.Println(1)</code></pre>';
  assert.equal(rewriteMermaidFences(plain), plain);
});

test("mermaid 围栏的容器与图表渲染模块使用同一形状（data 属性对齐）", async () => {
  // markdown.js 产出的容器必须能被 mermaid-diagram.js: hydrateMermaid 找到——
  // 两个模块不共享 import（markdown.js 走 data-URL 单测），靠这里钉住约定。
  const diagram = await readFile(new URL("./mermaid-diagram.js", import.meta.url), "utf8");
  assert.match(diagram, /\[data-mermaid-diagram\]/);
  assert.match(diagram, /\[data-mermaid-canvas\]/);
  assert.match(diagram, /pre\.mermaid-source/);
  assert.match(renderMarkdown(["```mermaid", "graph TD;", "```"].join("\n")), /data-mermaid-diagram/);
});
