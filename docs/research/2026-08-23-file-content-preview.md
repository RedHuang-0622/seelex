# 文件内容详情查看（File Preview）方案调研

> 调研日期：2026-08-23
> 性质：调研报告（调研结论不是已实现能力；采用后需在架构文档、模块 README 与代码中落地）
> 关联设计：[`docs/gui/modules/workspace-sandbox.md`](../gui/modules/workspace-sandbox.md) §9 File preview
> 关联工作包：[`docs/2026-08-23-worktable-sharding-filetree/README.md`](../2026-08-23-worktable-sharding-filetree/README.md)

## 1. 目标与约束

### 1.1 场景

工作台右栏「工作树」落地后（v1 只提供目录树 + 文件数），下一步是点击文件
行查看内容详情：文件类型识别、语法高亮、行号、二进制/超大文件降级、会话
内证据（读取来源）联动。本调研回答「用什么方案与哪些开源库」。

### 1.2 技术底座（已核实的现状）

- 桌面容器：Wails v2，Windows WebView2（Chromium 内核，现代 ES 能力可用）。
- 前端：无 bundler、无 npm 构建链；`gui/frontend/dist/` 既是源码也是
  embed.FS 产物，原生 ES Modules + 手写 DOM 渲染。
- 安全基线：`gui/frontend/README.md` 规定所有模型/工具/用户文本进入 HTML
  前必须 escape 或经受控 Markdown renderer；禁止 raw HTML 与任意脚本；
  现有 `gui/frontend/dist/markdown.js` 提供 `escapeHtml`/`escapeAttribute`
  与安全渲染管线，可复用。
- 后端语言：Go（`go.mod` 为 seelex 主模块），文件系统工具链已有
  `seelebridge/security` ProjectScope/PathGate 语义。
- 既有设计约束（workspace-sandbox.md §9）：默认 128 KiB / 400 行，
  最大值由构造选项限制；检测 NUL、高比例不可打印字节或已知 binary MIME
  后不返回正文；未知编码不做有损转换；前端显示 metadata 与「二进制文件不
  预览」；高亮使用已转义的轻量 renderer，不从文件内容加载语言插件或执行
  内容。

### 1.3 硬约束清单

| 编号 | 约束 | 影响 |
|---|---|---|
| C1 | PathGuard：只接受相对路径/后端签名资源 ID，禁止绝对路径与逃逸 | 所有 preview 接口必须 containment 校验 |
| C2 | 不把原始文件内容长期留在 renderer state | preview 是 query 而非快照字段；切换/关闭即释放 |
| C3 | 文本必须 escape 后进入 HTML | 高亮方案必须保证转义发生在渲染前 |
| C4 | 二进制/超大/未知编码不预览 | 后端先探测，前端只展示 metadata |
| C5 | 无 bundler 的 embed 环境 | 第三方库要么 vendored 进 dist，要么后端预渲染，要么引入构建链 |
| C6 | 不执行内容中的脚本/语言插件 | 拒绝「从内容加载插件」类方案 |
| C7 | 主题与现有 Tracebench token 体系一致 | 高亮样式映射到 `--status-*`/`--paper` 等语义 token |

## 2. 候选方案对比

### 2.1 方案 A：后端截断文本 + 前端轻量高亮

后端按 C4 探测并截断（128 KiB / 400 行），返回纯文本 + 语言/行数 metadata；
前端用轻量高亮库把文本 token 化后以 escape 的 HTML 输出。

- 代表库：highlight.js、Prism、Shiki。
- 安全面：转义责任在前端；必须「先 escape 再拼 HTML」，高亮库只产出 token
  span 包住已转义文本，禁止把原文当 innerHTML。
- 体积：highlight.js 核心约 8.2 KB（min），按语言按需引入后常见语言组合
  可控制在 100 KB 内；Prism 核心约 2 KB（min+gzip），每语言 300–500 B；
  Shiki 用 JS 正则引擎（v3 默认）可免 WASM，但完整包较大，需
  `@shikijs/core` 细粒度按需。
- 集成成本：无 bundler 时需把库的 ESM 文件 vendored 进 `dist/vendor/`；
  highlight.js v11 原生提供 `es/` ESM 分语言文件，最省事；Prism 无官方
  ESM 产物；Shiki 依赖较多、按需打包需要构建器。
- 推荐度：中（v1.5 可选）。

### 2.2 方案 B：后端预高亮（Go 侧 chroma）

后端读取文件 → 截断/二进制检测 → 用 Go 语法高亮器产出「已转义 HTML」
（class 或内联样式），前端原样插入受控容器。

- 代表库：`github.com/alecthomas/chroma/v2`（MIT）。
- 安全面：高亮与转义都在 Go 侧完成，前端不接触原文 token；chroma 的
  HTML formatter 对文本 token 做 HTML 转义（v2 修复过早期 XSS 问题），
  集成测试仍须覆盖 `<script>`、`onerror=`、`&` 等注入样本；行号由
  `WithLineNumbers()` 生成。
- 体积：Go 依赖进二进制，前端零新增体积；样式是一段静态 CSS。
- 集成成本：无前端依赖、不改变无 bundler 现状；按文件扩展名/内容自动
  选 lexer（`lexers.Match`/`lexers.Analyse`）。
- 推荐度：高（v1 首选）。

### 2.3 方案 C：CodeMirror 6 只读模式

用 `EditorState.readOnly` + 语言包渲染只读代码视图，内置虚拟化与大文档
能力。

- 代表库：`codemirror`/`@codemirror/state`/`@codemirror/view`/
  `@codemirror/language`/`@codemirror/lang-*`（MIT）。
- 体积：核心约 150 KB（原始），完整常见配置约 300 KB gzip；ESM 原生。
- 安全面：CM6 只处理文本节点、不执行内容；仍建议配合 readOnly + 不启用
  补全/跳转等 worker 能力。
- 集成成本：无 bundler 下需 vendored 一整套 ESM 依赖树（CM6 是拆包架构，
  依赖关系多），手工维护成本最高；引入 Vite/esbuild 后成本显著下降。
- 推荐度：中（v2 目标，若产品需要行内交互/虚拟化大文件）。

### 2.4 方案 D：Monaco Editor 只读

VS Code 同源编辑器，能力最全但最重。

- 代表库：`monaco-editor`（MIT）。
- 体积：完整包约 2–5 MB gzip，且语言服务依赖 Web Worker（编辑器 worker、
  ts/css/json worker）；WebView2 下需处理 worker 加载与 asset 路径。
- 集成成本：无 bundler 场景极不友好；只读预览属于「杀鸡用牛刀」。
- 推荐度：低（不推荐）。

### 2.5 方案 E：diff 渲染（Changes 场景）

文件详情不只是单文件视图，还有 git 变更 diff。两条路线：

- Go 侧：`git diff`（参数数组、固定 `--no-ext-diff --no-textconv`）拿
  unified diff，用 chroma `diff` lexer 预高亮成已转义 HTML（与方案 B 同一
  管线，可复用）。
- 前端：`diff`（jsdiff，BSD-3-Clause）算行级 diff + 自绘；或 `diff2html`
  （MIT）直接产出已转义 diff HTML。
- 推荐度：优先复用方案 B 的 Go 管线（一次实现两处复用）；diff2html 作为
  前端备选，但同样面对无 bundler vendored 成本。

### 2.6 总对比表

| 维度 | A 前端轻量高亮 | B 后端 chroma 预高亮 | C CodeMirror 6 | D Monaco | E diff 渲染 |
|---|---|---|---|---|---|
| 前端体积 | ~2–100 KB 按需 | 0（Go 侧） | ~150–300 KB | 2–5 MB+worker | 0（Go 侧）或 ~10–50 KB |
| 转义位置 | 前端 | 后端 | 前端（文本节点） | 前端 | 后端/前端 |
| 无 bundler 适配 | 需 vendored ESM | 天然适配 | 需 vendored 多包 | 不友好 | 后端路线天然适配 |
| 大文件虚拟化 | 无（截断即可） | 无（截断即可） | 有 | 有 | 无 |
| 交互（行内跳转/搜索） | 弱 | 弱 | 强 | 最强 | 弱 |
| 与既有安全基线契合 | 中 | 高 | 高 | 中 | 高 |
| 集成成本 | 中 | 低 | 高 | 很高 | 低 |
| 推荐 | v1.5 可选 | **v1 首选** | v2 目标 | 不推荐 | 复用 B |

## 3. 开源库清单

| 库 | 许可证 | 体积/形态 | ESM/集成 | 安全注意 | 适配评估 |
|---|---|---|---|---|---|
| [`highlight.js`](https://github.com/highlightjs/highlight.js) | BSD-3-Clause | 核心 ~8.2 KB min；全量 180+ 语言 ~1.2 MB；按需可 <100 KB | v11 提供 `es/` 分语言 ESM，可 vendored 进 dist | 先 escape 原文再包 span；禁止把结果当纯 HTML 直插未受控容器 | v1.5 前端高亮首选 |
| [`Prism`](https://prismjs.com/) | MIT | 核心 ~2 KB min+gzip，每语言 300–500 B | 无官方 ESM 产物（UMD/IIFE），需 bundler 或改造 | 同样先 escape；自动高亮需注意 `textContent` 模型 | 备选；体积最小 |
| [`Shiki`](https://shiki.style/) | MIT | v1 完整包 ~6.9 MB raw / ~1.3 MB gz；v3 默认 JS 正则引擎免 WASM；`@shikijs/core` 可细粒度按需 | ESM；依赖较多，按需打包通常需要构建器 | 输出 token HTML 需配合受控转义；wasm 引擎额外 ~200 KB | 追求高保真 TextMate 高亮时可选 |
| [`chroma`](https://github.com/alecthomas/chroma) | MIT | Go 库，进二进制；前端 0 体积 | Go module `github.com/alecthomas/chroma/v2` | HTML formatter 转义文本 token；集成测试覆盖注入样本；`WithClasses` 与 `ClassPrefix` 配合现有 CSS token | **v1 后端预高亮首选** |
| [`CodeMirror 6`](https://codemirror.net/) | MIT | 核心 ~150 KB，常见配置 ~300 KB gzip | ESM 原生；多包依赖树 | 文本节点渲染不执行内容；readOnly 关闭编辑 | v2 目标，建议届时引入 Vite |
| [`monaco-editor`](https://github.com/microsoft/monaco-editor) | MIT | 2–5 MB gzip + worker | 需 worker/asset 配置 | worker 沙箱良好但复杂度高 | 不推荐（只读预览过重） |
| [`diff2html`](https://github.com/rtfpessoa/diff2html) | MIT | 约 10–50 KB 级 | 有 bundle/UMD 产物 | 输出 HTML 需确认转义策略 | 前端 diff 备选 |
| [`diff`（jsdiff）](https://github.com/kpdecker/jsdiff) | BSD-3-Clause | 小 | ESM/CommonJS 均可 | 仅产出结构数据，渲染需自绘并 escape | 前端行级 diff 备选 |

许可证提醒：BSD/MIT 均为宽松许可，vendored 进 `dist/` 时须保留 LICENSE
文件与版权声明（与 `go.sum`/第三方声明同规范）。

## 4. 推荐路线

### v1（本期落地）：后端 chroma 预高亮 + 已转义 HTML

理由：

1. 完全契合 C5（无 bundler）——前端零新增依赖，不改变构建纪律；
2. 转义与高亮都在 Go 侧，安全面收敛到一处（C3 最易验证）；
3. 与既有 Go 文件工具链（containment/PathGate）同进程，接口最小；
4. diff 场景（方案 E）可复用同一 lexer/formatter 管线。

落地步骤：

1. `workspace/tree.go`（或 `workspace/preview.go`）增加
   `PreviewFile(root, relPath, opts)`：containment 校验 → stat → 大小/
   NUL/二进制探测 → 截断 128 KiB/400 行 → `chroma` 按扩展名/内容选 lexer
   → HTML formatter（`WithClasses` + `ClassPrefix("hl-")` + 行号）→ 返回
   `FilePreview{meta, html, language, truncated, binary}`。
2. application 增加 optional `WorkspacePreviewPort` 与 Service 方法
   （只读 query，不进 Snapshot，C2）。
3. Bridge 绑定 `PreviewWorkspace(relPath)`，走 requestContext 与 typed
   error。
4. 前端 `worktree-view.js` 行内「预览」展开：插入后端 HTML 前再次校验
   容器白名单；把 chroma class 映射到 `styles.css` 的语义 token（C7），
   不得硬编码新色值。
5. 测试：注入样本（`<script>`、`onerror`、`&<>"'`、超长文件、二进制、
   NUL、`..` 逃逸、符号链接目录）覆盖安全与边界；node 侧覆盖渲染/转义
   契约；Go 侧覆盖 containment 与截断。

### v1.5（可选）：highlight.js ESM 前端高亮

当需要主题切换、点击行交互且愿意把 `highlight.js/es/` 部分语言 vendored
进 `dist/vendor/` 时启用；后端仍负责截断与二进制探测，前端负责
「escape → tokenize → span」。

### v2（目标）：CodeMirror 6 只读

当产品需要行内搜索、滚动虚拟化、折叠等编辑器体验时引入；建议同步引入
Vite/esbuild 构建链（改变 `gui/frontend/README.md` 的构建纪律，需先过
架构决策），再按需加载 `@codemirror/lang-*`。

### 不推荐

- Monaco（体积与 worker 复杂度，只读场景无收益）；
- 从文件内容加载语言插件/执行内容的任何方案（违反 C6）。

## 5. 参考资料

- highlight.js LICENSE（BSD-3-Clause）：
  <https://github.com/highlightjs/highlight.js/blob/main/LICENSE>
- highlight.js v11 ESM 产物与体积（`es/core.min.js` ~8.2 KB）：
  <https://github.com/highlightjs/highlight.js/blob/main/CHANGES.md>
- Prism 官方说明与下载（核心 ~2 KB）：
  <https://prismjs.com/>
- Shiki 正则引擎与包体积文档（v3 JS 引擎免 WASM；v1 测量 6.9 MB/1.3 MB gz）：
  <https://shiki.style/guide/regex-engines>、<https://shiki.style/guide/why>
- CodeMirror 6 官方与许可（MIT）：<https://codemirror.net/>、
  <https://github.com/codemirror/view>
- Monaco Editor（MIT）与体积讨论：<https://github.com/microsoft/monaco-editor>
- chroma（MIT，Go）：<https://github.com/alecthomas/chroma>
- diff2html（MIT）：<https://github.com/rtfpessoa/diff2html>
- jsdiff / npm `diff`（BSD-3-Clause）：<https://github.com/kpdecker/jsdiff>
- 前端无 bundler 现状与安全基线：
  <https://github.com/RedHuang-0622/seelex/blob/main/gui/frontend/README.md>
- File preview 既有设计约束：
  [`docs/gui/modules/workspace-sandbox.md`](../gui/modules/workspace-sandbox.md)
