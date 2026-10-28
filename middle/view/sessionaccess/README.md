# sessionaccess

## 生态位

`middle/view/sessionaccess` 定义 mainagent、subagent、teammate 共用的
`ConversationSession` 能力面。调用方是 Application/装配层以及只读详情页；
它不拥有任何一类会话的生命周期。

## 职责与非职责

职责：

- 固定三类窗口：main Session、subagent Session、teammate WorkItem Session；
- 统一 `View` / `Chat` / `Recover` 接口；
- 统一记录种类、分页、UTF-8 安全裁剪与恢复状态词；
- 强制 teammate 窗口必须携带 WorkItem。

非职责：

- 不管理主会话目录、切换、审批或任务；
- 不管理子代理 fork 树与 merge-back；
- 不管理 team plan、角色在编、WorkItem 验收或 worktree；
- 不直接访问 Runtime、store 或 jobs。

## 核心实现

`Registry.Session` 只做窗口解析与必填校验。`PaginateRecords` 是分页和裁剪的
唯一实现：先按记录种类过滤，再应用 offset/limit，最后逐条按 rune 裁剪。
`Total` 表示过滤后的总数，`HasMore` 与 `Truncated` 分别回答“后面是否还有一页”
和“当前结果是否不是完整原文”。

`Recover` 只解析 `live → cold → interrupted → missing`；真正的续跑由各适配器在
冷 Chat 时注入该 Session 自己的 durable snapshot，再从同一 Session 继续，
不会把恢复历史写进主会话。

## 依赖方向

本包只依赖 `contract/dto` 与标准库。适配器由各领域实现，并在装配层注册。

## Review 指南

- 是否出现第二份分页或 UTF-8 裁剪；
- teammate 是否绕过 WorkItem 开长期角色私聊；
- `missing` 是否被伪装成空会话；
- 是否把管理生命周期塞进本接口。

## 测试与验证

```text
go test ./middle/view/sessionaccess -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### session.go

- `func NewRegistry() *Registry` — NewRegistry creates an empty adapter registry.
- `func (registry *Registry) Register(kind dto.ConversationKind, adapter ConversationSession)` — Register installs one adapter. Nil adapters remove the kind.
- `func (registry *Registry) Session(ref dto.ConversationSessionRef) (ConversationSession, error)` — Session resolves and validates one fixed conversation window.
- `func PaginateRecords(sessionID string, records []dto.ConversationRecord, request dto.ConversationViewRequest) dto.SessionRecordPage` — PaginateRecords is the single record filtering, clipping, and pagination
- `func clipRunes(text string, limit int) (string, bool)`

### session_test.go

- `func (s stubAdapter) View(context.Context, dto.ConversationSessionRef, dto.ConversationViewRequest) (dto.SessionRecordPage, error)`
- `func (s stubAdapter) Chat(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (s stubAdapter) Recover(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error)`
- `func TestRegistryRequiresWorkItemForTeammateWindow(t *testing.T)`
- `func TestSessionRecordsUseOnePaginationAndUTF8ClipRule(t *testing.T)`
- `func TestRecoveryStateVocabularyIsStable(t *testing.T)`
