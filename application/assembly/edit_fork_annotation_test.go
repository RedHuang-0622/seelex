package assembly

import (
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestEditForkAnnotationRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkAnnotation")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkAnnotation")
	}
	if field.Type != reflect.TypeOf((*applicationedit.AnnotationApplication)(nil)) {
		t.Fatalf("editForkAnnotation 字段类型 = %s", field.Type)
	}
	annotationType := reflect.TypeOf((*applicationedit.AnnotationApplication)(nil))
	if _, ok := annotationType.MethodByName("Annotate"); !ok {
		t.Fatal("editForkAnnotation 缺少 Annotate")
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
		if !ok || function.Recv == nil || function.Name.Name != "annotateConversationForEditLocked" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("annotateConversationForEditLocked 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("annotateConversationForEditLocked 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core annotateConversationForEditLocked forwarder must be removed")
	}
}

func TestEditForkAnnotationBackfillsSeqAndMarksEditable(t *testing.T) {
	component := applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
		Input: func(sessionID string) applicationedit.AnnotationInput {
			if sessionID != "session-a" {
				t.Fatalf("input session = %q", sessionID)
			}
			return applicationedit.AnnotationInput{
				Transcript: []model.TranscriptEvent{
					{Role: "user", MessageID: "m1", Seq: 4},
				},
				Floor: 3, HasWindow: true,
			}
		},
		IsUserText: func(message model.Message) bool { return message.Role == "user" },
	})
	messages := []model.Message{
		{ID: "m1", Role: "user", Content: "editable"},
		{ID: "m2", Role: "assistant", Content: "not editable"},
	}

	annotated := component.Annotate("session-a", messages, false)

	if !annotated[0].Editable || annotated[0].EventSeq != 4 || annotated[0].EditDisabledReason != "" {
		t.Fatalf("user annotation = %+v", annotated[0])
	}
	if annotated[1].Editable || annotated[1].EditDisabledReason != "" {
		t.Fatalf("assistant annotation = %+v", annotated[1])
	}
	if messages[0].EventSeq != 0 {
		t.Fatal("Annotate 不得原地修改输入切片")
	}
}

func TestEditForkAnnotationExplainsDisabledReasons(t *testing.T) {
	tests := []struct {
		name    string
		input   applicationedit.AnnotationInput
		running bool
		text    string
	}{
		{name: "running", input: applicationedit.AnnotationInput{HasWindow: true}, running: true, text: editForkRunning},
		{name: "missing", input: applicationedit.AnnotationInput{HasWindow: true}, text: editForkMissingSeq},
		{name: "no-window", input: applicationedit.AnnotationInput{Transcript: []model.TranscriptEvent{{Role: "user", MessageID: "m1", Seq: 4}}}, text: editForkNoWindow},
		{name: "compressed", input: applicationedit.AnnotationInput{Transcript: []model.TranscriptEvent{{Role: "user", MessageID: "m1", Seq: 4}}, Floor: 5, HasWindow: true}, text: editForkCompressed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			component := applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
				Input:      func(string) applicationedit.AnnotationInput { return test.input },
				IsUserText: func(message model.Message) bool { return message.Role == "user" },
			})
			annotated := component.Annotate("session-a", []model.Message{{ID: "m1", Role: "user", Content: "input"}}, test.running)
			if annotated[0].Editable || !strings.Contains(annotated[0].EditDisabledReason, test.text) {
				t.Fatalf("annotation = %+v, want reason %q", annotated[0], test.text)
			}
		})
	}
}
