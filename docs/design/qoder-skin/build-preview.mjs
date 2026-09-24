// 把 docs/design/qoder-skin/replica.html 打包成**单文件离线预览**：
//   docs/design/qoder-skin/build/seelex-qoder-preview.html
//
// 为什么要打包：replica.html 靠三条同源相对路径引用真 CSS（pico / styles.css / themes/<id>.css），
// 拷到别的机器（例如虚拟机）就要连目录一起拷。这个脚本把那几个文件**内联**进去，
// 产物是一个自包含 HTML：双击即看，断网也能看，换肤仍只换 token。
//
// 用法：node docs/design/qoder-skin/build-preview.mjs
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../../..");
const distDir = join(root, "gui/frontend/dist");
const outDir = join(here, "build");
const outFile = join(outDir, "seelex-qoder-preview.html");

const read = (p) => readFileSync(p, "utf8");
const SKIN_IDS = ["qoder", "graphite", "verdigris", "paper", "silver"];

// 皮肤文件里唯一的规则是 `:root { ... }`。内联时把选择器换成
// `html[data-skin="<id>"]`，三份皮肤才能共存、按属性切换（不靠 <link> 换 href）。
function skinBlock(id) {
  const css = read(join(distDir, "themes", `${id}.css`));
  const scoped = css.replace(/^\s*:root\s*\{/m, `html[data-skin="${id}"] {`);
  if (!scoped.includes(`html[data-skin="${id}"]`)) {
    throw new Error(`皮肤 ${id} 里没找到 :root 规则，内联失败`);
  }
  return `/* ---- skin: ${id} ---- */\n${scoped}`;
}

let html = read(join(here, "replica.html"));

// 1) 三条外链 → 内联 <style>（xterm 是终端仿真器的样式，静态预览用不到，直接去掉）
html = html.replace(
  /<link rel="stylesheet" href="\.\.\/\.\.\/\.\.\/gui\/frontend\/dist\/vendor\/pico\.min\.css">/,
  `<style>\n/* vendor/pico.min.css（内联） */\n${read(join(distDir, "vendor/pico.min.css"))}\n</style>`
);
html = html.replace(
  /\s*<link rel="stylesheet" href="\.\.\/\.\.\/\.\.\/gui\/frontend\/dist\/vendor\/xterm\/xterm\.css">/,
  ""
);
html = html.replace(
  /<link rel="stylesheet" href="\.\.\/\.\.\/\.\.\/gui\/frontend\/dist\/styles\.css">/,
  `<style>\n/* styles.css（内联，含第 27 节 Qoder 基座） */\n${read(join(distDir, "styles.css"))}\n</style>`
);

// 2) 皮肤外链 → 五份带作用域的内联皮肤（按 <html data-skin> 切换）
html = html.replace(
  /<link id="seelex-skin" rel="stylesheet" href="\.\.\/\.\.\/\.\.\/gui\/frontend\/dist\/themes\/qoder\.css">/,
  `<style>\n${SKIN_IDS.map(skinBlock).join("\n")}\n</style>`
);

// 3) 换肤脚本：单文件版没有 <link> 可换 href，只改 <html> 上的 data-skin / data-theme
html = html.replace(
  `      document.getElementById("seelex-skin").setAttribute("href", skin.file);\n`,
  ""
);
html = html.replace(
  "<title>Seelex · Qoder 皮肤复刻（静态预览）</title>",
  "<title>Seelex · Qoder 皮肤（单文件离线预览）</title>"
);

mkdirSync(outDir, { recursive: true });
writeFileSync(outFile, html, "utf8");
console.log(`built ${outFile}  (${(Buffer.byteLength(html, "utf8") / 1024).toFixed(0)} KB)`);
