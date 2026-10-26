package table

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

func TestBuildTraceBlockFiltersAndBounds(t *testing.T) {
	tasks := []dto.TaskRecord{
		{ID: "task-b", Status: dto.TaskRunning, Task: "background task"},
		{ID: "task-a", Status: dto.TaskPending, Task: "first task"},
		{ID: "task-done", Status: dto.TaskCompleted, Task: "must disappear"},
		{ID: "task-failed", Status: dto.TaskFailed, Task: "must disappear"},
	}
	block := BuildTraceBlock(TraceInputs{
		SessionID:     "s1",
		ViewSessionID: "s1",
		Tasks:         tasks,
		Limits:        worktable.Limits{WorkTableRows: 100, EvidenceChars: 800},
	})
	if !strings.HasPrefix(block, TraceMarkerOpen) || !strings.HasSuffix(block, TraceMarkerClose) {
		t.Fatalf("trace block markers = %q", block)
	}
	if strings.Index(block, "task-a") > strings.Index(block, "task-b") {
		t.Fatalf("trace rows not stable-sorted: %q", block)
	}
	for _, terminal := range []string{"task-done", "task-failed"} {
		if strings.Contains(block, terminal) {
			t.Fatalf("terminal task %s leaked into trace block: %q", terminal, block)
		}
	}
	if got := strings.Count(strings.TrimSpace(block), "\n") + 1; got > TraceMaxLines {
		t.Fatalf("trace block lines = %d, want <= %d", got, TraceMaxLines)
	}
}

func TestBuildTraceBlockEmpty(t *testing.T) {
	if got := BuildTraceBlock(TraceInputs{
		SessionID: "s1",
		Limits:    worktable.Limits{},
	}); got != "" {
		t.Fatalf("empty trace block = %q, want empty", got)
	}
}
