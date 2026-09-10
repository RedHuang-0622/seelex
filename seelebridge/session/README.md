# seelebridge/session — 子代理会话与上下文 actor 域

## 模块定位

承载子代理会话注册表与父证据/merge-back 两个 actor。主要调用方：根包 `runtime.go` 装配、`agent_node.go` 会话门面、`actor.go` 的 merge-back facade。

## 与其它域的关系

```text
node（producer）──► session（子代理会话/树/merge-back）──► fork/plan（消费者）
                        │
                        └──► worktree（节点会话的独立工作区）
```

session 承载子代理会话注册、fork 子代理树与 merge-back 队列；node 是
producer，fork/plan 是消费者；节点工具结果归档（result_ref）也在本域托管。

## 职责与非职责

职责：

- `SubagentSessions`：运行中子会话历史读取（走引擎发布的历史检查点，
  **绝不阻塞 actor**）、结束快照保留、节点工具结果归档（`ToolResultArchiverFor`/`ToolResult`）；
- `SubagentSessions`（第一视角数据面）：node 分阶段上下文日志（`RecordStage`/`StageLogs`，
  同一 node 会话自动补全 SessionID——多阶段同一 subagent 的认证面）与预定义语义
  结果返回（`RecordResult`/`Result`/`DrainResults`，结果经语义结果队列按消息队列
  路径返回给 mainagent / 下游 node）；`StageEvents` 提供阶段日志**实时推送通道**
  （即时输出面：每条阶段记录后立即投递，非轮询/缓存；通道有界，满时丢弃并计数，
  best-effort 不阻塞执行路径）；
- `ToolEventState`：子代理工具事件分发器支持多观察者（`SetCallback` 供 main 路径，
  `Subscribe` 供 Runtime 实时流等并行订阅），node 第一视角统一实时流的工具面；
- `SubagentContextActor`：父证据读-合并-写回（`MergeBackIntoParent`）、merge-back 有界 mailbox（`Enqueue`/`Drain`）、父证据无锁读取（`NodeParentEvidence`）。

非职责：

- 不执行子代理（执行在 `node/` 域）；
- 不决定合并策略（`seelexctx/merger` 负责）；
- 不接触主会话锁（mailbox 只是消息进出）。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `subagent_sessions.go` | 会话注册表 actor（运行期记录落盘 + 结束结论回传主会话 + 记录删除） |
| `subagent_stage_test.go` | 第一视角阶段日志与语义结果队列单元测试 |
| `subagent_tree.go` | fork 子代理树注册表与只读投影（运行中上下文走无锁计数，不读子会话） |
| `subagent_tree_lock_test.go` | 投影锁序与长流阻塞回归测试（2026-09-07 死锁 + 2026-09-10 长流热点） |
| `subagent_sessions_stream_test.go` | 注册表观测面不被运行中节点阻塞的回归测试（2026-09-10 长流热点） |
| `subagent_context.go` | 父证据合并 + merge-back mailbox actor |
| `context_clone.go` | `ContextSnapshot` 深拷贝辅助 |
| `subagent_sessions_test.go` | 会话注册表单元测试 |

## 核心实现

两个组件都是"channel 命令 + 单消费者 goroutine"的 actor：外部经有界命令通道投递操作（带 10s 超时），actor 串行处理，天然免锁。

- `SubagentContextActor`：`handleMerge` 串行执行"读当前父证据 → `merger.MergeBack`（copy-on-write 累积）→ 写回 `atomic.Pointer`"；`handleEnqueue` 在队列达 soft cap 时仅计数 `Overflow`，内容全部保留；`Drain` 一次性全量回收。
- `SubagentSessions`：注册/注销导出结束快照（`seelexctx.ExportSnapshot`，trace 可为 nil 降级）；工具结果走节点专属 `InMemoryToolResultArchiver`，ref 前缀由 `internal/model.NodeResultRefPrefix` 统一。
- 第一视角阶段日志（`model.NodeStageLog`）：`AgentNode`/telemetry 钩子写入，actor 按
  nodeID 追加并补全 SessionID 与 turn 编号；语义结果（`model.NodeSemanticResult`）
  登记时自动附带该节点全部阶段日志，并投入语义结果队列（`DrainResults` 一次性取空）；
  每条阶段日志写入后同时推入 `StageEvents` 通道（即时输出）。

## 数据流或生命周期

子代理完成 → `node/` 域 `mergeBack` 导出快照 → `SubagentContextActor.MergeBackIntoParent`（合并写回 parentEvidence）→ `Format()` 文本 `Enqueue` → 主会话 ChatStream 前后 `injectPendingSubagentContexts` 排空 mailbox 注入。

## 依赖方向

`session` → `seelexctx`（merger/provider/snapshot）、`internal/model`、框架 `session` 类型。**禁止反向依赖 seelebridge 根包**。

## 并发、存储、安全或错误语义

- actor 串行化父证据合并，防并发覆盖（B 修复）；
- mailbox soft cap 只作诊断计数、内容不丢（A 修复）；
- 命令投递带超时，actor 关闭后快速失败；
- **运行中会话读取一律非阻塞**（2026-09-10 长流热点）：`SubagentSessions` 是
  单 goroutine actor，`Conversation`/`ContextSnapshot`/落账若调用
  `Session.History()`，会停在子代理整段流式上（实测 28s），期间**所有**节点的
  详情、阶段日志与落账一起排队，mailbox（256）满后 `RecordStage` 开始丢事件。
  因此运行中读取统一走 `refreshLiveHistoryLocked`：用引擎的
  `HistoryIfAvailable()` 拿"循环在历史检查点发布的快照"（模型调用前 /
  assistant 落历史后 / 工具结果落历史后），最多滞后一个检查点。
  - 落账语义：`buildRecordLocked` 写的是"最近一次成功读到的检查点"，而不是
    卡几十秒后写下的那一份——崩溃恢复要的是写得及时，不是写得最全。
  - 结束路径（`Unregister`）此时 `ChatStream` 已出栈，读到的是权威历史；
    极端情况下退化为最后一次检查点，仍优于阻塞 actor。
  - 回归覆盖：`TestSubagentSessionsReadsDoNotBlockOnStreamingNode`
    （运行中节点自身读取 + 另一节点不被拖住）、`TestSubagentSessions*`。
- 运行期记录落盘（`<mainSessionID>-<subSessionID>.json`，见
  [sessionstore/README.md](../../sessionstore/README.md)）；节点结束
  （done/failed）时最终结论写入主会话事件库（`seelex.subagent.result`），
  节点记录文件随即删除——详情数据面保留在内存快照，进程存活期可读，
  重启后由恢复锚点（`Runtime.RestoreSubagentAnchors`）从主会话事件库重建；
- 压缩栈与主会话隔离：节点控制器使用独立内存栈（2026-08-24 修复，
  子代理压缩帧不再写入主会话 SessionContextStore）。
- `SubagentTree.Projection` 不读运行中子会话（2026-09-07 死锁 + 2026-09-10
  长流热点）：投影只持树锁浅拍快照（scalar + 结束快照指针 + 会话引用），
  释放树锁后组装 DTO，**全过程不取子代理会话锁**——运行中节点的紧凑上下文
  由 `NoteMessageCount` 上报的工作历史条数 + 遥测 trace 经
  `seelexctx.ExportSnapshotFromData` 组装。
  - 为什么不能读：`Session.ChatStream` 从进函数持到出函数整段持有会话锁，
    `Session.History()`/`ExportSnapshot` 用同一把锁。子代理长文流式（实测
    1200 字 ≈ 42s）期间读它会各被挡约 28s，表现为「工作表格卡住不动」。
  - 计数来源：节点在请求装配路径（`node/coordinator.go` 的
    `ScopeAssembler`，即持该节点会话锁的那个 goroutine）调用
    `Coordinator.NoteProgress` 上报 `len(WorkingHistory)`，因此观测侧读到它
    是无锁的；计数变化才 notify，避免每轮装配触发一次重投影。
  - 历史死锁同源消灭：投影一侧不再有会话锁，树锁 ↔ 会话锁的环不复存在
    （2026-09-07 真实 API 复现：多个子代理 running 后无任何 llm/tool 事件、
    行停留在 QUEUED）。回归覆盖：`subagent_tree_lock_test.go` 的
    `TestProjectionDoesNotBlockOnLiveSessionStream`（真会话持锁长流的确定性
    用例）与 `TestProjectionCarriesNodeReportedMessageCount`，以及
    `tmp/headless-smoke` 的 `TestRealAPIForkLiveProbe`（真实 API E2E）。

## 扩展方式

- 新增会话数据面：扩展 `SubagentSessions` 命令种类与处理；
- 调整 mailbox 容量：`NewSubagentContextActor(trace, queueCap)`；
- 改变合并策略：替换 `merger.NewMerger()` 调用点（保持 copy-on-write 语义）。

## Review 指南

- 是否仍由 actor 单 goroutine 持有全部可变状态（不再新增互斥锁）；
- `Drain` 是否只在锁外/ ChatStream 边界调用；
- overflow 是否"计数不丢"（不能悄悄丢消息）。

## 测试与验证

本包内：`subagent_sessions_test.go`；merge-back mailbox 并发/overflow 用例在根包 `merge_back_concurrency_test.go`（联调，保留根测试包）。验证：

```text
go test ./seelebridge/session/ -run 'TestProjection|TestSubAgent|TestSubagentSessions' -count=1
go test ./seelebridge/session/ ./seelebridge/ -count=1
go build ./...
```
