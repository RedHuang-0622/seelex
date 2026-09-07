// Package view_state owns the user-visible Snapshot, message sequence, event
// revision and runtime projection. It embeds the shared state kernel; runtime
// projection collection happens lock-free on external ports, and applying
// happens under Core.ViewMu. Worktable refresh is injected as a function port so
// the view never reaches into the work table implementation.
package view_state

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/session"
)

// SubagentContextMarker 标记子代理产出块（模型不误读为普通用户轮次）。
const SubagentContextMarker = "[子代理产出] "

// Deps 是 view_state 的装配输入。
type Deps struct {
	Core *state.Core
	// Units 是会话域（阶段 B：每会话可见投影的唯一所有者；本域只读/写单元
	// 内的 View，不自行持有会话容器）。
	Units *session.Domain
	// CurrentEffort 返回指定会话的 effort 级别（prompt 域；runtime 投影用；
	// G4：会话优先，未选择回退进程默认）。
	CurrentEffort func(sessionID string) string
	// CurrentFullAccess 返回指定会话生效的全权模式（G4：会话选择优先，
	// 未选择回退进程默认/引擎门值）。
	CurrentFullAccess func(sessionID string) bool
	// CurrentSessionID 返回当前视图指针会话（线程安全；投影收集在锁外
	// 读取视图归属——视图指针在 session.Domain actor，不经快照镜像）。
	CurrentSessionID func() string
	// RefreshWorkTableLocked 在锁内重建工作表格投影（work_table 域；
	// 调用方已持有 Core.ViewMu）。
	RefreshWorkTableLocked func(tasks []dto.TaskRecord)
	// Tasks 提供任务级 skill 激活投影（「目标」面板数据源）。
	Tasks interface {
		ActiveSkillIDs() []string
		GoalSkillActive() bool
	}
	// Goals 提供会话级 goal 治理只读视图（无 goal 返回 nil；前端据此渲染
	// 「目标 + 治理」面板与心跳）。nil 时投影留空（未装配 goal 协调器）。
	Goals interface {
		GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	}
	// Limits 返回当前生效的运行时上限（窗口配置）。
	Limits func() seelexctx.Limits
}

// Coordinator 拥有用户可见快照、消息序列与事件修订号。
type Coordinator struct {
	*state.Core
	units                  *session.Domain
	currentEffort          func(string) string
	currentFullAccess      func(string) bool
	currentSessionID       func() string
	refreshWorkTableLocked func([]dto.TaskRecord)
	tasks                  interface {
		ActiveSkillIDs() []string
		GoalSkillActive() bool
	}
	goals interface {
		GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	}
	limits     func() seelexctx.Limits
	messageSeq uint64
}

// NewCoordinator 构造 view 域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:                   deps.Core,
		units:                  deps.Units,
		currentEffort:          deps.CurrentEffort,
		currentFullAccess:      deps.CurrentFullAccess,
		currentSessionID:       deps.CurrentSessionID,
		refreshWorkTableLocked: deps.RefreshWorkTableLocked,
		tasks:                  deps.Tasks,
		goals:                  deps.Goals,
		limits:                 deps.Limits,
	}
}

// RuntimeStateProjection 是一次锁外收集的 runtime 投影（应用时只拷贝
// 已拥有的值）。
type RuntimeStateProjection struct {
	SessionID string
	Runtime   model.RuntimeState
	Tasks     []dto.TaskRecord
}

// SnapshotView 返回权威快照深拷贝。
func (c *Coordinator) SnapshotView() model.Snapshot {
	c.ViewMu.RLock()
	snapshot := model.CloneSnapshot(c.Snapshot)
	c.ViewMu.RUnlock()
	return snapshot
}

// Subscribe 订阅事件流。
func (c *Coordinator) Subscribe(buffer int) event.Subscription {
	return c.Events.Subscribe(buffer)
}

// CollectRuntimeProjection 锁外调用外部端口收集当前视图会话的 runtime
// 投影（M3/M5：引擎活跃别名不是事实源——草稿早分配 SID 后引擎
// SessionID() 在未建 bundle 阶段为空，路由必须读视图指针）。
func (c *Coordinator) CollectRuntimeProjection(ctx context.Context) RuntimeStateProjection {
	return c.CollectRuntimeProjectionFor(ctx, c.viewSessionID())
}

// viewSessionID 返回当前视图指针会话（装配注入的 Domain.ActiveID；未注入
// 时回退快照镜像——仅直接构造的测试桩路径）。
func (c *Coordinator) viewSessionID() string {
	if c.currentSessionID != nil {
		return c.currentSessionID()
	}
	return c.Snapshot.Session.ID
}

// CollectRuntimeProjectionFor 按显式会话收集 runtime 投影（G1：会话槽的
// 写入方必须知道自己的 sid，不能回读引擎活跃别名——后台会话运行时引擎
// 别名仍指向视图会话）。已有 per-session 端口优先（tokens/tasks/skills），
// 未具备的字段（todo 清单、subagent 树、replan 指标）在 G3/G4 分型前
// 暂时保留进程/视图口径，槽位内容以回合尾与工具边界的 For 收集为准。
func (c *Coordinator) CollectRuntimeProjectionFor(ctx context.Context, sessionID string) RuntimeStateProjection {
	effort := ""
	if c.currentEffort != nil {
		effort = c.currentEffort(sessionID)
	}
	projection := RuntimeStateProjection{
		SessionID: sessionID,
		Runtime: model.RuntimeState{
			Model:             c.Deps.Runtime.Model(),
			Provider:          c.Deps.Runtime.Provider(),
			Plugin:            c.Deps.Runtime.ActivePlugin(),
			Effort:            effort,
			FullAccess:        c.fullAccessFor(sessionID),
			VisibleTools:      append([]model.Tool(nil), c.Deps.Runtime.VisibleTools(ctx)...),
			Skills:            append([]model.SkillInfo(nil), c.Deps.Skills.All()...),
			Tokens:            c.tokenCountFor(sessionID),
			Plugins:           append([]model.PluginInfo(nil), c.Deps.Plugins.All()...),
			Accounts:          append([]model.AccountInfo(nil), c.Deps.Runtime.Accounts()...),
			TodoItems:         append([]dto.TodoItem(nil), c.Deps.Runtime.TodoSnapshot()...),
			ScheduledTasks:    append([]seelebridge.ScheduledTaskStatus(nil), c.Deps.Runtime.ScheduledTasksSnapshot()...),
			ScheduledCommands: append([]seelebridge.ScheduledCommandInfo(nil), c.Deps.Runtime.ScheduledCommands()...),
			SubAgentTree:      c.Deps.Engine.SubAgentTree(),
		},
	}
	if c.tasks != nil {
		projection.Runtime.ActiveSkills = append([]string(nil), c.activeSkillIDsFor(sessionID)...)
		projection.Runtime.GoalSkillActive = c.goalSkillActiveFor(sessionID)
	}
	if c.goals != nil {
		if goalView := c.goals.GoalGovernanceViewFor(sessionID); goalView != nil {
			copyView := *goalView
			projection.Runtime.GoalGovernance = &copyView
		}
	}
	metrics := c.replanMetricsFor(sessionID)
	projection.Runtime.Replan = model.ReplanMonitor{
		InFlight: metrics.InFlight, ConcurrentLimit: metrics.ConcurrentLimit,
		WindowAttempts: metrics.WindowAttempts, WindowLimit: metrics.WindowLimit,
		WindowStartedAt: metrics.WindowStartedAt, Accepted: metrics.Accepted,
		Succeeded: metrics.Succeeded, Failed: metrics.Failed, Rejected: metrics.Rejected,
		DuplicateRejected: metrics.DuplicateRejected, ProviderRequests: metrics.ProviderRequests,
		ProviderWindowRequests: metrics.ProviderWindowRequests, ProviderWindowLimit: metrics.ProviderWindowLimit,
	}
	// 任务表收集口径：视图会话读实时注册表（materialize 后 runtime 的
	// currentTaskSession 可能尚未切换，For 会取到空分区）；后台会话读自身
	// 分区快照（TaskAddFor 写自有域）。
	projection.Tasks = c.Deps.Runtime.TaskSnapshot()
	if sessionID != "" && sessionID != c.viewSessionID() {
		if forTasks, ok := c.Deps.Runtime.(interface {
			TaskSnapshotFor(string) []dto.TaskRecord
		}); ok {
			projection.Tasks = forTasks.TaskSnapshotFor(sessionID)
		}
	}
	return projection
}

// fullAccessFor 返回指定会话生效的全权模式（G4：会话选择优先；未选择时
// 回退进程默认/引擎门值）。
func (c *Coordinator) fullAccessFor(sessionID string) bool {
	if c.currentFullAccess != nil {
		return c.currentFullAccess(sessionID)
	}
	return c.Deps.Runtime.FullAccess()
}

// replanMetricsFor 返回指定会话的 replan 统计（per-session 端口优先；
// 未实现时回退进程级合计）。
func (c *Coordinator) replanMetricsFor(sessionID string) dto.ReplanMetrics {
	if forMetrics, ok := c.Deps.Runtime.(interface {
		ReplanMetricsFor(string) dto.ReplanMetrics
	}); ok {
		return forMetrics.ReplanMetricsFor(sessionID)
	}
	return c.Deps.Runtime.ReplanMetrics()
}

// tokenCountFor 返回指定会话的 token 计数（有 per-session 端口优先；
// 否则回退全局合计——单会话宿主语义不变）。
func (c *Coordinator) tokenCountFor(sessionID string) string {
	if forTokens, ok := c.Deps.Engine.(interface{ TokenCountFor(string) string }); ok {
		return forTokens.TokenCountFor(sessionID)
	}
	return c.Deps.Engine.TokenCount()
}

// activeSkillIDsFor 返回指定会话的任务激活 skill ID（per-session 端口
// 优先；未实现时回退活跃视图口径）。
func (c *Coordinator) activeSkillIDsFor(sessionID string) []string {
	if forSkills, ok := c.tasks.(interface{ ActiveSkillIDsFor(string) []string }); ok {
		return forSkills.ActiveSkillIDsFor(sessionID)
	}
	return c.tasks.ActiveSkillIDs()
}

// goalSkillActiveFor 返回指定会话的 goal skill 激活判定。
func (c *Coordinator) goalSkillActiveFor(sessionID string) bool {
	if forSkills, ok := c.tasks.(interface{ GoalSkillActiveFor(string) bool }); ok {
		return forSkills.GoalSkillActiveFor(sessionID)
	}
	return c.tasks.GoalSkillActive()
}

// ApplyRuntimeProjectionLocked 应用 runtime 投影到当前活跃会话（保留 Plan/
// Account 指针，锁内重建工作表格；委托 For 变体）。
func (c *Coordinator) ApplyRuntimeProjectionLocked(projection RuntimeStateProjection) {
	c.ApplyRuntimeProjectionForLocked(c.Snapshot.Session.ID, projection)
}

// ApplyRuntimeProjectionForLocked 应用 runtime 投影到指定会话（G1）：
// 投影写该会话自己的 Runtime 槽；仅当 sid 是视图指针时镜像
// Snapshot.Runtime 并重建视图工作表格。调用方持有 Core.ViewMu。
func (c *Coordinator) ApplyRuntimeProjectionForLocked(sessionID string, projection RuntimeStateProjection) {
	unit := c.units.Unit(sessionID)
	if unit == nil {
		if sessionID == "" {
			unit = session.NewDraftUnit()
		} else {
			unit, _ = session.NewSessionUnit(sessionID)
		}
		c.units.Register(unit)
	}
	existing := unit.RuntimeState()
	plan := existing.Plan
	account := existing.Account
	if sessionID == c.Snapshot.Session.ID {
		plan = c.Snapshot.Runtime.Plan
		account = c.Snapshot.Runtime.Account
	}
	// 视图会话身份解耦：Snapshot.Session.ID 只由装配/物化/恢复/热挂载/卸载
	// 路径显式设置，绝不随引擎活跃别名（projection.SessionID）被覆写——否则
	// 热挂载运行中会话后，引擎旧别名会在下一次 runtime 投影时把视图顶回旧
	// 会话（工具/LLM 事件串会话、视图表现混乱、动态更新丢失）。
	runtime := projection.Runtime
	if runtime.Plan == nil {
		runtime.Plan = plan
	}
	if runtime.Account == "" {
		runtime.Account = account
	}
	unit.SetRuntimeState(runtime)
	if sessionID == c.Snapshot.Session.ID {
		c.Snapshot.Runtime = runtime
		c.refreshWorkTableLocked(projection.Tasks)
	}
}

// AppendMessageLocked 追加一条可见消息到当前活跃会话（调用方持有
// Core.ViewMu；委托 AppendMessageLockedFor）。
func (c *Coordinator) AppendMessageLocked(role, content string, tool *model.ToolCall) *model.Message {
	return c.AppendMessageLockedFor(c.Snapshot.Session.ID, role, content, tool)
}

// AppendMessageLockedFor 追加一条可见消息到指定会话（阶段 1：可见对话收进
// 每会话 SessionView；活跃会话同步镜像 Snapshot，后台会话只写自身 scope，
// hot_attach 回看有数据）。
func (c *Coordinator) AppendMessageLockedFor(sessionID, role, content string, tool *model.ToolCall) *model.Message {
	if role == "assistant" || role == "tool_result" {
		content = chat.StripThoughtBlocks(content)
	}
	view := c.sessionViewLocked(sessionID)
	var message *model.Message
	view.Mutate(func(v *session.View) {
		c.messageSeq++
		next := model.Message{ID: fmt.Sprintf("message-%d", c.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
		v.Conversation = append(v.Conversation, next)
		if role != "system" {
			v.TotalMessages++
		}
		c.boundViewTailLocked(v)
		message = &v.Conversation[len(v.Conversation)-1]
	})
	c.mirrorActiveViewLocked(sessionID, view)
	if message != nil && message.ID != "" {
		return message
	}
	return message
}

// sessionViewLocked 返回指定会话的可见投影（按需创建会话域单元；调用方持有
// Core.ViewMu，域内锁序为 Core.ViewMu → Domain.mu，不反向）。
func (c *Coordinator) sessionViewLocked(sessionID string) *session.View {
	unit := c.units.Unit(sessionID)
	if unit == nil {
		if sessionID == "" {
			unit = session.NewDraftUnit()
		} else {
			unit, _ = session.NewSessionUnit(sessionID)
		}
		c.units.Register(unit)
	}
	return unit.View
}

// SessionViewLocked 返回指定会话的可见投影（core 域恢复/回看路径用；
// 调用方持有 Core.ViewMu）。
func (c *Coordinator) SessionViewLocked(sessionID string) *session.View {
	return c.sessionViewLocked(sessionID)
}

// SessionViewMutateLocked 在指定会话可见投影的 View.mu 内应用变更（G5 访问
// 器化：View 字段写一律经 View.mu，调用方持有 ViewMu 时同样安全——View.mu
// 是叶子锁，任何路径都不在持 View.mu 时反向获取 ViewMu）。
func (c *Coordinator) SessionViewMutateLocked(sessionID string, mutate func(*session.View)) {
	view := c.sessionViewLocked(sessionID)
	if view == nil || mutate == nil {
		return
	}
	view.Mutate(mutate)
}

// SessionViewReadLocked 在指定会话可见投影的 View.mu（读）内读取字段快照
// （G5 访问器化：与 Mutate 同锁序，镜像/增量/工具写回共用 View.mu 叶子锁）。
func (c *Coordinator) SessionViewReadLocked(sessionID string, read func(*session.View)) {
	view := c.sessionViewLocked(sessionID)
	if view == nil || read == nil {
		return
	}
	view.Read(read)
}

// SetSessionViewLocked 装载指定会话的可见投影（冷加载/恢复路径；调用方
// 持有 Core.ViewMu）。活跃会话同步镜像 Snapshot。
func (c *Coordinator) SetSessionViewLocked(sessionID string, view *session.View) {
	if view == nil {
		return
	}
	unit := c.units.Unit(sessionID)
	if unit == nil {
		if sessionID == "" {
			unit = session.NewDraftUnit()
		} else {
			unit, _ = session.NewSessionUnit(sessionID)
		}
		c.units.Register(unit)
	}
	// G5 访问器化：单元 View 指针一经注册不再被整体替换（避免与并发读
	// unit.View 的路径构成指针字段竞争）；装载在 View.mu 内逐字段拷贝。
	unit.View.Mutate(func(target *session.View) {
		target.Conversation = append([]model.Message(nil), view.Conversation...)
		target.Chat = view.Chat
		target.ReadFiles = append([]model.ReadFileRef(nil), view.ReadFiles...)
		target.TotalMessages = view.TotalMessages
		target.HistoryOffset = view.HistoryOffset
		target.HasMoreHistory = view.HasMoreHistory
		target.ConversationWindow = view.ConversationWindow
	})
	c.mirrorActiveViewLocked(sessionID, unit.View)
}

// SetSessionChatLockedFor 写指定会话的聊天运行态投影（调用方持有
// Core.ViewMu；活跃会话同步镜像 Snapshot.Chat）。
func (c *Coordinator) SetSessionChatLockedFor(sessionID string, chat model.ChatState) {
	view := c.sessionViewLocked(sessionID)
	view.Mutate(func(v *session.View) { v.Chat = chat })
	if sessionID == c.Snapshot.Session.ID {
		c.Snapshot.Chat = chat
	}
}

// SetReadFilesFor 写指定会话的 read 文件引用投影（调用方持有 Core.ViewMu）。
func (c *Coordinator) SetReadFilesFor(sessionID string, readFiles []model.ReadFileRef) {
	view := c.sessionViewLocked(sessionID)
	view.Mutate(func(v *session.View) {
		v.ReadFiles = append([]model.ReadFileRef(nil), readFiles...)
	})
	c.mirrorActiveViewLocked(sessionID, view)
}

// mirrorActiveViewLocked 把指定会话的 scope 镜像到 Snapshot（仅当目标为
// 当前活跃会话；调用方持有 Core.ViewMu）。
func (c *Coordinator) mirrorActiveViewLocked(sessionID string, view *session.View) {
	if sessionID != c.Snapshot.Session.ID {
		return
	}
	view.Read(func(v *session.View) {
		c.Snapshot.Conversation = append([]model.Message(nil), v.Conversation...)
		c.Snapshot.ReadFiles = append([]model.ReadFileRef(nil), v.ReadFiles...)
		c.Snapshot.TotalMessages = v.TotalMessages
		c.Snapshot.HistoryOffset = v.HistoryOffset
		c.Snapshot.HasMoreHistory = v.HasMoreHistory
		c.Snapshot.ConversationWindow = v.ConversationWindow
	})
}

// MirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot（切换/恢复
// 后调用；调用方持有 Core.ViewMu）。
func (c *Coordinator) MirrorActiveViewLocked() {
	view := c.sessionViewLocked(c.Snapshot.Session.ID)
	c.mirrorActiveViewLocked(c.Snapshot.Session.ID, view)
}

// AdvanceMessageSeqLocked 按既有消息 ID 推进消息序列（会话恢复路径）。
func (c *Coordinator) AdvanceMessageSeqLocked(messages []model.Message) {
	for _, message := range messages {
		if !strings.HasPrefix(message.ID, "message-") {
			continue
		}
		sequence, err := strconv.ParseUint(strings.TrimPrefix(message.ID, "message-"), 10, 64)
		if err == nil && sequence > c.messageSeq {
			c.messageSeq = sequence
		}
	}
}

// NextMessageSeqLocked 返回下一条消息序号并推进（分页加载 ID 分配用）。
func (c *Coordinator) NextMessageSeqLocked() uint64 {
	c.messageSeq++
	return c.messageSeq
}

func (c *Coordinator) boundViewTailLocked(view *session.View) {
	window := c.limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	view.ConversationWindow = window
	view.Conversation = BoundConversationTail(view.Conversation, window)
	visible := durableConversationCount(view.Conversation)
	view.HistoryOffset = view.TotalMessages - visible
	if view.HistoryOffset < 0 {
		view.HistoryOffset = 0
	}
	view.HasMoreHistory = view.HistoryOffset > 0
}

func durableConversationCount(messages []model.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role != "system" {
			count++
		}
	}
	return count
}

// BoundConversationTail 保留尾部窗口（system 与普通消息分列计数）。
func BoundConversationTail(messages []model.Message, window int) []model.Message {
	if window <= 0 {
		return nil
	}
	keep := make([]bool, len(messages))
	nonSystem, system := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "system" {
			if system < window {
				keep[index] = true
				system++
			}
			continue
		}
		if nonSystem < window {
			keep[index] = true
			nonSystem++
		}
	}
	bounded := make([]model.Message, 0, nonSystem+system)
	for index, message := range messages {
		if keep[index] {
			bounded = append(bounded, message)
		}
	}
	return bounded
}

// BoundConversationHead 保留头部窗口（分页加载前置用）。
func BoundConversationHead(messages []model.Message, window int) []model.Message {
	if window <= 0 {
		return nil
	}
	bounded := make([]model.Message, 0, min(len(messages), window*2))
	nonSystem, system := 0, 0
	for _, message := range messages {
		if message.Role == "system" {
			if system < window {
				bounded = append(bounded, message)
				system++
			}
			continue
		}
		if nonSystem < window {
			bounded = append(bounded, message)
			nonSystem++
		}
	}
	return bounded
}

// BumpLocked 递增快照修订号。
func (c *Coordinator) BumpLocked() uint64 {
	c.Snapshot.Revision++
	return c.Snapshot.Revision
}

// AddNotice 追加一条系统通知并发布事件。
func (c *Coordinator) AddNotice(notice string) {
	if strings.TrimSpace(notice) == "" {
		return
	}
	c.ViewMu.Lock()
	message := *c.AppendMessageLocked("system", notice, nil)
	revision := c.BumpLocked()
	sessionID := c.Snapshot.Session.ID
	c.ViewMu.Unlock()
	if hub, ok := c.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(event.EventMessageAdded, revision, "", sessionID, message)
	} else {
		c.Events.Publish(event.EventMessageAdded, revision, "", message)
	}
}

// ResetConversation 清空可见会话并注入 CLI 标识与通知。
func (c *Coordinator) ResetConversation(notice string) {
	modelName := c.Deps.Runtime.Model()
	c.ViewMu.Lock()
	c.Snapshot.Conversation = nil
	c.AppendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", modelName), nil)
	if notice != "" {
		c.AppendMessageLocked("system", notice, nil)
	}
	revision := c.BumpLocked()
	sessionID := c.Snapshot.Session.ID
	c.ViewMu.Unlock()
	if hub, ok := c.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(event.EventSnapshotChanged, revision, "", sessionID, nil)
	} else {
		c.Events.Publish(event.EventSnapshotChanged, revision, "", nil)
	}
}
