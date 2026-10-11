# vendor（前端第三方资源）

## 生态位

`gui/frontend/dist` 是**零构建**的静态前端，运行期从 `go:embed` 的
embeddedFrontend 读取资源，**没有网络访问**。第三方样式/脚本因此必须以
「落盘 + 版本 + 许可」的方式放在这里，不允许 CDN 引用。

## 内容

| 文件 | 用途 | 版本 | 许可 |
|---|---|---|---|
| `pico.min.css` | 组件库：元素基线 + 通用组件皮（按钮/表单/表格/压缩/对话框） | @picocss/pico 2.1.1 | MIT（见 `PICO-LICENSE.md`） |
| `marked/marked.min.js` | Markdown 渲染（文件预览） | marked | MIT |
| `highlightjs/highlight.min.js`、`languages.all.min.js` | 代码高亮 | highlight.js | BSD-3-Clause |
| `purify/purify.min.js` | 预览 HTML 净化（DOMPurify） | DOMPurify | Apache-2.0 / MPL-2.0 |
| `docx-preview/docx-preview.min.js` | Word 文档预览 | docx-preview | MIT |
| `pdfjs/pdf.min.js`、`pdf.worker.min.js` | PDF 预览 | PDF.js | Apache-2.0 |
| `mermaid/mermaid.min.js` | markdown 里 ```mermaid 围栏的图表渲染 | mermaid 10.9.5（UMD 单文件，无动态 chunk） | MIT（`mermaid/LICENSE`） |
| `xterm/xterm.js`、`xterm.css`、`xterm/addon-fit.js` | 下栏终端仿真器 + 容器自适应插件 | xterm 5.3.0 / @xterm/addon-fit 0.10.0 | MIT（`xterm/LICENSE`、`xterm/LICENSE.addon-fit`） |

> 上表中带版本的条目是本次引入时登记的（Pico）；既有条目的具体版本号在各自
> 压缩文件的头部注释里，补充登记时一并回填。

## 使用方式与边界

- `index.html` 在 `styles.css` **之前**引入 `vendor/pico.min.css`：Pico 提供
  元素级基线与通用组件皮（按钮/表单/表格/详情压缩/对话框），Seelex 的
  `styles.css` 里所有类选择器特异性更高，因此既有组件外观不会被它改掉；
- `styles.css` 末尾的「组件库桥接」段把 `--pico-*` 变量映射到 Seelex token
  （黄铜主信号 + 铁蓝石墨底 + 暖纸白正文），第三方组件因此不会带进自己的一套
  配色；同时把 Pico 给 `section`/`button` 的默认外边距在 `.app-shell` 内归零，
  避免与三栏网格布局打架；
- 升级第三方文件时：更新本表版本号、重新落盘文件、跑
  `node --test gui/frontend/dist/*.test.mjs` 并做一次真机目视验证。

## 按需加载（重型 vendor 不走 index.html 同步面）

`vendor-loader.js: ensureVendorScript` 是唯一的按需注入通道：首次真的走到该类型
预览时才注入 `<script>`，并发共用 in-flight Promise，失败/超时不缓存（可重试）。

| 资产 | 触发点 | 就绪判定 |
|---|---|---|
| `pdfjs/pdf.min.js` | 打开 `.pdf` 预览（`file-preview.js: renderPDF`） | `window.pdfjsLib` |
| `docx-preview/docx-preview.min.js` | 打开 `.docx` 预览（`file-preview.js: renderWord`） | `window.docx.renderAsync` |
| `xterm/xterm.js`、`xterm/addon-fit.js` | 第一次真的开下栏终端（`terminal-panel.js`） | `window.Terminal` / `window.FitAddon` |
| `mermaid/mermaid.min.js` | 第一次遇到 ```mermaid 围栏（`mermaid-diagram.js: hydrateMermaid`） | `window.mermaid.render` |

`index.html` 仍同步加载的只有：`pico.min.css`、`xterm.css`（样式）与
`highlightjs/`、`marked/`、`purify/`（消息/预览的基础渲染面）。新增重型脚本必须
补进本表并走 `ensureVendorScript`，不要在 `index.html` 尾部再加一条 `<script>`。

### mermaid 的离线与安全边界

- **离线**：`mermaid.min.js` 是 UMD 单文件（无 CDN、无动态 chunk 请求），随 embed
  打包进二进制；图内文本不走网络。
- **安全**：mermaid 产出的 SVG 经 `DOMPurify`（`USE_PROFILES: {svg, svgFilters}`）
  白名单放行后才写进画布；净化器缺席或净化失败一律**不放行**，退回源码代码块。
- **降级**：库未就绪 / 超时 / 渲染抛错 / `securityLevel: "strict"` 拒渲染，都保留
  源码代码块（`pre.mermaid-source`）并标 `data-mermaid-state="failed"`，不白屏。

## Review 指南

- 新增 vendor 资源必须同时提交版本与许可信息，禁止只丢一个 min 文件；
- 不允许出现指向 CDN / 远程字体的引用（运行期无网络）；
- 桥接层只做变量映射与必要归零，不用 `!important` 硬盖组件样式。
