package model

import (
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// Node 第一视角 / 语义结果返回的数据契约（叶子类型，node/session/telemetry
// 域共用，避免跨域依赖）。
//
// 语义结果返回原则：返回对象是**事先制定好**的结构（NodeSemanticResult），
// 由 seelex 从子代理会话快照与执行状态提取填充，不是 subagent 自拟的自由文本；
// subagent 的最终文本只进 Output/Summary 字段。

const (
	// NodeSemanticSchemaVersion 是语义结果对象的 schema 版本（变更时递增）。
	NodeSemanticSchemaVersion = 1
)

// 第一视角分阶段日志的阶段名。
const (
	NodeStageSpawn   = "spawn"   // 子代理会话建立、继承上下文（goal/父证据/预算）
	NodeStageTurn    = "turn"    // 每次 LLM 调用边界（同会话内按序递增）
	NodeStageTool    = "tool"    // 工具执行边界
	NodeStageResult  = "result"  // 结果返回（merge-back / 语义结果）
	NodeStageStopped = "stopped" // 被打断/叫停（预留）
)

// NodeStageLog 是同一 node 会话在某个阶段的上下文日志（第一视角数据面）。
// 同一子代理的多个阶段日志必须携带相同 SessionID——这是"分阶段上下文出自
// 同一个 subagent 而非多个 subagent 拼凑"的认证依据。
// 结构体本体以 application/contract/dto 为单源（跨层投影与对外契约同一形状），
// 本包用别名保持运行态引用面兼容。
type NodeStageLog = dto.NodeStageLog

// SemanticDecision 是语义结果中的关键决策（与 ContextSnapshot.Decision 同构，
// 叶子化避免跨包引用）。
type SemanticDecision struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

// NodeWorktreeSummary 是节点 worktree 现场的叶子摘要。
type NodeWorktreeSummary struct {
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
}

// NodeSemanticResult 是子代理返回的预定义语义结果（抽象，非 subagent 自拟）。
// 经 SubagentSessions actor 的语义结果队列返回（消息队列路径），可被
// mainagent 或 plan 中下一个依赖 node 消费。
type NodeSemanticResult struct {
	SchemaVersion int                  `json:"schema_version"`
	NodeID        string               `json:"node_id"`
	SessionID     string               `json:"session_id"`
	Status        string               `json:"status"`
	Goal          string               `json:"goal,omitempty"`
	Summary       string               `json:"summary,omitempty"`
	Output        string               `json:"output,omitempty"`
	Findings      []string             `json:"findings,omitempty"`
	Decisions     []SemanticDecision   `json:"decisions,omitempty"`
	Constraints   []string             `json:"constraints,omitempty"`
	PendingWork   []string             `json:"pending_work,omitempty"`
	TokenEstimate int                  `json:"token_estimate,omitempty"`
	Worktree      *NodeWorktreeSummary `json:"worktree,omitempty"`
	Stages        []NodeStageLog       `json:"stages,omitempty"`
}

// NodeFirstPersonView 是"查看 node 第一视角"的完整载荷：ProbedAt 记录查看动作
// 发生的时间；Stages 是按记录序逐步产出的分阶段上下文日志（At 单调递增，
// 全部早于 ProbedAt——先产出、后被查看）。
type NodeFirstPersonView struct {
	NodeID   string              `json:"node_id"`
	ProbedAt time.Time           `json:"probed_at"`
	Stages   []NodeStageLog      `json:"stages"`
	Result   *NodeSemanticResult `json:"result,omitempty"`
}
