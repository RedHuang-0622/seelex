# view_state

## 生态位

用户可见 Snapshot 读写与事件发布：`SnapshotView`/`Subscribe`/
`AppendMessageLocked`/`BumpLocked`/`ApplyRuntimeProjectionLocked`/
`CollectRuntimeProjection`/`AddNotice`/`ResetConversation`；消息序列与
revision 在此自持。

## 职责与非职责

- 做：消息追加与窗口裁剪、runtime 投影收集/应用、revision bump。
- 不做：工作表格构建（根包 `work_table.go`，经端口注入）、chat 流式。

## 依赖方向

依赖 `state.Core` + 注入端口（`CurrentEffort`、`RefreshWorkTableLocked`、
`Limits`）。

## 并发/安全语义

锁内 bump → 锁外 Publish；`CollectRuntimeProjection` 锁外收集外部端口，
`ApplyRuntimeProjectionLocked` 锁内应用并重建工作表格。

## 扩展与 Review

新增 Snapshot 派生面放本包。Review 重点：revision 单调、窗口裁剪不丢
system 引导消息、投影应用不覆盖 Plan/Account 指针。

## 测试

根包 `service_snapshot_test.go` 等覆盖。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造 view 域协调器。
- `func (c *Coordinator) SnapshotView() model.Snapshot` — SnapshotView 返回权威快照深拷贝。
- `func (c *Coordinator) Subscribe(buffer int) event.Subscription` — Subscribe 订阅事件流。
- `func (c *Coordinator) CollectRuntimeProjection(ctx context.Context) RuntimeStateProjection` — CollectRuntimeProjection 锁外调用外部端口收集 runtime 投影。
- `func (c *Coordinator) ApplyRuntimeProjectionLocked(projection RuntimeStateProjection)` — ApplyRuntimeProjectionLocked 应用 runtime 投影（保留 Plan/Account 指针，
- `func (c *Coordinator) AppendMessageLocked(role, content string, tool *model.ToolCall) *model.Message` — AppendMessageLocked 追加一条可见消息（返回快照内引用；调用方持有
- `func (c *Coordinator) AdvanceMessageSeqLocked(messages []model.Message)` — AdvanceMessageSeqLocked 按既有消息 ID 推进消息序列（会话恢复路径）。
- `func (c *Coordinator) NextMessageSeqLocked() uint64` — NextMessageSeqLocked 返回下一条消息序号并推进（分页加载 ID 分配用）。
- `func (c *Coordinator) boundConversationTailLocked()`
- `func durableConversationCount(messages []model.Message) int`
- `func BoundConversationTail(messages []model.Message, window int) []model.Message` — BoundConversationTail 保留尾部窗口（system 与普通消息分列计数）。
- `func BoundConversationHead(messages []model.Message, window int) []model.Message` — BoundConversationHead 保留头部窗口（分页加载前置用）。
- `func (c *Coordinator) BumpLocked() uint64` — BumpLocked 递增快照修订号。
- `func (c *Coordinator) AddNotice(notice string)` — AddNotice 追加一条系统通知并发布事件。
- `func (c *Coordinator) ResetConversation(notice string)` — ResetConversation 清空可见会话并注入 CLI 标识与通知。

