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

Model 初始化读取 Snapshot 并以 `SubscribeSession("")`（跟随当前视图会话）订阅 Application events，与 GUI 共用 application 投递端的会话归属判定，TUI 自身不判定事件属于哪个会话。Update 根据 event 更新本地投影；提交、取消、选择、翻页等 mutation 全部回调 AppController。View 是纯投影，不发起 IO。

TUI local state 只包含光标、viewport、输入框、suggestion 和布局信息；conversation/runtime 等业务事实来自 Snapshot/Event。

待批计数承载面（2026-09-03 收口）：TUI 无会话侧栏，采用最小可用面——状态行
右侧显示跨会话待批计数（`待批:N`），正文区顶部在有待批会话时显示提示行
（计数 + 短 ID）。数据源是目录联合镜像 `Snapshot.Sessions` 的
`awaiting_approval` 行；单格 `Interaction` 仍只表达当前视图会话审批（切换
到目标会话后经既有 dialog + `ResolveInteraction` 处理）。不做平行会话管理。

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
