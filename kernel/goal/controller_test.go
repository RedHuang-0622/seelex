package goal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var testCtx = context.Background()

func newTestController(t *testing.T, depth int) *Controller {
	t.Helper()
	return NewController(Options{Depth: depth})
}

func requireActive(t *testing.T, controller *Controller, title string) *GoalRecord {
	t.Helper()
	status := controller.Status()
	if status.Active == nil || status.Active.Title != title {
		t.Fatalf("期望栈顶 %q, 得 %+v", title, status.Active)
	}
	return status.Active
}

// TestBeginFinishSingle 验证会话单例主路径：begin → update → finish 弹栈删除、
// History 审计、投影回退、事件顺序（design §3.3/§3.4）。
func TestBeginFinishSingle(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	subscription := controller.Subscribe(64)
	defer subscription.Close()

	record, err := controller.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Statement: "完成 mdtable v1 并带测试",
		Acceptance: []string{"go test ./... 全绿", "README 更新"},
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if record.ID == "" || record.Status != StatusActive {
		t.Fatalf("begin 记录异常: %+v", record)
	}
	projection := controller.Projection()
	if projection.Active == nil || projection.Active.Title != "发布 v1" || len(projection.Goals) != 1 {
		t.Fatalf("投影应含单个 active goal: %+v", projection)
	}

	updated, err := controller.Update(testCtx, UpdateRequest{
		Acceptance:      []string{"go test ./... 全绿"},
		ProgressKind:    ProgressMilestone,
		ProgressContent: "框架装配完成",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Acceptance) != 1 || len(updated.Progress) != 1 {
		t.Fatalf("update 未生效: %+v", updated)
	}

	finished, err := controller.Finish(testCtx, FinishRequest{Result: "v1.0.0 已发布"})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != StatusCompleted || finished.FinishedAt == 0 {
		t.Fatalf("finish 记录异常: %+v", finished)
	}
	projection = controller.Projection()
	if projection.Active != nil || len(projection.Goals) != 0 {
		t.Fatalf("finish 后投影应为空（弹栈删除）: %+v", projection)
	}
	if status := controller.Status(); status.Active != nil || len(status.Stack) != 0 || status.History != 1 {
		t.Fatalf("finish 后 status 异常: %+v", status)
	}
	history := controller.History()
	if len(history) != 1 || history[0].ID != finished.ID || history[0].Status != StatusCompleted {
		t.Fatalf("History 审计缺失: %+v", history)
	}

	// 事件顺序与投影快照语义。
	var kinds []EventKind
	for len(kinds) < 3 {
		select {
		case event := <-subscription.Events:
			kinds = append(kinds, event.Kind)
			if event.Kind != EventRestore && event.Projection.Active == nil && event.Kind != EventFinish {
				t.Fatalf("事件应携带投影: %+v", event)
			}
		default:
			t.Fatalf("事件不足: %v", kinds)
		}
	}
	want := []EventKind{EventBegin, EventUpdate, EventFinish}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("事件顺序 %v, 期望 %v", kinds, want)
		}
	}
}

// TestStackDepthRejectsNested 验证 D2 会话单例：depth=1 时嵌套 begin 被拒。
func TestStackDepthRejectsNested(t *testing.T) {
	controller := newTestController(t, 1)
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "A"}); err != nil {
		t.Fatalf("begin A: %v", err)
	}
	_, err := controller.Begin(testCtx, BeginRequest{Title: "B"})
	if !errors.Is(err, ErrStackFull) {
		t.Fatalf("嵌套 begin 应报 ErrStackFull, 得 %v", err)
	}
	if active := requireActive(t, controller, "A"); active == nil {
		t.Fatal("栈顶应仍为 A")
	}
}

// TestNestedDepthRestores 验证 D2 放开嵌套（depth>1）：压栈下层 paused、
// 弹栈后下层恢复 active（design §3.3 自动恢复路径）。
func TestNestedDepthRestores(t *testing.T) {
	controller := newTestController(t, 2)
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "发布 v1"}); err != nil {
		t.Fatalf("begin A: %v", err)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "先修紧急 bug"}); err != nil {
		t.Fatalf("begin B: %v", err)
	}
	status := controller.Status()
	if status.Active.Title != "先修紧急 bug" || len(status.Stack) != 2 {
		t.Fatalf("嵌套栈异常: %+v", status)
	}
	if status.Stack[0].Status != StatusPaused {
		t.Fatalf("下层应为 paused: %+v", status.Stack[0])
	}
	if _, err := controller.Finish(testCtx, FinishRequest{}); err != nil {
		t.Fatalf("finish B: %v", err)
	}
	restored := requireActive(t, controller, "发布 v1")
	if restored.Status != StatusActive {
		t.Fatalf("弹栈后下层应恢复 active: %+v", restored)
	}
}

// TestNestedGoalsPopLIFOUntilEmpty 验证嵌套逐层弹栈直至栈空（治理收口
// 前提）：父→子 begin；子 finish → 父恢复 active；父 finish → 栈空、
// History=2、投影复位（design §2.1a 契约 1/2/6）。
func TestNestedGoalsPopLIFOUntilEmpty(t *testing.T) {
	controller := newTestController(t, 2)
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "父目标"}); err != nil {
		t.Fatalf("begin parent: %v", err)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "子目标"}); err != nil {
		t.Fatalf("begin child: %v", err)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{Result: "子目标完成"}); err != nil {
		t.Fatalf("finish child: %v", err)
	}
	if active := requireActive(t, controller, "父目标"); active.Status != StatusActive {
		t.Fatalf("子完成弹栈后父应恢复 active: %+v", active)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{Result: "父目标完成"}); err != nil {
		t.Fatalf("finish parent: %v", err)
	}
	status := controller.Status()
	if len(status.Stack) != 0 || status.Active != nil {
		t.Fatalf("栈空语义: %+v", status)
	}
	if history := controller.History(); len(history) != 2 {
		t.Fatalf("History 审计 = %d, want 2", len(history))
	}
	if projection := controller.Projection(); projection.Active != nil || len(projection.Goals) != 0 {
		t.Fatalf("栈空后投影应复位: %+v", projection)
	}
}

// TestGoalStackDepthBound 验证 Depth 放开后仍受 MaxStackDepth 上限约束：
// 超限构造被夹紧，压满后继续 begin 拒绝（ErrStackFull）。
func TestGoalStackDepthBound(t *testing.T) {
	controller := NewController(Options{Depth: MaxStackDepth + 10})
	for index := 0; index < MaxStackDepth; index++ {
		if _, err := controller.Begin(testCtx, BeginRequest{Title: fmt.Sprintf("目标-%d", index+1)}); err != nil {
			t.Fatalf("begin #%d: %v", index+1, err)
		}
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "超限目标"}); !errors.Is(err, ErrStackFull) {
		t.Fatalf("超限 begin 应报 ErrStackFull, 得 %v", err)
	}
	if status := controller.Status(); len(status.Stack) != MaxStackDepth {
		t.Fatalf("栈深 = %d, want %d", len(status.Stack), MaxStackDepth)
	}
}

// TestUpdateOnlyActive 验证更新边界：空栈/非 active 不可更新。
func TestUpdateOnlyActive(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	if _, err := controller.Update(testCtx, UpdateRequest{}); !errors.Is(err, ErrStackEmpty) {
		t.Fatalf("空栈 update 应报 ErrStackEmpty, 得 %v", err)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "A"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := controller.Update(testCtx, UpdateRequest{ProgressContent: "x"}); !errors.Is(err, ErrStackEmpty) {
		t.Fatalf("finish 后 update 应报 ErrStackEmpty, 得 %v", err)
	}
}

// TestAbortPopsAndHistory 验证 abort 路径。
func TestAbortPopsAndHistory(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "冒险目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	aborted, err := controller.Abort(testCtx, FinishRequest{Reason: "需求取消"})
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if aborted.Status != StatusAborted {
		t.Fatalf("abort 状态异常: %+v", aborted)
	}
	if projection := controller.Projection(); projection.Active != nil {
		t.Fatalf("abort 后投影应为空: %+v", projection)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{}); !errors.Is(err, ErrStackEmpty) {
		t.Fatalf("空栈 finish 应报 ErrStackEmpty, 得 %v", err)
	}
}

// TestIdempotentBegin 验证同标题 active 幂等返回现有 goal。
func TestIdempotentBegin(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	first, err := controller.Begin(testCtx, BeginRequest{Title: "发布 v1", Statement: "a"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	second, err := controller.Begin(testCtx, BeginRequest{Title: "发布 v1", Statement: "a"})
	if err != nil {
		t.Fatalf("重复 begin 应幂等, 得 %v", err)
	}
	if first.ID != second.ID || len(controller.Projection().Goals) != 1 {
		t.Fatalf("幂等语义被破坏: %s vs %s", first.ID, second.ID)
	}
	// 不同标题仍受深度约束。
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "另一个目标"}); !errors.Is(err, ErrStackFull) {
		t.Fatalf("不同标题 begin 应报 ErrStackFull, 得 %v", err)
	}
}

// TestJSONStoreRoundtrip 验证存储 roundtrip：变更落盘、新控制器 Reload 恢复
// active goal（崩溃恢复路径，design §3.7）。
func TestJSONStoreRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal-stack.json")
	store := NewJSONFileStore(path)

	first := NewController(Options{Depth: 1, Store: store})
	if _, err := first.Begin(testCtx, BeginRequest{Title: "发布 v1", Statement: "带测试"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := first.Update(testCtx, UpdateRequest{ProgressContent: "已过编译"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 模拟重启：同一 store 新控制器。
	second := NewController(Options{Depth: 1, Store: store})
	if err := second.Reload(testCtx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	active := requireActive(t, second, "发布 v1")
	if len(active.Progress) != 1 || active.Progress[0].Content != "已过编译" {
		t.Fatalf("reload 后进度丢失: %+v", active)
	}

	// finish 弹栈后，栈文件应为空；再次重启无 active。
	if _, err := second.Finish(testCtx, FinishRequest{Result: "收口"}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	third := NewController(Options{Depth: 1, Store: store})
	if err := third.Reload(testCtx); err != nil {
		t.Fatalf("reload3: %v", err)
	}
	if status := third.Status(); status.Active != nil || len(status.Stack) != 0 {
		t.Fatalf("finish 弹栈应持久化删除: %+v", status)
	}
}

// TestFrameRendersGoal 验证 Goal 帧渲染（TechLeader/装配嵌入素材）。
func TestFrameRendersGoal(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	if frame := controller.Frame(); frame != "" {
		t.Fatalf("空栈 Frame 应为空串: %q", frame)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Statement: "why: 收敛版本",
		Acceptance: []string{"go test 全绿"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := controller.Update(testCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "装配完成",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	frame := controller.Frame()
	for _, want := range []string{"发布 v1", "acceptance:", "- go test 全绿", "[milestone] 装配完成"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("Frame 缺少 %q:\n%s", want, frame)
		}
	}
}

// TestSubscribeDropAndClose 验证有界订阅：溢出仅计数（不丢语义由全量投影兜底）；
// Close 幂等且不再投递。
func TestSubscribeDropAndClose(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	subscription := controller.Subscribe(1)
	for round := 0; round < 50; round++ {
		if _, err := controller.Begin(testCtx, BeginRequest{Title: "t"}); err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, err := controller.Finish(testCtx, FinishRequest{}); err != nil {
			t.Fatalf("finish: %v", err)
		}
	}
	// 无读者消费 → 单缓冲必溢出。
	if subscription.Dropped() == 0 {
		t.Fatal("溢出应被计数（dropped=0）")
	}
	// Close 幂等。
	subscription.Close()
	subscription.Close()
	// Close 后控制器可继续工作（不向已关通道投递）。
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "后关闭目标"}); err != nil {
		t.Fatalf("close 后 begin: %v", err)
	}
}

// TestConcurrentBeginFinishRace 在 -race 下验证控制器并发安全与不变量。
// goal 栈是共享会话状态（栈顶即收口对象），并发 begin/finish 的正确性是
// 总量守恒：begin 成功数 - finish 成功数 ∈ {0,1}（≤1 个残留 active），
// 且 History 计数 == finish 成功数。
func TestConcurrentBeginFinishRace(t *testing.T) {
	controller := NewController(Options{Depth: 1, Store: NewMemoryStore()})
	const workers = 8
	const rounds = 40
	var beginOK atomic.Int64
	var finishOK atomic.Int64
	var wait sync.WaitGroup
	for w := 0; w < workers; w++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for round := 0; round < rounds; round++ {
				title := fmt.Sprintf("并发目标-%d-%d", worker, round)
				if _, err := controller.Begin(testCtx, BeginRequest{Title: title}); err == nil {
					beginOK.Add(1)
				}
				if _, err := controller.Finish(testCtx, FinishRequest{}); err == nil {
					finishOK.Add(1)
				}
			}
		}(w)
	}
	wait.Wait()
	status := controller.Status()
	if len(status.Stack) > 1 {
		t.Fatalf("并发后栈深超限: %d", len(status.Stack))
	}
	diff := beginOK.Load() - finishOK.Load()
	if diff < 0 || diff > 1 {
		t.Fatalf("总量不守恒: begin=%d finish=%d", beginOK.Load(), finishOK.Load())
	}
	if status.Active == nil && diff != 0 {
		t.Fatalf("有残留 begin 却无 active: begin=%d finish=%d", beginOK.Load(), finishOK.Load())
	}
	if status.Active != nil && diff != 1 {
		t.Fatalf("无残留 begin 却仍 active: %+v", status.Active)
	}
	if int64(len(controller.History())) != finishOK.Load() {
		t.Fatalf("History 计数异常: %d != %d", len(controller.History()), finishOK.Load())
	}
}
