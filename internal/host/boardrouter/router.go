// Package boardrouter adapts the concrete Store Router lifecycle to the
// Board-owned filesystem operations. It owns no board semantics.
package boardrouter

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

// Adapter satisfies the Board read/write consumer ports.
type Adapter struct{}

// New constructs the stateless adapter.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error) {
	return storeboard.Append(projectRoot, sessionID, file, rows)
}

func (a *Adapter) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error) {
	return storeboard.Watermark(projectRoot, sessionID, file)
}

func (a *Adapter) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]dto.ProjectBoardRow, error) {
	return storeboard.ReadRows(projectRoot, sessionID, file)
}

func (a *Adapter) ReadProjectBoard(projectRoot string, options dto.ProjectBoardReadOptions) ([]dto.ProjectBoardRow, bool, error) {
	return storeboard.Read(projectRoot, options)
}

func (a *Adapter) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error) {
	return storeboard.ReadMarkdown(projectRoot)
}
