# Terminal UI

## 模块定位

`tui` 是 Bubble Tea 终端前端。它通过 `AppController` 消费 Application Snapshot/Event 并提交用户动作，不直接调用 Seele Engine、Plugin Manager 或 Session Store。

## 文件结构

| 文件 | 职责 |
|---|---|
| `tui.go` | `AppController`、Model、Init/Update 主循环。 |
| `view.go` | 总体布局、conversation/input/status 渲染。 |
| `stream.go` | Application event 到 Tea message 的桥接。 |
| `dialog.go` | Interaction/account/session 等选择面板。 |
| `plan.go` | 按 Effort 和终端宽度渲染 Plan 生命周期。 |
| `suggest_view.go` | `/`、`#`、`@` suggestions。 |
| `state.go` / `types.go` | UI cell、message 和内部状态。 |
| `styles.go` | Lipgloss 主题。 |
| [`splash/`](splash/README.md) | 启动画面。 |

## 状态流

Model 初始化读取 Snapshot 并订阅 Application events。Update 根据 event 更新本地投影；提交、取消、选择、翻页等 mutation 全部回调 AppController。View 是纯投影，不发起 IO。

TUI local state 只包含光标、viewport、输入框、suggestion 和布局信息；conversation/runtime 等业务事实来自 Snapshot/Event。

## 交互和关闭

- Enter 提交原始输入。
- Ctrl+C 在 running chat 时取消对应 request，否则按产品语义处理复制/退出。
- Interaction 键选择通过 `ResolveInteraction` 返回 Application。
- 退出由 composition root 协调 graceful shutdown。

## Review 指南

- Update 不应阻塞；IO 必须包装为 `tea.Cmd`。
- Event 丢失时是否能从 Snapshot 恢复，而不是永久错位。
- 宽度计算使用 display width，中文/emoji 不应破坏布局。
- Plan status icon、interaction option 和 session ID 是否与 Application model 一致。
- 不要在 TUI 新增只对终端生效的业务状态。

## 测试

```text
go test ./tui/... -count=1
```
