package session

import (
	"encoding/json"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// Commit replaces the bounded provider cache and append-only transcript
// snapshot together with the latest projection. Tool results are immutable
// additions referenced by the committed state.
type Commit struct {
	ProviderHistory []types.Message
	Events          []model.TranscriptEvent
	State           []byte
	ToolResults     []model.ToolResultRecord
	// CompactionRecords is the full append-only projection list aligned by this
	// commit; persistence keeps only the rows beyond its stored watermark.
	CompactionRecords []json.RawMessage
}
