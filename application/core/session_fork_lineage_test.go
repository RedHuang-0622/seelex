package core

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/store"
)

func TestSessionForkLineageRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionForkLineage")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionForkLineage")
	}
	if field.Type != reflect.TypeOf((*sessionForkLineage)(nil)) {
		t.Fatalf("sessionForkLineage 字段类型 = %s", field.Type)
	}
	lineageType := reflect.TypeOf((*sessionForkLineage)(nil))
	if _, ok := lineageType.MethodByName("Load"); !ok {
		t.Fatal("sessionForkLineage 缺少 Load")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_message_edit.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "loadSessionForkRef" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("loadSessionForkRef 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("loadSessionForkRef 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core loadSessionForkRef forwarder must be removed")
	}
}

func TestSessionForkLineageReturnsNilWhenPayloadUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		err     error
	}{
		{name: "error", err: errTestLineage},
		{name: "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lineage := newSessionForkLineage(sessionForkLineageDeps{
				LoadPayload: func(session_runtime.Location, string) ([]byte, error) {
					return test.payload, test.err
				},
			})
			if got := lineage.Load(session_runtime.Location{}, "session-a"); got != nil {
				t.Fatalf("Load = %+v, want nil", got)
			}
		})
	}
}

func TestSessionForkLineageRejectsInvalidRecords(t *testing.T) {
	invalidContext := []byte(`{"forked_from":{`)
	missingParent := mustLineagePayload(t, store.SessionContextRecord{
		ForkedFrom: json.RawMessage(`{"event_seq":3}`),
	})
	invalidRef := []byte(`{"forked_from":[]}`)
	for name, payload := range map[string][]byte{
		"invalid-context": invalidContext,
		"missing-parent":  missingParent,
		"invalid-ref":     invalidRef,
	} {
		t.Run(name, func(t *testing.T) {
			lineage := newSessionForkLineage(sessionForkLineageDeps{
				LoadPayload: func(session_runtime.Location, string) ([]byte, error) {
					return payload, nil
				},
			})
			if got := lineage.Load(session_runtime.Location{}, "session-a"); got != nil {
				t.Fatalf("Load = %+v, want nil", got)
			}
		})
	}
}

func TestSessionForkLineageDecodesValidReference(t *testing.T) {
	want := model.SessionForkRef{
		ParentSessionID: "parent",
		ForkPoint:       model.ForkPoint{EventSeq: 3},
		SourceEventSeq:  4,
		SourceMessageID: "m4",
	}
	payload := mustLineagePayload(t, store.SessionContextRecord{ForkedFrom: mustJSON(t, want)})
	var gotSessionID string
	lineage := newSessionForkLineage(sessionForkLineageDeps{
		LoadPayload: func(_ session_runtime.Location, sessionID string) ([]byte, error) {
			gotSessionID = sessionID
			return payload, nil
		},
	})

	got := lineage.Load(session_runtime.Location{WorkspaceID: "workspace-a"}, "child")

	if gotSessionID != "child" {
		t.Fatalf("session ID = %q", gotSessionID)
	}
	if got == nil || *got != want {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
}

var errTestLineage = &lineageTestError{}

type lineageTestError struct{}

func (*lineageTestError) Error() string { return "read failed" }

func mustLineagePayload(t *testing.T, record store.SessionContextRecord) []byte {
	t.Helper()
	return mustJSON(t, record)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
