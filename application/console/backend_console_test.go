package console

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

type backendConsoleFakeApplication struct {
	hub       *application.EventHub
	submitted string
	waited    bool
}

type backendWorkspaceFakeApplication struct {
	snapshot application.Snapshot
	created  struct{ name, root string }
	boundID  string
}

func (fake *backendWorkspaceFakeApplication) Snapshot() application.Snapshot { return fake.snapshot }
func (fake *backendWorkspaceFakeApplication) CreateWorkspace(name, rootPath, _ string) error {
	fake.created.name, fake.created.root = name, rootPath
	return nil
}
func (fake *backendWorkspaceFakeApplication) BindWorkspace(workspaceID string) error {
	fake.boundID = workspaceID
	return nil
}

func newBackendConsoleFakeApplication() *backendConsoleFakeApplication {
	return &backendConsoleFakeApplication{hub: application.NewEventHub()}
}

func (fake *backendConsoleFakeApplication) Submit(_ context.Context, text string) error {
	fake.submitted = text
	return nil
}

func (fake *backendConsoleFakeApplication) WaitForIdle(context.Context) error {
	fake.waited = true
	return nil
}

func (fake *backendConsoleFakeApplication) Subscribe(buffer int) application.Subscription {
	return fake.hub.Subscribe(buffer)
}

func TestBackendEventLoggerLogsToolStagesWithoutPayloadContent(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	now := time.Unix(0, 0)
	logger := NewEventLogger(&output, func() time.Time { return now })
	logger.LogSubmit("do not print this input")

	started, err := json.Marshal(application.Message{ID: "tool-1", Role: "tool", Tool: &application.ToolCall{ID: "bash-1", Name: "bash", Status: "running", Arguments: `{"command":"secret-command"}`}})
	if err != nil {
		t.Fatal(err)
	}
	logger.LogEvent(application.Event{Kind: application.EventMessageAdded, RequestID: "request-1"})
	now = now.Add(12 * time.Millisecond)
	logger.LogEvent(application.Event{Kind: application.EventToolStarted, RequestID: "request-1", Payload: started})
	completed, err := json.Marshal(application.Message{ID: "tool-1", Role: "tool_result", Tool: &application.ToolCall{ID: "bash-1", Name: "bash", Status: "success", Result: "secret-result", Duration: 7 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Millisecond)
	logger.LogEvent(application.Event{Kind: application.EventToolCompleted, RequestID: "request-1", Payload: completed})

	got := output.String()
	if !strings.Contains(got, "stage=tool.started tool=bash") || !strings.Contains(got, "stage=tool.completed tool=bash status=success tool_duration=7ms result_bytes=13") {
		t.Fatalf("tool stage log = %q", got)
	}
	if strings.Contains(got, "secret-command") || strings.Contains(got, "secret-result") || strings.Contains(got, "do not print this input") {
		t.Fatalf("diagnostic log exposed payload content: %q", got)
	}
}

func TestBackendConsolePromptSubmitsAndWaitsForIdle(t *testing.T) {
	t.Parallel()
	fake := newBackendConsoleFakeApplication()
	var output bytes.Buffer
	if err := Run(context.Background(), fake, "inspect", time.Second, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if fake.submitted != "inspect" || !fake.waited {
		t.Fatalf("backend prompt calls = submitted:%q waited:%v", fake.submitted, fake.waited)
	}
	if !strings.Contains(output.String(), "stage=submit input_chars=7") || !strings.Contains(output.String(), "stage=chat.idle") {
		t.Fatalf("backend prompt log = %q", output.String())
	}
}

func TestOpenBackendOutputUsesStdoutWithoutLogPath(t *testing.T) {
	t.Parallel()
	output, closeOutput, err := OpenOutput("")
	if err != nil {
		t.Fatal(err)
	}
	if output != os.Stdout {
		t.Fatalf("default diagnostic output = %T, want os.Stdout", output)
	}
	if err := closeOutput(); err != nil {
		t.Fatal(err)
	}
}

func TestBackendEventLoggerLogsBashStagesWithoutCommandContent(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	now := time.Unix(0, 0)
	logger := NewEventLogger(&output, func() time.Time { return now })
	logger.LogBashEvent(seelebridge.BashDiagnosticEvent{Stage: "bash.process.started", Shell: "bash.exe"})
	now = now.Add(15 * time.Millisecond)
	logger.LogBashEvent(seelebridge.BashDiagnosticEvent{Stage: "bash.process.exited", Shell: "bash.exe", ExitCode: 1})

	got := output.String()
	if !strings.Contains(got, "stage=bash.process.started shell=bash.exe") || !strings.Contains(got, "stage=bash.process.exited shell=bash.exe exit_code=1") {
		t.Fatalf("bash diagnostic log = %q", got)
	}
	if strings.Contains(got, "secret-command") {
		t.Fatalf("bash diagnostic log exposed command content: %q", got)
	}
}

func TestBackendEventLoggerLogsToolHookStagesWithoutPayloadContent(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := NewEventLogger(&output, func() time.Time { return time.Unix(0, 0) })
	logger.LogToolHookEvent(application.ToolHookDiagnosticEvent{Stage: "toolhook.complete.project.done", Name: "bash"})
	if got := output.String(); !strings.Contains(got, "stage=toolhook.complete.project.done tool=bash") || strings.Contains(got, "secret-command") {
		t.Fatalf("tool hook diagnostic log = %q", got)
	}
}

func TestBindBackendProjectUsesExistingWorkspaceOrCreatesOne(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	existing := &backendWorkspaceFakeApplication{snapshot: application.Snapshot{Workspaces: []application.WorkspaceInfo{{ID: "workspace-1", RootPath: root}}}}
	if err := BindProject(existing, root); err != nil {
		t.Fatal(err)
	}
	if existing.boundID != "workspace-1" || existing.created.root != "" {
		t.Fatalf("existing project binding = %+v", existing)
	}

	created := &backendWorkspaceFakeApplication{}
	if err := BindProject(created, root); err != nil {
		t.Fatal(err)
	}
	if created.created.root == "" || created.created.name == "" || created.boundID != "" {
		t.Fatalf("created project binding = %+v", created)
	}
}
