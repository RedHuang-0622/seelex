# contract/protocol

## 生态位

`contract/protocol`（Go 包名 `model`）是 Snapshot、Event payload、会话记录和
前后端共享视图形状的权威纯契约层。主要调用方为 `backend`、`middle`、
`application` 与测试适配器；`middle/protocol/model` 在迁移期保留同身份 alias。

## 职责与非职责

- 做：定义版本化 wire 结构、枚举别名、JSON tag，以及只操作这些值类型的
  防御性 clone / 格式化 helper。
- 不做：不读写存储、不创建事件通道、不持有锁或可变全局状态，也不依赖
  backend、middle、application。

## 目录结构

| 文件 | 职责 |
|---|---|
| `state.go` | Snapshot、会话、消息、Runtime、Plan、WorkItem 与 clone helper。 |
| `context.go` | Task checkpoint、transcript 与 active skill 投影。 |
| `fork.go` | 会话 fork 请求与血缘 DTO。 |
| `event_units.go` | 完整协议轮次切分、Event 段落边界与 fork cut 的唯一实现。 |
| `conversation_window.go` | Durable 行计数与头/尾有界窗口的唯一实现。 |
| `session_input_index.go` | 会话输入索引只读形状。 |
| `toolcall_status_record.go` | 工具调用存档词到契约枚举的唯一转换点。 |
| `visible_output.go` | 流式可见正文的 think 块过滤（状态型 helper 的唯一实现）。 |

## 核心实现

结构体字段与 JSON tag 原样来自 `middle/protocol/model` 的 BM-002 迁移。
契约枚举直接复用 `contract/dto`，不维护第二份词表。clone helper 保持原实现的
切片、map、指针与嵌套节点深拷贝语义。

## 数据流

领域协调器构造本包值类型，Application 组装 Snapshot，`middle/protocol/event`
封装增量；GUI/TUI 只消费同一份 JSON 形状。本包本身不发布或持久化数据。

## 依赖方向

只允许标准库和 `contract/dto`。禁止 import `backend`、`middle`、
`application`、`store` 或 UI 包。

## 并发与兼容语义

本包类型是值语义容器，不提供线程安全保证；持有者负责锁。字段增删、JSON
重命名或 revision/seq 含义变化都属于 wire 变更。`middle/protocol/model` 的
alias 与这里同身份，不经过 JSON 转换。

## 扩展方式

先在 `contract/dto` 复用已有枚举与纯词表，再按主题加入本包；不得把行为状态机
带进来。新增跨前端字段时同步 GUI schema、reducer 和协议测试。

## Review 指南

- JSON tag 是否与迁移前逐字一致。
- 是否出现第二份 clone 或状态词转换实现。
- 是否引入 middle/backend/application 依赖。
- 共享 slice/map 是否在 clone 时仍被浅共享。

## 测试与验证

`middle/protocol/model` 的现存 wire/clone 用例通过 alias 继续覆盖本实现；另跑：

```text
go test ./contract/protocol ./middle/protocol/model -count=1
go test ./contract -run 'TestBM00' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### conversation_window.go

- `func DurableConversationCount(messages []Message) int` — DurableConversationCount returns the number of visible rows that participate
- `func BoundConversationTail(messages []Message, window int) []Message` — BoundConversationTail keeps the tail window with system and durable rows
- `func BoundConversationHead(messages []Message, window int) []Message` — BoundConversationHead keeps the head window with system and durable rows

### conversation_window_test.go

- `func TestConversationWindowKeepsSystemAndDurableRowsSeparately(t *testing.T)`

### event_units.go

- `func CompleteEventUnits(events []TranscriptEvent) [][]TranscriptEvent` — CompleteEventUnits splits a transcript into visible protocol units: user
- `func EventParagraphs(events []TranscriptEvent) []EventParagraph` — EventParagraphs derives paragraph boundaries from CompleteEventUnits.
- `func ParagraphEnd(events []TranscriptEvent, seq uint64) (uint64, bool)` — ParagraphEnd returns the inclusive end of the paragraph containing seq.
- `func IsParagraphBoundary(events []TranscriptEvent, seq uint64) bool` — IsParagraphBoundary reports whether seq ends a complete paragraph.
- `func userEventUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int)`
- `func toolEventUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int, bool)`

### message.go

- `func MessageOriginOf(event TranscriptEvent) MessageOrigin` — MessageOriginOf 把一条 transcript 事件行映射成可见消息的归属（含随行图片）。

### state.go

- `func ExplicitCompactionOrigin(origin string) bool` — ExplicitCompactionOrigin 报告该来源是否为显式要求（用户 /compact 或模型
- `func CompactionRangeLabel(messageFrom, messageTo string, eventFrom, eventTo uint64) string` — CompactionRangeLabel 把压缩区间（消息号 + transcript 事件序号）渲染成一句
- `func messageRangeLabel(from, to string) string` — messageRangeLabel 渲染消息号区间：单号不写 `..`，只有一端就只写一端。
- `func CloneWorkItems(items []WorkItem) []WorkItem` — CloneWorkItems 返回工作表格行的深拷贝（并发读者安全；供快照克隆与事件发布）。
- `func CloneSnapshot(snapshot Snapshot) Snapshot` — CloneSnapshot returns an independent copy suitable for concurrent readers.
- `func CloneSessionForkRef(ref *SessionForkRef) *SessionForkRef` — CloneSessionForkRef 返回会话血缘的独立副本。
- `func CloneRuntimeState(runtime RuntimeState) RuntimeState` — CloneRuntimeState returns an independent copy of mutable runtime slices.
- `func CloneTeamworkBoardView(view *dto.TeamworkBoardView) *dto.TeamworkBoardView` — CloneTeamworkBoardView 深拷贝团队看板投影：切片**与元素内嵌的切片**都要独立。
- `func CloneWorkTableBatches(batches []WorkTableBatch) []WorkTableBatch` — CloneWorkTableBatches 深拷贝批次头（Counts map 必须独立，避免并发读者
- `func cloneSubAgentTree(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode` — cloneSubAgentTree 深拷贝子代理树投影（快照克隆不改旧快照的契约）。
- `func clonePlanNodes(nodes []PlanNode) []PlanNode`

### tool_result_page.go

- `func WireToolResultRef(callID string) string` — WireToolResultRef 由工具调用 ID 构造 wire 结果引用。
- `func WireToolResultCallID(ref string) (string, bool)` — WireToolResultCallID 解析 wire 结果引用；非 wire 形状或空调用 ID 一律返回 false，
- `func NodeResultRef(ref string) (string, bool)` — NodeResultRef parses a `node:<nodeID>:...` subagent result reference.
- `func BuildPage(result StoredToolResult, offset, limit int, contains string) ToolResultPage` — BuildPage applies contains filtering, byte pagination and UTF-8 boundary
- `func EncodePage(result StoredToolResult, offset, limit int, contains string) (string, error)` — EncodePage keeps the model-facing read_tool_result JSON shape.
- `func utf8Start(value string, index int) int`
- `func utf8End(value string, index int) int`

### tool_result_page_test.go

- `func TestWireToolResultRefRoundTripsThroughCallID(t *testing.T)`
- `func TestWireToolResultCallIDRejectsNonWireShape(t *testing.T)`

### toolcall_status_record.go

- `func ToolCallStatusOfRecord(word string) dto.ToolEventStatus` — ToolCallStatusOfRecord 是"工具调用视图词"这一格在**记录/边界那一侧**的唯一转换点：
- `func (c *ToolCall) UnmarshalJSON(data []byte) error` — UnmarshalJSON 是这一格的落盘读法：`status` 按**词**读，经 ToolCallStatusOfRecord 折一次；

### visible_output.go

- `func NewVisibleOutputStream(requestID string) *VisibleOutputStream`
- `func (stream *VisibleOutputStream) RequestID() string` — RequestID 返回该输出流绑定的请求 ID（跨包只读访问）。
- `func (stream *VisibleOutputStream) Text() string` — Text 返回本请求已累积的可见正文（空串 = 还没有可见正文）。
- `func (stream *VisibleOutputStream) Consume(chunk string) string`
- `func (stream *VisibleOutputStream) filter(chunk string) string` — filter 是 think 块过滤本体：返回本分片里真正可见的正文。
- `func StripThoughtBlocks(value string) string`
- `func trailingTagPrefix(value, tag string) string`
