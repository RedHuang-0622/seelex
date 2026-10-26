package core

import (
	"context"

	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	context_runtime "github.com/RedHuang-0622/seelex/backend/engine/context"
)

// ContextCompactionResult is the stable public compaction receipt.
type ContextCompactionResult = conversationcontext.Result

// CompactionGateTiming is one gate timing in the receipt.
type CompactionGateTiming = conversationcontext.GateTiming

func compactionRangeLabel(result ContextCompactionResult) string {
	return conversationcontext.CompactionRangeLabel(result)
}

func compactionRecordNote(result ContextCompactionResult) string {
	return conversationcontext.CompactionRecordNote(result)
}

// CompactContextNow compacts the current execution session.
func (service *Service) CompactContextNow(ctx context.Context) (ContextCompactionResult, error) {
	return service.contextCompactionApplicationComponent().CompactContextNow(ctx)
}

// CompactContextHandler handles the compact_context tool.
func (service *Service) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	return service.contextCompactionApplicationComponent().CompactContextHandler(ctx, argsJSON)
}

func (service *Service) contextCompactionApplicationComponent() *conversationcontext.Application {
	if service == nil {
		return nil
	}
	return service.components.contextCompactionApplication
}

func newServiceContextCompactionApplication(service *Service) *conversationcontext.Application {
	return conversationcontext.New(conversationcontext.Deps{
		CurrentSessionID: func(ctx context.Context) string {
			sessionID := sessionIDFromContext(ctx)
			if sessionID == "" {
				service.ViewMu.RLock()
				sessionID = service.Core.Snapshot.Session.ID
				service.ViewMu.RUnlock()
			}
			return sessionID
		},
		Acquire: service.acquireCompactionRound,
		Release: service.releaseCompactionRound,
		Compact: func(ctx context.Context, sessionID string) (context_runtime.CompactResult, error) {
			return service.components.context.CompactContextNow(ctx, sessionID)
		},
	})
}
