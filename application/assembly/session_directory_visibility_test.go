package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// 会话目录的可见性判据（用户报告 2026-07）：
//  1. 刚发起的「工作区会话」必须一开始就归在它的工作区分组里，不能先掉进
//     「未关联会话」；
//  2. 首轮在跑的会话必须立刻在会话树里有一行（目录枚举只认已落盘会话，而会话
//     只在回合收尾落盘——补行判据见 Snapshot 的 withLiveSessionRowsLocked）；
//  3. 同上一条对**后台**会话成立：定时任务新建的会话不等落盘就要能看见。

// TestWorkspaceDraftShowsItsWorkspaceInDirectory 钉住第 1 条：草稿的工作区绑定
// 是视图级事实（存储绑定要等物化才写），目录投影必须带上它。
func TestWorkspaceDraftShowsItsWorkspaceInDirectory(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	if err := frontendOfTestService(service).CreateWorkspace("project", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := frontendOfTestService(service).BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := frontendOfTestService(service).BindWorkspace("project-1"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	draftID := snapshot.Session.ID
	if draftID == "" || !snapshot.Session.Draft {
		t.Fatalf("workspace draft = %+v", snapshot.Session)
	}
	if got := snapshot.SessionWorkspaces[draftID]; got != "project-1" {
		t.Fatalf("draft binding in directory projection = %q（want project-1），all=%v", got, snapshot.SessionWorkspaces)
	}
	listed := false
	for _, row := range snapshot.Sessions {
		if row.ID == draftID {
			listed = true
			if row.Status != SessionStatusDraft {
				t.Fatalf("draft row status = %q", row.Status)
			}
		}
	}
	if !listed {
		t.Fatalf("draft row missing from directory: %v", snapshot.Sessions)
	}
}

// TestTaskDraftStaysUnboundInDirectory 钉住反向判据：没有工作区的「任务会话」
// 草稿不得凭空获得一个工作区分组（旧口径的"真正未关联"不能被上面的修复放宽）。
func TestTaskDraftStaysUnboundInDirectory(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	if err := frontendOfTestService(service).CreateWorkspace("project", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	// 先在一个工作区会话里，再 /new 进「任务会话」：项目绑定必须被清掉。
	if err := frontendOfTestService(service).BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.CurrentWorkspace != nil {
		t.Fatalf("task draft must not inherit workspace: %+v", snapshot.CurrentWorkspace)
	}
	if got := snapshot.SessionWorkspaces[snapshot.Session.ID]; got != "" {
		t.Fatalf("task draft binding = %q（want 空）", got)
	}
}

// TestRunningSessionVisibleInDirectory 钉住第 2 条：首轮运行中（尚未落盘）的
// 会话在目录里必须有一行 running，且带自己的标题。
func TestRunningSessionVisibleInDirectory(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{}, started: make(chan struct{}), release: make(chan struct{}),
	}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	if err := frontendOfTestService(service).CreateWorkspace("project", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := frontendOfTestService(service).BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := frontendOfTestService(service).BindWorkspace("project-1"); err != nil {
		t.Fatal(err)
	}
	draftID := service.Snapshot().Session.ID
	if err := service.Submit(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}
	snapshot := service.Snapshot()
	row, ok := findSessionRow(snapshot.Sessions, draftID)
	close(engine.release)
	_ = frontendOfTestService(service).WaitForIdle(context.Background())
	if !ok {
		t.Fatalf("running session %s missing from directory: %v", draftID, snapshot.Sessions)
	}
	if row.Status != SessionStatusRunning {
		t.Fatalf("live row status = %q（want running）", row.Status)
	}
	if row.Name != "first question" {
		t.Fatalf("live row title = %q（want first question）", row.Name)
	}
	if got := snapshot.SessionWorkspaces[draftID]; got != "project-1" {
		t.Fatalf("running session binding = %q（want project-1）", got)
	}
}

// TestScheduledSessionVisibleBeforePersist 钉住第 3 条：定时任务新建的后台会话
// 不等落盘就要在目录里出现（用户口径："看不到定时任务的会话条目"）。
func TestScheduledSessionVisibleBeforePersist(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{}, started: make(chan struct{}), release: make(chan struct{}),
	}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	if err := frontendOfTestService(service).CreateWorkspace("project", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	workspaceID := "project-1"
	sessionID, err := frontendOfTestService(service).StartScheduledSession(context.Background(), assemble.ScheduledTaskSpec{
		Name:        "scheduled",
		Kind:        dto.ScheduledTaskPrompt,
		Prompt:      "scheduled prompt",
		Enabled:     true,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("StartScheduledSession: %v", err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled chat did not start")
	}
	snapshot := service.Snapshot()
	row, ok := findSessionRow(snapshot.Sessions, sessionID)
	close(engine.release)
	_ = frontendOfTestService(service).WaitForIdle(context.Background())
	if !ok {
		t.Fatalf("scheduled session %s missing from directory: %v", sessionID, snapshot.Sessions)
	}
	if row.Status != SessionStatusRunning {
		t.Fatalf("scheduled live row status = %q（want running）", row.Status)
	}
	if row.Name != "scheduled prompt" {
		t.Fatalf("scheduled live row title = %q", row.Name)
	}
	if got := snapshot.SessionWorkspaces[sessionID]; got != workspaceID {
		t.Fatalf("scheduled session binding = %q（want %q）", got, workspaceID)
	}
}

func findSessionRow(rows []model.SessionInfo, sessionID string) (model.SessionInfo, bool) {
	for _, row := range rows {
		if row.ID == sessionID {
			return row, true
		}
	}
	return model.SessionInfo{}, false
}
