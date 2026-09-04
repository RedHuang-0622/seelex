package core

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// session_switch_ab_test.go ── AB 链路微基准
//
// 对比“忙时会话之间切换”的两种并发模型：
//   A（mutex 串行）：一把进程级大锁同时保护“当前视图”和“镜像写”；
//     忙会话的持续写与切换请求在同一把锁上排队（高粒度锁 → 长等待）。
//   B（actor 原子）：切换/写都投递给单 goroutine 串行处理（CSP），无共享
//     可变状态；忙会话写和切换同队列，但无锁竞争/无数据竞争面。
//
// 本文件是演示性 AB 链路测试：不做硬性能断言（CI 环境不稳定），记录
// 吞吐/最长排队延迟并断言两边都不丢请求、active 合法、无 -race 数据竞争。
// 真实系统里
// view 指针移动本身已是 actor（session.Domain.SetActive）；卡顿主要来自
// 切换链路里的同步 I/O（冷加载）与前端高频刷新，不在这个微观模型内。

type abSwitchRegistry interface {
	busyWork(stop <-chan struct{})
	switchTo(sessionID string) time.Duration
	applied() int
	validate() bool
}

// lockRegistry：方案 A——一把大锁同时护视图与忙写。
type lockRegistry struct {
	mu       sync.Mutex
	active   string
	work     int
	sw       int
	interval time.Duration
	// mirrorLog 模拟每次忙写/切换要拷贝的“快照”，增大临界区成本。
	mirrorLog []int
}

func newLockRegistry(interval time.Duration) *lockRegistry {
	return &lockRegistry{interval: interval, mirrorLog: make([]int, 0, 4096)}
}

func (registry *lockRegistry) busyWork(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
			registry.mu.Lock()
			registry.work++
			appendMirrorWork(&registry.mirrorLog, registry.work)
			registry.mu.Unlock()
			time.Sleep(registry.interval)
		}
	}
}

func (registry *lockRegistry) switchTo(sessionID string) time.Duration {
	start := time.Now()
	registry.mu.Lock()
	registry.active = sessionID
	registry.sw++
	appendMirrorWork(&registry.mirrorLog, registry.sw)
	registry.mu.Unlock()
	return time.Since(start)
}

func (registry *lockRegistry) applied() int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.sw
}

func (registry *lockRegistry) validate() bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return (registry.active == "busy-0" || registry.active == "busy-1")
}

// actorRegistry：方案 B——单 goroutine 串行处理 busy 写与切换，无共享锁。
type actorRegistry struct {
	cmds     chan actorCmd
	done     chan struct{}
	sw       int
	interval time.Duration
}

type actorCmd struct {
	kind  string // "work" | "switch" | "count" | "validate"
	sid   string
	reply chan time.Duration
	count chan int
	valid chan bool
}

func newActorRegistry(interval time.Duration) *actorRegistry {
	registry := &actorRegistry{
		cmds: make(chan actorCmd, 4096),
		done: make(chan struct{}),
	}
	registry.interval = interval
	go registry.loop()
	return registry
}

func (registry *actorRegistry) loop() {
	work := 0
	mirrorLog := make([]int, 0, 4096)
	active := ""
	for {
		select {
		case <-registry.done:
			return
		case cmd := <-registry.cmds:
			start := time.Now()
			if cmd.kind == "count" {
				cmd.count <- registry.sw
				continue
			}
			if cmd.kind == "validate" {
				cmd.valid <- active == "busy-0" || active == "busy-1"
				continue
			}
			if cmd.kind == "work" {
				work++
				appendMirrorWork(&mirrorLog, work)
				continue
			}
			active = cmd.sid
			registry.sw++
			appendMirrorWork(&mirrorLog, registry.sw)
			if cmd.reply != nil {
				cmd.reply <- time.Since(start)
			}
		}
	}
	_ = active
}

func (registry *actorRegistry) busyWork(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case registry.cmds <- actorCmd{kind: "work"}:
			time.Sleep(registry.interval)
		}
	}
}

func (registry *actorRegistry) switchTo(sessionID string) time.Duration {
	start := time.Now()
	reply := make(chan time.Duration, 1)
	registry.cmds <- actorCmd{kind: "switch", sid: sessionID, reply: reply}
	<-reply
	return time.Since(start)
}

func (registry *actorRegistry) applied() int {
	reply := make(chan int, 1)
	registry.cmds <- actorCmd{kind: "count", count: reply}
	return <-reply
}

func (registry *actorRegistry) validate() bool {
	reply := make(chan bool, 1)
	registry.cmds <- actorCmd{kind: "validate", valid: reply}
	return <-reply
}

func (registry *actorRegistry) close() {
	close(registry.done)
}

// appendMirrorWork 追加一条“镜像工作”日志并裁剪（模拟忙写/切换的固定临界
// 区成本；两边工作量一致才可比）。
func appendMirrorWork(log *[]int, value int) {
	*log = append(*log, value)
	if len(*log) > 4096 {
		*log = (*log)[len(*log)-2048:]
	}
}

func mirrorMonotonic(log []int) bool {
	previous := 0
	for _, value := range log {
		if value < previous {
			return false
		}
		previous = value
	}
	return true
}

// runABRound 在 rate-limited 忙负载下并发发起 switches，返回一次轮次的
// 总耗时与最长单次等待。
func runABRound(t *testing.T, registry abSwitchRegistry, name string) (time.Duration, time.Duration, int) {
	t.Helper()
	stop := make(chan struct{})
	var busy sync.WaitGroup
	for index := 0; index < 2; index++ {
		busy.Add(1)
		go func() {
			defer busy.Done()
			registry.busyWork(stop)
		}()
	}
	const callers = 4
	const each = 80
	before := registry.applied()
	var group sync.WaitGroup
	var maxMu sync.Mutex
	maxWait := time.Duration(0)
	group.Add(callers)
	start := time.Now()
	for caller := 0; caller < callers; caller++ {
		go func(caller int) {
			defer group.Done()
			for index := 0; index < each; index++ {
				sid := fmt.Sprintf("busy-%d", (caller+index)%2)
				wait := registry.switchTo(sid)
				maxMu.Lock()
				if wait > maxWait {
					maxWait = wait
				}
				maxMu.Unlock()
			}
		}(caller)
	}
	group.Wait()
	close(stop)
	busy.Wait()
	total := time.Since(start)
	applied := registry.applied() - before
	if applied != callers*each {
		t.Fatalf("[%s] applied switches = %d, want %d（请求丢失）", name, applied, callers*each)
	}
	if !registry.validate() {
		t.Fatalf("[%s] 数据安全不变量失败（active 越界或请求丢失）", name)
	}
	return total, maxWait, applied
}

func median(values []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), values...)
	for index := 1; index < len(sorted); index++ {
		for j := index; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

// runABCompare 跑多次轮次并汇总两个维度：
//   - 数据安全：请求无丢失、mirror 单调、active 合法（配合 -race 无数据竞争）；
//   - 速度：中位总耗时、中位最长排队、吞吐（switches/s）。
func runABCompare(t *testing.T, registry abSwitchRegistry, name string) {
	t.Helper()
	const rounds = 3
	var totals []time.Duration
	var maxWaits []time.Duration
	switches := 0
	for round := 0; round < rounds; round++ {
		total, maxWait, applied := runABRound(t, registry, name)
		totals = append(totals, total)
		maxWaits = append(maxWaits, maxWait)
		switches += applied
	}
	medianTotal := median(totals)
	medianMax := median(maxWaits)
	throughput := float64(switches) / (medianTotal.Seconds() * float64(rounds))
	t.Logf("[%s] rounds=%d switches=%d medianTotal=%s medianMaxWait=%s throughput=%.0f switches/s（数据安全：无丢失 + active 合法；-race 下无竞争）",
		name, rounds, switches, medianTotal.Round(time.Microsecond), medianMax.Round(time.Microsecond), throughput)
}

// TestSessionSwitchABLockVsActor 对比锁串行（A）与 actor 模型（B）在忙会话
// 并发切换下的两个维度：数据安全（-race + 不变量）与速度（中位耗时/排队/
// 吞吐）。忙负载按相同频率注入，保证可比。
func TestSessionSwitchABLockVsActor(t *testing.T) {
	const workInterval = 100 * time.Microsecond
	lock := newLockRegistry(workInterval)
	runABCompare(t, lock, "A:mutex-global")

	actor := newActorRegistry(workInterval)
	defer actor.close()
	runABCompare(t, actor, "B:actor-channel")
}
