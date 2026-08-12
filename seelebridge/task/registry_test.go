package task

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── task 注册表 actor（Actor + Mailbox 白盒测试；todolist 融合验证）──

func TestTaskRegistryTodoThreeStateLifecycle(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()

	if err := registry.ReplaceTodo([]TodoItem{
		{Text: "a", Status: TodoItemPending},
		{Text: "b", Status: TodoItemPending},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetTodoStatusByIndex(0, TaskDoing); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetTodoStatusByIndex(0, TaskCompleted); err != nil {
		t.Fatal(err)
	}
	items := registry.TodoSnapshot()
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	todo := TaskToTodoItem(items[0])
	if todo.Status != TodoItemDone || !todo.Done {
		t.Fatalf("item0 = %+v, want done", todo)
	}
	if second := TaskToTodoItem(items[1]); second.Status != TodoItemPending || second.Done {
		t.Fatalf("item1 = %+v, want pending", second)
	}
}

func TestTaskRegistryRejectsInvalidOperations(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()

	if _, err := registry.SetTodoStatusByIndex(0, TaskCompleted); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range must be rejected, got %v", err)
	}
	if err := registry.ReplaceTodo([]TodoItem{{Text: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus("todo:1", "whatever", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown id must be rejected, got %v", err)
	}
}

func TestTaskRegistryAppendEnforcesLimitInsideActor(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	if err := registry.ReplaceTodo([]TodoItem{{Text: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.AppendTodo(TodoItem{Text: "b"}, 2); err != nil {
		t.Fatal(err)
	}
	if err := registry.AppendTodo(TodoItem{Text: "c"}, 2); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("limit must be enforced inside actor, got %v", err)
	}
	if len(registry.TodoSnapshot()) != 2 {
		t.Fatalf("items = %+v", registry.TodoSnapshot())
	}
}

func TestTaskRegistryTodoSnapshotIsDefensiveCopy(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	if err := registry.ReplaceTodo([]TodoItem{{Text: "a", Status: TodoItemDone}}); err != nil {
		t.Fatal(err)
	}
	items := registry.TodoSnapshot()
	items[0] = TaskRecord{Task: "mutated"}
	clean := registry.TodoSnapshot()
	if TaskToTodoItem(clean[0]).Text != "a" {
		t.Fatalf("snapshot must be a defensive copy: %+v", clean)
	}
}

func TestTaskRegistryConcurrentStatusAndSnapshot(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	const count = 50
	items := make([]TodoItem, count)
	for index := range items {
		items[index] = TodoItem{Text: "t", Status: TodoItemPending}
	}
	if err := registry.ReplaceTodo(items); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, _ = registry.SetTodoStatusByIndex(index, TaskCompleted)
		}(index)
	}
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = registry.Snapshot()
			_ = registry.TodoSnapshot()
		}()
	}
	close(start)
	wg.Wait()

	for index, item := range registry.TodoSnapshot() {
		if TaskToTodoItem(item).Status != TodoItemDone {
			t.Fatalf("item[%d] = %+v, want done", index, TaskToTodoItem(item))
		}
	}
}

func TestTaskRegistryCloseFailsSends(t *testing.T) {
	registry := NewTaskRegistry()
	registry.Close()
	if _, _, err := registry.Add(TaskSpec{Key: "k", Task: "x", Kind: "task"}); !errors.Is(err, errTaskClosed) {
		t.Fatalf("send after close = %v, want errTaskClosed", err)
	}
	registry.Close() // 幂等，不 panic
}

func TestTaskRegistryIdempotentAddByKey(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	key := TaskKeyForGoal("审查代码结构")
	first, created, err := registry.Add(TaskSpec{Key: key, Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || !created {
		t.Fatalf("first add: created=%v err=%v", created, err)
	}
	second, created, err := registry.Add(TaskSpec{Key: key, Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate add must return existing task: created=%v second=%+v first=%+v", created, second, first)
	}
	// 归一化：空白/大小写不同仍命中同一幂等键。
	again, created, err := registry.Add(TaskSpec{Key: TaskKeyForGoal("  审查  代码结构 "), Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("normalized key must dedupe: created=%v again=%+v", created, again)
	}
}

func TestTaskRegistryRetryIncrementsCount(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	task, created, err := registry.Add(TaskSpec{Key: "k1", Phase: "plan", Task: "t", Kind: "plan"})
	if err != nil || !created {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskRetry, "first failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskRunning, "retry run"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskRetry, "second failure"); err != nil {
		t.Fatal(err)
	}
	records := registry.Snapshot()
	var record TaskRecord
	for _, candidate := range records {
		if candidate.ID == task.ID {
			record = candidate
		}
	}
	if record.RetryCount != 2 || record.Status != TaskRetry {
		t.Fatalf("retry record = %+v, want retry_count=2", record)
	}
}

// TestTaskRegistryTerminalReopensToRetry 验证终态（completed/failed）可重开为
// retry（RetryCount 自增），但不可回退到其他状态；retry 不可回退 queued。
func TestTaskRegistryTerminalReopensToRetry(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	task, created, err := registry.Add(TaskSpec{Key: "k1", Phase: "plan", Task: "t", Kind: "plan"})
	if err != nil || !created {
		t.Fatal(err)
	}

	// completed → retry 允许（重试语义）。
	if _, err := registry.SetStatus(task.ID, TaskCompleted, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskRetry, "retried after done"); err != nil {
		t.Fatalf("completed -> retry must be allowed: %v", err)
	}
	// retry → running 允许（真正重跑），计数保留。
	if _, err := registry.SetStatus(task.ID, TaskRunning, "retry run"); err != nil {
		t.Fatal(err)
	}
	// running 中失败 → retry（计数自增）→ running。
	if _, err := registry.SetStatus(task.ID, TaskRetry, "second failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskRunning, "second run"); err != nil {
		t.Fatal(err)
	}
	// running → queued 必须拒绝（回归保护）。
	if _, err := registry.SetStatus(task.ID, TaskQueued, "regress"); err == nil {
		t.Fatal("running -> queued must be rejected")
	}
	// retry → queued 必须拒绝（避免 re-fork 覆盖重试计数）。
	if _, err := registry.SetStatus(task.ID, TaskRetry, "retry again"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskQueued, "regress from retry"); err == nil {
		t.Fatal("retry -> queued must be rejected")
	}

	records := registry.Snapshot()
	var record TaskRecord
	for _, candidate := range records {
		if candidate.ID == task.ID {
			record = candidate
		}
	}
	if record.RetryCount != 3 || record.Status != TaskRetry {
		t.Fatalf("retry record = %+v, want retry_count=3 retry", record)
	}
}

// TestTaskRegistryFailedReopensToRetry 验证 failed → retry 允许、failed →
// 其他状态仍拒绝。
func TestTaskRegistryFailedReopensToRetry(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	task, created, err := registry.Add(TaskSpec{Key: "k2", Phase: "plan", Task: "t", Kind: "plan"})
	if err != nil || !created {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskFailed, "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetStatus(task.ID, TaskQueued, "regress"); err == nil {
		t.Fatal("failed -> queued must be rejected")
	}
	if _, err := registry.SetStatus(task.ID, TaskRetry, "retry"); err != nil {
		t.Fatalf("failed -> retry must be allowed: %v", err)
	}
	record, ok, resolveErr := registry.ResolveByKey("k2")
	if resolveErr != nil || !ok || record.Status != TaskRetry || record.RetryCount != 1 {
		t.Fatalf("retry record = %+v", record)
	}
}

// TestTaskRegistryEmitChangeNeverBlocksActor 死锁回归：changes channel 被
// 消费者停摆占满时，注册表 actor 不得阻塞（非阻塞丢弃 + 计数）——否则会
// 形成“service.mu → 子代理会话锁 → actor → channel”环路死锁，子代理全部
// 卡 queued。
func TestTaskRegistryEmitChangeNeverBlocksActor(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	for index := 0; index < cap(registry.changes); index++ {
		registry.changes <- TaskRecord{ID: fmt.Sprintf("fill-%d", index)}
	}
	task, _, err := registry.Add(TaskSpec{Key: "k1", Phase: TaskPhaseTask, Task: "t", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = registry.SetStatus(task.ID, TaskRunning, "started")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registry actor must never block on the changes channel")
	}
	if registry.DroppedChanges() == 0 {
		t.Fatal("dropped change counter must increase when channel is full")
	}
}

func TestTaskRegistryChangesChannel(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	task, created, err := registry.Add(TaskSpec{Key: "k1", Phase: "task", Task: "t", Kind: "task"})
	if err != nil || !created {
		t.Fatal(err)
	}
	_, _ = registry.SetStatus(task.ID, TaskRunning, "started")
	changes := registry.TaskChanged()
	seen := make([]TaskRecord, 0, 2)
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case record := <-changes:
			seen = append(seen, record)
		case <-deadline:
			t.Fatalf("expected 2 change events, got %d: %+v", len(seen), seen)
		}
	}
	if seen[0].Status != TaskPending || seen[1].Status != TaskRunning || seen[1].ID != task.ID {
		t.Fatalf("change events = %+v", seen)
	}
}

// TestTaskRegistryReplaceAllSwitchesSession 会话级隔离：整体替换注册表
// （清空旧会话 task/todo，恢复目标会话），旧数据不残留。
func TestTaskRegistryReplaceAllSwitchesSession(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()
	_, _, _ = registry.Add(TaskSpec{ID: "task:a", Key: "k:a", Phase: TaskPhaseTask, Task: "a", Kind: "task"})
	_, _, _ = registry.Add(TaskSpec{ID: "todo:0", Phase: TaskPhaseTasklist, Task: "旧", Kind: "todo"})
	if err := registry.ReplaceAll([]TaskRecord{{
		ID: "task:b", Key: "k:b", Phase: TaskPhaseTask, Task: "b", Kind: "task", Status: TaskRunning,
	}}); err != nil {
		t.Fatal(err)
	}
	records := registry.Snapshot()
	if len(records) != 1 || records[0].ID != "task:b" || records[0].Status != TaskRunning {
		t.Fatalf("session switch must replace tasks: %+v", records)
	}
	if todo := registry.TodoSnapshot(); len(todo) != 0 {
		t.Fatalf("todo must be cleared on session switch: %+v", todo)
	}
}
