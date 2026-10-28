package session_test

import (
	"sync"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type attachTestLock struct {
	mu    sync.Mutex
	calls *[]string
}

func (lock *attachTestLock) Lock() {
	*lock.calls = append(*lock.calls, "lock")
	lock.mu.Lock()
}

func (lock *attachTestLock) Unlock() {
	*lock.calls = append(*lock.calls, "unlock")
	lock.mu.Unlock()
}

type attachTestPreparation struct {
	calls     *[]string
	running   bool
	workspace *model.WorkspaceInfo
}

func (port *attachTestPreparation) EnsureContent(string) error {
	*port.calls = append(*port.calls, "content")
	return nil
}

func (port *attachTestPreparation) TargetRunning(string) bool {
	*port.calls = append(*port.calls, "running")
	return port.running
}

func (port *attachTestPreparation) Workspace(string) (*model.WorkspaceInfo, bool) {
	*port.calls = append(*port.calls, "workspace")
	return port.workspace, port.workspace != nil
}

func (port *attachTestPreparation) BindProjectRootIfSafe(string, string) bool {
	*port.calls = append(*port.calls, "bind-root")
	return true
}

func (port *attachTestPreparation) SetWorkspaceWriteScope(string) {
	*port.calls = append(*port.calls, "write-scope")
}

func (port *attachTestPreparation) SetRuntimeWorkspace(string, string) {
	*port.calls = append(*port.calls, "runtime-workspace")
}

func (port *attachTestPreparation) SwitchTasks(string) {
	*port.calls = append(*port.calls, "tasks")
}

func (port *attachTestPreparation) RestoreSubagentAnchors(string) {
	*port.calls = append(*port.calls, "anchors")
}

func (port *attachTestPreparation) WorkspaceProjection() (switchworkspace.Projection, bool) {
	*port.calls = append(*port.calls, "projection")
	return switchworkspace.Projection{}, port.workspace != nil
}

func (port *attachTestPreparation) StoredPermissionTier(string) string {
	*port.calls = append(*port.calls, "tier-read")
	return "workspace"
}

type attachTestCommit struct {
	calls    *[]string
	unit     *sessioncase.SessionUnit
	prompt   string
	routed   bool
	revision uint64
}

func (port *attachTestCommit) SessionTitle(string) string {
	*port.calls = append(*port.calls, "title")
	return "Session A"
}

func (port *attachTestCommit) SetActive(string) {
	*port.calls = append(*port.calls, "active")
}

func (port *attachTestCommit) Unit(string) *sessioncase.SessionUnit {
	*port.calls = append(*port.calls, "unit")
	return port.unit
}

func (port *attachTestCommit) ApplyStoredPermissionTier(string, string) {
	*port.calls = append(*port.calls, "tier-apply")
}

func (port *attachTestCommit) SetSessionChat(string, model.ChatState) {
	*port.calls = append(*port.calls, "chat")
}

func (port *attachTestCommit) MirrorActiveView() {
	*port.calls = append(*port.calls, "mirror-view")
}

func (port *attachTestCommit) VisibleTask(string) *model.TaskState {
	*port.calls = append(*port.calls, "task")
	return &model.TaskState{RequestID: "turn-a"}
}

func (port *attachTestCommit) ActivePlan(string) *model.PlanState {
	*port.calls = append(*port.calls, "plan")
	return &model.PlanState{Name: "plan-a"}
}

func (port *attachTestCommit) MirrorPlan(string, *model.PlanState) {
	*port.calls = append(*port.calls, "mirror-plan")
}

func (port *attachTestCommit) MirrorApprovals(string) {
	*port.calls = append(*port.calls, "approvals")
}

func (port *attachTestCommit) ApplyWorkspace(*model.WorkspaceInfo, switchworkspace.Projection) {
	*port.calls = append(*port.calls, "apply-workspace")
}

func (port *attachTestCommit) SystemPrompt() string {
	*port.calls = append(*port.calls, "system-prompt")
	return port.prompt
}

func (port *attachTestCommit) PromptRoutesBySession() bool {
	return port.routed
}

func (port *attachTestCommit) BumpRevision() uint64 {
	*port.calls = append(*port.calls, "bump")
	port.revision++
	return port.revision
}

type attachTestPublish struct {
	calls *[]string
}

func (port *attachTestPublish) WriteSessionPrompt(string) {
	*port.calls = append(*port.calls, "prompt-before")
}

func (port *attachTestPublish) WriteGlobalPrompt(string) {
	*port.calls = append(*port.calls, "prompt-global")
}

func (port *attachTestPublish) RefreshRuntime(string) {
	*port.calls = append(*port.calls, "refresh")
}

func (port *attachTestPublish) PublishSnapshot(string, uint64) {
	*port.calls = append(*port.calls, "snapshot")
}

func (port *attachTestPublish) PublishRuntimeProjections() {
	*port.calls = append(*port.calls, "runtime")
}

func (port *attachTestPublish) RefreshCatalog() {
	*port.calls = append(*port.calls, "catalog")
}

func (port *attachTestPublish) TouchResident(string) {
	*port.calls = append(*port.calls, "touch-resident")
}

func (port *attachTestPublish) TouchContent(string) {
	*port.calls = append(*port.calls, "touch-content")
}

func TestSessionAttachCommitsOneCurrentSessionProjection(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	snapshot := &model.Snapshot{}
	preparation := &attachTestPreparation{
		calls:     &calls,
		workspace: &model.WorkspaceInfo{ID: "workspace-a", RootPath: "G:\\work"},
	}
	commit := &attachTestCommit{calls: &calls, unit: unit, prompt: "system"}
	publish := &attachTestPublish{calls: &calls}
	application := applicationsession.NewAttach(applicationsession.AttachDeps{
		Lock:        &attachTestLock{calls: &calls},
		Preparation: preparation,
		Commit:      commit,
		Publish:     publish,
	})

	revision, err := application.Attach("session-a", snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if revision != 1 || snapshot.Revision != 1 {
		t.Fatalf("revision = %d snapshot=%d, want 1", revision, snapshot.Revision)
	}
	if snapshot.Session.ID != "session-a" || snapshot.Session.Name != "Session A" {
		t.Fatalf("session projection = %+v", snapshot.Session)
	}
	if snapshot.Task == nil || snapshot.Task.RequestID != "turn-a" {
		t.Fatalf("task projection = %+v", snapshot.Task)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Name != "plan-a" {
		t.Fatalf("plan projection = %+v", snapshot.Runtime.Plan)
	}
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "workspace-a" {
		t.Fatalf("workspace projection = %+v", snapshot.CurrentWorkspace)
	}
	want := "content,running,prompt-before,workspace,bind-root,write-scope,runtime-workspace,tasks,anchors,projection,tier-read,lock,title,active,unit,tier-apply,chat,mirror-view,task,plan,mirror-plan,approvals,apply-workspace,system-prompt,bump,unlock,prompt-global,refresh,snapshot,runtime,catalog,touch-resident,touch-content"
	if got := joinAttachCalls(calls); got != want {
		t.Fatalf("calls =\n%s\nwant=\n%s", got, want)
	}
}

func TestSessionAttachSkipsPromptWritesForRunningTarget(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	preparation := &attachTestPreparation{calls: &calls, running: true}
	application := applicationsession.NewAttach(applicationsession.AttachDeps{
		Lock:        &attachTestLock{calls: &calls},
		Preparation: preparation,
		Commit:      &attachTestCommit{calls: &calls, unit: unit, routed: true},
		Publish:     &attachTestPublish{calls: &calls},
	})

	if _, err := application.Attach("session-a", &model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if call == "prompt-before" || call == "prompt-global" {
			t.Fatalf("running session touched prompt lock: %v", calls)
		}
	}
}

func joinAttachCalls(calls []string) string {
	out := ""
	for index, call := range calls {
		if index > 0 {
			out += ","
		}
		out += call
	}
	return out
}
