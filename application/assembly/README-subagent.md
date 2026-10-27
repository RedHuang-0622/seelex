# assembly/subagent（根包分卷）

## 生态位

子代理投影集成测试

覆盖：`subagent*.go`；未归属文件由覆盖自检拦下。

## Subagent Application 用例（AP-016 首个切片）

`Subagent view application` 集中恢复目录、中断恢复、定点恢复、fork 派发、
树清理和详情读取。Runtime 继续提供执行/恢复能力，subagent view 继续负责详情投影；
Service 对应入口只保留一行转发。

## 子代理工具记录的归属（阶梯提交第一级）

子代理的每次工具调用都要记一笔读数，但**归属不在视图上**（2026-10-19 方向 C）：

- 事件入口是 `HandleSubagentToolEventFor(ownerSessionID, event)`：owner 由装配层从
  **执行上下文**解析（`backend/engine/assemble` 的 `SubagentToolObservation`，
  取节点注册时随 ctx 绑定的主会话）；`HandleSubagentToolEvent(event)` 是兼容面，
  owner 为空 ⇒ 读数丢弃并计数，**不拿视图会话顶替**（切会话串写就是这么来的，#8/#13）。
- **记账先发生**：无 `NodeID` 的事件不再整条丢——单元键退到 `subagent:<event.ID>`
  （病灶 #9）；`Plan` 投影的早退分支在记账之后，只表示"这条消息不进投影"。
  回归用例：`record_attribution_test.go`（归属来自入参、记账先于投影早退、兼容面不编造归属）。
- 读数先落进该节点自己的**暂存**（`toolrecord.Stage`，本包只报"哪个单元、谁、什么
  状态、错误原文"，判据与合并都在 `application/assembly`），节点进入终态时收口
  （`CloseToolRecordUnit` → `application/assembly.CloseSubagentToolRecordUnit`）。
  落盘前 `FlushToolRecord` 还会抽干未收口的暂存，故读数不会丢在叶子这一级。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### subagent_detail_lock_regression_test.go

- `func (engine *lockOrderEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func TestSubagentDetailNoViewMuSessionLockInversion(t *testing.T)` — TestSubagentDetailNoViewMuSessionLockInversion 复现 2026-09-08 死锁链路：

### subagent_detail_test.go

- `func TestSubagentSessionDetailMissingNode(t *testing.T)` — TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesContext(t *testing.T)` — TestAdaptSubagentContext 验证上下文快照适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesWorktree(t *testing.T)` — TestSubagentSessionDetailCarriesWorktree 验证失败现场恢复入口：节点
- `func TestScheduledTasksProjectIntoRuntimeSnapshot(t *testing.T)` — TestScheduledTasksProjectIntoRuntimeSnapshot 验证周期任务与白名单命令经
- `func TestTodoItemsProjectIntoRuntimeSnapshot(t *testing.T)` — TestTodoItemsProjectIntoRuntimeSnapshot 验证 todolist 清单经运行时投影
- `func strPtr(value string) *string`

### subagent_envelope_store_test.go

- `func TestInheritedContextEnvelopeNotEnteringContextStores(t *testing.T)` — TestInheritedContextEnvelopeNotEnteringContextStores 复现：子代理 merge-back

### subagent_evidence_test.go

- `func TestSubagentMailboxContentDoesNotReachFrontendOrStorage(t *testing.T)`

### subagent_tree_test.go

- `func TestCollectRuntimeProjectionCarriesSubAgentTree(t *testing.T)` — TestCollectRuntimeProjectionCarriesSubAgentTree 验证权威 Snapshot 投影
