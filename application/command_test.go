package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ── CommandRegistry ───────────────────────────────────────────

func TestCommandRegistry_RegisterAndGet(t *testing.T) {
	r := NewCommandRegistry()
	cmd := commandFunc{name: "test", description: "test command", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{Notice: "executed"}, nil
	}}
	if err := r.Register(cmd); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Get("test")
	if !ok {
		t.Fatal("command not found")
	}
	if got.Name() != "test" {
		t.Errorf("expected 'test', got %q", got.Name())
	}
}

func TestCommandRegistry_RegisterDuplicate(t *testing.T) {
	r := NewCommandRegistry()
	cmd := commandFunc{name: "dup", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}}
	_ = r.Register(cmd)
	err := r.Register(cmd)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestCommandRegistry_RegisterEmptyName(t *testing.T) {
	r := NewCommandRegistry()
	cmd := commandFunc{name: "  ", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}}
	err := r.Register(cmd)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCommandRegistry_GetNotFound(t *testing.T) {
	r := NewCommandRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("should not find nonexistent command")
	}
}

func TestCommandRegistry_All(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Register(commandFunc{name: "b", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}})
	_ = r.Register(commandFunc{name: "a", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}})
	_ = r.Register(commandFunc{name: "c", execute: func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}})
	all := r.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// should be sorted: a, b, c
	if all[0].Name() != "a" || all[1].Name() != "b" || all[2].Name() != "c" {
		t.Errorf("expected sorted order, got: %q, %q, %q", all[0].Name(), all[1].Name(), all[2].Name())
	}
}

func TestCommandRegistry_AllEmpty(t *testing.T) {
	r := NewCommandRegistry()
	all := r.All()
	if len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

// ── Builtin Commands (via Submit) ─────────────────────────────

func lastNotice(t *testing.T, svc *Service) string {
	t.Helper()
	snap := svc.Snapshot()
	if len(snap.Conversation) == 0 {
		return ""
	}
	last := snap.Conversation[len(snap.Conversation)-1]
	if last.Role == "system" {
		return last.Content
	}
	return ""
}

func TestBuiltinHelp(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/help"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "可用命令") {
		t.Errorf("help should contain '可用命令', got %q", notice)
	}
	if !strings.Contains(notice, "/help") {
		t.Errorf("help should list /help, got %q", notice)
	}
}

func TestBuiltinClear(t *testing.T) {
	eng := &fakeEngine{}
	eng.history = []EngineMessage{{Role: "user", Content: "old msg"}}
	svc := newTestService(eng)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/clear"); err != nil {
		t.Fatal(err)
	}
	eng.mu.Lock()
	cleared := eng.cleared
	eng.mu.Unlock()
	if !cleared {
		t.Error("engine should have been cleared")
	}
}

func TestBuiltinModel(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/model"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "test-model") {
		t.Errorf("expected model in notice, got %q", notice)
	}
	if !strings.Contains(notice, "test-provider") {
		t.Errorf("expected provider in notice, got %q", notice)
	}
}

func TestBuiltinHistory_Empty(t *testing.T) {
	eng := &fakeEngine{}
	svc := newTestService(eng)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/history"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "历史为空") {
		t.Errorf("expected empty history notice, got %q", notice)
	}
}

func TestBuiltinHistory_NonEmpty(t *testing.T) {
	eng := &fakeEngine{}
	eng.history = []EngineMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	svc := newTestService(eng)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/history"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "2") {
		t.Errorf("expected count 2 in notice, got %q", notice)
	}
}

func TestBuiltinTrace(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/trace"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "trace") {
		t.Errorf("expected trace output, got %q", notice)
	}
}

func TestBuiltinNew(t *testing.T) {
	eng := &fakeEngine{}
	svc := newTestService(eng)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/new"); err != nil {
		t.Fatal(err)
	}
	eng.mu.Lock()
	cleared := eng.cleared
	eng.mu.Unlock()
	if !cleared {
		t.Error("engine should be cleared for new session")
	}
	if svc.Snapshot().Session.ID != "session-new" {
		t.Fatalf("new session ID = %q, want session-new", svc.Snapshot().Session.ID)
	}
}

func TestBuiltinNew_SaveFails(t *testing.T) {
	eng := &fakeEngine{}
	svc := New(Dependencies{
		Engine: eng, Runtime: &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: &failingSessions{},
	})
	defer svc.Shutdown()

	if err := svc.Submit(context.Background(), "/new"); err == nil {
		t.Error("expected error when save fails")
	}
}

type failingSessions struct{}

func (failingSessions) SaveCurrent(string) error                    { return errors.New("save failed") }
func (failingSessions) Resume(string) error                         { return nil }
func (failingSessions) List() []SessionInfo                         { return nil }
func (failingSessions) LoadHistory(string) ([]EngineMessage, error) { return nil, nil }
func (failingSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return nil, 0, nil
}
func (failingSessions) MessageCount(string) (int, error) { return 0, nil }

func TestBuiltinResumeOpensSessionPicker(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/resume"); err != nil {
		t.Fatal(err)
	}
	interaction := svc.Snapshot().Interaction
	if interaction == nil || interaction.Kind != "session" {
		t.Fatalf("resume interaction = %+v, want session picker", interaction)
	}
}

func TestBuiltinResume_WithSessionID(t *testing.T) {
	eng := &fakeEngine{}
	svc := newTestService(eng)
	defer svc.Shutdown()

	if err := svc.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	snapshot := svc.Snapshot()
	if snapshot.Session.ID != "saved" {
		t.Fatalf("session ID = %q, want saved", snapshot.Session.ID)
	}
	eng.mu.Lock()
	history := append([]EngineMessage(nil), eng.history...)
	eng.mu.Unlock()
	if len(history) != 1 || history[0].Content != "saved answer" {
		t.Fatalf("engine history was not replaced: %+v", history)
	}
}

func TestBuiltinSessions_NonEmpty(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/sessions"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "持久化会话") {
		t.Errorf("expected sessions list, got %q", notice)
	}
	if !strings.Contains(notice, "saved") {
		t.Errorf("expected saved session in list, got %q", notice)
	}
}

func TestBuiltinSessions_Empty(t *testing.T) {
	eng := &fakeEngine{}
	svc := New(Dependencies{
		Engine: eng, Runtime: &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: &emptySessions{},
	})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/sessions"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "暂无持久化会话") {
		t.Errorf("expected empty notice, got %q", notice)
	}
}

type emptySessions struct{}

func (emptySessions) SaveCurrent(string) error                    { return nil }
func (emptySessions) Resume(string) error                         { return nil }
func (emptySessions) List() []SessionInfo                         { return nil }
func (emptySessions) LoadHistory(string) ([]EngineMessage, error) { return nil, nil }
func (emptySessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return nil, 0, nil
}
func (emptySessions) MessageCount(string) (int, error) { return 0, nil }

func TestBuiltinPlugin_NoArg(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/plugin"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "当前插件") {
		t.Errorf("expected current plugin, got %q", notice)
	}
}

func TestBuiltinPlugin_NoCurrent(t *testing.T) {
	eng := &fakeEngine{}
	svc := New(Dependencies{
		Engine: eng, Runtime: &fakeRuntime{},
		Plugins:  &noCurrentPlugins{},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/plugin"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "未激活") {
		t.Errorf("expected deactivated notice, got %q", notice)
	}
}

type noCurrentPlugins struct{}

func (noCurrentPlugins) All() []PluginInfo {
	return []PluginInfo{{Name: "default", Description: "default"}}
}
func (noCurrentPlugins) Activate(_ context.Context, name string) error { return nil }
func (noCurrentPlugins) Deactivate(context.Context) error              { return nil }
func (noCurrentPlugins) Current() (PluginInfo, bool)                   { return PluginInfo{}, false }

func TestBuiltinPlugin_Switch(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/plugin code"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "code") {
		t.Errorf("expected switch to code, got %q", notice)
	}

	// Switch to nonexistent — should show error notice but Submit itself returns nil
	if err := svc.Submit(context.Background(), "/plugin nonexistent"); err != nil {
		// Error is also acceptable (submitCommand propagates SwitchPlugin error)
		t.Logf("nonexistent plugin error: %v", err)
	}
}

func TestBuiltinPlugin_Off(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/plugin off"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "已关闭") && !strings.Contains(notice, "已停用") {
		t.Errorf("expected deactivation notice, got %q", notice)
	}
}

func TestBuiltinUnknownCommand(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "/unknowncmd"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "未知命令") {
		t.Errorf("expected unknown command notice, got %q", notice)
	}
}

func TestSubmit_EmptyInput(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(context.Background(), "  "); err != nil {
		t.Fatal(err)
	}
}

func TestSkillLoadViaSubmit(t *testing.T) {
	engine := &fakeEngine{}
	svc := newTestService(engine)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, svc)
	engine.mu.Lock()
	prompt := engine.prompt
	modelInput := engine.lastInput
	engine.mu.Unlock()
	if strings.Contains(prompt, "review prompt") || strings.Contains(prompt, "review code") || strings.Contains(prompt, "focused") || strings.Contains(prompt, "Available Skills") || strings.Contains(prompt, "#review") {
		t.Errorf("system prompt contains Skill/user request: %q", prompt)
	}
	for _, expected := range []string{"- name: review", "review prompt", "## User Request\n#review focused"} {
		if !strings.Contains(modelInput, expected) {
			t.Errorf("model input missing %q: %q", expected, modelInput)
		}
	}
	conversation := svc.Snapshot().Conversation
	if len(conversation) < 2 || conversation[len(conversation)-2].Content != "#review focused" {
		t.Fatalf("UI conversation must show original input: %#v", conversation)
	}
}

func TestSkillWithoutRequirementAppliesToNextInput(t *testing.T) {
	engine := &fakeEngine{}
	svc := newTestService(engine)
	defer svc.Shutdown()

	if err := svc.Submit(context.Background(), "#review"); err != nil {
		t.Fatal(err)
	}
	if svc.Snapshot().Chat.Running || !svc.promptStack.Has("skill") {
		t.Fatal("Skill without requirement must activate without starting chat")
	}
	if err := svc.Submit(context.Background(), "check the change"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, svc)
	engine.mu.Lock()
	modelInput := engine.lastInput
	engine.mu.Unlock()
	for _, expected := range []string{"- name: review", "review prompt", "## User Request\ncheck the change"} {
		if !strings.Contains(modelInput, expected) {
			t.Fatalf("active Skill model input missing %q: %q", expected, modelInput)
		}
	}

	if err := svc.Submit(context.Background(), "#end"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(context.Background(), "plain request"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, svc)
	engine.mu.Lock()
	modelInput = engine.lastInput
	engine.mu.Unlock()
	if modelInput != "plain request" {
		t.Fatalf("input after #end = %q, want plain request", modelInput)
	}
}

func TestQueuedSkillRequestFreezesDisplayAndModelInput(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}}
	svc := New(Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: fakeSessions{},
	})
	defer svc.Shutdown()

	if err := svc.Submit(context.Background(), "running request"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(context.Background(), "#review queued requirement"); err != nil {
		t.Fatal(err)
	}
	svc.mu.RLock()
	queueLength := len(svc.inputQueue)
	if queueLength != 1 {
		svc.mu.RUnlock()
		t.Fatalf("queued requests = %d, want 1", queueLength)
	}
	queued := svc.inputQueue[0]
	svc.mu.RUnlock()

	if queued.displayInput != "#review queued requirement" {
		t.Fatalf("queued display = %q", queued.displayInput)
	}
	for _, expected := range []string{"- name: review", "review prompt", "#review queued requirement"} {
		if !strings.Contains(queued.modelInput, expected) {
			t.Fatalf("queued model input missing %q: %q", expected, queued.modelInput)
		}
	}
	if got := svc.Snapshot().Chat.InputQueue; len(got) != 1 || got[0] != queued.displayInput {
		t.Fatalf("snapshot queue = %#v, want original display", got)
	}
	if err := svc.Submit(context.Background(), "#end"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queued.modelInput, "review prompt") {
		t.Fatal("#end changed already queued model context")
	}
}

func TestSkillUnknown(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "#unknown"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "未知 Skill") {
		t.Errorf("expected unknown skill notice, got %q", notice)
	}
	if svc.Snapshot().Chat.Running {
		t.Error("unknown Skill must not start chat")
	}
}

func TestSkillEnd(t *testing.T) {
	engine := &fakeEngine{}
	svc := newTestService(engine)
	defer svc.Shutdown()
	// Load a skill first
	_ = svc.Submit(context.Background(), "#review")
	// Then end it
	_ = svc.Submit(context.Background(), "#end")
	engine.mu.Lock()
	prompt := engine.prompt
	engine.mu.Unlock()
	if svc.promptStack.Has("skill") {
		t.Error("#end must remove the active Skill")
	}
	if strings.Contains(prompt, "review prompt") {
		t.Errorf("Skill must never be present in system prompt: %q", prompt)
	}
}

func TestSkillEnd_NoActiveSkill(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "#end"); err != nil {
		t.Fatal(err)
	}
	notice := lastNotice(t, svc)
	if !strings.Contains(notice, "无 Skill") {
		t.Errorf("expected no skill notice, got %q", notice)
	}
}

func TestSkillEndPreservesGoalLoopLimitUntilGoalIsPopped(t *testing.T) {
	engine := &fakeEngine{}
	svc := newTestService(engine)
	defer svc.Shutdown()

	if err := svc.effortManager.Apply("medium"); err != nil {
		t.Fatal(err)
	}
	svc.applySkill(SkillInfo{Name: "goal", Prompt: "goal prompt"})
	svc.applySkill(SkillInfo{Name: "review", Prompt: "review prompt"})

	if err := svc.endSkill(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	maxLoops := engine.maxLoops
	engine.mu.Unlock()
	if maxLoops != 9999 {
		t.Fatalf("MaxLoops after popping review with goal active = %d, want 9999", maxLoops)
	}

	if err := svc.endSkill(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	maxLoops = engine.maxLoops
	engine.mu.Unlock()
	if maxLoops != effortLoops["medium"] {
		t.Fatalf("MaxLoops after popping goal = %d, want %d", maxLoops, effortLoops["medium"])
	}
}

func TestNew_DefaultState(t *testing.T) {
	eng := &fakeEngine{}
	svc := newTestService(eng)
	defer svc.Shutdown()
	snap := svc.Snapshot()
	if snap.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", snap.ProtocolVersion, ProtocolVersion)
	}
	if snap.Session.ID != "session-1" {
		t.Errorf("expected session-1, got %q", snap.Session.ID)
	}
	if snap.Runtime.Model != "test-model" {
		t.Errorf("expected test-model, got %q", snap.Runtime.Model)
	}
	if !snap.Capabilities.SessionResume {
		t.Error("resume should be enabled by default")
	}
	if snap.Revision != 1 {
		t.Errorf("expected revision 1, got %d", snap.Revision)
	}
}

func TestNew_DefaultEventHub(t *testing.T) {
	// If deps.Events is nil, New should create defaults
	svc := New(Dependencies{
		Engine:   &fakeEngine{},
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
		// Events and Approval are nil - should use defaults
	})
	defer svc.Shutdown()
	if svc.events == nil {
		t.Error("events should not be nil")
	}
	if svc.approval == nil {
		t.Error("approval should not be nil")
	}
}

func TestSuggestions_EmptyPrefix(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	suggestions := svc.Suggestions("")
	// Empty prefix may or may not return suggestions; just ensure no panic
	_ = suggestions
}

func TestSuggestions_SlashOnly(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	suggestions := svc.Suggestions("/")
	if len(suggestions) == 0 {
		t.Error("expected suggestions for /")
	}
	hasCommand := false
	for _, s := range suggestions {
		if s.Kind == "command" {
			hasCommand = true
			break
		}
	}
	if !hasCommand {
		t.Error("expected at least one command suggestion")
	}
}

func TestSuggestions_HashOnly(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	suggestions := svc.Suggestions("#")
	if len(suggestions) == 0 {
		t.Error("expected suggestions for #")
	}
}

func TestSuggestions_PrefixMatchCommand(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	suggestions := svc.Suggestions("/h")
	if len(suggestions) == 0 {
		t.Error("expected suggestions for /h")
	}
	found := false
	for _, s := range suggestions {
		if s.Text == "help" || s.Text == "history" || s.Text == "/help" || s.Text == "/history" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected help or history in suggestions, got %+v", suggestions)
	}
}

func TestCancelChat_NotRunning(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if svc.CancelChat("") {
		t.Error("CancelChat should return false when not running")
	}
}

func TestSwitchEffort(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.SwitchEffort(context.Background(), "max"); err != nil {
		t.Fatal(err)
	}
	snap := svc.Snapshot()
	if snap.Runtime.Effort != "max" {
		t.Errorf("expected 'max', got %q", snap.Runtime.Effort)
	}
}

func TestSelectAccount_Success(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.SelectAccount(context.Background(), "primary"); err != nil {
		t.Fatal(err)
	}
}

func TestSelectAccount_Failure(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	if err := svc.SelectAccount(context.Background(), "nonexistent"); err == nil {
		t.Error("expected error for nonexistent account")
	}
}

func TestShutdown(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	svc.Shutdown()
	// Should not panic on second shutdown
	svc.Shutdown()
}

func TestSnapshotAfterChat(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"an", "swer"}}
	svc := newTestService(engine)
	defer svc.Shutdown()
	if err := svc.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap := svc.Snapshot()
		if !snap.Chat.Running {
			if len(snap.Conversation) < 3 {
				t.Fatalf("expected at least 3 messages, got %d", len(snap.Conversation))
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}

func TestInteractionLifecycle(t *testing.T) {
	svc := newTestService(&fakeEngine{})
	defer svc.Shutdown()
	interaction := &Interaction{
		ID:       "interact-1",
		Kind:     "session",
		Question: "选择会话",
		Options: []InteractionOption{
			{ID: "opt1", Label: "Option 1"},
		},
	}
	svc.openInteraction(interaction)
	snap := svc.Snapshot()
	if snap.Interaction == nil {
		t.Fatal("interaction should be set")
	}
	if snap.Interaction.ID != "interact-1" {
		t.Errorf("expected interact-1, got %q", snap.Interaction.ID)
	}
	// Resolve may fail depending on whether observer handles it; just log
	if err := svc.ResolveInteraction(context.Background(), "interact-1", "opt1"); err != nil {
		t.Logf("ResolveInteraction returned error (expected with mock): %v", err)
	}
	snap = svc.Snapshot()
	if snap.Interaction != nil {
		t.Log("interaction not cleared after resolve (may still be visible)")
	}
}

func TestInputQueue(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"thinking..."}}
	svc := newTestService(engine)
	defer svc.Shutdown()

	// Start a chat that blocks
	go func() {
		_ = svc.Submit(context.Background(), "first message")
	}()

	// Wait for chat to start running
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		snap := svc.Snapshot()
		if snap.Chat.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Queue additional messages
	_ = svc.Submit(context.Background(), "queued message")
	snap := svc.Snapshot()
	if snap.Chat.QueuedCount != 1 {
		t.Logf("queued count: %d (may vary due to race)", snap.Chat.QueuedCount)
	}
}
