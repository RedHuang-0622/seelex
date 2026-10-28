package boardrouter_test

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/host/boardrouter"
)

func TestAdapterForwardsToBoardOwner(t *testing.T) {
	adapter := boardrouter.New()
	projectRoot := t.TempDir()
	const sessionID = "session-adapter"
	row := dto.ProjectBoardRow{
		Schema: dto.ProjectBoardSchema, Kind: dto.ProjectBoardKindWork, Key: "work:1", Seq: 1,
		Scope: dto.ProjectBoardScope{Session: sessionID}, Payload: map[string]any{"task": "adapter"},
	}
	if _, err := adapter.AppendProjectBoardRows(projectRoot, sessionID, dto.ProjectBoardWorkFile, []dto.ProjectBoardRow{row}); err != nil {
		t.Fatal(err)
	}
	rows, err := adapter.ReadProjectBoardRows(projectRoot, sessionID, dto.ProjectBoardWorkFile)
	if err != nil || len(rows) != 1 || rows[0].Key != "work:1" {
		t.Fatalf("adapter read rows=%#v err=%v", rows, err)
	}
	if _, err := adapter.ReadProjectBoardMarkdown(projectRoot); err != nil {
		t.Fatalf("adapter markdown read: %v", err)
	}
}
