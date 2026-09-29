// 「文本按钮」的组件库皮：前端里凡是长得像文本的行内按钮（提交标题 / 文件路径 /
// 会话名…），都必须自己抹掉 vendor/pico.min.css 的 button 皮（padding / border /
// background）。pico 的按钮背景走 `--pico-primary-background` → 桥接到 `--accent`，
// 而这些按钮的文字色多半是 `--text-strong`；浅色皮肤下两者同为 #1f2328。
//
// 现场（真机截图）：提交记录的「提交标题」列整列变成一块黑条，点得开、hover 也有
// 反应，就是读不出字——`.git-log-subject` 是**唯一**一枚只带裸类名（没有
// .text-button / .stack-button / .tree-file 这类皮肤类兜底）的行内按钮。
//
// 这条测试把「裸类名按钮必须有 background」立成通用护栏：新加一枚裸按钮却忘了
// 抹皮，会在这里当场失败，而不是等到截图里出现第二块黑条。
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import test from "node:test";

const DIR = new URL("./", import.meta.url);
const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

// cssRules 剥掉注释后按 { 选择器 → 声明体 } 展开（styles.css 里没有嵌套规则）。
function cssRules(css) {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (match[1].trim().startsWith("@")) continue;
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

// classWearsBackground 判断某个类名是否在 styles.css 里拿到了一份背景声明
// （显式 transparent 也算：那是"故意不留皮"的写法）。
function classWearsBackground(rules, className) {
  const hit = new RegExp(`\\.${className.replace(/[-]/g, "\\-")}(?![\\w-])`);
  return rules.some(rule => hit.test(rule.selector) && /background(-color)?\s*:/.test(rule.body));
}

// classTokens 从一枚 <button> 标签里取出**静态**类名。
// 类名常与模板表达式混写（`class="work-status-btn${current === value ? " is-active" : ""}"`），
// 直接按引号切会把 `value` / `===` / `?` 当成类名。这里按花括号配平跳过 `${...}`
// 整段表达式，只留下字面量；剩下的还要长得像类名（小写字母打头）。
function classTokens(tag) {
  const at = tag.indexOf('class="');
  if (at < 0) return [];
  let literal = "";
  let depth = 0;
  for (let index = at + 7; index < tag.length; index++) {
    const ch = tag[index];
    if (depth === 0 && ch === "$" && tag[index + 1] === "{") {
      depth = 1;
      index++;
      literal += " ";
      continue;
    }
    if (depth > 0) {
      if (ch === "{") depth++;
      else if (ch === "}") depth--;
      continue;
    }
    if (ch === '"') break;
    literal += ch;
  }
  return literal.split(/\s+/).filter(token => /^[a-z][a-z0-9-]*$/.test(token));
}

test("裸类名的行内按钮必须自己抹掉组件库的按钮皮", async () => {
  const rules = cssRules(await read("styles.css"));
  const files = (await readdir(DIR)).filter(name => name.endsWith(".js") && !name.endsWith(".test.mjs"));

  const offenders = [];
  const seen = new Set();
  for (const file of files.sort()) {
    const source = await read(file);
    for (const tag of source.matchAll(/<button[^>]*>/g)) {
      const classes = classTokens(tag[0]);
      if (!classes.length) continue; // 没写类名的按钮靠上下文选择器上色
      if (classes.some(name => classWearsBackground(rules, name))) continue;
      const key = `${file}:${classes.join(".")}`;
      if (seen.has(key)) continue;
      seen.add(key);
      offenders.push(`${key} —— 该按钮会继承 pico 的 --pico-primary-background（= --accent）`);
    }
  }

  assert.deepEqual(offenders, [], `这些按钮只有裸类名、又没有任何 background 规则：\n${offenders.join("\n")}`);
});

test("提交标题按钮：抹掉皮 + 文字色不与组件库背景撞色", async () => {
  const css = await read("styles.css");
  const rules = cssRules(css);
  const subject = rules.find(rule => rule.selector === ".git-log-subject");
  assert.ok(subject, "要找得到 .git-log-subject 规则");

  assert.match(subject.body, /padding:\s*0/, "标题按钮不能吃组件库的内边距");
  assert.match(subject.body, /border:\s*0/, "标题按钮不能吃组件库的描边");
  assert.match(subject.body, /background:\s*transparent/, "标题按钮必须是透明的（否则就是那块黑条）");
  assert.match(subject.body, /color:\s*var\(--text-strong\)/, "标题仍用正文强调色");
  assert.match(subject.body, /text-overflow:\s*ellipsis/, "窄栏里仍然省略而不是换行");

  // 撞色的前提是真实存在的（这就是当年变成黑条的原因）：pico 的按钮背景桥接到
  // --accent，而浅色基座里 --accent 与 --text-strong 是同一个色值。哪天这两者
  // 分开了，这条断言会提醒我们重新评估这块的对比度口径。
  assert.match(css, /--pico-primary-background:\s*var\(--accent\)/, "pico 按钮背景仍桥接到 --accent");
  const lightBase = css.match(/:root \{[\s\S]*?\n\}/);
  assert.ok(lightBase, "要找得到浅色基座的 :root 规则");
  const accent = lightBase[0].match(/--accent:[^;]*/)?.[0] || "";
  const textStrong = lightBase[0].match(/--text-strong:[^;]*/)?.[0] || "";
  assert.match(accent, /#1f2328/, "浅色 --accent 的兜底值是 #1f2328");
  assert.match(textStrong, /#1f2328/, "浅色 --text-strong 与 --accent 同色（所以透明背景是唯一解）");
});
