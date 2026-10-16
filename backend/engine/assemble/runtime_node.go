package assemble

// 这是子代理树与节点只读面的宿主转发面（R11-S4，契约 §2.3）。
// 状态（节点注册表/会话、stage 日志、语义结果、worktree 管理器）都在
// `Runtime` 自己持有的组件上，本层只是公开面；内容从 `ports.go` 原样搬来。

import (
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/backend/tool/worktree"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// mainAgentNodeID 是子代理树的合成根节点 ID（R11-S6 从 `ports.go` 搬来：随 `ports.go`
// 归零，它跟着子代理树的宿主面落在这里）。
const mainAgentNodeID = model.MainAgentNodeID

// ── 子代理树端口 ──────────────────────────────────────────────────

// ClearSubagentTree 清空子代理树（GUI"清空"按钮入口）。
func (r *Runtime) ClearSubagentTree() error {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Clear()
}

// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP 消费者）。
func (r *Runtime) SubagentTreeEvents() <-chan struct{} {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Events()
}

// SubAgentTree 返回子代理树的只读投影（根 = 主代理）。
func (r *Runtime) SubAgentTree() []dto.SubAgentTreeNode {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Projection()
}

// SetSubagentToolCallback 注入子代理工具活动观察者（委托 session.ToolEventState）。
func (r *Runtime) SetSubagentToolCallback(callback func(subagentsession.SubagentToolEvent)) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.SetCallback(callback)
}

// ── 节点端口 ──────────────────────────────────────────────────────

// NodeSessionConversation 返回节点子代理的会话记录：运行中 → 子会话
// History（实时）；已结束 → 最后快照。只读子代理 actor，绝不触碰主会话。
func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.Conversation(nodeID)
}

// NodeContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗"上下文"标签）。
func (r *Runtime) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.ContextSnapshot(nodeID)
}

// SetSkillRegistry 装配子代理 skill 目录 actor（skill.Registry 自带锁；
// 传 nil 关闭 skill 块，降级）。
func (r *Runtime) SetSkillRegistry(registry *skill.Registry) {
	if r == nil {
		return
	}
	// 两个读面同一份事实：子代理的 skill 块（node.SetSkills）与员工回合的技能
	// 目录（按会话装配的集合，见 runtime_role_plugins.go）。先存再判 node：
	// 没装配 node 时目录读面照样生效（角色回合不经过 node）。
	r.skills.Store(registry)
	if r.node == nil {
		return
	}
	r.node.SetSkills(registry)
}

// NodeToolResult 读回节点子代理的工具结果原始内容（ref 必须带
// node:<nodeID>: 前缀）。只读节点归档器，安全。
func (r *Runtime) NodeToolResult(nodeID, ref string) (string, bool) {
	if r == nil || r.node == nil {
		return "", false
	}
	return r.node.ToolResult(nodeID, ref)
}

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）；
// 类型本体在 backend/tool/worktree 域。
type NodeWorktreeInfo = worktree.NodeWorktreeInfo

// NodeWorktreeInfoFor 返回节点 worktree 现场信息（无现场 → false）。
func (r *Runtime) NodeWorktreeInfoFor(nodeID string) (NodeWorktreeInfo, bool) {
	if r == nil || r.worktreeMgr == nil || nodeID == "" {
		return NodeWorktreeInfo{}, false
	}
	return r.worktreeMgr.Info(nodeID)
}

// NodeStageLogs 返回 node 第一视角分阶段上下文日志（同一 subagent 会话的
// 认证面：全部阶段日志共享 SessionID）。
func (r *Runtime) NodeStageLogs(nodeID string) []model.NodeStageLog {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return r.node.StageLogs(nodeID)
}

// NodeStageEvents 返回第一视角阶段日志的实时推送通道（即时输出面）：
// 每个阶段（spawn/turn/tool/result）被记录后立即投递，消费方按 NodeID 过滤。
func (r *Runtime) NodeStageEvents() <-chan model.NodeStageLog {
	if r == nil || r.node == nil {
		return nil
	}
	return r.node.StageEvents()
}

// NodeFirstPersonView 返回 node 第一视角完整载荷：查看时间（ProbedAt）+
// 逐步产出的分阶段日志 + 语义结果。日志按记录序单调递增且早于 ProbedAt。
func (r *Runtime) NodeFirstPersonView(nodeID string) *model.NodeFirstPersonView {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return &model.NodeFirstPersonView{
		NodeID:   nodeID,
		ProbedAt: time.Now(),
		Stages:   r.node.StageLogs(nodeID),
		Result:   r.node.SemanticResult(nodeID),
	}
}

// NodeSemanticResult 返回 node 的预定义语义结果（只读；对象结构由 seelex
// 制定，非 subagent 自拟）。
func (r *Runtime) NodeSemanticResult(nodeID string) *model.NodeSemanticResult {
	if r == nil || r.node == nil || nodeID == "" {
		return nil
	}
	return r.node.SemanticResult(nodeID)
}

// DrainSubagentSemanticResults 取空子代理语义结果队列（消息队列消费面：
// mainagent / plan 下游 node 读取）。
func (r *Runtime) DrainSubagentSemanticResults() []*model.NodeSemanticResult {
	if r == nil || r.node == nil {
		return nil
	}
	return r.node.DrainSemanticResults()
}
