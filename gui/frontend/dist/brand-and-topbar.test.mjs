// 顶栏两条本地约定 + 品牌位资产的静态护栏：
//   ① 运行状态（provider · model / 用量）是用户要看的，诊断徽标是开发诊断——宽度不够时
//      徽标先让位（封顶 + 按视口分级收起），不许把运行信息挤成省略号；
//   ② 品牌位是图片而不是字母占位，且前端用的就是脚本裁过的同一张图。
// 行为面（徽标渲染、点击明细）在 perf-hooks.test.mjs / live-diag 相关用例里。
import assert from "node:assert/strict";
import { readFile, stat } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("顶栏宽度预算：徽标封顶 + 分级收起，运行状态不被挤掉", async () => {
  const css = await read("styles.css");
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");

  // 运行状态行的上限必须容纳两个封顶后的徽标（原来 32vw 装不下 22vw + 26vw 的徽标，
  // 结果 provider 被截成 "openai · deep…"）。
  const summary = bare.match(/^\.runtime-summary \{[^}]*\}/m);
  assert.ok(summary, "要找得到 .runtime-summary 规则");
  assert.doesNotMatch(summary[0], /max-width:\s*32vw/, "32vw 的预算装不下徽标");
  assert.match(summary[0], /max-width:\s*min\(64vw,\s*1040px\)/);

  // 徽标各自封顶到固定像素（vw 会随窗口放大，正是溢出成灾的原因）。
  for (const selector of [".perf-badge", ".live-diag-badge"]) {
    const rule = bare.match(new RegExp(`^\\${selector} \\{[^}]*\\}`, "m"));
    assert.ok(rule, `要找得到 ${selector} 规则`);
    const maxWidth = Number(rule[0].match(/max-width:\s*(\d+)px/)?.[1] || 0);
    assert.ok(maxWidth > 0 && maxWidth <= 240, `${selector} 的 max-width 要是 ≤240px 的固定值，实际 ${maxWidth}`);
    assert.doesNotMatch(rule[0], /max-width:\s*\d+vw/, `${selector} 不该用 vw 封顶`);
  }

  // 让位顺序：徽标宿主先缩（flex-shrink 更大），窄窗分级直接收起。
  const host = bare.match(/^\.perf-badge-host \{[^}]*\}/m);
  assert.ok(host, "要找得到 .perf-badge-host 规则");
  assert.match(host[0], /min-width:\s*0/, "徽标宿主必须可缩（否则撑破整行）");
  assert.match(host[0], /flex:\s*0 6 auto/, "徽标要让得比运行状态快（shrink 6 vs 1）");
  assert.match(bare, /@media \(max-width: 1500px\) \{ #live-diag-host \{ display: none; \} \}/);
  assert.match(bare, /@media \(max-width: 1180px\) \{ #perf-badge-host \{ display: none; \} \}/);
});

test("品牌位换成品牌图，不再是字母占位", async () => {
  const html = await read("index.html");
  assert.match(
    html,
    /<img class="brand-mark" src="\.\/assets\/seelex-logo\.png" alt="" draggable="false">/,
    "顶栏品牌位要是图片（alt 留空：装饰图，旁边就是 Seelex 字样）"
  );
  assert.doesNotMatch(html, /class="brand-mark"[^>]*>S</, "字母占位应已移除");
  assert.match(html, /<link rel="icon" type="image\/png" href="\.\/assets\/seelex-logo\.png">/, "页面图标指向同一资产");

  // 资产本体：PNG、方图、体积可控（前端显示 26px，128 已给 HiDPI 留 4x）。
  const asset = await stat(new URL("./assets/seelex-logo.png", import.meta.url));
  assert.ok(asset.size > 0 && asset.size < 64 * 1024, `品牌图体积应在 64KB 内，实际 ${asset.size}B`);
  const head = (await readFile(new URL("./assets/seelex-logo.png", import.meta.url))).subarray(0, 24);
  assert.equal(head.subarray(0, 8).toString("hex"), "89504e470d0a1a0a", "要是 PNG");
  assert.equal(head.readUInt32BE(16), 128, "宽 128");
  assert.equal(head.readUInt32BE(20), 128, "高 128（方图，icon 复用同一份品牌图）");

  const css = (await read("styles.css")).replace(/\/\*[\s\S]*?\*\//g, "");
  const mark = css.match(/^\.brand-mark \{[^}]*\}/m);
  assert.ok(mark, "要找得到 .brand-mark 规则");
  assert.match(mark[0], /object-fit:\s*contain/, "图片式品牌位不拉伸");
  assert.match(mark[0], /border-radius:\s*var\(--r-lg\)/, "保留原圆角");
  assert.doesNotMatch(mark[0], /place-items:\s*center/, "字母盒的居中排版已不再适用");
});
