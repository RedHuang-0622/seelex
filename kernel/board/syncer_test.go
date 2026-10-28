package board

// HEADER-syncer_test.go

import (
	"errors"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/board/boardtest"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func boardDeps(st ProjectBoardStore, idle time.Duration, rows func(string) []model.WorkItem) ProjectBoardDeps {
	return ProjectBoardDeps{
		Store:          st,
		WorkTableRows:  rows,
		EvidenceChars:  func() int { return 800 },
		ProjectRootFor: func(sessionID string) string { return boardtest.ProjectRoot },
		Triggers:       TriggerFor("todo_list"),
		FlushIdle:      idle,
		Now:            func() time.Time { return time.Unix(boardtest.At, 0) },
	}
}

func TestProjectBoardSyncerDebounce(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	for index := 0; index < 5; index++ {
		syncer.OnToolComplete("todo_add", nil, "s-1")
	}
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)
	boardtest.Settle(idle)
	if got := st.WriteCount(dto.ProjectBoardWorkFile); got != 1 {
		t.Fatalf("同批 5 次触发落盘 %d 次，希望 1 次", got)
	}
}

// ---------------------------------------------------------------------------
// T2.5 指纹去重：内容没变不写盘
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerFingerprint(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)

	// 活体变了 ⇒ 必须落第二次（这一步同时证明"管道还活着"，否则"没落盘"的断言毫无意义）。
	live.Add(boardtest.TaskItem("task:1", "b", "running"))
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 2)

	// 内容没变（连触发都给了）⇒ 不写盘。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	syncer.OnToolComplete("task_add", nil, "s-1")
	boardtest.Settle(idle)
	if got := st.WriteCount(dto.ProjectBoardWorkFile); got != 2 {
		t.Fatalf("内容没变仍落盘 %d 次，希望 2 次（指纹去重）", got)
	}
	if got := len(st.Folded(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile)); got != 2 {
		t.Fatalf("折叠读行数 = %d，希望 2", got)
	}
}

// ---------------------------------------------------------------------------
// T2.6 水位：seq = 水位 + 1；重复提交是空操作
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerWatermark(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)
	first := st.LastWrite(dto.ProjectBoardWorkFile)
	if len(first) != 1 || first[0].Seq != 1 {
		t.Fatalf("第一批 seq = %+v，希望 1（水位 0 + 1）", first)
	}
	if watermark, _ := st.ProjectBoardWatermark(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile); watermark != 1 {
		t.Fatalf("水位 = %d，希望 1", watermark)
	}

	// 重复提交（内容没变）⇒ 空操作：水位不动、没有新的追加批。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.Settle(idle)
	if got := st.WriteCount(dto.ProjectBoardWorkFile); got != 1 {
		t.Fatalf("重复提交落盘 %d 次，希望仍是 1 次", got)
	}
	if watermark, _ := st.ProjectBoardWatermark(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile); watermark != 1 {
		t.Fatalf("重复提交后水位 = %d，希望仍是 1", watermark)
	}

	// 内容变了 ⇒ 水位推进：下一批 seq = 2。
	live.Add(boardtest.TaskItem("task:1", "b", "running"))
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 2)
	second := st.LastWrite(dto.ProjectBoardWorkFile)
	for _, row := range second {
		if row.Seq != 2 {
			t.Fatalf("第二批 seq = %d，希望 2（水位 1 + 1）", row.Seq)
		}
	}
	if watermark, _ := st.ProjectBoardWatermark(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile); watermark != 2 {
		t.Fatalf("水位 = %d，希望 2", watermark)
	}
}

// ---------------------------------------------------------------------------
// T2.7 里程碑状态：运行态优先、声明态兜底；整队收口读 TeamworkState.State
// ---------------------------------------------------------------------------

func TestProjectBoardRebuildAfterDelete(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"), boardtest.TaskItem("task:1", "b", "running"))
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)
	before := boardtest.MarshalRows(t, st.Folded(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile))

	// 文件被删（人删 / 数据根搬迁 / 掉了一块盘）：下一次触发必须重建。
	st.DropFile(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile)
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 2)
	after := boardtest.MarshalRows(t, st.Folded(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile))
	if before != after {
		t.Fatalf("重建结果与删除前不同：\n before=%s\n after =%s", before, after)
	}
	// 水位从 0 重来 ⇒ 重建批的 seq 又是 1（水位是**文件内**的事实）。
	for _, row := range st.LastWrite(dto.ProjectBoardWorkFile) {
		if row.Seq != 1 {
			t.Fatalf("重建批 seq = %d，希望 1（文件没了，水位归零）", row.Seq)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.9 反例（I2）：篡改投影文件 ⇒ 活体投影与领域判定不变
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerIgnoresFailedAndUntriggeredCalls(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"))
	deps := boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() })
	deps.Triggers = TriggerFor("task_complete")
	syncer := NewProjectBoardSyncer(deps)
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")                     // 未命中触发面
	syncer.OnToolComplete("task_complete", errors.New("boom"), "s-1") // 工具失败
	syncer.OnToolComplete("task_complete", nil, "")                   // 没有归属会话
	boardtest.Settle(idle)
	if got := st.WriteCount(dto.ProjectBoardWorkFile); got != 0 {
		t.Fatalf("不该落盘却落了 %d 次", got)
	}

	syncer.OnToolComplete("task_complete", nil, "s-1") // 命中：正常落盘
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)
}

// ---------------------------------------------------------------------------
// 配套 3：消失的 key 补 tombstone（upsert 语义下"删掉的条目要能消失"）
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerTombstonesRemovedKeys(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"), boardtest.TaskItem("task:1", "b", "running"))
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 1)
	if got := len(st.Folded(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile)); got != 2 {
		t.Fatalf("折叠读行数 = %d，希望 2", got)
	}

	live.Drop(1) // 活没了：task:1 从活体投影里消失
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, dto.ProjectBoardWorkFile, 2)
	batch := st.LastWrite(dto.ProjectBoardWorkFile)
	removed := false
	for _, row := range batch {
		if row.Key == "work:task:1" {
			if row.Payload["removed"] != true {
				t.Fatalf("消失的 key 补的行 = %+v，希望 payload{removed:true}", row)
			}
			removed = true
		}
	}
	if !removed {
		t.Fatalf("没有为消失的 key work:task:1 补 tombstone：%+v", batch)
	}
	folded := st.Folded(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile)
	if got := boardtest.Keys(folded); len(got) != 1 || got[0] != "work:todo:0" {
		t.Fatalf("折叠读 key 集 = %v，希望只剩 work:todo:0", got)
	}
}

// ---------------------------------------------------------------------------
// 配套 4：Close 幂等且不泄漏 goroutine
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerCloseIsIdempotent(t *testing.T) {
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	syncer := NewProjectBoardSyncer(boardDeps(st, idle, func(string) []model.WorkItem { return nil }))

	syncer.OnToolComplete("todo_add", nil, "s-1")
	done := make(chan struct{})
	go func() {
		syncer.Close()
		syncer.Close() // 第二次 Close 幂等
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 没有在限期内返回（goroutine 泄漏）")
	}
	// 关闭之后再触发是无害的空操作（不 panic、不落盘）。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.Settle(idle)
	if got := st.WriteCount(dto.ProjectBoardWorkFile); got != 0 {
		t.Fatalf("Close 之后仍落盘 %d 次", got)
	}
}

// ---------------------------------------------------------------------------
// 配套 5：M3 装配要用的两个只读取数口
// ---------------------------------------------------------------------------

func TestProjectBoardHookFailureNoHalfWrite(t *testing.T) {
	const idle = 20 * time.Millisecond
	flaky := &boardtest.FlakyStore{Store: boardtest.NewStore(), FailTimes: 1}
	live := boardtest.NewLiveRows(boardtest.TodoItem("todo:0", "a", "doing"), boardtest.TaskItem("task:1", "b", "running"))
	syncer := NewProjectBoardSyncer(boardDeps(flaky, idle, func(string) []model.WorkItem { return live.Rows() }))
	defer syncer.Close()

	// 第一次触发：写盘失败 ⇒ 文件里一行都没有（不是"写了一半"）、水位也没被占号。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.Settle(idle)
	if got := flaky.Raw(boardtest.ProjectRoot, "s-1", dto.ProjectBoardWorkFile); len(got) != 0 {
		t.Fatalf("失败的批次留下了半截行：%+v", got)
	}

	// 记账没推进 ⇒ 下一次触发重试成功，且水位从 1 起（失败批次没占 Seq）。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, flaky.Store, dto.ProjectBoardWorkFile, 1)
	batch := flaky.LastWrite(dto.ProjectBoardWorkFile)
	if len(batch) != 2 {
		t.Fatalf("重试批行数 = %d，希望 2（%+v）", len(batch), batch)
	}
	for _, row := range batch {
		if row.Seq != 1 {
			t.Fatalf("重试批 seq = %d，希望 1（失败批次不该占号）", row.Seq)
		}
	}
}
