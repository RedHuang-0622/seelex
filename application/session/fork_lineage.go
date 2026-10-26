package session

import (
	"encoding/json"
	"strings"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// ForkLineageDeps supplies the persistent session-context payload read.
type ForkLineageDeps struct {
	LoadPayload func(location session_runtime.Location, sessionID string) ([]byte, error)
}

// ForkLineageApplication owns read-only decoding of persistent fork lineage.
type ForkLineageApplication struct {
	deps ForkLineageDeps
}

// NewForkLineage constructs the fork-lineage application.
func NewForkLineage(deps ForkLineageDeps) *ForkLineageApplication {
	return &ForkLineageApplication{deps: deps}
}

// Load returns validated fork lineage or nil for missing/corrupt data.
func (application *ForkLineageApplication) Load(location session_runtime.Location, sessionID string) *model.SessionForkRef {
	if application.deps.LoadPayload == nil {
		return nil
	}
	payload, err := application.deps.LoadPayload(location, sessionID)
	if err != nil || len(payload) == 0 {
		return nil
	}
	var record store.SessionContextRecord
	if json.Unmarshal(payload, &record) != nil || len(record.ForkedFrom) == 0 {
		return nil
	}
	var ref model.SessionForkRef
	if json.Unmarshal(record.ForkedFrom, &ref) != nil || strings.TrimSpace(ref.ParentSessionID) == "" {
		return nil
	}
	return &ref
}
