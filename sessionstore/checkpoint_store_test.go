package sessionstore

import (
	"errors"
	"testing"
	"time"

	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

func TestCheckpointStoreRoundTrip(t *testing.T) {
	router := newTestRouter(t)
	store := NewCheckpointStore(router, router.Workspace())

	snapshot := &workplanTypes.Snapshot{
		NodeID:    "start",
		Timestamp: time.Now().UTC(),
		Status:    workplanTypes.StatusRunning,
		Context:   workplanTypes.NewWorkflowContext(),
	}
	snapshot.Context.SetResultRaw("a", `"output-a"`)
	snapshot.Context.Vars["k"] = "v"
	snapshot.Context.Result.NodeResults = []*workplanTypes.NodeResult{
		{NodeBase: workplanTypes.NodeBase{NodeID: "a", Kind: "auto", Status: "completed", Output: "output-a"}},
	}

	if err := store.Save("plan:demo:run:1", snapshot); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	loaded, err := store.Load("plan:demo:run:1")
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if loaded.NodeID != "start" || loaded.Status != workplanTypes.StatusRunning {
		t.Fatalf("loaded = %+v, want node=start/status=running", loaded)
	}
	if got := loaded.Context.ResultText("a"); got != "output-a" {
		t.Fatalf("result text = %q, want output-a", got)
	}
	if got := loaded.Context.VariableText("k"); got != "v" {
		t.Fatalf("var text = %q, want v", got)
	}
	if len(loaded.Context.Result.NodeResults) != 1 {
		t.Fatalf("node results = %d, want 1", len(loaded.Context.Result.NodeResults))
	}
}

func TestCheckpointStoreOverwriteAndMissing(t *testing.T) {
	router := newTestRouter(t)
	store := NewCheckpointStore(router, router.Workspace())

	first := workplanTypes.NewWorkflowContext()
	first.SetResultRaw("a", `"first"`)
	if err := store.Save("plan:demo:run:1", &workplanTypes.Snapshot{NodeID: "a", Context: first, Status: workplanTypes.StatusRunning}); err != nil {
		t.Fatalf("save first checkpoint: %v", err)
	}
	second := workplanTypes.NewWorkflowContext()
	second.SetResultRaw("a", `"second"`)
	if err := store.Save("plan:demo:run:1", &workplanTypes.Snapshot{NodeID: "a", Context: second, Status: workplanTypes.StatusRunning}); err != nil {
		t.Fatalf("save second checkpoint: %v", err)
	}
	loaded, err := store.Load("plan:demo:run:1")
	if err != nil {
		t.Fatalf("load overwritten checkpoint: %v", err)
	}
	if got := loaded.Context.ResultText("a"); got != "second" {
		t.Fatalf("overwritten result = %q, want second", got)
	}

	if _, err := store.Load("plan:missing"); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("load missing = %v, want ErrCheckpointNotFound", err)
	}
}

func TestCheckpointStoreIsolationAcrossProjects(t *testing.T) {
	router := newTestRouter(t)
	first := NewCheckpointStore(router, "project-a")
	second := NewCheckpointStore(router, "project-b")

	context := workplanTypes.NewWorkflowContext()
	context.SetResultRaw("a", `"a"`)
	if err := first.Save("plan:demo", &workplanTypes.Snapshot{NodeID: "a", Context: context, Status: workplanTypes.StatusRunning}); err != nil {
		t.Fatalf("save in project-a: %v", err)
	}
	if _, err := second.Load("plan:demo"); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("load from project-b = %v, want ErrCheckpointNotFound", err)
	}
}
