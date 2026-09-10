package session

import (
	"encoding/json"
	"log"
	"strings"
	"sync/atomic"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/actor"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/sessionstore"
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
	actor *actor.Actor[subagentSessionCmd]
	trace provider.TraceSource // 结束快照导出时提取 Findings/Decisions（nil 降级）

	// 节点会话记录持久化（用户约定：<mainSessionID>-<subSessionID>.json，
	// 见 sessionstore.NodeSessionRecord）。nil → 保持纯内存态（测试/未装配）。
	nodeStore     *sessionstore.NodeSessionStore
	mainSessionID func() string
	// projectID 解析记录归属项目（稳定于主会话绑定，不读 Router active
	// workspace）。nil → 回退 nodeStore.ProjectID()（测试/旧装配兼容）。
	projectID func() string
	// mainSessionIDs 是节点注册时显式绑定的主会话 ID（ctx 路由注入）。
	// 冷恢复/后台执行时不能依赖“当前 active session”。
	mainSessionIDs map[string]string
	// conclusionSink 在节点结束（done/failed）时把最终结论交给主会话侧
	// 持久化（"最后的结论跟随 mainagent"）；随后本节点记录文件被删除。
	conclusionSink func(string, sessionstore.NodeSessionRecord)

	// 以下字段仅在 actor goroutine 内访问。
	sessions   map[string]*frameworkSession.Session
	sessionIDs map[string]string
	snapshots  map[string][]types.Message
	// liveHistories 是运行中节点最近一次读到的历史（循环发布的检查点）。
	// Seele 的 Session 在整段 ChatStream 期间持有会话锁，actor 若直接读
	// History() 会停在流式上几十秒并堵住整个注册表；因此运行中的读取一律走
	// 这里的缓存（由 refreshLiveHistoryLocked 用 HistoryIfAvailable 刷新）。
	liveHistories    map[string][]types.Message
	goals            map[string]string
	contextSnapshots map[string]*snapshot.ContextSnapshot
	toolArchivers    map[string]*seelexctx.InMemoryToolResultArchiver
	stages           map[string][]model.NodeStageLog
	results          map[string]*model.NodeSemanticResult
	resultQueue      []*model.NodeSemanticResult
	worktrees        map[string]sessionstore.NodeWorktreeRecord
	outcomes         map[string]subagentOutcome
	events           chan model.NodeStageLog
	droppedEvents    atomic.Int64
}

// subagentOutcome 是节点终态（done/failed）的持久化摘要。
type subagentOutcome struct {
	status  string
	summary string
	errMsg  string
}

type subagentSessionCmdKind int

const (
	subagentSessionRegister subagentSessionCmdKind = iota
	subagentSessionUnregister
	subagentSessionSession
	subagentSessionCount
	subagentSessionConversation
	subagentSessionContextSnapshot
	subagentSessionToolArchiver
	subagentSessionToolResult
	subagentSessionRecordStage
	subagentSessionStageLogs
	subagentSessionRecordResult
	subagentSessionResult
	subagentSessionDrainResults
	subagentSessionNoteWorktree
	subagentSessionNoteOutcome
	subagentSessionRestore
	subagentSessionConfigure
)

type subagentSessionCmd struct {
	kind   subagentSessionCmdKind
	nodeID string
	// mainSessionID 是注册时显式绑定的主会话 ID（context 路由）。
	mainSessionID string
	sess          *frameworkSession.Session
	goal          string
	ref           string
	stage         model.NodeStageLog
	res           *model.NodeSemanticResult
	reply         chan subagentSessionReply
	wt            sessionstore.NodeWorktreeRecord
	out           subagentOutcome
	recs          []sessionstore.NodeSessionRecord
	// configure 装配（AttachSubSessionStore 注入；nil 字段保持现状）。
	store  *sessionstore.NodeSessionStore
	mainID func() string
	// projectID 解析记录归属项目（稳定于主会话绑定，不读 Router active
	// workspace；否则 fork 期间 Router 作用域漂移会把记录写到另一个项目）。
	projectID func() string
	sink      func(string, sessionstore.NodeSessionRecord)
}

type subagentSessionReply struct {
	snap *snapshot.ContextSnapshot
	sess *frameworkSession.Session
	msgs []types.Message
	ok   bool
	n    int
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

type SubagentSessionsOption func(*SubagentSessions)

// WithNodeSessionStore 装配节点会话记录持久化（nil = 纯内存态）。
func WithNodeSessionStore(store *sessionstore.NodeSessionStore) SubagentSessionsOption {
	return func(s *SubagentSessions) { s.nodeStore = store }
}

// WithMainSessionID 提供当前主会话 ID（记录落盘时作为索引键；nil = 不落盘）。
func WithMainSessionID(fn func() string) SubagentSessionsOption {
	return func(s *SubagentSessions) { s.mainSessionID = fn }
}

// WithConclusionSink 装配节点结束时的结论回传（mainagent 侧持久化；nil = 只删不传）。
func WithConclusionSink(fn func(string, sessionstore.NodeSessionRecord)) SubagentSessionsOption {
	return func(s *SubagentSessions) { s.conclusionSink = fn }
}

func NewSubagentSessions(trace provider.TraceSource, opts ...SubagentSessionsOption) *SubagentSessions {
	s := &SubagentSessions{
		trace:            trace,
		sessions:         make(map[string]*frameworkSession.Session),
		sessionIDs:       make(map[string]string),
		snapshots:        make(map[string][]types.Message),
		liveHistories:    make(map[string][]types.Message),
		goals:            make(map[string]string),
		contextSnapshots: make(map[string]*snapshot.ContextSnapshot),
		toolArchivers:    make(map[string]*seelexctx.InMemoryToolResultArchiver),
		stages:           make(map[string][]model.NodeStageLog),
		results:          make(map[string]*model.NodeSemanticResult),
		worktrees:        make(map[string]sessionstore.NodeWorktreeRecord),
		outcomes:         make(map[string]subagentOutcome),
		mainSessionIDs:   make(map[string]string),
		events:           make(chan model.NodeStageLog, subagentStageEventCap),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	s.actor = actor.New(s.handle, actor.WithCap(subagentSessionCmdCap))
	return s
}

func (s *SubagentSessions) handle(cmd subagentSessionCmd) {
	switch cmd.kind {
	case subagentSessionRegister:
		s.sessions[cmd.nodeID] = cmd.sess
		s.goals[cmd.nodeID] = cmd.goal
		if cmd.mainSessionID != "" {
			s.mainSessionIDs[cmd.nodeID] = cmd.mainSessionID
		}
		if cmd.sess != nil {
			s.sessionIDs[cmd.nodeID] = cmd.sess.SessionID()
		}
		s.persistLocked(cmd.nodeID)
	case subagentSessionUnregister:
		sess := s.sessions[cmd.nodeID]
		delete(s.sessions, cmd.nodeID)
		goal := s.goals[cmd.nodeID]
		delete(s.goals, cmd.nodeID)
		delete(s.mainSessionIDs, cmd.nodeID)
		if sess == nil {
			s.reply(cmd, subagentSessionReply{})
			return
		}
		if _, ok := s.outcomes[cmd.nodeID]; !ok {
			s.outcomes[cmd.nodeID] = subagentOutcome{status: "done"}
		}
		var snap *snapshot.ContextSnapshot
		// 节点结束路径：ChatStream 已返回（UnregisterNodeSession 在 defer 中
		// 执行，晚于 ChatStream 出栈），此时 HistoryIfAvailable 拿到的是权威
		// 历史；万一被短暂占用则退化为最后一次检查点，仍优于阻塞 actor。
		history := s.refreshLiveHistoryLocked(cmd.nodeID, sess)
		if exported := seelexctx.ExportSnapshotFromData(s.sessionIDs[cmd.nodeID], goal, len(history), s.trace); exported != nil {
			s.contextSnapshots[cmd.nodeID] = exported
			snap = exported
		}
		s.snapshots[cmd.nodeID] = history
		delete(s.liveHistories, cmd.nodeID)
		// 生命周期策略：结束即收敛——最终结论交给 mainagent（conclusionSink），
		// 节点自己的记录文件删除（运行期记录已覆盖崩溃恢复；结束后的详情
		// 数据面保持在内存快照，进程存活期内仍可读）。
		s.finalizeLocked(cmd.nodeID)
		s.reply(cmd, subagentSessionReply{snap: snap, ok: true})
	case subagentSessionSession:
		sess := s.sessions[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{sess: sess, ok: sess != nil})
	case subagentSessionCount:
		s.reply(cmd, subagentSessionReply{n: len(s.sessions), ok: true})
	case subagentSessionConversation:
		if sess := s.sessions[cmd.nodeID]; sess != nil {
			s.reply(cmd, subagentSessionReply{msgs: s.refreshLiveHistoryLocked(cmd.nodeID, sess), ok: true})
			return
		}
		msgs, ok := s.snapshots[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{msgs: msgs, ok: ok})
	case subagentSessionContextSnapshot:
		if sess := s.sessions[cmd.nodeID]; sess != nil {
			goal := s.goals[cmd.nodeID]
			history := s.refreshLiveHistoryLocked(cmd.nodeID, sess)
			s.reply(cmd, subagentSessionReply{
				snap: seelexctx.ExportSnapshotFromData(s.sessionIDs[cmd.nodeID], goal, len(history), s.trace),
				ok:   true,
			})
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
			s.persistLocked(cmd.nodeID)
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
			s.persistLocked(cmd.nodeID)
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionResult:
		res := s.results[cmd.nodeID]
		s.reply(cmd, subagentSessionReply{res: res, ok: res != nil})
	case subagentSessionDrainResults:
		queue := s.resultQueue
		s.resultQueue = nil
		s.reply(cmd, subagentSessionReply{resq: queue, ok: len(queue) > 0})
	case subagentSessionNoteWorktree:
		if cmd.nodeID != "" {
			s.worktrees[cmd.nodeID] = cmd.wt
			s.persistLocked(cmd.nodeID)
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionNoteOutcome:
		if cmd.nodeID != "" {
			s.outcomes[cmd.nodeID] = cmd.out
			s.persistLocked(cmd.nodeID)
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionRestore:
		for _, record := range cmd.recs {
			s.restoreLocked(record)
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	case subagentSessionConfigure:
		if cmd.store != nil {
			s.nodeStore = cmd.store
		}
		if cmd.mainID != nil {
			s.mainSessionID = cmd.mainID
		}
		if cmd.projectID != nil {
			s.projectID = cmd.projectID
		}
		if cmd.sink != nil {
			s.conclusionSink = cmd.sink
		}
		s.reply(cmd, subagentSessionReply{ok: true})
	}
}

// finalizeLocked 节点结束时收敛持久化（actor goroutine 内调用）：
// 1) 组装最终记录；2) conclusionSink 交给 mainagent 侧；3) 删除节点记录文件。
func (s *SubagentSessions) finalizeLocked(nodeID string) {
	if s.nodeStore == nil || nodeID == "" {
		return
	}
	mainID := s.mainSessionIDs[nodeID]
	if mainID == "" && s.mainSessionID != nil {
		mainID = s.mainSessionID()
	}
	if mainID == "" {
		return
	}
	record := s.buildRecordLocked(nodeID)
	record.MainSessionID = mainID
	if s.conclusionSink != nil {
		s.conclusionSink(mainID, record)
	}
	projectID := s.nodeStore.ProjectID()
	if record.SessionID != "" {
		if err := s.nodeStore.Delete(projectID, mainID, record.SessionID); err != nil {
			log.Printf("seelebridge/session: delete node session %q: %v", nodeID, err)
		}
	}
}

// restoreLocked 从持久化记录重建节点的详情数据面（actor goroutine 内调用）：
// 会话 ID/goal/History 快照/上下文快照/阶段日志/语义结果/终态/worktree。
// 无运行中会话：Conversation 读历史快照，ContextSnapshot 读留存快照。
func (s *SubagentSessions) restoreLocked(record sessionstore.NodeSessionRecord) {
	if record.NodeID == "" || record.SessionID == "" {
		return
	}
	s.sessionIDs[record.NodeID] = record.SessionID
	s.goals[record.NodeID] = record.Goal
	// 空历史也登记：恢复后 Conversation 可读（ok=true），语义与运行期一致。
	s.snapshots[record.NodeID] = record.History
	if len(record.ContextJSON) > 0 {
		var snap snapshot.ContextSnapshot
		if err := json.Unmarshal(record.ContextJSON, &snap); err == nil {
			s.contextSnapshots[record.NodeID] = &snap
		}
	}
	if len(record.StagesJSON) > 0 {
		var stages []model.NodeStageLog
		if err := json.Unmarshal(record.StagesJSON, &stages); err == nil {
			s.stages[record.NodeID] = stages
		}
	}
	if len(record.ResultJSON) > 0 {
		var result model.NodeSemanticResult
		if err := json.Unmarshal(record.ResultJSON, &result); err == nil {
			s.results[record.NodeID] = &result
		}
	}
	if record.Worktree.Path != "" {
		s.worktrees[record.NodeID] = record.Worktree
	}
	if record.Status != "" {
		s.outcomes[record.NodeID] = subagentOutcome{
			status: record.Status, summary: record.Summary, errMsg: record.Error,
		}
	}
}

// persistLocked 把节点的当前内存态投影为 NodeSessionRecord 并落盘
// （actor goroutine 内调用；best-effort，失败只记日志，不影响执行路径）。
func (s *SubagentSessions) persistLocked(nodeID string) {
	if s.nodeStore == nil || nodeID == "" {
		return
	}
	mainID := ""
	if s.mainSessionID != nil {
		mainID = s.mainSessionID()
	}
	if mainID == "" {
		return
	}
	record := s.buildRecordLocked(nodeID)
	record.MainSessionID = mainID
	projectID := s.nodeStore.ProjectID()
	if s.projectID != nil {
		projectID = s.projectID()
	}
	if err := s.nodeStore.Save(projectID, mainID, record); err != nil {
		log.Printf("seelebridge/session: persist node session %q: %v", nodeID, err)
	}
}

// refreshLiveHistoryLocked 刷新并返回运行中节点的最近历史（actor goroutine 内
// 调用），**绝不阻塞**：Seele 的 Session 在整段 ChatStream 期间持有会话锁，
// History() 会让 actor 停在流式上（实测 28s；actor 是单 goroutine，一处阻塞
// 会让所有节点的详情与落账排队，mailbox 满后还丢阶段事件）。HistoryIfAvailable
// 返回循环在每个历史检查点发布的快照，代价是最多滞后一个检查点；节点刚注册、
// 尚未发布过检查点时返回上次缓存（可能为 nil）。
func (s *SubagentSessions) refreshLiveHistoryLocked(nodeID string, sess *frameworkSession.Session) []types.Message {
	if sess != nil {
		if history, ok := sess.HistoryIfAvailable(); ok {
			s.liveHistories[nodeID] = history
		}
	}
	return s.liveHistories[nodeID]
}

// buildRecordLocked 把节点当前内存态投影为 NodeSessionRecord（actor goroutine 内调用）。
func (s *SubagentSessions) buildRecordLocked(nodeID string) sessionstore.NodeSessionRecord {
	record := sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        nodeID,
		SessionID:     s.sessionIDs[nodeID],
		Goal:          s.goals[nodeID],
		UpdatedAt:     time.Now().UTC(),
	}
	if sess := s.sessions[nodeID]; sess != nil {
		// 运行期落账读的是循环发布的检查点（HistoryIfAvailable），**不再阻塞
		// actor**：本函数由 RecordStage/RecordResult/NoteWorktree/NoteOutcome
		// 在节点运行期间触发，而节点整段 ChatStream 都持有会话锁——旧实现让
		// actor 停在流式上（实测 28s），期间所有节点的详情与落账全部排队。
		// 代价是记录最多滞后一个检查点，换来的是"写得及时"：崩溃恢复要的是
		// 最近一次成功落盘的检查点，而不是卡了几十秒才写下的那一份。
		history := s.refreshLiveHistoryLocked(nodeID, sess)
		record.History = history
		record.ContextJSON, _ = json.Marshal(
			seelexctx.ExportSnapshotFromData(record.SessionID, record.Goal, len(history), s.trace),
		)
	} else if len(s.snapshots[nodeID]) > 0 {
		record.History = s.snapshots[nodeID]
		if snap := s.contextSnapshots[nodeID]; snap != nil {
			record.ContextJSON, _ = json.Marshal(snap)
		}
	} else if snap := s.contextSnapshots[nodeID]; snap != nil {
		record.ContextJSON, _ = json.Marshal(snap)
	}
	if stages := s.stages[nodeID]; len(stages) > 0 {
		record.StagesJSON, _ = json.Marshal(stages)
	}
	if result := s.results[nodeID]; result != nil {
		record.ResultJSON, _ = json.Marshal(result)
	}
	if wt, ok := s.worktrees[nodeID]; ok {
		record.Worktree = wt
	}
	if outcome, ok := s.outcomes[nodeID]; ok {
		record.Status = outcome.status
		record.Summary = outcome.summary
		record.Error = outcome.errMsg
	} else if _, running := s.sessions[nodeID]; running {
		record.Status = "running"
	} else {
		record.Status = "queued"
	}
	return record
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
	return s.actor.SendTimeout(cmd, subagentSessionCmdTimeout)
}

// Register 注册运行中的子代理会话与节点目标（goal 供 ContextSnapshot 导出复用）。
func (s *SubagentSessions) Register(nodeID string, sess *frameworkSession.Session, goal string) {
	s.RegisterFor("", nodeID, sess, goal)
}

// RegisterFor 在显式主会话作用域下注册节点（冷恢复/后台会话用；空 ID 回退
// 兼容旧调用）。
func (s *SubagentSessions) RegisterFor(mainSessionID, nodeID string, sess *frameworkSession.Session, goal string) {
	if s == nil || nodeID == "" || sess == nil {
		return
	}
	s.send(subagentSessionCmd{
		kind: subagentSessionRegister, mainSessionID: strings.TrimSpace(mainSessionID),
		nodeID: nodeID, sess: sess, goal: goal,
	})
}

// Session 返回指定节点当前注册的运行中会话（UC7 查询面）；不存在返回 nil。
func (s *SubagentSessions) Session(nodeID string) *frameworkSession.Session {
	if s == nil || nodeID == "" {
		return nil
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionSession, nodeID: nodeID, reply: reply}) {
		return nil
	}
	select {
	case result := <-reply:
		return result.sess
	case <-time.After(subagentSessionCmdTimeout):
		return nil
	case <-s.actor.Done():
		return nil
	}
}

// Count 返回当前注册的子代理会话数（监控/测试）。
func (s *SubagentSessions) Count() int {
	if s == nil {
		return 0
	}
	reply := make(chan subagentSessionReply, 1)
	if !s.send(subagentSessionCmd{kind: subagentSessionCount, reply: reply}) {
		return 0
	}
	select {
	case result := <-reply:
		return result.n
	case <-time.After(subagentSessionCmdTimeout):
		return 0
	case <-s.actor.Done():
		return 0
	}
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
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
	case <-s.actor.Done():
		return nil
	}
}

// NoteWorktree 记录节点 worktree 现场（持久化恢复数据面：重启后可重建
// nodeID → path/branch/baseCommit 索引）。
func (s *SubagentSessions) NoteWorktree(nodeID string, wt sessionstore.NodeWorktreeRecord) {
	if s == nil || nodeID == "" {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionNoteWorktree, nodeID: nodeID, wt: wt})
}

// NoteOutcome 记录节点终态（done/failed + 摘要/错误），落盘后恢复时
// worktable 认领与详情数据面可直接回填。
func (s *SubagentSessions) NoteOutcome(nodeID, status, summary, errMsg string) {
	if s == nil || nodeID == "" {
		return
	}
	s.send(subagentSessionCmd{
		kind: subagentSessionNoteOutcome, nodeID: nodeID,
		out: subagentOutcome{status: status, summary: summary, errMsg: errMsg},
	})
}

// Restore 从持久化记录重建节点会话的详情数据面（重启/恢复锚点；
// 无运行中会话，详情读取走留存快照与历史）。
func (s *SubagentSessions) Restore(records []sessionstore.NodeSessionRecord) {
	if s == nil || len(records) == 0 {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionRestore, recs: records})
}

// Configure 装配/替换节点会话记录持久化（Router 就绪后注入；幂等）。
// store/mainID/sink 任一为 nil 表示保持现状；显式关闭需分别传 nil 包装。
func (s *SubagentSessions) Configure(store *sessionstore.NodeSessionStore, mainID, projectID func() string, sink func(string, sessionstore.NodeSessionRecord)) {
	if s == nil {
		return
	}
	s.send(subagentSessionCmd{kind: subagentSessionConfigure, store: store, mainID: mainID, projectID: projectID, sink: sink})
}

// Close 关闭命令通道并等待 actor 退出（幂等）。
func (s *SubagentSessions) Close() {
	if s == nil {
		return
	}
	s.actor.Close()
	s.actor.Wait()
}
