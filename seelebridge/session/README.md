# seelebridge/session — 子代理会话与上下文 actor 域

## 模块定位

承载子代理会话注册表与父证据/merge-back 两个 actor。主要调用方：根包 `runtime.go` 装配、`agent_node.go` 会话门面、`actor.go` 的 merge-back facade。

## 职责与非职责

职责：

- `SubagentSessions`：运行中子会话 History 实时读取、结束快照保留、节点工具结果归档（`ToolResultArchiverFor`/`ToolResult`）；
- `SubagentContextActor`：父证据读-合并-写回（`MergeBackIntoParent`）、merge-back 有界 mailbox（`Enqueue`/`Drain`）、父证据无锁读取（`NodeParentEvidence`）。

非职责：

- 不执行子代理（执行在 `node/` 域）；
- 不决定合并策略（`seelexctx/merger` 负责）；
- 不接触主会话锁（mailbox 只是消息进出）。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `subagent_sessions.go` | 会话注册表 actor |
| `subagent_context.go` | 父证据合并 + merge-back mailbox actor |
| `context_clone.go` | `ContextSnapshot` 深拷贝辅助 |
| `subagent_sessions_test.go` | 会话注册表单元测试 |

## 核心实现

两个组件都是"channel 命令 + 单消费者 goroutine"的 actor：外部经有界命令通道投递操作（带 10s 超时），actor 串行处理，天然免锁。

- `SubagentContextActor`：`handleMerge` 串行执行"读当前父证据 → `merger.MergeBack`（copy-on-write 累积）→ 写回 `atomic.Pointer`"；`handleEnqueue` 在队列达 soft cap 时仅计数 `Overflow`，内容全部保留；`Drain` 一次性全量回收。
- `SubagentSessions`：注册/注销导出结束快照（`seelexctx.ExportSnapshot`，trace 可为 nil 降级）；工具结果走节点专属 `InMemoryToolResultArchiver`，ref 前缀由 `internal/model.NodeResultRefPrefix` 统一。

## 数据流或生命周期

子代理完成 → `node/` 域 `mergeBack` 导出快照 → `SubagentContextActor.MergeBackIntoParent`（合并写回 parentEvidence）→ `Format()` 文本 `Enqueue` → 主会话 ChatStream 前后 `injectPendingSubagentContexts` 排空 mailbox 注入。

## 依赖方向

`session` → `seelexctx`（merger/provider/snapshot）、`internal/model`、框架 `session` 类型。**禁止反向依赖 seelebridge 根包**。

## 并发、存储、安全或错误语义

- actor 串行化父证据合并，防并发覆盖（B 修复）；
- mailbox soft cap 只作诊断计数、内容不丢（A 修复）；
- 命令投递带超时，actor 关闭后快速失败；
- 内存态，不落盘；进程存活期可读。

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
go test ./seelebridge/session/ ./seelebridge/ -count=1
go build ./...
```
