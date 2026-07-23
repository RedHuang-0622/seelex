# 代码变更摘要

## 1. 新增、修改文件

| 文件 | 类型 | 说明 | 设计模式/约束 |
|------|------|------|---------------|
| `.github/workflows/ci.yml` | 修改 | `main/gui` 双分支触发，新增独立 GUI tests job | Policy as Code、独立质量门禁 |
| `gui/frontend/dist/effort-control.js` | 新增 | 四档 Effort 映射、预览、提交、失败回滚 | Controller、Ports and Adapters |
| `gui/frontend/dist/effort-control.test.mjs` | 新增 | Effort 纯逻辑和异步失败路径测试 | Fake DOM ports、Contract test |
| `gui/frontend/dist/index.html` | 修改 | Effort 移到 topbar，删除 Runtime modal 内旧 segmented control | 语义 HTML、原生 range |
| `gui/frontend/dist/app.js` | 修改 | 注入 Effort Controller，Runtime Snapshot 驱动 committed 状态 | Composition Root |
| `gui/frontend/dist/styles.css` | 修改 | 进度条、Max 紫色呼吸/流光、响应式和 reduced-motion | 派生视觉状态、渐进增强 |
| `gui/bridge_test.go` | 修改 | 固定 Effort 位于 modal 外，检查新模块已嵌入 | Embedded asset contract |
| `docs/gui/modules/*.md` | 新增 | 6 个 GUI 模块详细设计 | 模块边界、数据流、错误策略、源码追踪 |
| `docs/gui/decisions.md` | 新增 | ADR-GUI-001 至 ADR-GUI-011 | Architecture Decision Record |
| `docs/gui/code-review.md` | 新增 | 31 个功能打点到详设/源码/测试的追溯矩阵 | Traceability Matrix |
| `docs/gui/ci-and-testing.md` | 新增 | CI 触发、job、命令、分层和失败定位 | Test Pyramid、平台隔离 |
| `docs/gui/README.md` | 新增 | GUI 文档总入口和维护规则 | 单一维护入口 |
| `docs/README.md` | 修改 | 增加 GUI 文档入口 | 文档导航 |
| `docs/feature-instrumentation.md` | 修改 | 同步协议、会话、GUI CI 与 Effort 事实 | 功能打点 |

## 2. API 与协议变更

| API/契约 | 变更 | 兼容性 |
|----------|------|--------|
| `gui.Application.SwitchEffort` | 无签名变化；新增常驻 GUI 调用方 | 向后兼容 |
| Snapshot `runtime.effort` | 无 schema 变化；成为滑杆 committed 状态源 | Protocol v1 不变 |
| 前端模块 | 新增 `createEffortControl`、`effortPresentation` | 仅嵌入式内部 API |
| GitHub Actions | `push/pull_request` 新增 `gui`，新增 `workflow_dispatch` 和 `GUI tests` | 扩展门禁，不改变产物 |

## 3. 关键实现决策

1. 原生 range 表达 Effort 的有序强度，而不是继续使用弹窗 buttons。
2. `input` 只做本地视觉预览，`change` 才调用 Bridge，避免拖动导致多次 PromptStack/MaxLoops 修改。
3. Controller 只依赖注入的 `selectEffort/onError`，不直接访问 Wails 全局对象。
4. Runtime Snapshot 是 committed 权威状态；失败恢复旧值并通过统一 toast 报错。
5. Max 动效由 `data-effort=max` 派生，只使用 opacity/transform/background-position；reduced-motion 下关闭动画。
6. GUI 逻辑门禁运行在 Ubuntu，Windows matrix 单独负责 Wails production tags 编译。

## 4. 依赖与循环检查

- 新前端模块只被 `app.js` 单向 import，不反向依赖 composition root。
- GUI 仍只通过 Bridge 使用 Application Core，无新增业务副本。
- 无 npm runtime/test 依赖；Node 22 只作为 CI test runner。
- Go import 图未变；无新增循环依赖和模块级可变连接。

## 5. Commit 计划

```text
feat(gui): add persistent Effort control and CI

- expose a four-level Effort slider with Max purple motion effects
- run GUI frontend and bridge contracts on main and gui branches
- document GUI modules, decisions, tests, and source traceability

Refs: GUI-EFF-001, GUI-EFF-002, GUI-EFF-003, GUI-CI-001, GUI-CI-002
```

实际实现提交：`df0434b feat(gui): add persistent Effort control and CI`。该提交触发的 GitHub Actions run `30004410641` 六个 job 全部通过。
