# Seelex GUI 设计文档

本目录描述当前 Wails GUI 的稳定契约、模块边界、实现决策、质量门禁和代码审查追溯。它是长期维护文档；日期型 `docs/2026-07-23-*` 只记录当次变更过程。

## 系统边界

```text
Seele Engine / Session / Plugin / Skill
                 │
                 ▼
        application.Service
      Snapshot + ordered Event
                 │
                 ▼
          gui.Bridge (Wails)
        bound methods + events
                 │
                 ▼
        client-state + protocol
     reducer + Snapshot resynchronise
                 │
                 ▼
  app shell ─ chat view ─ conversation view
                 │
                 ▼
        embedded HTML/CSS/JavaScript
```

## 核心原则

1. Application Core 是唯一业务状态源；GUI 不直接操作 Engine、Session、Plugin 或 Skill 实现。
2. Snapshot 是权威恢复点，Event 是正常增量路径。
3. 前端本地只保存交互状态，例如滚动、弹窗、Effort 拖动预览、建议选中项和展开状态。
4. 会话节点必须使用稳定实体 ID，不使用数组位置标识长期节点。
5. 所有后端能力经 `gui.Application` 窄接口和 Wails Bridge 暴露。
6. Markdown 和工具输出按不可信内容处理；默认限制工具输出进入 DOM 的规模。

## 模块索引

| 模块 | 详细设计 | 主要实现 |
|------|---------|---------|
| Application 协议与会话 | [application-protocol.md](modules/application-protocol.md) | `application/state.go`、`event.go`、`app.go`、`chat.go` |
| Desktop Bridge 与生命周期 | [desktop-bridge.md](modules/desktop-bridge.md) | `gui/bridge.go`、`run_wails.go`、`assets.go` |
| 客户端状态与事件归并 | [client-state.md](modules/client-state.md) | `protocol.js`、`client-state.js` |
| 会话渲染与内容安全 | [conversation-rendering.md](modules/conversation-rendering.md) | `conversation-view.js`、`components.js`、`chat-view.js`、`markdown.js` |
| Shell、命令和交互 | [shell-and-interactions.md](modules/shell-and-interactions.md) | `index.html`、`app.js`、`styles.css` |
| Effort 常驻控件 | [effort-control.md](modules/effort-control.md) | `effort-control.js`、`index.html`、`styles.css` |
| JSON DSL 对话卡片（规划） | [dsl-card-runtime.md](modules/dsl-card-runtime.md) | 计划 `presentation/`、`dsl-*.js`、ConversationItem v2 |
| Agent E2E 交互（规划） | [agent-e2e-interaction.md](modules/agent-e2e-interaction.md) | 计划 `e2e/scenario/`、`gui/e2e/`、Wails smoke |
| 右栏 Workspace 沙盒（规划） | [workspace-sandbox.md](modules/workspace-sandbox.md) | 计划 `workspace/`、`workspace-*.js`、Bridge queries |
| 实现决策 | [decisions.md](decisions.md) | ADR-GUI-001 至 ADR-GUI-014（012—014 为规划） |
| CI 与测试 | [ci-and-testing.md](ci-and-testing.md) | `.github/workflows/ci.yml`、Go/Node tests |
| 代码审查与功能追溯 | [code-review.md](code-review.md) | 功能点、详设、源码、测试和结论 |

## 运行时主流程

```text
启动
  ├─ main 组装 application.Service
  ├─ gui.Run 创建 Bridge 与 Wails
  ├─ Bridge 订阅 EventHub，发送 seelex:ready
  └─ app.js 拉取初始 Snapshot 并注册事件监听

切换 Effort
  ├─ range input 只更新本地预览
  ├─ range change → Bridge.SwitchEffort
  ├─ Core 更新 PromptStack/MaxLoops 并发布 runtime.changed
  └─ Runtime Snapshot 覆盖客户端 committed 状态

提交输入
  ├─ app.js → Bridge.Submit
  ├─ Service 启动 Chat 或加入输入队列
  ├─ EventHub 发布 message/tool/runtime/interaction 事件
  ├─ client-state 检查 protocol/seq/revision
  ├─ protocol reducer 归并到客户端 Snapshot
  └─ keyed view 只替换变化节点

异常恢复
  ├─ seq 缺口 / 未知事件 / payload 无法归并
  ├─ client-state 合并刷新请求
  └─ Bridge.Snapshot 恢复权威状态
```

## 兼容与发布边界

- 当前协议版本：`1`。
- Agent Workbench 规划使用 protocol v2；在实现合并前当前运行时仍严格保持 v1。
- GUI 生产构建标签：`gui,desktop,production`。
- 默认无 GUI tags 的构建使用 stub，不引入桌面 WebView。
- 前端是嵌入式静态资源，不需要运行时 Node/npm。
- Windows 是当前手工 alpha 验收平台；Linux/macOS 仍需各自系统 WebView 依赖和真实运行验证。

## 文档维护规则

- 修改 Snapshot/Event schema 时同步更新协议详设、reducer 测试和追溯矩阵。
- 修改 Bridge 公开方法时同步更新 `gui.Application`、Bridge tests 和模块文档。
- 修改 GUI 功能时为 [code-review.md](code-review.md) 增加或更新功能点。
- 源码行号发生变化时保留 symbol 名并刷新当前行号。
- 只有同时具备实现、失败路径和自动化证据的功能点才能标记为“通过”。
