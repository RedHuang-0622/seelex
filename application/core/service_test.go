package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

func TestNewTestServiceCleansUpCatalogWorker(t *testing.T) {
	var service *Service
	t.Run("fixture", func(t *testing.T) {
		service = newTestService(t, &fakeEngine{})
	})
	select {
	case <-service.components.sessions.CatalogRefreshDone():
	case <-time.After(time.Second):
		t.Fatal("test fixture did not stop the session catalog worker")
	}
}

func TestSessionCatalogAllowsDuplicateNamesWithDistinctIDs(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{
			"project-1": {
				{ID: "session-a", UpdatedAt: updatedAt},
				{ID: "session-b", UpdatedAt: updatedAt},
			},
		},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {
				"session-a": {{Role: "user", Content: "same question"}},
				"session-b": {{Role: "user", Content: "same question"}},
			},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "project", RootPath: t.TempDir()}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	catalog := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 2 }).Sessions
	if len(catalog) != 2 {
		t.Fatalf("session count = %d, want 2", len(catalog))
	}
	if catalog[0].Name != "same question" || catalog[1].Name != "same question" {
		t.Fatalf("duplicate display names were not preserved: %#v", catalog)
	}
	if catalog[0].ID == catalog[1].ID {
		t.Fatalf("session IDs must remain distinct: %#v", catalog)
	}
}

func TestSessionTitleUsesFirstUserQuestion(t *testing.T) {
	history := []EngineMessage{
		{Role: "system", Content: "system"},
		{Role: "assistant", Content: "assistant"},
		{Role: "user", Content: wrapModelInput("\n  first   question  \nsecond line", "model context")},
	}
	if got := session_runtime.SessionTitleFromHistory(history, displayUserInput); got != "first question" {
		t.Fatalf("session title = %q, want %q", got, "first question")
	}
	long := strings.Repeat("界", 60)
	got := session_runtime.SessionTitle(long)
	if len([]rune(got)) != 48 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long title was not rune-truncated: %q (%d runes)", got, len([]rune(got)))
	}
}

func TestCurrentSessionNameUsesFirstQuestion(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.Submit(context.Background(), "  first live question  "); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Session.Name; got != "first live question" {
		t.Fatalf("current session name = %q", got)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkingHistoryReleasesOnlyAfterSuccessfulPersistence(t *testing.T) {
	tests := []struct {
		name         string
		sessions     SessionPort
		wantReleases int
	}{
		{name: "success", sessions: fakeSessions{}, wantReleases: 1},
		{name: "failure", sessions: persistenceFailingSessions{}, wantReleases: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := &fakeEngine{}
			service := mustNew(t, Dependencies{
				Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
				Skills: fakeSkills{}, Sessions: test.sessions,
			})
			defer service.Shutdown()
			if err := service.Submit(context.Background(), "persist before release"); err != nil {
				t.Fatal(err)
			}
			if err := service.WaitForIdle(context.Background()); err != nil {
				t.Fatal(err)
			}
			engine.mu.Lock()
			releases := engine.releaseCalls
			engine.mu.Unlock()
			if releases != test.wantReleases {
				t.Fatalf("working history releases = %d, want %d", releases, test.wantReleases)
			}
		})
	}
}

func TestBeginNewSessionIsLazyAndFirstQuestionMaterializesIt(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "old question"}}}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	startsBeforeSubmit := engine.starts
	clearedForDraft := engine.cleared
	engine.mu.Unlock()
	if startsBeforeSubmit != 0 || !clearedForDraft {
		t.Fatalf("draft engine state: starts=%d cleared=%v", startsBeforeSubmit, clearedForDraft)
	}
	snapshot := service.Snapshot()
	if !snapshot.Session.Draft || snapshot.Session.ID == "" || snapshot.Session.Name != "新会话" {
		t.Fatalf("draft session = %+v", snapshot.Session)
	}
	draftID := snapshot.Session.ID
	if len(snapshot.Conversation) != 0 || snapshot.Runtime.Plan != nil {
		t.Fatalf("draft must clear conversation and plan: %+v", snapshot)
	}
	sessions.mu.Lock()
	if len(sessions.savedIDs) != 1 || sessions.savedIDs[0] != "session-1" {
		t.Fatalf("saved sessions after repeated draft clicks = %v", sessions.savedIDs)
	}
	sessions.mu.Unlock()

	if err := service.Submit(context.Background(), "first lazy question"); err != nil {
		t.Fatal(err)
	}
	snapshot = service.Snapshot()
	if snapshot.Session.Draft || snapshot.Session.ID != draftID || snapshot.Session.Name != "first lazy question" {
		t.Fatalf("materialized session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	loaded := engine.loadedSessions[draftID]
	sessionID := engine.sessionID
	engine.mu.Unlock()
	if !loaded || sessionID != draftID {
		t.Fatalf("materialize must activate the pre-assigned draft ID: loaded=%v sessionID=%q draftID=%q", loaded, sessionID, draftID)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLazySessionInheritsProjectOnlyWhenMaterialized(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	draftID := service.Snapshot().Session.ID
	if draftID == "" {
		t.Fatal("draft session must hold a pre-assigned session ID")
	}
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, exists := workspaces.bindings[""]; exists {
		t.Fatalf("draft session created an empty-ID binding: %v", workspaces.bindings)
	}
	if _, exists := workspaces.bindings[draftID]; exists {
		t.Fatalf("draft session was bound before first request: %v", workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "project question"); err != nil {
		t.Fatal(err)
	}
	if got := workspaces.bindings[draftID]; got != "project-1" {
		t.Fatalf("materialized session workspace = %q, want project-1; bindings=%v", got, workspaces.bindings)
	}
	if sessions.Workspace() != "project-1" || runtime.projectRoot != root {
		t.Fatalf("materialized project scope: workspace=%q root=%q", sessions.Workspace(), runtime.projectRoot)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitialLazySessionIsDraftAndFirstSubmitMaterializes(t *testing.T) {
	engine := &fakeEngine{lazyStart: true}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	// Startup with a lazy engine must present an unmaterialized draft so the
	// first submission creates a real session under the pre-assigned draft ID.
	initial := service.Snapshot()
	if !initial.Session.Draft || initial.Session.ID == "" {
		t.Fatalf("initial lazy session = %+v, want Draft=true with pre-assigned ID", initial.Session)
	}
	draftID := initial.Session.ID

	if err := service.Submit(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.Draft || snapshot.Session.ID != draftID {
		t.Fatalf("materialized session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	loaded := engine.loadedSessions[draftID]
	sessionID := engine.sessionID
	engine.mu.Unlock()
	if !loaded || sessionID != draftID {
		t.Fatalf("materialize must activate the pre-assigned draft ID: loaded=%v sessionID=%q", loaded, sessionID)
	}
}

func TestResumeSessionLeavesLazyDraft(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"": {{ID: "saved", UpdatedAt: time.Now()}}},
		histories: map[string]map[string][]EngineMessage{
			"": {"saved": {{Role: "user", Content: "saved question"}, {Role: "assistant", Content: "saved answer"}}},
		},
	}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		for _, item := range snapshot.Sessions {
			if item.ID == "saved" {
				return true
			}
		}
		return false
	})
	if snapshot.Session.Draft || snapshot.Session.ID != "saved" || snapshot.Session.Name != "saved question" {
		t.Fatalf("resumed session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	starts := engine.starts
	engine.mu.Unlock()
	if starts != 0 {
		t.Fatalf("resume from draft called StartSession %d times", starts)
	}
}

// TestNewTaskSessionIsTrulyUnbound 未关联工作区的会话必须真正未关联：
// 新建「任务会话」（/new → BeginNewSession）不得继承上一个会话的项目绑定，
// 否则上一个对话的项目信息（项目地址、资源管理器文件树/提交记录、工作台
// 投影）会污染新会话。
func TestNewTaskSessionIsTrulyUnbound(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if runtime.projectRoot != root || sessions.Workspace() != "project-1" || workspaces.bindings["session-1"] != "project-1" {
		t.Fatalf("create project did not bind all scope state: root=%q sessionStore=%q bindings=%v", runtime.projectRoot, sessions.Workspace(), workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "/new"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.CurrentWorkspace != nil {
		t.Fatalf("/new must clear project binding: %+v", snapshot.CurrentWorkspace)
	}
	if !snapshot.Session.Draft || snapshot.Session.ID == "" {
		t.Fatalf("/new must remain an unmaterialized draft with pre-assigned ID: %+v", snapshot.Session)
	}
	draftID := snapshot.Session.ID
	if workspaces.bindings["session-1"] != "project-1" {
		t.Fatalf("previous session binding must be kept: %v", workspaces.bindings)
	}
	if _, exists := workspaces.bindings[draftID]; exists {
		t.Fatalf("draft session bound before first request: %v", workspaces.bindings)
	}
	if runtime.projectRoot != "" || sessions.Workspace() != "" {
		t.Fatalf("draft must unbind project scope: root=%q sessionStore=%q", runtime.projectRoot, sessions.Workspace())
	}
	if err := service.Submit(context.Background(), "first unbound question"); err != nil {
		t.Fatal(err)
	}
	snapshot = service.Snapshot()
	if got := workspaces.bindings[draftID]; got != "" {
		t.Fatalf("materialized task session must stay unbound: %v", workspaces.bindings)
	}
	if sessions.Workspace() != "" || runtime.projectRoot != "" {
		t.Fatalf("materialized task session scope: sessionStore=%q root=%q", sessions.Workspace(), runtime.projectRoot)
	}
	if snapshot.CurrentWorkspace != nil || snapshot.SessionWorkspaces[draftID] != "" {
		t.Fatalf("materialized snapshot = %+v", snapshot)
	}
	if snapshot.Session.Name != "first unbound question" {
		t.Fatalf("materialized session name = %q", snapshot.Session.Name)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestWorkspaceSessionBindsDraftBeforeMaterialization 覆盖 GUI「工作区会话」
// 的边界顺序：先 BeginNewSession 进入未关联草稿，再在草稿上 BindWorkspace；
// 首次提交物化后会话必须绑定到所选工作区（而不是丢失绑定）。
func TestWorkspaceSessionBindsDraftBeforeMaterialization(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.BindWorkspace("project-1"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if !snapshot.Session.Draft || snapshot.Session.ID == "" {
		t.Fatalf("workspace session must stay a draft with pre-assigned ID: %+v", snapshot.Session)
	}
	draftID := snapshot.Session.ID
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "project-1" {
		t.Fatalf("draft must be bound to the picked workspace: %+v", snapshot.CurrentWorkspace)
	}
	if _, exists := workspaces.bindings[draftID]; exists {
		t.Fatalf("draft session bound before first request: %v", workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "first project question"); err != nil {
		t.Fatal(err)
	}
	snapshot = service.Snapshot()
	if workspaces.bindings[draftID] != "project-1" || sessions.Workspace() != "project-1" {
		t.Fatalf("materialized workspace session did not bind: bindings=%v sessionStore=%q", workspaces.bindings, sessions.Workspace())
	}
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "project-1" || snapshot.SessionWorkspaces[draftID] != "project-1" {
		t.Fatalf("materialized snapshot = %+v", snapshot)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestResumeRestoresProjectScope(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	root := t.TempDir()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "project", RootPath: root}
	workspaces.BindSession("saved", "project-1")
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if runtime.projectRoot != root || sessions.Workspace() != "project-1" {
		t.Fatalf("resume did not restore project scope: root=%q store=%q", runtime.projectRoot, sessions.Workspace())
	}
}

func TestNewHydratesPersistedWorkspaceSessions(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{catalog: map[string][]SessionInfo{
		"project-1": {{ID: "saved-1", UpdatedAt: time.Unix(2, 0), TokenCount: 10}},
		"project-2": {{ID: "saved-2", UpdatedAt: time.Unix(3, 0), TokenCount: 20}},
	}}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved-1", "project-1")
	workspaces.BindSession("saved-2", "project-2")

	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 2 })
	if len(snapshot.Workspaces) != 2 || len(snapshot.Sessions) != 2 {
		t.Fatalf("hydrated snapshot workspaces=%v sessions=%v", snapshot.Workspaces, snapshot.Sessions)
	}
	if snapshot.Sessions[0].ID != "saved-2" || snapshot.SessionWorkspaces["saved-1"] != "project-1" {
		t.Fatalf("hydrated session catalog=%v bindings=%v", snapshot.Sessions, snapshot.SessionWorkspaces)
	}
}

func TestResumeReadsSessionFromItsPersistedWorkspace(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{
		workspace: "project-2",
		catalog:   map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": {{Role: "assistant", Content: "project one history"}}},
		},
	}
	workspaces := newFakeWorkspace()
	root := t.TempDir()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: root}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-1")

	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if sessions.LoadedWorkspace() != "project-1" || sessions.Workspace() != "project-1" {
		t.Fatalf("resume read workspace=%q active=%q", sessions.LoadedWorkspace(), sessions.Workspace())
	}
	if runtime.projectRoot != root || engine.History()[0].Content != "project one history" {
		t.Fatalf("resume root=%q history=%v", runtime.projectRoot, engine.History())
	}
}

func TestResumeRepairsBindingWhenHistoryLivesInAnotherWorkspace(t *testing.T) {
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": {{Role: "assistant", Content: "recover me"}}},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-2")
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if workspaces.bindings["saved"] != "project-1" {
		t.Fatalf("stale binding was not repaired: %v", workspaces.bindings)
	}
}

func TestLoadMoreHistoryUsesResumedSessionWorkspace(t *testing.T) {
	history := make([]EngineMessage, 250)
	for index := range history {
		history[index] = EngineMessage{Role: "assistant", Content: fmt.Sprintf("message-%d", index)}
	}
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": history},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-1")
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	sessions.SetWorkspace("project-2") // simulate unrelated active-scope drift
	if err := service.LoadMoreHistory(50); err != nil {
		t.Fatal(err)
	}
	if sessions.LoadedWorkspace() != "project-1" || service.Snapshot().HistoryOffset != 0 {
		t.Fatalf("history range workspace=%q offset=%d", sessions.LoadedWorkspace(), service.Snapshot().HistoryOffset)
	}
}

func TestSwitchProjectStartsIndependentSessionWhenHistoryExists(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "old project"}}}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{workspace: "project-1"}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("session-1", "project-1")
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BindWorkspace("project-2"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.ID != "session-new" || snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "project-2" {
		t.Fatalf("switched snapshot=%+v workspace=%+v", snapshot.Session, snapshot.CurrentWorkspace)
	}
	if workspaces.bindings["session-1"] != "project-1" || workspaces.bindings["session-new"] != "project-2" {
		t.Fatalf("project bindings=%v", workspaces.bindings)
	}
	if savedIDs := sessions.SavedIDs(); len(savedIDs) != 1 || savedIDs[0] != "session-1" {
		t.Fatalf("saved sessions=%v", savedIDs)
	}
}

func TestSnapshotIncludesPersistedSessions(t *testing.T) {
	t.Parallel()
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].ID != "saved" || snapshot.Sessions[0].TokenCount != 4 {
		t.Fatalf("unexpected session metadata: %+v", snapshot.Sessions[0])
	}
}

func TestSnapshotDoesNotReadBlockedSessionCatalog(t *testing.T) {
	sessions := &blockingCatalogSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()
	defer close(sessions.release)

	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not start")
	}
	for index := 0; index < 10; index++ {
		_ = service.Snapshot()
	}
	if calls := sessions.calls.Load(); calls != 1 {
		t.Fatalf("Snapshot invoked SessionPort.List %d times while catalog was blocked", calls)
	}
}

func TestShutdownDoesNotWaitForBlockedSessionCatalog(t *testing.T) {
	sessions := &blockingCatalogSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer close(sessions.release)
	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not start")
	}
	started := time.Now()
	service.Shutdown()
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("shutdown waited for blocked catalog I/O: %s", elapsed)
	}
}

func TestBeginNewSessionClearsWorkTable(t *testing.T) {
	runtime := &fakeRuntime{}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:a": {ID: "task:a", Phase: dto.TaskPhaseTask, Task: "a", Status: dto.TaskRunning},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.WorkTable = buildWorkTable(nil, runtime.TaskSnapshot(), nil)
	service.ViewMu.Unlock()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if got := runtime.TaskSnapshot(); len(got) != 0 {
		t.Fatalf("task registry must be cleared on new session: %+v", got)
	}
	if got := service.Snapshot().Runtime.WorkTable; len(got) != 0 {
		t.Fatalf("worktable must be empty on new session: %+v", got)
	}
}

func TestResumedChatPersistsToSelectedSession(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Chat.Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.savedIDs) != 1 || sessions.savedIDs[0] != "saved" {
		t.Fatalf("saved session IDs = %v, want [saved]", sessions.savedIDs)
	}
}

func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.HistoryOffset = 1
	service.ViewMu.Unlock()

	if err := service.LoadMoreHistory(1); err != nil {
		t.Fatal(err)
	}
	conversation := service.Snapshot().Conversation
	if len(conversation) == 0 || conversation[0].ID == "" {
		t.Fatalf("loaded history message has no stable ID: %#v", conversation)
	}
}

func TestResumeCommandOpensSessionInteraction(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	if err := service.Submit(context.Background(), "/resume"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction == nil || snapshot.Interaction.Kind != "session" {
		t.Fatalf("resume should open a session interaction: %#v", snapshot.Interaction)
	}
}
