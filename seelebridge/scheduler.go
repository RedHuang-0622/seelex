package seelebridge

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
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// ── 定时周期任务（scheduler）────────────────────────────────────────────
// 轻量调度器：标准库 time.Ticker 驱动单循环 goroutine，不引第三方库。
// 任务两类：
//   - kind=command：执行白名单命令（ScheduledCommand 登记即信任，argv 固定
//     直传、不经 shell 展开，杜绝任意命令注入）。外挂周期脚本（如
//     local/tools/auto_get_jobs）走此路径。
//   - kind=prompt：复用 agent 会话的周期提示词（扩展点）：经注入的
//     ScheduledPromptExecutor 触发一次会话执行，main 装配时接 application
//     Submit（排队语义：会话忙时任务被排队，不会与进行中的对话冲突）。
//     结果回传为"已提交"状态字；完整输出经会话记录查询（异步会话不在此
//     阻塞回传，见 gui/frontend/README.md「定时任务」一节）。
//
// 状态：Runtime 内存态（持久化留扩展点）；快照经 RuntimePort 投影进 GUI
// （runtime.changed 增量）；每次状态变化（创建/取消/运行开始/运行结束）
// 调用 observer 通知 application 重新投影并发布事件。

// 最小周期与 tick 粒度（包级变量，同包测试可下调加速）。
var (
	minScheduledInterval = 30 * time.Second
	schedulerTick        = 200 * time.Millisecond
)

// schedulerShutdownWait 是停机时等待运行中任务结束的时间上限
// （超时后运行中的命令随调度器 base ctx 取消而终止）。
const schedulerShutdownWait = 3 * time.Second

// 状态字与展示限界。
const (
	scheduledStatusPending = "pending" // 未运行过
	scheduledStatusRunning = "running" // 运行中
	scheduledStatusOK      = "ok"      // 上次运行成功
	scheduledStatusFailed  = "failed"  // 上次运行失败
	scheduledStatusSkipped = "skipped" // 上次被跳过（运行中被取消等）
)

const (
	scheduledResultTail     = 400     // 上次结果/错误尾部保留的 rune 数
	scheduledLogTail        = 20      // 运行日志尾部保留条目数
	scheduledLogLineTail    = 120     // 单条日志截断 rune 数
	scheduledDefaultTimeout = 10 * 60 // 白名单命令默认超时（秒）
)

// ScheduledTaskKind 周期任务类型。
// ScheduledTaskKind ????????????? application/contract/dto??
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

// ScheduledPromptExecutor 周期提示词任务执行器（main 装配注入：application
// Submit 复用当前主会话；nil = prompt 任务不可创建）。
type ScheduledPromptExecutor func(ctx context.Context, prompt, sessionID string) (string, error)

// schedulerState 是周期任务的 actor 资源（自带锁，读写即消息进出；
// 与 todoState / skill.Registry / filesystem 同构）。
type schedulerState struct {
	mu       sync.Mutex
	commands map[string]ScheduledCommand
	tasks    map[string]*scheduledTask
	executor ScheduledPromptExecutor
	observer func() // 状态变化通知（main 注入 application 投影发布）

	ctx     context.Context // base ctx：停机时取消运行中的任务
	cancel  context.CancelFunc
	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
	stopped bool
}

// scheduledTask 是调度器内部任务记录（快照 DTO 只读拷贝外发）。
type scheduledTask struct {
	id      string
	spec    ScheduledTaskSpec
	nextRun time.Time
	running bool
	status  ScheduledTaskStatus
}

func newSchedulerState() *schedulerState {
	ctx, cancel := context.WithCancel(context.Background())
	return &schedulerState{
		commands: make(map[string]ScheduledCommand),
		tasks:    make(map[string]*scheduledTask),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ── 调度器循环 ─────────────────────────────────────────────────────────

// start 懒启动 ticker 循环（首次创建任务时调用；重复调用幂等）。
func (s *schedulerState) start() {
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

func (s *schedulerState) loop() {
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
func (s *schedulerState) tick(now time.Time) {
	s.mu.Lock()
	var due []*scheduledTask
	for _, task := range s.tasks {
		if task.spec.Enabled && !task.running && !task.nextRun.IsZero() && !now.Before(task.nextRun) {
			task.running = true
			task.status.Running = true
			task.status.LastStatus = scheduledStatusRunning
			task.status.LastRunAt = now
			task.status.NextRunAt = time.Time{}
			task.status.LastError = ""
			task.appendLog(now, "运行开始")
			due = append(due, task)
		}
	}
	s.mu.Unlock()
	for _, task := range due {
		s.wg.Add(1)
		go s.executeTask(task)
	}
	if len(due) > 0 {
		s.observe()
	}
}

// executeTask 执行一次任务并回写状态（下次运行 = 本次开始时间 + 周期，
// 固定延迟语义；错过的时间点不追补）。
func (s *schedulerState) executeTask(task *scheduledTask) {
	defer s.wg.Done()
	var result string
	var runErr error
	switch task.spec.Kind {
	case ScheduledTaskCommand:
		result, runErr = s.runCommand(task)
	case ScheduledTaskPrompt:
		result, runErr = s.runPrompt(task)
	default:
		runErr = fmt.Errorf("未知任务类型 %q", task.spec.Kind)
	}
	now := time.Now()
	s.mu.Lock()
	task.running = false
	status := &task.status
	status.Running = false
	status.LastRunAt = now
	status.RunCount++
	if runErr != nil {
		status.LastStatus = scheduledStatusFailed
		status.LastError = tailText(runErr.Error(), scheduledResultTail)
		status.LastResult = tailText(result, scheduledResultTail)
		task.appendLogLocked(now, "运行失败: "+status.LastError)
	} else {
		status.LastStatus = scheduledStatusOK
		status.LastResult = tailText(result, scheduledResultTail)
		task.appendLogLocked(now, "运行完成")
	}
	next := now.Add(task.spec.Interval)
	task.nextRun = next
	status.NextRunAt = next
	s.mu.Unlock()
	s.observe()
}

// runCommand 执行白名单命令：argv 直传（不经 shell），cwd 固定，
// 环境变量清洗（复用 security 的 ScrubEnvironment），带超时。
func (s *schedulerState) runCommand(task *scheduledTask) (string, error) {
	s.mu.Lock()
	command, ok := s.commands[task.spec.Command]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("命令 %q 不在白名单中", task.spec.Command)
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
func (s *schedulerState) runPrompt(task *scheduledTask) (string, error) {
	s.mu.Lock()
	executor := s.executor
	s.mu.Unlock()
	if executor == nil {
		return "", errors.New("提示词任务执行器未装配")
	}
	return executor(s.ctx, task.spec.Prompt, task.spec.SessionID)
}

// ── 管理操作 ───────────────────────────────────────────────────────────

// registerCommand 登记白名单命令（重复键拒绝）。
func (s *schedulerState) registerCommand(command ScheduledCommand) error {
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

func (s *schedulerState) commandInfos() []ScheduledCommandInfo {
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

// schedule 校验入参并创建任务（创建后立刻排期；observer 通知投影）。
func (s *schedulerState) schedule(_ context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, errors.New("任务名称不能为空")
	}
	if spec.Interval < minScheduledInterval {
		return nil, fmt.Errorf("周期过短：至少 %s", minScheduledInterval)
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
	task := &scheduledTask{
		id: fmt.Sprintf("sched_%d", time.Now().UnixNano()),
		spec: ScheduledTaskSpec{
			Name: name, Kind: spec.Kind, Interval: spec.Interval,
			Command: strings.TrimSpace(spec.Command), Prompt: strings.TrimSpace(spec.Prompt),
			SessionID: strings.TrimSpace(spec.SessionID), Enabled: spec.Enabled,
		},
		nextRun: time.Now().Add(spec.Interval),
	}
	task.status = ScheduledTaskStatus{
		ID: task.id, Name: name, Kind: string(spec.Kind),
		IntervalSec: int64(spec.Interval / time.Second),
		Command:     task.spec.Command, Prompt: task.spec.Prompt,
		SessionID: task.spec.SessionID, Enabled: spec.Enabled,
		NextRunAt: task.nextRun, LastStatus: scheduledStatusPending,
	}
	s.mu.Lock()
	s.tasks[task.id] = task
	s.mu.Unlock()
	s.start()
	status := task.statusSnapshot()
	s.observe()
	return &status, nil
}

// cancelTask 取消并移除任务（运行中的执行不受影响，完成回写丢弃）。
func (s *schedulerState) cancelTask(id string) error {
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

// snapshot 返回任务只读快照（按创建时间排序）。
func (s *schedulerState) snapshot() []ScheduledTaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	statuses := make([]ScheduledTaskStatus, 0, len(s.tasks))
	for _, task := range s.tasks {
		statuses = append(statuses, task.statusSnapshot())
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

// observe 在锁外调用 observer（状态变化 → application 投影 → runtime.changed）。
func (s *schedulerState) observe() {
	s.mu.Lock()
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer()
	}
}

// stop 优雅停机：停 ticker、取消 base ctx（终止运行中任务），
// 等待运行 goroutine 退出（上限 schedulerShutdownWait）。
func (s *schedulerState) stop() {
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

// ── 小工具 ─────────────────────────────────────────────────────────────

// statusSnapshot 返回任务状态只读拷贝。
func (t *scheduledTask) statusSnapshot() ScheduledTaskStatus {
	return ScheduledTaskStatus{
		ID: t.status.ID, Name: t.status.Name, Kind: t.status.Kind,
		IntervalSec: t.status.IntervalSec, Command: t.status.Command,
		Prompt: t.status.Prompt, SessionID: t.status.SessionID,
		Enabled: t.status.Enabled, Running: t.status.Running,
		NextRunAt: t.status.NextRunAt, LastRunAt: t.status.LastRunAt,
		LastStatus: t.status.LastStatus, LastResult: t.status.LastResult,
		LastError: t.status.LastError, RunCount: t.status.RunCount,
		LogTail: append([]string(nil), t.status.LogTail...),
	}
}

// appendLog 追加运行日志尾部（有界环形保留）。
func (t *scheduledTask) appendLog(now time.Time, line string) {
	t.appendLogLocked(now, line)
}

func (t *scheduledTask) appendLogLocked(now time.Time, line string) {
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

// ── Runtime 装配面 ─────────────────────────────────────────────────────

// RegisterScheduledCommand 登记白名单命令（重复键拒绝；main 装配调用）。
func (r *Runtime) RegisterScheduledCommand(command ScheduledCommand) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.registerCommand(command)
}

// ScheduledCommands 返回白名单命令展示信息（GUI 新建弹窗数据源）。
func (r *Runtime) ScheduledCommands() []ScheduledCommandInfo {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.commandInfos()
}

// ScheduleTask 创建并启动一个周期任务（校验入参；返回创建后的快照）。
func (r *Runtime) ScheduleTask(ctx context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.schedule(ctx, spec)
}

// CancelScheduledTask 取消并移除周期任务。
func (r *Runtime) CancelScheduledTask(id string) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.cancelTask(id)
}

// ScheduledTasksSnapshot 返回周期任务只读快照（application 快照投影数据源；
// 状态变化经 observer 通知后由 runtime.changed 增量带到 GUI）。
func (r *Runtime) ScheduledTasksSnapshot() []ScheduledTaskStatus {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.snapshot()
}

// SetScheduledPromptExecutor 注入周期提示词任务执行器（nil = 禁用 prompt 任务）。
func (r *Runtime) SetScheduledPromptExecutor(executor ScheduledPromptExecutor) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.mu.Lock()
	r.scheduler.executor = executor
	r.scheduler.mu.Unlock()
}

// SetSchedulerObserver 注入调度器状态变化通知（main 接 application 的
// 快照投影发布，使 GUI 经 runtime.changed 增量更新定时任务面板）。
func (r *Runtime) SetSchedulerObserver(observer func()) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.mu.Lock()
	r.scheduler.observer = observer
	r.scheduler.mu.Unlock()
}
