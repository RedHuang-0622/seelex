package gui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
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
	resumedSession   string
	scheduledSpec    seelebridge.ScheduledTaskSpec
	cancelledTaskID  string
	searchQuery      string
	searchLimit      int
	searchResult     seelexctxsearch.Result
}

type staleCancelApplication struct {
	*fakeApplication
	calls []string
}

func (fake *staleCancelApplication) CancelChat(requestID string) bool {
	fake.calls = append(fake.calls, requestID)
	return len(fake.calls) > 1
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
func (fake *fakeApplication) ResumeSession(sessionID string) error {
	fake.resumedSession = sessionID
	return nil
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
func (fake *fakeApplication) ScheduleTask(_ context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	fake.scheduledSpec = spec
	return &seelebridge.ScheduledTaskStatus{ID: "sched_1", Name: spec.Name, Kind: string(spec.Kind), Enabled: spec.Enabled}, nil
}
func (fake *fakeApplication) CancelScheduledTask(id string) error {
	fake.cancelledTaskID = id
	return nil
}
func (fake *fakeApplication) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error) {
	fake.searchQuery = query
	fake.searchLimit = limit
	return fake.searchResult, nil
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
	if bridge.Info().Title != "Seelex Test" || bridge.Snapshot().Runtime.Model != "test-model" {
		t.Fatal("bridge metadata or snapshot mismatch")
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

func TestBridgeCancelChatRetriesAgainstActiveRequestWhenRendererIDIsStale(t *testing.T) {
	fake := &staleCancelApplication{fakeApplication: newFakeApplication()}
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.CancelChat("old-request") {
		t.Fatal("CancelChat did not retry against the active request")
	}
	if len(fake.calls) != 2 || fake.calls[1] != "" {
		t.Fatalf("cancel calls = %#v, want stale ID followed by active-request cancellation", fake.calls)
	}
}

type closeFakeApplication struct {
	*fakeApplication
	waitStarted chan struct{}
	idle        chan struct{}

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
	if fake.beginCalls != 1 || fake.waitCalls != 1 || fake.cancelCalls != 1 || fake.cancelled != "" {
		t.Fatalf("stalled close calls = begin:%d wait:%d cancel:%d request:%q", fake.beginCalls, fake.waitCalls, fake.cancelCalls, fake.cancelled)
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
	if !strings.Contains(string(script), `from "./active-chat-sync.js"`) {
		t.Fatal("embedded frontend does not reconcile a running chat from the authoritative Bridge Snapshot")
	}
	if strings.Contains(string(script), "let fullAccessOn") ||
		!strings.Contains(string(script), `client.current()?.runtime?.full_access`) ||
		!strings.Contains(string(script), `Boolean(runtime.full_access)`) {
		t.Fatal("Full Access control must use the authoritative GUI backend snapshot")
	}
	components, err := embeddedFrontend.ReadFile("frontend/dist/components.js")
	if err != nil {
		t.Fatal(err)
	}
	componentSource := string(components)
	if !strings.Contains(componentSource, `renderIOPanel("IN"`) || !strings.Contains(componentSource, `renderIOPanel("OUT"`) || !strings.Contains(componentSource, "2400, 40") {
		t.Fatal("tool component must split IN/OUT and limit default output")
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
	if !strings.Contains(rightPanel, `id="project-status"`) || !strings.Contains(rightPanel, `id="project-sources"`) {
		t.Fatal("right sidebar must render project status and sources")
	}
	if !strings.Contains(rightPanel, `id="plan-view"`) || strings.Contains(runtimeModal, `id="plan-view"`) {
		t.Fatal("Plan DSL must be mounted in the persistent right sidebar")
	}
	if !strings.Contains(string(script), `invoke("BeginNewSession")`) || strings.Contains(string(script), `invoke("Submit", "/new")`) {
		t.Fatal("GUI new-session action must enter a lazy draft instead of eagerly creating a session")
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
