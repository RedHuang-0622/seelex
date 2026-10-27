package store

// project_board_compat.go keeps the migration-period public identity stable while
// store/board is the single implementation.

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

const (
	ProjectBoardSchema          = dto.ProjectBoardSchema
	ProjectBoardMaxRows         = dto.ProjectBoardMaxRows
	ProjectBoardReadMaxSessions = dto.ProjectBoardReadMaxSessions
	ProjectBoardWorkFile        = dto.ProjectBoardWorkFile
	ProjectBoardTeamworkFile    = dto.ProjectBoardTeamworkFile
	ProjectBoardMarkdownFile    = dto.ProjectBoardMarkdownFile

	ProjectBoardKindWork          = dto.ProjectBoardKindWork
	ProjectBoardKindMilestone     = dto.ProjectBoardKindMilestone
	ProjectBoardKindMilestoneItem = dto.ProjectBoardKindMilestoneItem
)

type ProjectBoardScope = dto.ProjectBoardScope
type ProjectBoardRow = dto.ProjectBoardRow
type ProjectBoardReadOptions = dto.ProjectBoardReadOptions

func ProjectBoardKinds() []string {
	return storeboard.Kinds()
}

func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow {
	return storeboard.Fold(rows, logf)
}

func ProjectBoardDir(projectRoot string) string {
	return storeboard.Dir(projectRoot)
}

func ProjectBoardSessionDir(projectRoot, sessionID string) string {
	return storeboard.SessionDir(projectRoot, sessionID)
}

func ProjectBoardMarkdown(rows []ProjectBoardRow) []byte {
	return storeboard.Markdown(rows)
}

func validateProjectRoot(projectRoot string) error {
	return storeboard.ValidateProjectRoot(projectRoot)
}

func projectBoardLock(path string) *sync.Mutex {
	return storeboard.Mutex(path)
}

func (router *Router) SetProjectBoardRootFor(resolve func(sessionID string) string) {
	router.mu.Lock()
	defer router.mu.Unlock()
	router.projectBoardRootFor = resolve
}

func (router *Router) projectBoardRootOf(sessionID string) string {
	router.mu.RLock()
	resolve := router.projectBoardRootFor
	router.mu.RUnlock()
	if resolve == nil {
		return ""
	}
	return strings.TrimSpace(resolve(sessionID))
}

func (router *Router) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []ProjectBoardRow) (uint64, error) {
	return storeboard.Append(projectRoot, sessionID, file, rows)
}

func (router *Router) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]ProjectBoardRow, error) {
	return storeboard.ReadRows(projectRoot, sessionID, file)
}

func (router *Router) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error) {
	return storeboard.Watermark(projectRoot, sessionID, file)
}

func (router *Router) ReadProjectBoard(projectRoot string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error) {
	return storeboard.Read(projectRoot, opts)
}

func (router *Router) ReclaimProjectBoard(_ /*projectID*/, sessionID string) error {
	return storeboard.Reclaim(router.projectBoardRootOf(sessionID), sessionID)
}

func (router *Router) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error) {
	return storeboard.ReadMarkdown(projectRoot)
}
