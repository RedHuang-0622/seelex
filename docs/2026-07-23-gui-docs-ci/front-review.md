# GUI 文档与 CI 前置审查

## 需求摘要

为 Wails GUI 建立模块级详细设计、实现决策和带源码位置的功能审查追溯，让 `gui` 分支直接运行 GUI 自动化门禁，并将 Effort 从 Runtime 弹窗拆为常驻四档滑杆，在 `max` 档提供可降级的动态紫色光效。

## 现状结论

- `.github/workflows/ci.yml` 存在，但 `push` 和 `pull_request` 只监听 `main`；直接 push 到 `gui` 不触发。
- Windows build job 已执行 `go build -tags "gui,desktop,production" ./...`。
- CI 没有执行 `gui/frontend/dist/*.test.mjs`，因此协议 reducer、Markdown、组件和客户端状态没有远端门禁。
- GUI 设计资料分散在日期型 front-review/plan 中，没有面向维护者的模块索引和统一代码追溯矩阵。
- `docs/feature-instrumentation.md` 中会话恢复、协议版本和 GUI 测试状态已经落后于当前实现。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `.github/workflows/ci.yml` | 修改 | triggers、jobs | 监听 `gui` 并增加独立 GUI tests job |
| `gui/frontend/dist/index.html` | 修改 | topbar、runtime modal | 新增常驻 Effort 滑杆并移除弹窗内旧控件 |
| `gui/frontend/dist/app.js` | 修改 | imports、elements、renderRuntime | 接入独立 Effort controller，保持 Snapshot 为权威状态 |
| `gui/frontend/dist/effort-control.js` | 新增 | 全文 | 封装四档映射、预览、提交、失败回滚和可访问性状态 |
| `gui/frontend/dist/effort-control.test.mjs` | 新增 | 全文 | 覆盖档位映射、Max 状态、提交和失败回滚 |
| `gui/frontend/dist/styles.css` | 修改 | topbar、effort、responsive/reduced-motion | 实现进度条视觉、Max 紫色动效和响应式降级 |
| `gui/bridge_test.go` | 修改 | embedded shell contract | 断言 Effort 控件位于 Runtime modal 之外 |
| `docs/README.md` | 修改 | 文档索引 | 增加 GUI 维护文档入口 |
| `docs/feature-instrumentation.md` | 修改 | GUI/协议/会话/质量功能点 | 修正与 HEAD 不一致的状态和证据 |
| `docs/gui/README.md` | 新增 | 全文 | GUI 模块边界、阅读顺序和总览 |
| `docs/gui/modules/*.md` | 新增 | 全文 | 分模块详设、数据流、约束、错误路径、实现位置和测试 |
| `docs/gui/decisions.md` | 新增 | 全文 | 记录关键实现选择、替代方案和后果 |
| `docs/gui/ci-and-testing.md` | 新增 | 全文 | CI 门禁、平台边界、本地等价命令和故障定位 |
| `docs/gui/code-review.md` | 新增 | 全文 | 功能打点与源码/测试位置的审查追溯矩阵 |

## 依赖分析

- 上游：Application Snapshot/Event DTO、Wails Bridge、嵌入式静态资源、GitHub Actions runner。
- 下游：`gui` 分支 push/PR、发布前 GUI 回归、后续 GUI 维护者和代码审查者。
- 文档只引用现有符号和当前行号，不成为运行时依赖。
- CI 新 job 只调用仓库已有测试和构建命令，不引入 npm 生产依赖。
- Effort controller 只依赖 `SwitchEffort` 端口；业务档位、Prompt 和 MaxLoops 仍由 Application Core 决定。

## 循环依赖检查

- [x] CI 和文档不改变 Go/JS import 图。
- [x] `gui-tests` 与现有 build/race/release jobs 独立，无 job 依赖环。

## 风险预估

- 行号随未来修改漂移：中概率、中影响；同时记录 symbol 名称，代码变更时按审查清单更新。
- GUI test job 与 build job 重复 Go 测试：低概率、低影响；GUI job只跑 `application/gui` 契约，职责明确。
- Linux 无桌面 WebView 依赖：中概率、中影响；production GUI build 保留在 Windows runner，Ubuntu job只跑无 WebView测试。
- 文档状态再次过期：中概率、中影响；将追溯矩阵和 CI 文档列为 GUI 变更审查项。
- 动效导致干扰或高 GPU 占用：低概率、中影响；仅 `max` 激活，使用 transform/opacity，并在 `prefers-reduced-motion` 下关闭。
- 拖动过程中产生重复远程调用：低概率、中影响；`input` 只预览，`change` 才提交，提交期间禁用控件。

## 建议方案

扩展现有 CI 的分支触发，并增加单独的 Ubuntu `gui-tests` job；将 Effort 作为独立、可测试的顶栏控件实现；文档采用 `docs/gui` 长期维护目录，日期型目录只保留本次过程记录。
