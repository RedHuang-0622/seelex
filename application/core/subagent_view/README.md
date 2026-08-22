# subagent_view

## 生态位

子代理投影面：详情（截断会话 + 上下文快照 + worktree 现场）、live 流
透传、树/节点事件投影与有界工具事件增量。

## 职责与非职责

- 做：`SubagentDetail`、`SubscribeSubagentLive`、`HandleSubagentToolEvent`、
  `FindPlanNodeByID`/`ClonePlanNode`/`SubagentChangedPayload`。
- 不做：fork/merge-back 执行（seelebridge 负责）。

## 依赖方向

依赖 `state.Core` + `contract.ChatEngine` Node 查询面（只读子代理 actor）+
`ViewPort`（bump）+ `Limits`。

## 并发/安全语义

锁内只改权威 Plan 节点投影并 bump；引擎 Node 查询在锁外（只读 actor，
安全）。工具事件与节点时间线有界（`plan_node_events`/`evidence_chars`）。

## 扩展与 Review

新增子代理数据面放本包。Review 重点：不触碰子代理执行锁、事件有界、投影
深拷贝不共享引擎数据。

## 测试

```text
go test ./application/core/subagent_view -count=1
```

根包 `subagent_detail_test.go`/`subagent_tree_test.go` 覆盖集成。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### adapt_test.go

- `func testCoordinator() *Coordinator`
- `func TestAdaptSubagentConversation(t *testing.T)` — TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
- `func TestAdaptSubagentContext(t *testing.T)` — TestAdaptSubagentContext 验证上下文快照适配：截断、条目上限、空快照 → nil。
- `func strPtr(value string) *string`

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造 subagent_view 协调器。
- `func (c *Coordinator) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)` — SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 只读通道 +
- `func (c *Coordinator) HandleSubagentToolEvent(e seelsession.SubagentToolEvent)` — HandleSubagentToolEvent 把 Runtime 工具分发投影进有界权威 Plan 节点快照
- `func (c *Coordinator) upsertSubagentToolEvent(node *model.PlanNode, e model.SubagentToolEvent)`
- `func (c *Coordinator) truncateSubagentEvidence(value string) string`
- `func FindPlanNodeByID(nodes []model.PlanNode, nodeID string) *model.PlanNode` — FindPlanNodeByID 递归查找 Plan 节点（含子节点）。
- `func ClonePlanNode(node model.PlanNode) model.PlanNode` — ClonePlanNode 深拷贝单个 Plan 节点（事件投影发布用）。
- `func SubagentChangedPayload(plan *model.PlanState, planID, runID string, node model.PlanNode) model.SubagentEvent` — SubagentChangedPayload 组装子代理变更事件负载。
- `func (c *Coordinator) SubagentDetail(nodeID string) (*model.SubagentDetail, error)` — SubagentDetail 返回节点子代理详情（截断会话 + 上下文快照 + worktree 现场；
- `func (c *Coordinator) nodeWorktreeInfo(nodeID string) *model.SubagentWorktreeInfo`
- `func (c *Coordinator) adaptSubagentContext(snap *snapshot.ContextSnapshot) *model.SubagentContext`
- `func isRunningSubagentStatus(status model.NodeStatus) bool`
- `func (c *Coordinator) adaptSubagentConversation(messages []types.Message) []model.Message`

