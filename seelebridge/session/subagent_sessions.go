package session

import (
	"strings"
	"sync"
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
	snapshots        map[string][]types.Message
	goals            map[string]string
	contextSnapshots map[string]*snapshot.ContextSnapshot
	toolArchivers    map[string]*seelexctx.InMemoryToolResultArchiver
}

type subagentSessionCmdKind int

const (
	subagentSessionRegister subagentSessionCmdKind = iota
	subagentSessionUnregister
	subagentSessionConversation
	subagentSessionContextSnapshot
	subagentSessionToolArchiver
	subagentSessionToolResult
)

type subagentSessionCmd struct {
	kind   subagentSessionCmdKind
	nodeID string
	sess   *frameworkSession.Session
	goal   string
	ref    string
	reply  chan subagentSessionReply
}

type subagentSessionReply struct {
	snap *snapshot.ContextSnapshot
	msgs []types.Message
	ok   bool
	arch *seelexctx.InMemoryToolResultArchiver
	raw  string
}

const (
	subagentSessionCmdCap     = 256
	subagentSessionCmdTimeout = 10 * time.Second
)

func NewSubagentSessions(trace provider.TraceSource) *SubagentSessions {
	s := &SubagentSessions{
		cmd:              make(chan subagentSessionCmd, subagentSessionCmdCap),
		trace:            trace,
		done:             make(chan struct{}),
		sessions:         make(map[string]*frameworkSession.Session),
		snapshots:        make(map[string][]types.Message),
		goals:            make(map[string]string),
		contextSnapshots: make(map[string]*snapshot.ContextSnapshot),
		toolArchivers:    make(map[string]*seelexctx.InMemoryToolResultArchiver),
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

// Close 关闭命令通道并等待 actor 退出（幂等）。
func (s *SubagentSessions) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.done) })
	s.wg.Wait()
}
