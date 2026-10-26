package adapters

import (
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract"
)

var _ applicationexecution.BoardReader = teamwork.BoardReader{}

// NewTeamworkBoardReaderFactory returns the Backend-owned read-only teamwork
// projection over the configured Runtime.
func NewTeamworkBoardReaderFactory() func(contract.RuntimePort) applicationexecution.BoardReader {
	return func(runtime contract.RuntimePort) applicationexecution.BoardReader {
		return teamwork.NewBoardReader(runtime)
	}
}
