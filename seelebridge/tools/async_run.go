package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/internal/winhide"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// 后台命令的轮询型执行域·执行体：派发、收尾、终止与资源回收。
// 表与状态机在 async_exec.go，工具面在 async_tools.go。

const (
	// asyncTimeoutExit / asyncKilledExit 是两种"被外力结束"的合成退出码
	// （124 与 timeout(1) 同口径，137 与 128+SIGKILL 同口径）：让"被上限掐死"
	// "被杀"在终态里与"命令自己退非零"可分。
	asyncTimeoutExit = 124
	asyncKilledExit  = 137

	// asyncDisabledText 是切片关闭时的统一拒绝文本：能力不可实施时必须拒绝，
	// 不得静默降级（与 security/sandbox.go 头注同源）。
	asyncDisabledText = "后台命令切片未启用（limits.async_exec.enabled=false）"

	// asyncWaitDelay 只兜一件事：孙进程继承了写端管道时，cmd.Wait 会一直等管道
	// 关闭。它不是"多久算卡死"的判据——终止请求发出后进程死活由 Wait 的返回说话。
	asyncWaitDelay = 5 * time.Second
)

// 收尾注记写进输出文件（因此随增量进入上下文）：硬超时、被杀、执行体 panic 都必须在
// 正文里留下痕迹，否则模型只看到"命令突然结束"。三者都不含时间戳——这段字节会永久
// 留在可缓存前缀里。
var (
	asyncTimeoutNote = fmt.Sprintf("\n[seelex:async] 命令超过硬上限 %s，进程树已终止（exit=%d）\n", asyncHardCap, asyncTimeoutExit)
	asyncKilledNote  = fmt.Sprintf("\n[seelex:async] 命令被终止（async_kill 或会话销毁），进程树已退出（exit=%d）\n", asyncKilledExit)
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
	// 有新字节 → 通知投影（登记表内部去抖）。锁序固定为 writer.mu → registry.mu：
	// 登记表层从不反过来调 writer，所以这条边不会成环。
	if written > 0 && w.registry != nil {
		w.registry.noteOutput(w.handle)
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

// asyncEnabled 报告后台命令能力是否常驻（Deps.AsyncExecEnabled）。
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

// CloseSessionAsync 杀掉某个会话的全部在途后台执行（会话销毁即杀）。
//
// 为什么归工具域而不是执行体自己管：句柄表按会话键持有执行体，会话没了就再没有
// 任何一条取回路径能拿到它——不杀就是无人认领的孤儿进程，而打点表会一路跟到进程退出。
// 返回被登记的句柄数（不是"确认杀死数"：杀不掉的仍由各自 awaitAsync 收敛）。
func (r *Router) CloseSessionAsync(sessionID string) int {
	if r == nil || r.async == nil || sessionID == "" {
		return 0
	}
	return r.async.killSession(sessionID)
}

// AsyncPendingFor 报告某会话此刻还在跑的后台命令数（能力关闭时恒 0）。
//
// 它是"无进展预算"的判据输入：一条安静的长命令被反复取回时载荷逐字节相同（载荷必须
// 确定性，否则每轮白烧缓存），字节口径的进展计数不会推进——但"有在途执行被查询"本身
// 就是进展。会话键与工具路径根同源（见 Deps.SessionKey），否则这里会数错会话。
func (r *Router) AsyncPendingFor(sessionID string) int {
	if r == nil || r.async == nil || !r.asyncEnabled() || sessionID == "" {
		return 0
	}
	return r.async.countRunningFor(sessionID)
}

// currentBatch 返回派发该工具调用的 chat 请求 ID（工作表格的批次归属）。
// 未注入解析口时回空串 = 归到"未分批"，绝不去猜一个批次。
func (r *Router) currentBatch(sessionID string) string {
	if r.deps.AsyncBatchID == nil {
		return ""
	}
	return r.deps.AsyncBatchID(sessionID)
}

// dispatchAsync 派发一条后台命令并立即返回受理回执（配对在同一次 tool_call /
// tool_result 里完成，所以历史只追加、不回写）。
//
// description 在这里归一成一行的短标签：它是工作表格的行标题，长文本既挤坏表格
// 也会每轮重播进上下文尾部打点块。
func (r *Router) dispatchAsync(ctx context.Context, command, description, workdir string) (string, error) {
	sessionID := r.sessionKey(ctx)
	run, started, err := r.async.begin(sessionID, command, clampProbeLine(description), r.currentBatch(sessionID))
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
//
// 进程树两件事缺一不可：ConfigureProcessTree 让 bash 自成一组（否则杀不到孙进程），
// cmd.Cancel 把 ctx 到点的默认"只杀直接子进程"换成整组终止。
func (r *Router) startAsync(ctx context.Context, run asyncRun, command, workdir string) error {
	tree := security.NewProcessTree()
	shell, shellArgs := scopedBashCommand(command)
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)
	cmd := exec.CommandContext(runCtx, shell, shellArgs...)
	winhide.Apply(cmd)
	cmd.Dir = workdir
	security.ConfigureHiddenCommand(cmd)
	security.ConfigureProcessTree(cmd)
	// ctx 到点时 exec 默认只杀直接子进程（bash），它派出的孙进程照活——硬超时
	// 因此必须换成"终止整棵树"。
	cmd.Cancel = func() error { return tree.Terminate() }
	cmd.WaitDelay = asyncWaitDelay

	file, err := os.Create(run.logPath)
	if err != nil {
		cancel()
		tree.Close()
		return fmt.Errorf("bash: 无法创建后台输出文件: %w", err)
	}
	// stdout+stderr 交错写同一文件：顺序本身是信息（同一次报错的上下文）。
	writer := &cappedLogWriter{registry: r.async, handle: run.handle, file: file, remain: asyncLogCap}
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		cancel()
		_ = file.Close()
		tree.Close()
		return fmt.Errorf("bash: %w", err)
	}
	if cmd.Process != nil {
		// 挂不上树（Job 建不出、OpenProcess 被拒）不放弃执行：派发已经发生，
		// 退化成"只杀直接子进程"比报错有用。差别由 tree.Degraded() 说得出。
		_ = tree.Attach(cmd.Process.Pid)
	}
	if !r.async.attach(run.handle, tree) {
		// 记录已不在（极端：派发后立刻被驱逐/关停）。树再也没人关，自己收。
		tree.Close()
	}
	go r.awaitAsync(runCtx, cancel, cmd, writer, file, run.handle)
	return nil
}

// awaitAsync 等执行体收尾并合成终态。终态恰好合成一次——正常退出、硬超时、被杀、
// 这里 panic，四条路都必须落到 finish，否则句柄会永久停在 running，模型只能空转到死。
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
		case errors.Is(err, exec.ErrWaitDelay):
			// 按 os/exec 的定义，这只发生在"进程已带成功状态退出、只是输出管道没在
			// WaitDelay 内关闭"（典型形状：孙进程继承了写端）。命令自己成了，记 0。
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
	// 被杀的痕迹必须进正文：否则模型只看到"命令突然结束"，分不清是超时、崩了还是自己杀的。
	snap, ok := r.async.snapshot(handle)
	if ok && snap.killRequested {
		_, _ = writer.Write([]byte(asyncKilledNote))
	}
	// Wait 已收回拷贝 goroutine，此后不会再有写入（exec 对非 *os.File 的
	// Stdout/Stderr 会起拷贝协程，Wait 等它们结束）。
	_ = file.Close()
	if ok && snap.tree != nil {
		// 关树 = Windows 上 KILL_ON_JOB_CLOSE 生效：任何还留在 Job 里的漏网进程
		// 由系统带走。这是唯一确定的回收点，不排定时器、不按"多久算陈旧"猜。
		snap.tree.Close()
	}
	r.async.finish(handle, exitCode)
}
