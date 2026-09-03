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

视图锁（`Core.ViewMu`，保护 Snapshot 与可见投影）内 bump → 锁外 Publish；
`CollectRuntimeProjection` 锁外收集外部端口，`ApplyRuntimeProjectionLocked`
锁内应用并重建工作表格。会话可见投影的字段写一律经 `View.mu`
（`SessionViewMutateLocked`/`SessionViewReadLocked`；G5 访问器化）：单元
View 指针注册后不再整体替换，冷加载装载在 View.mu 内逐字段拷贝。

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
- `func (c *Coordinator) CollectRuntimeProjection(ctx context.Context) RuntimeStateProjection` — CollectRuntimeProjection 锁外调用外部端口收集当前视图会话的 runtime
- `func (c *Coordinator) CollectRuntimeProjectionFor(ctx context.Context, sessionID string) RuntimeStateProjection` — CollectRuntimeProjectionFor 按显式会话收集 runtime 投影（G1：会话槽的
- `func (c *Coordinator) fullAccessFor(sessionID string) bool` — fullAccessFor 返回指定会话生效的全权模式（G4：会话选择优先；未选择时
- `func (c *Coordinator) replanMetricsFor(sessionID string) dto.ReplanMetrics` — replanMetricsFor 返回指定会话的 replan 统计（per-session 端口优先；
- `func (c *Coordinator) tokenCountFor(sessionID string) string` — tokenCountFor 返回指定会话的 token 计数（有 per-session 端口优先；
- `func (c *Coordinator) activeSkillIDsFor(sessionID string) []string` — activeSkillIDsFor 返回指定会话的任务激活 skill ID（per-session 端口
- `func (c *Coordinator) goalSkillActiveFor(sessionID string) bool` — goalSkillActiveFor 返回指定会话的 goal skill 激活判定。
- `func (c *Coordinator) ApplyRuntimeProjectionLocked(projection RuntimeStateProjection)` — ApplyRuntimeProjectionLocked 应用 runtime 投影到当前活跃会话（保留 Plan/
- `func (c *Coordinator) ApplyRuntimeProjectionForLocked(sessionID string, projection RuntimeStateProjection)` — ApplyRuntimeProjectionForLocked 应用 runtime 投影到指定会话（G1）：
- `func (c *Coordinator) AppendMessageLocked(role, content string, tool *model.ToolCall) *model.Message` — AppendMessageLocked 追加一条可见消息到当前活跃会话（调用方持有
- `func (c *Coordinator) AppendMessageLockedFor(sessionID, role, content string, tool *model.ToolCall) *model.Message` — AppendMessageLockedFor 追加一条可见消息到指定会话（阶段 1：可见对话收进
- `func (c *Coordinator) sessionViewLocked(sessionID string) *session.View` — sessionViewLocked 返回指定会话的可见投影（按需创建会话域单元；调用方持有
- `func (c *Coordinator) SessionViewLocked(sessionID string) *session.View` — SessionViewLocked 返回指定会话的可见投影（core 域恢复/回看路径用；
- `func (c *Coordinator) SessionViewMutateLocked(sessionID string, mutate func(*session.View))` — SessionViewMutateLocked 在指定会话可见投影的 View.mu 内应用变更（G5 访问
- `func (c *Coordinator) SessionViewReadLocked(sessionID string, read func(*session.View))` — SessionViewReadLocked 在指定会话可见投影的 View.mu（读）内读取字段快照
- `func (c *Coordinator) SetSessionViewLocked(sessionID string, view *session.View)` — SetSessionViewLocked 装载指定会话的可见投影（冷加载/恢复路径；调用方
- `func (c *Coordinator) SetSessionChatLockedFor(sessionID string, chat model.ChatState)` — SetSessionChatLockedFor 写指定会话的聊天运行态投影（调用方持有
- `func (c *Coordinator) SetReadFilesFor(sessionID string, readFiles []model.ReadFileRef)` — SetReadFilesFor 写指定会话的 read 文件引用投影（调用方持有 Core.ViewMu）。
- `func (c *Coordinator) mirrorActiveViewLocked(sessionID string, view *session.View)` — mirrorActiveViewLocked 把指定会话的 scope 镜像到 Snapshot（仅当目标为
- `func (c *Coordinator) MirrorActiveViewLocked()` — MirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot（切换/恢复
- `func (c *Coordinator) AdvanceMessageSeqLocked(messages []model.Message)` — AdvanceMessageSeqLocked 按既有消息 ID 推进消息序列（会话恢复路径）。
- `func (c *Coordinator) NextMessageSeqLocked() uint64` — NextMessageSeqLocked 返回下一条消息序号并推进（分页加载 ID 分配用）。
- `func (c *Coordinator) boundViewTailLocked(view *session.View)`
- `func durableConversationCount(messages []model.Message) int`
- `func BoundConversationTail(messages []model.Message, window int) []model.Message` — BoundConversationTail 保留尾部窗口（system 与普通消息分列计数）。
- `func BoundConversationHead(messages []model.Message, window int) []model.Message` — BoundConversationHead 保留头部窗口（分页加载前置用）。
- `func (c *Coordinator) BumpLocked() uint64` — BumpLocked 递增快照修订号。
- `func (c *Coordinator) AddNotice(notice string)` — AddNotice 追加一条系统通知并发布事件。
- `func (c *Coordinator) ResetConversation(notice string)` — ResetConversation 清空可见会话并注入 CLI 标识与通知。

