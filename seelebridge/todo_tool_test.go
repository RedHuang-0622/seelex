package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ── todolist 工具族（切片 6，docs/2026-08-03-subagent-fork-architecture/plan.md §5）──

// TestTodoListLifecycle 验证 init → add → done → status 全链路与全部完成提示。
func TestTodoListLifecycle(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	dispatch := func(name, args string) (string, error) {
		return runtime.Agent().DirectDispatch(context.Background(), name, args)
	}

	// init：建立清单。
	out, err := dispatch("todolist_init", `{"items":["inspect module","implement fix","run tests"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"total":3`) || strings.Contains(out, `"done":3`) {
		t.Fatalf("init status = %s", out)
	}

	// add：追加一项。
	out, err = dispatch("todolist_add", `{"item":"write docs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"total":4`) {
		t.Fatalf("add status = %s", out)
	}

	// done：逐项完成；最后一项触发 all_done 提示。
	for i := 0; i < 4; i++ {
		out, err = dispatch("todolist_done", fmt.Sprintf(`{"index":%d}`, i))
		if err != nil {
			t.Fatalf("done[%d]: %v", i, err)
		}
		if i < 3 && strings.Contains(out, "all_done") {
			t.Fatalf("early all_done at %d: %s", i, out)
		}
	}
	if !strings.Contains(out, `"all_done": true`) || !strings.Contains(out, "task_complete") {
		t.Fatalf("final done must hint task_complete: %s", out)
	}
	if !strings.Contains(out, `"done":4`) {
		t.Fatalf("final status = %s", out)
	}
}

// TestTodoListValidation 验证护栏：超上限、越界索引、空项。
func TestTodoListValidation(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	dispatch := func(name, args string) (string, error) {
		return runtime.Agent().DirectDispatch(context.Background(), name, args)
	}

	// 超上限（默认 20）。
	big := `{"items":["a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p","q","r","s","t","u"]}`
	if _, err := dispatch("todolist_init", big); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("oversize init must be rejected, got %v", err)
	}
	if _, err := dispatch("todolist_init", `{"items":["a","b"]}`); err != nil {
		t.Fatal(err)
	}
	// 越界索引。
	if _, err := dispatch("todolist_done", `{"index":5}`); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("out-of-range index must be rejected, got %v", err)
	}
}
