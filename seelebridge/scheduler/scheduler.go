// Package scheduler 承载 seelex 的定时周期任务 actor：标准库 time.Ticker
// 驱动单循环 goroutine，任务两类（command 白名单命令 / prompt 复用 agent
// 会话），状态只读快照经 Runtime 投影到 GUI。域内不依赖 seelebridge 根包。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

var (
	minScheduledInterval = 30 * time.Second
	schedulerTick        = 200 * time.Millisecond
)

// schedulerShutdownWait 是停机时等待运行中任务结束的时间上限
// （超时后运行中的命令随调度器 base ctx 取消而终止）。
const schedulerShutdownWait = 3 * time.Second

// 状态字与展示界限。
//
// "上次运行结果"这一格的词**只有一份**，住在契约（`dto.ScheduleRunStatus`）：
// 这里不再写第二份字面量，写方引枚举（词与 JSON 形状都没变）。
const (
	scheduledStatusPending = dto.ScheduleRunPending
	scheduledStatusRunning = dto.ScheduleRunRunning
	scheduledStatusOK      = dto.ScheduleRunOK
	scheduledStatusFailed  = dto.ScheduleRunFailed
	scheduledStatusSkipped = dto.ScheduleRunSkipped
)

const (
	scheduledResultTail     = 400     // 上次结果/错误尾部保留的 rune 数
	scheduledLogTail        = 20      // 运行日志尾部保留条数
	scheduledLogLineTail    = 120     // 单条日志截断 rune 数
	scheduledDefaultTimeout = 10 * 60 // 白名单命令默认超时（秒）
	scheduledClockLayout    = "15:04" // 周期锚点墙钟格式（HH:MM）
)

// ScheduledTaskKind 周期任务类型（DTO 别名）。
type ScheduledTaskKind = dto.ScheduledTaskKind

const (
	ScheduledTaskCommand = dto.ScheduledTaskCommand
	ScheduledTaskPrompt  = dto.ScheduledTaskPrompt
)

// ScheduledCommand 白名单命令描述（登记即信任；argv 固定直传，不解析用户文本）。
type ScheduledCommand = dto.ScheduledCommand

// ScheduledCommandInfo 白名单命令展示信息（GUI 新建弹窗下拉数据源）。
type ScheduledCommandInfo = dto.ScheduledCommandInfo

// ScheduledTaskSpec 创建任务入参（GUI Bridge 输入）。
type ScheduledTaskSpec = dto.ScheduledTaskSpec

// ScheduledTaskStatus 任务快照 DTO（GUI 定时任务面板消费）。
type ScheduledTaskStatus = dto.ScheduledTaskStatus

// PromptExecutor 周期提示词任务执行器（main 装配注入：application Submit
// 复用当前主会话；nil = prompt 任务不可创建）。
type PromptExecutor func(ctx context.Context, prompt, sessionID string) (string, error)

// State 是周期任务的 actor 资源（自带锁，读写即消息进出；与 task 注册表 /
// skill.Registry / filesystem 同构）。
type State struct {
	mu       sync.Mutex
	commands map[string]ScheduledCommand
	tasks    map[string]*task
	executor PromptExecutor
	observer func() // 状态变化通知（main 注入 application 投影发布）
	ctx      context.Context
	cancel   context.CancelFunc
	ticker   *time.Ticker
	stopCh   chan struct{}
	wg       sync.WaitGroup
	stopped  bool
}

// task 是调度器内部任务记录（快照 DTO 只读拷贝外发）。
type task struct {
	id      string
	spec    ScheduledTaskSpec
	nextRun time.Time
	running bool
	status  ScheduledTaskStatus
}

// NewState 构造调度器状态（base ctx 用于停机时取消运行中任务）。
func NewState() *State {
	ctx, cancel := context.WithCancel(context.Background())
	return &State{
		commands: make(map[string]ScheduledCommand),
		tasks:    make(map[string]*task),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 惰启动 ticker 循环（首次创建任务时调用；重复调用幂等）。
func (s *State) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.ticker != nil {
		return
	}
	s.ticker = time.NewTicker(schedulerTick)
	s.stopCh = make(chan struct{})
	s.wg.Add(1)
	go s.loop()
}

func (s *State) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-s.ticker.C:
			s.tick(now)
		}
	}
}

// tick 找出到期任务，逐个独立 goroutine 执行（长任务不阻塞调度循环；
// running 标志保证同一任务不重叠执行）。
func (s *State) tick(now time.Time) {
	s.mu.Lock()
	var due []*task
	for _, t := range s.tasks {
		if t.spec.Enabled && !t.running && !t.nextRun.IsZero() && !now.Before(t.nextRun) {
			t.running = true
			t.status.Running = true
			t.status.LastStatus = scheduledStatusRunning
			t.status.LastRunAt = now
			t.status.NextRunAt = time.Time{}
			t.status.LastError = ""
			t.appendLog(now, "运行开始")
			due = append(due, t)
		}
	}
	s.mu.Unlock()
	for _, t := range due {
		s.wg.Add(1)
		go s.executeTask(t)
	}
	if len(due) > 0 {
		s.observe()
	}
}

// executeTask 执行一次任务并回写状态（下次运行 = 本次开始时间 + 周期，
// 固定延迟语义；错过的时间点不追补）。
func (s *State) executeTask(t *task) {
	defer s.wg.Done()
	var result string
	var runErr error
	switch t.spec.Kind {
	case ScheduledTaskCommand:
		result, runErr = s.runCommand(t)
	case ScheduledTaskPrompt:
		result, runErr = s.runPrompt(t)
	default:
		runErr = fmt.Errorf("未知任务类型 %q", t.spec.Kind)
	}
	now := time.Now()
	s.mu.Lock()
	t.running = false
	status := &t.status
	status.Running = false
	status.LastRunAt = now
	status.RunCount++
	if runErr != nil {
		status.LastStatus = scheduledStatusFailed
		status.LastError = tailText(runErr.Error(), scheduledResultTail)
		status.LastResult = tailText(result, scheduledResultTail)
		t.appendLogLocked(now, "运行失败: "+status.LastError)
	} else {
		status.LastStatus = scheduledStatusOK
		status.LastResult = tailText(result, scheduledResultTail)
		t.appendLogLocked(now, "运行完成")
	}
	if !t.spec.RunAt.IsZero() {
		// 一次性定时任务：执行完成后自动停用并清除下次排期，记录保留供面板查看。
		t.spec.Enabled = false
		t.nextRun = time.Time{}
		status.NextRunAt = time.Time{}
		status.Enabled = false
	} else {
		next := nextScheduledAt(now, t.spec)
		t.nextRun = next
		status.NextRunAt = next
	}
	s.mu.Unlock()
	s.observe()
}

// runCommand 执行白名单命令：argv 直传（不经 shell），cwd 固定，
// 环境变量清洗（复用 security.ScrubEnvironment），带超时。
func (s *State) runCommand(t *task) (string, error) {
	s.mu.Lock()
	command, ok := s.commands[t.spec.Command]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("命令 %q 不在白名单中", t.spec.Command)
	}
	if len(command.Argv) == 0 {
		return "", fmt.Errorf("命令 %q 未配置可执行参数", command.Key)
	}
	timeout := command.TimeoutSec
	if timeout <= 0 {
		timeout = scheduledDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(s.ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	execCmd := exec.CommandContext(runCtx, command.Argv[0], command.Argv[1:]...)
	winhide.Apply(execCmd)
	execCmd.Dir = command.WorkingDir
	execCmd.Env = security.ScrubEnvironment(os.Environ())
	security.ConfigureHiddenCommand(execCmd)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("命令 %q 退出失败: %v", command.Key, err)
		if strings.TrimSpace(string(output)) != "" {
			message += "\n" + tailText(string(output), scheduledResultTail)
		}
		return "", errors.New(message)
	}
	return string(output), nil
}

// runPrompt 调用注入的执行器触发一次 agent 会话（扩展点）。
func (s *State) runPrompt(t *task) (string, error) {
	s.mu.Lock()
	executor := s.executor
	s.mu.Unlock()
	if executor == nil {
		return "", errors.New("提示词任务执行器未装配")
	}
	return executor(s.ctx, t.spec.Prompt, t.spec.SessionID)
}

// RegisterCommand 登记白名单命令（重复键拒绝）。
func (s *State) RegisterCommand(command ScheduledCommand) error {
	key := strings.TrimSpace(command.Key)
	if key == "" {
		return errors.New("白名单命令键不能为空")
	}
	if len(command.Argv) == 0 {
		return fmt.Errorf("白名单命令 %q 未配置可执行参数", key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.commands[key]; exists {
		return fmt.Errorf("白名单命令 %q 重复登记", key)
	}
	s.commands[key] = command
	return nil
}

// CommandInfos 返回白名单命令展示信息（GUI 新建弹窗数据源）。
func (s *State) CommandInfos() []ScheduledCommandInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]ScheduledCommandInfo, 0, len(s.commands))
	for _, command := range s.commands {
		infos = append(infos, ScheduledCommandInfo{
			Key: command.Key, Label: command.Label, Description: command.Description,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key })
	return infos
}

// Schedule 校验入参并创建任务（创建后立即排期；observer 通知投影）。
func (s *State) Schedule(_ context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, errors.New("任务名称不能为空")
	}
	oneShot := !spec.RunAt.IsZero()
	effective := time.Duration(0)
	if oneShot {
		if !spec.RunAt.After(time.Now()) {
			return nil, errors.New("定时执行时间必须晚于当前时间")
		}
		// 锚点只属于周期任务：一次性任务的时刻就是 RunAt，给了锚点只会被无声忽略。
		if strings.TrimSpace(spec.StartClock) != "" || spec.StartWeekday != dto.WeekdayUnset {
			return nil, errors.New("一次性定时任务不接受周期锚点（开始时间/星期）")
		}
		// 一次性任务创建即启用，避免"已停用且无法重新启用"的死角。
		spec.Enabled = true
	} else {
		if err := validatePeriod(spec); err != nil {
			return nil, err
		}
		effective = effectiveInterval(spec)
		if effective < minScheduledInterval {
			return nil, fmt.Errorf("周期过短：至少 %s", minScheduledInterval)
		}
	}
	switch spec.Kind {
	case ScheduledTaskCommand:
		key := strings.TrimSpace(spec.Command)
		if _, ok := s.commands[key]; !ok {
			return nil, fmt.Errorf("命令 %q 不在白名单中", key)
		}
	case ScheduledTaskPrompt:
		if strings.TrimSpace(spec.Prompt) == "" {
			return nil, errors.New("提示词内容不能为空")
		}
		s.mu.Lock()
		executor := s.executor
		s.mu.Unlock()
		if executor == nil {
			return nil, errors.New("提示词任务执行器未装配")
		}
	default:
		return nil, fmt.Errorf("未知任务类型 %q", spec.Kind)
	}
	t := &task{
		id: fmt.Sprintf("sched_%d", time.Now().UnixNano()),
		spec: ScheduledTaskSpec{
			Name: name, Kind: spec.Kind, Interval: spec.Interval,
			PeriodUnit: spec.PeriodUnit, PeriodValue: spec.PeriodValue,
			StartClock: canonicalStartClock(spec), StartWeekday: spec.StartWeekday,
			RunAt:   spec.RunAt,
			Command: strings.TrimSpace(spec.Command), Prompt: strings.TrimSpace(spec.Prompt),
			SessionID: strings.TrimSpace(spec.SessionID), Enabled: spec.Enabled,
		},
		nextRun: nextScheduledAt(time.Now(), spec),
	}
	t.status = ScheduledTaskStatus{
		ID: t.id, Name: name, Kind: string(spec.Kind),
		IntervalSec: int64(effective / time.Second),
		PeriodUnit:  string(spec.PeriodUnit), PeriodValue: spec.PeriodValue,
		StartClock: t.spec.StartClock, StartWeekday: t.spec.StartWeekday,
		RunAt: t.nextRun, OneShot: oneShot,
		Command: t.spec.Command, Prompt: t.spec.Prompt,
		SessionID: t.spec.SessionID, Enabled: spec.Enabled,
		NextRunAt: t.nextRun, LastStatus: scheduledStatusPending,
	}
	s.mu.Lock()
	s.tasks[t.id] = t
	s.mu.Unlock()
	s.Start()
	status := t.statusSnapshot()
	s.observe()
	return &status, nil
}

// CancelTask 取消并移除任务（运行中的执行不受影响，完成回写丢弃）。
func (s *State) CancelTask(id string) error {
	s.mu.Lock()
	if _, exists := s.tasks[id]; !exists {
		s.mu.Unlock()
		return fmt.Errorf("周期任务 %q 不存在", id)
	}
	delete(s.tasks, id)
	s.mu.Unlock()
	s.observe()
	return nil
}

// Snapshot 返回任务只读快照（按 ID 排序）。
func (s *State) Snapshot() []ScheduledTaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	statuses := make([]ScheduledTaskStatus, 0, len(s.tasks))
	for _, t := range s.tasks {
		statuses = append(statuses, t.statusSnapshot())
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

// SetPromptExecutor 注入周期提示词任务执行器（nil = 禁用 prompt 任务）。
func (s *State) SetPromptExecutor(executor PromptExecutor) {
	s.mu.Lock()
	s.executor = executor
	s.mu.Unlock()
}

// SetObserver 注入状态变化通知（main 接 application 快照投影发布）。
func (s *State) SetObserver(observer func()) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}

// observe 在锁外调用 observer（状态变化 → application 投影 → runtime.changed）。
func (s *State) observe() {
	s.mu.Lock()
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer()
	}
}

// Stop 优雅停机：停 ticker、取消 base ctx（终止运行中任务），
// 等待运行 goroutine 退出（上限 schedulerShutdownWait）。
func (s *State) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.ticker != nil {
		s.ticker.Stop()
	}
	if s.stopCh != nil {
		close(s.stopCh)
	}
	s.mu.Unlock()
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(schedulerShutdownWait):
	}
}

// statusSnapshot 返回任务状态只读拷贝。
func (t *task) statusSnapshot() ScheduledTaskStatus {
	return ScheduledTaskStatus{
		ID: t.status.ID, Name: t.status.Name, Kind: t.status.Kind,
		IntervalSec: t.status.IntervalSec, Command: t.status.Command,
		PeriodUnit: t.status.PeriodUnit, PeriodValue: t.status.PeriodValue,
		StartClock: t.status.StartClock, StartWeekday: t.status.StartWeekday,
		RunAt: t.status.RunAt, OneShot: t.status.OneShot,
		Prompt: t.status.Prompt, SessionID: t.status.SessionID,
		Enabled: t.status.Enabled, Running: t.status.Running,
		NextRunAt: t.status.NextRunAt, LastRunAt: t.status.LastRunAt,
		LastStatus: t.status.LastStatus, LastResult: t.status.LastResult,
		LastError: t.status.LastError, RunCount: t.status.RunCount,
		LogTail: append([]string(nil), t.status.LogTail...),
	}
}

// validatePeriod 校验周期单位/数值与锚点（空单位 = 秒级 Interval 路径）。
func validatePeriod(spec ScheduledTaskSpec) error {
	anchor, err := parseStartAnchor(spec)
	if err != nil {
		return err
	}
	switch spec.PeriodUnit {
	case "":
		if anchor.hasClock() || anchor.weekday != dto.WeekdayUnset {
			return errors.New("固定间隔周期不支持开始时间/星期锚点（请选择天/周/月周期）")
		}
		return nil
	case dto.PeriodMinute, dto.PeriodHour:
		if anchor.hasClock() || anchor.weekday != dto.WeekdayUnset {
			return fmt.Errorf("%s 周期不接受开始时间/星期锚点（子日周期按创建时刻滚动）", spec.PeriodUnit)
		}
	case dto.PeriodDay, dto.PeriodMonth:
		if anchor.weekday != dto.WeekdayUnset {
			return fmt.Errorf("只有周周期可以指定星期（%s 周期请只给开始时间）", spec.PeriodUnit)
		}
	case dto.PeriodWeek:
		// 星期可省（省 = 按创建时刻那一周的那一天滚动），但给了就必须合法。
	default:
		return fmt.Errorf("未知周期单位 %q", spec.PeriodUnit)
	}
	if spec.PeriodValue < 1 {
		return errors.New("周期数值必须 >= 1")
	}
	return nil
}

// startAnchor 是周期任务的墙钟锚点（HH:MM + 可选 ISO 星期）。
type startAnchor struct {
	hour, minute int
	weekday      int
	clock        bool // 是否给出了 HH:MM
}

func (a startAnchor) hasClock() bool { return a.clock }

// parseStartAnchor 解析周期锚点：空 StartClock = 以创建时刻为锚点（合法），
// 非空必须是 "HH:MM"；StartWeekday 走 ISO 1..7（0 = 未指定）。
func parseStartAnchor(spec ScheduledTaskSpec) (startAnchor, error) {
	anchor := startAnchor{weekday: spec.StartWeekday}
	if anchor.weekday < dto.WeekdayUnset || anchor.weekday > dto.WeekdaySunday {
		return startAnchor{}, fmt.Errorf("星期锚点 %d 越界（ISO 1=周一 … 7=周日）", anchor.weekday)
	}
	raw := strings.TrimSpace(spec.StartClock)
	if raw == "" {
		if anchor.weekday != dto.WeekdayUnset {
			return startAnchor{}, errors.New("指定星期时必须同时给出开始时间（HH:MM）")
		}
		return anchor, nil
	}
	parsed, err := time.Parse(scheduledClockLayout, raw)
	if err != nil {
		return startAnchor{}, fmt.Errorf("开始时间 %q 需要 HH:MM 格式", raw)
	}
	anchor.hour, anchor.minute, anchor.clock = parsed.Hour(), parsed.Minute(), true
	return anchor, nil
}

// canonicalStartClock 把锚点归一成 "HH:MM"（未给锚点 = 空串）。
//
// Go 的 time.Parse("15:04") 收紧放：`9:00` 也认。归一之后状态快照里只有一种
// 写法，"每天 9:00" 与 "每天 09:00" 在面板上不会显示成两种字面。
func canonicalStartClock(spec ScheduledTaskSpec) string {
	anchor, err := parseStartAnchor(spec)
	if err != nil || !anchor.hasClock() {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", anchor.hour, anchor.minute)
}

// effectiveInterval 返回用于最小周期校验与状态展示的等价秒级周期
// （month 使用 30 天名义值；真实排期走 nextScheduledAt 的日历语义）。
func effectiveInterval(spec ScheduledTaskSpec) time.Duration {
	switch spec.PeriodUnit {
	case dto.PeriodMinute:
		return time.Duration(spec.PeriodValue) * time.Minute
	case dto.PeriodHour:
		return time.Duration(spec.PeriodValue) * time.Hour
	case dto.PeriodDay:
		return time.Duration(spec.PeriodValue) * 24 * time.Hour
	case dto.PeriodWeek:
		return time.Duration(spec.PeriodValue) * 7 * 24 * time.Hour
	case dto.PeriodMonth:
		return time.Duration(spec.PeriodValue) * 30 * 24 * time.Hour
	default:
		return spec.Interval
	}
}

// nextScheduledAt 计算任务下一次运行时间（严格晚于 now）。
//
// 两种口径：
//   - 锚点口径（给了 StartClock）：候选时刻是"含 now 的那一天/那一周/那一月
//     的锚点墙钟"，再按周期步进到第一个晚于 now 的时刻。每天 09:00 的任务在
//     10:00 创建 → 明天 09:00；08:00 创建 → 今天 09:00。
//   - 滚动口径（未给锚点）：now + 周期，即"每个周期走当前时间"。
//
// 一次性任务直接用 RunAt；month 为日历月（月末钳制）。
func nextScheduledAt(now time.Time, spec ScheduledTaskSpec) time.Time {
	if !spec.RunAt.IsZero() {
		return spec.RunAt
	}
	anchor, err := parseStartAnchor(spec)
	if err != nil {
		// 校验在 Schedule 里做；这里退回滚动口径，绝不 panic。
		return now.Add(effectiveInterval(spec))
	}
	switch spec.PeriodUnit {
	case dto.PeriodMinute:
		return now.Add(time.Duration(spec.PeriodValue) * time.Minute)
	case dto.PeriodHour:
		return now.Add(time.Duration(spec.PeriodValue) * time.Hour)
	case dto.PeriodDay:
		if !anchor.hasClock() {
			return now.AddDate(0, 0, spec.PeriodValue)
		}
		return advanceToAnchor(now, anchorOnDay(now, anchor), func(at time.Time) time.Time {
			return at.AddDate(0, 0, spec.PeriodValue)
		})
	case dto.PeriodWeek:
		if !anchor.hasClock() {
			return now.AddDate(0, 0, 7*spec.PeriodValue)
		}
		target := anchor.weekday
		if target == dto.WeekdayUnset {
			target = isoWeekday(now)
		}
		delta := (target - isoWeekday(now) + 7) % 7
		return advanceToAnchor(now, anchorOnDay(now.AddDate(0, 0, delta), anchor), func(at time.Time) time.Time {
			return at.AddDate(0, 0, 7*spec.PeriodValue)
		})
	case dto.PeriodMonth:
		if !anchor.hasClock() {
			return addCalendarMonths(now, spec.PeriodValue)
		}
		return advanceToAnchor(now, anchorOnDay(now, anchor), func(at time.Time) time.Time {
			return addCalendarMonths(at, spec.PeriodValue)
		})
	default:
		return now.Add(spec.Interval)
	}
}

// advanceToAnchor 从候选时刻按步进找到第一个严格晚于 now 的时刻。
func advanceToAnchor(now, candidate time.Time, step func(time.Time) time.Time) time.Time {
	for !candidate.After(now) {
		candidate = step(candidate)
	}
	return candidate
}

// anchorOnDay 返回 day 那一天（本地时区）的锚点墙钟时刻。
func anchorOnDay(day time.Time, anchor startAnchor) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), anchor.hour, anchor.minute, 0, 0, day.Location())
}

// isoWeekday 返回 ISO 星期（1 = 周一 … 7 = 周日）。
func isoWeekday(at time.Time) int {
	day := int(at.Weekday())
	if day == 0 {
		return dto.WeekdaySunday
	}
	return day
}

// addCalendarMonths 按日历月推进并钳制月末日期（如 1-31 加 1 月 → 2-28/29）。
func addCalendarMonths(now time.Time, months int) time.Time {
	target := time.Date(now.Year(), now.Month()+time.Month(months), 1,
		now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	last := lastDayOfMonth(target.Year(), target.Month())
	day := now.Day()
	if day > last {
		day = last
	}
	return time.Date(target.Year(), target.Month(), day,
		now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
}

func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// appendLog 追加运行日志尾部（有界环保留）。
func (t *task) appendLog(now time.Time, line string) {
	t.appendLogLocked(now, line)
}

func (t *task) appendLogLocked(now time.Time, line string) {
	entry := fmt.Sprintf("[%s] %s", now.Format("15:04:05"), tailText(line, scheduledLogLineTail))
	t.status.LogTail = append(t.status.LogTail, entry)
	if len(t.status.LogTail) > scheduledLogTail {
		t.status.LogTail = t.status.LogTail[len(t.status.LogTail)-scheduledLogTail:]
	}
}

// tailText 截取字符串尾部（结果/错误展示用，超长从尾部保留）。
func tailText(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return "…" + string(runes[len(runes)-maxRunes:])
}
