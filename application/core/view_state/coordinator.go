// Package view_state owns the user-visible Snapshot, message sequence, event
// revision and runtime projection. It embeds the shared state kernel; runtime
// projection collection happens lock-free on external ports, and applying
// happens under Core.Mu. Worktable refresh is injected as a function port so
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
)

// SubagentContextMarker 标记子代理产出块（模型不误读为普通用户轮次）。
const SubagentContextMarker = "[子代理产出] "

// Deps 是 view_state 的装配输入。
type Deps struct {
	Core *state.Core
	// CurrentEffort 返回当前 effort 级别（prompt 域；runtime 投影用）。
	CurrentEffort func() string
	// RefreshWorkTableLocked 在锁内重建工作表格投影（work_table 域；
	// 调用方已持有 Core.Mu）。
	RefreshWorkTableLocked func(tasks []dto.TaskRecord)
	// Limits 返回当前生效的运行时上限（窗口配置）。
	Limits func() seelexctx.Limits
}

// Coordinator 拥有用户可见快照、消息序列与事件修订号。
type Coordinator struct {
	*state.Core
	currentEffort          func() string
	refreshWorkTableLocked func([]dto.TaskRecord)
	limits                 func() seelexctx.Limits
	messageSeq             uint64
}

// NewCoordinator 构造 view 域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:                   deps.Core,
		currentEffort:          deps.CurrentEffort,
		refreshWorkTableLocked: deps.RefreshWorkTableLocked,
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
	c.Mu.RLock()
	snapshot := model.CloneSnapshot(c.Snapshot)
	c.Mu.RUnlock()
	return snapshot
}

// Subscribe 订阅事件流。
func (c *Coordinator) Subscribe(buffer int) event.Subscription {
	return c.Events.Subscribe(buffer)
}

// CollectRuntimeProjection 锁外调用外部端口收集 runtime 投影。
func (c *Coordinator) CollectRuntimeProjection(ctx context.Context) RuntimeStateProjection {
	projection := RuntimeStateProjection{
		SessionID: c.Deps.Engine.SessionID(),
		Runtime: model.RuntimeState{
			Model:             c.Deps.Runtime.Model(),
			Provider:          c.Deps.Runtime.Provider(),
			Plugin:            c.Deps.Runtime.ActivePlugin(),
			Effort:            c.currentEffort(),
			FullAccess:        c.Deps.Runtime.FullAccess(),
			VisibleTools:      append([]model.Tool(nil), c.Deps.Runtime.VisibleTools(ctx)...),
			Skills:            append([]model.SkillInfo(nil), c.Deps.Skills.All()...),
			Tokens:            c.Deps.Engine.TokenCount(),
			Plugins:           append([]model.PluginInfo(nil), c.Deps.Plugins.All()...),
			Accounts:          append([]model.AccountInfo(nil), c.Deps.Runtime.Accounts()...),
			TodoItems:         append([]dto.TodoItem(nil), c.Deps.Runtime.TodoSnapshot()...),
			ScheduledTasks:    append([]seelebridge.ScheduledTaskStatus(nil), c.Deps.Runtime.ScheduledTasksSnapshot()...),
			ScheduledCommands: append([]seelebridge.ScheduledCommandInfo(nil), c.Deps.Runtime.ScheduledCommands()...),
			SubAgentTree:      c.Deps.Engine.SubAgentTree(),
		},
	}
	metrics := c.Deps.Runtime.ReplanMetrics()
	projection.Runtime.Replan = model.ReplanMonitor{
		InFlight: metrics.InFlight, ConcurrentLimit: metrics.ConcurrentLimit,
		WindowAttempts: metrics.WindowAttempts, WindowLimit: metrics.WindowLimit,
		WindowStartedAt: metrics.WindowStartedAt, Accepted: metrics.Accepted,
		Succeeded: metrics.Succeeded, Failed: metrics.Failed, Rejected: metrics.Rejected,
		DuplicateRejected: metrics.DuplicateRejected, ProviderRequests: metrics.ProviderRequests,
		ProviderWindowRequests: metrics.ProviderWindowRequests, ProviderWindowLimit: metrics.ProviderWindowLimit,
	}
	projection.Tasks = c.Deps.Runtime.TaskSnapshot()
	return projection
}

// ApplyRuntimeProjectionLocked 应用 runtime 投影（保留 Plan/Account 指针，
// 锁内重建工作表格）。
func (c *Coordinator) ApplyRuntimeProjectionLocked(projection RuntimeStateProjection) {
	plan := c.Snapshot.Runtime.Plan
	account := c.Snapshot.Runtime.Account
	c.Snapshot.Session.ID = projection.SessionID
	c.Snapshot.Runtime = projection.Runtime
	c.Snapshot.Runtime.Plan = plan
	c.Snapshot.Runtime.Account = account
	c.refreshWorkTableLocked(projection.Tasks)
}

// AppendMessageLocked 追加一条可见消息（返回快照内引用；调用方持有
// Core.Mu）。
func (c *Coordinator) AppendMessageLocked(role, content string, tool *model.ToolCall) *model.Message {
	// 子代理继承上下文（SubagentContextMarker 前缀）只注入 provider history
	// 供模型消费，不进入可见会话区。
	if role == "user" && strings.HasPrefix(content, SubagentContextMarker) {
		return nil
	}
	if role == "assistant" || role == "tool_result" {
		content = chat.StripThoughtBlocks(content)
	}
	c.messageSeq++
	message := model.Message{ID: fmt.Sprintf("message-%d", c.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
	visibleBefore := durableConversationCount(c.Snapshot.Conversation)
	c.Snapshot.Conversation = append(c.Snapshot.Conversation, message)
	if role != "system" {
		if c.Snapshot.TotalMessages < visibleBefore {
			c.Snapshot.TotalMessages = visibleBefore
		}
		c.Snapshot.TotalMessages++
	}
	c.boundConversationTailLocked()
	for index := len(c.Snapshot.Conversation) - 1; index >= 0; index-- {
		if c.Snapshot.Conversation[index].ID == message.ID {
			return &c.Snapshot.Conversation[index]
		}
	}
	return &message
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

func (c *Coordinator) boundConversationTailLocked() {
	window := c.limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	c.Snapshot.ConversationWindow = window
	c.Snapshot.Conversation = BoundConversationTail(c.Snapshot.Conversation, window)
	visible := durableConversationCount(c.Snapshot.Conversation)
	c.Snapshot.HistoryOffset = c.Snapshot.TotalMessages - visible
	if c.Snapshot.HistoryOffset < 0 {
		c.Snapshot.HistoryOffset = 0
	}
	c.Snapshot.HasMoreHistory = c.Snapshot.HistoryOffset > 0
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
	c.Mu.Lock()
	message := *c.AppendMessageLocked("system", notice, nil)
	revision := c.BumpLocked()
	c.Mu.Unlock()
	c.Events.Publish(event.EventMessageAdded, revision, "", message)
}

// ResetConversation 清空可见会话并注入 CLI 标识与通知。
func (c *Coordinator) ResetConversation(notice string) {
	modelName := c.Deps.Runtime.Model()
	c.Mu.Lock()
	c.Snapshot.Conversation = nil
	c.AppendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", modelName), nil)
	if notice != "" {
		c.AppendMessageLocked("system", notice, nil)
	}
	revision := c.BumpLocked()
	c.Mu.Unlock()
	c.Events.Publish(event.EventSnapshotChanged, revision, "", nil)
}
