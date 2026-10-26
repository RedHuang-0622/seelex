package session

import (
	"context"
	"errors"
	"strings"
)

// SubmitDeps supplies draft materialization, loaded/content state sampling,
// restore and the conversation submit command.
type SubmitDeps struct {
	Materialize func(sessionID, text string) error
	Loaded      func(sessionID string) (loaded, contentUnloaded bool)
	Resume      func(sessionID string) error
	Submit      func(context.Context, string, string) error
}

// SubmitApplication owns explicit-session admission order: draft gate, load
// check, restore when needed, then submit. It does not own queue or chat state.
type SubmitApplication struct {
	deps SubmitDeps
}

func NewSubmit(deps SubmitDeps) *SubmitApplication {
	return &SubmitApplication{deps: deps}
}

func (application *SubmitApplication) SubmitToSession(ctx context.Context, sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if err := application.deps.Materialize(sessionID, text); err != nil {
		return err
	}
	loaded, contentUnloaded := application.deps.Loaded(sessionID)
	if !loaded || contentUnloaded {
		if err := application.deps.Resume(sessionID); err != nil {
			return err
		}
	}
	return application.deps.Submit(ctx, sessionID, text)
}
