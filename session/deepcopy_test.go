package session

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestForkSessionDeepCopy（T2.7）：fork 后父子引用不相交，改子不影响父。
func TestForkSessionDeepCopy(t *testing.T) {
	parent, err := NewSessionUnit("parent")
	if err != nil {
		t.Fatal(err)
	}
	parent.View.Conversation = append(parent.View.Conversation, model.Message{
		ID: "p-1", Role: "assistant", Content: "parent content",
	})
	parent.View.TotalMessages = 1
	parent.Queue.Enqueue("p-queued", nil)
	parent.Context = &ContextStack{Plan: []string{"plan-p"}, Task: []string{"task-p"}}

	child, err := ForkDeepCopy(parent, "child")
	if err != nil {
		t.Fatal(err)
	}
	if child.View == nil || child.Context == nil || child.Queue == nil {
		t.Fatal("fork child must have fresh V/Q/C")
	}

	// 修改子会话：View/队列/上下文栈全部变更。
	child.View.Conversation = append(child.View.Conversation, model.Message{
		ID: "c-1", Role: "assistant", Content: "child mutation",
	})
	child.View.TotalMessages++
	child.Queue.Enqueue("c-queued", nil)
	child.Context.Plan = append(child.Context.Plan, "plan-c")
	child.Context.Task[0] = "task-c"

	// 父会话不受影响（引用不相交）。
	if len(parent.View.Conversation) != 1 || parent.View.Conversation[0].ID != "p-1" {
		t.Fatalf("parent conversation mutated by child: %+v", parent.View.Conversation)
	}
	if parent.View.TotalMessages != 1 {
		t.Fatalf("parent total messages = %d, want 1", parent.View.TotalMessages)
	}
	if parent.Queue.Len() != 1 {
		t.Fatalf("parent queue len = %d, want 1", parent.Queue.Len())
	}
	if len(parent.Context.Plan) != 1 || parent.Context.Plan[0] != "plan-p" {
		t.Fatalf("parent plan stack mutated: %+v", parent.Context.Plan)
	}
	if parent.Context.Task[0] != "task-p" {
		t.Fatalf("parent task stack mutated: %+v", parent.Context.Task)
	}
}

// TestDeepCopyBoundary（B4）：fork/冷加载引用不相交；共享面仅只读常量。
func TestDeepCopyBoundary(t *testing.T) {
	stack := ContextStack{
		Plan:    []string{"p1", "p2"},
		Task:    []string{"t1"},
		Skill:   []string{"s1"},
		Compact: []string{"c1", "c2", "c3"},
	}
	copied := DeepCopyContextStack(stack)
	copied.Plan[0] = "changed"
	copied.Task = append(copied.Task, "extra")
	copied.Compact = copied.Compact[:1]

	if stack.Plan[0] != "p1" || len(stack.Task) != 1 || len(stack.Compact) != 3 {
		t.Fatalf("source context stack mutated: %+v", stack)
	}

	record := SessionRecord{
		ID:       "sess-a",
		Kind:     KindMain,
		Title:    "A",
		Status:   StatusIdle,
		Binding:  SessionBinding{WorkspaceID: "proj-a", Kind: KindMain},
	}
	copiedRecord := DeepCopyRecordPrefix(record)
	copiedRecord.Title = "B"
	copiedRecord.Binding.WorkspaceID = "proj-b"
	if record.Title != "A" || record.Binding.WorkspaceID != "proj-a" {
		t.Fatalf("record prefix copy mutated source: %+v", record)
	}
	if copiedRecord.ID != record.ID || copiedRecord.Kind != record.Kind {
		t.Fatalf("record prefix copy lost identity: %+v", copiedRecord)
	}
}

// TestForkDeepCopyNilParent（B1 边界）：nil 父单元明确错误，不 panic。
func TestForkDeepCopyNilParent(t *testing.T) {
	if _, err := ForkDeepCopy(nil, "child"); err == nil {
		t.Fatal("fork from nil parent must return an error")
	}
	if _, err := ForkDeepCopy(nil, ""); err == nil {
		t.Fatal("fork from nil parent with empty id must return an error")
	}
}
