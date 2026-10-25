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

### state.go

- `func ExplicitCompactionOrigin(origin string) bool` — 判定压缩来源是否为显式请求。
- `func CompactionRangeLabel(messageFrom, messageTo string, eventFrom, eventTo uint64) string` — 渲染压缩区间。
- `func CloneWorkItems(items []WorkItem) []WorkItem` — 深拷贝工作表格行。
- `func CloneSnapshot(snapshot Snapshot) Snapshot` — 深拷贝完整 Snapshot。
- `func CloneSessionForkRef(ref *SessionForkRef) *SessionForkRef` — 深拷贝会话血缘。
- `func CloneRuntimeState(runtime RuntimeState) RuntimeState` — 深拷贝 Runtime 投影。
- `func CloneTeamworkBoardView(view *dto.TeamworkBoardView) *dto.TeamworkBoardView` — 深拷贝团队看板。
- `func CloneWorkTableBatches(batches []WorkTableBatch) []WorkTableBatch` — 深拷贝批次头。

### context.go

- 本文件只声明 Task context、transcript、token audit 与 active skill 值类型。

### fork.go

- 本文件只声明 fork 请求、结果、切点与会话血缘值类型。

### session_input_index.go

- 本文件只声明会话输入索引、窗口与行投影。

### toolcall_status_record.go

- `func ToolCallStatusOfRecord(word string) dto.ToolEventStatus` — 把存档词折成工具事件枚举。
- `func (c *ToolCall) UnmarshalJSON(data []byte) error` — 按存兼容词表读取工具调用。

### visible_output.go

- `func NewVisibleOutputStream(requestID string) *VisibleOutputStream` — 构造按请求累积的可见输出流。
- `func StripThoughtBlocks(value string) string` — 从完整文本中剥离 think 块。
