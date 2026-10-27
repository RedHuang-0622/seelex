package sessionstate

import (
	"sync/atomic"
	"time"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// ApplicationRuntimeState owns process-level session identifiers and defaults
// that must stay coherent with the session domain across command transitions.
// The application coordinator decides when to read or advance these values;
// their storage and meaning belong here rather than in the assembly package.
type ApplicationRuntimeState struct {
	ChatSeq               uint64
	DraftSeq              uint64
	SessionIDSeq          atomic.Uint64
	FullAccessDefault     bool
	PermissionTierDefault string
}

// DraftSlot is the pre-materialized session shell. ID is the real session key
// allocated before an engine exists; Workspace is set only for workspace
// drafts. The application coordinator clears the slot after first submission.
type DraftSlot struct {
	ID        string
	Workspace *protocol.WorkspaceInfo
	CreatedAt time.Time
	UpdatedAt time.Time
}
