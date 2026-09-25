package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 后台命令的轮询型执行域·登记表与状态机（规格见
// docs/2026-09-24-async-tool-deferred-ack/README.md §8 与 §10）。
//
// 形状：派发一次 → 立即拿到 handle + 现状；想知道进展就再调 async_output(handle, wait_ms)，
// 想终止就调 async_kill(handle)。每一问一答都是一对正常的 tool_call/tool_result，所以：
// 历史只追加、不回写；不需要"结果到达时把空闲会话叫醒"那条链路（它必然回闯被 ChatStream
// 全程持有的会话锁）。推送/补记路线已废：见同文件 §5 D1 与 §8 的选型说明。
//
// 本文件只管表与状态；执行体在 async_run.go，工具面在 async_tools.go。

const (
	asyncStateRunning = "running"
	asyncStateDone    = "done"
	asyncStateFailed  = "failed"
	// asyncStateKilled = 由 async_kill 或会话销毁终止，与"命令自己退非零"可分。
	asyncStateKilled = "killed"

	// asyncMaxRunning 是在途上限；asyncMaxRecords 是记录总槽（只为兜内存，
	// 超了驱逐最老的已完成项）。上限只数在跑的，否则长会话累计到 32 条就再也发不出。
	asyncMaxRunning = 32
	asyncMaxRecords = 256

	// asyncHardCap 是后台命令的最长存活时间。同步路径的 scopedToolTimeout 不适用：
	// 受理回执一返回，工具调用的 ctx 就失效了，执行体必须自带独立上限。
	asyncHardCap = 30 * time.Minute

	// asyncDefaultWaitMS / asyncMaxWaitMS 是 async_output 的等待预算。上限同时是
	// "单次工具调用最多钉住会话多久"的上限（不变量 I-22）：ChatStream 在工具执行
	// 期间持有会话锁，用户输入与审批都要等它。
	asyncDefaultWaitMS = 5000
	asyncMaxWaitMS     = 60000

	// asyncPollTailBudget 是单次取回最多带回的增量字符数；asyncLogCap 是输出文件字节上限，
	// 超了截断并在正文里说明——不封顶的话一条后台命令能撑爆磁盘也能烧光缓存预算。
	asyncPollTailBudget = 4000
	asyncLogCap         = 1 << 20

	// asyncProbeThrottle 是"输出有新字节"信号的最小间隔：工作打点表要看得出在动，
	// 但不能每行日志都重投影一次整表。它是**去抖**（只限制发信号的频率），不用来
	// 推断命令死活——按墙钟猜状态一律禁止。
	asyncProbeThrottle = time.Second
	// asyncProbeTailBytes 是探针读文件末尾的窗口；asyncProbeTailRunes 是末行进投影
	// 的字符上限（一条进度条能长到几 KB，界面上只需要看见最后一眼）。
	asyncProbeTailBytes = 512
	asyncProbeTailRunes = 120
)

// asyncRun 是一次后台执行。startedAt/exit/killRequested 只在判定与回读时用，
// 不进正文（正文必须确定性）。cmd 只在 g.mu 内读写：杀动作在锁外做。
//
// description/command/batchID/endedAt/lastSignalAt 只服务**投影读侧**（工作表格与
// 实时探针，见 async_probe.go）：它们一律不进 asyncPayload，因此不受"载荷必须
// 确定性、不含时间戳"那条缓存纪律约束——那条纪律只管进上下文的字节。
type asyncRun struct {
	handle string
	// seq = 句柄的数字序号：投影要按派发顺序稳定排（"a10" 的字符串序排在 "a2" 前）。
	seq         int
	sessionID   string
	key         string
	description string
	command     string
	batchID     string
	state       string
	exit        int
	logPath     string
	cursor      int64 // 已交付给模型的文件偏移，决定"增量"从哪算
	capHit      bool  // 输出文件是否触到字节上限
	done        chan struct{}
	startedAt   time.Time
	endedAt     time.Time
	// lastSignalAt = 上一次因"有新字节"发投影信号的时刻（只在 g.mu 内读写）。
	// 它是去抖时间戳，不是"多久没动就算卡死"的判据。
	lastSignalAt time.Time
	// killRequested = 终止请求已确认发出（不是"请求过就算"，见 markKilled）。
	killRequested bool
	// tree = 该执行体所属的可终止进程树（Windows 上是 Job Object）。nil = 还没起
	// 来或建树失败，此时没有任何"可杀"的对象。只在 g.mu 内读写，终止动作在锁外做。
	tree processTree
}

// processTree 是执行体所属进程树的最小面（实现在 seelebridge/security，
// Windows = Job Object，POSIX = 进程组）。做成接口只为让"杀"这条路径在单测里
// 能在没有真进程时被判据覆盖。
type processTree interface {
	Terminate() error
	Close()
	// Degraded = 树没建出来或进程没挂上：此时"终止"只能打到直接子进程。投影要如实
	// 标出这条差别，不能让用户以为杀干净了。
	Degraded() bool
}

// asyncRegistry 是后台执行表。单锁；工具 goroutine 只写自己那条，读侧在 handler 里。
type asyncRegistry struct {
	mu      sync.Mutex
	runs    map[string]*asyncRun
	byKey   map[string]string
	seq     int
	dir     string
	dirErr  error
	tempDir string
	// closed = CloseAsync 已调用。此刻若还有执行体在跑，删目录只会撞上未关闭的句柄，
	// 所以只记意图，由最后一条收尾补删（见 finish / close）。
	closed bool
	// changed 是"表内容有可见变化"的信号口（派发/终态/驱逐/新字节）：驱动工作表格
	// 重投影。容量 1 + 非阻塞发送 = latest-wins，读侧自己汇聚，绝不把执行域按住在
	// 等一个慢消费者。
	changed chan struct{}
}

func newAsyncRegistry() *asyncRegistry {
	return &asyncRegistry{
		runs:    map[string]*asyncRun{},
		byKey:   map[string]string{},
		changed: make(chan struct{}, 1),
	}
}

// Events 返回状态变化信号口（供装配层转成 Runtime 的投影事件）。
func (g *asyncRegistry) Events() <-chan struct{} {
	if g == nil {
		return nil
	}
	return g.changed
}

// asyncKey 是去重键：同一会话同一条命令在途期间只有一个执行体。
func asyncKey(sessionID, command string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + command))
	return hex.EncodeToString(sum[:8])
}

func (g *asyncRegistry) directory() (string, error) {
	if g.dirErr != nil {
		return "", g.dirErr
	}
	if g.dir != "" {
		return g.dir, nil
	}
	dir, err := os.MkdirTemp(g.tempDir, "seelex-async-")
	if err != nil {
		g.dirErr = err
		return "", err
	}
	g.dir = dir
	return dir, nil
}

// begin 登记一次派发。去重命中时返回 (existing, false, nil)：调用方据此回"已在跑"，
// 不重跑（轮询型下模型能自己取回，重发同命令几乎总是误解，拦掉更省）。
//
// description = 模型写的"这条命令在干什么"，batchID = 派发它的那次 chat 请求：两者只给
// 投影读侧（工作表格行标题与批次归属），不进模型载荷。
//
// 按值返回（与 snapshot 同口径）：表内那条 asyncRun 的 state/exit 由执行体收尾时
// 在锁内改写，派发侧若拿到指针，就是在锁外读这两个字段——回执渲染只需要 handle 与
// logPath，给副本即可，且"回执渲染的是派发那一刻的状态"本身就是对的语义。
func (g *asyncRegistry) begin(sessionID, command, description, batchID string) (asyncRun, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		// 关停后再登记就会新建一个没人回收的目录（CloseAsync 已经跑过），宁可报错。
		return asyncRun{}, false, fmt.Errorf("bash: 后台执行域已关停（进程正在收尾）")
	}
	key := asyncKey(sessionID, command)
	if id, ok := g.byKey[key]; ok {
		if run := g.runs[id]; run != nil && run.state == asyncStateRunning {
			return *run, false, nil
		}
	}
	if g.countRunningLocked() >= asyncMaxRunning {
		return asyncRun{}, false, fmt.Errorf("bash: 后台执行在途已满 %d 个，先用 async_output 等其中一个结束", asyncMaxRunning)
	}
	g.evictLocked()
	dir, err := g.directory()
	if err != nil {
		return asyncRun{}, false, fmt.Errorf("bash: 无法创建后台输出目录: %w", err)
	}
	// seq 单调：不能用 len(runs) 推，驱逐过已完成项后会同号，两条执行共用输出文件。
	g.seq++
	handle := fmt.Sprintf("a%d", g.seq)
	run := &asyncRun{
		handle:      handle,
		seq:         g.seq,
		sessionID:   sessionID,
		key:         key,
		description: description,
		command:     command,
		batchID:     batchID,
		state:       asyncStateRunning,
		logPath:     filepath.Join(dir, handle+".log"),
		done:        make(chan struct{}),
		startedAt:   time.Now(),
	}
	g.runs[handle] = run
	g.byKey[key] = handle
	g.notifyLocked()
	return *run, true, nil
}

func (g *asyncRegistry) countRunningLocked() int {
	count := 0
	for _, run := range g.runs {
		if run.state == asyncStateRunning {
			count++
		}
	}
	return count
}

// countRunningFor 报告某会话此刻还在跑的后台执行数。
//
// 它是「无进展预算」的判据输入（见 application/core/task_context）：一条安静的长命令
// 连续被取回时载荷逐字节相同（载荷必须确定性，否则每轮白烧缓存），进展计数就不推进；
// 但"有在途执行被查询"本身就是进展——命令只是还没完，不是模型在原地打转。
func (g *asyncRegistry) countRunningFor(sessionID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	count := 0
	for _, run := range g.runs {
		if run.state == asyncStateRunning && run.sessionID == sessionID {
			count++
		}
	}
	return count
}

// evictLocked 驱逐最老的已完成记录（连带去重键与输出文件）把表封顶。在途项永不丢。
//
// 输出文件必须连带删：记录槽有上限、文件没有——只封记录会让临时目录在一个长
// 会话里无界增长。删除是尽力而为（失败只意味着那份日志多留一会儿）。
func (g *asyncRegistry) evictLocked() {
	for len(g.runs) >= asyncMaxRecords {
		oldestID, oldest := "", (*asyncRun)(nil)
		for id, run := range g.runs {
			if run.state == asyncStateRunning {
				continue
			}
			if oldest == nil || run.startedAt.Before(oldest.startedAt) {
				oldestID, oldest = id, run
			}
		}
		if oldest == nil {
			return
		}
		delete(g.byKey, oldest.key)
		delete(g.runs, oldestID)
		_ = os.Remove(oldest.logPath)
	}
}

// attach 记下执行体所属的进程树（只有起得来的进程才可能被杀）。返回 false = 记录
// 已被收尾或驱逐，调用方据此自己关掉那棵树。
func (g *asyncRegistry) attach(handle string, tree processTree) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok || run.state != asyncStateRunning {
		return false
	}
	run.tree = tree
	return true
}

// killTarget 取"该不该杀、杀哪棵树"。锁内只做校验与取对象，终止动作一律在锁外做
// ——finish 也要这把锁，等 taskkill / TerminateJobObject 返回不能把整张表按住。
//
// 三种结果：需要杀（返回树）、已结束（alreadyDone，不是错误）、句柄无效（error）。
func (g *asyncRegistry) killTarget(handle, sessionID string) (processTree, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		return nil, false, fmt.Errorf("async_kill: 未知句柄 %q（可能已被驱逐，或进程重启后登记表已清空）", handle)
	}
	if run.sessionID != sessionID {
		return nil, false, fmt.Errorf("async_kill: 句柄 %q 不属于本会话", handle)
	}
	if run.state != asyncStateRunning {
		return nil, true, nil
	}
	if run.tree == nil {
		// 起得来但还没 Start 完（attach 未落地）：这一刻没有可杀的对象。报"暂不可杀"
		// 而不是谎报成功——派发返回前模型拿不到句柄，正常路径走不到这里。
		return nil, false, fmt.Errorf("async_kill: 句柄 %q 的执行体尚未就绪，稍后再试", handle)
	}
	return run.tree, false, nil
}

// killSession 杀某会话全部在途执行（会话销毁即杀）。同 killTarget：锁内取目标，
// 锁外动手。返回登记的句柄数。
func (g *asyncRegistry) killSession(sessionID string) int {
	g.mu.Lock()
	var targets []string
	var trees []processTree
	for handle, run := range g.runs {
		if run.sessionID == sessionID && run.state == asyncStateRunning && run.tree != nil {
			targets = append(targets, handle)
			trees = append(trees, run.tree)
		}
	}
	g.mu.Unlock()

	for i, tree := range trees {
		// 尽力而为：一个杀不掉不影响其余（杀不掉的仍归 awaitAsync 收敛）。
		if tree.Terminate() == nil {
			g.markKilled(targets[i])
		}
	}
	return len(targets)
}

// markKilled 在终止请求**确认成功之后**记下"是我们杀的"。终态仍由 awaitAsync 合成，
// 所以这里只落意图、不改 state——改了就会有"状态是 killed 但进程还在跑"的双轨。
func (g *asyncRegistry) markKilled(handle string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[handle]; ok {
		run.killRequested = true
	}
}

func (g *asyncRegistry) finish(handle string, exitCode int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		return
	}
	run.endedAt = time.Now()
	switch {
	case run.killRequested:
		// 终止请求已确认发出，进程随之结束：终态归因给杀，不混进"命令自己退非零"。
		run.exit = asyncKilledExit
		run.state = asyncStateKilled
	case exitCode != 0:
		run.exit = exitCode
		run.state = asyncStateFailed
	default:
		run.exit = exitCode
		run.state = asyncStateDone
	}
	close(run.done)
	g.notifyLocked()
	// 关停时若还有句柄没关，close 删不掉；这里是补删点（文件已由 awaitAsync 关闭，
	// 且没有人再读这个句柄）。别的执行体还在写的话这次删除照样失败，交给它自己的
	// 收尾再试一次——不需要定时器，也不用判断"多久算陈旧"。
	if g.closed {
		g.removeDirLocked()
	}
}

// snapshot 复制一份判定所需字段，避免把锁外的渲染代码接进锁内。
func (g *asyncRegistry) snapshot(handle string) (asyncRun, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		return asyncRun{}, false
	}
	return *run, true
}

// advanceTail 取 (cursor, 末尾增量, 是否触顶)：一次轮询只交付**新增**部分，
// 反复轮询同一命令不会把整份输出重播进上下文（那是轮询型唯一真实的 token 风险）。
// 返回的 cursor 由调用方在确认交付后推进，所以这里只读不写。
func (g *asyncRegistry) advanceTail(handle string, budget int) (next int64, delta string, truncated bool, ok bool) {
	g.mu.Lock()
	run, exists := g.runs[handle]
	if !exists {
		g.mu.Unlock()
		return 0, "", false, false
	}
	cursor, logPath, capHit := run.cursor, run.logPath, run.capHit
	g.mu.Unlock()

	info, err := os.Stat(logPath)
	if err != nil {
		return cursor, "", capHit, true
	}
	size := info.Size()
	if size <= cursor {
		return cursor, "", capHit, true
	}
	end := size
	if end-cursor > int64(budget) {
		end = cursor + int64(budget)
	}
	file, err := os.Open(logPath)
	if err != nil {
		return cursor, "", capHit, true
	}
	defer file.Close()
	chunk := make([]byte, end-cursor)
	if _, err := file.ReadAt(chunk, cursor); err != nil {
		return cursor, "", capHit, true
	}
	return end, string(chunk), capHit || size > asyncLogCap, true
}

// markCursor 推进已交付偏移；truncated=true 时同时记下表，后续取回都要带上提示。
func (g *asyncRegistry) markCursor(handle string, next int64, truncated bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[handle]; ok {
		run.cursor = next
		run.capHit = run.capHit || truncated
	}
}

// markCapHit 登记"该次执行的输出已触字节上限"。
func (g *asyncRegistry) markCapHit(handle string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[handle]; ok {
		run.capHit = true
	}
}

// noteOutput 记下"这条执行又写了字节"，按 asyncProbeThrottle 去抖后发一次投影信号。
//
// 为什么要它：打点表要看得出命令在动（字节数、末行都在长），而可见变化只在写的时候
// 发生——一条安静等结果的命令不该刷信号，也不会有人去看它。
//
// 为什么这不违反"不许按墙钟猜状态"：这里的时间只用来限制**发信号的频率**，不参与
// "进程还活着吗"的判断——终态一律由 cmd.Wait 的返回说话（见 awaitAsync）。
func (g *asyncRegistry) noteOutput(handle string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok || run.state != asyncStateRunning {
		return
	}
	now := time.Now()
	if !run.lastSignalAt.IsZero() && now.Sub(run.lastSignalAt) < asyncProbeThrottle {
		return
	}
	run.lastSignalAt = now
	g.notifyLocked()
}

// notifyLocked 发"表内容有可见变化"的信号。调用方必须已持有 g.mu，所以信号
// **必须**是非阻塞的：投影侧慢或没在读，都不能把执行域按住（容量 1 + default
// 丢弃 = latest-wins，读侧醒来时自己取最新整表）。
func (g *asyncRegistry) notifyLocked() {
	if g.changed == nil {
		return
	}
	select {
	case g.changed <- struct{}{}:
	default:
	}
}

// close 记下关停意图并当场试删输出目录。
func (g *asyncRegistry) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	g.removeDirLocked()
}

// removeDirLocked 删目录，成功才忘掉它（失败时留着，等下一个可删点重试）。
// 调用方必须已持有 g.mu。
func (g *asyncRegistry) removeDirLocked() {
	if g.dir == "" {
		return
	}
	if err := os.RemoveAll(g.dir); err != nil {
		return
	}
	g.dir = ""
}

// asyncPayload 是派发回执、取回结果与终止回执的共同载荷。字段刻意不含时间戳与耗时：
// 它会随所在轮次永久留在可缓存前缀里，任何"每次都不一样"的字节都在白烧 token。
type asyncPayload struct {
	Status    string `json:"status"`
	Handle    string `json:"handle"`
	State     string `json:"state"`
	ExitCode  int    `json:"exit_code"`
	LogPath   string `json:"log_path"`
	Output    string `json:"output,omitempty"`
	Repeated  bool   `json:"repeated,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Hint      string `json:"hint"`
}

func encodeAsync(payload asyncPayload) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("bash: 渲染异步载荷失败: %w", err)
	}
	return string(encoded), nil
}

// renderAccepted 渲染派发回执（= 该 tool_call 的 tool 结果，配对在此完成）。
func renderAccepted(run asyncRun, repeated bool) (string, error) {
	hint := "命令已在后台跑。要结果就调 async_output(handle) 并把 wait_ms 给到预期剩余时长" +
		"（上限 60000）；只想确认进展看工作打点表，不必为此花一次往返。不要重复派发同一条命令。"
	if repeated {
		hint = "同一条命令已在后台跑（未重复派发）。用 async_output(handle) 取回它的进展。"
	}
	return encodeAsync(asyncPayload{
		Status: "accepted", Handle: run.handle, State: asyncStateRunning,
		ExitCode: -1, LogPath: run.logPath, Repeated: repeated, Hint: hint,
	})
}

// renderPolled 渲染一次取回：running 带增量，终态带 exit_code 与末尾增量。
func renderPolled(run asyncRun, delta string, truncated bool) (string, error) {
	status, hint := "progress", "仍在运行。需要结果就再调一次 async_output(handle)。"
	switch run.state {
	case asyncStateRunning:
	case asyncStateKilled:
		status, hint = "finished", "命令已被终止（async_kill 或会话销毁）；以上为其输出增量。"
	default:
		status, hint = "finished", "命令已结束；以上为其输出增量。"
	}
	if truncated {
		hint += " 输出已按上限截断，完整内容读 log_path。"
	}
	return encodeAsync(asyncPayload{
		Status: status, Handle: run.handle, State: run.state, ExitCode: run.exit,
		LogPath: run.logPath, Output: delta, Truncated: truncated, Hint: hint,
	})
}

// renderKilled 渲染终止回执。杀完不带输出正文——正文一律走 async_output，
// 否则同一次调用的两种返回形状会让模型分不清"杀掉了"和"拿到了结果"。
func renderKilled(run asyncRun) (string, error) {
	return encodeAsync(asyncPayload{
		Status: "killed", Handle: run.handle, State: asyncStateRunning,
		ExitCode: -1, LogPath: run.logPath,
		Hint: "终止请求已发出。用 async_output(handle) 取末尾增量与终态（state=killed, exit_code=137）。",
	})
}

// renderAlreadyFinished 渲染"不用杀"：句柄已经终态不是错误，但也不能谎报杀了谁。
func renderAlreadyFinished(run asyncRun) (string, error) {
	return encodeAsync(asyncPayload{
		Status: "already_finished", Handle: run.handle, State: run.state,
		ExitCode: run.exit, LogPath: run.logPath,
		Hint: "命令早已结束，未做任何终止。用 async_output(handle) 取结果。",
	})
}
