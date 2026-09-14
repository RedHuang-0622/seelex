package session

import "testing"

// TestInputQueueMove_ReordersPositionsKeepsSeq 调换排队顺序：位置变化、seq
// 身份不变（seq 是入队时分配的元素身份，不是位置号）。
func TestInputQueueMove_ReordersPositionsKeepsSeq(t *testing.T) {
	queue := NewInputQueue()
	for _, text := range []string{"a", "b", "c", "d"} {
		queue.Enqueue(text, text)
	}

	// a b c d --(1 → 3)--> a c d b
	if !queue.Move(1, 3) {
		t.Fatal("Move(1,3) must succeed inside range")
	}
	assertQueue(t, queue, []string{"a", "c", "d", "b"}, []uint64{1, 3, 4, 2})

	// 回移：a c d b --(3 → 0)--> b a c d
	if !queue.Move(3, 0) {
		t.Fatal("Move(3,0) must succeed inside range")
	}
	assertQueue(t, queue, []string{"b", "a", "c", "d"}, []uint64{2, 1, 3, 4})

	// from == to 是成功的空操作（不改变顺序）。
	if !queue.Move(2, 2) {
		t.Fatal("Move(2,2) must be a successful no-op")
	}
	assertQueue(t, queue, []string{"b", "a", "c", "d"}, []uint64{2, 1, 3, 4})
}

// TestInputQueueMove_OutOfRangeOrNil 越界与 nil 句柄：返回 false、不 panic、
// 队列保持不变（B1 安全操作）。
func TestInputQueueMove_OutOfRangeOrNil(t *testing.T) {
	queue := NewInputQueue()
	queue.Enqueue("only", nil)

	for _, bounds := range [][2]int{{-1, 0}, {0, -1}, {1, 0}, {0, 1}, {5, 5}} {
		if queue.Move(bounds[0], bounds[1]) {
			t.Fatalf("Move(%d,%d) must be rejected for a length-1 queue", bounds[0], bounds[1])
		}
	}
	assertQueue(t, queue, []string{"only"}, []uint64{1})

	var nilQueue *InputQueue
	if nilQueue.Move(0, 1) {
		t.Fatal("nil queue Move must be false")
	}
	if nilQueue.Move(0, 0) {
		t.Fatal("nil queue Move must not report success")
	}
	empty := NewInputQueue()
	if empty.Move(0, 0) {
		t.Fatal("Move on an empty queue must be false")
	}
}

// TestInputQueueRemoveAt_PopsIdentity 撤回：按位置取出元素并保留 seq/载荷，
// 其余元素顺序与 seq 不变。
func TestInputQueueRemoveAt_PopsIdentity(t *testing.T) {
	queue := NewInputQueue()
	queue.Enqueue("a", 1)
	queue.Enqueue("b", 2)
	queue.Enqueue("c", 3)

	item, ok := queue.RemoveAt(1)
	if !ok || item.Text != "b" || item.Seq != 2 || item.Payload != 2 {
		t.Fatalf("RemoveAt(1) = %+v, ok=%v; want b/seq2/payload2", item, ok)
	}
	assertQueue(t, queue, []string{"a", "c"}, []uint64{1, 3})

	// 末尾与队首删除
	if tail, ok := queue.RemoveAt(1); !ok || tail.Text != "c" {
		t.Fatalf("RemoveAt(1) = %+v, ok=%v; want c", tail, ok)
	}
	if head, ok := queue.RemoveAt(0); !ok || head.Text != "a" {
		t.Fatalf("RemoveAt(0) = %+v, ok=%v; want a", head, ok)
	}
	if queue.Len() != 0 {
		t.Fatalf("queue length after full drain = %d, want 0", queue.Len())
	}
	// 越界（含空队列）返回 false
	for _, index := range []int{-1, 0, 7} {
		if _, ok := queue.RemoveAt(index); ok {
			t.Fatalf("RemoveAt(%d) on an empty queue must be false", index)
		}
	}
	var nilQueue *InputQueue
	if _, ok := nilQueue.RemoveAt(0); ok {
		t.Fatal("nil queue RemoveAt must be false")
	}
}

// TestSessionUnitRecallAndReorder 会话单元层的调换/撤回：撤回交还展示原文
// （引擎侧载荷随条目丢弃），调换只改位置。
func TestSessionUnitRecallAndReorder(t *testing.T) {
	unit, err := NewSessionUnit("sess-queue")
	if err != nil {
		t.Fatalf("NewSessionUnit: %v", err)
	}
	unit.Enqueue(QueuedRequest{DisplayInput: "first", Payload: "p-first"})
	unit.Enqueue(QueuedRequest{DisplayInput: "second", Payload: "p-second"})
	unit.Enqueue(QueuedRequest{DisplayInput: "third", Payload: "p-third"})

	if !unit.ReorderRequests(2, 0) {
		t.Fatal("ReorderRequests(2,0) must succeed")
	}
	requests := unit.PendingRequests()
	assertDisplays(t, requests, []string{"third", "first", "second"})

	recalled, ok := unit.RecallRequest(1)
	if !ok || recalled.DisplayInput != "first" || recalled.Payload != "p-first" {
		t.Fatalf("RecallRequest(1) = %+v, ok=%v; want first/p-first", recalled, ok)
	}
	assertDisplays(t, unit.PendingRequests(), []string{"third", "second"})

	// 越界与 nil 单元
	if _, ok := unit.RecallRequest(2); ok {
		t.Fatal("RecallRequest out of range must be false")
	}
	if unit.ReorderRequests(-1, 0) || unit.ReorderRequests(0, 2) {
		t.Fatal("ReorderRequests out of range must be false")
	}
	var nilUnit *SessionUnit
	if _, ok := nilUnit.RecallRequest(0); ok {
		t.Fatal("nil unit RecallRequest must be false")
	}
	if nilUnit.ReorderRequests(0, 1) {
		t.Fatal("nil unit ReorderRequests must be false")
	}
}

func assertQueue(t *testing.T, queue *InputQueue, wantText []string, wantSeq []uint64) {
	t.Helper()
	snapshot := queue.Snapshot()
	if len(snapshot) != len(wantText) {
		t.Fatalf("queue length = %d, want %d (%+v)", len(snapshot), len(wantText), snapshot)
	}
	for index, item := range snapshot {
		if item.Text != wantText[index] || item.Seq != wantSeq[index] {
			t.Fatalf("queue[%d] = %q/seq%d, want %q/seq%d", index, item.Text, item.Seq, wantText[index], wantSeq[index])
		}
	}
}

func assertDisplays(t *testing.T, requests []QueuedRequest, want []string) {
	t.Helper()
	if len(requests) != len(want) {
		t.Fatalf("pending requests = %d, want %d (%+v)", len(requests), len(want), requests)
	}
	for index, request := range requests {
		if request.DisplayInput != want[index] {
			t.Fatalf("pending[%d] = %q, want %q", index, request.DisplayInput, want[index])
		}
	}
}
