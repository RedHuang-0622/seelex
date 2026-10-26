package execution

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// RoleActivityDeps supplies the bounded evidence projection, tool-record
// staging and session event publication owned by the Application host.
type RoleActivityDeps struct {
	RequestID     func() string
	EvidenceChars func() int
	Truncate      func(string, int) string
	Stage         func(ownerSessionID, unitID, name, status, errorText string)
	Publish       func(event.EventKind, uint64, string, string, any)
	NoteDropped   func(string)
	StartedKind   event.EventKind
	CompletedKind event.EventKind
}

// RoleActivityApplication projects already-bounded teammate tool activity. It
// owns only routing/classification; tool-record state remains in its stage and
// session event transport remains in the host callback.
type RoleActivityApplication struct {
	deps RoleActivityDeps
}

func NewRoleActivity(deps RoleActivityDeps) *RoleActivityApplication {
	return &RoleActivityApplication{deps: deps}
}

func (application *RoleActivityApplication) Handle(activity dto.RoleToolActivity) {
	if application == nil || application.deps.Publish == nil {
		return
	}
	sessionID := strings.TrimSpace(activity.MainSessionID)
	if sessionID == "" || strings.TrimSpace(activity.ID) == "" {
		if sessionID == "" && application.deps.NoteDropped != nil {
			application.deps.NoteDropped(toolrecord.DropNoOwner)
		}
		return
	}
	limit := application.deps.EvidenceChars()
	activity.Arguments = application.deps.Truncate(activity.Arguments, limit)
	activity.Result = application.deps.Truncate(activity.Result, limit)
	activity.Error = application.deps.Truncate(activity.Error, limit)

	kind := application.deps.CompletedKind
	if status, ok := dto.ParseToolEventStatus(strings.TrimSpace(activity.Status)); ok && status == dto.ToolEventRunning {
		kind = application.deps.StartedKind
	}
	requestID := application.deps.RequestID()
	unitID := toolrecord.RoleUnitID(activity.RoleSessionID, activity.ID)
	if application.deps.Stage != nil {
		application.deps.Stage(sessionID, unitID, activity.Name, activity.Status, activity.Error)
	}
	application.deps.Publish(kind, 0, requestID, sessionID, activity)
}
