package assembly

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestSessionColdActivationRulesAreExtracted(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionColdActivation")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionColdActivation")
	}
	if field.Type != reflect.TypeOf((*sessionColdActivation)(nil)) {
		t.Fatalf("sessionColdActivation 字段类型 = %s", field.Type)
	}
	activationType := reflect.TypeOf((*sessionColdActivation)(nil))
	if _, ok := activationType.MethodByName("PrepareLocked"); !ok {
		t.Fatal("sessionColdActivation 缺少 PrepareLocked")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_history.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
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
			if ok && (selector.Sel.Name == "clearRestoringLocked" ||
				selector.Sel.Name == "applyStoredPermissionTier" ||
				selector.Sel.Name == "SetSessionTitleLocked") {
				t.Errorf("resumeSessionCold 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestSessionColdActivationPreparesActiveSession(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	unit.SetCancel(func() {})
	snapshot := model.Snapshot{Session: model.SessionState{ID: "session-a", Status: SessionStatusRestoring}}
	var calls []string
	activation := newSessionColdActivation(sessionColdActivationDeps{
		ClearRestoring: func(sessionID string) { calls = append(calls, "clear:"+sessionID) },
		ViewIntent:     func(string) uint64 { return 9 },
		TitleFromHistory: func([]contract.EngineMessage) string {
			return "legacy title"
		},
		SetActive: func(sessionID string) { calls = append(calls, "active:"+sessionID) },
		UnitFor:   func(string) *sessioncase.SessionUnit { return unit },
		ApplyStoredTier: func(sessionID, tier string) {
			calls = append(calls, "tier:"+tier)
		},
		SetTitle: func(sessionID string, title model.SessionTitle) {
			calls = append(calls, "title:"+title.Value+":"+title.Source)
		},
	})
	record := model.SessionRecord{Title: model.SessionTitle{Value: "record title", Source: "first_request"}}

	got := activation.PrepareLocked(
		"session-a", 9, &snapshot,
		[]contract.EngineMessage{{Role: "user", Content: "one"}},
		record, true, "auto",
	)

	if !got.MayActivate || got.Name != "record title" {
		t.Fatalf("activation = %+v", got)
	}
	if snapshot.Session.Name != "record title" || snapshot.Session.ID != "session-a" {
		t.Fatalf("snapshot session = %+v", snapshot.Session)
	}
	if unit.CancelFunc() != nil {
		t.Fatal("cold activation must clear cancel")
	}
	want := []string{"clear:session-a", "active:session-a", "tier:auto", "title:record title:legacy_history", "title:record title:first_request"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestSessionColdActivationKeepsViewWhenEpochOrTargetChanged(t *testing.T) {
	tests := []struct {
		name   string
		epoch  uint64
		viewID string
	}{
		{name: "old-epoch", epoch: 8, viewID: "session-a"},
		{name: "view-moved", epoch: 9, viewID: "session-b"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unit, err := sessioncase.NewSessionUnit("session-a")
			if err != nil {
				t.Fatal(err)
			}
			snapshot := model.Snapshot{Session: model.SessionState{ID: test.viewID, Name: "current"}}
			activation := newSessionColdActivation(sessionColdActivationDeps{
				ClearRestoring:   func(string) {},
				ViewIntent:       func(string) uint64 { return 9 },
				TitleFromHistory: func([]contract.EngineMessage) string { return "legacy" },
				SetActive:        func(string) { t.Fatal("superseded load must not activate") },
				UnitFor:          func(string) *sessioncase.SessionUnit { return unit },
				ApplyStoredTier:  func(string, string) {},
				SetTitle:         func(string, model.SessionTitle) {},
			})

			got := activation.PrepareLocked("session-a", test.epoch, &snapshot, nil, model.SessionRecord{}, false, "")

			if got.MayActivate {
				t.Fatal("superseded load must not activate")
			}
			if snapshot.Session.ID != test.viewID || snapshot.Session.Name != "current" {
				t.Fatalf("view snapshot changed: %+v", snapshot.Session)
			}
		})
	}
}
