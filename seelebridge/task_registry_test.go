package seelebridge

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
	registry := newTaskRegistry()
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
	todo := taskToTodoItem(items[0])
	if todo.Status != TodoItemDone || !todo.Done {
		t.Fatalf("item0 = %+v, want done", todo)
	}
	if second := taskToTodoItem(items[1]); second.Status != TodoItemPending || second.Done {
		t.Fatalf("item1 = %+v, want pending", second)
	}
}

func TestTaskRegistryRejectsInvalidOperations(t *testing.T) {
	registry := newTaskRegistry()
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
	registry := newTaskRegistry()
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
	registry := newTaskRegistry()
	defer registry.Close()
	if err := registry.ReplaceTodo([]TodoItem{{Text: "a", Status: TodoItemDone}}); err != nil {
		t.Fatal(err)
	}
	items := registry.TodoSnapshot()
	items[0] = TaskRecord{Task: "mutated"}
	clean := registry.TodoSnapshot()
	if taskToTodoItem(clean[0]).Text != "a" {
		t.Fatalf("snapshot must be a defensive copy: %+v", clean)
	}
}

func TestTaskRegistryConcurrentStatusAndSnapshot(t *testing.T) {
	registry := newTaskRegistry()
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
		if taskToTodoItem(item).Status != TodoItemDone {
			t.Fatalf("item[%d] = %+v, want done", index, taskToTodoItem(item))
		}
	}
}

func TestTaskRegistryCloseFailsSends(t *testing.T) {
	registry := newTaskRegistry()
	registry.Close()
	if _, _, err := registry.Add(TaskSpec{Key: "k", Task: "x", Kind: "task"}); !errors.Is(err, errTaskClosed) {
		t.Fatalf("send after close = %v, want errTaskClosed", err)
	}
	registry.Close() // 幂等，不 panic
}

func TestTaskRegistryIdempotentAddByKey(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.Close()
	key := taskKeyForGoal("审查代码结构")
	first, created, err := registry.Add(TaskSpec{Key: key, Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || !created {
		t.Fatalf("first add: created=%v err=%v", created, err)
	}
	second, created, err := registry.Add(TaskSpec{Key: key, Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate add must return existing task: created=%v second=%+v first=%+v", created, second, first)
	}
	// 归一化：空白/大小写不同仍命中同一幂等键。
	again, created, err := registry.Add(TaskSpec{Key: taskKeyForGoal("  审查  代码结构 "), Phase: "task", Task: "审查代码结构", Kind: "task"})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("normalized key must dedupe: created=%v again=%+v", created, again)
	}
}

func TestTaskRegistryRetryIncrementsCount(t *testing.T) {
	registry := newTaskRegistry()
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

// TestTaskRegistryEmitChangeNeverBlocksActor 死锁回归：changes channel 被
// 消费者停摆占满时，注册表 actor 不得阻塞（非阻塞丢弃 + 计数）——否则会
// 形成“service.mu → 子代理会话锁 → actor → channel”环路死锁，子代理全部
// 卡 queued。
func TestTaskRegistryEmitChangeNeverBlocksActor(t *testing.T) {
	registry := newTaskRegistry()
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
	registry := newTaskRegistry()
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
	registry := newTaskRegistry()
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

// TestBindSubagentTaskIdempotent 验证 B6 装配件：相同 goal 的子代理绑定同一
// task（幂等），第二个子代理作为参与者挂到同一 task。
func TestBindSubagentTaskIdempotent(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	first := runtime.bindSubagentTask(forkSubagentSpec{ID: "s1", Goal: "分析作者画像"})
	second := runtime.bindSubagentTask(forkSubagentSpec{ID: "s2", Goal: "分析作者画像"})
	if first == "" || first != second {
		t.Fatalf("same goal must bind same task: first=%q second=%q", first, second)
	}
	records := runtime.TaskSnapshot()
	if len(records) != 1 {
		t.Fatalf("tasks = %+v, want 1（同一 task 不重复建条目）", records)
	}
	record := records[0]
	if record.Status != TaskQueued {
		t.Fatalf("task status after bind = %v, want queued（会话未启动前不显示 running）", record.Status)
	}
	if len(record.Participants) != 2 || record.Participants[0] != "s1" || record.Participants[1] != "s2" {
		t.Fatalf("participants = %v, want [s1 s2]", record.Participants)
	}
}
