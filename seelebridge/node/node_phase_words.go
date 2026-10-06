package node

// node_phase_words.go — 节点事件流里**不是节点状态**的那两条阶段词（本包唯一一处字面量）。
//
// 它们与节点状态走**同一个事件字段**（`AppendNodePhase` 的 status → `dto.PlanNodeEvent.Status`），
// 但语义是"worktree 收尾怎么落的"，不是"节点现在处在哪一步"：
//
//   - worktree_unmerged：子代理在自己 worktree 里留了未提交改动（收尾协议没执行完）；
//   - merge_blocked：合并被主工作区的在途改动挡住。
//
// 两者都**不判节点失败**（现场保留 + 产出末尾加显式警告，节点按 Chat 结果算成功），
// 所以读方不许拿这两个词去覆盖节点状态——历史上映射的 default 把它们折成了 pending，
// 于是一个跑完并交付产出的节点在收尾警告之后被显成"待开始"、还被从计划进度里扣掉
// （判据见 application/core 的 ApplyPlanNodeProjection 与 plan_node_phase_word_test.go）。
//
// 分工：契约（`dto.NodeStatus`）登记的是**节点状态**词，这两条阶段词登记在这里，不进契约。
const (
	nodePhaseWorktreeUnmerged = "worktree_unmerged"
	nodePhaseMergeBlocked     = "merge_blocked"
)
