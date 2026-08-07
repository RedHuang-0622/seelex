package seelebridge

import (
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── 子代理树（fork 内存态可视化数据面）────────────────────────────────
// fork_subagents 创建子代理时记录 parent/child 链（父节点 ID → 子节点列表），
// 节点状态（running/done/failed）、goal、紧凑 ContextSnapshot、会话摘要挂在
// 节点上；整棵树经 Runtime.SubAgentTree() 投影为只读 DTO 供 GUI 树视图渲染。
//
// 完成即清走（2026-08-07 用户约定）：done 节点在 completeSubagentNode 成功
// 路径立即从树中移除——工作区树只保留运行中/失败的节点，完成的子代理不
// 占位。详情数据面（nodeSessions 注册表：会话记录/上下文快照/工具结果）
// 独立保留，Plan 节点与详情弹窗仍可查历史。
//
// 明确不落盘：树随进程生命周期存在，会话恢复/重启后为空（与 nodeSessions
// 注册表同构，见 agent_node.go）。ContextSnapshot 以紧凑投影挂在节点上
// （运行中实时导出、结束后快照，截断有界）；完整快照仍经既有
// Runtime.NodeContextSnapshot(nodeID) 数据面按需读取。

// SubAgentNodeStatus 是子代理树节点的生命周期状态（树投影专用；
// PlanNode 的细粒度状态仍由 PlanNodeStatus 表达）。
type SubAgentNodeStatus string

const (
	SubAgentRunning SubAgentNodeStatus = "running"
	SubAgentDone    SubAgentNodeStatus = "done"
	SubAgentFailed  SubAgentNodeStatus = "failed"
)

// mainAgentNodeID 是子代理树的合成根节点 ID（主代理；不是真实子代理）。
const mainAgentNodeID = "main"

// SubAgentTreeNode 是子代理树的只读投影节点（GUI 树视图数据源）：
//   - Goal：fork 时传入的子代理目标；
//   - Status：running/done/failed（终态由节点 Run 结果投影）；
//   - Summary：节点最终输出（会话摘要）；
//   - SessionID：子会话 ID（详情弹窗溯源用）。
type SubAgentTreeNode struct {
	ID        string               `json:"id"`
	ParentID  string               `json:"parent_id,omitempty"`
	Goal      string               `json:"goal,omitempty"`
	Status    SubAgentNodeStatus   `json:"status"`
	Summary   string               `json:"summary,omitempty"`
	Error     string               `json:"error,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
	StartedAt time.Time            `json:"started_at,omitempty"`
	EndedAt   time.Time            `json:"ended_at,omitempty"`
	Context   *SubAgentNodeContext `json:"context,omitempty"`
	Children  []SubAgentTreeNode   `json:"children,omitempty"`
}

// SubAgentNodeContext 是树节点的紧凑上下文（ContextSnapshot 的有界投影）：
// 运行中实时导出、结束后快照；只含公开证据（Goal/Progress/MessageCount/
// TokenEstimate/Findings），单条截断到 subagentTreeContextLimit。
type SubAgentNodeContext struct {
	Goal          string   `json:"goal,omitempty"`
	Progress      string   `json:"progress,omitempty"`
	MessageCount  int      `json:"message_count"`
	TokenEstimate int      `json:"token_estimate,omitempty"`
	Findings      []string `json:"findings,omitempty"`
}

// subagentTreeContextLimit 是树投影单条上下文文本截断长度（树保持轻量）。
const subagentTreeContextLimit = 120

// subagentTreeContextFindings 是树投影 findings 条目上限。
const subagentTreeContextFindings = 3

// subagentNodeRecord 是树节点的内存态记录（含运行中会话引用；只存引用不读
// 内容，详情读取走 NodeSessionConversation，遵守"只读子代理 actor"约束）。
type subagentNodeRecord struct {
	id          string
	parentID    string
	goal        string
	status      SubAgentNodeStatus
	summary     string
	errorMsg    string
	sessionID   string
	session     *session.Session          // 运行中会话（实时上下文导出）
	contextSnap *snapshot.ContextSnapshot // 结束后快照（unregisterNodeSession 写入）
	startedAt   time.Time
	endedAt     time.Time
}

// subagentTreeState 是子代理树注册表（Runtime 自有锁，与 nodeSessions 同构）。
type subagentTreeState struct {
	mu       sync.Mutex
	nodes    map[string]*subagentNodeRecord
	children map[string][]string // parentID → 有序子节点列表
}

func newSubagentTreeState() *subagentTreeState {
	return &subagentTreeState{
		nodes:    make(map[string]*subagentNodeRecord),
		children: make(map[string][]string),
	}
}

// registerFork 记录一次 fork_subagents：parentID 下挂 N 个子代理节点
// （状态 running、goal 来自 spec）。幂等：同 id 重复 fork 覆盖旧记录。
func (s *subagentTreeState) registerFork(parentID string, specs []forkSubagentSpec) {
	if s == nil || len(specs) == 0 {
		return
	}
	if parentID == "" {
		parentID = mainAgentNodeID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, spec := range specs {
		s.nodes[spec.ID] = &subagentNodeRecord{
			id: spec.ID, parentID: parentID, goal: spec.Goal,
			status: SubAgentRunning, startedAt: time.Now(),
		}
		s.children[parentID] = append(s.children[parentID], spec.ID)
	}
}

// noteSession 挂载运行中子会话（registerNodeSession 调用；幂等）。
// 非 fork 节点（plan_run 的 agent 节点未入树）直接忽略。
// 会话引用用于运行中实时导出紧凑上下文（只读子代理 actor，安全）。
func (s *subagentTreeState) noteSession(nodeID string, sess *session.Session) {
	if s == nil || nodeID == "" || sess == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.nodes[nodeID]
	if record == nil {
		return
	}
	record.sessionID = sess.SessionID()
	record.session = sess
	if record.startedAt.IsZero() {
		record.startedAt = time.Now()
	}
}

// noteSnapshot 挂载结束时导出的上下文快照（unregisterNodeSession 调用；
// 幂等，之后投影直接复用，无需再次导出）。
func (s *subagentTreeState) noteSnapshot(nodeID string, snap *snapshot.ContextSnapshot) {
	if s == nil || nodeID == "" || snap == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.nodes[nodeID]
	if record == nil {
		return
	}
	record.contextSnap = snap
}

// completeSubagentNode 写入节点终态（SeelexAgentNode.Run 结束路径调用）：
// 成功 → 记录摘要/结束时间后**立即从树中移除**（"跑完就清走"——完成的
// 子代理不占位；详情数据面保留在 nodeSessions 注册表，Plan/详情弹窗仍可
// 查历史）；失败 → failed + 错误（保留现场供排查）。非 fork 节点 no-op。
func (s *subagentTreeState) completeSubagentNode(nodeID, summary string, runErr error) {
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.nodes[nodeID]
	if record == nil {
		return
	}
	record.summary = summary
	record.endedAt = time.Now()
	if runErr != nil {
		record.status = SubAgentFailed
		record.errorMsg = runErr.Error()
		return
	}
	s.removeNodeLocked(nodeID)
}

// removeNodeLocked 从注册表移除节点（调用方持锁）：删除节点记录与其子
// 节点列表，并从父节点 children 列表摘除。done 节点完成即清走。
func (s *subagentTreeState) removeNodeLocked(nodeID string) {
	record := s.nodes[nodeID]
	if record == nil {
		return
	}
	parent := record.parentID
	delete(s.nodes, nodeID)
	delete(s.children, nodeID)
	if parent == "" {
		return
	}
	list := s.children[parent]
	for index, childID := range list {
		if childID == nodeID {
			s.children[parent] = append(list[:index], list[index+1:]...)
			return
		}
	}
}

// clear 清空整棵树（GUI「清空」入口：一次移除全部节点，含失败节点与
// 嵌套层级；详情数据面在 nodeSessions 注册表，不受影响）。
func (s *subagentTreeState) clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes = make(map[string]*subagentNodeRecord)
	s.children = make(map[string][]string)
	return nil
}

// ClearSubagentTree 清空子代理树（GUI「清空」按钮入口）。失败节点（树里
// 唯一会长期驻留的节点）由用户显式清走；详情数据面不受影响。
func (r *Runtime) ClearSubagentTree() error {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.clear()
}

// SubAgentTree 返回子代理树的只读投影（根 = 主代理；含全部层级子节点）。
// 纯内存态投影：每次调用重建（深拷贝语义），不持有内部指针。
// 紧凑上下文：结束后节点复用 unregisterNodeSession 导出的快照（零额外导出）；
// 运行中节点经 ExportSnapshot 实时导出（与详情弹窗同一数据面，只读子代理
// actor，安全）。
func (r *Runtime) SubAgentTree() []SubAgentTreeNode {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.projection(func(record *subagentNodeRecord) *SubAgentNodeContext {
		if record.contextSnap != nil {
			return compactSubAgentContext(record.contextSnap)
		}
		if record.session != nil {
			if snap := seelexctx.ExportSnapshot(record.session, r.Tracer(), record.goal); snap != nil {
				return compactSubAgentContext(snap)
			}
		}
		return nil
	})
}

// projection 组装树投影：主代理为合成根；孤儿节点（父已不在注册表）归到
// 主代理下，树保持完整。空树（无 fork）返回 nil。
// export 把节点记录投影为紧凑上下文（nil = 节点无上下文）。
func (s *subagentTreeState) projection(export func(*subagentNodeRecord) *SubAgentNodeContext) []SubAgentTreeNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	mainChildren := append([]string(nil), s.children[mainAgentNodeID]...)
	for id, record := range s.nodes {
		if record.parentID == mainAgentNodeID {
			continue
		}
		if _, ok := s.nodes[record.parentID]; !ok {
			mainChildren = append(mainChildren, id)
		}
	}
	if len(mainChildren) == 0 {
		return nil
	}
	root := SubAgentTreeNode{ID: mainAgentNodeID, Status: SubAgentRunning}
	for _, childID := range mainChildren {
		root.Children = append(root.Children, s.projectNode(childID, make(map[string]bool), export))
	}
	root.Status = subagentMainStatus(root.Children)
	return []SubAgentTreeNode{root}
}

// projectNode 递归投影单个树节点（visited 防环，防御 fork DAG 无环的意外）。
func (s *subagentTreeState) projectNode(id string, seen map[string]bool, export func(*subagentNodeRecord) *SubAgentNodeContext) SubAgentTreeNode {
	record := s.nodes[id]
	node := SubAgentTreeNode{ID: id}
	if record == nil {
		return node
	}
	node.ParentID = record.parentID
	node.Goal = record.goal
	node.Status = record.status
	node.Summary = record.summary
	node.Error = record.errorMsg
	node.SessionID = record.sessionID
	node.StartedAt = record.startedAt
	node.EndedAt = record.endedAt
	if context := export(record); context != nil {
		node.Context = context
	}
	if seen[id] {
		return node
	}
	seen[id] = true
	for _, childID := range s.children[id] {
		node.Children = append(node.Children, s.projectNode(childID, seen, export))
	}
	delete(seen, id)
	return node
}

// compactSubAgentContext 把完整上下文快照压成树投影的紧凑 DTO
// （单条截断 + findings 上限，保持树投影轻量）。
func compactSubAgentContext(snap *snapshot.ContextSnapshot) *SubAgentNodeContext {
	if snap == nil {
		return nil
	}
	truncate := func(value string) string {
		if len(value) > subagentTreeContextLimit {
			return value[:subagentTreeContextLimit] + "…"
		}
		return value
	}
	context := &SubAgentNodeContext{
		Goal:          truncate(snap.Goal),
		Progress:      truncate(snap.Progress),
		MessageCount:  snap.MessageCount,
		TokenEstimate: snap.TokenEstimate,
	}
	for _, finding := range snap.Findings {
		if len(context.Findings) >= subagentTreeContextFindings {
			break
		}
		if finding = truncate(finding); finding != "" {
			context.Findings = append(context.Findings, finding)
		}
	}
	return context
}

// subagentMainStatus 合成主代理根状态：任一后代运行中 → running；
// 否则任一失败 → failed；全部结束 → done。
func subagentMainStatus(children []SubAgentTreeNode) SubAgentNodeStatus {
	status := SubAgentDone
	var walk func(items []SubAgentTreeNode)
	walk = func(items []SubAgentTreeNode) {
		for _, item := range items {
			switch item.Status {
			case SubAgentRunning:
				status = SubAgentRunning
			case SubAgentFailed:
				if status != SubAgentRunning {
					status = SubAgentFailed
				}
			}
			walk(item.Children)
		}
	}
	walk(children)
	return status
}
