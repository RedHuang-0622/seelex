package session

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ─── 子代理会话注册表 actor（Runtime 装配件拆分 Step 1）───
//
// 原 Runtime 直接持有 nodeSessions/nodeSnapshots/nodeGoals/nodeContextSnapshots/
// nodeToolArchivers 五组 map 和一把 nodeSessionsMu。本组件把全部可变状态收进单个
// goroutine（actor 模型）：外部通过命令 channel 投递操作，actor 串行处理，天然
// 免锁。读取面经同步 reply 返回（命令量级低，同步足够）；Close 幂等并等待退出。
//
// 边界：组件只管理“会话注册表”数据面。subagentTree 的挂载副作用（noteSession/
// noteSnapshot）仍由 Runtime 委托方法在组件调用后完成，避免组件反向依赖树。
type SubagentSessions struct {
	cmd   chan subagentSessionCmd
	trace provider.TraceSource // 结束快照导出时提取 Findings/Decisions（nil 降级）
	done  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup

	// 以下字段仅在 actor goroutine 内访问。
	sessions         map[string]*frameworkSession.Session
	sessionIDs       map[string]string
	snapshots        map[string][]types.Message
	goals            map[string]string
	contextSnapshots map[string]*snapshot.ContextSnapshot
	toolArchivers    map[string]*seelexctx.InMemoryToolResultArchiver
	stages           map[string][]model.NodeStageLog
	results          map[string]*model.NodeSemanticResult
	resultQueue      []*model.NodeSemanticResult
	events           chan model.NodeStageLog
	droppedEvents    atomic.Int64
}

type subagentSessionCmdKind int

const (
	subagentSessionRegister subagentSessionCmdKind = iota
	subagentSessionUnregister
	subagentSessionConversation
	subagentSessionContextSnapshot
	subagentSessionToolArchiver
	subagentSessionToolResult
	subagentSessionRecordStage
	subagentSessionStageLogs
	subagentSessionRecordResult
	subagentSessionResult
	subagentSessionDrainResults
)

type subagentSessionCmd struct {
	kind   subagentSessionCmdKind
	nodeID string
	sess   *frameworkSession.Session
	goal   string
	ref    string
	stage  model.NodeStageLog
	res    *model.NodeSemanticResult
	reply  chan subagentSessionReply
}

type subagentSessionReply struct {
	snap *snapshot.ContextSnapshot
	msgs []types.Message
	ok   bool
	arch *seelexctx.InMemoryToolResultArchiver
	raw  string
	logs []model.NodeStageLog
	res  *model.NodeSemanticResult
	resq []*model.NodeSemanticResult
}

const (
	subagentSessionCmdCap     = 256
	subagentSessionCmdTimeout = 10 * time.Second
	subagentStageEventCap     = 512
)

func NewSubagentSessions(trace provider.TraceSource) *SubagentSessions {
	s := &SubagentSessions{
		cmd:              make(chan subagentSessionCmd, subagentSessionCmdCap),
		trace:            trace,
		done:             make(chan struct{}),
		sessions:         make(map[string]*frameworkSession.Session),
		sessionIDs:       make(map[string]string),
		snapshots:        make(map[string][]types.Message),
		goals:            make(map[string]string),
		contextSnapshots: make(map[string]*snapshot.ContextSnapshot),
		toolArchivers:    make(map[string]*seelexctx.InMemoryToolResultArchiver),
		stages:           make(map[string][]model.NodeStageLog),
		results:          make(map[string]*model.NodeSemanticResult),
		events:           make(chan model.NodeStageLog, subagentStageEventCap),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *SubagentSessions) run() {
	defer s.wg.Done()
	for {
		select {
		case cmd, ok := <-s.cmd:
			if !ok {
				return
			}
			s.handle(cmd)
		case <-s.done:
			return
		}
	}
}

func (s *SubagentSessions) handle(cmd subagentSessionCmd) {
	switch cmd.kind {
	case subagentSessionRegister:
		s.sessions[cmd.nodeID] = cmd.sess
		s.goals[cmd.nodeID] = cmd.goal
		if cmd.sess != nil {
			s.sessionIDs[cmd.nodeID] = cmd.sess.SessionID()
		}
	case subagentSessionUnregister:
		sess := s.sessions[cmd.nodeID]
		delete(s.sessions, cmd.nodeID)
		goal := s.goals[cmd.nodeID]
		delete(s.goals, cmd.nodeID)
		if sess == nil {
			s.reply(cmd, subagentSessionReply{})
			return
		}
		var snap *snapshot.ContextSnapshot
		if exported := seelexctx.ExportSnapshot(sess, s.trace, goal); exported != nil {
			s.contextSnapshots[cmd.nodeID] = exported
			snap = exported
		}
		s.snapshots[cmd.nodeID] = sess.History()
		s.reply(cmd, subagentSessionReply{snap: snap, ok: true})
	case subagentSessionConversation:
		if sess := s.sessions[cmd.nodeID]; sess != nil {
			s.reply(cmd, subagentSessionReply{msgs: sess.History(), ok: true})
			return
		}
		msgs, ok := s.snapshots[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{msgs: msgs, ok: ok})
	case subagentSessionContextSnapshot:
		if sess := s.sessions[cmd.nodeID]; sess != nil {
			goal := s.goals[cmd.nodeID]
			s.reply(cmd, subagentSessionReply{snap: seelexctx.ExportSnapshot(sess, s.trace, goal), ok: true})
			return
		}
		snap := s.contextSnapshots[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{snap: snap, ok: snap != nil})
	case subagentSessionToolArchiver:
		arch := s.toolArchivers[cmd.nodeID]
		if arch == nil {
			arch = seelexctx.NewInMemoryToolResultArchiver()
			s.toolArchivers[cmd.nodeID] = arch
		}
		s.reply(cmd, subagentSessionReply{arch: arch, ok: true})
	case subagentSessionToolResult:
		arch := s.toolArchivers[cmd.nodeID]
		if arch == nil {
			s.reply(cmd, subagentSessionReply{})
			return
		}
		raw, ok := arch.Read(strings.TrimPrefix(cmd.ref, model.NodeResultRefPrefix+cmd.nodeID+":"))
		s.reply(cmd, subagentSessionReply{raw: raw, ok: ok})
	case subagentSessionRecordStage:
		if cmd.nodeID != "" {
			log := cmd.stage
			log.NodeID = cmd.nodeID
			if log.SessionID == "" {
				log.SessionID = s.sessionIDs[cmd.nodeID]
			}
			if log.Stage == model.NodeStageTurn {
				turn := 0
				for _, existing := range s.stages[cmd.nodeID] {
					if existing.Stage == model.NodeStageTurn {
						turn++
					}
				}
				log.Turn = turn + 1
			}
			log.At = time.Now()
			s.stages[cmd.nodeID] = append(s.stages[cmd.nodeID], log)
			select {
			case s.events <- log:
			default:
				s.droppedEvents.Add(1)
			}
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionStageLogs:
		s.reply(cmd, subagentSessionReply{
			logs: append([]model.NodeStageLog(nil), s.stages[cmd.nodeID]...), ok: true,
		})
	case subagentSessionRecordResult:
		if cmd.res != nil && cmd.nodeID != "" {
			result := cmd.res
			result.NodeID = cmd.nodeID
			if result.SessionID == "" {
				result.SessionID = s.sessionIDs[cmd.nodeID]
			}
			result.Stages = append([]model.NodeStageLog(nil), s.stages[cmd.nodeID]...)
			s.results[cmd.nodeID] = result
			s.resultQueue = append(s.resultQueue, result)
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionResult:
		res := s.results[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{res: res, ok: res != nil})
	case subagentSessionDrainResults:
		queue := s.resultQueue
		s.resultQueue = nil
		s.reply(cmd, subagentSessionReply{resq: queue, ok: len(queue) > 0})
	}
}

func (s *SubagentSessions) reply(cmd subagentSessionCmd, reply subagentSessionReply) {
	if cmd.reply != nil {
		cmd.reply <- reply
	}
}

// send 投递命令并等待 actor 处理（带超时；actor 关闭后快速返回 false）。
func (s *SubagentSessions) send(cmd subagentSessionCmd) bool {
	if s == nil {
		return false
	}
	timer := time.NewTimer(subagentSessionCmdTimeout)
	defer timer.Stop()
	select {
	case s.cmd <- cmd:
		return true
	case <-timer.C:
		return false
	case <-s.done:
		return false
	}
}

// Register 注册运行中的子代理会话与节点目标（goal 供 ContextSnapshot 导出复用）。
func (s *SubagentSessions) Register(nodeID string, sess *frameworkSession.Session, goal string) {
	if s == nil || nodeID == "" || sess == nil {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionRegister, nodeID: nodeID, sess: sess, goal: goal})
}

// Unregister 结束注册：移除会话，导出并留存结束快照与最后 History；
// 返回导出的结束快照（无会话返回 nil），供 Runtime 挂载到 subagentTree。
func (s *SubagentSessions) Unregister(nodeID string) *snapshot.ContextSnapshot {
	if s == nil || nodeID == "" {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionUnregister, nodeID: nodeID, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.snap
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.done:
		return nil
	}
}

// Conversation 返回节点子代理会话记录：运行中实时 History；已结束返回留存快照。
func (s *SubagentSessions) Conversation(nodeID string) ([]types.Message, bool) {
	if s == nil || nodeID == "" {
		return nil, false
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionConversation, nodeID: nodeID, reply: reply}) {
		return nil, false
	}
	select {
	case result := <-reply:
		return result.msgs, result.ok
	case <-time.After(subagentSessionCmdTimeout):
		return nil, false
	case <-s.done:
		return nil, false
	}
}

// ContextSnapshot 返回节点子代理结构化上下文快照：运行中实时导出；已结束返回留存快照。
func (s *SubagentSessions) ContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if s == nil || nodeID == "" {
		return nil, false
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionContextSnapshot, nodeID: nodeID, reply: reply}) {
		return nil, false
	}
	select {
	case result := <-reply:
		return result.snap, result.ok
	case <-time.After(subagentSessionCmdTimeout):
		return nil, false
	case <-s.done:
		return nil, false
	}
}

// ToolResultArchiverFor 返回节点专属工具结果归档器（惰性创建并复用）。
func (s *SubagentSessions) ToolResultArchiverFor(nodeID string) *seelexctx.InMemoryToolResultArchiver {
	if s == nil || nodeID == "" {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionToolArchiver, nodeID: nodeID, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.arch
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.done:
		return nil
	}
}

// ToolResult 读回节点子代理的工具结果原始内容（ref 可带 node:<nodeID>: 前缀）。
func (s *SubagentSessions) ToolResult(nodeID, ref string) (string, bool) {
	if s == nil || nodeID == "" || ref == "" {
		return "", false
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionToolResult, nodeID: nodeID, ref: ref, reply: reply}) {
		return "", false
	}
	select {
	case result := <-reply:
		return result.raw, result.ok
	case <-time.After(subagentSessionCmdTimeout):
		return "", false
	case <-s.done:
		return "", false
	}
}

// RecordStage 记录 node 第一视角分阶段日志（同一 node 会话的认证面：
// SessionID 由 actor 从注册表补全，保证同节点多阶段同会话）。
func (s *SubagentSessions) RecordStage(nodeID string, log model.NodeStageLog) {
	if s == nil || nodeID == "" {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionRecordStage, nodeID: nodeID, stage: log})
}

// StageLogs 返回 node 的全部第一视角阶段日志（拷贝，按记录序）。
func (s *SubagentSessions) StageLogs(nodeID string) []model.NodeStageLog {
	if s == nil || nodeID == "" {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionStageLogs, nodeID: nodeID, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.logs
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.done:
		return nil
	}
}

// StageEvents 返回第一视角阶段日志的实时推送通道：每个阶段被记录后立即投递
// （即时输出，非轮询/缓存）；消费方按 NodeID 过滤。通道有界，满时丢弃并
// 计数（best-effort，绝不阻塞执行路径）。
func (s *SubagentSessions) StageEvents() <-chan model.NodeStageLog {
	if s == nil {
		return nil
	}
	return s.events
}

// DroppedEvents 返回因通道满被丢弃的实时事件数（诊断计数）。
func (s *SubagentSessions) DroppedEvents() int64 {
	if s == nil {
		return 0
	}
	return s.droppedEvents.Load()
}

// RecordResult 登记 node 的预定义语义结果并投入语义结果队列（消息队列路径）；
// actor 会补全 SessionID 与阶段日志。
func (s *SubagentSessions) RecordResult(nodeID string, result *model.NodeSemanticResult) {
	if s == nil || nodeID == "" || result == nil {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionRecordResult, nodeID: nodeID, res: result})
}

// Result 返回 node 最近一次语义结果（只读）。
func (s *SubagentSessions) Result(nodeID string) *model.NodeSemanticResult {
	if s == nil || nodeID == "" {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionResult, nodeID: nodeID, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.res
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.done:
		return nil
	}
}

// DrainResults 取空语义结果队列（消息队列消费面：mainagent / 下游 node 读取）。
func (s *SubagentSessions) DrainResults() []*model.NodeSemanticResult {
	if s == nil {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionDrainResults, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.resq
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.done:
		return nil
	}
}

// Close 关闭命令通道并等待 actor 退出（幂等）。
func (s *SubagentSessions) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.done) })
	s.wg.Wait()
}
