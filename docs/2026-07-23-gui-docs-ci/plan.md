# GUI 文档与 CI 实现方案

## 设计目标

- `gui` 分支 push 和以 `gui` 为目标的 PR 能直接触发 CI。
- GUI 前端的 Node 测试、JS 语法、Application/Bridge 契约和 Windows production build 都有明确门禁。
- 每个 GUI 模块都有职责、边界、数据结构、流程、错误策略、决策和源码位置。
- 代码审查可以从功能点追溯到详设、实现段落和自动化测试。
- Effort 作为常驻四档滑杆独立于 Runtime 弹窗；`max` 档有动态紫色光效并支持 reduced-motion 降级。

## 方案对比

| 维度 | 方案 A：三平台 build job 内加入 Node | 方案 B：独立 gui-tests job |
|------|------------------------------------|--------------------------|
| 执行次数 | 每次 3 次，重复 | 每次 1 次 |
| 门禁可见性 | 混在 build matrix | 独立稳定检查名 |
| 平台职责 | Node 与 GUI production build 混合 | Ubuntu 测逻辑，Windows 编译桌面产物 |
| 故障定位 | 较慢 | 直接定位 GUI frontend/contract |
| 扩展性 | 后续 E2E 难隔离 | 可独立增加 artifact/E2E |

## 推荐：方案 B

独立 `gui-tests` job 运行 Node 22、JS syntax、前端测试和 `application/gui` Go 契约；现有 Windows matrix 保持 production tags build。最大风险是真实 WebView 行为仍无法由 headless job覆盖，因此在文档中明确保留手工/E2E 门禁。

## 文档模块划分

| 模块 | 文档 | 源码范围 |
|------|------|---------|
| Application 协议与会话 | `modules/application-protocol.md` | `application/state.go`、`event.go`、`app.go`、`chat.go` |
| Desktop Bridge 与生命周期 | `modules/desktop-bridge.md` | `gui/bridge.go`、`run_wails.go`、`assets.go` |
| 客户端状态与事件归并 | `modules/client-state.md` | `protocol.js`、`client-state.js` |
| 会话渲染与内容安全 | `modules/conversation-rendering.md` | `conversation-view.js`、`components.js`、`chat-view.js`、`markdown.js` |
| Shell、命令和交互 | `modules/shell-and-interactions.md` | `index.html`、`app.js`、`styles.css` |
| Effort 控件 | `modules/effort-control.md` | `effort-control.js`、`index.html`、`styles.css` |
| CI 与验证 | `ci-and-testing.md` | `.github/workflows/ci.yml`、Go/Node tests |

## 实现步骤

| # | 步骤 | 文件 | 模式 |
|---|------|------|------|
| 1 | 拆分 Effort controller | `effort-control.js` | Controller + 纯档位映射 |
| 2 | 将 Effort 移至 topbar | `index.html`、`app.js` | 常驻控件 + Snapshot 权威状态 |
| 3 | 实现 Max 动态紫色光效 | `styles.css` | CSS 状态机 + reduced-motion |
| 4 | 扩展 branch triggers | `.github/workflows/ci.yml` | Policy as Code |
| 5 | 添加 GUI tests job | `.github/workflows/ci.yml` | 独立质量门禁 |
| 6 | 编写模块详设与 ADR | `docs/gui/**` | C4-lite、ADR、Traceability Matrix |
| 7 | 修正文档索引与事实状态 | `docs/README.md`、`feature-instrumentation.md` | 单一文档入口 |
| 8 | 本地执行 CI 等价命令 | Node/Go/Git | Contract Test |
| 9 | 生成带代码位置的最终审查 | `docs/gui/code-review.md` | 功能追溯 |

## 测试策略

- `node --check` 覆盖所有 `gui/frontend/dist/*.js`。
- `node --test gui/frontend/dist/*.test.mjs` 覆盖协议、客户端、Markdown 和组件。
- Effort controller tests 覆盖四档映射、拖动预览、单次提交、失败回滚与 Max 状态。
- `go test ./application ./gui -count=1` 覆盖 Core/Bridge 契约。
- `go build -tags "gui,desktop,production" ./...` 验证 Wails 手工构建标签。
- `go test ./...`、`go vet ./...` 防止文档/CI改动掩盖仓库回归。
- 静态核对 workflow trigger、job 名、Node/Go setup 和命令完整性。

## 回滚方案

删除 `gui-tests` job并将 triggers 恢复为仅 `main` 即可回滚 CI；Effort 可回退为 Runtime modal 内的 segmented buttons；`docs/gui` 为纯文档，不影响运行时。
