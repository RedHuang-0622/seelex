package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

type emittedEvent struct {
	name    string
	payload any
}

type fakeApplication struct {
	hub              *application.EventHub
	snapshot         application.Snapshot
	submitted        string
	cancelled        string
	resolvedID       string
	resolvedOption   string
	selectedAccount  string
	selectedEffort   string
	selectedPlugin   string
	loadedHistory    int
	suggestionsInput string
	beganNewSession  bool
	composerText     string
	resumedSession   string
	forkedSession    string
	scheduledSpec    seelebridge.ScheduledTaskSpec
	cancelledTaskID  string
	searchQuery      string
	searchLimit      int
	searchResult     seelexctxsearch.Result
	workItemID       string
	workItemStatus   string
	workItemErr      error
	treeListing      dto.TreeListing
	treeCount        dto.TreeCount
	treeRel          string
	treeDepth        int
	treeErr          error
	gitLog           dto.GitLogResult
	gitLimit         int
	metaSessionID    string
	sessionMeta      application.SessionMeta
	catalogSettles   int
	catalogGate      chan struct{}
}

// recordingCancelApplication 记录 Bridge 转发的 request_id 序列。
type recordingCancelApplication struct {
	*fakeApplication
	calls []string
}

func (fake *recordingCancelApplication) CancelChat(requestID string) bool {
	fake.calls = append(fake.calls, requestID)
	return true
}

func newFakeApplication() *fakeApplication {
	return &fakeApplication{
		hub: application.NewEventHub(),
		snapshot: application.Snapshot{
			ProtocolVersion: application.ProtocolVersion,
			Revision:        1,
			Runtime:         application.RuntimeState{Model: "test-model"},
		},
	}
}

func (fake *fakeApplication) Snapshot() application.Snapshot { return fake.snapshot }
func (*fakeApplication) BeginGracefulShutdown()              {}
func (*fakeApplication) WaitForIdle(context.Context) error   { return nil }
func (fake *fakeApplication) Subscribe(buffer int) application.Subscription {
	return fake.hub.Subscribe(buffer)
}
func (fake *fakeApplication) Submit(_ context.Context, text string) error {
	fake.submitted = text
	return nil
}
func (fake *fakeApplication) BeginNewSession() error {
	fake.beganNewSession = true
	return nil
}
func (fake *fakeApplication) SaveComposerDraft(text string) error {
	fake.composerText = text
	return nil
}
func (fake *fakeApplication) ResumeSession(sessionID string) error {
	fake.resumedSession = sessionID
	return nil
}

func (fake *fakeApplication) ForkSessionLatest(sessionID string) (string, error) {
	fake.forkedSession = sessionID
	return "child-" + sessionID, nil
}
func (fake *fakeApplication) CancelChat(requestID string) bool {
	fake.cancelled = requestID
	return true
}
func (fake *fakeApplication) ResolveInteraction(_ context.Context, id, optionID string) error {
	fake.resolvedID, fake.resolvedOption = id, optionID
	return nil
}
func (fake *fakeApplication) SelectAccount(_ context.Context, name string) error {
	fake.selectedAccount = name
	return nil
}
func (fake *fakeApplication) SwitchEffort(_ context.Context, level string) error {
	fake.selectedEffort = level
	return nil
}
func (fake *fakeApplication) SwitchPlugin(_ context.Context, name string) error {
	fake.selectedPlugin = name
	return nil
}
func (fake *fakeApplication) LoadMoreHistory(limit int) error {
	fake.loadedHistory = limit
	return nil
}
func (fake *fakeApplication) Suggestions(input string) []application.Suggestion {
	fake.suggestionsInput = input
	return []application.Suggestion{{Text: "help", Kind: "command"}}
}
func (fake *fakeApplication) DeleteSession(sessionID string) error {
	return nil
}
func (fake *fakeApplication) SetSessionMeta(sessionID string, meta application.SessionMeta) error {
	fake.metaSessionID = sessionID
	fake.sessionMeta = meta
	return nil
}

// WaitCatalogRefresh 记录 Bridge 的目录收敛等待；catalogGate 非空时阻塞到该
// channel 关闭，用于断言命令未等到收敛就不会返回。
func (fake *fakeApplication) WaitCatalogRefresh(ctx context.Context) error {
	fake.catalogSettles++
	if fake.catalogGate == nil {
		return nil
	}
	select {
	case <-fake.catalogGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (fake *fakeApplication) CreateWorkspace(name, rootPath, gitRemote string) error {
	return nil
}
func (fake *fakeApplication) BindWorkspace(workspaceID string) error {
	return nil
}
func (fake *fakeApplication) UnbindWorkspace()      {}
func (fake *fakeApplication) SetFullAccess(on bool) {}
func (fake *fakeApplication) SessionStorageConfig() (sessionstore.Config, error) {
	return sessionstore.Config{Backend: sessionstore.BackendJSON, Path: "sessions"}, nil
}
func (fake *fakeApplication) TestSessionStorage(context.Context, sessionstore.Config) error {
	return nil
}
func (fake *fakeApplication) ConfigureSessionStorage(context.Context, sessionstore.Config) error {
	return nil
}
func (fake *fakeApplication) SubagentSessionDetail(string) (*application.SubagentDetail, error) {
	return nil, nil
}
func (fake *fakeApplication) SubscribeSubagentLive(string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	ch := make(chan dto.SubagentLiveEvent, 16)
	return nil, ch, func() {}, nil
}
func (fake *fakeApplication) ScheduleTask(_ context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	fake.scheduledSpec = spec
	return &seelebridge.ScheduledTaskStatus{ID: "sched_1", Name: spec.Name, Kind: string(spec.Kind), Enabled: spec.Enabled}, nil
}
func (fake *fakeApplication) CancelScheduledTask(id string) error {
	fake.cancelledTaskID = id
	return nil
}
func (fake *fakeApplication) ClearSubagentTree() error { return nil }
func (fake *fakeApplication) UpdateWorkItemStatus(id, status string) error {
	fake.workItemID, fake.workItemStatus = id, status
	return fake.workItemErr
}

func (fake *fakeApplication) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error) {
	fake.searchQuery = query
	fake.searchLimit = limit
	return fake.searchResult, nil
}

func (fake *fakeApplication) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	fake.treeRel = relPath
	fake.treeDepth = depth
	return fake.treeListing, fake.treeErr
}

func (fake *fakeApplication) WorkspaceFileCount() (dto.TreeCount, error) {
	return fake.treeCount, fake.treeErr
}

func (fake *fakeApplication) WorkspaceGitLog(limit int) (dto.GitLogResult, error) {
	fake.gitLimit = limit
	return fake.gitLog, nil
}

func (fake *fakeApplication) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (application.ToolResultPage, error) {
	return application.ToolResultPage{ResultRef: resultRef, Offset: offset, NextOffset: offset + limit, Content: "page"}, nil
}

func (fake *fakeApplication) PerfStats() application.PerfStats {
	return application.PerfStats{SnapshotBytes: 1024, ConversationMessages: 3}
}

func (fake *fakeApplication) PromptLayers() []application.PromptLayer {
	return nil
}

func TestNewBridgeRequiresApplication(t *testing.T) {
	t.Parallel()
	if _, err := NewBridge(nil, Options{}); err == nil {
		t.Fatal("NewBridge accepted a nil application")
	}
}

func TestBridgeDiscoversProjectSources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("project"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge, err := NewBridge(newFakeApplication(), Options{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	info := bridge.Info()
	if info.Project.Root != root || len(info.Project.Sources) != 1 || info.Project.Sources[0].Path != "README.md" {
		t.Fatalf("unexpected project info: %+v", info.Project)
	}
}

func TestBridgeDoesNotTreatLaunchDirectoryAsProject(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridge(newFakeApplication(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project := bridge.Info().Project; project.Name != "" || project.Root != "" || len(project.Sources) != 0 {
		t.Fatalf("empty project root discovered launch directory: %+v", project)
	}
}

func TestBridgeSubmitForwardsFrontendRequestToApplication(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if err := bridge.Submit("hello"); err != nil {
		t.Fatal(err)
	}
	if fake.submitted != "hello" {
		t.Fatalf("frontend Submit text = %q, want hello", fake.submitted)
	}
}

func TestBridgeForwardsOtherCommands(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if err := bridge.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.ResumeSession("session-2"); err != nil {
		t.Fatal(err)
	}
	if !bridge.CancelChat("request-1") {
		t.Fatal("CancelChat returned false")
	}
	if err := bridge.ResolveInteraction("approval-1", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SelectAccount("main"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SwitchEffort("high"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SwitchPlugin("default"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.LoadMoreHistory(50); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SaveComposerDraft("尚未发送的问题"); err != nil {
		t.Fatal(err)
	}
	suggestions := bridge.Suggestions("/he")

	if !fake.beganNewSession || fake.resumedSession != "session-2" || fake.cancelled != "request-1" {
		t.Fatalf("chat commands were not forwarded: %#v", fake)
	}
	if fake.resolvedID != "approval-1" || fake.resolvedOption != "allow" {
		t.Fatalf("interaction was not forwarded: %#v", fake)
	}
	if fake.selectedAccount != "main" || fake.selectedEffort != "high" || fake.selectedPlugin != "default" {
		t.Fatalf("runtime commands were not forwarded: %#v", fake)
	}
	if fake.loadedHistory != 50 || fake.suggestionsInput != "/he" || len(suggestions) != 1 {
		t.Fatalf("history or suggestions were not forwarded: %#v", fake)
	}
	if fake.composerText != "尚未发送的问题" {
		t.Fatalf("composer draft was not forwarded: %#v", fake)
	}
	if bridge.Info().Title != "Seelex Test" || bridge.Snapshot().Runtime.Model != "test-model" {
		t.Fatal("bridge metadata or snapshot mismatch")
	}
}

func TestBridgeWorkspaceTreeForwardsArguments(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	fake.treeListing = dto.TreeListing{Entries: []dto.TreeEntry{
		{Name: "src", Path: "src", Type: "dir", Count: 2},
		{Name: "README.md", Path: "README.md", Type: "file", Size: 10},
	}}
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	listing, err := bridge.WorkspaceTree("src", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fake.treeRel != "src" || fake.treeDepth != 1 {
		t.Fatalf("forwarded rel=%q depth=%d", fake.treeRel, fake.treeDepth)
	}
	if len(listing.Entries) != 2 || listing.Entries[0].Name != "src" {
		t.Fatalf("unexpected listing: %+v", listing.Entries)
	}
}

func TestBridgeWorkspaceFileCountForwards(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	fake.treeCount = dto.TreeCount{Files: 42, Dirs: 7}
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	count, err := bridge.WorkspaceFileCount()
	if err != nil {
		t.Fatal(err)
	}
	if count.Files != 42 || count.Dirs != 7 {
		t.Fatalf("unexpected count: %+v", count)
	}
}

func TestBridgeWorkspaceGitLogForwardsLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	fake.gitLog = dto.GitLogResult{Lines: []dto.GitLogLine{
		{Graph: "*", Commit: &dto.GitCommitNode{Hash: "aaaa", ShortHash: "a1b2", Author: "Alice", Date: "08-29", Subject: "fix: git log"}},
	}, Commits: []dto.GitCommitNode{{Hash: "aaaa"}}}
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := bridge.WorkspaceGitLog(20)
	if err != nil {
		t.Fatal(err)
	}
	if fake.gitLimit != 20 {
		t.Fatalf("forwarded limit=%d", fake.gitLimit)
	}
	if len(result.Lines) != 1 || result.Lines[0].Commit == nil || result.Lines[0].Commit.Subject != "fix: git log" {
		t.Fatalf("unexpected git log result: %+v", result.Lines)
	}
}

func TestBridgeForwardsScheduledTaskCommands(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	created, err := bridge.ScheduleTask(seelebridge.ScheduledTaskSpec{
		Name: "抓职位", Kind: seelebridge.ScheduledTaskCommand,
		Command: "auto_get_jobs", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "sched_1" || created.Name != "抓职位" {
		t.Fatalf("created = %+v", created)
	}
	if fake.scheduledSpec.Name != "抓职位" || fake.scheduledSpec.Command != "auto_get_jobs" {
		t.Fatalf("schedule not forwarded: %+v", fake.scheduledSpec)
	}

	runAt := time.Now().Add(time.Hour).Truncate(time.Second)
	if _, err := bridge.ScheduleTask(seelebridge.ScheduledTaskSpec{
		Name: "one-shot", Kind: seelebridge.ScheduledTaskPrompt, Prompt: "P", RunAt: runAt,
	}); err != nil {
		t.Fatal(err)
	}
	if !fake.scheduledSpec.RunAt.Equal(runAt) {
		t.Fatalf("one-shot runAt not forwarded: %+v", fake.scheduledSpec)
	}

	if err := bridge.CancelScheduledTask("sched_1"); err != nil {
		t.Fatal(err)
	}
	if fake.cancelledTaskID != "sched_1" {
		t.Fatalf("cancel not forwarded: %q", fake.cancelledTaskID)
	}
}

func TestBridgeSearchHistoryForwardsQueryAndReturnsAuthoritativeResult(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	fake.searchResult = seelexctxsearch.Result{
		Query: "数据库优化", TotalUnits: 3, IndexedFrames: 2,
		Hits: []seelexctxsearch.Hit{{SegmentID: "compact-a", From: 0, To: 1, Score: 2.5,
			Records: []seelexctxsearch.ChatRecord{{Role: "user", Content: "聊聊数据库索引"}}}},
	}
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := bridge.SearchHistory("数据库优化", 5)
	if err != nil {
		t.Fatal(err)
	}
	if fake.searchQuery != "数据库优化" || fake.searchLimit != 5 {
		t.Fatalf("search not forwarded: query=%q limit=%d", fake.searchQuery, fake.searchLimit)
	}
	if len(result.Hits) != 1 || result.Hits[0].SegmentID != "compact-a" {
		t.Fatalf("result not relayed as authoritative: %+v", result)
	}
}

// TestBridgeCancelChatForwardsOnce：取消对象的归属判断在 application 层
// （视图会话当前回合），Bridge 不得再用空 id 重试兜底。
func TestBridgeCancelChatForwardsOnce(t *testing.T) {
	fake := &recordingCancelApplication{fakeApplication: newFakeApplication()}
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.CancelChat("old-request") {
		t.Fatal("CancelChat should forward the application result")
	}
	if len(fake.calls) != 1 || fake.calls[0] != "old-request" {
		t.Fatalf("cancel calls = %#v, want a single forwarded request id", fake.calls)
	}
}

// TestBridgeSettlesSessionCatalogBeforeReturning 会改变会话目录的命令必须在
// 目录刷新收敛后才返回给 renderer（C3）：否则前端只能靠"列表为空就回填上一次
// 列表"掩盖竞态。
func TestBridgeSettlesSessionCatalogBeforeReturning(t *testing.T) {
	commands := []struct {
		name   string
		invoke func(*Bridge) error
	}{
		{"BeginNewSession", func(bridge *Bridge) error { return bridge.BeginNewSession() }},
		{"DeleteSession", func(bridge *Bridge) error { return bridge.DeleteSession("session-a") }},
		{"ForkSessionLatest", func(bridge *Bridge) error {
			_, err := bridge.ForkSessionLatest("session-a")
			return err
		}},
		{"SetSessionMeta", func(bridge *Bridge) error {
			return bridge.SetSessionMeta("session-a", true, "alias", 1)
		}},
	}
	for _, command := range commands {
		command := command
		t.Run(command.name, func(t *testing.T) {
			fake := newFakeApplication()
			fake.catalogGate = make(chan struct{})
			bridge, err := NewBridge(fake, Options{})
			if err != nil {
				t.Fatal(err)
			}
			returned := make(chan error, 1)
			go func() { returned <- command.invoke(bridge) }()

			select {
			case err := <-returned:
				t.Fatalf("%s returned before the session catalog settled (err=%v)", command.name, err)
			case <-time.After(50 * time.Millisecond):
			}
			close(fake.catalogGate)
			select {
			case err := <-returned:
				if err != nil {
					t.Fatalf("%s: %v", command.name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not return after the session catalog settled", command.name)
			}
			if fake.catalogSettles != 1 {
				t.Fatalf("catalog settle waits = %d, want exactly 1", fake.catalogSettles)
			}
		})
	}
}

type closeFakeApplication struct {
	*fakeApplication
	waitStarted   chan struct{}
	idle          chan struct{}
	mu            sync.Mutex
	beginCalls    int
	waitCalls     int
	cancelCalls   int
	waitStartOnce sync.Once
}

func (fake *closeFakeApplication) CancelChat(requestID string) bool {
	fake.mu.Lock()
	fake.cancelled = requestID
	fake.cancelCalls++
	fake.mu.Unlock()
	// 模拟取消后收尾完成（旧宿主只有视图会话，取消即空闲）。
	select {
	case <-fake.idle:
	default:
		close(fake.idle)
	}
	return true
}

func newCloseFakeApplication(running bool) *closeFakeApplication {
	fake := newFakeApplication()
	fake.snapshot.Chat.Running = running
	return &closeFakeApplication{
		fakeApplication: fake,
		waitStarted:     make(chan struct{}),
		idle:            make(chan struct{}),
	}
}

func (fake *closeFakeApplication) BeginGracefulShutdown() {
	fake.mu.Lock()
	fake.beginCalls++
	fake.mu.Unlock()
}

func (fake *closeFakeApplication) WaitForIdle(ctx context.Context) error {
	fake.mu.Lock()
	fake.waitCalls++
	fake.mu.Unlock()
	fake.waitStartOnce.Do(func() { close(fake.waitStarted) })
	select {
	case <-fake.idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// activityCloseFakeApplication 模拟生产 application.Service（G0c）：视图
// 快照可能空闲，但后台会话仍在运行；取消动作覆盖全部运行中会话。
type activityCloseFakeApplication struct {
	*closeFakeApplication
	anyRunning     bool
	cancelAllCalls int
}

func newActivityCloseFakeApplication(viewRunning, anyRunning bool) *activityCloseFakeApplication {
	return &activityCloseFakeApplication{
		closeFakeApplication: newCloseFakeApplication(viewRunning),
		anyRunning:           anyRunning,
	}
}

func (fake *activityCloseFakeApplication) AnyChatRunning() bool {
	return fake.anyRunning
}

// CancelAllChats 模拟取消全部运行中会话：取消后各 runChat 收尾并把进程
// 标记 idle（WaitForIdle 因此收敛）。
func (fake *activityCloseFakeApplication) CancelAllChats() {
	fake.mu.Lock()
	fake.cancelAllCalls++
	fake.mu.Unlock()
	select {
	case <-fake.idle:
	default:
		close(fake.idle)
	}
}

func TestCloseCoordinatorWaitsForRunningChat(t *testing.T) {
	t.Parallel()
	fake := newCloseFakeApplication(true)
	quit := make(chan struct{}, 1)
	coordinator := newCloseCoordinator(fake, func() { quit <- struct{}{} })

	if !coordinator.BeforeClose() {
		t.Fatal("running chat must prevent native window close")
	}
	if !coordinator.BeforeClose() {
		t.Fatal("repeated close must remain prevented while waiting")
	}
	select {
	case <-fake.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("idle wait did not start")
	}
	select {
	case <-quit:
		t.Fatal("application quit before chat became idle")
	default:
	}

	close(fake.idle)
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("application did not quit after chat became idle")
	}
	if coordinator.BeforeClose() {
		t.Fatal("coordinator must permit the programmatic close")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.beginCalls != 1 || fake.waitCalls != 1 {
		t.Fatalf("unexpected close coordination calls: begin=%d wait=%d", fake.beginCalls, fake.waitCalls)
	}
}

// TestCloseCoordinatorWaitsForBackgroundRunningChat G0c 回归：视图会话空闲
// （Snapshot.Chat.Running=false）而后台会话在跑时，窗口关闭必须被阻止并
// 进入 graceful drain——旧实现只看视图快照会直接关窗。
func TestCloseCoordinatorWaitsForBackgroundRunningChat(t *testing.T) {
	t.Parallel()
	fake := newActivityCloseFakeApplication(false, true)
	quit := make(chan struct{}, 1)
	coordinator := newCloseCoordinator(fake, func() { quit <- struct{}{} })

	if !coordinator.BeforeClose() {
		t.Fatal("background running chat must prevent native window close")
	}
	select {
	case <-fake.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("idle wait did not start for background running chat")
	}
	select {
	case <-quit:
		t.Fatal("application quit while the background session was still running")
	default:
	}

	close(fake.idle)
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("application did not quit after the background session became idle")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.beginCalls != 1 || fake.waitCalls != 1 || fake.cancelAllCalls != 0 {
		t.Fatalf("unexpected close coordination calls: begin=%d wait=%d cancelAll=%d",
			fake.beginCalls, fake.waitCalls, fake.cancelAllCalls)
	}
}

func TestCloseCoordinatorAllowsIdleClose(t *testing.T) {
	t.Parallel()
	fake := newCloseFakeApplication(false)
	coordinator := newCloseCoordinator(fake, nil)
	if coordinator.BeforeClose() {
		t.Fatal("idle application must close immediately")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.beginCalls != 1 || fake.waitCalls != 0 {
		t.Fatalf("unexpected idle close calls: begin=%d wait=%d", fake.beginCalls, fake.waitCalls)
	}
}

func TestCloseCoordinatorCancelsStalledChatAndQuits(t *testing.T) {
	t.Parallel()
	fake := newCloseFakeApplication(true)
	quit := make(chan struct{}, 1)
	coordinator := newCloseCoordinatorWithTimeout(fake, func() { quit <- struct{}{} }, 10*time.Millisecond)

	if !coordinator.BeforeClose() {
		t.Fatal("running chat must initially prevent native window close")
	}
	select {
	case <-fake.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("idle wait did not start")
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("stalled chat did not force the native quit path")
	}
	if coordinator.BeforeClose() {
		t.Fatal("forced quit must permit the programmatic close")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	// 旧宿主（无 AnyChatRunning/CancelAllChats）：超时回退到只取消视图会话，
	// 随后等待取消收尾（第二次 WaitForIdle 立即返回）。
	if fake.beginCalls != 1 || fake.waitCalls != 2 || fake.cancelCalls != 1 || fake.cancelled != "" {
		t.Fatalf("stalled close calls = begin:%d wait:%d cancel:%d request:%q",
			fake.beginCalls, fake.waitCalls, fake.cancelCalls, fake.cancelled)
	}
}

// TestCloseCoordinatorCancelsAllRunningSessionsAfterTimeout G0c 超时路径：
// 生产宿主取消全部运行中会话（不只视图），并等待其收尾（逐会话 flush）。
func TestCloseCoordinatorCancelsAllRunningSessionsAfterTimeout(t *testing.T) {
	t.Parallel()
	fake := newActivityCloseFakeApplication(false, true)
	quit := make(chan struct{}, 1)
	coordinator := newCloseCoordinatorWithTimeout(fake, func() { quit <- struct{}{} }, 10*time.Millisecond)

	if !coordinator.BeforeClose() {
		t.Fatal("background running chat must initially prevent native window close")
	}
	select {
	case <-fake.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("idle wait did not start")
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("stalled background chat did not force the native quit path")
	}
	if coordinator.BeforeClose() {
		t.Fatal("forced quit must permit the programmatic close")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.beginCalls != 1 || fake.waitCalls != 2 || fake.cancelAllCalls != 1 || fake.cancelCalls != 0 {
		t.Fatalf("stalled close calls = begin:%d wait:%d cancelAll:%d legacyCancel:%d",
			fake.beginCalls, fake.waitCalls, fake.cancelAllCalls, fake.cancelCalls)
	}
}

func TestBridgeRelaysApplicationEvents(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}

	emitted := make(chan emittedEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	defer bridge.Stop()

	ready := waitEmitted(t, emitted)
	if ready.name != "seelex:ready" {
		t.Fatalf("first event = %q, want seelex:ready", ready.name)
	}

	published := fake.hub.Publish(application.EventRuntimeChanged, 2, "", map[string]string{"plugin": "default"})
	relayed := waitEmitted(t, emitted)
	if relayed.name != eventName {
		t.Fatalf("relayed event name = %q, want %q", relayed.name, eventName)
	}
	event, ok := relayed.payload.(application.Event)
	if !ok {
		t.Fatalf("relayed payload type = %T", relayed.payload)
	}
	if event.ProtocolVersion != application.ProtocolVersion || event.Seq != published.Seq || event.Kind != published.Kind {
		t.Fatalf("relayed event = %#v, want %#v", event, published)
	}
}

func TestBridgeRelaysToolCompletedEventToFrontend(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}

	emitted := make(chan emittedEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	defer bridge.Stop()
	_ = waitEmitted(t, emitted)

	want := application.Message{
		ID: "tool-result-1", Role: "tool_result", Content: `{"stdout":"ok"}`,
		Tool: &application.ToolCall{ID: "tool-1", Name: "bash", Status: "success", Result: `{"stdout":"ok"}`},
	}
	published := fake.hub.Publish(application.EventToolCompleted, 2, "request-1", want)
	relayed := waitEmitted(t, emitted)
	if relayed.name != eventName {
		t.Fatalf("relayed event name = %q, want %q", relayed.name, eventName)
	}
	event, ok := relayed.payload.(application.Event)
	if !ok {
		t.Fatalf("relayed payload type = %T", relayed.payload)
	}
	if event.Kind != application.EventToolCompleted || event.Seq != published.Seq || event.RequestID != "request-1" {
		t.Fatalf("relayed tool completion = %#v, want %#v", event, published)
	}
	var got application.Message
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool == nil || got.Tool.Name != "bash" || got.Tool.Status != "success" || got.Tool.Result != want.Tool.Result {
		t.Fatalf("frontend tool completion payload = %#v", got)
	}
}

func TestBridgeRelaysSubagentToolEventsToSeelexEvent(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}

	emitted := make(chan emittedEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	defer bridge.Stop()
	_ = waitEmitted(t, emitted)

	payload := application.SubagentToolEvent{
		ID: "subtool-1", NodeID: "worker", Name: "read_file",
		Status: "success", Result: "done",
	}
	published := fake.hub.Publish(application.EventSubagentToolCompleted, 3, "request-1", payload)
	relayed := waitEmitted(t, emitted)
	if relayed.name != eventName {
		t.Fatalf("relayed event name = %q, want %q", relayed.name, eventName)
	}
	event, ok := relayed.payload.(application.Event)
	if !ok {
		t.Fatalf("relayed payload type = %T", relayed.payload)
	}
	if event.Kind != application.EventSubagentToolCompleted || event.Seq != published.Seq || event.RequestID != "request-1" {
		t.Fatalf("relayed event = %#v, want subagent completion %#v", event, published)
	}
	var decoded application.SubagentToolEvent
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != payload.ID || decoded.NodeID != payload.NodeID || decoded.Result != "done" {
		t.Fatalf("relayed subagent payload = %#v", decoded)
	}
}

func TestEmbeddedFrontendExists(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"frontend/dist/index.html", "frontend/dist/app.js", "frontend/dist/components.js",
		"frontend/dist/protocol.js", "frontend/dist/client-state.js", "frontend/dist/conversation-view.js",
		"frontend/dist/chat-view.js", "frontend/dist/runtime-events.js", "frontend/dist/effort-control.js", "frontend/dist/plan-dsl.js", "frontend/dist/styles.css",
	} {
		if _, err := embeddedFrontend.ReadFile(name); err != nil {
			t.Fatalf("embedded frontend %q: %v", name, err)
		}
	}
	index, err := embeddedFrontend.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := embeddedFrontend.ReadFile("frontend/dist/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="session-list"`) || !strings.Contains(string(script), "renderSessions") {
		t.Fatal("embedded frontend does not include the session list")
	}
	if !strings.Contains(string(script), `session.name || shortSessionID(session.id)`) ||
		!strings.Contains(string(script), `data-session="${escapeHtml(session.id)}"`) {
		t.Fatal("session list must render display names while keeping IDs as action keys")
	}
	if !strings.Contains(string(script), `from "./plan-dsl.js"`) {
		t.Fatal("embedded frontend does not load the Plan JSON DSL renderer")
	}
	if !strings.Contains(string(script), `invoke("AckEvents"`) ||
		!strings.Contains(string(script), `invoke("ReplayEvents"`) {
		t.Fatal("embedded frontend must report its applied delivery_seq and replay gaps incrementally (C4)")
	}
	if strings.Contains(string(script), "active-chat-sync") {
		t.Fatal("running-chat reconciliation must not fall back to periodic Snapshot polling")
	}
	if strings.Contains(string(script), "let fullAccessOn") ||
		!strings.Contains(string(script), `client.current()?.runtime?.full_access`) ||
		!strings.Contains(string(script), `Boolean(runtime.full_access)`) {
		t.Fatal("Full Access control must use the authoritative GUI backend snapshot")
	}
	if !strings.Contains(string(script), `from "./perf-hooks.js"`) || !strings.Contains(string(script), `createPerfHooks`) || !strings.Contains(string(script), `invoke("PerfStats")`) || !strings.Contains(string(script), `invoke("ToolResultContent"`) {
		t.Fatal("embedded frontend must wire performance hooks and result_ref loading")
	}
	if !strings.Contains(string(index), `id="perf-badge-host"`) {
		t.Fatal("embedded frontend must include the perf badge host")
	}
	components, err := embeddedFrontend.ReadFile("frontend/dist/components.js")
	if err != nil {
		t.Fatal(err)
	}
	componentSource := string(components)
	if !strings.Contains(componentSource, `class="chat-chip is-tool"`) || !strings.Contains(componentSource, `data-trajectory-key`) || !strings.Contains(componentSource, `reasoning_content`) {
		t.Fatal("chat view must collapse tool calls and thinking into one-line chips linked to the trajectory view")
	}
	trajectory, err := embeddedFrontend.ReadFile("frontend/dist/trajectory.js")
	if err != nil {
		t.Fatal(err)
	}
	trajectorySource := string(trajectory)
	if !strings.Contains(trajectorySource, `class="io-label">IN`) || !strings.Contains(trajectorySource, `class="io-label">OUT`) || !strings.Contains(trajectorySource, "limitText(output, 4000") || !strings.Contains(trajectorySource, `data-load-ref`) || !strings.Contains(trajectorySource, `io-collapse`) {
		t.Fatal("trajectory detail must split IN/OUT, cap preview (4KB), and offer result_ref expansion for truncated output")
	}
	if !strings.Contains(trajectorySource, "renderContextAxis") || !strings.Contains(trajectorySource, "trajectory-think") {
		t.Fatal("trajectory view must include the context axis and the full THINK panel")
	}
	if !strings.Contains(string(index), `data-icon="command"`) || !strings.Contains(string(index), `data-icon="send"`) {
		t.Fatal("primary GUI actions must use icon controls")
	}
	html := string(index)
	leftStart := strings.Index(html, `<aside class="left-panel panel">`)
	rightStart := strings.Index(html, `<aside class="right-panel panel">`)
	if leftStart < 0 || rightStart < 0 {
		t.Fatal("embedded frontend sidebars are incomplete")
	}
	leftEnd := strings.Index(html[leftStart:], `</aside>`)
	rightEnd := strings.Index(html[rightStart:], `</aside>`)
	runtimeStart := strings.Index(html, `id="runtime-modal"`)
	effortStart := strings.Index(html, `id="effort-control"`)
	if leftEnd < 0 || rightEnd < 0 || runtimeStart < 0 || effortStart < 0 {
		t.Fatal("embedded frontend layout regions are incomplete")
	}
	leftPanel := html[leftStart : leftStart+leftEnd]
	rightPanel := html[rightStart : rightStart+rightEnd]
	runtimeModal := html[runtimeStart:]
	if strings.Contains(leftPanel, `id="plugin-list"`) || strings.Contains(rightPanel, `id="plugin-list"`) || !strings.Contains(runtimeModal, `id="plugin-list"`) {
		t.Fatal("plugins must be rendered in the runtime modal")
	}
	if effortStart > runtimeStart || strings.Contains(runtimeModal, `id="effort-range"`) || !strings.Contains(string(script), "createEffortControl") {
		t.Fatal("Effort must be a persistent topbar control outside the runtime modal")
	}
	if !strings.Contains(rightPanel, `id="project-status"`) || !strings.Contains(rightPanel, `id="worktree-view"`) || !strings.Contains(rightPanel, `id="file-count"`) {
		t.Fatal("right sidebar must render project status and the work tree panel")
	}
	if !strings.Contains(rightPanel, `id="work-table-open"`) || strings.Contains(runtimeModal, `id="work-table-open"`) {
		t.Fatal("工作表格入口按钮必须常驻右侧栏")
	}
	if !strings.Contains(html, `id="work-table-modal-view"`) || !strings.Contains(runtimeModal, `id="work-table-modal-view"`) {
		t.Fatal("完整工作表格必须挂载在详情弹窗中（工作台窄，详情弹窗看全）")
	}
	if !strings.Contains(string(script), `invoke("BeginNewSession")`) || strings.Contains(string(script), `invoke("Submit", "/new")`) {
		t.Fatal("GUI new-session action must enter a lazy draft instead of eagerly creating a session")
	}
	if !strings.Contains(string(script), "await beginNewSession();\n    await invoke(\"BindWorkspace\", workspaceID);") {
		t.Fatal("workspace new-session must draft first (unbound) and then bind, so plain task sessions stay truly unassociated")
	}
	if !strings.Contains(string(script), `invoke("ResumeSession", sessionID)`) || strings.Contains(string(script), "/resume ${button.dataset.session}") {
		t.Fatal("GUI session rows must use the direct resume boundary")
	}
	if strings.Contains(string(script), "currentSession") || strings.Contains(string(script), "bindings") {
		t.Fatal("session-resume callback must use its render arguments, not undefined globals")
	}
	if strings.Contains(string(script), `session-draft-row`) || strings.Contains(string(script), `尚未创建 Session ID`) {
		t.Fatal("an unmaterialized draft must not create a row in the session list")
	}
	if !strings.Contains(string(script), `session.name || shortSessionID(session.id)`) {
		t.Fatal("materialized session rows must show display names while retaining ID-only action keys")
	}
	if !strings.Contains(html, `id="command-modal"`) || !strings.Contains(string(script), "updateInlineSuggestions") {
		t.Fatal("embedded frontend does not include GUI command mode")
	}
}

func waitEmitted(t *testing.T, events <-chan emittedEvent) emittedEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for GUI event")
		return emittedEvent{}
	}
}

func TestBridgeUpdateWorkItemStatusForwardsAndReturnsErrors(t *testing.T) {
	app := newFakeApplication()
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.UpdateWorkItemStatus("todo:0", "doing"); err != nil {
		t.Fatal(err)
	}
	if app.workItemID != "todo:0" || app.workItemStatus != "doing" {
		t.Fatalf("forwarded (%q, %q)", app.workItemID, app.workItemStatus)
	}

	app.workItemErr = errors.New("todolist: index out of range")
	if err := bridge.UpdateWorkItemStatus("todo:99", "done"); err == nil || err.Error() != "todolist: index out of range" {
		t.Fatalf("error must be transparent, got %v", err)
	}
}

func TestBridgeForwardsForkSession(t *testing.T) {
	app := newFakeApplication()
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	childID, err := bridge.ForkSessionLatest("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if app.forkedSession != "session-1" || childID != "child-session-1" {
		t.Fatalf("fork forwarded = %q child = %q", app.forkedSession, childID)
	}
}
