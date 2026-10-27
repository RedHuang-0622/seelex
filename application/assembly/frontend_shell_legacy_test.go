package assembly

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/board"
)

// These shims keep existing core integration fixtures source-compatible after
// their public facade moved to application/compat. They exist only in the test
// build and call the same extracted owners as the frontend adapter.
func (service *Service) SetProjectBoardSink(sink board.ProjectBoardSink) {
	service.components.projectBoard.store(sink)
}

func (service *Service) PromptLayers() []prompt.PromptLayer {
	if service == nil || service.promptStack == nil {
		return nil
	}
	return service.promptStack.Layers()
}

func (service *Service) PrepareProviderHistory(sessionID string) error {
	if service == nil || service.components.history == nil {
		return fmt.Errorf("prepare provider history: history coordinator is not assembled")
	}
	return service.components.history.PrepareProviderHistoryFor(sessionID)
}

func (service *Service) SetToolRecordEnabled(enabled bool) (bool, error) {
	if service == nil || service.toolRecords == nil {
		return false, nil
	}
	return service.toolRecords.SetEnabled(enabled)
}

func (service *Service) ApplyToolRecordEnabled(enabled bool) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.ApplyEnabled(enabled)
}

func (service *Service) ToolRecordEnabled() bool {
	if service == nil || service.toolRecords == nil {
		return false
	}
	return service.toolRecords.Enabled()
}

func (service *Service) FlushToolRecord(sessionID string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.Flush(sessionID)
}

func (service *Service) SetToolRecordSnapshotStore(store contract.ToolRecordSnapshotStore) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.SetSnapshotStore(store)
}

func (service *Service) ToolRecordBoard() dto.ToolRecordBoard {
	if service == nil || service.toolRecords == nil {
		return dto.ToolRecordBoard{}
	}
	return service.toolRecords.Board()
}

func (service *Service) RecordToolCommandOutcome(sessionID, tool string, exitCode int) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.RecordCommandOutcome(sessionID, tool, exitCode)
}
