package store

// Session-board archive compatibility names. Validation and persistence live in
// store/board; this file only preserves the old package identity while callers
// migrate to the shared DTOs and domain store.

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

const (
	BoardKindGoal = dto.BoardKindGoal
	BoardKindTeam = dto.BoardKindTeam

	BoardStateActive = dto.BoardStateActive
	BoardStateClosed = dto.BoardStateClosed

	BoardCloseGoalFinish = dto.BoardCloseGoalFinish
	BoardCloseGoalAbort  = dto.BoardCloseGoalAbort
	BoardCloseTeamReplan = dto.BoardCloseTeamReplan
	BoardCloseTeamClose  = dto.BoardCloseTeamClose
)

type BoardLifecycle = dto.BoardLifecycle
type GoalBoardActive = dto.GoalBoardActive
type GoalBoardHistory = dto.GoalBoardHistory
type GoalBoardMeta = dto.GoalBoardMeta
type TeamBoardMeta = dto.TeamBoardMeta
type SessionBoards = storeboard.SessionBoardStore

func ValidateGoalBoardMeta(meta GoalBoardMeta) error {
	return storeboard.ValidateGoalBoardMeta(meta)
}

func ValidateTeamBoardMeta(meta TeamBoardMeta) error {
	return storeboard.ValidateTeamBoardMeta(meta)
}
