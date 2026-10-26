package core

// reference_tools.go is the core-side adapter for the read-only reference
// application. Request validation, paging and Plan shaping live in
// application/reference; this file only supplies the authoritative Tool, Task
// and Session stores and preserves the Service compatibility methods.

import (
	"errors"
	"strings"

	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// nodeToolResult reads a subagent tool result through the Engine bridge.
func (service *Service) nodeToolResult(nodeID, ref string) (string, bool) {
	if service == nil || service.Deps.Engine == nil {
		return "", false
	}
	return service.Deps.Engine.NodeToolResult(nodeID, ref)
}

// hasToolResultRefLocked reports whether ref exists in the current session's
// in-memory archive. The caller holds Core.ViewMu.
func (service *Service) hasToolResultRefLocked(resultRef string) bool {
	for _, result := range service.components.tasks.ToolResultRefs() {
		if result.Ref == resultRef {
			return true
		}
	}
	return false
}

// resolveToolResultRefAlias maps result:call_<id> to the archived tr- ref.
func (service *Service) resolveToolResultRefAlias(ref string) string {
	const prefix = "result:"
	if !strings.HasPrefix(ref, prefix) {
		return ref
	}
	callID := strings.TrimPrefix(ref, prefix)
	if callID == "" {
		return ref
	}
	service.ViewMu.RLock()
	realRef := service.components.tasks.ToolResultRefByCallID(callID)
	service.ViewMu.RUnlock()
	if realRef == "" {
		return ref
	}
	return realRef
}

func (service *Service) referenceApplicationComponent() *applicationreference.Application {
	if service == nil {
		return nil
	}
	return service.components.referenceApplication
}

func newServiceReferenceApplication(service *Service) *applicationreference.Application {
	return applicationreference.New(applicationreference.Deps{
		ReferencePageSize: func() int {
			return Limits().ReferencePageSize
		},
		MaxReferencePageSize: func() int {
			return Limits().MaxReferencePageSize
		},
		NodeResult: func(nodeID, ref string) (string, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.nodeToolResult(nodeID, ref)
		},
		ResolveAlias: service.resolveToolResultRefAlias,
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			service.ViewMu.RLock()
			if !service.hasToolResultRefLocked(ref) {
				service.ViewMu.RUnlock()
				return model.StoredToolResult{}, false, nil
			}
			for _, pending := range service.components.tasks.PendingToolResults() {
				if pending.Ref == ref {
					service.ViewMu.RUnlock()
					return pending, true, nil
				}
			}
			sessionID := service.Core.Snapshot.Session.ID
			workspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
			service.ViewMu.RUnlock()

			store, ok := service.Deps.Sessions.(session_runtime.SessionTranscriptPort)
			if !ok {
				return model.StoredToolResult{}, false, errors.New("durable result storage is unavailable")
			}
			result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, ref)
			if err != nil {
				return model.StoredToolResult{}, false, err
			}
			return result, true, nil
		},
		ActivePlanID: func() string {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.components.tasks.ActivePlanID()
		},
		PlanFrame: func(planRef string) (string, model.RuntimeState, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			frame := task_context.ActivePlanFrame(service.components.tasks.PlanStack(), planRef)
			if frame == nil {
				return "", model.RuntimeState{}, false
			}
			planState := cloneRuntimeState(model.RuntimeState{Plan: frame.Plan})
			return frame.Arguments, planState, true
		},
		CompressedTurnView: func() (string, string) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Session.ID,
				session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
		},
		LoadCompressedTurn: func(workspaceID, sessionID, ref string) (string, error) {
			store, ok := service.Deps.Sessions.(session_runtime.SessionTranscriptPort)
			if !ok {
				return "", errors.New("durable storage is unavailable")
			}
			result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, ref)
			if err != nil {
				return "", err
			}
			return result.Content, nil
		},
		CompressedTurnPrefix: compressedTurnRefPrefix,
	})
}
