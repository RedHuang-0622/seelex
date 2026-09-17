# Terminal UI

## 生态位

`tui` 是 Bubble Tea 终端前端。它通过 `AppController` 消费 Application Snapshot/Event 并提交用户动作，不直接调用 Seele Engine、Plugin Manager 或 Session Store。

主要调用方：组合根 `main.go`（`-frontend tui`，默认入口）。

## 架构图

```mermaid
flowchart TB
    subgraph TEA["Bubble Tea 主循环"]
        INIT["Init：读取 Snapshot + SubscribeSession(\"\")"]
        UPD["Update：应用 event / 用户按键"]
        VIEW["View：纯投影，不发起 IO"]
    end

    CTRL["AppController<br/>mutation 全部回调"]
    SVC["application.Service"]
    EV["Application event 流（含会话归属判定）"]

    subgraph PANELS["面板"]
        CONV["conversation / input / status"]
        PLANP["plan：按 Effort 与终端宽度渲染"]
        GT["goalteam：Alt+G 目标治理 / Alt+T AgentTeam"]
        SUG["suggest：/ # $ @ 前缀建议"]
        SPL["splash：启动画面"]
    end

    INIT --> EV
    EV --> UPD
    UPD --> VIEW
    UPD --> CTRL
    CTRL --> SVC
    VIEW --> CONV
    VIEW --> PLANP
    VIEW --> GT
    VIEW --> SUG
    SPL --> INIT
```

本地状态只含光标、viewport、输入框、suggestion 与布局；业务事实一律来自
Snapshot/Event，会话归属由 application 投递端判定，TUI 不自行判断。

## 文件结构

| 文件 | 职责 |
|---|---|
| `tui.go` | `AppController`、Model、Init/Update 主循环。 |
| `view.go` | 总体布局、conversation/input/status 渲染。 |
| `stream.go` | Application event 到 Tea message 的桥接。 |
| `dialog.go` | Interaction/account/session 等选择面板。 |
| `plan.go` | 按 Effort 和终端宽度渲染 Plan 生命周期。 |
| `goalteam.go` | 目标治理（Alt+G）与 AgentTeam（Alt+T）的只读面板：投影 + 面板键 + 团队读面的异步取值。 |
| `suggest_view.go` | `/`、`#`、`$`、`@` suggestions。 |
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
- 输入前缀（sigil）与后端同一张表（`application.HasSigilPrefix` / `application.SigilOf`）：
  `/` 命令与工具、`#` 切换 Plugin、`$` 召回 Skill、`@` 手动召唤团队。前缀后无空白时
  进入 `suggMode` 并显示 suggestions 面板（数据源 = `Suggestions`，Tab 接受）。
  TUI 不自持字符表：**Suggestions 在 `View()` 渲染路径上**，所以后端那一侧必须保持
  零 I/O（`@` 因此只列内置团队形态，不读团队库）。
- 只读面板：`alt+g` 目标治理（数据源 = `Snapshot.Runtime.GoalGovernance`，与 GUI
  「目标」面板同源投影，无活跃 goal 时给上线入口提示）、`alt+t` 团队（成员/发言
  顺序/定时 agent/调度运行态，取值走一次 `tea.Cmd` 调
  `AppController` 的可选团队读面 `AgentTeamView(mainSessionID)`——`*application.Service`
  已实现，与 GUI 经 Bridge 读的是同一个方法；读面缺失或后端报错时面板给出明确
  文案）。面板只读：不提交输入、不改后端状态、不新增只对终端生效的业务事实；
  有待批选择时不打开（键义不打架）。`esc` 关闭，`alt+t` 在团队面板上再按 = 刷新。
  `AppController` 主接口因此**不需要**新增方法：读面是可选接口，装配根不改也能跑。
- 分页：`pgup`（viewport 到顶且还有更早历史时取更早一页）、`home` 同上；
  `end` 在窗口已锚定在更早历史（回看中）时回到最新一页
  （`LoadLatestHistory`）——后端分页在回看期间不再把窗口拽回尾部，这是
  终端端的显式出口。`LoadMoreHistory(limit<=0)` 表示「一整窗」。
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
