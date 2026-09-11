# 会话全序日志（Session Order Log）设计与实施路线

> 状态：**设计中，已按用户决策落地第一批（显式类别贯通 + JSON transcript append-only 日志）**。
> 本文件是一次性工作包的方案稿，不是长期架构事实；代码与测试仍是最终事实源。
> 关联：外部调研 [codex-session-resume.md](../research/codex-session-resume.md)；既有已实现架构 [context-prefix-chain.md](../arch/context-prefix-chain.md)；会话改造目标 [target-design.md](../2026-09-02-session-subsystem-remediation/target-design.md)。

## 1. 目标（用户需求转译）

1. 审查 Seelex 当前会话恢复方式；
2. 调研 Codex 会话恢复；
3. 参考 Codex：恢复时同时恢复**内容**与**整体调用顺序 / LLM 请求顺序**；
4. 对日常请求发送做治理：会话内容 **append-only**，发送按**前缀 + 追加**组织；
5. **从存储层开始**：按“顺序”存，而不是按“类别”存。

## 2. 现状审查（代码事实）

### 2.1 存储：按类别 + 代覆盖快照

- `sessionstore.Repository` 把会话拆成多通道：`ProviderHistory`（有界 provider 缓存）、`Events`（transcript 事件）、`State`（SessionRecord 全量 JSON）、`ToolResults`（超限结果对象）、`ContextState`（system prompt + plan/task/skill/compact 四栈）；另有 `framework-events.json` 执行事件库（v2 起为独立 append-only 文件）。
- `Commit{ProviderHistory, Events, State, ToolResults}` 每次落盘：读旧 manifest → `mergeEvents(existing, commit.Events)` → 新建 `generation-<id>/` 写入全量 history/events/state shard → 原子替换 `manifest.json`。**逻辑上事件 append-only（按 Seq 合并、幂等），物理上整代重写**。
- 权威分散：record（可见对话/plan/task）、provider history（引擎缓存）、transcript（事件权威）、tool results（不可变对象）各有独立读取 API（Read/ReadRange/ReadEventTail/ReadToolResult/…）。

### 2.2 运行期：transcript 已有每会话全序

- `task_context` 的 `transcript` 对每个会话维护 `transcriptSeq` 自增并逐条 append；事件含 role（user/assistant/tool…）、reasoning、tool calls、result ref、TaskID、token count。
- 内部指令（激活 skill 正文、checkpoint 信封等）也以 internal user 事件进入同一条 append-only 流。
- 但该流**只覆盖对话类事件**：不记录请求边界/生命周期（turn_started/request_started）、usage、world state、压缩帧与回滚标记的统一顺序，也未把“一次 LLM 请求 + 其工具周期”作为一等边界。

### 2.3 落盘：整轮快照重建

- `session_runtime/archive.go PersistCurrentSession`：读磁盘事件全集 + 内存事件按 Seq 合并 → 从事件重建可见对话 → 连同 `engineHistory`（provider 全量）、SessionRecord、pending tool results 一次写快照。
- 提交时机为每轮收尾/切走/LRU 驱逐/关闭/压缩 checkpoint，属于**轮次级原子快照**，运行中中间 LLM 周期只在内存。

### 2.4 恢复：三读拼装 + 尾部窗口

- `resumeSessionCold` 三路并发读 record / history 尾部 / transcript 尾部；
- record 为可见对话权威；引擎历史优先取 `TranscriptTailHistory(transcript, budget, maxUnits=4)`（尾部协议单元窗口），或 record 派生对话，兜底 resume 信封；
- 之后 context_runtime 在每次请求时按 prefix chain 组装 system → project → memory → compact → 累积 context → plan/task → 当前输入。
- 结果：可见内容可恢复，但**磁盘上没有一条能精确重放的“上次实际发过/实际发生的完整调用序列”**；冷恢复只拿到尾部窗口 + 摘要帧的近似，跨重启的“前缀字节稳定”与调用/LLM 顺序无法保证。

### 2.5 已实现的基础（不要重造）

- `context-prefix-chain.md`（已实现）：运行期请求已经是「稳定前缀 + 已定稿轮次 append-only 累积段，达峰才压缩，plan/task 后置不参与压缩」；
- `TranscriptTailHistory(events, budget, maxUnits<=0)` = 全量累积；`RetainedSystemHistory` = 从引擎历史剔除动态尾部得到可复用前缀；
- `framework-events.json` 已示范物理 append-only 文件与按 Seq 读。

## 3. 差距总结（现状 vs Codex）

| 维度 | Seelex 现状 | Codex rollout | 差距 |
|---|---|---|---|
| 存储组织 | 按类别 + manifest 代快照 | 每会话单条物理 append-only JSONL，类别混排 | 缺“从存储开始按顺序” |
| 写入 | 轮次级整代重写（read-merge-replace） | 单 writer 逐条 append + barrier | 缺运行时增量追加与崩溃续写 |
| 顺序范围 | transcript 事件序（对话类） | 全类别（meta/context/world/lifecycle/usage/compacted）统一 ordinal | 请求/工具/生命周期未统一成序 |
| 发送治理 | 运行期 prefix chain（内存级） | 每次请求 = 完整有序前缀 + 追加；rollback/compact 是流内标记 | 治理不跨重启 |
| 恢复 | 三读拼装 + 尾部窗口近似 | 从最新压缩检查点后正序重放后缀 | 内容/调用/LLM 顺序无法精确还原 |
| 派生层 | 无（各通道都是权威） | DB/UI 只是 rollout 投影 | 权威与投影未分层 |

## 4. 目标模型：从存储开始按顺序

### 4.1 原则

1. **每会话一条全序日志是唯一事实源**：所有“类别”内容都作为日志条目按发生顺序落盘；
2. **日志物理 append-only**：不改写旧条目；回滚与压缩以流内标记表达；
3. **既有通道降级为派生投影**：Commit/record/visible conversation 改为从日志重放生成（或短期内保留为兼容缓存，但写入顺序必须来自日志）；
4. 单一写者串行追加，保证 append 顺序 = 真实事件顺序。

### 4.2 日志条目（envelope）建议

```go
type SessionLogKind string

const (
    LogSessionMeta     SessionLogKind = "session_meta"      // 会话头：id/project/settings/base
    LogUserInput       SessionLogKind = "user_input"        // 用户提交（含合并队列）
    LogInternalUser    SessionLogKind = "internal_user"     // 内部注入（skill 正文/plan 政策/恢复信封等，对标现有 internal user）
    LogAssistant       SessionLogKind = "assistant"         // LLM 文本回复
    LogReasoning       SessionLogKind = "reasoning"         // reasoning 片段
    LogToolCall        SessionLogKind = "tool_call"         // 模型宣告 tool call
    LogToolOutput      SessionLogKind = "tool_output"       // 工具结果（超限存 ref）
    LogRequestBegin    SessionLogKind = "request_begin"     // 一次 LLM 请求边界（含组装指纹）
    LogRequestEnd      SessionLogKind = "request_end"       // 完成/中断/错误 + usage
    LogTurnBegin       SessionLogKind = "turn_begin"
    LogTurnEnd         SessionLogKind = "turn_end"
    LogWorldState      SessionLogKind = "world_state"       // full/patch
    LogContextChange   SessionLogKind = "context_change"    // 影响前缀的配置/上下文变化
    LogCompacted       SessionLogKind = "compacted"         // 压缩帧：summary + replacement 基线 + 窗口血缘
    LogRolledBack      SessionLogKind = "rolled_back"       // 回滚 N 个用户轮（不删数据）
    LogTokenUsage      SessionLogKind = "token_usage"
    LogSubagent        SessionLogKind = "subagent"          // join 回父会话的证据
)

type SessionLogEntry struct {
    Ordinal   uint64            // 每会话全局单调
    Seq       uint64            // 若为对话事件，保留原 TranscriptEvent.Seq 供 UI/投影连续
    Kind      SessionLogKind
    RequestID string
    TurnID    string
    MessageID string
    CreatedAt time.Time
    TokenCount int
    Payload   json.RawMessage   // 类型化内容：role/content/tool calls/usage/world/compaction/...
}
```

设计要点：

- `Ordinal` 与 `Seq` 解耦：非对话事件（lifecycle/usage/world）不挤占对话 Seq；
- 对话类事件尽量保持与现有 `TranscriptEvent` 1:1，减少重建成本；
- `request_begin/request_end` 让“一次 LLM 请求 + 其工具周期”成为可恢复的一等边界；
- `compacted` 携带 replacement 基线（对标 Codex `CompactedItem.replacement_history`），使压缩后可精确续播；
- `context_change`/`world_state` 记录会影响前缀重建的差异；system/project/memory 层仍由 context_runtime 按当前配置渲染，日志记录其生效版本/指纹。

### 4.3 物理布局（按后端）

- JSON：`<sessionDir>/rollout.jsonl`，每行一个 envelope；创建/续开用 append 模式；崩溃恢复扫描尾行推 `next_ordinal`；保留 `manifest.json` 作为派生快照指针直至迁移完成。
- SQLite/PostgreSQL：`seelex_session_log(project_id, session_id, ordinal, kind, request_id, turn_id, message_id, seq, created_at, token_count, payload)`，`PRIMARY KEY(project_id, session_id, ordinal)`；追加+推进 head 在同一事务；不整代重写 shard。
- Redis：项目 hash-tag 内每会话有序结构（如 list/stream）+ head ordinal 计数器，单 MULTI/EXEC 原子追加。

## 5. 运行期写入治理：append-only + 前缀发送

把现有 prefix chain 从“引擎内存内重排”收紧为“日志驱动”：

1. 所有事件先经 task_context / 请求生命周期钩子落一条日志（单一写者），再进入内存 history / 引擎；
2. 请求发送前，context_runtime 从**当前重放基线**（运行期=内存历史；恢复期=日志重放结果）取完整有序前缀，追加本轮新事件与 plan/task/当前输入后发送；不按类别重新拼装、不重排旧条目；
3. 前缀字节稳定用既有 `RetainedSystemHistory` 语义测量：新增事件只追加在保留段之后；达到软阈值才触发压缩，压缩以 `compacted` 条目落日志（含 replacement 基线），此后新请求从新基线续播；
4. 回滚/清空不删日志：写 `rolled_back` 标记，重放时按标记截断。

不变式（写进契约测试）：

- `I-LOG-1`：同会话日志 Ordinal 单调连续，无空洞、无重复；
- `I-LOG-2`：日志追加顺序 = 运行期事件发生顺序（user input → request → assistant/tool → output → next request）；
- `I-LOG-3`：任何已落盘条目不可原地修改；可撤销仅通过 rolled_back/compacted 标记；
- `I-LOG-4`：请求 input 序列可由日志重放得到，重放结果与运行时组装结果逐字节一致（对同一版本上下文）；
- `I-LOG-5`：压缩是唯一使前缀失效的事件，压缩后日志仍可重放续播。

## 6. 恢复设计：顺序重放

把 `resumeSessionCold` 改为“日志为主、旧通道兜底”：

1. 打开会话日志，读 head（last ordinal / checkpoint）；
2. 逆序找最新 `compacted`（含 replacement 基线）；无则从 `session_meta` 开始；
3. 对存活后缀**正序重放**：
   - 重建可见对话（现有 conversationFromTranscript 语义保留）；
   - 重建 transcript（全量按 Seq，含 internal 事件）；
   - 重建引擎基线（带预算与窗口语义，但顺序与内容来自日志而非近似）；
   - 应用 world_state 差异、恢复 context baseline、window/usage 统计；
4. 未完成轮次处理：重放到最后一个完整单位；残余 in-progress 事件保留或标记 interrupted（进入决策项，见 §8）；
5. 恢复完成后引擎历史 = 重放前缀；下一请求走 §5 的同一前缀 + 追加路径，**恢复前后的顺序语义完全一致**；
6. 旧会话无日志时的迁移/降级见 §7。

## 7. 存量数据与迁移

- 旧会话没有完整调用序（这正是问题），无法凭空重建历史 LLM 请求输入；迁移只能保证：可见对话 + 事件序一致，并生成 `session_meta` + `legacy_import` 基线条目；
- 迁移策略建议“**下一次提交起双写**”：新事件开始进日志；旧前缀以 compacted/import 摘要作为基线；后续重放从基线续播，新会话从第一轮就全序；
- 迁移属用户数据处理：按 MEMORY.md 先备份、先预警、获确认后执行，不静默改写 `.seelex`/dist 副本；
- 旧 Repository 读 API 保留为兼容投影，直至所有后端完成切换并做回归，才允许移除（单独一期，需要用户明确授权）。

## 8. 分期实施建议

| 期 | 内容 | 验收 | 风险 |
|---|---|---|---|
| P0 | 契约先行：定义 `SessionLog` 条目/接口/不变式测试（JSON 最小实现：物理 append + ordinal + 崩溃续写） | `go test ./sessionstore -count=1`；I-LOG-1/2 契约测试 | 低（新增通道） |
| P1 | 运行期单写者接入：task_context/请求生命周期在现有 transcript append 处同步写日志（先 JSON 后端），Commit 落盘由“整代重写”改为“日志导出投影” | 现有会话切换/恢复回归绿；双写一致性测试 | 中（并发/锁） |
| P2 | 恢复改造：resumeSessionCold 从日志重放重建内容与顺序；旧通道兜底 | 冷恢复后重放顺序与运行期一致；中断/压缩场景测试 | 高（涉及多模块） |
| P3 | 发送治理：请求组装改为“基线 + 追加”，前缀稳定、压缩/回滚走流内标记；恢复路径复用同一组装 | I-LOG-3/4/5 测试 + 跨重启前缀稳定性 | 中 |
| P4 | SQLite/PostgreSQL/Redis 后端日志 + 全后端投影 | 跨后端契约测试 | 高（schema/migration） |
| P5 | 存量迁移与旧通道退役（先备份+预警+确认） | 迁移工具 + 回归 + README 同步 | 高（用户数据） |

建议起点：P0 + P1 的 JSON 后端垂直切片（含一段真实运行日志的往返测试），验证成本最低且能回答“从存储开始按顺序”是否成立；P2/P3 依赖该切片。

## 9. 涉及代码位置（实施时以此为入口，先读各自 README/测试）

- `sessionstore/sessionstore.go`：Repository/Commit/manifest/generation；
- `sessionstore/event_store.go`：物理 append-only 先例（framework-events.json）；
- `internal/adapters/session_workspace_ports.go`：SaveSessionSnapshotWorkspace 类别映射；
- `application/core/task_context/task_context_state.go`：transcript Seq/append 入口；
- `application/core/context_runtime/coordinator.go`：PrepareExecutionContext / fitExecutionHistory / RetainedSystemHistory；
- `application/core/session_runtime/archive.go`：PersistCurrentSession / 全量重建；
- `application/core/session_history.go`：resumeSessionCold 三读/尾部窗口；
- `application/model`：TranscriptEvent/SessionRecord/DTO（新条目类型下沉位置）。

## 10. 待你确认的范围问题（非阻塞评审项）

1. 只先做 JSON 后端垂直切片（推荐），还是要求四后端同步落地？
2. 是否需要记录每次 LLM 请求的“组装指纹/usage/request 边界”（比对话事件更细），还是只保证对话事件 + 工具周期顺序？
3. 旧通道按“保留为投影直到迁移完成”处理，还是允许直接切换为日志单源？

> 实施任一阶段均遵循 AGENTS.md：先契约测试、同步模块 README、不自动 commit；涉及存量用户数据（.seelex/config/dist）的删除或覆盖前必须先预警并获得确认。

## 11. 执行记录与决策（2026-09-07）

用户决策：

1. **不做迁移**：直接重构后端存储模块，旧数据不导入（保留在磁盘但不读取；不执行任何删除/覆盖）；
2. **以抽象接口为实现缝**：Repository/Router 对外方法集合保持不变，优先改造接口下的后端实现；确需改接口时同步改；
3. **轨迹多线谱需要显式识别历史条目类别**：工具调用 / LLM / 用户输入 / 工具输出 / 错误等不能只靠 role 启发式。

本轮已落地（代码为事实源）：

- `sessionstore.Event.Kind` + 常量与 `EventKindOf` 回退；`model.TranscriptEvent.Kind` /
  `model.Message.Kind`；adapters 双向透传；task_context append 时统一补标；
  archive 重建可见消息时写入 Kind；前端 `trajectory.js` 显式 kind 优先、role 回退
  （含契约测试）。
- JSON 后端 transcript 事件改为每会话 `transcript.log` 物理 append-only 增量追加
  （`WriteCommit` 按 `Seq` 求增量、幂等重试、崩溃残尾跳过），不再整代重写事件分片；
  `ReadEventTail`/`ReadEventRange` 以日志为事实源；history/state 仍走 generation
  快照（派生投影）。配套 `sessionstore` 全量测试与契约测试通过。

剩余（仍按 §8 分期推进，下一步建议）：

- SQLite/PostgreSQL/Redis 的 transcript 有序日志化（保持 Repository 方法不变）；
- 会话级单一全序日志继续扩展 `kind`（request_begin/request_end/compacted/rolled_back…），
  恢复路径改为“最新检查点后正序重放”；
- 请求发送治理的跨重启前缀稳定性与“压缩=唯一前缀失效”测试。
