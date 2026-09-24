package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/internal/winhide"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// 后台命令的轮询型执行域（规格见
// docs/2026-09-24-async-tool-deferred-ack/README.md §8）。
//
// 形状：派发一次 → 立即拿到 handle + 现状；想知道进展就再调 async_output(handle, wait_ms)。
// 每一问一答都是一对正常的 tool_call/tool_result，所以：历史只追加、不回写；
// 不需要"结果到达时把空闲会话叫醒"那条链路（它必然回闯被 ChatStream 全程持有的会话锁）。
// 推送/补记路线已废：见同文件 §5 D1 与 §8 的选型说明。

const (
	asyncStateRunning = "running"
	asyncStateDone    = "done"
	asyncStateFailed  = "failed"

	// asyncMaxRunning 是在途上限；asyncMaxRecords 是记录总槽（只为兜内存，
	// 超了驱逐最老的已完成项）。上限只数在跑的，否则长会话累计到 32 条就再也发不出。
	asyncMaxRunning = 32
	asyncMaxRecords = 256

	// asyncHardCap 是后台命令的最长存活时间。同步路径的 scopedToolTimeout 不适用：
	// 受理回执一返回，工具调用的 ctx 就失效了，执行体必须自带独立上限。
	asyncHardCap = 30 * time.Minute

	// asyncDefaultWaitMS / asyncMaxWaitMS 是 async_output 的等待预算。
	asyncDefaultWaitMS = 5000
	asyncMaxWaitMS     = 60000

	// asyncPollTailBudget 是单次取回最多带回的增量字符数；asyncLogCap 是输出文件字节上限，
	// 超了截断并在正文里说明——不封顶的话一条后台命令能撑爆磁盘也能烧光缓存预算。
	asyncPollTailBudget = 4000
	asyncLogCap         = 1 << 20
)

// asyncRun 是一次后台执行。startedAt/exit 只在判定与回读时用，不进正文（正文必须确定性）。
type asyncRun struct {
	handle    string
	sessionID string
	key       string
	state     string
	exit      int
	logPath   string
	cursor    int64 // 已交付给模型的文件偏移，决定"增量"从哪算
	capHit    bool  // 输出文件是否触到字节上限
	done      chan struct{}
	startedAt time.Time
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
}

func newAsyncRegistry() *asyncRegistry {
	return &asyncRegistry{runs: map[string]*asyncRun{}, byKey: map[string]string{}}
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
// 按值返回（与 snapshot 同口径）：表内那条 asyncRun 的 state/exit 由执行体收尾时
// 在锁内改写，派发侧若拿到指针，就是在锁外读这两个字段——回执渲染只需要 handle 与
// logPath，给副本即可，且"回执渲染的是派发那一刻的状态"本身就是对的语义。
func (g *asyncRegistry) begin(sessionID, command string) (asyncRun, bool, error) {
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
		handle:    handle,
		sessionID: sessionID,
		key:       key,
		state:     asyncStateRunning,
		logPath:   filepath.Join(dir, handle+".log"),
		done:      make(chan struct{}),
		startedAt: time.Now(),
	}
	g.runs[handle] = run
	g.byKey[key] = handle
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

func (g *asyncRegistry) finish(handle string, exitCode int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	run, ok := g.runs[handle]
	if !ok {
		return
	}
	run.exit = exitCode
	run.state = asyncStateDone
	if exitCode != 0 {
		run.state = asyncStateFailed
	}
	close(run.done)
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

// asyncPayload 是派发回执与取回结果的共同载荷。字段刻意不含时间戳与耗时：
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
	hint := "命令已在后台跑。想知道进展或结果就调用 async_output(handle)，wait_ms 是这次最多等多久；不要重复派发同一条命令。"
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
	if run.state != asyncStateRunning {
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

// ── 执行体：派发、收尾、取回 ──────────────────────────────────────────────

const (
	// asyncTimeoutExit 是硬超时的合成退出码（与 timeout(1) 的 124 同口径）：
	// 让"被上限掐死"在终态里与"命令自己退非零"可分。
	asyncTimeoutExit = 124

	// asyncDisabledText 是切片关闭时的统一拒绝文本：能力不可实施时必须拒绝，
	// 不得静默降级（与 security/sandbox.go 头注同源）。
	asyncDisabledText = "后台命令切片未启用（limits.async_exec.enabled=false）"
)

// 收尾注记写进输出文件（因此随增量进入上下文）：硬超时与执行体 panic 必须在
// 正文里留下痕迹，否则模型只看到"命令突然结束"。两者都不含时间戳——这段
// 字节会永久留在可缓存前缀里。
var (
	asyncTimeoutNote = fmt.Sprintf("\n[seelex:async] 命令超过硬上限 %s，进程已终止（exit=%d）\n", asyncHardCap, asyncTimeoutExit)
	asyncPanicNote   = "\n[seelex:async] 执行体内部错误，已合成失败终态\n"
)

// cappedLogWriter 把子进程输出写进日志文件，并在字节上限处截断：超上限只丢弃
// 字节，仍向子进程报告"已写"——上限是磁盘与缓存预算的保护，不是命令的失败原因
// （报短写会让命令自己异常退出，那是把基础设施问题伪装成命令问题）。
type cappedLogWriter struct {
	registry *asyncRegistry
	handle   string
	file     *os.File
	mu       sync.Mutex
	remain   int64
	hit      bool
}

func (w *cappedLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.remain <= 0 {
		w.markHitLocked()
		return len(p), nil
	}
	chunk := p
	if int64(len(chunk)) > w.remain {
		chunk = chunk[:w.remain]
		w.markHitLocked()
	}
	written, err := w.file.Write(chunk)
	w.remain -= int64(written)
	if err != nil {
		return written, err
	}
	// 报告已消费全部入参（超出上限的部分按上限丢弃），否则子进程会看到短写。
	return len(p), nil
}

// markHitLocked 把"触顶"记进登记表：取回侧据此在正文里声明输出已被截断。
func (w *cappedLogWriter) markHitLocked() {
	if w.hit {
		return
	}
	w.hit = true
	if w.registry != nil {
		w.registry.markCapHit(w.handle)
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

// asyncEnabled 报告后台命令切片是否打开（Deps.AsyncExecEnabled）。
func (r *Router) asyncEnabled() bool {
	return r != nil && r.async != nil && r.deps.AsyncExecEnabled
}

// CloseAsync 释放后台执行域的输出目录（装配层在进程收尾时登记）。
//
// 为什么需要：目录是进程级的，进程死了就再没有人会去删它——实测本机临时目录里
// 累积了 44 个 seelex-async-*。
//
// 先当场试删；删不掉说明还有执行体握着日志文件句柄（Windows 下会失败），那就记下
// 关停意图，由最后一条 `finish` 补删——那一刻文件已关闭且无人再读句柄，是唯一确定
// 的可删点。不排定时器，也不靠"过多久算陈旧"猜状态。
func (r *Router) CloseAsync() {
	if r == nil || r.async == nil {
		return
	}
	r.async.close()
}

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

// dispatchAsync 派发一条后台命令并立即返回受理回执（配对在同一次 tool_call /
// tool_result 里完成，所以历史只追加、不回写）。
func (r *Router) dispatchAsync(ctx context.Context, command, workdir string) (string, error) {
	run, started, err := r.async.begin(r.sessionKey(ctx), command)
	if err != nil {
		return "", err
	}
	if started {
		if err := r.startAsync(ctx, run, command, workdir); err != nil {
			// 起不来就当场失败：受理回执不得谎报 running（模型会一直轮询一个
			// 永远不会结束的句柄）。合成终态后重发同一命令不会被去重挡住
			// （去重键只对 state=running 生效）。
			r.async.finish(run.handle, 1)
			return "", err
		}
	}
	return renderAccepted(run, !started)
}

// startAsync 起执行体；返回错误 = 没能启动，调用方据此当场失败。
//
// 授权与 workdir 解析都在派发前完成，这里是"已经批准之后的执行"。取消信号用
// context.WithoutCancel 摘掉：受理回执一返回，本次工具调用的 ctx 就失效，沿用
// 它会立刻把刚起的命令连带杀掉（同步路径的 scopedToolTimeout 同理不适用）；
// 存活上限由执行体自带的 asyncHardCap 兜住。
func (r *Router) startAsync(ctx context.Context, run asyncRun, command, workdir string) error {
	shell, shellArgs := scopedBashCommand(command)
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)
	cmd := exec.CommandContext(runCtx, shell, shellArgs...)
	winhide.Apply(cmd)
	cmd.Dir = workdir
	security.ConfigureHiddenCommand(cmd)

	file, err := os.Create(run.logPath)
	if err != nil {
		cancel()
		return fmt.Errorf("bash: 无法创建后台输出文件: %w", err)
	}
	// stdout+stderr 交错写同一文件：顺序本身是信息（同一次报错的上下文）。
	writer := &cappedLogWriter{registry: r.async, handle: run.handle, file: file, remain: asyncLogCap}
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		cancel()
		_ = file.Close()
		return fmt.Errorf("bash: %w", err)
	}
	go r.awaitAsync(runCtx, cancel, cmd, writer, file, run.handle)
	return nil
}

// awaitAsync 等执行体收尾并合成终态。终态恰好合成一次——正常退出、硬超时、
// 这里 panic，三条路都必须落到 finish，否则会话会永久停在 running，模型只能
// 空转到死。
func (r *Router) awaitAsync(runCtx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, writer *cappedLogWriter, file *os.File, handle string) {
	defer cancel()
	exitCode := 1
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				_, _ = writer.Write([]byte(asyncPanicNote))
			}
		}()
		err := cmd.Wait()
		switch {
		case err == nil:
			exitCode = 0
		default:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
		if runCtx.Err() == context.DeadlineExceeded {
			exitCode = asyncTimeoutExit
			_, _ = writer.Write([]byte(asyncTimeoutNote))
		}
	}()
	// Wait 已收回拷贝 goroutine，此后不会再有写入（exec 对非 *os.File 的
	// Stdout/Stderr 会起拷贝协程，Wait 等它们结束）。
	_ = file.Close()
	r.async.finish(handle, exitCode)
}

// ── async_output：取回增量 ───────────────────────────────────────────────

type asyncOutputInput struct {
	Handle string `json:"handle"`
	WaitMS int    `json:"wait_ms,omitempty"`
}

// scopedAsyncOutput 取回一次后台执行的进展/终态。只交付**新增**输出：反复轮询
// 同一句柄不会把整份输出重播进上下文（那是轮询型唯一真实的 token 风险）。
//
// 授权：句柄只对本会话有效（跨会话取回直接拒绝）；取回本身是只读，不重复弹
// 审批——它取的是已经获批的那次派发的输出。
func (r *Router) scopedAsyncOutput(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("async_output: %s", asyncDisabledText)
	}
	var input asyncOutputInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("async_output: invalid args: %w", err)
	}
	if input.Handle == "" {
		return "", fmt.Errorf("async_output: handle is required")
	}
	run, ok := r.async.snapshot(input.Handle)
	if !ok {
		return "", fmt.Errorf("async_output: 未知句柄 %q（可能已被驱逐，或进程重启后登记表已清空）", input.Handle)
	}
	if run.sessionID != r.sessionKey(ctx) {
		return "", fmt.Errorf("async_output: 句柄 %q 不属于本会话", input.Handle)
	}
	if run.state == asyncStateRunning {
		awaitAsyncDeadline(ctx, run.done, input.WaitMS)
	}
	next, delta, truncated, ok := r.async.advanceTail(input.Handle, asyncPollTailBudget)
	if !ok {
		return "", fmt.Errorf("async_output: 句柄 %q 已不在登记表里", input.Handle)
	}
	r.async.markCursor(input.Handle, next, truncated)
	fresh, _ := r.async.snapshot(input.Handle)
	return renderPolled(fresh, delta, truncated)
}

// awaitAsyncDeadline 等到执行体收尾、等待预算用尽或调用被取消——三者都返回，
// 让取回带回"此刻"的增量（等待不是目的，交付增量才是）。
func awaitAsyncDeadline(ctx context.Context, done <-chan struct{}, waitMS int) {
	budget := clampAsyncWaitMS(waitMS)
	if budget <= 0 {
		return
	}
	timer := time.NewTimer(time.Duration(budget) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}
}

// clampAsyncWaitMS 归一 wait_ms：负数 = 不等待（立刻交付当前增量）；0/省略 =
// 默认 5s；超上限按上限——模型给一个大数不该让这一问卡住整轮。
func clampAsyncWaitMS(waitMS int) int {
	switch {
	case waitMS < 0:
		return 0
	case waitMS == 0:
		return asyncDefaultWaitMS
	case waitMS > asyncMaxWaitMS:
		return asyncMaxWaitMS
	default:
		return waitMS
	}
}

// ── 工具面 ──────────────────────────────────────────────────────────────

// asyncOutputSchema 是 async_output 的入参 schema。
func asyncOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"handle":  map[string]interface{}{"type": "string"},
			"wait_ms": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"handle"},
	}
}

// asyncOutputDescription 说明取回语义：只给增量、wait_ms 三档、句柄是本会话的。
func asyncOutputDescription() string {
	return "Read the incremental output of a command dispatched with bash background=true. " +
		"Each call returns only the bytes produced since the previous call for that handle " +
		"(never a replay of the whole log). wait_ms: negative returns immediately, " +
		"0 or omitted waits up to 5s, values above 60000 are capped at 60000."
}

// asyncBackgroundHint 是 bash 描述在切片打开时追加的一句：派发回执不含命令输出，
// 取回必须另调 async_output——不写清，模型会以为 background 只是"超时更长"。
const asyncBackgroundHint = " With background=true the command runs in the background: that call's result is only " +
	"an acceptance receipt (handle + log_path), never the command output — poll async_output(handle) for progress."
