package goal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// View 是前端投影的紧凑 goal 视图（字段与未来 dto.GoalView 对齐，
// 见 docs/2026-09-07-goal-domain-techleader/prototype.md §5）。
type View struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    Status `json:"status"`
	UpdatedAt int64  `json:"updated_at"`
}

// Projection 是前端/工作台投影：goals 为栈底→栈顶，Active == goals 末元素
// （无 active 时为 nil）。每次变更后由事件携带全量投影（快照语义）。
type Projection struct {
	Active *View  `json:"active,omitempty"`
	Goals  []View `json:"goals,omitempty"`
}

func viewOf(record *GoalRecord) *View {
	if record == nil {
		return nil
	}
	return &View{
		ID:        record.ID,
		Title:     record.Title,
		Status:    record.Status,
		UpdatedAt: record.UpdatedAt,
	}
}

func projectionOf(records []*GoalRecord) Projection {
	projection := Projection{Goals: make([]View, 0, len(records))}
	for _, record := range records {
		projection.Goals = append(projection.Goals, *viewOf(record))
	}
	if len(projection.Goals) > 0 {
		top := projection.Goals[len(projection.Goals)-1]
		projection.Active = &top
	}
	return projection
}

// StatusView 是 goal_status 返回的全量视图（含 acceptance/progress 副本）。
type StatusView struct {
	Stack   []*GoalRecord `json:"stack"`            // 栈底→栈顶
	Active  *GoalRecord   `json:"active,omitempty"` // = 栈顶
	History int           `json:"history_count"`    // 已收口目标数（审计计数）
}

// EventKind 是 goal 事件类型。
type EventKind string

const (
	EventBegin   EventKind = "goal.begin"
	EventUpdate  EventKind = "goal.update"
	EventFinish  EventKind = "goal.finish"
	EventAbort   EventKind = "goal.abort"
	EventRestore EventKind = "goal.restore" // 嵌套弹栈后下层恢复 active
)

// Event 是投递给订阅者/前端的事件；Projection 为全量快照（后发覆盖安全）。
type Event struct {
	Kind       EventKind  `json:"kind"`
	At         int64      `json:"at"`
	GoalID     string     `json:"goal_id,omitempty"`
	Status     Status     `json:"status,omitempty"`
	Projection Projection `json:"projection"`
}

// Options 是 Controller 构造参数。
type Options struct {
	// Depth 是 goal 栈深上限：1 = 会话单例（D2 默认）；>1 放开嵌套。
	Depth int
	// Store 为 nil 时纯内存（不持久化）。
	Store Store
	// Audit 为 nil 时不审计；注入后每次状态机变更成功追加一条 append-only
	// 审计（GoalStack 活栈终态清空，审计账本保留收口记录）。
	Audit AuditAccount
	// Now 可注入时间源（测试）；nil 用 time.Now().Unix()。
	Now func() int64
}

// Controller 是 goal 域控制器（Part I 实现）。
// 并发：全部可变状态由 mu 保护；事件在持锁下投递（有界通道，select-default
// 不阻塞；满则 dropped 计数）。读取面（Status/Projection/Frame）返回深拷贝。
type Controller struct {
	mu    sync.Mutex
	depth int
	store Store
	audit AuditAccount
	now   func() int64

	stack   *Stack
	history []*GoalRecord
	seq     uint64

	subs map[*Subscription]struct{}
}

// Subscription 是一次事件订阅（有界缓冲；溢出计数 Dropped，不丢语义由
// 事件自带全量 Projection 保证——后发覆盖即可追平）。
type Subscription struct {
	Events    <-chan Event
	ch        chan Event
	ctl       *Controller
	closeOnce sync.Once
	dropped   atomic.Int64
}

// NewController 构造控制器（Depth 默认 1；Now 默认 time.Now().Unix）。
func NewController(options Options) *Controller {
	if options.Depth <= 0 {
		options.Depth = DefaultStackDepth
	}
	if options.Depth > MaxStackDepth {
		options.Depth = MaxStackDepth
	}
	now := options.Now
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &Controller{
		depth: options.Depth,
		store: options.Store,
		audit: options.Audit,
		now:   now,
		stack: &Stack{},
		subs:  make(map[*Subscription]struct{}),
	}
}

// Begin 注册并压栈一个 goal（design §3.4 goal_begin）。
// 幂等：同标题且非终态时返回现有 active goal。
func (c *Controller) Begin(ctx context.Context, request BeginRequest) (*GoalRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if top := c.stack.Top(); top != nil && !IsTerminal(top.Status) &&
		strings.EqualFold(strings.TrimSpace(top.Title), strings.TrimSpace(request.Title)) {
		return top.Clone(), nil
	}
	if c.stack.Len() >= c.depth {
		return nil, ErrStackFull
	}
	c.seq++
	record := newGoalRecord(fmt.Sprintf("g-%d", c.seq), request, now)
	if err := record.validateBegin(); err != nil {
		return nil, err
	}
	// 嵌套：原栈顶降为 paused（弹栈时恢复 active，见 finish/abort 恢复路径）。
	if previous := c.stack.Top(); previous != nil && previous.Status == StatusActive {
		previous.Status = StatusPaused
		previous.UpdatedAt = now
	}
	c.stack.Push(record)
	if err := c.persistLocked(ctx); err != nil {
		c.stack.Pop() // 回滚
		return nil, err
	}
	if err := c.appendAuditLocked(ctx, AuditEntry{
		Kind: EventBegin, GoalID: record.ID, Title: record.Title,
		Status: record.Status,
	}); err != nil {
		return nil, err
	}
	c.emitLocked(Event{
		Kind: EventBegin, At: now, GoalID: record.ID, Status: record.Status,
		Projection: c.projectionLocked(),
	})
	return record.Clone(), nil
}

// Update 更新栈顶 active goal（design §3.4 goal_update）。
func (c *Controller) Update(ctx context.Context, request UpdateRequest) (*GoalRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	top := c.stack.Top()
	if top == nil {
		return nil, ErrStackEmpty
	}
	if top.Status != StatusActive {
		return nil, fmt.Errorf("%w: 当前状态 %s", ErrGoalNotActive, top.Status)
	}
	now := c.now()
	if request.Title != nil {
		top.Title = strings.TrimSpace(*request.Title)
	}
	if request.Statement != nil {
		top.Statement = strings.TrimSpace(*request.Statement)
	}
	if request.Acceptance != nil {
		top.Acceptance = append([]string(nil), request.Acceptance...)
	}
	if request.OutOfScope != nil {
		top.OutOfScope = append([]string(nil), request.OutOfScope...)
	}
	if content := strings.TrimSpace(request.ProgressContent); content != "" {
		kind := request.ProgressKind
		if kind == "" {
			kind = ProgressMilestone
		}
		if !validProgressKinds[kind] {
			return nil, fmt.Errorf("%w: 非法 progress_kind %q", ErrInvalidArgument, request.ProgressKind)
		}
		if len([]rune(content)) > MaxProgressRunes {
			return nil, fmt.Errorf("%w: progress 超长（> %d runes）", ErrInvalidArgument, MaxProgressRunes)
		}
		top.Progress = append(top.Progress, Progress{At: now, Kind: kind, Content: content})
		if len(top.Progress) > MaxProgressItems {
			top.Progress = top.Progress[len(top.Progress)-MaxProgressItems:]
		}
	}
	if err := top.validateBegin(); err != nil {
		return nil, err
	}
	top.UpdatedAt = now
	if err := c.persistLocked(ctx); err != nil {
		return nil, err
	}
	detail := ""
	if content := strings.TrimSpace(request.ProgressContent); content != "" {
		detail = content
	} else if request.Title != nil || request.Statement != nil ||
		request.Acceptance != nil || request.OutOfScope != nil {
		detail = "goal updated"
	}
	if err := c.appendAuditLocked(ctx, AuditEntry{
		Kind: EventUpdate, GoalID: top.ID, Title: top.Title,
		Status: top.Status, Detail: detail,
	}); err != nil {
		return nil, err
	}
	c.emitLocked(Event{
		Kind: EventUpdate, At: now, GoalID: top.ID, Status: top.Status,
		Projection: c.projectionLocked(),
	})
	return top.Clone(), nil
}

// Finish 把栈顶 goal 标记 completed 并弹栈（design §3.4 goal_finish；
// 工作台/栈删除，History 留审计）。嵌套时下层自动恢复 active。
func (c *Controller) Finish(ctx context.Context, request FinishRequest) (*GoalRecord, error) {
	return c.finishOrAbort(ctx, request, StatusCompleted, EventFinish)
}

// Abort 把栈顶 goal 标记 aborted 并弹栈。
func (c *Controller) Abort(ctx context.Context, request FinishRequest) (*GoalRecord, error) {
	return c.finishOrAbort(ctx, request, StatusAborted, EventAbort)
}

func (c *Controller) finishOrAbort(ctx context.Context, request FinishRequest, terminal Status, kind EventKind) (*GoalRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	top := c.stack.Top()
	if top == nil {
		return nil, ErrStackEmpty
	}
	if IsTerminal(top.Status) {
		return nil, fmt.Errorf("%w: goal %s 已是终态 %s", ErrInvalidArgument, top.ID, top.Status)
	}
	if len(request.Result) > MaxProgressRunes {
		return nil, fmt.Errorf("%w: result 超长（> %d runes）", ErrInvalidArgument, MaxProgressRunes)
	}
	if top.Status == StatusActive && terminal == StatusCompleted {
		// 收口前允许附最终进度（可选）。
		if content := strings.TrimSpace(request.Result); content != "" {
			top.Progress = append(top.Progress, Progress{At: now, Kind: ProgressFinding, Content: content})
		}
	}
	top.Status = terminal
	top.FinishedAt = now
	top.UpdatedAt = now
	c.stack.Pop()
	c.history = append(c.history, top)

	// 嵌套恢复：新栈顶若为 paused → active。
	if restored := c.stack.Top(); restored != nil && restored.Status == StatusPaused {
		restored.Status = StatusActive
		restored.UpdatedAt = now
	}
	if err := c.persistLocked(ctx); err != nil {
		return nil, err
	}
	if err := c.appendAuditLocked(ctx, AuditEntry{
		Kind: kind, GoalID: top.ID, Title: top.Title, Status: top.Status,
		Reason: request.Reason, Result: request.Result,
	}); err != nil {
		return nil, err
	}
	event := Event{
		Kind: kind, At: now, GoalID: top.ID, Status: top.Status,
		Projection: c.projectionLocked(),
	}
	if restored := c.stack.Top(); restored != nil {
		if err := c.appendAuditLocked(ctx, AuditEntry{
			Kind: EventRestore, GoalID: restored.ID, Title: restored.Title,
			Status: restored.Status, Detail: "parent restored active",
		}); err != nil {
			return nil, err
		}
		c.emitLocked(Event{
			Kind: EventRestore, At: now, GoalID: restored.ID, Status: restored.Status,
			Projection: event.Projection,
		})
	}
	c.emitLocked(event)
	return top.Clone(), nil
}

// Status 返回全量视图（深拷贝，锁外安全）。
func (c *Controller) Status() StatusView {
	c.mu.Lock()
	defer c.mu.Unlock()
	view := StatusView{Stack: make([]*GoalRecord, 0, c.stack.Len()), History: len(c.history)}
	for _, record := range c.stack.All() {
		view.Stack = append(view.Stack, record.Clone())
	}
	if len(view.Stack) > 0 {
		view.Active = view.Stack[len(view.Stack)-1]
	}
	return view
}

// Projection 返回前端投影（深拷贝视图）。
func (c *Controller) Projection() Projection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.projectionLocked()
}

func (c *Controller) projectionLocked() Projection {
	return projectionOf(c.stack.All())
}

// History 返回已收口（finish/abort）goal 的审计副本（栈底→收口序）。
func (c *Controller) History() []*GoalRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*GoalRecord, 0, len(c.history))
	for _, record := range c.history {
		out = append(out, record.Clone())
	}
	return out
}

// Frame 渲染栈顶 goal 的 Goal 帧文本（design §3.5；Part II TechLeader 每轮
// 嵌入 goal 内容与 goal 帧注入的前端素材）。空栈返回空串。
func (c *Controller) Frame() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	top := c.stack.Top()
	if top == nil {
		return ""
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "[goal] %s  (%s)\n", top.Title, top.Status)
	if statement := strings.TrimSpace(top.Statement); statement != "" {
		fmt.Fprintf(&builder, "statement: %s\n", statement)
	}
	if len(top.Acceptance) > 0 {
		builder.WriteString("acceptance:\n")
		for _, item := range top.Acceptance {
			fmt.Fprintf(&builder, "- %s\n", item)
		}
	}
	// 最近 ≤2 条 progress。
	start := 0
	if len(top.Progress) > 2 {
		start = len(top.Progress) - 2
	}
	if start < len(top.Progress) {
		builder.WriteString("progress:\n")
		for _, item := range top.Progress[start:] {
			fmt.Fprintf(&builder, "- [%s] %s\n", item.Kind, item.Content)
		}
	}
	return builder.String()
}

// Subscribe 订阅事件流（buffer ≤ 0 时用 64）。Close 幂等。
func (c *Controller) Subscribe(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = 64
	}
	subscription := &Subscription{ch: make(chan Event, buffer), ctl: c}
	subscription.Events = subscription.ch
	c.mu.Lock()
	c.subs[subscription] = struct{}{}
	c.mu.Unlock()
	return subscription
}

// Events 是订阅通道（等价 Subscription.Events，便利字段）。
func (s *Subscription) EventsChannel() <-chan Event { return s.Events }

// Close 移除订阅并关闭通道（幂等）。
func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		s.ctl.mu.Lock()
		delete(s.ctl.subs, s)
		s.ctl.mu.Unlock()
		close(s.ch)
	})
}

// Dropped 返回因缓冲满而被丢弃的事件数（事件自带全量投影，可覆盖追平）。
func (s *Subscription) Dropped() int64 { return s.dropped.Load() }

// emitLocked 在持锁下向全部订阅者投递；不阻塞（满则 dropped++）。
func (c *Controller) emitLocked(event Event) {
	for subscription := range c.subs {
		select {
		case subscription.ch <- event:
		default:
			subscription.dropped.Add(1)
		}
	}
}

// persistLocked 在持锁下持久化当前栈（无 store 时为空操作）。
func (c *Controller) persistLocked(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	if err := c.store.Save(ctx, c.stack.All()); err != nil {
		return err
	}
	return nil
}

// appendAuditLocked 在持锁下向审计账本追加一条有界审计（无 Audit 时为空
// 操作）。At 未填时取当前时间。
func (c *Controller) appendAuditLocked(ctx context.Context, entry AuditEntry) error {
	if c.audit == nil {
		return nil
	}
	if entry.At <= 0 {
		entry.At = c.now()
	}
	return c.audit.AppendGoalAudit(ctx, entry.normalized())
}

// Reload 从 Store 装载 goal 栈（崩溃/重启恢复；design §3.7）。装载后修正：
// 下层 paused、栈顶 active 的状态不变量（仅保留一个 active 语义）。
func (c *Controller) Reload(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		return nil
	}
	records, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	c.stack = &Stack{}
	c.history = nil
	for index, record := range records {
		if record == nil {
			continue
		}
		clone := record.Clone()
		if index < len(records)-1 {
			clone.Status = StatusPaused
		} else {
			clone.Status = StatusActive
		}
		c.stack.Push(clone)
		if id := clone.ID; len(id) > 2 {
			var suffix uint64
			if _, scanErr := fmt.Sscanf(id[2:], "%d", &suffix); scanErr == nil && suffix > c.seq {
				c.seq = suffix
			}
		}
	}
	return nil
}
