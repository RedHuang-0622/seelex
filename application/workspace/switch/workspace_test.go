package workspace

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type fakeStore struct {
	infos    []model.WorkspaceInfo
	byID     map[string]model.WorkspaceInfo
	bindings map[string]string
	detected string

	createErr error
	getErr    error

	createdName   string
	createdRoot   string
	createdRemote string
	createdRoots  []string
	bound         [][2]string
	unbound       []string
	detectCalls   int
}

func newFakeStore(infos ...model.WorkspaceInfo) *fakeStore {
	store := &fakeStore{
		infos:    append([]model.WorkspaceInfo(nil), infos...),
		byID:     make(map[string]model.WorkspaceInfo, len(infos)),
		bindings: make(map[string]string),
	}
	for _, info := range infos {
		store.byID[info.ID] = info
	}
	return store
}

func (store *fakeStore) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error) {
	store.createdName, store.createdRoot, store.createdRemote = name, rootPath, gitRemote
	store.createdRoots = append(store.createdRoots, rootPath)
	if store.createErr != nil {
		return model.WorkspaceInfo{}, store.createErr
	}
	info := model.WorkspaceInfo{ID: "ws-created", Name: name, RootPath: rootPath, GitRemote: gitRemote}
	store.infos = append(store.infos, info)
	store.byID[info.ID] = info
	return info, nil
}

func (store *fakeStore) Get(id string) (model.WorkspaceInfo, error) {
	if store.getErr != nil {
		return model.WorkspaceInfo{}, store.getErr
	}
	info, ok := store.byID[id]
	if !ok {
		return model.WorkspaceInfo{}, errors.New("workspace not found")
	}
	return info, nil
}

func (store *fakeStore) List() []model.WorkspaceInfo {
	return append([]model.WorkspaceInfo(nil), store.infos...)
}

func (store *fakeStore) Delete(string) error { return nil }

func (store *fakeStore) BindSession(sessionID, workspaceID string) {
	store.bound = append(store.bound, [2]string{sessionID, workspaceID})
	store.bindings[sessionID] = workspaceID
}

func (store *fakeStore) UnbindSession(sessionID string) {
	store.unbound = append(store.unbound, sessionID)
	delete(store.bindings, sessionID)
}

func (store *fakeStore) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool) {
	workspaceID, ok := store.bindings[sessionID]
	if !ok {
		return model.WorkspaceInfo{}, false
	}
	info, ok := store.byID[workspaceID]
	return info, ok
}

func (store *fakeStore) AllBindings() map[string]string {
	out := make(map[string]string, len(store.bindings))
	for sessionID, workspaceID := range store.bindings {
		out[sessionID] = workspaceID
	}
	return out
}

func (store *fakeStore) DetectGitRemote(rootPath string) string {
	store.detectCalls++
	store.createdRoot = rootPath
	return store.detected
}

// fakeTreeStore 在基础端口之上实现树/提交查询面。
type fakeTreeStore struct {
	*fakeStore
	listing dto.TreeListing
	count   dto.TreeCount
	log     dto.GitLogResult
	changes dto.WorkspaceChangesResult
	detail  dto.GitCommitDetail

	lastRoot  string
	lastRel   string
	lastDepth int
	lastLimit int
	lastHash  string
}

func (store *fakeTreeStore) ListTree(root, relPath string, depth int) (dto.TreeListing, error) {
	store.lastRoot, store.lastRel, store.lastDepth = root, relPath, depth
	return store.listing, nil
}

func (store *fakeTreeStore) CountFiles(root string) (dto.TreeCount, error) {
	store.lastRoot = root
	return store.count, nil
}

func (store *fakeTreeStore) GitLog(root string, limit int) (dto.GitLogResult, error) {
	store.lastRoot, store.lastLimit = root, limit
	return store.log, nil
}

func (store *fakeTreeStore) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error) {
	store.lastRoot, store.lastLimit = root, limit
	return store.changes, nil
}

func (store *fakeTreeStore) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error) {
	store.lastRoot, store.lastHash, store.lastLimit = root, hash, limit
	return store.detail, nil
}

// fakeFileStore 在读端口之上实现写入面。
type fakeFileStore struct {
	*fakeStore
	content  dto.FileContent
	written  dto.FileWriteResult
	writeErr error

	lastRoot    string
	lastRel     string
	lastLimit   int64
	lastHash    string
	writeRoot   string
	writeRel    string
	writeBody   string
	lastCommit  string
	lastContent string
}

func (store *fakeFileStore) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	store.lastRoot, store.lastRel, store.lastLimit = root, relPath, limit
	return store.content, nil
}

func (store *fakeFileStore) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error) {
	store.lastRoot, store.lastHash, store.lastRel, store.lastLimit = root, hash, relPath, limit
	return store.content, nil
}

func (store *fakeFileStore) WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error) {
	store.writeRoot, store.writeRel, store.writeBody = root, relPath, string(content)
	return store.written, store.writeErr
}

type draftBinding struct {
	info       model.WorkspaceInfo
	projection Projection
}

type sessionBinding struct {
	info           model.WorkspaceInfo
	projection     Projection
	freshSessionID string
	notice         string
}

type fakeView struct {
	state ViewState

	locked   int
	unlocked int
	drafts   []draftBinding
	binds    []sessionBinding
	unbinds  []unbindCall

	nextRevision uint64
	published    []uint64
	refreshes    int
	reviewClears int
}

type unbindCall struct {
	sessionID  string
	projection Projection
}

func (view *fakeView) LockTransition() func() {
	view.locked++
	return func() { view.unlocked++ }
}

func (view *fakeView) State() ViewState { return view.state }

func (view *fakeView) ApplyDraftBinding(info model.WorkspaceInfo, projection Projection) uint64 {
	view.drafts = append(view.drafts, draftBinding{info: info, projection: projection})
	view.nextRevision++
	return view.nextRevision
}

func (view *fakeView) ApplyBinding(info model.WorkspaceInfo, projection Projection, freshSessionID, notice string) uint64 {
	view.binds = append(view.binds, sessionBinding{
		info: info, projection: projection, freshSessionID: freshSessionID, notice: notice,
	})
	view.nextRevision++
	return view.nextRevision
}

func (view *fakeView) ApplyUnbind(sessionID string, projection Projection) uint64 {
	view.unbinds = append(view.unbinds, unbindCall{sessionID: sessionID, projection: projection})
	view.nextRevision++
	return view.nextRevision
}

func (view *fakeView) PublishSnapshotChanged(revision uint64) {
	view.published = append(view.published, revision)
}

func (view *fakeView) RequestCatalogRefresh() { view.refreshes++ }

func (view *fakeView) ClearReviewRequest() { view.reviewClears++ }

type fakeSessions struct {
	scopes  []string
	saved   []string
	saveErr error
}

func (sessions *fakeSessions) SetWorkspace(workspaceID string) {
	sessions.scopes = append(sessions.scopes, workspaceID)
}

func (sessions *fakeSessions) SaveCurrent(sessionID string) error {
	sessions.saved = append(sessions.saved, sessionID)
	return sessions.saveErr
}

type fakeRuntime struct {
	rootBindings   []string
	rootUnbinds    int
	sessionUnbinds []string
	workspaces     [][2]string
	switchTasks    [][2]string
	tasks          []dto.TaskRecord
	clearedTrees   int
	clearTreeErr   error
}

func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error {
	runtime.rootBindings = append(runtime.rootBindings, rootPath)
	return nil
}

func (runtime *fakeRuntime) UnbindProjectRoot() { runtime.rootUnbinds++ }

func (runtime *fakeRuntime) UnbindProjectRootFor(sessionID string) {
	runtime.sessionUnbinds = append(runtime.sessionUnbinds, sessionID)
}

func (runtime *fakeRuntime) SetSessionWorkspace(sessionID, workspaceID string) {
	runtime.workspaces = append(runtime.workspaces, [2]string{sessionID, workspaceID})
}

func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	runtime.switchTasks = append(runtime.switchTasks, [2]string{sessionID, "read"})
	return runtime.tasks
}

func (runtime *fakeRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	runtime.switchTasks = append(runtime.switchTasks, [2]string{sessionID, "write"})
}

func (runtime *fakeRuntime) ClearSubagentTree() error {
	runtime.clearedTrees++
	return runtime.clearTreeErr
}

type fakeEngine struct {
	history     []contract.EngineMessage
	startID     string
	prompt      string
	activated   []string
	activateErr error
	startCalls  int
}

func (engine *fakeEngine) History() []contract.EngineMessage { return engine.history }

func (engine *fakeEngine) StartSession() string {
	engine.startCalls++
	return engine.startID
}

func (engine *fakeEngine) SetSystemPrompt(prompt string) { engine.prompt = prompt }

func (engine *fakeEngine) ActivateSession(sessionID string) error {
	engine.activated = append(engine.activated, sessionID)
	return engine.activateErr
}

type harness struct {
	usecase  *UseCase
	store    *fakeStore
	port     contract.WorkspacePort
	view     *fakeView
	sessions *fakeSessions
	runtime  *fakeRuntime
	engine   *fakeEngine

	workspaceIDFor      string
	workspaceIDForCalls []string
	newSessionID        string
	perSession          bool
	attached            [][2]string
	refreshed           int
	chatRunningErr      error
}

func newHarness(state ViewState, store *fakeStore) *harness {
	h := &harness{
		store:          store,
		view:           &fakeView{state: state, nextRevision: 40},
		sessions:       &fakeSessions{},
		runtime:        &fakeRuntime{},
		engine:         &fakeEngine{},
		newSessionID:   "ws_generated",
		chatRunningErr: errors.New("chat is already running"),
	}
	if store != nil {
		h.port = store
	}
	h.usecase = New(Deps{
		Store:    h.port,
		Sessions: h.sessions,
		Runtime:  h.runtime,
		Engine:   h.engine,
		View:     h.view,
		Prompt:   func() string { return "system prompt" },
		PerSessionExecution: func() bool {
			return h.perSession
		},
		NewSessionID: func(string) string { return h.newSessionID },
		AttachSessionContext: func(workspaceID, sessionID string) error {
			h.attached = append(h.attached, [2]string{workspaceID, sessionID})
			return nil
		},
		RefreshWorkTable: func() { h.refreshed++ },
		WorkspaceIDForSession: func(sessionID string) string {
			h.workspaceIDForCalls = append(h.workspaceIDForCalls, sessionID)
			return h.workspaceIDFor
		},
		ChatRunningError: h.chatRunningErr,
	})
	return h
}

func TestCreateWorkspaceValidatesAndDetectsRemote(t *testing.T) {
	h := newHarness(ViewState{Draft: true}, newFakeStore())
	h.store.detected = "git@example.com:repo.git"

	if err := h.usecase.CreateWorkspace("", "/root", ""); err == nil {
		t.Fatal("CreateWorkspace accepted an empty name")
	}
	if err := h.usecase.CreateWorkspace("project", "", ""); err == nil {
		t.Fatal("CreateWorkspace accepted an empty root")
	}
	if h.store.createdName != "" {
		t.Fatalf("store.Create called before validation: %+v", h.store)
	}

	if err := h.usecase.CreateWorkspace("project", "/root", ""); err != nil {
		t.Fatal(err)
	}
	if h.store.detectCalls != 1 || h.store.createdRemote != "git@example.com:repo.git" {
		t.Fatalf("detect calls=%d remote=%q", h.store.detectCalls, h.store.createdRemote)
	}
	if len(h.view.drafts) != 1 || h.view.drafts[0].info.ID != "ws-created" {
		t.Fatalf("draft binding not applied: %+v", h.view.drafts)
	}
	if h.view.locked != 1 || h.view.unlocked != 1 {
		t.Fatalf("transition lock = %d/%d, want 1/1", h.view.locked, h.view.unlocked)
	}
}

func TestCreateWorkspaceKeepsExplicitRemote(t *testing.T) {
	h := newHarness(ViewState{Draft: true}, newFakeStore())
	h.store.detected = "detected"
	if err := h.usecase.CreateWorkspace("project", "/root", "explicit"); err != nil {
		t.Fatal(err)
	}
	if h.store.detectCalls != 0 || h.store.createdRemote != "explicit" {
		t.Fatalf("detect calls=%d remote=%q", h.store.detectCalls, h.store.createdRemote)
	}
}

func TestCreateWorkspacesCreatesAllWithoutBinding(t *testing.T) {
	h := newHarness(ViewState{SessionID: "s1", Draft: true}, newFakeStore())
	h.store.detected = "git@example.com:team/project.git"

	result, err := h.usecase.CreateWorkspaces([]dto.WorkspaceCreateRequest{
		{Name: "alpha", RootPath: "/src/alpha"},
		{Name: "beta", RootPath: "/src/beta", GitRemote: "explicit.git"},
	})
	if err != nil {
		t.Fatalf("CreateWorkspaces: %v", err)
	}
	if len(result.Workspaces) != 2 {
		t.Fatalf("created %d workspaces, want 2", len(result.Workspaces))
	}
	if len(h.store.createdRoots) != 2 || h.store.createdRoots[0] != "/src/alpha" || h.store.createdRoots[1] != "/src/beta" {
		t.Fatalf("store create roots = %#v", h.store.createdRoots)
	}
	if len(h.store.bound) != 0 || len(h.view.binds) != 0 || len(h.view.drafts) != 0 || h.view.locked != 0 {
		t.Fatal("batch creation must not bind a project to the current view")
	}
}

func TestCreateWorkspacesRejectsDuplicateRootsBeforeCreating(t *testing.T) {
	h := newHarness(ViewState{SessionID: "s1"}, newFakeStore())

	_, err := h.usecase.CreateWorkspaces([]dto.WorkspaceCreateRequest{
		{Name: "alpha", RootPath: "/src/alpha"},
		{Name: "alpha-copy", RootPath: "/src/alpha/"},
	})
	if err == nil {
		t.Fatal("CreateWorkspaces accepted duplicate roots")
	}
	if len(h.store.createdRoots) != 0 {
		t.Fatalf("duplicate batch must validate before writing, created %#v", h.store.createdRoots)
	}
}

func TestCreateWorkspacesRejectsExistingRootBeforeCreating(t *testing.T) {
	h := newHarness(ViewState{SessionID: "s1"}, newFakeStore(model.WorkspaceInfo{
		ID: "existing", Name: "existing", RootPath: "/src/alpha",
	}))

	_, err := h.usecase.CreateWorkspaces([]dto.WorkspaceCreateRequest{
		{Name: "beta", RootPath: "/src/beta"},
		{Name: "alpha-again", RootPath: "/src/alpha/"},
	})
	if err == nil {
		t.Fatal("CreateWorkspaces accepted an already registered root")
	}
	if len(h.store.createdRoots) != 0 {
		t.Fatalf("batch must reject existing roots before writing, created %#v", h.store.createdRoots)
	}
}

func TestBindWorkspaceDraftKeepsBindingInView(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-draft", Name: "Draft", RootPath: "/draft", GitRemote: "origin"}
	h := newHarness(ViewState{SessionID: "draft-1", Draft: true}, newFakeStore(info))
	h.store.bindings["s-old"] = "ws-old"

	if err := h.usecase.BindWorkspace("ws-draft"); err != nil {
		t.Fatal(err)
	}
	if len(h.runtime.rootBindings) != 1 || h.runtime.rootBindings[0] != info.RootPath {
		t.Fatalf("project root bindings = %v", h.runtime.rootBindings)
	}
	if len(h.sessions.scopes) != 1 || h.sessions.scopes[0] != info.ID {
		t.Fatalf("write scopes = %v", h.sessions.scopes)
	}
	if len(h.view.drafts) != 1 {
		t.Fatalf("draft bindings = %+v", h.view.drafts)
	}
	draft := h.view.drafts[0]
	if draft.info.ID != info.ID || len(draft.projection.Workspaces) != 1 || draft.projection.Workspaces[0].ID != info.ID {
		t.Fatalf("unexpected draft projection: %+v", draft)
	}
	if draft.projection.Bindings["s-old"] != "ws-old" {
		t.Fatalf("projection bindings = %v", draft.projection.Bindings)
	}
	if len(h.view.binds) != 0 || len(h.view.unbinds) != 0 {
		t.Fatal("draft binding touched the session binding surface")
	}
	if len(h.engine.history) != 0 && h.engine.startCalls != 0 {
		t.Fatal("draft binding touched the engine")
	}
	if len(h.view.published) != 1 || h.view.published[0] != 41 || h.view.refreshes != 1 || h.view.reviewClears != 1 {
		t.Fatalf("publish=%v refreshes=%d clears=%d", h.view.published, h.view.refreshes, h.view.reviewClears)
	}
}

func TestBindWorkspaceStartsFreshSessionAcrossProjects(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-new", Name: "New", RootPath: "/new", GitRemote: ""}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceID: ""}, newFakeStore(info))
	h.engine.history = []contract.EngineMessage{{Role: "user", Content: "hello"}}
	h.workspaceIDFor = "ws-old"
	h.perSession = true
	h.runtime.tasks = []dto.TaskRecord{{ID: "task-1"}}

	if err := h.usecase.BindWorkspace("ws-new"); err != nil {
		t.Fatal(err)
	}
	if len(h.sessions.saved) != 1 || h.sessions.saved[0] != "s1" {
		t.Fatalf("saved sessions = %v", h.sessions.saved)
	}
	if len(h.workspaceIDForCalls) != 1 || h.workspaceIDForCalls[0] != "s1" {
		t.Fatalf("workspace lookup = %v", h.workspaceIDForCalls)
	}
	if len(h.sessions.scopes) != 2 || h.sessions.scopes[0] != "ws-old" || h.sessions.scopes[1] != info.ID {
		t.Fatalf("write scopes = %v", h.sessions.scopes)
	}
	if len(h.engine.activated) != 1 || h.engine.activated[0] != "ws_generated" {
		t.Fatalf("engine activations = %v", h.engine.activated)
	}
	if h.engine.prompt != "system prompt" {
		t.Fatalf("engine prompt = %q", h.engine.prompt)
	}
	if len(h.store.bound) != 1 || h.store.bound[0] != [2]string{"ws_generated", info.ID} {
		t.Fatalf("bindings = %v", h.store.bound)
	}
	if len(h.runtime.workspaces) != 1 || h.runtime.workspaces[0] != [2]string{"ws_generated", info.ID} {
		t.Fatalf("runtime workspaces = %v", h.runtime.workspaces)
	}
	if len(h.attached) != 1 || h.attached[0] != [2]string{info.ID, "ws_generated"} {
		t.Fatalf("attached contexts = %v", h.attached)
	}
	if len(h.view.binds) != 1 {
		t.Fatalf("session bindings = %+v", h.view.binds)
	}
	bind := h.view.binds[0]
	if bind.info.ID != info.ID || bind.freshSessionID != "ws_generated" || bind.notice != "已切换到项目 New，新建独立会话" {
		t.Fatalf("unexpected binding: %+v", bind)
	}
	if len(h.runtime.switchTasks) != 2 || h.runtime.switchTasks[0] != [2]string{"ws_generated", "read"} || h.runtime.switchTasks[1] != [2]string{"ws_generated", "write"} {
		t.Fatalf("task switches = %v", h.runtime.switchTasks)
	}
	if h.runtime.clearedTrees != 1 || h.refreshed != 1 {
		t.Fatalf("tree clears=%d worktable refreshes=%d", h.runtime.clearedTrees, h.refreshed)
	}
	if len(h.view.published) != 1 || h.view.published[0] != 41 {
		t.Fatalf("published revisions = %v", h.view.published)
	}
}

func TestBindWorkspaceReusesSessionWhenProjectUnchanged(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-same", Name: "Same", RootPath: "/same"}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceID: "ws-same"}, newFakeStore(info))
	h.engine.history = []contract.EngineMessage{{Role: "user", Content: "hello"}}

	if err := h.usecase.BindWorkspace("ws-same"); err != nil {
		t.Fatal(err)
	}
	if len(h.sessions.saved) != 0 || len(h.engine.activated) != 0 || len(h.attached) != 0 {
		t.Fatalf("same-project bind started a fresh session: saved=%v activated=%v attached=%v",
			h.sessions.saved, h.engine.activated, h.attached)
	}
	if len(h.store.bound) != 1 || h.store.bound[0] != [2]string{"s1", info.ID} {
		t.Fatalf("bindings = %v", h.store.bound)
	}
	if len(h.view.binds) != 1 || h.view.binds[0].freshSessionID != "" || h.view.binds[0].notice != "" {
		t.Fatalf("unexpected binding: %+v", h.view.binds)
	}
}

func TestBindWorkspaceWithoutHistoryKeepsSession(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-new", Name: "New", RootPath: "/new"}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceID: "ws-old"}, newFakeStore(info))

	if err := h.usecase.BindWorkspace("ws-new"); err != nil {
		t.Fatal(err)
	}
	if len(h.sessions.saved) != 0 || len(h.engine.activated) != 0 {
		t.Fatal("empty history must not start a new session")
	}
	if len(h.view.binds) != 1 || h.view.binds[0].freshSessionID != "" {
		t.Fatalf("unexpected binding: %+v", h.view.binds)
	}
}

func TestBindWorkspaceRejectsRunningChat(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-new", Name: "New", RootPath: "/new"}
	h := newHarness(ViewState{SessionID: "s1", Running: true}, newFakeStore(info))
	h.engine.history = []contract.EngineMessage{{Role: "user", Content: "hello"}}

	err := h.usecase.BindWorkspace("ws-new")
	if !errors.Is(err, h.chatRunningErr) {
		t.Fatalf("BindWorkspace(running) = %v, want host's chat-running error", err)
	}
	if len(h.store.bound) != 0 || len(h.view.binds) != 0 || len(h.sessions.scopes) != 0 {
		t.Fatal("running rejection still mutated bindings")
	}
	if h.view.locked != 1 || h.view.unlocked != 1 {
		t.Fatalf("transition lock = %d/%d", h.view.locked, h.view.unlocked)
	}
}

func TestBindWorkspaceFallsBackToEngineSession(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-new", Name: "New", RootPath: "/new"}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceID: "ws-old"}, newFakeStore(info))
	h.engine.history = []contract.EngineMessage{{Role: "user", Content: "hello"}}
	h.engine.startID = "engine-sid"
	h.perSession = false

	if err := h.usecase.BindWorkspace("ws-new"); err != nil {
		t.Fatal(err)
	}
	if h.engine.startCalls != 1 || len(h.store.bound) != 1 || h.store.bound[0] != [2]string{"engine-sid", info.ID} {
		t.Fatalf("start calls=%d bindings=%v", h.engine.startCalls, h.store.bound)
	}
}

func TestUnbindWorkspaceClearsSessionAndView(t *testing.T) {
	info := model.WorkspaceInfo{ID: "ws-1", Name: "One", RootPath: "/one"}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceID: "ws-1"}, newFakeStore(info))

	h.usecase.UnbindWorkspace()
	if h.runtime.rootUnbinds != 1 {
		t.Fatalf("global root unbinds = %d", h.runtime.rootUnbinds)
	}
	if len(h.store.unbound) != 1 || h.store.unbound[0] != "s1" {
		t.Fatalf("store unbinds = %v", h.store.unbound)
	}
	if len(h.runtime.sessionUnbinds) != 1 || h.runtime.sessionUnbinds[0] != "s1" {
		t.Fatalf("runtime unbinds = %v", h.runtime.sessionUnbinds)
	}
	if len(h.sessions.scopes) != 1 || h.sessions.scopes[0] != "" {
		t.Fatalf("write scopes = %v", h.sessions.scopes)
	}
	if len(h.view.unbinds) != 1 || h.view.unbinds[0].sessionID != "s1" {
		t.Fatalf("view unbinds = %+v", h.view.unbinds)
	}
	if len(h.view.published) != 1 || h.view.refreshes != 1 || h.view.reviewClears != 1 {
		t.Fatalf("publish=%v refreshes=%d clears=%d", h.view.published, h.view.refreshes, h.view.reviewClears)
	}
}

func TestUnbindWorkspaceLeavesDraftBindingToView(t *testing.T) {
	h := newHarness(ViewState{SessionID: "draft-1", Draft: true}, newFakeStore())
	h.usecase.UnbindWorkspace()
	if len(h.store.unbound) != 0 || len(h.runtime.sessionUnbinds) != 0 {
		t.Fatal("draft unbind touched the persisted binding surface")
	}
	if len(h.view.unbinds) != 1 || h.view.unbinds[0].sessionID != "draft-1" {
		t.Fatalf("view unbinds = %+v", h.view.unbinds)
	}
}

func TestWorkspaceQueriesForwardCurrentRootAndCapabilities(t *testing.T) {
	store := &fakeTreeStore{
		fakeStore: newFakeStore(model.WorkspaceInfo{ID: "ws-1", RootPath: "/root"}),
		listing:   dto.TreeListing{Entries: []dto.TreeEntry{{Path: "src"}}},
		count:     dto.TreeCount{Files: 2},
		log:       dto.GitLogResult{Commits: []dto.GitCommitNode{{Hash: "abc"}}},
		changes:   dto.WorkspaceChangesResult{Branch: "main"},
		detail:    dto.GitCommitDetail{Hash: "abc"},
	}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceRoot: "/root"}, store.fakeStore)
	h.usecase = newStoreUsecase(h, store)

	if listing, err := h.usecase.Tree("src", 2); err != nil || len(listing.Entries) != 1 {
		t.Fatalf("Tree = %+v, %v", listing, err)
	}
	if store.lastRoot != "/root" || store.lastRel != "src" || store.lastDepth != 2 {
		t.Fatalf("tree args root=%q rel=%q depth=%d", store.lastRoot, store.lastRel, store.lastDepth)
	}
	if count, err := h.usecase.FileCount(); err != nil || count.Files != 2 || store.lastRoot != "/root" {
		t.Fatalf("FileCount = %+v, %v root=%q", count, err, store.lastRoot)
	}
	if log, err := h.usecase.GitLog(10); err != nil || len(log.Commits) != 1 || store.lastLimit != 10 {
		t.Fatalf("GitLog = %+v, %v limit=%d", log, err, store.lastLimit)
	}
	if changes, err := h.usecase.Changes(50); err != nil || changes.Branch != "main" || store.lastLimit != 50 {
		t.Fatalf("Changes = %+v, %v limit=%d", changes, err, store.lastLimit)
	}
	if detail, err := h.usecase.GitCommitDetail("abc", 20); err != nil || detail.Hash != "abc" || store.lastHash != "abc" {
		t.Fatalf("GitCommitDetail = %+v, %v hash=%q", detail, err, store.lastHash)
	}
}

func TestWorkspaceFileQueriesForwardCurrentRoot(t *testing.T) {
	store := &fakeFileStore{
		fakeStore: newFakeStore(model.WorkspaceInfo{ID: "ws-1", RootPath: "/root"}),
		content:   dto.FileContent{Path: "src/main.go"},
		written:   dto.FileWriteResult{Path: "src/main.go", Size: 5},
	}
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceRoot: "/root"}, store.fakeStore)
	h.usecase = newFileUsecase(h, store)

	if content, err := h.usecase.FileContent("src/main.go", 16); err != nil || content.Path != "src/main.go" || store.lastLimit != 16 {
		t.Fatalf("FileContent = %+v, %v limit=%d", content, err, store.lastLimit)
	}
	if content, err := h.usecase.GitCommitFileContent("abc", "src/main.go", 8); err != nil || content.Path != "src/main.go" || store.lastHash != "abc" {
		t.Fatalf("GitCommitFileContent = %+v, %v hash=%q", content, err, store.lastHash)
	}
	if result, err := h.usecase.WriteFile("src/main.go", "hello"); err != nil || result.Size != 5 {
		t.Fatalf("WriteFile = %+v, %v", result, err)
	}
	if store.writeRoot != "/root" || store.writeRel != "src/main.go" || store.writeBody != "hello" {
		t.Fatalf("write args root=%q rel=%q body=%q", store.writeRoot, store.writeRel, store.writeBody)
	}
}

func TestWorkspaceQueriesRequireBoundRoot(t *testing.T) {
	h := newHarness(ViewState{SessionID: "s1"}, newFakeStore())
	if _, err := h.usecase.Tree("", 1); err == nil {
		t.Fatal("Tree succeeded without a bound root")
	}
	if _, err := h.usecase.FileContent("README.md", 0); err == nil {
		t.Fatal("FileContent succeeded without a bound root")
	}
	if _, err := h.usecase.WriteFile("README.md", "x"); err == nil {
		t.Fatal("WriteFile succeeded without a bound root")
	}
}

func TestWorkspaceQueriesReportMissingCapabilities(t *testing.T) {
	h := newHarness(ViewState{SessionID: "s1", CurrentWorkspaceRoot: "/root"}, newFakeStore())
	if _, err := h.usecase.Tree("", 1); err == nil || err.Error() != "worktree: workspace backend does not support tree listing" {
		t.Fatalf("Tree error = %v", err)
	}
	if _, err := h.usecase.FileContent("README.md", 0); err == nil || err.Error() != "worktree: workspace backend does not support file preview" {
		t.Fatalf("FileContent error = %v", err)
	}
	if _, err := h.usecase.WriteFile("README.md", "x"); err == nil || err.Error() != "worktree: workspace backend does not support file editing" {
		t.Fatalf("WriteFile error = %v", err)
	}
}

func TestProjectionCopiesStoreState(t *testing.T) {
	store := newFakeStore(
		model.WorkspaceInfo{ID: "ws-1", Name: "One", RootPath: "/one"},
		model.WorkspaceInfo{ID: "ws-2", Name: "Two", RootPath: "/two"},
	)
	store.bindings["s1"] = "ws-1"
	store.bindings["s2"] = "ws-2"
	h := newHarness(ViewState{}, store)

	projection := h.usecase.Projection()
	if len(projection.Workspaces) != 2 || projection.Workspaces[0].ID != "ws-1" || projection.Workspaces[1].ID != "ws-2" {
		t.Fatalf("workspaces = %+v", projection.Workspaces)
	}
	if projection.Bindings["s1"] != "ws-1" || projection.Bindings["s2"] != "ws-2" {
		t.Fatalf("bindings = %v", projection.Bindings)
	}
	projection.Workspaces[0].Name = "mutated"
	projection.Bindings["s1"] = "mutated"
	if store.infos[0].Name != "One" || store.bindings["s1"] != "ws-1" {
		t.Fatal("projection aliases store state")
	}
}

func TestProjectionWithoutStoreIsEmpty(t *testing.T) {
	h := newHarness(ViewState{}, nil)
	projection := h.usecase.Projection()
	if len(projection.Workspaces) != 0 || len(projection.Bindings) != 0 {
		t.Fatalf("projection = %+v", projection)
	}
}

// newFileUsecase 复制 harness 的宿主面，换一个实现文件端口的 store。
func newFileUsecase(h *harness, store *fakeFileStore) *UseCase {
	return newStoreUsecase(h, store)
}

// newStoreUsecase 复制 harness 的宿主面，换一个实现可选端口的 store。
func newStoreUsecase(h *harness, store contract.WorkspacePort) *UseCase {
	h.port = store
	return New(Deps{
		Store:    store,
		Sessions: h.sessions,
		Runtime:  h.runtime,
		Engine:   h.engine,
		View:     h.view,
		Prompt:   func() string { return "system prompt" },
		PerSessionExecution: func() bool {
			return h.perSession
		},
		NewSessionID: func(string) string { return h.newSessionID },
		AttachSessionContext: func(workspaceID, sessionID string) error {
			h.attached = append(h.attached, [2]string{workspaceID, sessionID})
			return nil
		},
		RefreshWorkTable: func() { h.refreshed++ },
		WorkspaceIDForSession: func(sessionID string) string {
			h.workspaceIDForCalls = append(h.workspaceIDForCalls, sessionID)
			return h.workspaceIDFor
		},
		ChatRunningError: h.chatRunningErr,
	})
}
