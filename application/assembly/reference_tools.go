package assembly

// reference_tools.go is the core-side adapter for the read-only reference
// application. Request validation, paging and Plan shaping live in
// application/reference; this file only supplies the authoritative Tool, Task
// and Session stores and preserves the Service compatibility methods.

import "strings"

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
