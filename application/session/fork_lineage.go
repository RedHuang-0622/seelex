package session

import (
	"encoding/json"
	"strings"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// ForkLineageDeps supplies the persistent session-context payload read.
type ForkLineageDeps struct {
	LoadPayload func(location session_runtime.Location, sessionID string) ([]byte, error)
}

// ForkLineageApplication owns read-only decoding of persistent fork lineage.
type ForkLineageApplication struct {
	deps ForkLineageDeps
}

// forkLineagePayload is the only persistent field this read use case needs.
// Other session-context payload fields belong to their own readers; decoding
// the whole Store record here would make Application depend on persistence.
type forkLineagePayload struct {
	ForkedFrom json.RawMessage `json:"forked_from,omitempty"`
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
	var record forkLineagePayload
	if json.Unmarshal(payload, &record) != nil || len(record.ForkedFrom) == 0 {
		return nil
	}
	var ref model.SessionForkRef
	if json.Unmarshal(record.ForkedFrom, &ref) != nil || strings.TrimSpace(ref.ParentSessionID) == "" {
		return nil
	}
	return &ref
}
