package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

type compressedTurnStoreStub struct {
	projectID string
	sessionID string
	commit    store.Commit
}

func (stub *compressedTurnStoreStub) SaveCommitWorkspace(projectID, sessionID string, commit store.Commit) error {
	stub.projectID = projectID
	stub.sessionID = sessionID
	stub.commit = commit
	return nil
}

func TestCompressedTurnStorePreservesExplicitScopeAndPayload(t *testing.T) {
	stub := &compressedTurnStoreStub{}
	port := NewCompressedTurnStore(stub)
	if err := port.SaveCompressedTurn("project-a", "session-a", "compressed:seg", "body"); err != nil {
		t.Fatal(err)
	}
	if stub.projectID != "project-a" || stub.sessionID != "session-a" {
		t.Fatalf("scope = %s/%s", stub.projectID, stub.sessionID)
	}
	if len(stub.commit.ToolResults) != 1 {
		t.Fatalf("tool results = %+v", stub.commit.ToolResults)
	}
	result := stub.commit.ToolResults[0]
	if result.Ref != "compressed:seg" || result.Tool != "compact_frame" || result.Content != "body" || result.Size != 4 {
		t.Fatalf("tool result = %+v", result)
	}
}
