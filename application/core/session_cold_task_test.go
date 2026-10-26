package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestSessionColdTaskRulesAreExtracted(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionColdTask")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionColdTask")
	}
	if field.Type != reflect.TypeOf((*sessionColdTask)(nil)) {
		t.Fatalf("sessionColdTask 字段类型 = %s", field.Type)
	}
	taskType := reflect.TypeOf((*sessionColdTask)(nil))
	if _, ok := taskType.MethodByName("RestoreLocked"); !ok {
		t.Fatal("sessionColdTask 缺少 RestoreLocked")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_history.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resumeSessionCold" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "RestoreSessionTaskLockedFor" ||
				selector.Sel.Name == "ResetForNewSessionLocked" ||
				selector.Sel.Name == "LoadSessionCompactionRecords" ||
				selector.Sel.Name == "RetainedFromForCompactions") {
				t.Errorf("resumeSessionCold 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestSessionColdTaskRestoresCompactionsAndRetainedWindow(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	storedCompactions := []model.ContextCompaction{{Version: 1, Reason: "stored"}}
	record := model.SessionRecord{
		Execution: model.SessionExecutionRecord{
			Task: &model.TaskState{ContextCompactions: storedCompactions},
		},
		Projection: &model.TaskContextProjection{
			Checkpoint: model.TaskCheckpoint{CoversEventRange: model.EventRange{End: 9}},
		},
	}
	transcript := []model.TranscriptEvent{{Seq: 4}}
	var restored task_context.RestoredTaskState
	component := newSessionColdTask(sessionColdTaskDeps{
		LoadCompactions: func(session_runtime.Location, string) ([]model.ContextCompaction, bool, error) {
			t.Fatal("record compactions 存在时不得读新址")
			return nil, false, nil
		},
		LatestUser: func([]model.Message) string { return "latest" },
		RetainedFrom: func(got []model.TranscriptEvent, compactions []model.ContextCompaction) int {
			if len(got) != 1 || len(compactions) != 1 || compactions[0].Reason != "stored" {
				t.Fatalf("retained args = %+v/%+v", got, compactions)
			}
			return 3
		},
		Restore: func(sessionID string, state task_context.RestoredTaskState) {
			restored = state
		},
		Reset: func() { t.Fatal("有 record 时不得 reset") },
	})

	component.RestoreLocked("session-a", location, record, transcript, false, true)

	if restored.TranscriptSeq != 9 || restored.ContextRetainedFrom != 3 ||
		len(restored.ContextCompactions) != 1 || restored.FallbackObjective != "latest" {
		t.Fatalf("restored task = %+v", restored)
	}
}

func TestSessionColdTaskLoadsCompactionsFromNewStore(t *testing.T) {
	loaded := []model.ContextCompaction{{Version: 2, Reason: "new"}}
	var got []model.ContextCompaction
	component := newSessionColdTask(sessionColdTaskDeps{
		LoadCompactions: func(session_runtime.Location, string) ([]model.ContextCompaction, bool, error) {
			return loaded, true, nil
		},
		LatestUser: func([]model.Message) string { return "" },
		RetainedFrom: func([]model.TranscriptEvent, []model.ContextCompaction) int {
			t.Fatal("renumbered transcript 不得推导 retainedFrom")
			return 0
		},
		Restore: func(_ string, state task_context.RestoredTaskState) {
			got = state.ContextCompactions
		},
		Reset: func() {},
	})

	component.RestoreLocked("session-a", session_runtime.Location{}, model.SessionRecord{},
		[]model.TranscriptEvent{{Seq: 2}}, true, true)

	if len(got) != 1 || got[0].Reason != "new" {
		t.Fatalf("compactions = %+v", got)
	}
}

func TestSessionColdTaskResetsWithoutRecord(t *testing.T) {
	reset := false
	component := newSessionColdTask(sessionColdTaskDeps{
		Reset: func() { reset = true },
	})

	component.RestoreLocked("session-a", session_runtime.Location{}, model.SessionRecord{}, nil, false, false)

	if !reset {
		t.Fatal("无 record 时必须 reset task state")
	}
}
