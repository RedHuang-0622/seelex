# 会话薄封装重构设计（Thin-Wrapper Session Design）

> 日期: 2026-09-01
> 状态: 定稿（实施 9.1 的权威依据）
> 前置: [session-domain-design.md](./session-domain-design.md)（会话域定位）、
>       Seele v0.1.1 `session.Session`（引擎+loop 持有者）
> 取代: 本文档取代 session-domain-design 中「自造生命周期状态机/并行 ChatRuntime」
>       的平行世界部分；会话性归 Seele，seelex 只做薄封装。

## 0. 一句话结论

**会话单元 = Seele 引擎/loop 的薄包装 + seelex 视图/队列/存储绑定 + 观察 hooks**；
存储从「项目粒度」改为「**会话粒度**」（项目 = 会话集合，主会话与子代理会话同构）；
沙箱是工具调用的中间件生态位，FC 许可即沙箱权限授予的一部分。

## 1. 依赖转变（所有依赖 session 的地方）

### 1.1 目标依赖方向

```text
gui/tui ──→ application/core（facade）
application/core ──→ session.Ports（接口）
session/（薄封装）──→ session.EnginePort / StorePort / workspace.Binding（接口）
seelebridge 实现 EnginePort（bundle/loop/trace）→ Seele
sessionstore 实现 StorePort（会话粒度持久化）
```

### 1.2 各依赖方转变清单

| 依赖方 | 现状（冗杂） | 转变后 |
|---|---|---|
| `application/core/chat.go` runChat | 自编 ReAct 编排/预算/transcript | 委托 Seele loop；seelex 经 LoopHooks 投影 |
| `task_context` | 平行维护 per-session 执行态 | 收敛为 session 单元的上下文栈（plan/task/skill/compact） |
| `work_table.go` | 全局注册表 + 分区同步 | 会话粒度的 task 视图（session.Ports 读写） |
| `session_history/lifecycle/scope` | 自造冷/热/状态机 | 包 seelebridge：HasSession / NewMainSessionWithID / ChatStreamFor / Unload |
| `session_fork.go` | 项目级深拷贝 + 跨层落盘 | 会话粒度：拷贝 SessionRecord 前缀 → 新会话单元 |
| GUI/TUI | 读 Snapshot + 事件流 | 只消费 SessionUnit.View + 会话事件流（不变，契约化） |
| 持久化编排（session_runtime） | workspace 粒度三读一写 | session 粒度三读一写（见 §2） |

## 2. 存储模型：会话粒度

### 2.1 原则

- **原子单位 = 会话**：一个会话（main 或 subagent）的 record/history/transcript/
  tool-results/context 是同一持久化单元，按键 `sessionID` 读写。
- **项目 = 会话集合**：workspace/project 只承担分组与索引（project → sessions 目录），
  不再是存储根。
- **主/子代理会话同构**：subagent 也是完整 SessionUnit（独立引擎/loop/视图/历史），
  通过 `parent_session_id` 关联到主会话；存储上无特殊通道。

### 2.2 键与接口

```text
sessionstore 会话粒度键：
  session:<sessionID>            → SessionRecord（身份/标题/状态/checkpoint）
  session:<sessionID>:history     → DurableHistory（框架 working history）
  session:<sessionID>:transcript  → 事件日志
  session:<sessionID>:toolresults → 工具结果归档
  session:<sessionID>:context     → 上下文四栈

索引（项目=集合）：
  project:<projectID>:sessions    → sessionID 列表（目录/恢复用）
  session:<sessionID>:binding     → workspaceID / parent_session_id / kind
```

接口（session.StorePort）：

```go
type StorePort interface {
    SaveSession(record SessionRecord) error            // 会话粒度原子写
    LoadSession(sessionID string) (SessionRecord, bool, error)
    History(sessionID string) DurableHistory            // 框架工作历史
    Transcript(sessionID string) ([]TranscriptEvent, error)
    ToolResults(sessionID string) ([]ToolResultRef, error)
    Context(sessionID string) (ContextStack, error)
    SessionsOf(projectID string) ([]SessionInfo, error) // 项目 = 集合
    Bind(sessionID, binding SessionBinding) error
}
```

### 2.3 迁移

- `Router` 现有 `(workspaceID, sessionID)` 复合键保留为物理布局，
  暴露层改为会话粒度 API；`LoadHistoryByWorkspace` 等旧口逐步废弃。
- `session_runtime.Coordinator` 的三读一写改为按 sessionID 编排。

## 3. SessionUnit 内部分层：main / subagent

### 3.1 单元结构

```go
type SessionKind string // main | subagent

type SessionUnit struct {
    ID       string
    Kind     SessionKind
    ParentID string          // subagent 归属的主会话（main 为空）
    Title    string
    Status   string          // draft/idle/running/queued（展示态）

    Engine   EngineHandle    // seelebridge bundle（framework Session：引擎+loop）
    View     *View           // 可见投影（前端数据面）
    Queue    *InputQueue     // seelex 输入队列
    Context  *ContextStack   // plan/task/skill/compact
    Binding  SessionBinding  // workspaceID / parent / kind
}
```

### 3.2 生命周期（包 seelebridge，不重造状态机）

- 热：`EnginePort.HasSession(sid)` 为真 → bundle 存活，attach 视图即可。
- 冷：`EnginePort.NewMainSessionWithID(sid, hooks)` / `NewSubagentSessionWithID(...)`
  重建 bundle（framework Session + DurableHistory），seed 存储记录。
- 运行：`EnginePort.ChatStreamFor(sid, ctx, input, onChunk)` 跑 Seele loop；
  seelex 经 LoopHooks 投影增量，不持有循环。
- 结束：persist（session 粒度）→ `EnginePort.UnloadSession(sid)`。

### 3.3 subagent 会话

- 每个 plan 节点 = 一个 subagent SessionUnit（独立 loop/视图/历史）；
- 现有 seelebridge `subagentSessions/bundle` 直接复用，薄封装建模；
- fork 子代理 = 会话粒度深拷贝（见 §5）。

## 4. 沙箱与 FC 生态位

### 4.1 生态位划分

```text
模型 ──工具调用──▶ FC 许可（权限判定：manual rules / full_access）
                        │
                        ▼
                沙箱中间件（工具调用中间件：PathGate / worktree / 限额 / 禁用名单）
                        │
                        ▼
                      执行（bash / 文件 / 网络…）
```

- **FC（function calling）**：模型→工具的边界许可；**FC 本身即沙箱权限授予的
  一部分**（工具 schema/审批 → 沙箱 allowance）。
- **沙箱**：工具调用的**中间件生态位**——在 FC 许可之后、执行之前施加
  路径门禁/工作树/限额/禁用规则；不改变工具注册与执行本身。
- 现状复用：`toolspermission`（FC 许可）、`PathGate`/`worktree`/`limits`
  （沙箱）保持不变；薄封装只把两者串成清晰管线。

## 5. fork 简化

- fork = **会话粒度深拷贝**：以 `session:<parent>` 的 record 前缀（对话/事件/
  tool-results/checkpoint）+ 上下文栈为拷贝面，落到新 `session:<child>`；
  引擎工作历史从 record 重派生，执行态全新。
- 项目级绑定与 parent 指针随拷贝；不再需要跨层 fork 编排。

## 6. core 简化

- `application/core` 收窄为 facade：前端接口（Snapshot/事件/提交）+ 存储编排
  （经 session.StorePort）+ 产品逻辑（plugins/skills/effort/worktable 同步）。
- runChat 循环、预算、transcript 平行世界删除；观察改挂 Seele LoopHooks。
- 会话生命周期、切换、trace 全部经 session/ 端口。

## 7. 迁移阶段

1. **9.1**：session/ 端口契约（EnginePort/StorePort）+ SessionUnit 骨架 +
   依赖方切换清单落地（本文档）。
2. **9.2**：runChat 委托 Seele loop（hooks 投影）；删除平行循环。
3. **9.3**：存储切会话粒度（StorePort 实现 + 索引迁移）。
4. **9.4**：main/subagent 同构化 + fork 会话粒度重写。
5. **9.5**：清理旧状态机/平行 ChatRuntime/workspace 粒度口；全量验证。

## 8. MBD 建模文档

- [mbd-overview.md](./mbd-overview.md)：阶段划分 + 活动图 + 模型索引 + 验证映射
- [mbd-us.md](./mbd-us.md)：用例建模（UC1–UC11）
- [mbd-models.md](./mbd-models.md)：数学模型
  - M1 trace 注入与结果返回
  - M2 session 组成部分（main/subagent）
  - M3 组成部分 → 前端投影（对话 Π_conv + 轨迹 Π_traj）
  - M4 视图切换
  - M5 多会话并行
- [mbd-activities.md](./mbd-activities.md)：活动图（AD-0–AD-9，UML 泳道）
- [test-cases.md](./test-cases.md)：测试用例（模型不变量 → 用例验收 → 阶段门禁）
- [module-map.md](./module-map.md)：模块改造前置文件与最终验收文件清单
- [task-dispatch.md](./task-dispatch.md)：任务派发提示词 + 打点表

实施以模型不变量为门禁（§4 验证映射）。
