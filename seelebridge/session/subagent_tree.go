package session

import (
	"sort"
	"sync"
	"time"

	frameworksession "github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ──── 子代理树（fork 内存态可视化数据面）────
// fork_subagents 创建子代理时记录 parent/child 链（父节点 ID → 子节点列表），
// 节点状态（running/done/failed）、goal、紧凑 ContextSnapshot、会话摘要挂在
// 节点上；整棵树经 Runtime.SubAgentTree() 投影为只读 DTO 供 GUI 树视图渲染。
//
// done 节点有界保留（工作表格被动证据）：CompleteSubagentNode 成功路径写入
// done 终态并保留节点，超 subagentTreeRetainDone 上限时清理最旧 done；
// 失败节点保留现场；ClearSubagentTree 显式清空。注册/完成/清空均触发
// observer（application 装配后自动刷新工作表格——被动技能，不依赖模型
// 主观意愿）。详情数据面（nodeSessions 注册表：会话记录/上下文快照/工具
// 结果）独立保留，Plan 节点与详情弹窗仍可查历史。
//
// 明确不落盘：树随进程生命周期存在，会话恢复/重启后为空（与 nodeSessions
// 注册表同构）。ContextSnapshot 以紧凑投影挂在节点上（运行中实时导出、
// 结束后快照，截断有界）；完整快照仍经既有 Runtime.NodeContextSnapshot(nodeID)
// 数据面按需读取。

// SubAgentNodeStatus 是子代理树节点的生命周期状态（树投影专用；
// PlanNode 的细粒度状态仍由 PlanNodeStatus 表达）。
type SubAgentNodeStatus = dto.SubAgentNodeStatus

const (
	SubAgentQueued  = dto.SubAgentQueued
	SubAgentRunning = dto.SubAgentRunning
	SubAgentDone    = dto.SubAgentDone
	SubAgentFailed  = dto.SubAgentFailed
)

// SubAgentTreeNode 是子代理树的只读投影节点（GUI 树视图数据源）。
type SubAgentTreeNode = dto.SubAgentTreeNode

// SubAgentNodeContext 是树节点的紧凑上下文（ContextSnapshot 的有界投影）：
// 运行中实时导出、结束后快照；只含公开证据（Goal/Progress/MessageCount/
// TokenEstimate/Findings），单条截断到 subagentTreeContextLimit。
type SubAgentNodeContext = dto.SubAgentNodeContext

const (
	subagentTreeContextLimit     = 120
	subagentTreeContextFindings  = 3
	subagentLifecycleEventBuffer = 64
	subagentTreeRetainDone       = 50
)

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
	session     *frameworksession.Session // 运行中会话（实时上下文导出）
	contextSnap *snapshot.ContextSnapshot // 结束后快照（unregisterNodeSession 写入）
	startedAt   time.Time
	endedAt     time.Time
}

// SubagentTree 是子代理树注册表（Runtime 自有锁，与 nodeSessions 同构）。
type SubagentTree struct {
	mu       sync.Mutex
	nodes    map[string]*subagentNodeRecord
	children map[string][]string // parentID → 有序子节点列表
	events   chan struct{}
	trace    provider.TraceSource // 运行中上下文实时导出（nil 降级）
}

// NewSubagentTree 构造子代理树（trace 可为 nil，运行中实时导出降级为无
// Findings/Decisions）。
func NewSubagentTree(trace provider.TraceSource) *SubagentTree {
	return &SubagentTree{
		nodes:    make(map[string]*subagentNodeRecord),
		children: make(map[string][]string),
		events:   make(chan struct{}, subagentLifecycleEventBuffer),
		trace:    trace,
	}
}

// RegisterFork 记录一次 fork_subagents：parentID 下挂 N 个子代理节点
// （状态 queued、goal 来自 spec）。幂等：同 id 重复 fork 覆盖旧记录。
func (s *SubagentTree) RegisterFork(parentID string, specs []fork.SubagentSpec) {
	if s == nil || len(specs) == 0 {
		return
	}
	if parentID == "" {
		parentID = model.MainAgentNodeID
	}
	s.mu.Lock()
	for _, spec := range specs {
		s.nodes[spec.ID] = &subagentNodeRecord{
			id: spec.ID, parentID: parentID, goal: spec.Goal,
			status: SubAgentQueued, startedAt: time.Now(),
		}
		s.children[parentID] = append(s.children[parentID], spec.ID)
	}
	s.mu.Unlock()
	s.notify()
}

// notify 在锁外投递生命周期信号到 channel（CSP：application 消费者刷新
// 工作表格；非阻塞，满则丢信号——权威状态仍由消费者重读）。
func (s *SubagentTree) notify() {
	if s == nil {
		return
	}
	select {
	case s.events <- struct{}{}:
	default:
	}
}

// Events 返回生命周期信号 channel（CSP 消费者；幂等刷新）。
func (s *SubagentTree) Events() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.events
}

// NoteSession 挂载运行中子会话（registerNodeSession 调用；幂等）。
// 非 fork 节点（plan_run 的 agent 节点未入树）直接忽略。
func (s *SubagentTree) NoteSession(nodeID string, sess *frameworksession.Session) {
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

// MarkRunning 节点首次组装请求（真正开始执行，SSE 流开启）→ queued 转
// running。会话挂载不改变状态（B5：running 必须表示"在工作"）。
func (s *SubagentTree) MarkRunning(nodeID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	changed := false
	if record, ok := s.nodes[nodeID]; ok && record.status == SubAgentQueued {
		record.status = SubAgentRunning
		changed = true
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

// NoteSnapshot 挂载结束导出的上下文快照（unregisterNodeSession 调用；
// 幂等，之后投影直接复用，无需再次导出）。
func (s *SubagentTree) NoteSnapshot(nodeID string, snap *snapshot.ContextSnapshot) {
	if s == nil || nodeID == "" || snap == nil {
		return
	}
	s.mu.Lock()
	record := s.nodes[nodeID]
	if record == nil {
		s.mu.Unlock()
		return
	}
	record.contextSnap = snap
	s.mu.Unlock()
	// 快照挂载即通知：非打开节点的上下文数据随信号刷新，不必等 done。
	s.notify()
}

// CompleteSubagentNode 写入节点终态（AgentNode.Run 结束路径调用）：
// 成功 → done + 摘要/结束时间（有界保留，作为工作表格被动证据，直到
// ClearSubagentTree 显式清空）；失败 → failed + 错误（保留现场供排查）。
// 终态写入后通知 observer（application 自动刷新工作表格）。非 fork 节点
// no-op。
func (s *SubagentTree) CompleteSubagentNode(nodeID, summary string, runErr error) {
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	record := s.nodes[nodeID]
	if record == nil {
		s.mu.Unlock()
		return
	}
	record.summary = summary
	record.endedAt = time.Now()
	if runErr != nil {
		record.status = SubAgentFailed
		record.errorMsg = runErr.Error()
		s.mu.Unlock()
		s.notify()
		return
	}
	record.status = SubAgentDone
	s.pruneDoneBeyondCapLocked()
	s.mu.Unlock()
	s.notify()
}

// pruneDoneBeyondCapLocked 保留最近 subagentTreeRetainDone 个 done 节点
// （调用方持锁）；超限清理 endedAt 最旧的 done。
func (s *SubagentTree) pruneDoneBeyondCapLocked() {
	var done []*subagentNodeRecord
	for _, record := range s.nodes {
		if record.status == SubAgentDone {
			done = append(done, record)
		}
	}
	if len(done) <= subagentTreeRetainDone {
		return
	}
	sort.Slice(done, func(left, right int) bool {
		if done[left].endedAt.Equal(done[right].endedAt) {
			return done[left].id < done[right].id
		}
		return done[left].endedAt.Before(done[right].endedAt)
	})
	for _, record := range done[:len(done)-subagentTreeRetainDone] {
		s.removeNodeLocked(record.id)
	}
}

// removeNodeLocked 从注册表移除节点（调用方持锁）：删除节点记录与其子
// 节点列表，并从父节点 children 列表摘除。done 节点完成即清掉。
func (s *SubagentTree) removeNodeLocked(nodeID string) {
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

// Clear 清空整棵树（GUI"清空"入口：一次移除全部节点，含失败节点与
// 嵌套层级；详情数据面在 nodeSessions 注册表，不受影响）。
func (s *SubagentTree) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes = make(map[string]*subagentNodeRecord)
	s.children = make(map[string][]string)
	return nil
}

// SummaryFor 返回节点最后一次完成的输出摘要（复用判定：子代理跑完但外层
// 结果返回失败（final_output 被截断/read_tool_result 失败）时，retry 可以
// 直接读回已保存输出，避免重跑浪费 token）。仅 done 节点且摘要非空才算
// 可复用；failed/queued/running 无完整输出 → 空串。
func (s *SubagentTree) SummaryFor(nodeID string) string {
	if s == nil || nodeID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.nodes[nodeID]
	if record == nil || record.status != SubAgentDone || record.summary == "" {
		return ""
	}
	return record.summary
}

// Projection 返回子代理树的只读投影（根 = 主代理；含全部层级子节点）。
// 纯内存态投影：每次调用重建（深拷贝语义），不持有内部指针。
// 紧凑上下文：结束后节点复用 unregisterNodeSession 导出的快照（零额外
// 导出）；运行中节点经 ExportSnapshot 实时导出（与详情弹窗同一数据面，
// 只读子代理 actor，安全）。
func (s *SubagentTree) Projection() []SubAgentTreeNode {
	if s == nil {
		return nil
	}
	return s.projection(func(record *subagentNodeRecord) *SubAgentNodeContext {
		if record.contextSnap != nil {
			return compactSubAgentContext(record.contextSnap)
		}
		if record.session != nil {
			if snap := seelexctx.ExportSnapshot(record.session, s.trace, record.goal); snap != nil {
				return compactSubAgentContext(snap)
			}
		}
		return nil
	})
}

// projection 组装树投影：主代理为合成根；孤儿节点（父已不在注册表）归到
// 主代理下，树保持完整。空树（无 fork）返回 nil。
func (s *SubagentTree) projection(export func(*subagentNodeRecord) *SubAgentNodeContext) []SubAgentTreeNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	mainChildren := append([]string(nil), s.children[model.MainAgentNodeID]...)
	for id, record := range s.nodes {
		if record.parentID == model.MainAgentNodeID {
			continue
		}
		if _, ok := s.nodes[record.parentID]; !ok {
			mainChildren = append(mainChildren, id)
		}
	}
	if len(mainChildren) == 0 {
		return nil
	}
	root := SubAgentTreeNode{ID: model.MainAgentNodeID, Status: SubAgentRunning}
	for _, childID := range mainChildren {
		root.Children = append(root.Children, s.projectNode(childID, make(map[string]bool), export))
	}
	root.Status = subagentMainStatus(root.Children)
	return []SubAgentTreeNode{root}
}

// projectNode 递归投影单个树节点（visited 防环，防意外 fork DAG 无环的意外）。
func (s *SubagentTree) projectNode(id string, seen map[string]bool, export func(*subagentNodeRecord) *SubAgentNodeContext) SubAgentTreeNode {
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
		context.Findings = append(context.Findings, truncate(finding))
	}
	return context
}

// subagentMainStatus 汇总主代理合成根状态（递归）：任一子代理
// running/queued → running；存在 failed 且无 running/queued → failed；
// 否则 done。
func subagentMainStatus(children []SubAgentTreeNode) SubAgentNodeStatus {
	status := SubAgentDone
	var walk func(items []SubAgentTreeNode)
	walk = func(items []SubAgentTreeNode) {
		for _, item := range items {
			switch item.Status {
			case SubAgentRunning, SubAgentQueued:
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
