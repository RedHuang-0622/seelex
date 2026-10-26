package residency

import (
	"context"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
	middleresidency "github.com/RedHuang-0622/seelex/middle/residency"
)

type snapshotEngine map[string]bool

func (engine snapshotEngine) HasSession(sessionID string) bool { return engine[sessionID] }
func (snapshotEngine) UnloadSession(string) error              { return nil }
func (snapshotEngine) SessionID() string                       { return "" }
func (snapshotEngine) StartSession() string                    { return "" }
func (snapshotEngine) SetSystemPrompt(string)                  {}
func (snapshotEngine) SetMaxLoops(int)                         {}
func (snapshotEngine) ChatStream(context.Context, string, func(string)) (string, error) {
	return "", nil
}
func (snapshotEngine) ClearHistory() {}
func (snapshotEngine) History() []contract.EngineMessage {
	return nil
}
func (snapshotEngine) ReplaceHistory(string, []contract.EngineMessage) error {
	return nil
}
func (snapshotEngine) TraceText() string                    { return "" }
func (snapshotEngine) TokenCount() string                   { return "" }
func (snapshotEngine) AppendHistory(contract.EngineMessage) {}
func (snapshotEngine) NodeSessionConversation(string) ([]contract.EngineMessage, bool) {
	return nil, false
}
func (snapshotEngine) NodeContextSnapshot(string) (*dto.ContextSnapshot, bool) {
	return nil, false
}
func (snapshotEngine) NodeToolResult(string, string) (string, bool) {
	return "", false
}
func (snapshotEngine) NodeWorktreeInfoFor(string) (dto.NodeWorktreeInfo, bool) {
	return dto.NodeWorktreeInfo{}, false
}
func (snapshotEngine) SubscribeSubagentLive(string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	return nil, nil, func() {}, nil
}
func (snapshotEngine) SubagentLiveHistoryPage(string, int, int) dto.SubagentLiveHistoryPage {
	return dto.SubagentLiveHistoryPage{}
}
func (snapshotEngine) SubAgentTree() []dto.SubAgentTreeNode { return nil }

type snapshotPort struct{}

func (snapshotPort) Preview(content string, limit int) (string, bool) {
	return middleresidency.Preview(content, limit)
}
func (snapshotPort) Touch(order []string, sessionID string) []string {
	return middleresidency.Order(order).Touch(sessionID)
}
func (snapshotPort) Forget(order []string, sessionID string) []string {
	return middleresidency.Order(order).Forget(sessionID)
}
func (snapshotPort) Prune(order []string, loaded func(string) bool, unitIDs []string) []string {
	return middleresidency.PruneOrder(middleresidency.Order(order), loaded, unitIDs)
}
func (snapshotPort) Budget(legacyLimit, protectedCount, window int) int {
	return middleresidency.Budget(legacyLimit, protectedCount, window)
}
func (snapshotPort) InputIndexSummaryLimit() int { return 0 }
func (snapshotPort) BuildInputIndex([]protocol.Message, []string, int, func(protocol.Message) string) []protocol.SessionInputIndexRow {
	return nil
}
func (snapshotPort) WaitCatalogRefresh(context.Context, applicationsession.CatalogRefresher) error {
	return nil
}
func (snapshotPort) Evict(applicationsession.EvictionDeps) {}
func (snapshotPort) Archive(applicationsession.ArchiveUseCaseDeps, string) error {
	return nil
}

func TestEvictionSnapshotsDoNotAliasOrderBackingArray(t *testing.T) {
	engine := snapshotEngine{"session-a": true, "session-b": true}
	kernel := state.New(contract.Dependencies{Engine: engine})
	units := sessionstate.NewDomain()
	for _, sessionID := range []string{"session-a", "session-b"} {
		unit, err := sessionstate.NewSessionUnit(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		unit.SetResident(true)
		unit.View.Mutate(func(view *sessionstate.View) {
			view.Conversation = []protocol.Message{{Role: "user", Content: sessionID}}
		})
		units.Register(unit)
	}
	view := viewstate.NewCoordinator(viewstate.Deps{
		Core:            kernel,
		Units:           units,
		Limits:          func() dto.ViewLimits { return dto.ViewLimits{} },
		TitleForSession: func(string) string { return "" },
		DraftView:       func() (string, string) { return "", "" },
	})
	runtime := NewRuntime(RuntimeDeps{
		Core: kernel, Units: units, View: view, Port: snapshotPort{},
		ResidentSessionLimit: func() int { return 0 },
		LoadedContentLimit:   func() int { return 0 },
		ReleaseWindow:        func() int { return 1 },
	})
	runtime.residentOrder = []string{"session-a", "session-b"}
	runtime.loadedContentOrder = []string{"session-a", "session-b"}

	kernel.ViewMu.Lock()
	content := runtime.ContentSnapshotLocked("session-a")
	resident := runtime.ResidentSnapshotLocked()
	kernel.ViewMu.Unlock()
	if &content.Order[0] == &runtime.loadedContentOrder[0] {
		t.Fatal("content snapshot aliases the owned order")
	}
	if &resident.Order[0] == &runtime.residentOrder[0] {
		t.Fatal("resident snapshot aliases the owned order")
	}
	runtime.ForgetContentLocked("session-a")
	runtime.ForgetResidentLocked("session-a")
	if content.Order[0] != "session-a" || resident.Order[0] != "session-a" {
		t.Fatalf("snapshot mutated after order write: content=%v resident=%v", content.Order, resident.Order)
	}
}
