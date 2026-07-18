# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| `application/*.go` | 新增 | Headless application service、DTO、事件、Chat、命令、补全和审批 | Service、Strategy、Pub/Sub、Broker |
| `application/application_test.go` | 新增 | application 无界面用例与事件测试 | Fake Ports |
| `application_adapters.go` | 新增 | Engine/Runtime/Plugin/Skill/Session 到 application ports 的转换 | Adapter |
| `main.go` | 修改 | 按 EventHub → Broker → Engine → Application → TUI 装配 | Composition Root |
| `tui/*.go` | 重构 | 仅保留 Bubble Tea 输入、局部 UI 状态和 Snapshot 渲染 | UI Adapter |
| `tui/tui_test.go` | 新增 | 输入、取消和 Interaction 映射测试 | Fake Controller |
| `tui/approve`, `tui/commands`, `tui/sugg`, `tui/stream` | 删除 | 删除旧业务子包和包级可变桥接 | — |
| `README.md` | 修改 | 更新架构、装配顺序和项目结构 | — |
| `scripts/sync-claudecode-account.ps1` | 新增 | 将 Claude Code MiniMax 账号转换为本地 OpenAI 账号池 | Adapter |
| `.gitignore` | 修改 | 忽略包含真实 Token 的 `config/*.local.yaml` | — |

## API 变更

| API | 变更 | 兼容性 |
|-----|------|-------|
| `tui.NewModel` | 改为只接收 `AppController` | 内部破坏性变更，`main.go` 已同步 |
| `application.Service` | 新增 Snapshot、Subscribe、Submit、CancelChat、Suggestions、ResolveInteraction | 新增 |
| `application.ApprovalBroker` | 替代 `tui/approve` 的全局同步等待 | 行为保持，依赖方向修正 |
| `application.ToolHookBridge` | 替代 TUI 包级 stream channel | 新增 |

## 设计模式使用

| 模式 | 文件 | 效果 |
|------|------|------|
| Ports and Adapters | `application/ports.go`, `application_adapters.go` | application 不依赖具体产品包 |
| Strategy | `application/command.go` | 命令实例化注册，无全局 registry |
| Publish/Subscribe | `application/event.go` | 多前端可独立订阅有序事件 |
| Broker | `application/approval.go` | 审批请求按 ID 等待和决议 |
| Snapshot | `application/state.go` | 前端可从稳定 DTO 完整重建画面 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|------|--------|--------|
| `ChatEngine` | `enginePort` | `application.Service` |
| `RuntimePort` | `runtimePort` | application runtime/read model |
| `PluginPort` | `pluginPort` | application plugin use case |
| `SkillPort` | `skillPort` | application input/completion |
| `SessionPort` | `sessionPort` | application session commands |
| `AppController` | `application.Service` | `tui.Model` |

## 循环依赖检查

- [x] `application` 不依赖 `tui`
- [x] `tui` 不依赖 Engine、Plugin、Skill、Session 或 `seelebridge`
- [x] 无包级事件 channel、命令 registry 或 pending interaction

## Commit 建议

```text
refactor(tui): separate headless application core

- move chat, commands, completion and interactions into application
- make TUI consume snapshots and ordered application events
- replace global stream and approval bridges with instance services
- update composition root, tests and architecture documentation

Refs: TUI application core separation
```
