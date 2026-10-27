package conversation

import (
	"context"
	"encoding/json"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// TurnStoreCommitPort is the consumer-owned three-aggregate persistence port.
type TurnStoreCommitPort interface {
	WriteArtifacts(context.Context, string, string, []protocol.StoredToolResult) error
	WriteMessages(context.Context, string, string, []protocol.TranscriptEvent) error
	WriteCompactions(context.Context, string, string, []json.RawMessage) error
}

// TurnStoreCommitBatch is the aggregate batch for one durable turn.
type TurnStoreCommitBatch struct {
	ProjectID   string
	SessionID   string
	ToolResults []protocol.StoredToolResult
	Events      []protocol.TranscriptEvent
	Compactions []json.RawMessage
}

// TurnStoreCommitApplication owns the cross-aggregate turn write order.
type TurnStoreCommitApplication struct {
	port TurnStoreCommitPort
}

// NewTurnStoreCommit constructs the application turn unit-of-work.
func NewTurnStoreCommit(port TurnStoreCommitPort) *TurnStoreCommitApplication {
	return &TurnStoreCommitApplication{port: port}
}

// Commit preserves the M0 compatible artifact -> message -> compaction order.
func (application *TurnStoreCommitApplication) Commit(ctx context.Context, batch TurnStoreCommitBatch) error {
	if err := application.port.WriteArtifacts(ctx, batch.ProjectID, batch.SessionID, batch.ToolResults); err != nil {
		return err
	}
	if err := application.port.WriteMessages(ctx, batch.ProjectID, batch.SessionID, batch.Events); err != nil {
		return err
	}
	return application.port.WriteCompactions(ctx, batch.ProjectID, batch.SessionID, batch.Compactions)
}
