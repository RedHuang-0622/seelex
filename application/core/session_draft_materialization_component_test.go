package core

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestSessionDraftMaterializationRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionDraftMaterialization")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionDraftMaterialization")
	}
	if field.Type != reflect.TypeOf((*applicationsession.DraftMaterializationApplication)(nil)) {
		t.Fatalf("sessionDraftMaterialization 字段类型 = %s", field.Type)
	}
	componentType := reflect.TypeOf((*applicationsession.DraftMaterializationApplication)(nil))
	if _, ok := componentType.MethodByName("Materialize"); !ok {
		t.Fatal("sessionDraftMaterialization 缺少 Materialize")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_draft.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "materializeDraftSession" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("materializeDraftSession 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("materializeDraftSession 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core materializeDraftSession forwarder must be removed")
	}
}

func TestSessionDraftMaterializationSkipsNonDraft(t *testing.T) {
	component := applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			return applicationsession.DraftMaterializationSource{}
		},
		BindWorkspace: func(*model.WorkspaceInfo) error {
			t.Fatal("非草稿不得绑定工作区")
			return nil
		},
		OpenEngine: func(string, *model.WorkspaceInfo) (string, error) {
			t.Fatal("非草稿不得打开引擎")
			return "", nil
		},
	})

	if err := component.Materialize("input"); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
}

func TestSessionDraftMaterializationRequiresPreassignedID(t *testing.T) {
	component := applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			return applicationsession.DraftMaterializationSource{Draft: true}
		},
	})

	err := component.Materialize("input")

	if err == nil || !strings.Contains(err.Error(), "pre-assigned session ID") {
		t.Fatalf("Materialize error = %v, want missing draft ID", err)
	}
}

func TestSessionDraftMaterializationStopsWhenWorkspaceBindFails(t *testing.T) {
	bindErr := errors.New("root denied")
	component := applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			return applicationsession.DraftMaterializationSource{
				Draft:   true,
				DraftID: "draft-a",
				Workspace: &model.WorkspaceInfo{
					ID: "workspace-a", RootPath: "C:/workspace-a",
				},
			}
		},
		BindWorkspace: func(*model.WorkspaceInfo) error {
			return bindErr
		},
		OpenEngine: func(string, *model.WorkspaceInfo) (string, error) {
			t.Fatal("workspace 绑定失败后不得打开引擎")
			return "", nil
		},
	})

	err := component.Materialize("input")

	if !errors.Is(err, bindErr) {
		t.Fatalf("Materialize error = %v, want bind error", err)
	}
}

func TestSessionDraftMaterializationPreservesCommitAndPublicationOrder(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"}
	var calls []string
	var committedTitle model.SessionTitle
	component := applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			calls = append(calls, "snapshot")
			return applicationsession.DraftMaterializationSource{
				Draft: true, DraftID: "draft-a", Workspace: workspace,
			}
		},
		BindWorkspace: func(got *model.WorkspaceInfo) error {
			calls = append(calls, "bind")
			if got != workspace {
				t.Fatalf("workspace = %+v, want %+v", got, workspace)
			}
			return nil
		},
		OpenEngine: func(sessionID string, got *model.WorkspaceInfo) (string, error) {
			calls = append(calls, "open:"+sessionID)
			if got != workspace {
				t.Fatalf("open workspace = %+v, want %+v", got, workspace)
			}
			return "session-real", nil
		},
		Title: func(firstQuestion string) model.SessionTitle {
			return model.SessionTitle{Value: firstQuestion, Source: "first_request"}
		},
		Commit: func(newID string, title model.SessionTitle) uint64 {
			calls = append(calls, "commit:"+newID)
			committedTitle = title
			return 17
		},
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot-event:"+sessionID)
			if revision != 17 {
				t.Fatalf("revision = %d, want 17", revision)
			}
		},
		RefreshCatalog:  func() { calls = append(calls, "catalog") },
		FlushToolRecord: func(sessionID string) { calls = append(calls, "flush:"+sessionID) },
		TouchResident:   func(sessionID string) { calls = append(calls, "resident:"+sessionID) },
	})

	if err := component.Materialize("  first input  "); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	want := []string{
		"snapshot",
		"bind",
		"open:draft-a",
		"commit:session-real",
		"runtime",
		"snapshot-event:session-real",
		"catalog",
		"flush:session-real",
		"resident:session-real",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if committedTitle.Source != "first_request" || committedTitle.Value == "" {
		t.Fatalf("committed title = %+v, want first_request title", committedTitle)
	}
}
