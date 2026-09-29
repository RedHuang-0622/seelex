# 提交标题变黑条 + 设置弹窗够不到底（2026-09-29）

> 日期: 2026-09-29 | 范围: `gui/frontend/dist/styles.css`、`gui/frontend/README.md`
> 回归: `gui/frontend/dist/text-button-chrome.test.mjs`（新，2 例）、
> `gui/frontend/dist/settings-modal-scroll.test.mjs`（新，3 例）
> 承接: [2026-09-29-commit-detail-and-file-content.md](2026-09-29-commit-detail-and-file-content.md)
> （上一批给提交记录加了下钻，这轮修的是它右上栏的观感）

## 1. 用户口径

两条：

1. **「提交记录旁边是黑的」** —— 右栏「提交记录」面板里，提交的标题那一列整列是一块
   黑色圆角块，读不出字；
2. **「设置下的内容需要有滚轮并且支持适应屏幕大小」** —— 设置弹窗内容比窗口高时滚不动，
   底部的「保存并切换」永远点不着。

## 2. 侦察（先把"是什么在画这块黑"钉死，再动 CSS）

静态推理会走进死胡同：`styles.css` 里 `.git-log-subject` 根本没有 `background` 声明，
所以"黑"只可能来自**没被覆盖的组件库基线**。用真机像素取证：

- 屏幕取点（PowerShell + `System.Drawing.CopyFromScreen`）测出黑块是
  `R31 G35 B40` = **#1f2328**，几何是 `x 1635..1805 / y 414..980`（圆角、贴着面板右内边）；
- 在 `styles.css` 里 `#1f2328` 恰好是浅色基座的 `--text` / `--text-strong` / `--tick-hot`，
  以及缺省皮肤下 `--accent` 的兜底值——`.git-log-subject` 的 `color` 正是 `--text-strong`；
- 决定性实验：把 `vendor/pico.min.css` + `styles.css` + **真的** `git-log-view.js`
  放进 headless Chromium（本机 Edge，`--dump-dom` 里回填 `getComputedStyle`），
  读出 `.git-log-subject` 的 `background-color` = `rgb(31, 35, 40)`，
  `padding` = `10.125px 13.5px`、`border` = `1px`、`height` = `38.75px`。
  即：**pico 的 button 皮没被抹掉**。

`styles.css` 把 pico 桥接进 Seelex token 的那一段里有
`--pico-primary-background: var(--accent)`，pico 的 `button { background-color:
var(--pico-primary-background) }` 就是那块底。而 Seelex 的约定是"文本按钮自己抹皮"——
`.git-log-hash` / `.git-commit-path` / `.changes-path` / `button.team-library-name`
都写了 `padding: 0; border: 0; background: transparent`，**只有 `.git-log-subject` 漏了**。

为什么全前端只有它中招：扫了一遍 `dist/*.js` 里所有 `<button class="…">`，其余"看起来像
文本"的按钮都挂着 `text-button` / `stack-button` / `tree-file` 这类皮肤类，
`.git-log-subject` 是**唯一**一枚只有裸类名的按钮——既没有自己的 `background`，
也没有可借底的同伴类。

## 3. 两条修复

### 3.1 `.git-log-subject`：补齐文本按钮的归零三件套

`padding: 0; border: 0; background: transparent`（外加 `text-align: left; cursor: pointer`
与 `:hover` / `:focus-visible`，口径同 `.git-log-hash`）。顺带把行高还回去：
39px 高的 pico 按钮曾把每一行撑高。

### 3.2 `.settings-card`：自己封顶 + 自己滚

`.modal` 是 `position: fixed` + `place-items: center` 的网格遮罩，**自己不带 `overflow`**。
卡片一高过视口，两端就被切在屏幕外，滚轮没有落点——设置面板把「存储 / 外观（皮肤 + 深浅
两排）/ 终端」三段叠在一起，正好高过小窗口。改法与 `.runtime-card` 同一套：
`max-height: min(760px, calc(100vh - 48px))` + `overflow-y: auto` + `overscroll-behavior: contain`
（`48px` = 遮罩上下各 24px 的内边距）。遮罩不复制第二份滚动，纵向滚动容器只有一个。
复用同一枚卡片类的 `#scheduled-task-modal`（带 `data-resizable`，`overflow` 特异性更高）
只补上它缺的高度上限。

## 4. 验证

- **真机（修前取证）**：屏幕取点 `R31 G35 B40` = #1f2328，几何 `x 1635..1805 / y 414..980`。
  修后的实机画面要等 post-commit 重建 + 重启进程才看得到（正在跑的进程里嵌的还是旧前端），
  因此这一条不作为本次的验收证据。
- **渲染域（headless Chromium，同一份 styles.css）**：
  `.git-log-subject` → `background: rgba(0, 0, 0, 0)`、`padding: 0px`、`border: 0px`、
  `height: 16.5px`（修前分别是 `rgb(31, 35, 40)` / `10.125px 13.5px` / `1px` / `38.75px`）。
- **设置弹窗**：视口 327px 高时卡片 `max-height: 279px`、`overflow-y: auto`、
  `scrollHeight 780 > clientHeight 277`，且 `scrollTop` 真的能推到 503——
  轮子有落点、两端都在视口内（`cardTop 47 / cardBottom 316`）。
- **回归钉子**：`text-button-chrome.test.mjs` 第一条是通用扫描（凡是"裸类名 + 没有任何
  `background` 规则"的按钮一律失败）。把 `.git-log-subject` 的 `background: transparent`
  临时删掉后，它确实报 `git-log-view.js:git-log-subject`（钉子会咬人，不是摆设）。
  前端套件新增 5 例后 540 例全绿。

## 5. 口径（写进 gui/frontend/README.md）

1. 组件库的 `button` 皮属于**基线**，自绘的"文本按钮"必须自己抹掉（不只是颜色，
   还有 `padding` / `border` / `background`），否则它会带着一块 `--accent` 实心底出现；
2. 弹窗卡片要自己封顶 + 自己滚：`place-items: center` 的遮罩帮不上忙，溢出两端是真丢。
