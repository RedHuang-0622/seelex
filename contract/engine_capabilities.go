package contract

// Engine capability adapters keep optional host fallbacks out of composition
// roots while preserving the single-session compatibility path.

type sessionPromptWriter interface {
	SetSystemPromptFor(sessionID, prompt string)
}

type workingHistoryReleaser interface {
	ReleaseWorkingHistoryFor(sessionID string)
}

type sessionActivator interface {
	ActivateSession(sessionID string) error
}

type sessionUnloader interface {
	UnloadSession(sessionID string) error
}

type sessionBundleReleaser interface {
	ReleaseSessionBundle(sessionID string) bool
}

type sessionBackedEngine interface {
	SessionBacked() bool
}

type perSessionExecutionEngine interface {
	PerSessionExecution() bool
}

// ResidentSessionEngine is the explicit-session engine plus unload support.
type ResidentSessionEngine interface {
	SessionChatEngine
	UnloadSession(sessionID string) error
}

// RoutedSessionEngine returns the explicit-session engine when supported.
func RoutedSessionEngine(engine ChatEngine) (SessionChatEngine, bool) {
	routed, ok := engine.(SessionChatEngine)
	return routed, ok
}

// ResidentSessionEngineFor returns the resident engine when supported.
func ResidentSessionEngineFor(engine ChatEngine) (ResidentSessionEngine, bool) {
	resident, ok := engine.(ResidentSessionEngine)
	return resident, ok
}

// SetSystemPromptFor writes a session prompt when the engine supports it.
func SetSystemPromptFor(engine ChatEngine, sessionID, prompt string) bool {
	writer, ok := engine.(sessionPromptWriter)
	if !ok {
		return false
	}
	writer.SetSystemPromptFor(sessionID, prompt)
	return true
}

// SupportsSystemPromptFor reports whether session-routed prompt writes are
// available without performing a write.
func SupportsSystemPromptFor(engine ChatEngine) bool {
	_, ok := engine.(sessionPromptWriter)
	return ok
}

// ReleaseWorkingHistoryFor releases one session's working history.
func ReleaseWorkingHistoryFor(engine ChatEngine, sessionID string) bool {
	releaser, ok := engine.(workingHistoryReleaser)
	if !ok {
		return false
	}
	releaser.ReleaseWorkingHistoryFor(sessionID)
	return true
}

// ActivateSession creates the explicit engine bundle when supported.
func ActivateSession(engine ChatEngine, sessionID string) (bool, error) {
	activator, ok := engine.(sessionActivator)
	if !ok {
		return false, nil
	}
	return true, activator.ActivateSession(sessionID)
}

// UnloadSession unloads one resident session bundle when supported.
func UnloadSession(engine ChatEngine, sessionID string) (bool, error) {
	unloader, ok := engine.(sessionUnloader)
	if !ok {
		return false, nil
	}
	return true, unloader.UnloadSession(sessionID)
}

// ReleaseSessionBundle releases one resident engine bundle.
func ReleaseSessionBundle(engine ChatEngine, sessionID string) bool {
	releaser, ok := engine.(sessionBundleReleaser)
	if !ok {
		return false
	}
	return releaser.ReleaseSessionBundle(sessionID)
}

// SessionBacked reports whether the engine owns a reentrant session-backed
// history.
func SessionBacked(engine ChatEngine) bool {
	backed, ok := engine.(sessionBackedEngine)
	return ok && backed.SessionBacked()
}

// PerSessionExecution reports whether the host supports parallel session
// execution.
func PerSessionExecution(runtime RuntimePort) bool {
	provider, ok := runtime.(perSessionExecutionEngine)
	return ok && provider.PerSessionExecution()
}
