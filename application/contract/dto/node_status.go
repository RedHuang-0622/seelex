package dto

// NodeStatus 是**一个计划节点**的状态（`PlanNode.Status`：GUI/TUI 的 plan 面板与
// 子代理详情的数据源）。
//
// 这一格的两个来源：
//   - **框架词**：Seele workplan 的 `NodeBase.Status`（queued | running | completed |
//     failed | skipped | canceled | aborted | panicked）——框架发什么词，我们折一次
//     （`task_context.NodeStatusOfFramework`，带 ok：认不得的词**不是节点状态**）；
//   - **我们自己的词**：worktree 生命周期（pending | worktree_creating | rebasing |
//     merging）与"启动中"（started → running 的别名）。
//
// 与邻格的边界（都在 `docs/arch/state-machine-inventory.md`）：
//   - **记录状态**（`SubAgentNodeStatus`：queued|running|done|failed|interrupted）是
//     子代理**会话记录**的生命周期，与这一格有映射但取值面不同（done vs completed、
//     interrupted vs failed——详情页按这两种口径各说各的事实）；
//   - **计划状态**（`PlanStatus`）是整张 plan 的生命周期；
//   - `dto.PlanNodeEvent.Status` 是**事件**的原始词（框架词 + 我们的阶段词混在一起），
//     它**不是**这一格：事件流里出现一个不是节点状态的词，不许拿它覆盖节点状态
//     （见 `ApplyPlanNodeProjection` 的现场：worktree 收尾阶段词曾把跑完的节点显成"待开始"）。
type NodeStatus uint8

const (
	// NodeUnknown 是零值：不是"还没开始"，而是"这个词不是节点状态 / 读不懂"。
	NodeUnknown NodeStatus = iota
	// NodePending = 已装载、还没轮到它。
	NodePending
	// NodeQueued = 已排队（框架派活但尚未启动）。
	NodeQueued
	// NodeRunning = 正在跑（框架的 "started" 也折到这里）。
	NodeRunning
	// NodeWorktreeCreating = 子代理节点的 worktree 现场正在创建。
	NodeWorktreeCreating
	// NodeRebasing = 收尾变基中。
	NodeRebasing
	// NodeMerging = 合并回主工作区中。
	NodeMerging
	// NodeCompleted = 跑完且成功。
	NodeCompleted
	// NodeFailed = 跑失败。
	NodeFailed
	// NodeAborted = 被中止（不判失败：批次被叫停）。
	NodeAborted
	// NodeSkipped = 被跳过（计入完成一侧，见 RecalculatePlanProgress）。
	NodeSkipped
	// NodeCanceled = 被取消（fail-fast 连坐）。
	NodeCanceled
	// NodePanicked = 框架内恐慌退出（按失败处理）。
	NodePanicked
)

// nodeStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var nodeStatusWords = [...]string{
	NodeUnknown:          "unknown",
	NodePending:          "pending",
	NodeQueued:           "queued",
	NodeRunning:          "running",
	NodeWorktreeCreating: "worktree_creating",
	NodeRebasing:         "rebasing",
	NodeMerging:          "merging",
	NodeCompleted:        "completed",
	NodeFailed:           "failed",
	NodeAborted:          "aborted",
	NodeSkipped:          "skipped",
	NodeCanceled:         "canceled",
	NodePanicked:         "panicked",
}

var nodeStatusCodec = stateCodec{name: "节点状态", words: nodeStatusWords[:]}

// String 给出对外词。
func (s NodeStatus) String() string { return nodeStatusCodec.word(uint8(s)) }

// ParseNodeStatus 把对外词读回枚举；第二个返回值报告认不认得。**用它判"这个词是不是
// 节点状态"**：认不得就是认不得，不许折成 pending/unknown 之后拿去覆盖已知状态。
func ParseNodeStatus(text string) (NodeStatus, bool) {
	ordinal, ok := nodeStatusCodec.ordinal(text)
	return NodeStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "running" 这样的词。
func (s NodeStatus) MarshalJSON() ([]byte, error) { return nodeStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *NodeStatus) UnmarshalJSON(data []byte) error {
	return nodeStatusCodec.unmarshal(data, (*uint8)(s))
}

// NodeStatusFromFramework 把框架 workplan 的节点状态词折成这一格；第二个返回值报告
// 这个词**是不是节点状态词**。框架的 "started" 与我们的 running 是同一个态（框架的
// 启动词，历史上有两处这么写）。
//
// 放在契约层：折法本身是"词与词的对应"，两处读者（plan 投影 / plan 状态推导）必须同一份。
func NodeStatusFromFramework(word string) (NodeStatus, bool) {
	if word == "started" {
		return NodeRunning, true
	}
	return ParseNodeStatus(word)
}
