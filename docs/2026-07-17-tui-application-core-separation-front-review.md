# TUI 前端与应用逻辑拆分前置审查

## 需求摘要

将当前 `tui` 包中的 Bubble Tea 前端、组件渲染与 Seelex 应用逻辑分离，使 TUI 只负责输入适配、组件局部状态和渲染；聊天、命令、Skill、Plugin、Session、审批等逻辑沉淀为无界面的 Go application core，供未来 Electron 前端通过 IPC 复用。

本文将用户所说的 “election” 按 Electron 理解。Electron 无法直接 import Go 包，因此复用链路必须是：

```text
Bubble Tea TUI ── Go 接口直调 ──┐
                               ├── Seelex application core ── Seele framework
Electron UI ── IPC/RPC adapter ┘
```

## 可行性评估

结论：**可行性高，收益明确，工作强度中高。**

当前代码已经有部分有利基础：

- Seele 框架能力已集中到 `seelebridge`，application core 不需要再次理解 Seele 深层 API。
- Plugin、Skill、Session 已有独立包，适合通过窄接口注入。
- TUI 已采用 Bubble Tea 的事件驱动形式，输入事件和应用事件可以进一步分层。
- `RuntimeView`、`PluginController` 等接口已经出现，说明依赖倒置方向正确。

真正的工作量来自当前 TUI 同时承担了以下职责：

1. Bubble Tea 键盘、鼠标、窗口尺寸和 viewport 管理。
2. Lipgloss 组件渲染。
3. ChatStream 生命周期和 goroutine/channel 编排。
4. Engine 工具 hook 到 UI 消息的转换。
5. Slash command 解析和业务命令执行。
6. Skill 查找、参数拼接和提交。
7. Plugin 切换后的工具与 Skill 刷新。
8. Session 恢复、账号选择和选择器业务处理。
9. 同步审批请求的等待、展示和决议。

仅把 `View()` 或 Lipgloss 文件移动出去，Electron 仍然无法复用第 3—9 项，因此不能实现目标。

## 规模与工作量

当前 `tui/` 约 2,600 行 Go 代码，其中约 1,000—1,400 行属于可抽取的应用逻辑或应用状态编排。

| 目标范围 | 预计工作量 | 说明 |
|---|---:|---|
| 最小拆分 | 4—6 人日 | 抽出 Chat、命令、基本事件；审批和选择器仍有 TUI 特例，不建议作为长期终态 |
| 推荐的生产级拆分 | 8—12 人日 | application state、事件流、命令、Skill、Plugin、Session、审批全部无界面化，并完成 TUI 回归测试 |
| 增加 Electron sidecar IPC | 额外 2—4 人日 | JSON-RPC/stdio 协议、事件通知、协议测试；不包含 Electron 页面开发 |
| 完整 Electron 客户端 | 额外 1—3 周 | 取决于页面复杂度、终端工具展示、审批交互和安装打包要求 |

## 当前关键耦合点

| 文件 | 位置 | 当前职责混合 | 拆分方向 |
|---|---:|---|---|
| `tui/tui.go` | `Model`、`Update`、`handleEnter` | Bubble Tea 状态、业务依赖、命令/Skill 解析、聊天启动 | `Model` 仅保留 UI 状态；提交输入交给 application service |
| `tui/stream.go` | 全文件 | ChatStream、Engine hook、全局 channel、Conversation 更新 | Chat 和 hook 移到 application；TUI 只消费 application event/snapshot |
| `tui/state.go` | `Cell`、`Conversation`、`AppState` | 领域消息与 Lipgloss Render 混合 | 领域/只读状态进入 application；渲染模型留在 TUI |
| `tui/command.go` | 全文件 | 命令装配、全局建议缓存、UI 消息转换 | 命令注册与执行进入 application；TUI 读取 command catalog |
| `tui/commands/*` | 全包 | 直接依赖 Engine、Session、Plugin，并使用全局 registry | 改为实例化 command registry，由 application 注入依赖 |
| `tui/dialog.go` | selector 处理 | Session/账号业务操作与键盘选择混合 | application 提供选项/执行选择；TUI 只维护光标和弹窗 |
| `tui/approve/*` | `Ask`、全局请求、Manager/View | 同步业务等待与 Bubble Tea 展示混合 | `ApprovalBroker` 进入 application；TUI 仅渲染并 resolve request |
| `tui/sugg/engine.go` | 全文件 | 补全算法较通用，但依赖 `seelebridge.Tool` 和 `context.Background()` | 移到 application completion service，返回稳定 DTO |
| `tui/view.go` | `renderStatusBar` | 渲染时直接读取 Runtime 和 Engine | 改为只读取 application snapshot |
| `main.go` | `registerAskApprove`、`initEngine`、`initTUI` | 产品工具直接调用 TUI 审批，Engine hook 由 TUI 创建 | 先装配 event hub/broker，再创建 Engine 和 application，最后创建 TUI |

## 全局状态问题

当前至少有以下包级可变状态：

- `tui/stream.go`: `streamEventCh`
- `tui/tui.go`: `skillsNeedRefresh`
- `tui/dialog.go`: `pendingPrompt`
- `tui/commands/commands.go`: `registry`
- `tui/command.go`: `refreshCmdSuggestions`

这些状态导致：

- 不能安全创建多个 UI/application 实例。
- 测试间容易相互污染。
- TUI 与 Electron 同时接入时无法区分订阅者和请求来源。
- Engine hook 与 UI 生命周期绑定，UI 重建后可能仍写入旧 channel。

拆分时应全部改为构造注入的实例状态，不保留包级事件 channel 或 registry。

## 影响文件清单

### 新增

| 文件路径 | 类型 | 作用 |
|---|---|---|
| `application/app.go` | 新增 | application service、生命周期、公开用例入口 |
| `application/ports.go` | 新增 | Engine、Runtime、Plugin、Skill、Session 等消费方接口 |
| `application/state.go` | 新增 | 与 UI 技术无关、可 JSON 序列化的 Snapshot/read model |
| `application/event.go` | 新增 | 有序事件、订阅与重新同步机制 |
| `application/chat.go` | 新增 | ChatStream、取消、消息增量、工具事件处理 |
| `application/input.go` | 新增 | 普通消息、slash command、Skill 输入解析 |
| `application/command.go` | 新增 | 实例化命令注册表与类型化结果 |
| `application/approval.go` | 新增 | 无界面的 ApprovalBroker，请求等待和 resolve |
| `application/completion.go` | 新增 | command/tool/skill 补全目录与过滤 |
| `transport/jsonrpc/*` | 后续新增 | Electron sidecar 的 JSON-RPC/stdio adapter |

### 修改或迁移

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `main.go` | 修改 | 装配顺序、`registerAskApprove`、`initEngine`、`initTUI` | application/event hub/approval broker 先于 UI 创建 |
| `tui/tui.go` | 重构 | `Model`、`NewModel`、`Update`、`handleEnter` | 删除业务依赖，只调用 `AppController` |
| `tui/stream.go` | 大幅缩减 | 全文件 | 只保留 application event 到 `tea.Msg` 的桥接 |
| `tui/state.go` | 拆分 | `Cell`、`Conversation`、`AppState` | 领域状态移入 application，Render 留在 TUI |
| `tui/view.go` | 修改 | `View`、status/conversation render | 只读取 Snapshot 和 TUI 局部状态 |
| `tui/dialog.go` | 修改 | selector/approval handler | 只处理前端选择，业务动作交给 application |
| `tui/command.go` | 删除或改为 adapter | 全文件 | 命令目录由 application 提供 |
| `tui/commands/*` | 迁移/删除 | 全包 | 迁入 application 的实例化 command registry |
| `tui/sugg/*` | 迁移或 renderer 化 | 全包 | 补全逻辑进入 application，TUI 只显示结果 |
| `tui/approve/*` | 拆分 | `Ask` 与 Manager/View | Broker 移入 application；View/键盘组件保留 |
| `session/manager.go` | 小改 | 接口补全 | application 通过窄接口使用，不暴露具体 Manager |
| `seelebridge/*` | 原则上不改 | — | Seele 仍是纯框架依赖；只有缺少稳定 DTO 时才扩展薄封装 |

## 依赖分析

### 当前

```text
main
 ├── engine
 ├── plugin / skill / session / seelebridge
 └── tui
      ├── engine
      ├── plugin / skill / session / seelebridge
      └── Bubble Tea / Lipgloss
```

### 目标

```text
main
 ├── engine（允许直用）
 ├── seelebridge / plugin / skill / session
 ├── application
 └── tui

application ──> 自己定义的 ports ──> main 注入的实现
tui ──> 自己定义的 AppController ──> application.Service
transport/jsonrpc ──> 自己定义的 AppController ──> application.Service

application 不 import tui
tui 不 import engine/plugin/skill/session/seelebridge
transport 不 import tui
```

## 风险预估

| 风险 | 概率 | 严重度 | 对策 |
|---|:---:|:---:|---|
| 流式事件乱序或丢失 | 中 | 高 | Event 带单调 `Seq` 和 `Revision`；客户端发现跳号后重新拉 Snapshot |
| 工具审批死锁 | 中 | 高 | Broker 使用 request ID、context cancel、缓冲响应 channel；禁止 UI goroutine 内阻塞 |
| Engine hook 生命周期与 App 生命周期错位 | 中 | 高 | EventHub 先创建，hook 绑定实例 sink；Shutdown 时取消订阅和活动 Chat |
| 迁移期间 TUI 行为回归 | 高 | 中 | 先建立 characterization tests，再逐条迁移用例 |
| 为 Electron 过度设计通用协议 | 中 | 中 | 第一阶段只定义 Go application API；第二阶段从已稳定 API 生成 JSON-RPC adapter |
| Snapshot 随历史增长过大 | 中 | 中 | 事件增量为主，Snapshot 用于首次加载与跳号恢复；后续支持分页消息 |
| 当前 Session Resume 受 Seele API 限制 | 已存在 | 中 | 拆分不伪装修复；application 返回明确 capability/error |

## 建议方案

推荐采用“**Headless Application Service + UI Adapter**”架构：

- application 是唯一业务状态和副作用编排者。
- TUI 与 Electron 都是 application 的客户端。
- application 发布可序列化事件与只读 Snapshot。
- TUI 只维护尺寸、滚动、焦点、光标、选中项、颜色等前端局部状态。
- Electron 通过 sidecar JSON-RPC/stdio 调用同一 application service。
- Seele 不做任何修改；application 继续通过 `engine` 和 `seelebridge` 使用框架能力。

该方案改动面较大，但能真正实现多前端复用，并消除当前全局 channel/registry 带来的并发与测试问题。

