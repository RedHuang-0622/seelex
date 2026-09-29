package core

// 复现（二）：**自锁**——同一条 goroutine 在持有 Core.ViewMu 写锁时，又在锁内取
// 同一把锁的读锁。
//
// 链路（每一跳都是生产接线，不是假想）：
//
//	prepareExecutionContextFor                       （coordinator.go:664 起持写锁）
//	  └─ c.ViewMu.Lock()                             ← 写锁落在执行这条折叠的 goroutine 上
//	       └─ c.pushCompactionFrame                  （coordinator.go:721，仍在临界区内）
//	            └─ CompactionIndexPort.PushCompactionFrame(context.Background(), …)
//	                 （context_runtime/compaction_index.go:68 —— ctx 是 Background）
//	                 └─ adapters.RuntimePort.PushCompactionFrame（只转发）
//	                      └─ seelebridge.Runtime.PushCompactionFrame
//	                           └─ CompressedTurnArchiver.StoreTurn(ctx, …)（归档原文）
//	                                ├─ sessionIDFromContext(ctx) == ""   ← Background 不带会话
//	                                └─ a.SessionIDProvider()             ← 兜底会话归属
//	                                     └─ main.go:312 `app.Snapshot().Session.ID`
//	                                          └─ view_state.Coordinator.SnapshotView
//	                                               └─ ViewMu.RLock()  ← 同 goroutine 再取读锁
//
// sync.RWMutex 不可重入：持写锁者再取读锁 = 永久阻塞（不是排队、没有超时、外部
// 取消也进不来）。于是从 replace 关（进度条 3/7）起整个交互面永久冻结：
//
//	/compact 的 RPC 永不返回 → GUI 的 `await invoke(...)` 不返回 → 输入框留着 `/compact`；
//	Snapshot 永不返回 → 会话切不动、会话列表与右栏不再刷新；
//	Submit 的第一步（ViewMu.RLock）永不返回 → 消息连队列都进不去。
//
// 触发条件是「折叠折出了非空区间」（overflow 非空，才会走到归档）且装配了轮次归档器
// —— 生产装配（main.go:310 注入 CompressedTurnArchiver）恒为真，所以这条路径在真实
// 进程里是**确定性**自锁，不是概率事件，与是否打开 context_compaction_summary 无关
// （打开时只是在自锁之前多跑一次模型调用）。

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	types "github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// archiverSessionsStub 是 CompressedTurnArchiver.Sessions 需要的写通道
// （生产上由 session.Manager 的 sessionCommitPort 面提供）。
type archiverSessionsStub struct {
	mu      sync.Mutex
	commits int
}

func (stub *archiverSessionsStub) SaveCommitWorkspace(_, _ string, _ sessionstore.Commit) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.commits++
	return nil
}

func (stub *archiverSessionsStub) commitCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.commits
}

// productionArchiveIndexRuntime 按生产接线模拟折叠帧的推帧落点：跑完 DAG（这里以桩
// 代替）后走**真实**的 CompressedTurnArchiver 归档原文，并把 ctx 原样转发——与
// seelebridge/runtime_compaction_index.go 的第二步同形。
type productionArchiveIndexRuntime struct {
	runtimeWithContextLimits
	archiver *CompressedTurnArchiver
}

func (runtime *productionArchiveIndexRuntime) PushCompactionFrame(
	ctx context.Context,
	_ string,
	request context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	overflow := make([]types.Message, 0, len(request.Overflow))
	for _, message := range request.Overflow {
		content := message.Content
		overflow = append(overflow, types.Message{Role: message.Role, Content: &content})
	}
	if _, err := runtime.archiver.StoreTurn(ctx, "compact-selflock-1", overflow); err != nil {
		return context_runtime.CompactionIndexReceipt{}, err
	}
	return context_runtime.CompactionIndexReceipt{
		SegmentID: "compact-selflock-1", Summary: "x", SummarySource: "local",
	}, nil
}

// TestExplicitCompactFramePushSelfDeadlocksRepro：在真实归档接线（main.go:310 同形）
// 下提交一次 `/compact`，判定交互面是否被永久冻死。
//
// 修法（2026-09-29）：折叠落点拆成三段——锁内提交状态（A）→ **锁外**推帧与渲染
// （B）→ 锁内落存储与写记录（C）。本用例因此从"复现证据"转为**存活断言**：提交
// 必须返回、快照仍可取、归档真的落盘。它就是这条路径的回归守卫——把推帧放回
// 临界区，本用例立刻变红（有牙证明见 docs/devlog/2026-09-29-compaction-fold-lock-granularity.md）。
func TestExplicitCompactFramePushSelfDeadlocksRepro(t *testing.T) {
	var serviceRef *Service
	commits := &archiverSessionsStub{}
	providerEntered := make(chan struct{})
	var providerOnce sync.Once
	archiver := &CompressedTurnArchiver{
		Sessions: commits,
		// 与 main.go:312 同形：归档器的兜底会话归属读的是视图快照。
		SessionIDProvider: func() string {
			providerOnce.Do(func() { close(providerEntered) })
			return serviceRef.Snapshot().Session.ID
		},
	}
	runtime := &productionArchiveIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{
			fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
		},
		archiver: archiver,
	}
	// 直接构造而不走 newTestService：后者的 t.Cleanup(service.Shutdown) 也要取
	// ViewMu，一旦复现出自锁就会连清理一起挂死（测试进程永不退出）。被冻住的
	// goroutine 只持有本用例私有 service 的锁，是本用例要留下的事实。
	service := mustNew(t, Dependencies{
		Engine:   &fakeEngine{},
		Runtime:  runtime,
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: &fakeSessions{},
	})
	serviceRef = service
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-selflock"}
	service.components.tasks.BeginTask("task-selflock", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-selflock")
	sessionID := service.Snapshot().Session.ID

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	// 用户按回车提交 `/compact`：与 GUI 同一条 RPC 链路。
	commandReturned := make(chan struct{})
	go func() {
		_ = service.Submit(context.Background(), "/compact")
		close(commandReturned)
	}()

	// 归档器要归属会话了：下一跳就是 provider → app.Snapshot() → ViewMu.RLock。
	select {
	case <-providerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("5s 内没有走到归档器的会话归属兜底（夹具失效）")
	}

	if !returnsWithin(3*time.Second, func() { <-commandReturned }) {
		// 自锁仍在（推帧又回到 ViewMu 临界区里）：给出可判定的证据面。
		frames := drainCompactionProgress(t, subscription)
		lastGate := ""
		lastIndex := 0
		if len(frames) > 0 {
			lastGate = frames[len(frames)-1].event.Gate
			lastIndex = frames[len(frames)-1].event.Index
		}
		evidence := []string{
			"进度条停在 " + lastGate + "(" + strconv.Itoa(lastIndex) + "/7)：index 关永远到不了（自锁点就在推帧内部）",
			"/compact 提交 3s 未返回（GUI 因此不清空输入框）",
			"归档 commit 数=" + strconv.Itoa(commits.commitCount()) + "（自锁发生在写盘之前）",
		}
		if returnsWithin(time.Second, func() { _ = service.Snapshot() }) {
			evidence = append(evidence, "快照仍可读（与自锁结论矛盾，请复核）")
		} else {
			evidence = append(evidence, "Snapshot 1s 未返回（会话切换/列表刷新被冻）")
		}
		if returnsWithin(time.Second, func() { _ = service.Submit(context.Background(), "hello") }) {
			evidence = append(evidence, "新消息仍可提交（与自锁结论矛盾，请复核）")
		} else {
			evidence = append(evidence, "新消息 1s 未进队列（Submit 第一步就取 ViewMu.RLock）")
		}
		t.Fatalf("装配层折叠自锁（ViewMu 写锁内再取读锁，/compact 永不返回）：%s", strings.Join(evidence, "；"))
	}

	// 走到这里说明自锁已不存在：交互面必须保持可用，且归档必须真的落盘。
	if !returnsWithin(3*time.Second, func() { _ = service.Snapshot() }) {
		t.Fatal("压缩期间快照被冻住（会话切不动）")
	}
	if !returnsWithin(3*time.Second, func() { _ = service.Submit(context.Background(), "hello") }) {
		t.Fatal("压缩期间新消息提交不返回")
	}
	if got := commits.commitCount(); got != 1 {
		t.Fatalf("归档 commit 数 = %d，want 1（原文归档必须落盘，否则 read_compressed_turn 无入口）", got)
	}
}
