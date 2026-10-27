package application

// Compile-time assertions keep the root frontend adapter structurally
// compatible with both legacy and capability-oriented inbound faces while the
// compatibility methods move out of application/assembly.
var (
	_ ViewFace                 = (*Frontend)(nil)
	_ CommandFace              = (*Frontend)(nil)
	_ PolicyFace               = (*Frontend)(nil)
	_ QueueFace                = (*Frontend)(nil)
	_ ConversationAPI          = (*Frontend)(nil)
	_ SessionAPI               = (*Frontend)(nil)
	_ ExecutionQueryAPI        = (*Frontend)(nil)
	_ ExecutionCommandAPI      = (*Frontend)(nil)
	_ WorkspaceAPI             = (*Frontend)(nil)
	_ GoalAPI                  = (*Frontend)(nil)
	_ PolicyAPI                = (*Frontend)(nil)
	_ AdminAPI                 = (*Frontend)(nil)
	_ RoleAPI                  = (*Frontend)(nil)
	_ SubagentAPI              = (*Frontend)(nil)
	_ StateAPI                 = (*Frontend)(nil)
	_ QueueAPI                 = (*Frontend)(nil)
	_ LifecycleAPI             = (*Frontend)(nil)
	_ SessionQuery             = (*Frontend)(nil)
	_ ConversationHistoryQuery = (*Frontend)(nil)
)
