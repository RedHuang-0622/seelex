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

// PromptOutcome 是一次提示词触发的落点：Message 是面板「上次结果」的展示文本，
// SessionID 是这次真正落到的会话（空 = 没有落到任何会话）。
type PromptOutcome struct {
	Message   string
	SessionID string
}

// PromptExecutor 定时提示词任务执行器（main 装配注入；nil = prompt 任务不可创建）。
//
// 入参是这一条任务的**归一后定义**（不是散装参数）：落点与装配都从它读，判据
// 只有契约里这一份——
//   - `SessionID` 非空 → 投递到该既有会话；空（**默认**）→ 新建会话发起；
//   - `WorkspaceID` 非空 → 新会话装配到该工作区（项目根 + 会话记录落该项目分区）；
//   - `PermissionTier` → 触发那次会话的权限档位（空已在归一里落成默认 full）；
//   - `Plugins` → 这一轮的能力包装配（空 = 继承宿主当前激活插件）。
type PromptExecutor func(ctx context.Context, task ScheduledTaskSpec) (PromptOutcome, error)

// State 是周期任务的 actor 资源（自带锁，读写即消息进出；与 task 注册表 /
// skill.Registry / filesystem 同构）。
type State struct {
	mu       sync.Mutex
	commands map[string]ScheduledCommand
	tasks    map[string]*task
	executor PromptExecutor
	// store 是任务定义的全局 JSONL（nil = 不持久化：任务只活在进程内）。
	// 它是**全局**通道：任务列表不按项目分区、不按会话分片；触发产生的
	// 会话记录才走会话自己的存储纪律。
	store    Persistence
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
func NewState() *State { return NewStateWithStore(nil) }

// NewStateWithStore 构造带全局 JSONL 任务定义的调度器状态（store 为 nil =
// 关闭持久化）。已落盘的任务不在这里读回：冷启动由 Restore 显式触发，保证
// "先装执行器与观察者、再恢复任务"的顺序不会反过来。
func NewStateWithStore(store Persistence) *State {
	ctx, cancel := context.WithCancel(context.Background())
	return &State{
		commands: make(map[string]ScheduledCommand),
		tasks:    make(map[string]*task),
		store:    store,
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
	var runSessionID string
	var runErr error
	switch t.spec.Kind {
	case ScheduledTaskCommand:
		result, runErr = s.runCommand(t)
	case ScheduledTaskPrompt:
		result, runSessionID, runErr = s.runPrompt(t)
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
	if runSessionID != "" {
		// 本次落点（默认是新开会话；显式绑定会话时就是那个会话）。失败路径
		// 也记：面板要能看到"这次跑到哪个会话去了"。
		status.LastSessionID = runSessionID
	}
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

// runPrompt 调用注入的执行器触发一次 agent 会话（扩展点）：执行器按
// "sessionID 空 = 新建会话（默认）/ 非空 = 投递既有会话"这条唯一判据落点，
// 并把实际落点会话号随结果回传。
func (s *State) runPrompt(t *task) (string, string, error) {
	s.mu.Lock()
	executor := s.executor
	s.mu.Unlock()
	if executor == nil {
		return "", "", errors.New("提示词任务执行器未装配")
	}
	outcome, err := executor(s.ctx, t.spec)
	return outcome.Message, strings.TrimSpace(outcome.SessionID), err
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
	definition, err := s.normalizeSpec(spec)
	if err != nil {
		return nil, err
	}
	t := &task{id: fmt.Sprintf("sched_%d", time.Now().UnixNano())}
	t.applyDefinition(definition, nextScheduledAt(time.Now(), definition.spec))
	t.status.ID = t.id
	t.status.LastStatus = scheduledStatusPending
	s.mu.Lock()
	s.tasks[t.id] = t
	s.mu.Unlock()
	// 登记即承诺落盘：全局 JSONL 写不进去就当作没登记（内存回滚），
	// 否则用户会在面板上看到一个重启就消失的任务。
	if err := s.persistDefinition(t, false); err != nil {
		s.mu.Lock()
		delete(s.tasks, t.id)
		s.mu.Unlock()
		return nil, fmt.Errorf("定时任务落盘失败（未登记）: %w", err)
	}
	s.Start()
	status := t.statusSnapshot()
	s.observe()
	return &status, nil
}

// Update 用一份新定义覆盖既有任务（编辑入口）。
//
// 语义是**整体替换**（PUT）：面板上是什么，任务就是什么——名称/类型/周期或
// RunAt/锚点/工作区/启用状态一并按入参改写，校验走与创建**同一份**判据
// （normalizeSpec），不存在"创建时拦得住、编辑时漏得过"。
//
// 运行账目保留（run_count / 上次结果 / 上次落点）：那是"这个任务跑过什么"，
// 编辑改的是"接下来怎么跑"，两件事不该互相清空。下次运行时间按新定义重算，
// 停机/改期错过的触发点同样不追补。
//
// 落盘顺序与取消一致：先写新的定义行（同一 ID 的后写行在重启时覆盖旧行），
// 写失败 = 这次编辑不成立（内存原样）。
func (s *State) Update(_ context.Context, id string, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("任务 ID 不能为空")
	}
	s.mu.Lock()
	t, exists := s.tasks[id]
	s.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("定时任务 %q 不存在", id)
	}
	definition, err := s.normalizeSpec(spec)
	if err != nil {
		return nil, err
	}
	if err := s.persistRecord(ScheduledTaskRecord{
		ID: id, Spec: definition.spec, Enabled: definition.spec.Enabled,
	}); err != nil {
		return nil, fmt.Errorf("编辑定时任务落盘失败（未修改）: %w", err)
	}
	s.mu.Lock()
	if _, alive := s.tasks[id]; !alive {
		// 落盘期间并发取消赢了：内存里已经没有它，磁盘上刚写的那行也不能留下
		// （否则重启会把这任务"复活"回来）。补一行墓碑，让盘与内存同向。
		s.mu.Unlock()
		_ = s.persistDefinition(t, true)
		return nil, fmt.Errorf("定时任务 %q 已不存在", id)
	}
	t.applyDefinition(definition, nextScheduledAt(time.Now(), definition.spec))
	status := t.statusSnapshot()
	s.mu.Unlock()
	s.Start()
	s.observe()
	return &status, nil
}

// normalizedSpec 是校验并归一后的任务定义（创建与编辑共用同一份判据的产物）。
type normalizedSpec struct {
	spec      ScheduledTaskSpec
	oneShot   bool
	effective time.Duration
}

// normalizeSpec 校验并归一创建/编辑入参，是"什么算一个合法任务定义"的**唯一**
// 一份实现：名称与各文本字段 trim、锚点归一、周期与锚点搭配、最小周期、白名单
// 命令与提示词执行器装配、一次性任务语义（创建即启用、不接受周期锚点）。
// 归一后的 spec 可以直接落盘或进内存（不保留调用方的空白与简写锚点写法）。
func (s *State) normalizeSpec(spec ScheduledTaskSpec) (normalizedSpec, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return normalizedSpec{}, errors.New("任务名称不能为空")
	}
	oneShot := !spec.RunAt.IsZero()
	effective := time.Duration(0)
	if oneShot {
		if !spec.RunAt.After(time.Now()) {
			return normalizedSpec{}, errors.New("定时执行时间必须晚于当前时间")
		}
		// 锚点只属于周期任务：一次性任务的时刻就是 RunAt，给了锚点只会被无声忽略。
		if strings.TrimSpace(spec.StartClock) != "" || spec.StartWeekday != dto.WeekdayUnset {
			return normalizedSpec{}, errors.New("一次性定时任务不接受周期锚点（开始时间/星期）")
		}
		// 一次性任务创建即启用，避免"已停用且无法重新启用"的死角。
		spec.Enabled = true
	} else {
		if err := validatePeriod(spec); err != nil {
			return normalizedSpec{}, err
		}
		effective = effectiveInterval(spec)
		if effective < minScheduledInterval {
			return normalizedSpec{}, fmt.Errorf("周期过短：至少 %s", minScheduledInterval)
		}
	}
	switch spec.Kind {
	case ScheduledTaskCommand:
		key := strings.TrimSpace(spec.Command)
		if _, ok := s.commands[key]; !ok {
			return normalizedSpec{}, fmt.Errorf("命令 %q 不在白名单中", key)
		}
	case ScheduledTaskPrompt:
		if strings.TrimSpace(spec.Prompt) == "" {
			return normalizedSpec{}, errors.New("提示词内容不能为空")
		}
		s.mu.Lock()
		executor := s.executor
		s.mu.Unlock()
		if executor == nil {
			return normalizedSpec{}, errors.New("提示词任务执行器未装配")
		}
	default:
		return normalizedSpec{}, fmt.Errorf("未知任务类型 %q", spec.Kind)
	}
	// 装配（触发那次会话/那一轮用什么权限档位与能力包）：档位空 = 默认 full，
	// 插件空 = 继承宿主；两者的非法取值都在这里显式拒绝（不静默降级、不静默去重）。
	tier, err := scheduledPermissionTier(spec.PermissionTier)
	if err != nil {
		return normalizedSpec{}, err
	}
	plugins, err := dto.NormalizePlugins(spec.Plugins, 0)
	if err != nil {
		return normalizedSpec{}, err
	}
	return normalizedSpec{
		spec: ScheduledTaskSpec{
			Name: name, Kind: spec.Kind, Interval: spec.Interval,
			PeriodUnit: spec.PeriodUnit, PeriodValue: spec.PeriodValue,
			StartClock: canonicalStartClock(spec), StartWeekday: spec.StartWeekday,
			RunAt:   spec.RunAt,
			Command: strings.TrimSpace(spec.Command), Prompt: strings.TrimSpace(spec.Prompt),
			SessionID: strings.TrimSpace(spec.SessionID), WorkspaceID: strings.TrimSpace(spec.WorkspaceID),
			PermissionTier: tier, Plugins: plugins,
			Enabled: spec.Enabled,
		},
		oneShot:   oneShot,
		effective: effective,
	}, nil
}

// scheduledPermissionTier 归一任务声明的权限档位：空 = **默认 full access**。
//
// 为什么与主会话的默认相反（主会话"空 = 手动"）：定时任务在后台跑，没有人在
// 审批弹窗上点"同意"——默认手动等于每次触发都卡死在一次永远没人回答的审批上。
// 这个默认只落在**这次任务新建的会话**上，不改进程默认档位、不影响其它会话。
func scheduledPermissionTier(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return dto.PermissionTierFull, nil
	}
	return dto.NormalizePermissionTier(raw)
}

// applyDefinition 把归一后的定义写进任务本体与它的状态快照（创建、编辑、冷启动
// 重建共用）：定义字段的落点只有这一处，也就不可能出现"编辑后某几格还是旧值"。
// 它不碰运行账目（run_count / 上次结果 / 上次落点）与 running 标志。
func (t *task) applyDefinition(definition normalizedSpec, nextRun time.Time) {
	t.spec = definition.spec
	t.nextRun = nextRun
	status := &t.status
	status.Name = definition.spec.Name
	status.Kind = string(definition.spec.Kind)
	status.IntervalSec = int64(definition.effective / time.Second)
	status.PeriodUnit = string(definition.spec.PeriodUnit)
	status.PeriodValue = definition.spec.PeriodValue
	status.StartClock = definition.spec.StartClock
	status.StartWeekday = definition.spec.StartWeekday
	status.RunAt = nextRun
	status.OneShot = definition.oneShot
	status.Command = definition.spec.Command
	status.Prompt = definition.spec.Prompt
	status.SessionID = definition.spec.SessionID
	status.WorkspaceID = definition.spec.WorkspaceID
	status.PermissionTier = definition.spec.PermissionTier
	status.Plugins = append([]string(nil), definition.spec.Plugins...)
	status.Enabled = definition.spec.Enabled
	status.NextRunAt = nextRun
}

// CancelTask 取消并移除任务（运行中的执行不受影响，完成回写丢弃）。
func (s *State) CancelTask(id string) error {
	s.mu.Lock()
	t, exists := s.tasks[id]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("周期任务 %q 不存在", id)
	}
	s.mu.Unlock()
	// 先落墓碑再删内存：写失败 = 取消失败（任务仍在，重启后也还在），
	// 不会出现"面板没了、磁盘还在"的劈叉。
	if err := s.persistDefinition(t, true); err != nil {
		return fmt.Errorf("取消定时任务落盘失败（未取消）: %w", err)
	}
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
	s.observe()
	return nil
}

// Restore 从全局 JSONL 读回任务定义（冷启动重建），返回（恢复数, 跳过数）。
//
// 恢复口径（与运行期同一套判据，不另立一份）：
//   - 周期任务：重算下次运行时间；停机期间错过的触发点**不追补**（运行期的
//     "错过不追补"在这里同样成立）；停用的任务恢复为停用。
//   - 一次性任务：执行时刻还没到 → 按原时刻恢复；已经过去（进程当时没在跑）
//     → 不恢复：一次性的时刻过了就没有可执行的意义，也不留一条假的待运行行。
//   - 命令不在白名单 / 周期非法 / 名称为空 → **逐条跳过**（跳过数计数），
//     一条坏记录不挡住其余任务；跳过与失败都以返回值报告给装配根。
//
// 调用时机必须在执行器与观察者注入**之后**（恢复出来的任务会立刻按排期触发）。
func (s *State) Restore() (int, int, error) {
	if s == nil {
		return 0, 0, nil
	}
	s.mu.Lock()
	store := s.store
	s.mu.Unlock()
	if store == nil {
		return 0, 0, nil
	}
	records, err := store.Load()
	if err != nil {
		return 0, 0, err
	}
	now := time.Now()
	restored, skipped := 0, 0
	for _, record := range records {
		ok, restoreErr := s.restoreRecord(record, now)
		if restoreErr != nil {
			return restored, skipped, fmt.Errorf("恢复定时任务 %q: %w", record.ID, restoreErr)
		}
		if !ok {
			skipped++
			continue
		}
		restored++
	}
	if restored > 0 {
		s.Start()
		s.observe()
	}
	return restored, skipped, nil
}

// restoreRecord 把一行持久化定义还原成内存任务；返回 false = 这条不适用
// （已过期/已失效/坏记录），调用方按"跳过"计数。
//
// 判据复用 normalizeSpec：周期搭配、白名单命令、执行器装配、一次性语义都在
// 那一份里；这里只加一条恢复特有的口径——**执行时刻已过的一次性任务不恢复**
// （进程当时没在跑，追补没有意义）。
func (s *State) restoreRecord(record ScheduledTaskRecord, now time.Time) (bool, error) {
	if strings.TrimSpace(record.ID) == "" {
		return false, nil
	}
	spec := record.Spec
	spec.Enabled = record.Enabled
	definition, err := s.normalizeSpec(spec)
	if err != nil {
		return false, nil // 坏记录 / 已过期的一次性任务：逐条跳过
	}
	if definition.oneShot && !definition.spec.RunAt.After(now) {
		return false, nil
	}
	t := &task{id: record.ID}
	t.applyDefinition(definition, nextScheduledAt(now, definition.spec))
	t.status.ID = t.id
	t.status.LastStatus = scheduledStatusPending
	s.mu.Lock()
	s.tasks[t.id] = t
	s.mu.Unlock()
	return true, nil
}

// persistRecord 把一行记录写进全局 JSONL（store 未装配时是空操作）。写失败
// 返回错误——调用方据此决定"这次变更算不算成立"。
func (s *State) persistRecord(record ScheduledTaskRecord) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	store := s.store
	s.mu.Unlock()
	if store == nil {
		return nil
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	return store.Append(record)
}

// persistDefinition 把一条任务定义（含启用状态）写进全局 JSONL；deleted=true
// 写的是取消墓碑行。
func (s *State) persistDefinition(t *task, deleted bool) error {
	if s == nil || t == nil {
		return nil
	}
	s.mu.Lock()
	record := ScheduledTaskRecord{
		ID: t.id, Spec: t.spec, Enabled: t.spec.Enabled, Deleted: deleted,
	}
	s.mu.Unlock()
	return s.persistRecord(record)
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
		WorkspaceID: t.status.WorkspaceID, LastSessionID: t.status.LastSessionID,
		PermissionTier: t.status.PermissionTier, Plugins: append([]string(nil), t.status.Plugins...),
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
