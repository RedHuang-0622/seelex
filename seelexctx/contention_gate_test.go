package seelexctx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 锁竞争门禁（pprof mutex + block profile）。
//
// 为什么必须采样而不是读代码：竞争是**运行时事实**——"这段代码里没有 mutex"推不出
// "这条路径没有竞争"（它可能持着别人的锁、也可能让出 CPU 让别的 goroutine 阻塞）。
// 因此门禁把本次改动（《压缩四区模型》边界判定 / 帧摘要传递上限 / 分片重放链 /
// 四区显式化）放进并发负载，再用 runtime.SetMutexProfileFraction(1) +
// pprof.Lookup("mutex"/"block") 取事实，分两臂断言：
//
//	A 臂（本次改动的主体：纯函数）——**用户级锁竞争样本必须为零**：这些函数不含
//	  任何锁，出现 sync.* 竞争就说明它们被塞进了持锁区间。
//	B 臂（真实形状：多会话共用一把压缩栈锁）——profile 必须**非空**（否则断言是
//	  空集上的真命题），且每个用户级竞争样本的**阻塞点**（sync 帧的直接调用者）
//	  不得落在本次改动的符号上。
//
// 不设竞争总量的绝对上界：mutex profile 的取值是 CPU 周期且依赖机器，钉一个绝对
// 数字只会得到一条会随机器抖动的假门禁。总量与分布留档到 tmp/ 供人工复核。
const (
	contentionGateWorkers    = 12
	contentionGateIterations = 40
)

// contentionGateSymbols 是本次改动新增/重写的符号：作为**阻塞点**出现即失败。
var contentionGateSymbols = []string{
	"CarryPreviousChapter2",
	"LocalChapter2WithCarry",
	"ChunkReplayMessages",
	"SummarizeChunkPlan",
	"CarryEvidence",
	"ReplayEvidence",
	"AnchorSourceWithCarry",
	"RetainedContextTokensWithFloor",
	"RetainFloorTokens",
	"buildCompactFrame",
	"summarizeOverflow",
	"localCurrentWork",
}

// TestPrefixChainLockContentionGate：并发负载下取 mutex/block profile 并断言本次
// 改动不引入锁竞争（两臂见文件头注释）。
func TestPrefixChainLockContentionGate(t *testing.T) {
	previousFraction := runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
	t.Cleanup(func() {
		runtime.SetMutexProfileFraction(previousFraction)
		runtime.SetBlockProfileRate(0)
	})

	// ── A 臂：纯函数并发（本次改动的主体）──────────────────────────
	runConcurrent(t, contentionGateWorkers, func(worker int) error {
		return contentionGatePureWorker(worker)
	})
	pureMutex := dumpProfile(t, "mutex-pure")
	_ = dumpProfile(t, "block-pure")
	pureSamples := contentionGateSamples(pureMutex)
	if locked := contentionGateLockedSamples(pureSamples); len(locked) > 0 {
		t.Fatalf("A 臂（纯函数）出现 %d 个用户级锁竞争样本——这些函数本不该接触锁：\n%s",
			len(locked), contentionGateRender(locked))
	}
	t.Logf("A 臂：samples=%d（用户级锁竞争 0）——纯函数无锁竞争", len(pureSamples))

	// ── B 臂：多会话共用一把压缩栈锁（真实形状）────────────────────
	shared := NewMemoryCompactStack()
	runConcurrent(t, contentionGateWorkers, func(worker int) error {
		return contentionGateSharedWorker(worker, shared)
	})
	mutexText := dumpProfile(t, "mutex-shared")
	blockText := dumpProfile(t, "block-shared")
	samples := contentionGateSamples(mutexText)
	locked := contentionGateLockedSamples(samples)
	if len(locked) == 0 {
		t.Fatal("B 臂 mutex profile 没有用户级锁竞争样本：探针没生效，符号断言会变成空集上的真命题")
	}
	for _, sample := range locked {
		site := contentionGateBlockingSite(sample)
		for _, symbol := range contentionGateSymbols {
			if strings.Contains(site, symbol) {
				t.Fatalf("B 臂有竞争样本的阻塞点落在本次改动的符号 %s 上（新代码把工作塞进了持锁区间）：\n%s",
					symbol, contentionGateRender([]contentionSample{sample}))
			}
		}
	}
	t.Logf("B 臂：samples=%d（用户级锁竞争 %d）阻塞点=%v——改动符号零命中",
		len(samples), len(locked), contentionGateBlockingSites(locked))
	if strings.TrimSpace(blockText) == "" {
		t.Fatal("block profile 为空：阻塞事件探针没生效")
	}
}

// runConcurrent 并发跑 worker 次负载，回收全部错误（t.Fatalf 不能在子 goroutine 调用）。
func runConcurrent(t *testing.T, workers int, work func(worker int) error) {
	t.Helper()
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			if err := work(worker); err != nil {
				errors <- err
			}
		}(worker)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("并发负载失败：%v", err)
	}
}

// contentionGatePureWorker 只跑本次改动的纯函数路径（A 臂：这些函数不含任何锁，
// 因此用户级锁竞争样本必须为零）。
func contentionGatePureWorker(worker int) error {
	history := roundHistory(8)
	prev := carryTestFrame("### 目标 (Goal)\n" + strings.Repeat("previous body ", 60))
	var plan ReplayChunkPlan
	for iteration := 0; iteration < contentionGateIterations; iteration++ {
		plan = ChunkReplayMessages(history, 64)
		if plan.ChunkCount() == 0 {
			return fmt.Errorf("worker %d: chunk plan is empty", worker)
		}
		if _, err := SummarizeChunkPlan(context.Background(), gateSummarizer{}, ReplayRequest{}, plan); err != nil {
			return fmt.Errorf("worker %d: %w", worker, err)
		}
		if _, facts := CarryPreviousChapter2(&prev, 64); facts.LimitTokens == 0 {
			return fmt.Errorf("worker %d: carry facts missing", worker)
		}
		if _, carry := LocalChapter2WithCarry(LocalCompactOptions{
			UnitCount: 2, PrevTop: &prev, CarryLimitTokens: 64,
		}); carry.LimitTokens == 0 {
			return fmt.Errorf("worker %d: local fold carry facts missing", worker)
		}
		if got := RetainFloorTokens(60, 174_488); got != 104_692 {
			return fmt.Errorf("worker %d: retain floor = %d", worker, got)
		}
		_ = WindowConfig{Ratio: 0.05}.RetainedContextTokensWithFloor(120_000, 200_000, 104_692)
	}
	return nil
}

// contentionGateSharedWorker 跑真实形状：每 worker 一份压缩产物 + 一个 DAG，共用一把压缩栈锁。
func contentionGateSharedWorker(worker int, shared CompactStackStore) error {
	sessionID := fmt.Sprintf("sess-gate-%d", worker)
	dag := NewCompactionDAG(CompactionDAGOptions{
		SessionIDProvider: func() string { return sessionID },
		Summarizer:        gateSummarizer{},
		ReplayInputTokens: 64,
		FrameCarryTokens:  128,
	})
	for iteration := 0; iteration < contentionGateIterations; iteration++ {
		history := roundHistory(8)
		// 写共享压缩栈：2026-09-30 起回合内控制器不再折帧，写栈的只剩装配层压缩
		// 路径——这里按它同一条契约（PushCompact）造真竞争。否则 B 臂的探针会
		// 退化成"空集上的真命题"（本文件头注释警告的那件事）。
		if err := shared.PushCompact(sessionstore.CompactFrame{
			SegmentID: fmt.Sprintf("compact-%s-%d", sessionID, iteration),
			From:      iteration,
			To:        iteration + 1,
		}); err != nil {
			return fmt.Errorf("shared.PushCompact: %w", err)
		}
		if _, err := dag.Execute(context.Background(), CompactionInput{
			Messages: history, History: history, UnitCount: len(history),
		}); err != nil {
			return fmt.Errorf("dag.Execute: %w", err)
		}
		// 共享栈读写：压缩路径写帧、真空区覆盖/报表读。
		_ = shared.Snapshot()
	}
	return nil
}

// gateSummarizer 是门禁负载用的确定性摘要器（不消耗网络/模型）。
type gateSummarizer struct{}

func (gateSummarizer) Summarize(_ context.Context, req ReplayRequest) (ReplayResult, error) {
	if len(req.History) == 0 {
		return ReplayResult{}, fmt.Errorf("empty chunk")
	}
	return ReplayResult{Chapter2: "### 目标 (Goal)\nchunk"}, nil
}

// contentionSample 是 mutex/block profile 里的一个样本（count + delay + 调用栈）。
type contentionSample struct {
	count  int64
	delay  int64
	frames []string
}

// contentionGateSamples 解析 pprof 文本格式：
//
//	--- mutex:
//	cycles/second=…
//	<count> <delay> @ <pc…>
//	#\t<pc>\t<symbol>\t\t<file:line>
//	# …
func contentionGateSamples(text string) []contentionSample {
	var samples []contentionSample
	var current *contentionSample
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#\t") {
			if current == nil {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 3 {
				continue
			}
			current.frames = append(current.frames, symbolOf(fields[2]))
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "@" {
			continue
		}
		count, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		delay, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		samples = append(samples, contentionSample{count: count, delay: delay})
		current = &samples[len(samples)-1]
	}
	return samples
}

// symbolOf 去掉 pprof 帧里的 +0x… 位移。
func symbolOf(frame string) string {
	if index := strings.Index(frame, "+0x"); index > 0 {
		return frame[:index]
	}
	return frame
}

// contentionGateLockedSamples 只保留**归属本次改动观测面**的用户级锁竞争样本：
//
//   - 首帧（frames[0]）必须是 sync.*（用户级锁；runtime 的调度器/分配器锁任何分配
//     内存的代码都会命中，不是本改动的观测面）；
//   - 阻塞点（frames[1]）不能是标准库内部实现：`sync.(*Pool)` 是 fmt 的 printer
//     池（fmt.Fprintf/Sprintf 在并发下会短暂 pin pool），`fmt.`/`runtime.`/`os.`/
//     `log.` 同理。它们既非本改动引入，也与本改动的锁设计无关；而**新代码自己加锁**
//     时 frames[1] 会是新符号，仍会被本函数保留并在 TestPrefixChainLockContentionGate
//     的 A/B 两臂断言里被抓到。
func contentionGateLockedSamples(samples []contentionSample) []contentionSample {
	locked := make([]contentionSample, 0, len(samples))
	for _, sample := range samples {
		if len(sample.frames) < 2 || !strings.HasPrefix(sample.frames[0], "sync.") {
			continue
		}
		if contentionGateStdlibInternal(sample.frames[1]) {
			continue
		}
		locked = append(locked, sample)
	}
	return locked
}

// contentionGateStdlibInternal 判定阻塞点是否为标准库内部实现（见上）。
func contentionGateStdlibInternal(site string) bool {
	for _, prefix := range []string{"sync.", "runtime.", "fmt.", "os.", "log.", "internal/"} {
		if strings.HasPrefix(site, prefix) {
			return true
		}
	}
	return false
}

// contentionGateBlockingSite 返回竞争样本的**阻塞点**：等锁帧（frames[0]）的直接
// 调用者，即"是谁持锁/在哪等锁"。回归会在这里露出来（新代码自己加锁 → 该符号出现）。
func contentionGateBlockingSite(sample contentionSample) string {
	if len(sample.frames) < 2 {
		return ""
	}
	return sample.frames[1]
}

func contentionGateBlockingSites(samples []contentionSample) []string {
	sites := make([]string, 0, len(samples))
	for _, sample := range samples {
		sites = append(sites, contentionGateBlockingSite(sample))
	}
	return sites
}

func contentionGateRender(samples []contentionSample) string {
	var builder strings.Builder
	for _, sample := range samples {
		fmt.Fprintf(&builder, "count=%d delay=%d frames=%s\n", sample.count, sample.delay, strings.Join(sample.frames, " <- "))
	}
	return builder.String()
}

// dumpProfile 把 profile 渲染为文本（debug=1）并留档到 tmp/lock-contention-gate/
// 供事后审计（profile 本体是二进制/blobby，落一份文本才能复核"到底哪儿在等锁"）。
func dumpProfile(t *testing.T, name string) string {
	t.Helper()
	profile := pprof.Lookup(strings.SplitN(name, "-", 2)[0])
	if profile == nil {
		t.Fatalf("运行时未提供 %s profile", name)
	}
	var buffer bytes.Buffer
	if err := profile.WriteTo(&buffer, 1); err != nil {
		t.Fatalf("写 %s profile: %v", name, err)
	}
	dir := filepath.Join("..", "tmp", "lock-contention-gate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建留档目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("留档 %s profile: %v", name, err)
	}
	return buffer.String()
}
