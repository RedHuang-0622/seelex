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

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestSessionColdPublishRulesAreExtracted(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionColdPublish")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionColdPublish")
	}
	if field.Type != reflect.TypeOf((*sessionColdPublish)(nil)) {
		t.Fatalf("sessionColdPublish 字段类型 = %s", field.Type)
	}
	publishType := reflect.TypeOf((*sessionColdPublish)(nil))
	if _, ok := publishType.MethodByName("Publish"); !ok {
		t.Fatal("sessionColdPublish 缺少 Publish")
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
			if ok && (selector.Sel.Name == "attachSessionContextFor" ||
				selector.Sel.Name == "SwitchSessionTasks" ||
				selector.Sel.Name == "ClearSubagentTree" ||
				selector.Sel.Name == "RestoreSubagentAnchors" ||
				selector.Sel.Name == "refreshRuntimeProjectionForSession" ||
				selector.Sel.Name == "touchResident" ||
				selector.Sel.Name == "touchContent") {
				t.Errorf("resumeSessionCold 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestSessionColdPublishActivatesRuntimeAndLRU(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	record := model.SessionRecord{Tasks: []dto.TaskRecord{{Key: "task-1"}}}
	var calls []string
	component := newSessionColdPublish(sessionColdPublishDeps{
		SetGlobalPrompt: func(prompt string) { calls = append(calls, "prompt:"+prompt) },
		AttachContext: func(got session_runtime.Location, sessionID string) error {
			calls = append(calls, "context")
			return nil
		},
		SwitchTasks:       func(sessionID string, tasks []dto.TaskRecord) { calls = append(calls, "tasks") },
		ClearSubagentTree: func() { calls = append(calls, "clear-tree") },
		RestoreAnchors:    func(sessionID string) { calls = append(calls, "anchors") },
		RefreshRuntime:    func(sessionID string) { calls = append(calls, "runtime") },
		PublishSnapshot: func(revision uint64, sessionID string) {
			calls = append(calls, "snapshot")
			if revision != 11 {
				t.Fatalf("revision = %d", revision)
			}
		},
		PublishRuntime: func() { calls = append(calls, "publish-runtime") },
		RefreshCatalog: func() { calls = append(calls, "catalog") },
		TouchResident:  func(sessionID string) { calls = append(calls, "resident") },
		TouchContent:   func(sessionID string) { calls = append(calls, "content") },
	})

	if err := component.Publish("session-a", location, record, true, 11, "prompt"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	want := []string{
		"prompt:prompt", "context", "tasks", "clear-tree", "anchors",
		"runtime", "snapshot", "publish-runtime", "catalog", "resident", "content",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSessionColdPublishNonActiveStillAttachesAndTouches(t *testing.T) {
	var calls []string
	component := newSessionColdPublish(sessionColdPublishDeps{
		SetGlobalPrompt: func(string) { t.Fatal("非 active 不得写全局 prompt") },
		AttachContext: func(session_runtime.Location, string) error {
			calls = append(calls, "context")
			return nil
		},
		SwitchTasks:       func(string, []dto.TaskRecord) { t.Fatal("非 active 不得切 task registry") },
		ClearSubagentTree: func() { t.Fatal("非 active 不得清子代理树") },
		RestoreAnchors:    func(string) { t.Fatal("非 active 不得恢复 anchors") },
		RefreshRuntime:    func(string) { t.Fatal("非 active 不得刷新共享 runtime projection") },
		PublishSnapshot:   func(uint64, string) { t.Fatal("非 active 不得发布 snapshot") },
		PublishRuntime:    func() { t.Fatal("非 active 不得发布 runtime") },
		RefreshCatalog:    func() { calls = append(calls, "catalog") },
		TouchResident:     func(string) { calls = append(calls, "resident") },
		TouchContent:      func(string) { calls = append(calls, "content") },
	})

	if err := component.Publish("session-a", session_runtime.Location{}, model.SessionRecord{}, false, 0, "prompt"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := strings.Join(calls, ","); got != "context,catalog,resident,content" {
		t.Fatalf("calls = %q", got)
	}
}

func TestSessionColdPublishStopsOnContextError(t *testing.T) {
	attachErr := errors.New("attach failed")
	component := newSessionColdPublish(sessionColdPublishDeps{
		SetGlobalPrompt:   func(string) {},
		AttachContext:     func(session_runtime.Location, string) error { return attachErr },
		SwitchTasks:       func(string, []dto.TaskRecord) { t.Fatal("context 失败后不得切 task") },
		ClearSubagentTree: func() { t.Fatal("context 失败后不得清树") },
		RestoreAnchors:    func(string) { t.Fatal("context 失败后不得恢复 anchors") },
		RefreshRuntime:    func(string) { t.Fatal("context 失败后不得刷新 runtime") },
		PublishSnapshot:   func(uint64, string) { t.Fatal("context 失败后不得发布 snapshot") },
		PublishRuntime:    func() { t.Fatal("context 失败后不得发布 runtime") },
		RefreshCatalog:    func() { t.Fatal("context 失败后不得刷新 catalog") },
		TouchResident:     func(string) { t.Fatal("context 失败后不得 touch resident") },
		TouchContent:      func(string) { t.Fatal("context 失败后不得 touch content") },
	})

	err := component.Publish("session-a", session_runtime.Location{}, model.SessionRecord{}, true, 1, "prompt")

	if !errors.Is(err, attachErr) {
		t.Fatalf("Publish error = %v, want attach error", err)
	}
}
