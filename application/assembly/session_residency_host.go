package assembly

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

// residentEngine is the optional engine face used by the residency runtime.
type residentEngine interface {
	HasSession(sessionID string) bool
	UnloadSession(sessionID string) error
}

const (
	releaseIdle = applicationsession.ReleaseIdle
	releaseFull = applicationsession.ReleaseFull
)
