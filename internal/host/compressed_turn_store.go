package host

import (
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

type compressedTurnCommitStore interface {
	SaveCommitWorkspace(projectID, sessionID string, commit storesession.Commit) error
}

type compressedTurnStore struct {
	target compressedTurnCommitStore
}

var _ applicationreference.CompressedTurnStore = compressedTurnStore{}

// NewCompressedTurnStore adapts the session manager's explicit-scope commit
// writer to the narrow compressed-turn write port.
func NewCompressedTurnStore(target any) applicationreference.CompressedTurnStore {
	port, ok := target.(compressedTurnCommitStore)
	if !ok {
		return nil
	}
	return compressedTurnStore{target: port}
}

func (port compressedTurnStore) SaveCompressedTurn(projectID, sessionID, ref, content string) error {
	return port.target.SaveCommitWorkspace(projectID, sessionID, storesession.Commit{
		ToolResults: []model.ToolResultRecord{{
			Ref: ref, Tool: "compact_frame", Content: content, Size: len(content),
		}},
	})
}
