package conversation

// TurnExecutionDeps supplies execution-policy synchronization callbacks.
type TurnExecutionDeps struct {
	BindProjectRoot     func(sessionID string)
	SyncSubagentPolicy  func(sessionID string)
	SyncReasoningEffort func(sessionID string)
	SyncFullAccess      func(sessionID string)
}

// TurnExecutionApplication synchronizes per-session execution policy.
type TurnExecutionApplication struct {
	deps TurnExecutionDeps
}

// NewTurnExecution constructs the turn execution application.
func NewTurnExecution(deps TurnExecutionDeps) *TurnExecutionApplication {
	return &TurnExecutionApplication{deps: deps}
}

// Prepare applies the execution policy in stable order.
func (application *TurnExecutionApplication) Prepare(sessionID string) {
	application.deps.BindProjectRoot(sessionID)
	application.deps.SyncSubagentPolicy(sessionID)
	application.deps.SyncReasoningEffort(sessionID)
	application.deps.SyncFullAccess(sessionID)
}
