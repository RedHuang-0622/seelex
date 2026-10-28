package view_state

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// SnapshotDeps supplies the application-owned enrichment that is not part of
// the raw view projection. Callbacks run while the view read lock is held.
type SnapshotDeps struct {
	Status                func(string) protocol.SessionStatus
	DraftRow              func() *protocol.SessionInfo
	Annotate              func(string, []protocol.Message, bool) []protocol.Message
	LatestTranscriptSeq   func(string) uint64
	ProjectDraftWorkspace func(*protocol.Snapshot)
	WithLiveRows          func([]protocol.SessionInfo) []protocol.SessionInfo
	EnrichRows            func([]protocol.SessionInfo) []protocol.SessionInfo
}

// SnapshotApplication owns the frontend snapshot composition. The raw snapshot
// and all message/revision mutation remain in Coordinator.
type SnapshotApplication struct {
	view        *Coordinator
	deps        SnapshotDeps
	toolResults *ToolResultBudgetApplication
}

func NewSnapshotApplication(view *Coordinator, deps SnapshotDeps) *SnapshotApplication {
	return &SnapshotApplication{view: view, deps: deps}
}

// BindToolResultBudget installs the visible tool-result boundary owner.
func (application *SnapshotApplication) BindToolResultBudget(budget *ToolResultBudgetApplication) {
	application.toolResults = budget
}

// ToolResultBudget returns the visible tool-result boundary owner.
func (application *SnapshotApplication) ToolResultBudget() *ToolResultBudgetApplication {
	return application.toolResults
}

// Snapshot returns a deep-copied projection with session status, the retained
// draft row, conversation annotations and directory enrichment applied. It
// follows the same two-phase boundary as the former core facade: clone first,
// then reacquire the read lock for application-owned projections.
func (application *SnapshotApplication) Snapshot() protocol.Snapshot {
	if application == nil || application.view == nil {
		return protocol.Snapshot{}
	}
	snapshot := application.view.SnapshotView()
	application.view.ViewMu.RLock()
	defer application.view.ViewMu.RUnlock()

	if snapshot.Session.Draft {
		snapshot.Session.Status = protocol.SessionStatusDraft
	} else if application.deps.Status != nil {
		snapshot.Session.Status = application.deps.Status(snapshot.Session.ID)
	}

	if application.deps.DraftRow != nil {
		if draft := application.deps.DraftRow(); draft != nil && draft.ID != "" {
			alreadyListed := false
			for _, item := range snapshot.Sessions {
				if item.ID == draft.ID {
					alreadyListed = true
					break
				}
			}
			if !alreadyListed {
				row := *draft
				snapshot.Sessions = append([]protocol.SessionInfo{row}, snapshot.Sessions...)
			}
		}
	}

	if application.deps.Annotate != nil {
		snapshot.Conversation = application.deps.Annotate(
			snapshot.Session.ID,
			snapshot.Conversation,
			snapshot.Chat.Running,
		)
		// Annotation is allowed to build its result from typed nils. Keep the
		// frontend wire contract at this composition boundary: an empty session
		// (draft, new, or restoring shell) must serialize as [] rather than null.
		if snapshot.Conversation == nil {
			snapshot.Conversation = []protocol.Message{}
		}
	}
	if application.deps.LatestTranscriptSeq != nil {
		snapshot.ConversationRevision = application.deps.LatestTranscriptSeq(snapshot.Session.ID)
	}
	if application.deps.ProjectDraftWorkspace != nil {
		application.deps.ProjectDraftWorkspace(&snapshot)
	}
	if application.deps.WithLiveRows != nil {
		snapshot.Sessions = application.deps.WithLiveRows(snapshot.Sessions)
	}
	if application.deps.EnrichRows != nil {
		snapshot.Sessions = application.deps.EnrichRows(snapshot.Sessions)
	}
	return snapshot
}
