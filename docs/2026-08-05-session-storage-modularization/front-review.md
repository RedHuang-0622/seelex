# Session 存储模块化前置审查

## 结论

当前实现已经具备“按技术载体分片”的能力，但还没有实现“按领域模块分片”。JSON、SQLite 和 Redis 都能把 Provider history、Transcript event、状态、工具结果拆到不同物理对象中；然而 Application 的核心记录仍通过 `State []byte` 作为一个不透明的 `SessionRecord` 写入。这个 blob 同时包含可见对话、标题、Plan、执行投影、checkpoint、工具引用和 continuation，因此 UI 看到的完整会话、模型恢复所需的尾窗、工具结果原文和 context stack 之间没有清晰的存储所有权。

这正是“JSON 看起来离散，但又不知道每块属于谁”的根因：物理文件名分开了，领域契约没有分开。

本次只做前置审查，不改变现有数据格式。建议先确认下面的目标布局和迁移规则，再进入实现阶段。

## 当前真实数据流

### 写入路径

一次正常回合结束时，`application/core/session_archive.go` 的 `persistCurrentSession` 从内存复制四类数据：

1. `Engine.History()`：Provider working history，属于模型执行缓存。
2. `service.transcript`：协议正确的 TranscriptEvent 流，包含 user、assistant、tool call、tool result 及 `ResultRef`。
3. `service.sessionRecordLocked()`：可见 Conversation、标题、PlanStack、Execution、Projection、Checkpoints、ToolResults 引用。
4. `pendingToolResults`：大工具结果或压缩轮次原文的不可变 blob。

生产适配器 `application_adapters.go:586-603` 将它们打包为一个 `sessionstore.Commit`：

```text
Commit.ProviderHistory -> history.*.json
Commit.Events          -> events.*.json       (Transcript)
Commit.State           -> state.json           (完整 SessionRecord blob)
Commit.ToolResults     -> tool-results/<ref>.json
```

JSON 后端 `sessionstore/sessionstore.go:546` 每次创建新的 `generation-*`，重写 Provider history 和 Transcript 的全部分片，然后写入 `state.json`，最后替换 `manifest.json`。工具结果实际写在 session 目录下的 `tool-results/`，不属于 generation；manifest 只保存引用列表。

因此“原子提交”只对 Commit 携带的四类数据成立，而且其中 State 仍是一个大 blob。`SessionContextStore` 的 `Persist`（`sessionstore/session_context.go:142`）则直接调用 `Router.SaveState`，会在当前 generation 内原地覆盖 `state.json`，绕过 generation 切换。

### JSON 当前布局

```text
project-<hash>/
  session-<hash>/
    manifest.json
    generation-<id>/
      history.000.json ...       # Provider history/cache
      events.000.json ...        # TranscriptEvent archive
      state.json                  # opaque SessionRecord
      events.json                 # framework execution facts（另一种 event）
    tool-results/
      <ref-hash>.json             # ToolResult / compact turn 原文
```

这里有两个同名语义不同的事件面：`events.*.json` 是 Transcript，`events.json` 是 `frameworkevent.Sink` 写入的执行事实事件。前者参与 generation，后者不参与 generation manifest。

### 恢复路径

`application/core/session_history.go:14` 的 `resumeSession` 并行读取三个来源：

```text
SessionRecord(state.json)       -> UI 元数据、完整 Conversation、Plan/Task 投影
Provider history(history.*)    -> 尾部 provider cache
Transcript(events.*)            -> 最近完整协议单元
```

模型最终通常只得到 Transcript 的最近 4 个完整协议单元（`loadSessionTranscript` 和 `transcriptTailHistory`）。如果 Transcript 尾窗为空或不包含最新 user，代码才把 `SessionRecord.Conversation` 转换成 Transcript 再取尾窗。`LoadMoreHistory` 又会重新读取整个 `state.json`，然后在内存中切 `Conversation`（`application_adapters.go:689`）。

所以 UI 可以显示完整 Conversation，而模型只能看到尾窗；这不是压缩发生在前端，而是“可见投影”和“Provider 上下文缓存”本来就是两条不同的读取路径，且窗口外没有真正接入的语义 CompactStack。

## 按模块的所有权现状

| 领域模块 | 当前载体 | 当前所有者 | 主要问题 |
|---|---|---|---|
| 可见 Conversation | `SessionRecord.Conversation`，位于 `state.json` | Application | 与标题、Plan、执行投影、checkpoint 共用一个 blob；范围读取必须先解析整 blob |
| Transcript | `events.*.json` / SQL `seelex_session_event_shard` | Application adapter + sessionstore | 每次 Commit 读旧分片、全量 merge、全量重写；和 framework event 事件面命名混淆 |
| Provider history | `history.*.json` / SQL `seelex_session_shard` | Runtime/Seele DurableHistory | 只是执行缓存，不是真相源；恢复后只取尾窗，不能代替语义记忆 |
| 工具结果原文 | `tool-results/<ref>.json` / SQL `seelex_tool_result` | Application + sessionstore | `ToolResultRef`、Transcript.ResultRef、UI warning 三处重复引用；压缩轮次原文也复用该通道 |
| Plan | `SessionRecord.PlanStack`；另有 `SessionContextRecord.PlanStack` | Application / seelexctx 两套模型 | 两种 frame 类型和两个恢复入口，生产代码没有把 SessionContextStore 接到 Runtime |
| Task / Projection | `SessionRecord.Execution.Task`、`Projection`、`Checkpoints` | Application | Task 状态、checkpoint 和 context summary 混在 SessionRecord，更新频率与 Conversation 不同 |
| Skill / Compact | 设计在 `SessionContextRecord.SkillStack/CompactStack` | seelexctx | `NewSessionContextStore` 只有测试调用，生产没有 `AttachSessionContextStore`；窗口外摘要没有可靠 durable owner |
| Framework execution facts | JSON `events.json`；SQL `seelex_framework_event`；Redis list | Runtime EventStore | 与 Transcript 使用不同语义和不同原子边界；JSON generation rollover 时可能丢失旧 generation 的 `events.json` |
| Project knowledge | project 目录 `project-record.json` / SQL project table | sessionstore | 已经是独立项目模块，边界相对清楚 |

## 已确认的风险

### P0：JSON framework event 可能在 generation rollover 后不可见

`AppendFrameworkEvent`（`sessionstore/sessionstore.go:916`）将 `events.json` 写入当前 generation；`WriteCommit`（`:546`）创建新 generation 时只复制 history、Transcript、state，不复制 `events.json`。随后 `ReadFrameworkEvents`（`:942`）优先读取新 generation 的 `events.json`，找不到时只回退 session 根目录，不会读取旧 generation。一次普通的下一轮 Commit 就可能使之前的 framework execution facts 对读取端消失。

这不是“模块化不够漂亮”，而是事实轨数据丢失风险。目标布局必须把 framework events 设为独立 append-only 模块，或明确纳入 root manifest 的 generation 指针。

### P1：State blob 不是冷加载模块

`SessionRecord`（`application/model/state.go:160`）把 Conversation、Plan、Execution、Projection、Checkpoints、ToolResults 放在一个 JSON 对象中。`LoadConversationRangeWorkspace`（`application_adapters.go:689`）先完整 `LoadSessionRecordWorkspace`，再切消息范围；长会话向上翻页仍然需要将全部 Conversation 和所有执行状态载入内存。

这违背文档中“前端 select 只读取窗口、内存只保留当前窗口”的目标，也解释了长任务时内存峰值和恢复变慢。

### P1：SessionContextStore 的四个 stack 尚未进入生产恢复链路

`SessionContextRecord`（`sessionstore/session_context.go:66`）已经定义 SystemPrompt、PlanStack、TaskStack、SkillStack、CompactStack，但生产装配只调用了 `runtime.AttachHistoryRouter`（`main.go:125`）和 `SetTurnArchiver`（`main.go:241`），没有创建或调用 `AttachSessionContextStore`。`seelebridge/context_components.go:177-182` 的生产接线仍是待接入能力。

因此当前模型上下文窗口外没有稳定的 durable CompactStack；早期事实即使在 UI Conversation 中存在，也不会自动成为下一轮 provider prompt 的语义摘要。

### P1：长会话读取钩子和压缩范围索引还没有形成闭环

现有 `CompactFrame`（`sessionstore/session_context.go:52-62`）的 `From/To` 明确表示 Provider `ChatQueue` 中的**累计轮次单元索引**，不是 Application 的 `message-*` ID，也不是 Transcript 的 `Event.Seq`。压缩控制器（`seelexctx/controller.go:448-459`）固定生成 `From=0` 或沿用上一帧起点，`Runtime.seelexController` 也没有把当前 session ID 注入 `ControllerOptions.SessionID`，因此 SegmentID 目前不能稳定地作为跨恢复实例的会话索引。

这会造成三个问题：

1. UI 只能依赖 Conversation 的尾窗和 `LoadMoreHistory`，无法从 CompactFrame 的范围直接 select 到对应的会话记录。
2. 模型或工具拿到 `segment_id` 后，只能读压缩原文 blob，不能知道它覆盖了哪些可见消息、哪些 Transcript event，也不能定位前后邻接轮次。
3. 恢复时无法判断“当前 Transcript 尾窗是否覆盖了该 CompactFrame 之后的全部内容”，只能用最新 user 是否出现的启发式回退。

压缩帧必须同时保存三种可审计定位：

```text
session_id                     # 归属会话，不能只靠时间戳推断
segment_id                     # 稳定的压缩片段 ID
unit_range: [from, to]         # Provider 协议单元范围，保留现有审计语义
event_range: [seq_start, seq_end]
message_range: [id_start, id_end]
transcript_revision            # 该范围所属的 Transcript 提交版本
conversation_revision          # UI 可见 Conversation 提交版本
```

其中 `event_range` 是恢复和模型组装的主索引，`message_range` 是 UI 直接 select 的主索引；`unit_range` 只用于验证 ContextController 的窗口不变量，不能单独承担恢复定位。当前 `TranscriptEvent` 没有持久化 Application `Message.ID`，不能依赖数组位置临时推导 `message_range`；需要给 Transcript event 增加 `MessageID`，或建立同 revision 的 event-to-message 索引表。

## 长会话读取与恢复钩子

当前 `LoadConversationRangeWorkspace`（`application_adapters.go:689-705`）的实现是：先读取完整 `SessionRecord`，再切 `record.Conversation.Messages[offset:end]`。这不是长会话的存储 select，而是“读取大 blob 后在 Application 内存切片”。

目标接口应按模块暴露，而不是继续扩展 `LoadSessionRecord`：

```go
type ConversationReader interface {
    LoadConversationRange(ctx context.Context, scope Scope, sessionID string, offset, limit int) (Page[Message], error)
    LoadConversationAround(ctx context.Context, scope Scope, sessionID, messageID string, before, after int) (Page[Message], error)
}

type TranscriptReader interface {
    LoadTranscriptTail(ctx context.Context, scope Scope, sessionID string, tokenBudget, maxUnits int) ([]TranscriptEvent, error)
    LoadTranscriptRange(ctx context.Context, scope Scope, sessionID string, fromSeq, toSeq uint64) ([]TranscriptEvent, error)
}

type CompactReader interface {
    LoadCompactFrame(ctx context.Context, scope Scope, sessionID, segmentID string) (CompactFrame, error)
    LoadCompactOriginal(ctx context.Context, scope Scope, sessionID, segmentID string, offset, limit int) (Page[Message], error)
}
```

恢复流程建议改为：

```text
root manifest
  ├─ metadata select
  ├─ conversation tail/range select       -> UI
  ├─ transcript tail + event_range select -> provider
  ├─ context stacks select                -> assembler
  └─ execution projection select         -> task panel
```

只有用户显式滚动、点击压缩片段、或模型通过 `read_compressed_turn` 请求证据时，才按 `message_range`、`event_range` 或 `segment_id` 读取更老内容。恢复会话不应再把完整 `SessionRecord` 作为默认入口；它只应作为 v1 兼容读取器和迁移输入。

## 对“直接 select 会话存储内容”的判断

这个方向是正确的，但 select 必须针对明确模块和索引，不能提供一个重新返回整个 SessionRecord 的万能接口：

- 会话列表：只 select metadata/manifest。
- UI 历史页：只 select conversation shard 的范围。
- 模型恢复：select transcript 尾窗、CompactFrame 摘要和最新未完成 user unit。
- 压缩详情：先 select CompactFrame 的范围，再按 `segment_id` 分页读取 tool-result 原文。
- 执行详情：只 select framework-events 或 execution projection。

这样“历史可见性”和“模型上下文”可以分别扩展，而不会因为 UI 要查看一页旧消息就把整份上下文和工具输出重新载入内存。

## DDD 领域编号约束

Session 应作为聚合根，ConversationRound 作为有序子实体。会话截断不能依赖 `generation-<random>`、时间戳或哈希字符串；这些只能是物理存储句柄。领域层至少需要三种单调编号：

| 编号 | 作用 | 是否允许重新编号 |
|---|---|---|
| `RoundNo` | 会话轮次截断、Conversation 直接 select | 不允许 |
| `EventSeq` | Transcript 事件顺序和恢复范围 | 不允许 |
| `Message.ID` | UI 增量 reducer 和消息定位 | 不允许 |

压缩帧保留旧的 `unit_range` 兼容字段，但正式恢复索引必须使用 `RoundFrom/RoundTo` 和 `EventFrom/EventTo`。同一 Session 的轮次写入必须由一个聚合写入器分配，不能由各 backend 按 shard 或时间自行生成。

### P1：同一会话存在三份可相互漂移的历史

Provider history、Transcript 和 Conversation 由不同代码维护。恢复时使用“Transcript 尾窗优先、最新 user 不在则从 Conversation 重建”的启发式补偿（`session_history.go:48-103`），而不是由一个模块 manifest 给出一致版本。这可以修复部分空 checkpoint 场景，但不能证明三者覆盖的 event range 一致。

### P1：写入粒度与更新频率不匹配

Conversation、Plan、Task、Compact stack 和 tool refs 的修改频率不同，但每次 `SaveSessionSnapshot` 都会重新 marshal 整个 `SessionRecord`；而 `SessionContextStore.update` 又能单独改写同一个 `state.json`。这会造成大 blob 写放大、模块间 revision 不透明，以及对恢复顺序的隐式依赖。

### P2：各 backend 的模块原子性不一致

SQL 已经把 `seelex_session_state`、`seelex_session_event_shard`、`seelex_tool_result`、`seelex_framework_event` 分成表（`sessionstore/sessionstore.go:1595-1641`），Redis 也按 key 分开；但 JSON 把 State 放在 generation 内、framework event 放在 generation 内的独立文件、tool result 放在 generation 外，三种 backend 的快照边界并不等价。Repository 接口的 `State []byte` 也无法表达模块 revision。

## 推荐目标布局

不要让“按模块分片”变成把一个大 JSON 拆成几个随意文件；需要让每个模块有明确 owner、读取接口和 revision。建议使用 root manifest + 模块 manifest：

```text
project-<hash>/session-<hash>/
  manifest.json                    # layout_version + module revisions + commit id
  metadata/
    session.json                   # id/title/workspace/created/updated
  conversation_message/
    manifest.json
    shard-000.json ...             # 语义消息及 tool call/result，按 RoundNo 读
  events/
    manifest.json
    shard-000.json ...             # 独立流式/框架事件，按 EventSeq 读
  provider-cache/
    manifest.json
    shard-000.json ...             # 有界执行缓存，可重建，不是真相源
  execution/
    projection.json
    checkpoints.000.json ...
    continuation.json
  context/
    system-prompt.json
    plan-stack.json
    task-stack.json
    skill-stack.json
    compact-stack.json
  tool-results/
    <ref-hash>.json                # 内容不可变，引用方只保存 ref/digest/range
```

### 所有权规则

- `conversation_message` 是语义消息的唯一真相源；每个完整 RoundNo 内包含 user、assistant、tool call/result 摘要，不把它塞进 `context` 或 provider cache。
- `events` 是独立 append-only 事件流；工具生命周期、stream chunk 和框架诊断通过 `MessageID/CallID/RoundNo` 关联，但不作为对话恢复真相源。
- `provider-cache` 只保存当前 provider 所需的有界窗口，丢失时从 conversation_message + context 重建。
- `context` 下四个 stack 分开存储；`compact-stack` 的每个 frame 必须记录 `segment_id`、覆盖 event range、摘要版本和 evidence refs。
- `tool-results` 只保存不可变原文；conversation_message、events、CompactFrame 都只保存引用。

### 提交与恢复协议

1. 后端先为变更模块写入新的 immutable shard/generation。
2. 写完并校验模块 manifest 后，原子替换 root `manifest.json`，其中记录所有模块的 revision、event range 和 schema version。
3. 恢复时先读 root manifest，再并行读取 metadata、conversation_message 尾窗、context stacks 和 execution projection；只有事件重放或诊断才读取有界 events 尾窗。
4. UI 只读取 conversation_message range；模型上下文由 conversation_message 尾窗 + CompactStack 摘要 + 当前回合组装；provider cache 仅作加速，不作真相源。
5. 任一模块损坏时按降级矩阵处理：provider cache 可重建；conversation_message/context 必须显式报错或显示缺口；events 缺失时诊断能力降级但不能伪造为空。

## 迁移策略

1. 保留现有 `layout_version=1` 读取器：先读旧 `manifest.json` 和 `state.json`。
2. 第一次成功写入新布局时，将旧 `SessionRecord` 一次性拆分到 metadata/conversation_message/execution/context；旧 `history.*`、legacy `events.*`、framework events 和 tool-results 原样迁移并建立引用。
3. 新 root manifest 写入 `layout_version=2` 和各模块 revision 后，再把旧 generation 标记为兼容只读；不在迁移成功前删除旧数据。
4. `LoadConversationRange` 改为只访问 conversation 模块，不再反序列化完整 SessionRecord。
5. `SessionContextStore` 增加按模块 provider 或统一 `ContextSnapshot` 读取接口，并在 `ResumeSession` 建立真实 `AttachSessionContextStore` 接线；没有 context 数据时明确返回“无摘要”，而不是默默退化成内存栈。
6. 为 JSON、SQLite、Redis 共用同一组模块级 contract tests：提交后可见、崩溃前后 manifest 二选一、各模块 revision 一致、旧布局可读、framework facts 不丢失、tool ref 可追溯。

## 建议的实现批次

| 批次 | 内容 | 验收重点 |
|---|---|---|
| 1 | 修复 JSON framework event 的 generation 丢失问题；补 framework-events 独立 contract test | 连续两次 Commit 后旧 execution facts 仍可读 |
| 2 | 引入模块化 `SessionSnapshot`/`ModuleManifest` 接口，旧 `Commit.State` 保持兼容 | JSON/SQLite/Redis 读写语义一致 |
| 3 | Conversation 独立分片和范围读取 | 长会话向上翻页不加载完整 state blob |
| 4 | Context 四 stack 独立持久化并接入 Resume/Runtime | CompactFrame 能参与下一轮 prompt 组装 |
| 5 | Provider cache 降级为可重建尾窗，增加完整恢复/断电/竞态测试 | UI 完整、模型有摘要、两者不互相覆盖 |

第 4 批次还必须包含：为 CompactFrame 增加 `event_range/message_range`，把 session ID 注入 ContextController，并实现 Conversation/Transcript/Compact 的长会话 select 钩子；否则“压缩了但不知道覆盖哪段会话”的问题仍然存在。

## 需要确认的边界

在进入实现前，需要确认两点：

1. 是否接受保留旧 JSON/SQLite/Redis 布局的只读兼容期，并由首次写入迁移到新模块布局？
2. `provider-cache` 是否明确视为可丢失缓存（从 Transcript + CompactStack 重建），还是必须继续作为持久化主链的一部分？

确认后再进入 devplan/实现阶段，避免在没有确定“真相源”的情况下只做文件拆分，最后仍然保留同一个大 `state.json`。
