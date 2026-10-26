package goal

import (
	"testing"
)

func TestStackLIFO(t *testing.T) {
	var stack Stack
	if stack.Len() != 0 || stack.Top() != nil || stack.Pop() != nil {
		t.Fatalf("空栈语义错误: len=%d", stack.Len())
	}
	stack.Push(&GoalRecord{ID: "g-1", Status: StatusActive})
	stack.Push(&GoalRecord{ID: "g-2", Status: StatusActive})
	if stack.Len() != 2 {
		t.Fatalf("压栈后 len=%d", stack.Len())
	}
	if top := stack.Top(); top == nil || top.ID != "g-2" {
		t.Fatalf("栈顶应为 g-2, 得 %+v", top)
	}
	all := stack.All()
	if len(all) != 2 || all[0].ID != "g-1" || all[1].ID != "g-2" {
		t.Fatalf("All 应为 栈底→栈顶 [g-1 g-2], 得 %d 个", len(all))
	}
	if popped := stack.Pop(); popped == nil || popped.ID != "g-2" {
		t.Fatalf("弹栈应为 g-2, 得 %+v", popped)
	}
	if top := stack.Top(); top == nil || top.ID != "g-1" {
		t.Fatalf("弹栈后栈顶应为 g-1, 得 %+v", top)
	}
	if stack.Len() != 1 {
		t.Fatalf("弹栈后 len=%d", stack.Len())
	}
}
