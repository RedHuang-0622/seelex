package cold_test

import (
	"errors"
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"strings"
	"testing"
)

func TestSessionColdPublishActivatesRuntimeAndLRU(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	record := model.SessionRecord{Tasks: []dto.TaskRecord{{Key: "task-1"}}}
	var calls []string
	component := cold.NewPublish(cold.PublishDeps{
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
	component := cold.NewPublish(cold.PublishDeps{
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
	component := cold.NewPublish(cold.PublishDeps{
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
