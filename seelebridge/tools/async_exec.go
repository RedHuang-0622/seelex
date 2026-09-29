package tools

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// 后台命令的轮询型执行域·登记表与状态机（规格见
// docs/2026-09-24-async-tool-deferred-ack/README.md §8 与 §10）。
//
// 形状：派发一次（bash_bg / read_batch / subagent 作业）→ 立即拿到 handle + 现状；
// 想知道进展就再调 job_manage(op=observe)（只读）或 op=fetch（消费式取回增量），
// 想终止就调 op=kill、想结清就调 op=done。每一问一答都是一对正常的
// tool_call/tool_result，所以：
// 历史只追加、不回写；不需要"结果到达时把空闲会话叫醒"那条链路（它必然回闯被 ChatStream
// 全程持有的会话锁）。推送/补记路线已废：见同文件 §5 D1 与 §8 的选型说明。
//
// 本文件只管表与状态；执行体在 async_run.go，工具面在 async_tools.go。

const (
	asyncStateRunning = "running"
	asyncStateDone    = "done"
	asyncStateFailed  = "failed"
	// asyncStateKilled = 由 job_manage(op=kill) 或会话销毁终止，与"命令自己退非零"可分。
	asyncStateKilled = "killed"

	// asyncMaxRunning 是在途上限；asyncMaxRecords 是记录总槽（只为兜内存，
	// 超了驱逐最老的已完成项）。上限只数在跑的，否则长会话累计到 32 条就再也发不出。
	asyncMaxRunning = 32
	asyncMaxRecords = 256

	// asyncHardCap 是后台命令的最长存活时间。同步路径的 scopedToolTimeout 不适用：
	// 受理回执一返回，工具调用的 ctx 就失效了，执行体必须自带独立上限。
	asyncHardCap = 30 * time.Minute

	// asyncDefaultWaitMS / asyncMaxWaitMS 是 job_manage(op=fetch) 的等待预算。上限同时是
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

	// asyncSummaryMaxBytes 是**终态摘要**的硬上限（打点 K-5 的 K-2 约束）：完成行
	// 每轮都会随打点块重播，无界即按轮数线性烧 token。超限截断并标注全文走取回。
	asyncSummaryMaxBytes = 512
	// asyncSummaryTailRunes 是摘要里末行采样的字符上限。
	asyncSummaryTailRunes = 120
	// asyncLineCountCap 是"数行数"的读取上限（与输出文件上限同量级）：数行是 O(文件),
	// 只在终态做一次，但仍设上限，避免超大文件把收尾路径按住。
	asyncLineCountCap = asyncLogCap
)

// 作业类别（Kind）：决定"读取口与 kill 语义"（设计文档 §A.2 的行 schema）。
//
// 三类都在**同一张登记表、同一套状态机**上，差别只在执行体：
//   - process  = 后台 shell 子进程（进程树可终止，输出走日志文件）；
//   - inline   = 进程内的扇出作业（批量读：没有进程，取消靠 ctx，输出同样落日志文件）；
//   - subagent = 子代理（执行体是 plan 节点，取消靠 ctx，产出走日志文件）。
const (
	asyncKindProcess  = "process"
	asyncKindInline   = "inline"
	asyncKindSubagent = "subagent"
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
	// kind = process | inline | subagent（打点 K-2）：决定读取口与 kill 语义。
	kind string
	// index = 同批内 wire 下标（批量派发用）。它是**排序键**，不是完成序：
	// 一批 N 个作业的投影必须按派发序排，否则同一批的多次请求会看到不同的行顺序。
	index int
	// summary = 终态摘要（≤asyncSummaryMaxBytes）；lines = 输出行数。两者都只在
	// 终态由执行体判定之后才有值（打点 K-5 的"回填内容 = 有界摘要"）。
	summary string
	lines   int
	// notified = 该作业的终态**已回填过一次**（打点 K-5 的 K-4 约束：幂等键）。
	// 终态迁移只发生一次，所以这个位只会被置一次；重复 finish / 重复 done 都不
	// 会产生第二次回填。
	notified bool
	// cancel 是非进程作业（inline/subagent）的取消口：它们没有进程树可杀，
	// 终止只能靠取消 context。进程作业的取消口是 tree（见 processTree）。
	cancel func()
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
	// retired 是**销项墓碑**：销项后句柄不在册了，但"重复 done / 重复 fetch"必须是
	// **幂等的无副作用**操作（K-5 的 K-4），不能变成"未知句柄"这种含糊错误。
	// 墓碑按 FIFO 封顶（只记状态字面量，不记正文，不占资源）。
	retired       map[string]string
	retiredRing   []string
	retiredMaxLen int
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

// ensureDirectory 返回（必要时创建）作业输出根目录。created=true 表示**本次调用
// 建的**（调用方据此在关停竞争里回收它）。
//
// 目录创建（os.MkdirTemp）在 g.mu **之外**做：临界区只装表内改写（同 killTarget /
// retire 的纪律）。锁内只做"查已有 / 登记新建"，并发下保留先建好的那一个（没人回收
// 多余的目录）。
func (g *asyncRegistry) ensureDirectory() (dir string, created bool, err error) {
	g.mu.Lock()
	existing, dirErr := g.dir, g.dirErr
	g.mu.Unlock()
	if dirErr != nil {
		return "", false, dirErr
	}
	if existing != "" {
		return existing, false, nil
	}
	fresh, createErr := os.MkdirTemp(g.tempDir, "seelex-async-")
	if createErr != nil {
		g.mu.Lock()
		if g.dirErr == nil {
			g.dirErr = createErr
		}
		g.mu.Unlock()
		return "", false, createErr
	}
	g.mu.Lock()
	if g.dir != "" {
		// 并发下已被别人建好：保留先到的那一个，删掉自己刚建的（此时它是空目录）。
		winner := g.dir
		g.mu.Unlock()
		_ = os.Remove(fresh)
		return winner, false, nil
	}
	g.dir = fresh
	g.mu.Unlock()
	return fresh, true, nil
}

// begin 登记一次后台命令派发（Kind=process 的便捷入口，历史调用点与用例都走它）。
func (g *asyncRegistry) begin(sessionID, command, description, batchID string) (asyncRun, bool, error) {
	return g.beginJob(JobSpec{
		Kind: asyncKindProcess, SessionID: sessionID,
		Command: command, Title: description, BatchID: batchID, Dedup: true,
	})
}

// beginJob 登记一次派发——**三类作业（process / inline / subagent）共用这一张表**，
// 这是打点 K-1 里 `Add` 的落点。
//
// 去重只对声明了 Dedup 的作业生效（后台命令：同一会话同一条命令在途期间只留一个，
// 模型重发同一条命令几乎总是误解）；批量扇出的作业各自独立，不去重。
//
// description = 模型写的"这条命令在干什么"，batchID = 派发它的那次 chat 请求：两者只给
// 投影读侧（工作表格行标题与批次归属），不进模型载荷。
//
// 按值返回（与 snapshot 同口径）：表内那条 asyncRun 的 state/exit 由执行体收尾时
// 在锁内改写，派发侧若拿到指针，就是在锁外读这两个字段——回执渲染只需要 handle 与
// logPath，给副本即可，且"回执渲染的是派发那一刻的状态"本身就是对的语义。
func (g *asyncRegistry) beginJob(spec JobSpec) (asyncRun, bool, error) {
	// 关停先判：免得为一个注定被拒的登记新建目录（CloseAsync 已经跑过，没人回收）。
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return asyncRun{}, false, fmt.Errorf("job: 作业执行域已关停（进程正在收尾）")
	}
	// 输出目录创建在**锁外**（MkdirTemp 是文件 I/O；临界区只装表内改写）。
	dir, created, err := g.ensureDirectory()
	if err != nil {
		return asyncRun{}, false, fmt.Errorf("job: 无法创建作业输出目录: %w", err)
	}

	// 被驱逐记录的日志文件在**锁外**删（evictLocked 只摘表）：文件 I/O 不进临界区，
	// 否则一条疯狂写日志的命令会让"回收"把派发与探针一起按住（同 killTarget 的纪律）。
	var evicted []string
	g.mu.Lock()
	defer func() {
		g.mu.Unlock()
		for _, path := range evicted {
			_ = os.Remove(path)
		}
	}()

	if g.closed {
		// 关停后再登记就会新建一个没人回收的目录（CloseAsync 已经跑过），宁可报错；
		// 自己刚建的那个当场回收（os.Remove 只删空目录，已有日志时失败也无害）。
		if created {
			evicted = append(evicted, dir)
		}
		return asyncRun{}, false, fmt.Errorf("job: 作业执行域已关停（进程正在收尾）")
	}
	key := ""
	if spec.Dedup {
		key = asyncKey(spec.SessionID, spec.Command)
		if id, ok := g.byKey[key]; ok {
			if run := g.runs[id]; run != nil && run.state == asyncStateRunning {
				return *run, false, nil
			}
		}
	}
	if g.countRunningLocked() >= asyncMaxRunning {
		return asyncRun{}, false, fmt.Errorf("job: 在途作业已满 %d 个，先用 job_manage(op=observe) 查看、或等其中一个结束", asyncMaxRunning)
	}
	evicted = append(evicted, g.evictLocked()...)
	// seq 单调：不能用 len(runs) 推，驱逐过已完成项后会同号，两条执行共用输出文件。
	g.seq++
	handle := fmt.Sprintf("a%d", g.seq)
	kind := string(spec.Kind)
	if kind == "" {
		kind = asyncKindProcess
	}
	run := &asyncRun{
		handle:      handle,
		seq:         g.seq,
		sessionID:   spec.SessionID,
		key:         key,
		description: spec.Title,
		command:     spec.Command,
		batchID:     spec.BatchID,
		state:       asyncStateRunning,
		kind:        kind,
		index:       spec.Index,
		logPath:     filepath.Join(dir, handle+".log"),
		done:        make(chan struct{}),
		startedAt:   time.Now(),
	}
	g.runs[handle] = run
	if key != "" {
		g.byKey[key] = handle
	}
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

// evictLocked 驱逐最老的已完成记录（连带去重键）把表封顶，返回**待删的输出文件
// 路径**（调用方在锁外删）。在途项永不丢。
//
// 输出文件必须连带删：记录槽有上限、文件没有——只封记录会让临时目录在一个长
// 会话里无界增长。删除是尽力而为（失败只意味着那份日志多留一会儿），但**动作必须
// 在锁外**：os.Remove 是文件 I/O，持 g.mu 删会把派发/收尾/探针一起按住。
func (g *asyncRegistry) evictLocked() []string {
	var removed []string
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
			return removed
		}
		delete(g.byKey, oldest.key)
		delete(g.runs, oldestID)
		removed = append(removed, oldest.logPath)
	}
	return removed
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
// （Kind=process 专用；非进程作业的取消口走 killHandle。）
func (g *asyncRegistry) killTarget(handle, sessionID string) (processTree, bool, error) {
	tree, _, alreadyDone, err := g.killHandle(handle, sessionID)
	return tree, alreadyDone, err
}

// killSession 杀某会话全部在途执行（会话销毁即杀）。同 killTarget：锁内取目标，
// 锁外动手。返回登记的句柄数。
//
// 两类执行体都要覆盖：进程作业走进程树终止，非进程作业（inline / subagent）走
// 取消口——漏掉后者的后果是"会话没了，子代理/扇出作业还在跑"，而它们的句柄再没有
// 任何一条取回路径能拿到（无人认领的孤儿）。
func (g *asyncRegistry) killSession(sessionID string) int {
	g.mu.Lock()
	var targets []string
	var trees []processTree
	var cancels []func()
	for handle, run := range g.runs {
		if run.sessionID != sessionID || run.state != asyncStateRunning {
			continue
		}
		switch {
		case run.tree != nil:
			targets = append(targets, handle)
			trees = append(trees, run.tree)
			cancels = append(cancels, nil)
		case run.cancel != nil:
			targets = append(targets, handle)
			trees = append(trees, nil)
			cancels = append(cancels, run.cancel)
		}
	}
	g.mu.Unlock()

	for index, tree := range trees {
		// 尽力而为：一个杀不掉不影响其余（杀不掉的仍归 awaitAsync 收敛）。
		if tree != nil {
			if tree.Terminate() == nil {
				g.markKilled(targets[index])
			}
			continue
		}
		if cancels[index] != nil {
			cancels[index]()
			g.markKilled(targets[index])
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

// finish 合成终态。**恰好一次**：正常退出、硬超时、被杀、执行体 panic，四条路都
// 必须落到这里，否则句柄会永久停在 running，模型只能空转到死；而重复落到这里
// （例如 kill 之后执行体又收尾一次）不得产生第二次状态迁移——那会重复回填，
// 也会让 `close(run.done)` 二次关闭而 panic（打点 K-6 的判据）。
//
// 终态只由执行体判定（K-5 的 K-5 约束）：本函数不做任何"多久没动就算死了"的推断，
// 它只接受执行体给出的退出码。
func (g *asyncRegistry) finish(handle string, exitCode int) {
	// 摘要在锁外算（要读日志文件）：登记表纪律是"锁内不做文件 I/O"。
	snap, ok := g.snapshot(handle)
	if !ok {
		return
	}
	if snap.state != asyncStateRunning {
		// 已经是终态：这是重复收尾，无副作用地返回（幂等键在状态上）。
		return
	}
	state := asyncStateDone
	switch {
	case snap.killRequested:
		state = asyncStateKilled
		exitCode = asyncKilledExit
	case exitCode != 0:
		state = asyncStateFailed
	}
	summary, lines := summarizeLog(snap.logPath, exitCode, state)

	g.mu.Lock()
	reapDir := false
	defer func() {
		g.mu.Unlock()
		if reapDir {
			// 删目录在锁外（os.RemoveAll 是文件 I/O）。
			g.removeDir()
		}
	}()
	run, ok := g.runs[handle]
	if !ok || run.state != asyncStateRunning {
		// 记录已被销项/驱逐，或另一个入口先一步落了终态：都不再迁移（只迁移一次）。
		return
	}
	run.endedAt = time.Now()
	run.exit = exitCode
	run.state = state
	run.summary = summary
	run.lines = lines
	// 回填位：终态迁移只发生一次 ⇒ 这个位只会被置一次（K-5 的 K-4 约束）。
	run.notified = true
	close(run.done)
	g.notifyLocked()
	// 关停时若还有句柄没关，close 删不掉；这里是补删点（文件已由 awaitAsync 关闭，
	// 且没有人再读这个句柄）。别的执行体还在写的话这次删除照样失败，交给它自己的
	// 收尾再试一次——不需要定时器，也不用判断"多久算陈旧"。
	if g.closed {
		reapDir = true
	}
}

// retire 销项：把一条**已终态**的记录从表里摘掉（连带去重键与输出文件）。
//
// 语义（设计文档 §A.3 的行生命周期）：模型取回/确认之后，history 里的工具结果就是
// 唯一事实，投影里的那一行随之消失。在途作业永远不会被销项——那是"丢掉一个还在跑的
// 执行体"，连句柄都没人再拿得到（会话销毁走 killSession，不是这条路）。
//
// 销项后留下一枚墓碑：重复 done / 重复 fetch 必须幂等（无副作用），而不是变成
// "未知句柄"——模型重试一次调用是常态，含糊错误会让它以为出了别的问题。
//
// 返回 false = 句柄不存在或还在跑（调用方据此报"不能销项"，绝不谎报）。
func (g *asyncRegistry) retire(handle string) bool {
	g.mu.Lock()
	run, ok := g.runs[handle]
	if !ok || run.state == asyncStateRunning {
		g.mu.Unlock()
		return false
	}
	if run.key != "" {
		delete(g.byKey, run.key)
	}
	delete(g.runs, handle)
	logPath := run.logPath
	g.noteRetiredLocked(handle, run.state)
	g.notifyLocked()
	g.mu.Unlock()

	_ = os.Remove(logPath)
	return true
}

// noteRetiredLocked 记一枚销项墓碑（FIFO 封顶）。调用方必须已持有 g.mu。
func (g *asyncRegistry) noteRetiredLocked(handle, state string) {
	if g.retired == nil {
		g.retired = map[string]string{}
	}
	if g.retiredMaxLen <= 0 {
		g.retiredMaxLen = asyncMaxRecords
	}
	if _, exists := g.retired[handle]; !exists {
		g.retiredRing = append(g.retiredRing, handle)
	}
	g.retired[handle] = state
	for len(g.retiredRing) > g.retiredMaxLen {
		oldest := g.retiredRing[0]
		g.retiredRing = g.retiredRing[1:]
		delete(g.retired, oldest)
	}
}

// retiredState 报告某句柄是否**已销项**及其终态字面量。
func (g *asyncRegistry) retiredState(handle string) (string, bool) {
	if g == nil {
		return "", false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.retired[handle]
	return state, ok
}

// setCancel 记下非进程作业的取消口（inline / subagent）：它们没有进程树可杀，
// 终止只能靠取消 context。
func (g *asyncRegistry) setCancel(handle string, cancel func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[handle]; ok {
		run.cancel = cancel
	}
}

// killHandle 取"该不该杀、怎么杀"。锁内只做校验与取对象，终止动作一律在锁外做
// ——finish 也要这把锁，等 taskkill / TerminateJobObject / 取消级联返回不能把
// 整张表按住。
//
// 四种结果：有进程树可杀（返回 tree）、无进程树但有取消口（返回 cancel）、
// 已结束（alreadyDone，不是错误）、句柄无效（error）。
func (g *asyncRegistry) killHandle(handle, sessionID string) (processTree, func(), bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		return nil, nil, false, fmt.Errorf("job_manage: 未知句柄 %q（可能已被销项或驱逐，或进程重启后登记表已清空）", handle)
	}
	if run.sessionID != sessionID {
		return nil, nil, false, fmt.Errorf("job_manage: 句柄 %q 不属于本会话", handle)
	}
	if run.state != asyncStateRunning {
		return nil, nil, true, nil
	}
	if run.tree != nil {
		return run.tree, nil, false, nil
	}
	if run.cancel != nil {
		return nil, run.cancel, false, nil
	}
	// 起得来但还没 Start 完（attach / setCancel 未落地）：这一刻没有可杀的对象。
	// 报"暂不可杀"而不是谎报成功——派发返回前模型拿不到句柄，正常路径走不到这里。
	return nil, nil, false, fmt.Errorf("job_manage: 句柄 %q 的执行体尚未就绪，稍后再试", handle)
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

// appendNote 把**运行体系**产出的正文追加进作业的输出文件。
//
// 它是非进程作业的产出入口（inline 写读到的内容、subagent 写节点摘要与发现）：契约
// 的 fetch 是"按文件偏移取回增量"，所以产出必须落在同一个载体上，三类作业才共用
// 同一套游标语义。写盘在锁外做（登记表纪律：锁内不做文件 I/O），写完补一次新字节
// 信号（与 cappedLogWriter 的 noteOutput 同源），投影因此看得出这条作业在动。
//
// 在途才接受追加：终态之后再追加就没人会按游标取回它（终态行只回填有界摘要），
// 那种字节既进不了上下文也进不了投影，只会留在临时文件里。
func (g *asyncRegistry) appendNote(handle, text string) error {
	if text == "" {
		return nil
	}
	g.mu.Lock()
	run, ok := g.runs[handle]
	if !ok {
		g.mu.Unlock()
		return fmt.Errorf("job: 未知句柄 %q", handle)
	}
	if run.state != asyncStateRunning {
		state := run.state
		g.mu.Unlock()
		return fmt.Errorf("job: 句柄 %q 已终态（%s），不再接受正文追加", handle, state)
	}
	logPath := run.logPath
	g.mu.Unlock()

	file, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("job: 打开作业输出文件: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return fmt.Errorf("job: 写作业输出文件: %w", err)
	}
	g.noteOutput(handle)
	return nil
}

// summarizeLog 生成终态摘要（打点 K-5 的 K-2 约束：**有界**）。它是"回填内容"
// 的全部——退出码 + 行数 + 字节数 + 有界末行，硬上限 asyncSummaryMaxBytes。
//
// 为什么不是全文：完成行随打点块**每轮重播**，塞全文就是按轮数线性烧 token；
// 全文只走 job_manage(op=fetch)（消费式增量）或 log_path。
//
// 为什么不在锁内做：这是文件 I/O。调用方（finish）先取快照、锁外算、再进锁迁移。
func summarizeLog(logPath string, exitCode int, state string) (string, int) {
	bytes, _, tail := sampleLog(logPath)
	lines := countLogLines(logPath)
	parts := []string{state, fmt.Sprintf("exit=%d", exitCode), fmt.Sprintf("%d 行", lines), formatBytesCompact(bytes)}
	if tail != "" {
		parts = append(parts, "末行: "+truncateRunes(tail, asyncSummaryTailRunes))
	}
	summary := strings.Join(parts, " · ")
	// 上限是**字节**（打点 K-2 的硬约束）：末行按字符截断，中文末行 120 字符可以到
	// 480 字节，两者混着算就会让"≤512B"这条上限变成"≤512 字符"而悄悄超限。
	if len(summary) <= asyncSummaryMaxBytes {
		return summary, lines
	}
	// 超限只截摘要，不截事实：尾部标注全文走取回，避免模型以为这就是全部输出。
	const marker = "…（全文经 job_manage(op=fetch) 取回）"
	return truncateBytes(summary, asyncSummaryMaxBytes-len(marker)) + marker, lines
}

// truncateBytes 按**字节**上限截断，且只在 UTF-8 字符边界切（切出半个字符会在
// 投影里变成乱码）。摘要的硬上限是字节，所以这里必须按字节算。
func truncateBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// countLogLines 数输出文件的行数（上限 asyncLineCountCap 字节，防超大文件把收尾
// 路径按住）。它不是精确的 wc -l 复刻——只用于摘要里给一个量级。
func countLogLines(logPath string) int {
	if logPath == "" {
		return 0
	}
	file, err := os.Open(logPath)
	if err != nil {
		return 0
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	lines, read := 0, 0
	for read < asyncLineCountCap {
		chunk, err := reader.ReadBytes('\n')
		read += len(chunk)
		if len(chunk) > 0 {
			lines++
		}
		if err != nil {
			break
		}
	}
	return lines
}

// truncateRunes 按**字符**截断（不是字节）：摘要里会出现中文末行，按字节截会把
// 一个 UTF-8 字符劈成半个，投影里就是乱码。
func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// formatBytesCompact 是摘要里的字节数口径（与投影的 formatAsyncBytes 同形，
// 但这里不依赖 application 侧）。
func formatBytesCompact(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/1024/1024)
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
	g.closed = true
	g.mu.Unlock()
	// 删目录在**锁外**：os.RemoveAll 是文件 I/O，持 g.mu 删会把派发/收尾/探针一起
	// 按住（同 killTarget 的纪律）。
	g.removeDir()
}

// removeDir 删输出根目录，成功才忘掉它（失败时留着，等下一个可删点重试）。
//
// 调用方**不得**持 g.mu：本方法自己在锁内取路径、在锁外删、再锁内就地清除
// （删的是目录，中途有并发登记时 g.dir 已变，此时不清 = 让新目录继续可用）。
func (g *asyncRegistry) removeDir() {
	g.mu.Lock()
	dir := g.dir
	g.mu.Unlock()
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		return
	}
	g.mu.Lock()
	if g.dir == dir {
		g.dir = ""
	}
	g.mu.Unlock()
}

// asyncPayload 是派发回执、观察/取回结果与终止回执的共同载荷。字段刻意不含时间戳与耗时：
// 它会随所在轮次永久留在可缓存前缀里，任何"每次都不一样"的字节都在白烧 token。
type asyncPayload struct {
	Status    string `json:"status"`
	Handle    string `json:"handle"`
	Kind      string `json:"kind,omitempty"`
	State     string `json:"state"`
	ExitCode  int    `json:"exit_code"`
	LogPath   string `json:"log_path,omitempty"`
	Output    string `json:"output,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Repeated  bool   `json:"repeated,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Hint      string `json:"hint"`
}

// encodeAsync 把载荷渲染成**字节**（JSON）。契约层的出参一律 []byte：那份字节
// 就是要交给模型/前端的结构化输出，先一步转成 string 只会让每个调用点多做一次
// 编解码往返（框架的 ToolHandler 只吃 string，转换点因此只留在工具边界）。
func encodeAsync(payload asyncPayload) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("job: 渲染作业载荷失败: %w", err)
	}
	return encoded, nil
}

// renderAccepted 渲染派发回执（= 该 tool_call 的 tool 结果，配对在此完成）。
//
// **字段与字节保持不变**：后台 bash 从 `bash(background=true)` 换到 `bash_bg`
// 之后，模型侧的观感必须连续（打点 L-1 的判据：回执逐字段等价）。实现只有一个
// ——合同面（renderJobAccepted）——避免"两条路径各写一份回执"而慢慢漂移。
func renderAccepted(run asyncRun, repeated bool) ([]byte, error) {
	return renderJobAccepted(handleForRun(run, repeated))
}

// renderPolled 渲染一次取回：running 带增量，终态带 exit_code、有界摘要与末尾增量。
func renderPolled(run asyncRun, delta string, truncated bool) ([]byte, error) {
	status, hint := "progress", "仍在运行。需要结果就再调一次 job_manage(op=fetch, handle)。"
	switch run.state {
	case asyncStateRunning:
	case asyncStateKilled:
		status, hint = "finished", "作业已被终止（job_manage(op=kill) 或会话销毁）；以上为其输出增量。"+
			"确认收到后可调 job_manage(op=done, handle) 销项。"
	default:
		status, hint = "finished", "作业已结束；以上为其输出增量。确认收到后可调 "+
			"job_manage(op=done, handle) 销项，让它从工作打点表上消失。"
	}
	if truncated {
		hint += " 输出已按上限截断，完整内容读 log_path。"
	}
	return encodeAsync(asyncPayload{
		Status: status, Handle: run.handle, Kind: run.kind, State: run.state, ExitCode: run.exit,
		LogPath: run.logPath, Output: delta, Summary: run.summary, Truncated: truncated, Hint: hint,
	})
}

// renderObserved 渲染一次观察（只读旁路：**不推进游标**、不进上下文正文）。
// 有句柄 = 一条作业的一眼读数；无句柄 = 本会话在册作业的清单。
func renderObserved(lines []string) ([]byte, error) {
	return encodeAsync(asyncPayload{
		Status: "observed", Output: strings.Join(lines, "\n"), ExitCode: -1,
		Hint: "观察是只读的：不推进取回游标，看多少次都不会吃掉输出。取结果用 " +
			"job_manage(op=fetch, handle)，终止用 job_manage(op=kill, handle)。",
	})
}

// renderKilled 渲染终止回执。杀完不带输出正文——正文一律走取回，
// 否则同一次调用的两种返回形状会让模型分不清"杀掉了"和"拿到了结果"。
func renderKilled(run asyncRun) ([]byte, error) {
	return encodeAsync(asyncPayload{
		Status: "killed", Handle: run.handle, Kind: run.kind, State: asyncStateRunning,
		ExitCode: -1, LogPath: run.logPath,
		Hint: "终止请求已发出；**已产出的内容不会丢**。用 job_manage(op=fetch, handle) 取末尾增量与终态" +
			"（state=killed, exit_code=137）。",
	})
}

// renderAlreadyFinished 渲染"不用杀"：句柄已经终态不是错误，但也不能谎报杀了谁。
func renderAlreadyFinished(run asyncRun) ([]byte, error) {
	return encodeAsync(asyncPayload{
		Status: "already_finished", Handle: run.handle, Kind: run.kind, State: run.state,
		ExitCode: run.exit, LogPath: run.logPath, Summary: run.summary,
		Hint: "作业早已结束，未做任何终止。用 job_manage(op=fetch, handle) 取结果，或 op=done 销项。",
	})
}

// renderRetired 渲染一次销项（done）：行从工作打点表上消失，history 里那次取回的
// 结果成为唯一事实。
func renderRetired(run asyncRun) ([]byte, error) {
	return encodeAsync(asyncPayload{
		Status: "retired", Handle: run.handle, Kind: run.kind, State: run.state,
		ExitCode: run.exit, Summary: run.summary,
		Hint: "作业已销项（终态 + 已回填过一次），不再出现在工作打点表里。要再看输出读它已经" +
			"取回过的那一轮 tool 结果，或 log_path。",
	})
}
