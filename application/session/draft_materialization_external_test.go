package session_test

import (
	"errors"
	"strings"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestDraftMaterializationSkipsNonDraft(t *testing.T) {
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

func TestDraftMaterializationRequiresPreassignedID(t *testing.T) {
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

func TestDraftMaterializationStopsWhenWorkspaceBindFails(t *testing.T) {
	bindErr := errors.New("root denied")
	component := applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			return applicationsession.DraftMaterializationSource{
				Draft: true, DraftID: "draft-a",
				Workspace: &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"},
			}
		},
		BindWorkspace: func(*model.WorkspaceInfo) error { return bindErr },
		OpenEngine: func(string, *model.WorkspaceInfo) (string, error) {
			t.Fatal("workspace 绑定失败后不得打开引擎")
			return "", nil
		},
	})

	if err := component.Materialize("input"); !errors.Is(err, bindErr) {
		t.Fatalf("Materialize error = %v, want bind error", err)
	}
}

func TestDraftMaterializationOrdersFirstInput(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "/repo"}
	var calls []string
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
		OpenEngine: func(draftID string, _ *model.WorkspaceInfo) (string, error) {
			calls = append(calls, "open:"+draftID)
			return "session-a", nil
		},
		Title: func(input string) model.SessionTitle {
			calls = append(calls, "title:"+input)
			return model.SessionTitle{Value: input}
		},
		Commit: func(id string, title model.SessionTitle) uint64 {
			calls = append(calls, "commit:"+id+":"+title.Value)
			return 5
		},
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot")
			if revision != 5 || sessionID != "session-a" {
				t.Fatalf("snapshot publication = %d/%q, want 5/session-a", revision, sessionID)
			}
		},
		RefreshCatalog:  func() { calls = append(calls, "catalog") },
		FlushToolRecord: func(string) { calls = append(calls, "flush") },
		TouchResident:   func(string) { calls = append(calls, "resident") },
	})

	if err := component.Materialize("hello"); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	want := "snapshot,bind,open:draft-a,title:hello,commit:session-a:hello,runtime,snapshot,catalog,flush,resident"
	if got := strings.Join(calls, ","); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}
