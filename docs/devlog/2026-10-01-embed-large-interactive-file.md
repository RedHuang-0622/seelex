# 会话内渲染块：一份 141 KB 的本地交互 HTML 为什么「放不进去」（实测）

- 日期：2026-10-01
- 范围：只读实测，**未改产品代码**；探针留在 `tmp/embed-repro/{probe2,probe3,probe4}.mjs`
- 上游：用户口径（"`G:\黄子然简历\行程\广州地铁简版地图_人流热力.html` 这种有交互的 svg 的渲染
  仍然无法在沙箱做到，你还是需要改下"）、`docs/devlog/2026-10-03-svg-embed-interactivity.md`
  （沙箱契约与跨帧动作通道）、上一轮的绕过产物 `G:\黄子然简历\行程\{build_embed.py,_block.html,_chunks\}`

## 0. 一句话结论

**卡住的不是沙箱的渲染/交互能力，是「内容怎么进块」。** 走 app 的真实路径
（`renderMarkdown` → `renderHtmlEmbed` → 真实沙箱 iframe）实测：这份文件在块内**渲染完整、
交互正常、零报错**；块内唯一做不到的是**把一份已存在的本地文件拉进来**——所有本地读取途径
都被封死，唯一入口是"模型在围栏正文里手写"。141 KB 的文档因此只能靠重打 132,455 字进块。

## 1. 沙箱内实测（探针 `probe2.mjs` / `probe3.mjs`）

被测原文：`G:\黄子然简历\行程\广州地铁简版地图_人流热力.html`（141,728 B / 132,455 字），
围栏 ` ```seelex-html title="广州地铁" height=640 interactive=1 `，宿主页带真实
`gui/frontend/dist/styles.css`。**块内状态用 Playwright 的 frame 句柄直接读**
（无 `allow-same-origin` 也能读：按 CDP frame tree 定位，不依赖 JS 同源）。

| 量 | 块内实测 | 直接打开（对照） |
|---|---|---|
| `window.__READY__` | `true`（脚本跑了） | `true` |
| `window.seelex` | `object`（桥已注入） | — |
| `#stDots circle` | 171 | 171 |
| `#labels text` | 171 | 171 |
| `#heatStation circle` | 171 | 171 |
| `#parkShapes path` | 16 | — |
| `.row` / `.ln` / `radialGradient` | 14 / 14 / 13 | 14 / 14 / 13 |
| SVG 渲染尺寸 | 794×496（frame 862×640） | 1252×783 |
| `pageerror` / console error | **0** | **0** |

真实交互（同一份块内，`probe3.mjs`，Playwright 真鼠标事件）：

| 动作 | 结果 |
|---|---|
| 拖 `#sliderHit` 到 75% | 时段读数 `18:00` → `19:00`（`renderHeat`+`renderPanel` 跑了） |
| hover 第 21 个站点圆点 | `#tip` 从 `off` 变可见，`#tipT` = `昌岗` |
| 点第 4 条线（`.row`） | `.row.on` = 1，`#tipT` = `5号线 · 聚焦`，35 个元素进 `.dim` |

形状无关：整份文档（含 `<!doctype>/<html>/<head>/<body>`）与剥壳片段**结果完全一致**（都
171/171/171/16）；`loading="lazy"` 有/无也一致。

## 2. 块内读本地文件：全部封死（探针 `probe4.mjs`）

在真实沙箱块内逐一试，`location.origin` = `null`、`protocol` = `about:`：

| 途径 | 结果 |
|---|---|
| `fetch("file:///…/CHANGELOG.md")` | `TypeError`（CSP `default-src 'none'` + opaque origin） |
| `XMLHttpRequest` 同一 URL | `error` 事件 |
| `new Image().src = "file:///…"` | `error` 事件 |
| `<script src="file:///…/html-embed.js">` | `error` 事件 |
| `<link rel=stylesheet href="file:///…/styles.css">` | `error` 事件 |

所以块内容的**唯一来源**就是宿主拼 srcdoc 时手里那块正文——今天等于"模型在围栏里写的那份
文本"。`parseEmbedInfo` 也只认 `title` / `height` / `interactive`，没有任何"引用外部文件"的口径。

## 3. 尺寸账（为什么"手写进块"这条路本身就不成立）

| 量 | 值 |
|---|---|
| 原文件 | 141,728 B / 132,455 字 |
| 围栏正文（模型要原样输出） | 132,455 字 |
| 渲染后 HTML（srcdoc 转义） | **431,292 字 / ≈431 KB**（其中「查看源码」的 `<pre>` 又把原文复制一遍） |
| 现有高度钳制 | 120–640 px（`MIN_HEIGHT`/`MAX_HEIGHT`） |

一条视觉回答要模型手打 13 万字、同时在会话里落 431 KB DOM——这才是上一轮不得不去做
`build_embed.py`（去重冗余标记 + gzip + base64 + 运行时 `DecompressionStream` 重建，141 KB →
35 KB）和 `split_chunks.py`（切成 9 个 3.8 KB 分片跨消息贴）的原因。**那是绕过，不是修复**：
它靠"作者手工预编译"，模型在真实会话里不这么干，也没人保证每次都对得上。

## 4. 红鲱鱼：`gs.appendChild is not a function`（自查为伪）

上一轮的 `tmp/embed-repro/diag.mjs` 报过一条 `pageerror: gs.appendChild is not a function`。
**这是探针自己的 bug，不是文件或沙箱的问题**：`diag.mjs` 用
`page1.replace("__HTML__", frag.html)` 拼宿主页，而 JS 的 `String.replace` 会把替换串里的
`$$` 吃成一个 `$`——原文第 355 行 `var $$ = function (s) { …querySelectorAll(s)… }` 于是变成
`var $ = …`，把第 354 行的 `$ = querySelector` 覆盖掉；第 436 行 `var gs = $("#heatStation")`
拿回的就是一个 **Array**，`Array.appendChild` 自然"不是函数"。

`diag2.mjs`（用模板字符串，无该处替换）8 个变体全部 `errors: []`；`probe2.mjs`（模板字符串）
块内零报错且功能完整。**两个方向都指向同一结论：报错来自探针字符串替换，不来自被测对象。**

## 5. 未决：两条候选改法（都属"入口"，不动沙箱契约）

沙箱属性（`sandbox="allow-scripts"`、无 same-origin）、CSP、跨帧动作白名单都**不需要改**。

- **A｜围栏 `src=`（块引用一个已存在的文件）**：模型写
  ` ```seelex-html src="行程/图.html" height=640 `，**宿主**经 `Bridge.WorkspaceFileContent`
  一类通道读字节、用现成的 `buildEmbedDocument` 包成 srcdoc。
  代价：要给"读文件"定边界（工作区相对 vs 绝对路径、上限、敏感文件过滤）。注意本例文件在
  **工作区之外**（`G:\黄子然简历\…`），只认工作区相对路径就够不到它。
  能力增量≈0：模型本来就能 `read_file` 并把内容贴进回答。
- **B｜文件预览抽屉加「沙箱渲染」档**：`file-preview.js` 现在把 `.html/.htm` 当**代码高亮**
  显示（`CODE_EXTENSIONS`），`.svg` 走图片档；给它们加一档"沙箱渲染"，复用同一套
  `buildEmbedDocument`。文件来自用户自己点开的那条路径，模型不参与，边界最保守；但块不会
  出现在会话里。

## 6. 复现

```powershell
node G:\Program\go\seelex\tmp\embed-repro\probe2.mjs   # 块内渲染完整性
node G:\Program\go\seelex\tmp\embed-repro\probe3.mjs   # 块内交互（拖/悬停/点）
node G:\Program\go\seelex\tmp\embed-repro\probe4.mjs   # 块内读本地文件（预期全封死）
```

依赖：`G:\黄子然简历\行程\survey\pw\node_modules\playwright`（本机既有）。

## 7. 边界（未验证）

- 端到端用的是**真实渲染件 + 真实宿主接线**跑在 headless Chromium 上；**WebView2 真机 GUI
  未逐操作冒烟**，与上一轮 devlog 的保留项相同。
- 交互只验了三类（滑块拖拽 / 悬停提示 / 行点击聚焦）；平移缩放、可写画布这类重交互本就不在
  embed 的口径里（见上一轮 devlog §6）。
