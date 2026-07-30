package core

// serviceComponents is the application composition graph. Service coordinates
// these parts but does not own their implementation details.
type serviceComponents struct {
	prompts  *promptCoordinator
	context  *contextCoordinator
	history  *historySafetyCoordinator
	sessions *sessionCoordinator
	tasks    *taskContextCoordinator
	view     *viewCoordinator
	input    inputDispatcher
}

// historySafetyCoordinator owns provider-cache normalization. Recovery remains
// a Service workflow because it spans context assembly and chat retry policy.
type historySafetyCoordinator struct {
	*serviceState
}

func newHistorySafetyCoordinator(state *serviceState) *historySafetyCoordinator {
	return &historySafetyCoordinator{serviceState: state}
}

// taskContextCoordinator owns the durable transcript, task projection, and
// checkpoint state used by prompt, context, and session components.
type taskContextCoordinator struct {
	*serviceState
}

func newTaskContextCoordinator(state *serviceState) *taskContextCoordinator {
	return &taskContextCoordinator{serviceState: state}
}

type sessionTaskProjectionPort interface {
	taskProjectionLocked(string) *TaskContextProjection
}

// sessionCoordinator owns durable session data, discovery, and storage policy.
// Session switching remains a Service workflow because it coordinates runtime,
// workspace, prompt, and view components as one transaction.
type sessionCoordinator struct {
	*serviceState
	tasks sessionTaskProjectionPort
}

func newSessionCoordinator(state *serviceState) *sessionCoordinator {
	return &sessionCoordinator{serviceState: state}
}

func (coordinator *sessionCoordinator) taskProjectionLocked(sessionID string) *TaskContextProjection {
	if coordinator.tasks == nil {
		return nil
	}
	return coordinator.tasks.taskProjectionLocked(sessionID)
}

type contextTaskPort interface {
	activePlanProjectionLocked() *ActivePlanProjection
	buildTaskCheckpointLocked(*taskExecutionState) TaskCheckpoint
	recordContextCompactionLocked(string, ContextCompaction) bool
	storeToolResultLocked(string, string) StoredToolResult
	countTranscriptEvent(TranscriptEvent) int
}

type contextHistoryPort interface {
	prepareProviderHistory() error
}

type contextCollaborators struct {
	prompts  *promptCoordinator
	sessions *sessionCoordinator
	view     *viewCoordinator
	tasks    contextTaskPort
	history  contextHistoryPort
}

// contextCoordinator assembles bounded provider context from durable task
// state. Its collaborators are narrow ports, so context policy cannot reach
// unrelated application behavior through the Service facade.
type contextCoordinator struct {
	*serviceState
	collaborators contextCollaborators
}

func newContextCoordinator(state *serviceState, collaborators contextCollaborators) *contextCoordinator {
	return &contextCoordinator{serviceState: state, collaborators: collaborators}
}

func (coordinator *contextCoordinator) persistCurrentSession(sessionID string) error {
	return coordinator.collaborators.sessions.persistCurrentSession(sessionID)
}

func (coordinator *contextCoordinator) systemPromptForActiveTaskLocked() string {
	return coordinator.collaborators.prompts.systemPromptForActiveTaskLocked()
}

func (coordinator *contextCoordinator) prepareProviderHistory() error {
	return coordinator.collaborators.history.prepareProviderHistory()
}

func (coordinator *contextCoordinator) activePlanProjectionLocked() *ActivePlanProjection {
	return coordinator.collaborators.tasks.activePlanProjectionLocked()
}

func (coordinator *contextCoordinator) buildTaskCheckpointLocked(state *taskExecutionState) TaskCheckpoint {
	return coordinator.collaborators.tasks.buildTaskCheckpointLocked(state)
}

func (coordinator *contextCoordinator) recordContextCompactionLocked(requestID string, compaction ContextCompaction) bool {
	return coordinator.collaborators.tasks.recordContextCompactionLocked(requestID, compaction)
}

func (coordinator *contextCoordinator) storeToolResultLocked(name, content string) StoredToolResult {
	return coordinator.collaborators.tasks.storeToolResultLocked(name, content)
}

func (coordinator *contextCoordinator) countTranscriptEvent(event TranscriptEvent) int {
	return coordinator.collaborators.tasks.countTranscriptEvent(event)
}

func (coordinator *contextCoordinator) bumpLocked() uint64 {
	return coordinator.collaborators.view.bumpLocked()
}
