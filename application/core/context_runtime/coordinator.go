package context_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/context_control"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	TaskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"
	planContextPrefix           = "<!-- seelex:active-plan:v1 -->"
	// ActiveSkillPrefix 标记激活技能正文 internal 事件：作为 append-only user
	// 轮次进入 transcript（与 task_context.ActiveSkillMarker 同源字符串），并
	// **以 wire material 落盘**（resume 回放、检索回读）；装配/存档/import 用
	// IsActiveSkillContent 把它挡在可见会话之外；它不是每轮重建的动态尾部消息，
	// 保留段照常携带（定稿轮次，字节稳定）。压缩窗口裁剪后不再出现在 wire 上。
	ActiveSkillPrefix       = "<!-- seelex:active-skill:v1 -->"
	ToolResultOmittedPrefix = "<seelex-tool-result-omitted>"
	// AutonomousCompactionPrefix 标记自主压缩帧：正常有界窗口装不下全量预算
	// 时，装配层主动把可变 transcript 折叠为有界 checkpoint 摘要（而不是直接
	// 拒绝发送）。属动态尾部消息 → 不进保留前缀，回合结束由应用清理路径移除。
	AutonomousCompactionPrefix = "<!-- seelex:context-compact:v1 -->"
	// 恢复/预算终局前缀：与根包 history_safety.go / chat.go 同源协议字符串
	// （context_runtime 不反向依赖 core 根包，字符串字面量在此保留）。
	contextRecoveryPrefix         = "<!-- seelex:context-recovery:v1 -->"
	providerRecoveryPrefix        = "<!-- seelex:provider-recovery:v1 -->"
	reactBudgetFinalizationPrefix = "<!-- seelex:react-budget-finalize:v1 -->"
)

// IsActiveSkillContent 判定内容是否为激活技能 internal 事件（Append-only
// transcript 技能轮次；前端可见性/存档/import 据此跳过）。
func IsActiveSkillContent(content string) bool {
	return strings.HasPrefix(content, ActiveSkillPrefix)
}

// ErrProviderContextBudgetExceeded 标记 provider 上下文超过安全 token 预算。
var ErrProviderContextBudgetExceeded = errors.New("provider context exceeds the safe token budget")

// Coordinator 拥有 provider 上下文装配与控制（token 预算/压缩/result-ref/
// 可恢复中断）。
type Coordinator struct {
	*state.Core
	tasks           TaskPort
	sessions        SessionPort
	prompts         PromptPort
	view            ViewPort
	history         HistoryPort
	workTable       func(sessionID string) string
	compactionIndex CompactionIndexPort

	// pendingCompactMu 保护 pendingForceCompact：显式压缩（/compact、
	// compact_context）落在**还没有执行纪元**的会话上时（冷加载、刚清空），
	// 没有 RequestID 可以折叠，也不伪造一个——改登记为"该会话下一次装配
	// provider 上下文时按显式路径压缩"。本锁只在装配入口最前面短暂持有，
	// 不与 Core.ViewMu 构成嵌套。
	pendingCompactMu    sync.Mutex
	pendingForceCompact map[string]bool

	// pushMu 保护 pushLocks：按会话键的推帧串行锁表（见 compactionPushLock）。
	// 生命周期与 sessionStates 同口径——按会话键长期保留，条目上限即会话数。
	pushMu    sync.Mutex
	pushLocks map[string]*sync.Mutex
}

// compactionPushLock 返回指定会话的推帧串行锁（惰性创建）。
//
// 为什么需要一条**只包推帧**的窄串行：压缩栈是链式结构，PushCompact 会校验
// PrevSegmentID / PrevRequestFrom / PrevRequestTo 必须与栈顶逐一相等（见
// sessionstore.SessionContextStore.PushCompact）。两个折叠并发推同一会话时，
// 后者按自己读到的栈顶填锚点，必然撞上这条校验。推帧原先在 Core.ViewMu 的临界
// 区里，这条串行是**顺带**得到的；推帧移出锁（见 prepareExecutionContextFor 的
// 锁纪律）之后必须显式补回，否则就是我们在缩小锁粒度时把一条既有不变量丢了。
// 只包推帧本身：渲染、落存储、写记录都不在这把锁里，ViewMu 也不会回来。
// 按会话键取锁，跨会话不互等。
func (c *Coordinator) compactionPushLock(sessionID string) *sync.Mutex {
	c.pushMu.Lock()
	defer c.pushMu.Unlock()
	if c.pushLocks == nil {
		c.pushLocks = make(map[string]*sync.Mutex)
	}
	lock := c.pushLocks[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		c.pushLocks[sessionID] = lock
	}
	return lock
}

// NewCoordinator 构造 context 域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:                deps.Core,
		tasks:               deps.Tasks,
		sessions:            deps.Sessions,
		prompts:             deps.Prompts,
		view:                deps.View,
		history:             deps.History,
		workTable:           deps.WorkTableTraceBlock,
		compactionIndex:     deps.CompactionIndex,
		pendingForceCompact: make(map[string]bool),
	}
}

// ScheduleForceCompact 登记「该会话下一次装配 provider 上下文时按显式路径压缩」。
//
// 用途：`/compact` / compact_context 打在还没有执行纪元的会话上——刚冷加载、
// 刚清空的会话只有已装载的历史，没有 TaskExecutionState.RequestID 可以折叠
// （prepareExecutionContextFor 在 state == nil 时按设计直接返回）。这里不伪造
// 纪元（伪造会把"有人在跑这个会话"写进状态），而是把"用户要求现在就压"记成
// 一件待办：下一条消息组装上下文时先折叠再发送，压缩对那条消息立即生效。
func (c *Coordinator) ScheduleForceCompact(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if c == nil || sessionID == "" {
		return
	}
	c.pendingCompactMu.Lock()
	defer c.pendingCompactMu.Unlock()
	if c.pendingForceCompact == nil {
		c.pendingForceCompact = make(map[string]bool)
	}
	c.pendingForceCompact[sessionID] = true
}

// consumePendingForceCompact 取走（并清除）登记项：true = 本次装配按显式压缩处理。
func (c *Coordinator) consumePendingForceCompact(sessionID string) bool {
	if c == nil || sessionID == "" {
		return false
	}
	c.pendingCompactMu.Lock()
	defer c.pendingCompactMu.Unlock()
	if !c.pendingForceCompact[sessionID] {
		return false
	}
	delete(c.pendingForceCompact, sessionID)
	return true
}

// Ports 是装配端口图的只读快照（组装校验/诊断用）。
func (c *Coordinator) Ports() Ports {
	return Ports{
		Tasks: c.tasks, Sessions: c.sessions, Prompts: c.prompts,
		View: c.view, History: c.history,
	}
}

// Ports 描述 context 域的协作端口。
type Ports struct {
	Tasks    TaskPort
	Sessions SessionPort
	Prompts  PromptPort
	View     ViewPort
	History  HistoryPort
}

// CompactTaskContext 把整个可变 transcript 替换为一个私有、有界的 checkpoint
// （引擎迭代 hook 调用，绝不持有 Core.ViewMu；活跃会话兼容包装）。
func (c *Coordinator) CompactTaskContext(requestID string) error {
	return c.CompactTaskContextFor(c.tasks.SessionIDForRequest(requestID), requestID)
}

// CompactTaskContextFor 把指定会话整个可变 transcript 替换为一个私有、有界
// 的 checkpoint（引擎迭代 hook 调用，绝不持有 Core.ViewMu）。
func (c *Coordinator) CompactTaskContextFor(sessionID, requestID string) error {
	return c.compactTaskContextFor(sessionID, requestID, prepareOptions{})
}

// forceCompactTaskContextFor 是显式压缩入口（/compact、compact_context）：
// **不设阈值前提**，也不受"每个 progress epoch 只压一次"的自动节流——用户/模型
// 明确要求现在就压缩时，"还没到线"不是理由（此前 129409 tokens 的会话被回一句
// "未达压缩阈值 118962"，正是显式路径仍被软阈值挡住 + 判据量与展示量混用的结果）。
//
// 返回 decision：把"压没压、按哪个量判、有没有落记录"如实带回调用方，调用方不再
// 用别的数字反推结论。
func (c *Coordinator) forceCompactTaskContextFor(ctx context.Context, sessionID, requestID string) (compactDecision, error) {
	decision := compactDecision{}
	options := prepareOptions{forceCompact: true, decision: &decision}
	if err := c.compactTaskContextFor(sessionID, requestID, options); err != nil {
		return compactDecision{}, err
	}
	return decision, nil
}

func (c *Coordinator) compactTaskContextFor(sessionID, requestID string, options prepareOptions) error {
	_, err := c.prepareExecutionContextFor(sessionID, requestID, "", options)
	if err != nil {
		return err
	}
	if err := c.sessions.PersistCurrentSession(c.sessionLocationLocked(sessionID), sessionID); err != nil {
		return fmt.Errorf("persist context checkpoint: %w", err)
	}
	return nil
}

// CompactOutcome 是主动压缩的结果分类：已压缩并落记录 / 已折叠但未落记录 /
// 已登记（无执行纪元，下一次装配兑现）/ 判据未达（兜底）。调用方据此给出准确
// 提示，而不是把"没做事"混成"出错了"，也不拿与判据无关的数字拼一句自相矛盾的话。
type CompactOutcome string

const (
	CompactDone             CompactOutcome = "compacted"
	CompactFoldedUnrecorded CompactOutcome = "folded_without_record"
	CompactScheduled        CompactOutcome = "scheduled"
	CompactBelowThreshold   CompactOutcome = "below_threshold"
)

// CompactResult 是主动压缩的结果面：结果分类 + 判据事实 + 压缩记录（落记录时）。
//
// 三个数字含义不同，混用就会说出「129409 tokens 未达压缩阈值 118962」这种话：
//
//	ComparedTokens  = 判据量（全量累积/引擎缓存峰值的请求估算，压缩决策用的那个量）
//	AssembledTokens = 装配后估算（真正发给 provider 的请求大小）
//	SoftThreshold   = 判据量的软阈值（limits.context_soft_percent，默认 95%）
type CompactResult struct {
	Outcome         CompactOutcome
	Record          model.ContextCompaction
	Folded          bool
	Recorded        bool
	Version         uint64
	ComparedTokens  int
	AssembledTokens int
	SoftThreshold   int
	HardThreshold   int
	// Gates 是本轮门禁的逐关实测耗时（权威顺序，与进度事件同源）。显式路径的
	// 回执据此自带"走了哪几关、各花多久"：进度事件是瞬态、不进快照、终局后
	// ~2.5s 撤条，只靠它用户按完回车再抬头就什么都看不到了。
	Gates []CompactionGateTiming
	// NoEpoch 标记本次压缩落在**没有在飞回合**的会话上（冷加载、刚清空）：
	// 折叠按会话级维护身份执行（见 task_context.SessionMaintenanceRequestPrefix），
	// 而不是登记到"下一条消息"再兑现。回执据此说明这次压缩为什么能立刻生效。
	NoEpoch bool
}

// CompactContextNow 主动压缩指定会话的可变 transcript（`/compact` 命令与
// `compact_context` 工具的同一落点）：与引擎钩子走同一条 CompactTaskContextFor
// 路径，但走**显式语义**——不设阈值前提（用户/模型明确要求即压）。
//
// 会话有没有在飞回合走两条路，**两条都当场压缩**：
//   - 有匹配当前 request 的执行纪元 → 按该纪元折叠（显式路径正常落记录）；
//   - 没有执行纪元（冷加载、刚清空）→ 打开会话级维护身份，立刻折叠**已装载**
//     的上下文（含落盘、落记录、出帧正文），不再只登记到"下一条消息组装时
//     兑现"——用户按下回车就是要现在看到结果，不能要求他再发一条消息。
//     只有会话真的没有可折叠内容（transcript 与引擎历史都没有对话材料）时，
//     才回落到登记语义（CompactScheduled）。
//
// 三种提前返回都如实分类，不伪造压缩、也不谎报理由：
//   - 没有可折叠的请求上下文（空会话）→ 登记"下一条消息组装时立即压缩"
//     （CompactScheduled），不伪造一个假回合；
//   - 折叠已发生但记录被拒 → CompactFoldedUnrecorded。显式路径正常必落记录
//     （记录门槛对显式来源放宽到"回合已收尾也记"），走到这里只可能是该请求的
//     执行面已被新回合替换；自动路径在回合收尾后按口径不补记，也会落到这一类；
//   - 判据没命中（显式路径正常不会发生，兜底）→ CompactBelowThreshold。
func (c *Coordinator) CompactContextNow(ctx context.Context, sessionID string) (CompactResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return CompactResult{}, errors.New("compact context: session id is required")
	}
	// 冷加载/刚清空：当场折叠已装载的上下文（返回 handled=false 表示这次该由
	// 既有纪元路径处理：并发开了回合，或会话没有任何可折叠材料）。
	if result, handled, err := c.compactSessionContextWithoutEpoch(ctx, sessionID); handled {
		return result, err
	}
	state := c.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || strings.TrimSpace(state.RequestID) == "" {
		c.ScheduleForceCompact(sessionID)
		return CompactResult{Outcome: CompactScheduled}, nil
	}
	decision, err := c.forceCompactTaskContextFor(ctx, sessionID, state.RequestID)
	if err != nil {
		return CompactResult{}, err
	}
	result := CompactResult{
		Folded:          decision.Folded,
		Recorded:        decision.Recorded,
		Version:         decision.Version,
		ComparedTokens:  decision.ComparedTokens,
		AssembledTokens: decision.AssembledTokens,
		SoftThreshold:   decision.SoftThreshold,
		HardThreshold:   decision.HardThreshold,
		Gates:           decision.Gates,
	}
	switch {
	case decision.NoEpoch:
		c.ScheduleForceCompact(sessionID)
		result.Outcome = CompactScheduled
	case !decision.Folded:
		result.Outcome = CompactBelowThreshold
	case decision.Recorded:
		result.Outcome = CompactDone
		if updated := c.tasks.CurrentTaskExecutionFor(sessionID); updated != nil && len(updated.ContextCompactions) > 0 {
			result.Record = updated.ContextCompactions[len(updated.ContextCompactions)-1]
		}
	default:
		result.Outcome = CompactFoldedUnrecorded
	}
	return result, nil
}

// compactSessionContextWithoutEpoch 处理"会话没有在飞回合"（冷加载、刚清空）
// 的显式压缩：**立刻**折叠已装载的上下文，而不是登记到下一次装配。
//
// handled=false 表示本次不该由本分支负责，调用方继续按纪元路径处理：
//   - 会话此刻已有在飞回合（并发开了新回合）；
//   - 会话没有可折叠材料（引擎历史里没有对话消息、transcript 为空）——空会话
//     折叠只会产出一条区间为空的记录，那是把"没做事"记成"做了事"，所以这里
//     维持既有语义：登记下一条消息兑现（handled=true + CompactScheduled）。
//
// 维护身份的作用范围严格限定在这一次折叠内：拿到身份 → 折叠（落记录、出帧正文、
// 替换引擎历史、按会话落盘）→ 撤销身份。期间不写 ChatState.Running、不设
// Snapshot.Chat.RequestID、不建任务注册表条目，因此可见面上没有"有人在跑这个
// 会话"的假信号。
func (c *Coordinator) compactSessionContextWithoutEpoch(ctx context.Context, sessionID string) (CompactResult, bool, error) {
	if state := c.tasks.CurrentTaskExecutionFor(sessionID); state != nil && strings.TrimSpace(state.RequestID) != "" {
		return CompactResult{}, false, nil
	}
	if !c.hasFoldableSessionContext(sessionID) {
		// 空会话：折叠只会产出一条区间为空的记录（把"没做事"记成"做了事"），
		// 因此维持既有语义——登记为"下一条消息组装上下文时先压后发"。
		c.ScheduleForceCompact(sessionID)
		return CompactResult{Outcome: CompactScheduled}, true, nil
	}
	c.ViewMu.Lock()
	requestID := c.tasks.BeginSessionContextMaintenanceLocked(sessionID)
	c.ViewMu.Unlock()
	if requestID == "" {
		// 身份没拿到：期间会话开了回合（或已挂着别的身份）→ 交回调用方重判。
		return CompactResult{}, false, nil
	}
	decision, err := c.forceCompactTaskContextFor(ctx, sessionID, requestID)
	// 无论成败都撤销维护身份：留下一个假身份会让这个会话的后续压缩与回合
	// 都拿不到干净的状态。
	c.ViewMu.Lock()
	c.tasks.EndSessionContextMaintenanceLocked(sessionID, requestID)
	c.ViewMu.Unlock()
	if err != nil {
		return CompactResult{}, true, err
	}
	result := CompactResult{
		Folded:          decision.Folded,
		Recorded:        decision.Recorded,
		Version:         decision.Version,
		ComparedTokens:  decision.ComparedTokens,
		AssembledTokens: decision.AssembledTokens,
		SoftThreshold:   decision.SoftThreshold,
		HardThreshold:   decision.HardThreshold,
		Gates:           decision.Gates,
		NoEpoch:         true,
	}
	switch {
	case !decision.Folded:
		result.Outcome = CompactBelowThreshold
	case decision.Recorded:
		result.Outcome = CompactDone
		if updated := c.tasks.CurrentTaskExecutionFor(sessionID); updated != nil && len(updated.ContextCompactions) > 0 {
			result.Record = updated.ContextCompactions[len(updated.ContextCompactions)-1]
		}
	default:
		result.Outcome = CompactFoldedUnrecorded
	}
	return result, true, nil
}

// hasFoldableSessionContext 判定会话是否装载了**可折叠的对话材料**：transcript
// 有事件，或引擎历史里有非 system 消息（纯 system 前缀折叠不出任何区间，不算
// 材料）。
func (c *Coordinator) hasFoldableSessionContext(sessionID string) bool {
	if len(c.tasks.TranscriptFor(sessionID)) > 0 {
		return true
	}
	for _, message := range c.foldHistory(sessionID) {
		if !strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			return true
		}
	}
	return false
}

// sessionLocationLocked 返回指定会话的持久化定位（workspace 绑定优先；
// 回退当前活跃工作区）。压缩 checkpoint 落盘的目标会话可能不是活跃会话，
// 必须按会话键落盘（对应 R3 键漂移修复）。
func (c *Coordinator) sessionLocationLocked(sessionID string) session_runtime.Location {
	c.ViewMu.RLock()
	defer c.ViewMu.RUnlock()
	location := session_runtime.Location{Meta: model.SessionInfo{ID: sessionID}}
	if workspaceID, ok := c.Snapshot.SessionWorkspaces[sessionID]; ok && workspaceID != "" {
		location.WorkspaceID = workspaceID
		return location
	}
	if c.Snapshot.CurrentWorkspace != nil {
		location.WorkspaceID = c.Snapshot.CurrentWorkspace.ID
		location.Workspace = c.Snapshot.CurrentWorkspace
	}
	return location
}

// PrepareExecutionContext 从 durable task 状态与完整 transcript 单元重建
// provider 缓存（活跃会话兼容包装）。返回可能被引用的当前输入；仍超安全
// 预算时拒绝发送。
func (c *Coordinator) PrepareExecutionContext(requestID, currentInput string) (string, error) {
	return c.PrepareExecutionContextFor(c.tasks.SessionIDForRequest(requestID), requestID, currentInput)
}

// PrepareExecutionContextFor 从 durable task 状态与完整 transcript 单元重建
// 指定会话 provider 缓存。返回可能被引用的当前输入；仍超安全预算时拒绝
// 发送。sessionID 指明执行会话（多会话并行时目标会话）。
func (c *Coordinator) PrepareExecutionContextFor(sessionID, requestID, currentInput string) (string, error) {
	return c.prepareExecutionContextFor(sessionID, requestID, currentInput, prepareOptions{})
}

// compactDecision 是一次装配在压缩判据上的**事实面**：显式入口（/compact、
// compact_context）据此如实报告结果，而不是拿别的数字（如装配后估算）反推。
type compactDecision struct {
	Folded          bool // 本次是否折叠了可变 transcript（原 compacting 判据）
	Recorded        bool // 是否落了压缩记录（由 task_context 的记录门槛判定）
	Version         uint64
	ComparedTokens  int // 判据量：全量累积/引擎缓存峰值的请求估算（rawTokens）
	AssembledTokens int // 装配后估算（estimated，真正发给 provider 的大小）
	SoftThreshold   int
	HardThreshold   int
	// Gates 是本轮门禁的逐关实测耗时（权威顺序，与进度事件同一份数字）。
	// 显式路径的调用方据此把"走了哪几关、各花多久"带回用户：进度事件是瞬态
	// （revision=0、不进快照、终局后 ~2.5s 撤条），只靠它，用户按完回车再抬头
	// 就什么都看不到了——回执必须自己拿得住这份事实。
	Gates   []CompactionGateTiming
	NoEpoch bool // 没有可折叠的执行纪元（state == nil 或 requestID 不匹配）
}

// prepareOptions 是装配的可选语义（零值 = 自动路径）。
type prepareOptions struct {
	// forceCompact 表示调用方显式要求压缩（/compact、compact_context）：
	// 不设阈值前提（不再等软阈值），也不受"每个 progress epoch 只压一次"的
	// 自动节流。显式路径的硬前提只有一条：该会话有匹配当前 request 的执行纪元。
	forceCompact bool
	// decision 是出参：非 nil 时由装配过程回填压缩判据事实（见 compactDecision）。
	decision *compactDecision
}

// compactionOrigin 判定一轮折叠的来源：自动路径（软/硬阈值、自主压缩）记 auto，
// 显式要求（/compact、compact_context）记 explicit；回合已收尾时的显式要求记
// explicit_after_turn——记录门槛按它放行，否则「回合之间压缩」在前端彻底不可见。
func compactionOrigin(options prepareOptions, state *task_context.TaskExecutionState) string {
	if !options.forceCompact {
		return model.CompactionOriginAuto
	}
	if state.Status != task_context.StatusRunning {
		return model.CompactionOriginExplicitAfterTurn
	}
	return model.CompactionOriginExplicit
}

func (c *Coordinator) prepareExecutionContextFor(sessionID, requestID, currentInput string, options prepareOptions) (out string, err error) {
	// 显式压缩的「下一条消息兑现」：没有执行纪元时登记的强压在这里取走，
	// 本次装配即按显式路径折叠（先压后发，压缩对本条消息立即生效）。
	if !options.forceCompact && c.consumePendingForceCompact(sessionID) {
		options.forceCompact = true
	}
	if _, err := c.rejectOversizedToolResults(sessionID, task_context.DefaultToolResultLimit()); err != nil {
		return "", err
	}
	// 工作打点表：请求尾部的只读标记块（system 前缀保持不变 → 缓存友好；
	// 无活动任务时块为空 → 自动删除；不落历史 → 不参与压缩）。打点按正在
	// 组装上下文的会话取数：后台会话只见自己的 scope 分区，不串活跃会话。
	if block := c.workTable(sessionID); block != "" {
		if currentInput == "" {
			currentInput = block
		} else {
			currentInput = block + "\n\n" + currentInput
		}
	}
	budget := task_context.ContextBudgetFor(c.Deps.Runtime)
	tools := c.Deps.Runtime.VisibleTools(context.Background())
	existing := c.foldHistory(sessionID)
	c.ViewMu.RLock()
	systemPrompt := c.prompts.SystemPromptForActiveTaskLockedFor(sessionID)
	c.ViewMu.RUnlock()
	c.setFoldSystemPrompt(sessionID, systemPrompt)

	runtimeModel := c.Deps.Runtime.Model()
	c.ViewMu.Lock()
	state := c.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || state.RequestID != requestID {
		c.ViewMu.Unlock()
		if options.decision != nil {
			options.decision.NoEpoch = true
		}
		return currentInput, nil
	}
	events := append([]model.TranscriptEvent(nil), c.tasks.TranscriptFor(sessionID)...)
	events = excludeCurrentInputEvent(events, requestID, currentInput)
	// 累积上下文的绝对起点：上一次折叠已经覆盖的 transcript 前缀不再回填。
	// 判据、保留窗口与装配都从同一起点往后看——否则每次装配都把已折出的前缀
	// 重新算进“全量累积”，长会话稳定越线、每回合重新压一次。
	retainedFrom := 0
	if state.ContextRetainedFrom > 0 && state.ContextRetainedFrom <= len(events) {
		retainedFrom = state.ContextRetainedFrom
	}
	accumulated := events[retainedFrom:]
	// 门禁进度：显式路径（/compact、compact_context）在动第一个重活之前就开轮，
	// 并立刻发起手帧——从"按下回车"到"判据关收口"之间要跑两次全量请求估算
	// （原始累积上下文 + 引擎缓存峰值），是整轮里最长的一段；没有这帧，界面在这
	// 段时间里完全空白（用户看到的是"按了没反应"，然后突然冒出一条已完成的压缩
	// 记录，于是合理地怀疑"只有前端、后端没接线"）。
	//
	// 自动路径提前不了：要不要折叠正是这次估算的结果。显式路径可以——fold 判据里
	// forceCompact 恒为真（fold := ... || options.forceCompact），所以这里开轮与
	// 下方 `if fold` 一定配对，"没折叠"的轮次不会为不存在的事发进度。
	//
	// 起手帧的版本号此刻还不存在（新版本在下方 newCheckpoint 里定稿），发 0 表示
	// "未定"；判据关收口时用 setVersion 补正，绝不先猜一个版本号。
	var progress *compactionProgress
	var recorded bool
	if options.forceCompact {
		progress = c.startCompactionProgress(sessionID, requestID, 0, compactionOrigin(options, state))
		progress.begin()
		defer func() { progress.settle(err, recorded, "") }()
	}
	// 达峰判定以**未被折叠覆盖**的全量累积 context 为准（而非可能已被框架
	// 压缩的引擎历史）：与引擎缓存估算取峰值，压缩是唯一使累积前缀失效的事件。
	// 尾窗选择注入请求同款估算器（TranscriptTailWindowBy），裁剪量与判据量
	// 才是同一把尺子；否则一边按事件记录值裁、一边按当前校准值判，保留窗口
	// 会重新“膨胀”越线。
	fullContext := task_context.TranscriptTailHistoryBy(accumulated, budget.Budget, 0, c.transcriptUnitTokens)
	rawTokens := c.tasks.CountRequestTokens(systemPrompt, fullContext, currentInput, tools)
	if cacheTokens := c.tasks.CountRequestTokens(systemPrompt, existing, currentInput, tools); cacheTokens > rawTokens {
		rawTokens = cacheTokens
	}
	currentInput = c.protectOversizedCurrentInputLocked(sessionID, requestID, currentInput, budget)
	// 压缩策略输入（同一份 window 配置段，框架侧 WindowPolicy 与这里共用）：
	//
	//	软压缩 = provider 比例阈值（limits.context_soft_percent，默认 95%）+ 保留窗口规则
	//	         保留前缀 = min(token1, token2)，token1 = 配置里硬编码的
	//	         保留窗口 token 数（未配置回退账号上下文窗口），
	//	         token2 = ratio × all_context（全量上下文 token 数）；
	//	         窗口外部分尽数交给 compact_context 折叠。
	//	硬压缩 = all_context ≥ window.force_compact_tokens（必须自主压缩，
	//	         不再等比例阈值：长任务下比例阈值可能永远不触发）。
	windowConfig := context_control.Current()
	allContextTokens := c.tasks.CountRequestTokens("", fullContext, "", nil)
	hardCompact := windowConfig.MustCompact(allContextTokens)
	// 折叠判据（三条，命中任一条即折叠）：
	//	① 软阈值：rawTokens ≥ budget.SoftThreshold（自动路径的主判据）；
	//	② 硬阈值：all_context ≥ window.force_compact_tokens（必须压，不等比例）；
	//	③ 显式路径：options.forceCompact（/compact、compact_context）——用户/模型
	//	   明确要求现在就压缩时**不设阈值前提**（"还没到线"不是拒绝理由）。
	fold := rawTokens >= budget.SoftThreshold || hardCompact || options.forceCompact
	// 自动路径按 progress epoch 节流（同一批进展只压一次）；显式路径与硬压缩
	// 不受节流挡下（用户/模型明确要求时不接受"等下一批进展再说"，硬阈值必须压）。
	//
	// 额外放行"本执行还没有任何压缩记录"（len(ContextCompactions) == 0）：会话/执行的
	// 第一次折叠必须留痕。progress epoch 与 CompactedEpoch 的初值都是"未开始"语义，
	// 只看"两值不等"会把首轮折叠判成本纪元已压过——前三关照跑（进度报表都出来了）、
	// 却不落记录不落帧，用户看到"压缩了"却查不到压了哪段。首压留痕，同纪元后续再挡。
	newCheckpoint := fold && (options.forceCompact || hardCompact ||
		state.CompactedEpoch != state.ProgressEpoch || len(state.ContextCompactions) == 0)
	if newCheckpoint {
		state.ContextVersion++
		state.CompactedEpoch = state.ProgressEpoch
	}
	checkpoint := c.tasks.BuildTaskCheckpointLocked(state)
	checkpoint.Version = state.ContextVersion
	// 压缩来源在折叠这一刻判定一次：进度面与压缩记录必须写同一个 origin，两处
	// 分别采样 status 会让同一次折叠对不上号。
	origin := compactionOrigin(options, state)
	summary := state.ContextSummary()
	planMessage := c.planContextMessageLocked(sessionID)
	c.ViewMu.Unlock()

	// 门禁进度：只有真的要折叠才开一轮。"没有纪元、登记为下一条消息兑现"与
	// "未达判据"都不是压缩进行中——为没发生的事画进度条，比没有进度条更糟。
	// 显式路径的轮次已在估算之前开好（见上），这里只补正版本号。
	if fold {
		if progress == nil {
			progress = c.startCompactionProgress(sessionID, requestID, checkpoint.Version, origin)
			defer func() { progress.settle(err, recorded, "") }()
		} else {
			// 起手帧发出时新版本号还没定稿（发的是 0=未定）：判据关收口时补上，
			// 否则进度条上的 #N 会缺一截，或者对到上一条压缩记录上。
			progress.setVersion(checkpoint.Version)
		}
	}

	systems := RetainedSystemHistory(c.foldHistory(sessionID))
	// 折叠留下的保留窗口是 transcript 的**后缀**，已由 accumulated 从头重建；
	// 保留段只留 system 前缀，避免窗口内容与累积段重复计入。
	if retainedFrom > 0 {
		systems = RetainedSystemOnly(systems)
	}
	// 保留前缀窗口（软压缩）：min(token1, token2)，见上方 windowConfig 注释。
	// 窗口外部分尽数送进 compact_context；保留窗口按完整协议单元边界收敛
	// （单元不可拆分），因此不再叠加配置单元上限做第二次截断。
	compacting := fold
	target := budget.Budget
	retain := RetainDecision{}
	if compacting {
		retain = retainWindowDecision(windowConfig, allContextTokens, budget, limits.Get().ContextRetainFloorPercent)
		if retain.Retained > 0 {
			target = retain.Retained
		}
	}
	if fold {
		// 判据关在保留窗口决策**之后**收口：这一关的事实就是"拿什么数字比的"
		// ——判据量 + 保留窗口决策（含保护区下限），两者同源、同一次采样
		// （all= 由保留窗口决策给出，不在这里重复一份同值事实）。
		progress.gate(CompactionGateJudge, fmt.Sprintf("compared=%d soft=%d hard=%d %s",
			rawTokens, budget.SoftThreshold, budget.HardThreshold, retain.Terse()))
	}
	// transcript 压缩区间的记事基准：累积模式可能丢掉已覆盖前缀，记录边界
	// 时用原始 events（未裁剪）＋丢弃条数还原绝对下标。
	transcript := events
	windowEvents := accumulated
	discardedEvents := 0
	if !compacting {
		// 累积模式：保留段（稳定前缀 + 已定稿轮次）已覆盖 transcript 前缀，
		// 只追加保留段之后的新事件（append-only，字节稳定）。
		if covered := retainedContextEventCount(systems); covered > 0 {
			if covered < len(windowEvents) && !retainedMatchesTranscriptPrefix(systems, windowEvents) {
				// 冷恢复只装载了尾部窗口：保留段是 transcript 的**后缀**而
				// 非前缀（retainedMatchesTranscriptPrefix=false）。此时按
				// “已覆盖 covered 条事件”跳过会得到 [tail]+[middle] 的
				// 重排与重复（恢复后首个请求上下文顺序 != 会话顺序，前缀
				// 字节不稳定 → 缓存无法命中）。回退为从完整 transcript
				// 按原序重建：保留段只留 system，事件不裁剪。
				systems = RetainedSystemOnly(systems)
			} else if covered < len(windowEvents) {
				discardedEvents = covered
				windowEvents = windowEvents[covered:]
			} else {
				windowEvents = nil // 全部事件已被保留段覆盖：本次无需追加
			}
		}
	}
	assembled, retainedWindowFrom, estimated := c.fitExecutionHistory(systemPrompt, systems, planMessage, windowEvents, currentInput, tools, target, compacting, 0)
	// 自主压缩（探测即主动触发）：装配结果一旦逼近硬阈值（limits.context_hard_percent，
	// 默认 98%），说明
	// 可变 transcript 已经压不动——此时立刻折叠为有界 checkpoint 帧（稳定
	// system 前缀 + 任务证据摘要 + plan + 当前输入），而不是把贴着上限的历史
	// 发出去、等下一次超过全量预算再兜底。触发点是"探测到接近上限"，不是
	// "已经超限"：主动压缩给下一轮留出确定余量，也避免在窗口边缘反复抖动。
	//
	// 原始轮次仍完整留在会话存储里，模型需要细节时按结果引用/分页回读；
	// 只有压缩形态自身仍超全量预算（如 system 指令自身超窗口）才拒绝发送。
	autonomous := false
	if estimated > budget.HardThreshold {
		if compressed, compressedTokens, ok := c.compressExecutionHistory(systemPrompt, systems, summary, planMessage, currentInput, tools, budget); ok && compressedTokens < estimated {
			assembled, estimated, autonomous = compressed, compressedTokens, true
		}
	}
	// 本次折叠覆盖到的 transcript 绝对边界：普通折叠 = 已折出前缀的终点
	// （保留窗口从它开始），自主压缩 = 整个 transcript 都被 checkpoint 替代。
	// 记录区间与下一回合的累积起点都读这一份事实，不再各自重算。
	compressedTo := retainedFrom + discardedEvents + retainedWindowFrom
	if autonomous {
		compressedTo = len(transcript)
	}
	// 四区显式化（《压缩四区模型》① ② ③ ④ + 当轮输入）：分区、各区 token 数与
	// 来源来自**装配结果本身**，判据量与保留窗口决策同一次采样写入。门禁 Detail
	// 与帧正文的四区区块都读这一份 layout —— 报表口径与判据口径因此不可能分叉。
	layout := c.buildContextLayout(systemPrompt, assembled, currentInput)
	layout.Retain = retain
	layout.ComparedTokens = rawTokens
	layout.EstimatedTokens = estimated
	layout.SoftThreshold = budget.SoftThreshold
	layout.HardThreshold = budget.HardThreshold
	layout.Compacting = compacting
	progress.gate(CompactionGateAssemble, fmt.Sprintf("assembled=%d target=%d autonomous=%t %s",
		estimated, target, autonomous, layout.ZonesTerse()))
	if estimated > budget.Budget {
		return "", fmt.Errorf("%w: estimated=%d budget=%d", ErrProviderContextBudgetExceeded, estimated, budget.Budget)
	}
	replacement := c.withInFlightTail(existing, assembled)
	if err := c.replaceFoldHistory(sessionID, replacement); err != nil {
		return "", fmt.Errorf("assemble provider context: %w", err)
	}
	if err := c.history.PrepareProviderHistoryFor(sessionID); err != nil {
		return "", err
	}
	progress.gate(CompactionGateReplace, fmt.Sprintf("messages=%d", len(replacement)))

	// ── 折叠落点的锁纪律：ViewMu 内只留内存提交，慢活一律在锁外 ─────────────
	//
	// 这一段原先是**单个** ViewMu 临界区里做完三件事：推进会话状态、推帧
	// （CompactionIndexPort → 前缀重放 DAG + 原文归档 + 压缩栈写盘）、帧正文落
	// 内容存储。第一件是微秒级内存操作，后两件是模型调用与磁盘 I/O。持锁做后两
	// 件有两个后果，都不是假想：
	//
	//	① 永久自锁：推帧会回调到装配根注入的实现（见 main.go 的
	//	   CompressedTurnArchiver）。它在 ctx 没有会话归属时读 app.Snapshot()，
	//	   而 Snapshot 要 ViewMu.RLock——同一个 goroutine 持写锁再取读锁，
	//	   RWMutex 不可重入 → 永久阻塞，没有超时、外部取消也进不来。观感是整块
	//	   交互面冻死：进度条停在 index 关之前（replace 3/7），/compact 不返回，
	//	   快照取不到（会话切不动、列表与右栏不刷新），新消息连队列都进不去。
	//	② 锁的持有时间 = 推帧耗时：软线折叠在回合里跑，于是**一个**会话的折叠
	//	   把它自己连同其它会话的提交、快照、切会话一起堵在这把全局锁上。与
	//	   2026-09-23 message 读路径那条教训同形（读路径持写锁做整段解码）。
	//
	// 因此切成三段：锁内提交状态（A）→ 锁外推帧与渲染（B）→ 锁内落存储与写记录
	// （C）。段间传递的都是值拷贝（记录、区间、帧输入），不把锁内对象的指针带出
	// 去；C 段写记录时由 task 域自己按 requestID 复核归属（回合已换人 →
	// recorded=false，与"记录门槛不满足"同一结论），不需要额外的锁内校验。
	c.ViewMu.Lock()
	state = c.tasks.CurrentTaskExecutionFor(sessionID)
	stateMatched := state != nil && state.RequestID == requestID
	var revision uint64
	// B/C 两段的材料：只有拿到执行纪元（stateMatched）且本轮要落记录时才有效。
	commitFold := false
	compactedRange := task_context.TranscriptEventRange{}
	record := model.ContextCompaction{}
	reason := ""
	if stateMatched {
		// 本次折叠后累积上下文的起点前移到新的保留窗口/checkpoint 边界；
		// 未折叠不动（保留窗口没有变化）。跨回合由 continuationTaskExecutionState
		// 继承，避免下一回合从 transcript 头部重新累积。
		if compacting || autonomous {
			state.ContextRetainedFrom = compressedTo
		}
		state.TokenAudit = model.TokenAudit{
			Model: runtimeModel, Counter: c.tasks.TokenCounterName(),
			Budget: budget.Budget, SoftThreshold: budget.SoftThreshold, HardThreshold: budget.HardThreshold,
			TargetAfterCompaction: budget.TargetAfterCompaction, EstimatedPromptTokens: estimated,
			ActualPromptTokens: state.TokenAudit.ActualPromptTokens, UpdatedAt: time.Now(),
		}
		// 自主压缩也开启一个新压缩纪元：checkpoint 版本前进，下一轮达峰判定
		// 与压缩记录不重复（压缩帧本身是动态尾部，不参与保留前缀）。
		if autonomous && !newCheckpoint {
			state.ContextVersion++
			state.CompactedEpoch = state.ProgressEpoch
			checkpoint.Version = state.ContextVersion
			// 进度面跟随同一个版本：判定关取数时自主压缩还没发生，不校正就会
			// 把进度条对到上一条压缩记录上。
			progress.setVersion(checkpoint.Version)
		}
		if newCheckpoint || autonomous {
			c.tasks.RememberCheckpointLocked(checkpoint)
			reason = "context_budget"
			if autonomous {
				reason = "context_budget_autonomous"
			}
			// 压缩区间（记录，不推算）：被压出保留窗口、送进 compact_context 的
			// transcript 前缀（compressedTo 在装配后、落记录前已定稿）。区间与记录
			// 都是**值事实**，在这里定稿后就可以安全带出锁外（B 段要用它渲染帧正文）。
			compactedRange = task_context.TranscriptPrefixRange(transcript, compressedTo)
			record = model.ContextCompaction{
				Version: checkpoint.Version, Reason: reason, Origin: origin,
				MessagesBefore:  len(existing),
				EstimatedTokens: rawTokens, CompactedAt: time.Now(),
				MessageFrom: compactedRange.MessageFrom, MessageTo: compactedRange.MessageTo,
				EventFrom: compactedRange.EventFrom, EventTo: compactedRange.EventTo,
			}
			commitFold = true
		} else if fold {
			// 折叠真的发生了（前三关照跑），但没有走到落记录：同一个 progress 纪元内
			// 已压过、且装配后估算未越硬阈值（走进来的 autonomous 为假）。把这条原因
			// 写进终局 Detail——进度条不该走到一半就沉默，读者需要一个能自答的句号。
			progress.skip(fmt.Sprintf("skipped=epoch_throttled compacted_epoch=%d progress_epoch=%d context_version=%d",
				state.CompactedEpoch, state.ProgressEpoch, state.ContextVersion))
		}
	}
	c.ViewMu.Unlock()

	// ── B 段（锁外）：推帧 + 帧正文渲染 ────────────────────────────────────
	if commitFold {
		// 推帧：把这次折出保留窗口的区间推进会话压缩栈（窄可选能力，见
		// CompactionIndexPort）。**必须在渲染帧正文之前**——正文要嵌入回执
		// 里的 segment_id、摘要来源与降级原因，否则读帧的人只能看到"没有
		// 细筛入口"，而模型根本拿不到 read_compressed_turn 的入参。
		//
		// 溢出素材取 transcript[retainedFrom:compressedTo]：retainedFrom 之前
		// 的区间已被更早的帧覆盖（帧链自足），重复喂进去只会让检索命中两段
		// 同内容；自主压缩时 compressedTo = len(transcript)，即"尚未被任何帧
		// 覆盖的全部"。
		//
		// 重放素材（ReplayHistory）首选 existing——上一次真实请求的历史字节，
		// 与产出该请求是同一条装配路径（前缀重放靠它命中前缀缓存）。但它**在
		// 没有在飞请求时必然为空**：冷加载/刚清空的会话还没有物化引擎历史，
		// 而这类会话的折叠恰恰是最常见的一次（/compact、会话级维护身份、以及
		// 冷加载后第一条消息触发的自动折叠）。此时素材改取 fullContext——本次
		// 装配从 transcript 投影出的那份历史（"如果这次不折叠，就会发出去的
		// 对话内容"）。否则每一帧都落在 no-replay-material 上：帧里只有一句
		// "本次不调用模型"，模型从未被问到，而读帧的人无从知道差的就是这份素材
		// （2026-09-29 现场）。
		replayMaterial := existing
		if len(replayMaterial) == 0 {
			replayMaterial = fullContext
		}
		//
		// 未装配索引面与推帧失败都不中断装配（索引缺失是降级不是错误），
		// 但门禁 index 关与帧正文的 readback 段都要如实写出是哪一种。
		//
		// 这一步在锁外（见上方锁纪律）：DAG 可能做前缀重放厚摘要的**模型调用**，
		// 归档器要写盘，且接收侧实现是宿主任意代码——持 ViewMu 跑它既会把交互面
		// 冻住，也会被它回调取读锁而永久自锁。
		push := compactionIndexPush{}
		if compacting || autonomous {
			push = c.pushCompactionFrame(sessionID, requestID,
				task_context.TranscriptEventMessages(foldedOverflowEvents(transcript, retainedFrom, compressedTo)),
				replayMaterial, compactedRange)
			progress.gate(CompactionGateStackPush, push.gateDetail())
		}
		// 帧正文（同样在锁外渲染；纯函数，只读上面这份值事实）：快照只带 ref，
		// 前端按 ref 分页回读。此前帧正文只活在内存 engine history（回合收尾即
		// 被剔除）、摘要只进自主压缩的 wire 正文，前端因此"看得到压缩、看不到帧"。
		frame := compactionFrameBody(compactionFrameInput{
			Version:       checkpoint.Version,
			Reason:        reason,
			Origin:        origin,
			At:            record.CompactedAt,
			SegmentID:     push.SegmentID,
			SummarySource: push.SummarySource,
			// SummaryNote 只在"这次没有模型摘要"时有值：帧正文据此把 local 的三种
			// 来路（开关关闭 / 无重放素材 / 重放失败及其报错）分开写实。
			SummaryNote:  push.SummaryNote,
			Summary:      push.Summary,
			IndexError:   push.indexError(),
			IndexSkipped: push.Skipped,
			Range: compactionFoldedRange{
				MessageFrom: record.MessageFrom,
				MessageTo:   record.MessageTo,
				EventFrom:   record.EventFrom,
				EventTo:     record.EventTo,
				Label:       model.CompactionRangeLabel(record.MessageFrom, record.MessageTo, record.EventFrom, record.EventTo),
			},
			ComparedTokens:  rawTokens,
			AssembledTokens: estimated,
			SoftThreshold:   budget.SoftThreshold,
			HardThreshold:   budget.HardThreshold,
			Evidence:        summary,
			PlanMessage:     planMessage,
			Injected:        autonomous,
			Layout:          layout,
		})

		// ── C 段（锁内）：帧正文进内容存储 + 写压缩记录 + 翻转视图修订 ──────
		// 这一段只碰内存（内容存储的 pending 登记与记录投影），因此是短锁。
		c.ViewMu.Lock()
		if strings.TrimSpace(frame) != "" {
			stored := c.tasks.StoreToolResultForLocked(sessionID, compactionFrameTool, frame)
			record.FrameRef, record.FrameBytes, record.FrameTokens = stored.Ref, stored.Size, stored.TokenCount
			progress.gate(CompactionGateFrame, fmt.Sprintf("bytes=%d injected=%t", len(frame), autonomous))
			progress.gate(CompactionGateStore, fmt.Sprintf("bytes=%d tokens=%d", record.FrameBytes, record.FrameTokens))
		}
		recorded = c.tasks.RecordContextCompactionLocked(requestID, record)
		if recorded {
			revision = c.view.BumpLocked()
		}
		progress.gate(CompactionGateRecord, fmt.Sprintf("recorded=%t version=%d", recorded, checkpoint.Version))
		c.ViewMu.Unlock()
	}
	if stateMatched && options.decision != nil {
		// 压缩判据事实（显式入口据此如实报告，不拿别的数字反推）：
		options.decision.Folded = compacting
		options.decision.Recorded = recorded
		options.decision.Version = checkpoint.Version
		options.decision.ComparedTokens = rawTokens
		options.decision.AssembledTokens = estimated
		options.decision.SoftThreshold = budget.SoftThreshold
		options.decision.HardThreshold = budget.HardThreshold
		// 逐关耗时在全部门禁收口之后取（record 关在上方已发），因此这份
		// 清单不会缺最后一关。
		options.decision.Gates = progress.GateTimings()
	}
	if recorded {
		if hub, ok := c.Events.(event.SessionAwareHub); ok {
			hub.PublishSession(event.EventSnapshotChanged, revision, requestID, sessionID, nil)
		} else {
			c.Events.Publish(event.EventSnapshotChanged, revision, requestID, nil)
		}
	}
	return currentInput, nil
}

// fitExecutionHistory 按目标预算装配 provider 历史：稳定前缀（system）→
// 累积 context（已定稿轮次，含 append-only 的激活技能事件）→ plan 尾部。
// windowed=false = 全量累积（达峰前 append-only，字节稳定）；windowed=true =
// 有界窗口（压缩后新鲜窗口）。maxUnits <= 0 = 只按 token 窗口截断
// （单元不可拆分，按完整单元边界收敛）。
//
// 第二个返回值是保留窗口在 events 中的起始下标（events 中被保留的 transcript
// 前缀边界）：压缩记录用它记下被压区间的结束，不再事后推算。
func (c *Coordinator) fitExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	planMessage string,
	events []model.TranscriptEvent,
	currentInput string,
	tools []model.Tool,
	target int,
	windowed bool,
	maxUnits int,
) ([]contract.EngineMessage, int, int) {
	// 压缩窗口模式：丢弃保留的累积段，从事件重建新鲜窗口（保留段只供全量
	// 累积模式复用，避免与窗口内容重复）。
	base := systems
	if windowed {
		base = RetainedSystemOnly(systems)
	}
	if history, retainedFrom, estimated := c.tryFitExecutionHistory(systemPrompt, base, planMessage, events, currentInput, tools, target, maxUnits); estimated <= target {
		return history, retainedFrom, estimated
	}
	// 达峰回退：全量累积超预算 → 折为有界窗口；窗口仍超 → 逐级收缩。
	// 最终兜底不“静默清空”：TranscriptTailHistory 保证至少返回最新 1 个
	// 完整单元（即使估算超过 target），不再走 events=nil 的 system+plan
	// 空历史分支；估算仍超出全量预算时由调用方以 ErrProviderContextBudgetExceeded
	// 拒绝发送（拒绝优于“模型失忆”，正常路径不用 checkpoint 兜底）。
	for shrink := limits.Get().ContextMaxUnits; shrink > 0; shrink-- {
		if history, retainedFrom, estimated := c.tryFitExecutionHistory(systemPrompt, base, planMessage, events, currentInput, tools, target, shrink); estimated <= target {
			return history, retainedFrom, estimated
		}
	}
	return c.tryFitExecutionHistory(systemPrompt, RetainedSystemOnly(systems), planMessage, events, currentInput, tools, target, 1)
}

// tryFitExecutionHistory 装配一次 system → context → plan 历史并估算 token，
// 同时回报保留窗口在 events 中的起始下标。
func (c *Coordinator) tryFitExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	planMessage string,
	events []model.TranscriptEvent,
	currentInput string,
	tools []model.Tool,
	target int,
	maxUnits int,
) ([]contract.EngineMessage, int, int) {
	history := append([]contract.EngineMessage(nil), systems...)
	tail, retainedFrom := task_context.TranscriptTailWindowBy(events, target, maxUnits, c.transcriptUnitTokens)
	history = append(history, tail...)
	// plan 后置贴近当前输入（LLM 循环会把当前输入追加到历史尾部）。
	if planMessage != "" {
		history = append(history, contract.EngineMessage{Role: "system", Content: planMessage, ContentSet: true})
	}
	return history, retainedFrom, c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
}

// transcriptUnitTokens 按请求装配同款估算器给一个协议单元计价。保留窗口
// （target）、达峰判据与最终装配必须共用这一把尺子：事件自带的 TokenCount 是
// 落盘那一刻的估算值，校准因子变化后与当前估算会漂移，按记录值裁出的窗口在
// 重新估算时会“膨胀”回阈值以上，造成每回合重压。
func (c *Coordinator) transcriptUnitTokens(unit []model.TranscriptEvent) int {
	return c.tasks.CountRequestTokens("", task_context.TranscriptEventMessages(unit), "", nil)
}

// compressExecutionHistory 是自主压缩兜底：正常有界窗口装不下全量预算时，
// 把可变 transcript 折叠为「稳定 system 前缀 + 有界 checkpoint 摘要 + plan +
// 当前输入」。摘要由 TaskExecutionState.ContextSummary 提供
// （objective/plan/evidence/已完成工具结果的恢复材料，正文受
// limits.max_tool_result_chars 约束）；原始轮次仍完整留在会话存储里，模型
// 需要细节时用分页/过滤工具回读。返回 ok=false 表示连压缩形态都超预算
// （例如 system 提示自身就超出窗口），此时调用方仍以
// ErrProviderContextBudgetExceeded 拒绝发送。
func (c *Coordinator) compressExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	summary string,
	planMessage string,
	currentInput string,
	tools []model.Tool,
	budget task_context.ContextBudget,
) ([]contract.EngineMessage, int, bool) {
	history := append(RetainedSystemOnly(systems), contract.EngineMessage{
		Role: "system", Content: AutonomousCompactionMessage(summary), ContentSet: true,
	})
	if planMessage != "" {
		history = append(history, contract.EngineMessage{Role: "system", Content: planMessage, ContentSet: true})
	}
	estimated := c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
	if estimated > budget.Budget {
		return nil, estimated, false
	}
	return history, estimated, true
}

// AutonomousCompactionMessage 渲染自主压缩帧正文（system 消息）：显式告知
// 模型上下文已被框架压缩、必须从有界 checkpoint 继续，并以窄化工具调用
// 补取细节——避免模型基于想象补全被折叠的内容。
func AutonomousCompactionMessage(summary string) string {
	var builder strings.Builder
	builder.WriteString(AutonomousCompactionPrefix)
	builder.WriteString("\n## Autonomous Context Compaction\n")
	builder.WriteString("The conversation exceeded the provider context budget and was compacted automatically. ")
	builder.WriteString("Continue from the bounded task checkpoint below without assuming omitted details. ")
	builder.WriteString("Reacquire omitted detail with narrow, paginated, or filtered tool calls; do not request a full large result.\n")
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		builder.WriteString("\n")
		builder.WriteString(trimmed)
		builder.WriteString("\n")
	} else {
		builder.WriteString("\nNo durable checkpoint evidence is available; rely on the current request and re-read as needed.\n")
	}
	return builder.String()
}

// retainedMatchesTranscriptPrefix 判定引擎保留段（非 system 的已定稿轮次）
// 是否与 transcript 事件流的前缀一一对应。正常续跑时引擎历史就是 transcript
// 前缀，covered 计数可直接用于跳过；冷恢复（resumeSessionCold 只装载尾部
// 窗口）时保留段对应 transcript 后缀，若仍按 covered 跳过会把中段事件
// 追加到尾部之后，造成上下文重排/重复。比较以 Role+Content 为准（工具轮
// 的 ToolCalls 只影响 wire 展示，不改变覆盖判断）。
func retainedMatchesTranscriptPrefix(systems []contract.EngineMessage, events []model.TranscriptEvent) bool {
	retained := make([]contract.EngineMessage, 0, len(systems))
	for _, message := range systems {
		if message.Role != "system" {
			retained = append(retained, message)
		}
	}
	if len(retained) == 0 || len(retained) > len(events) {
		return false
	}
	for index := range retained {
		if retained[index].Role != events[index].Role ||
			retained[index].Content != events[index].Content {
			return false
		}
	}
	return true
}

func (c *Coordinator) planContextMessageLocked(sessionID string) string {
	projection := c.tasks.ActivePlanProjectionLockedFor(sessionID)
	if projection == nil || projection.Status == string(model.PlanCompleted) {
		return ""
	}
	payload := map[string]any{
		"plan_ref": projection.CanonicalPlanRef, "plan_id": projection.PlanID,
		"status": projection.Status, "current": projection.CurrentNode,
		"completed": projection.CompletedNodes, "failed": projection.FailedNodes,
		"pending": projection.PendingNodes,
	}
	if frame := task_context.ActivePlanFrame(c.tasks.PlanStackFor(sessionID), c.tasks.ActivePlanIDFor(sessionID)); frame != nil {
		payload["current_slice"] = currentPlanSlice(frame.Arguments, projection.CurrentNode)
	}
	encoded, _ := json.Marshal(payload)
	// plan 执行指令并入尾部（原本在 system 的 Active Plan Execution Policy 段，
	// 会随 plan 加载/完成改写 system 头部 → 一次前缀悬崖）。尾部消息每轮随节点
	// 状态重建，本来就在缓存未命中区，指令放这里零额外失效成本。
	return planContextPrefix + "\n" + string(encoded) + "\n\n## Active Plan Execution Policy\n" +
		"The Plan is validated and authoritative for this task. Do not silently replace or reorder it. " +
		"Execute the current node and its declared dependencies in stable order. Use read_plan for omitted node detail. " +
		"plan_ref=" + projection.CanonicalPlanRef
}

func currentPlanSlice(arguments, currentNode string) any {
	var plan struct {
		Nodes map[string]json.RawMessage `json:"nodes"`
		Edges map[string][]string        `json:"edges"`
	}
	if json.Unmarshal([]byte(arguments), &plan) != nil {
		return nil
	}
	nodes := make(map[string]json.RawMessage)
	edges := make(map[string][]string)
	if node, ok := plan.Nodes[currentNode]; ok {
		nodes[currentNode] = node
	}
	for source, targets := range plan.Edges {
		if source == currentNode {
			edges[source] = append([]string(nil), targets...)
			for _, target := range targets {
				if node, ok := plan.Nodes[target]; ok {
					nodes[target] = node
				}
			}
		}
		for _, target := range targets {
			if target == currentNode {
				edges[source] = task_context.AppendUniqueStrings(edges[source], target)
				if node, ok := plan.Nodes[source]; ok {
					nodes[source] = node
				}
			}
		}
	}
	return map[string]any{"nodes": nodes, "edges": edges}
}

func excludeCurrentInputEvent(events []model.TranscriptEvent, requestID, currentInput string) []model.TranscriptEvent {
	if currentInput == "" || len(events) == 0 {
		return events
	}
	last := events[len(events)-1]
	if last.TaskID == requestID && last.Role == "user" {
		return events[:len(events)-1]
	}
	return events
}

// protectOversizedCurrentInputLocked 把超**单条**预算的当前输入归档为引用：
// 阈值 = budget.SingleItemInputLimit（预算 × context_single_item_percent，默认
// 50%）——比例来自配置，见 newContextBudget。limit <= 0 表示未设置，不做外置。
func (c *Coordinator) protectOversizedCurrentInputLocked(sessionID, requestID, currentInput string, budget task_context.ContextBudget) string {
	if currentInput == "" || budget.SingleItemInputLimit <= 0 ||
		c.tasks.CountTextTokens(currentInput) <= budget.SingleItemInputLimit {
		return currentInput
	}
	stored := c.tasks.StoreToolResultForLocked(sessionID, "user_input", currentInput)
	warning := ContentReferenceWarning(stored.Ref)
	transcript := c.tasks.TranscriptFor(sessionID)
	for index := len(transcript) - 1; index >= 0; index-- {
		event := &transcript[index]
		if event.TaskID == requestID && event.Role == "user" {
			event.Content = warning
			event.ResultRef = stored.Ref
			event.TokenCount = c.tasks.CountTranscriptEvent(*event)
			break
		}
	}
	return warning
}

// ContentReferenceWarning 是超限用户输入归档引用警告文本。
func ContentReferenceWarning(resultRef string) string {
	return "<seelex-content-reference>\nresult_ref=" + resultRef + "\n" +
		"The user input is stored out of band because it exceeds the single-item context budget. " +
		"Use read_tool_result with pagination or filtering.\n</seelex-content-reference>"
}

// FrameworkToolOutputTruncatedMarker 是框架截断工具输出标记。
const FrameworkToolOutputTruncatedMarker = "\n...[truncated]"

// rejectOversizedToolResults 把超限输出替换为显式重试指令（不给头部/尾部
// 预览，避免基于误导片段的推理；目标会话显式传入）。
func (c *Coordinator) rejectOversizedToolResults(sessionID string, maxChars int) (bool, error) {
	history := c.foldHistory(sessionID)
	c.ViewMu.RLock()
	refs := c.tasks.ResultRefsByCallIDFor(sessionID)
	c.ViewMu.RUnlock()
	filtered, changed := rejectToolResultsWithRefs(history, maxChars, refs)
	if !changed {
		return false, nil
	}
	if err := c.replaceFoldHistory(sessionID, filtered); err != nil {
		return false, fmt.Errorf("reject oversized tool results: %w", err)
	}
	return true, nil
}

// RejectToolResults 替换超限工具结果为显式引用警告（纯函数面）。
func RejectToolResults(history []contract.EngineMessage, maxChars int) ([]contract.EngineMessage, bool) {
	return rejectToolResultsWithRefs(history, maxChars, nil)
}

func rejectToolResultsWithRefs(history []contract.EngineMessage, maxChars int, refs map[string]string) ([]contract.EngineMessage, bool) {
	filtered := append([]contract.EngineMessage(nil), history...)
	changed := false
	for index := range filtered {
		message := &filtered[index]
		if message.Role != "tool" || !IsOversizedToolResult(message.Content, maxChars) {
			continue
		}
		message.Content = OversizedToolResultWarning(message.Name, refs[message.ToolCallID])
		message.ContentSet = true
		changed = true
	}
	return filtered, changed
}

// IsOversizedToolResult 判定工具结果是否超限（或带框架截断标记）。
func IsOversizedToolResult(content string, maxChars int) bool {
	return len(content) > maxChars || strings.HasSuffix(content, FrameworkToolOutputTruncatedMarker)
}

// OversizedToolResultWarning 是超限工具结果归档警告文本。
func OversizedToolResultWarning(name, resultRef string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	var builder strings.Builder
	builder.WriteString(ToolResultOmittedPrefix + "\n")
	builder.WriteString("tool=")
	builder.WriteString(name)
	builder.WriteByte('\n')
	if resultRef != "" {
		builder.WriteString("result_ref=")
		builder.WriteString(resultRef)
		builder.WriteByte('\n')
	}
	builder.WriteString("The result exceeded the provider-context item budget; raw content was not included.\n")
	builder.WriteString("Do not infer facts from omitted content. Use read_tool_result with pagination or filtering, or issue a narrower read-only query.\n")
	builder.WriteString("</seelex-tool-result-omitted>")
	return builder.String()
}

// ProviderSafeToolResult 把超限工具结果替换为警告（provider 路径）。
func ProviderSafeToolResult(name, result string, toolErr error) string {
	if toolErr != nil || !IsOversizedToolResult(result, limits.Get().MaxToolResultChars) {
		return result
	}
	return OversizedToolResultWarning(name, "")
}

// EstimateEngineHistoryTokens 估算引擎历史 token 数（纯函数面）。
func EstimateEngineHistoryTokens(history []contract.EngineMessage) int {
	tokens := 0
	for _, message := range history {
		tokens += seelexctx.EstimateTokens(message.Content)
		tokens += seelexctx.EstimateTokens(message.ReasoningContent)
		for _, call := range message.ToolCalls {
			tokens += seelexctx.EstimateTokens(call.Name) + seelexctx.EstimateTokens(call.Arguments)
		}
	}
	return tokens
}

// TaskContextRecoveryHistory 保留 system 指令并把可变协议记录替换为 checkpoint。
func TaskContextRecoveryHistory(history []contract.EngineMessage, checkpoint string) []contract.EngineMessage {
	compacted := RetainedSystemOnly(history)
	return append(compacted, contract.EngineMessage{Role: "system", Content: checkpoint, ContentSet: true})
}

// RetainedSystemHistory 保留稳定前缀 + 已定稿轮次的 append-only 累积段：
// 从既有引擎历史中剔除动态尾部（plan 上下文 / checkpoint / 压缩帧标记 /
// 恢复信封），使下一轮装配只追加保留段之后的新事件，前缀字节稳定。
// 恢复路径（provider 504 / history-safety）请用 RetainedSystemOnly。
func RetainedSystemHistory(history []contract.EngineMessage) []contract.EngineMessage {
	retained := make([]contract.EngineMessage, 0, len(history))
	for _, message := range history {
		if !isDynamicTailMessage(message) {
			retained = append(retained, message)
		}
	}
	return retained
}

// RetainedSystemOnly 保留一条产品指令（框架侧摘要也是 system 消息，全保留
// 会让错误/重复的历史替换成倍放大 prompt）。恢复路径专用：不得携带已定稿
// 轮次（provider 已拒绝过大上下文，重放 transcript 会再次失败）。
func RetainedSystemOnly(history []contract.EngineMessage) []contract.EngineMessage {
	for _, message := range history {
		if message.Role == "system" {
			return []contract.EngineMessage{message}
		}
	}
	return nil
}

// isDynamicTailMessage 判定消息是否为动态尾部/控制消息（plan 上下文、
// checkpoint、压缩帧标记、恢复信封、预算终局输入、自主压缩帧）：这类消息每轮
// 重建或由恢复路径单独管理，不进入保留的稳定前缀 + 已定稿累积段。激活技能
// 事件不在其列——它是 append-only 的定稿轮次，由保留段照常携带并计数。
func isDynamicTailMessage(message contract.EngineMessage) bool {
	content := message.Content
	return strings.HasPrefix(content, planContextPrefix) ||
		strings.HasPrefix(content, TaskContextCheckpointPrefix) ||
		strings.HasPrefix(content, seelexctx.CompactContextMarker) ||
		strings.HasPrefix(content, contextRecoveryPrefix) ||
		strings.HasPrefix(content, providerRecoveryPrefix) ||
		strings.HasPrefix(content, reactBudgetFinalizationPrefix) ||
		strings.HasPrefix(content, AutonomousCompactionPrefix)
}

// retainedContextEventCount 返回保留段中已定稿轮次的 message 数（与
// transcript 事件 1:1，供累积模式跳过已保留前缀）。
func retainedContextEventCount(retained []contract.EngineMessage) int {
	count := 0
	for _, message := range retained {
		if message.Role != "system" {
			count++
		}
	}
	return count
}

// RemoveTaskContextCheckpoints 阻止 Application 控制消息被持久化/重建为
// 前端会话占位（活跃会话兼容包装）。
func (c *Coordinator) RemoveTaskContextCheckpoints() error {
	return c.RemoveTaskContextCheckpointsFor(c.tasks.SessionIDForRequest(""))
}

// RemoveTaskContextCheckpointsFor 阻止 Application 控制消息被持久化/重建为
// 前端会话占位（目标会话由调用方显式传入）。
func (c *Coordinator) RemoveTaskContextCheckpointsFor(sessionID string) error {
	history := c.engineHistory(sessionID)
	filtered := make([]contract.EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && strings.HasPrefix(message.Content, TaskContextCheckpointPrefix) {
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := c.replaceEngineHistory(sessionID, filtered); err != nil {
		return fmt.Errorf("remove task context checkpoint: %w", err)
	}
	return nil
}

// IsTaskContextCheckpoint 判定内容是否为上下文 checkpoint 标记。
func IsTaskContextCheckpoint(content string) bool {
	return strings.HasPrefix(content, TaskContextCheckpointPrefix)
}

// RecordContextControlFailure 把 hook 失败转移给 runChat（委托 task 域）。
func (c *Coordinator) RecordContextControlFailure(requestID string, err error) {
	c.tasks.RecordContextControlFailure(requestID, err)
}

// TakeContextControlFailure 取走当前请求的 context 控制失败（委托 task 域）。
func (c *Coordinator) TakeContextControlFailure(requestID string) error {
	return c.tasks.TakeContextControlFailure(requestID)
}

// engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
// 引擎）。
func (c *Coordinator) engineHistory(sessionID string) []contract.EngineMessage {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return c.Deps.Engine.History()
}

// setEngineSystemPrompt 设置指定会话引擎 system prompt（会话路由引擎用
// SetSystemPromptFor，否则活跃引擎）。
func (c *Coordinator) setEngineSystemPrompt(sessionID, prompt string) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.SetSystemPromptFor(sessionID, prompt)
		return
	}
	c.Deps.Engine.SetSystemPrompt(prompt)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (c *Coordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return c.Deps.Engine.ReplaceHistory(sessionID, history)
}

// clearEngineHistory 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
// 否则活跃引擎）。
func (c *Coordinator) clearEngineHistory(sessionID string) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.ClearHistoryFor(sessionID)
		return
	}
	c.Deps.Engine.ClearHistory()
}

// appendEngineHistory 追加消息到指定会话引擎历史（会话路由引擎用
// AppendHistoryFor，否则活跃引擎）。
func (c *Coordinator) appendEngineHistory(sessionID string, msg types.Message) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.AppendHistoryFor(sessionID, msg)
		return
	}
	c.Deps.Engine.AppendHistory(msg)
}
