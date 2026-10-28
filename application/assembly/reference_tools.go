package assembly

// reference_tools.go is the core-side adapter for the read-only reference
// application. Request validation, paging, Plan shaping and the
// result:<callID> wire alias live in application/reference and
// contract/protocol; this file only supplies the Engine node bridge.

// nodeToolResult reads a subagent tool result through the Engine bridge.
func (service *Service) nodeToolResult(nodeID, ref string) (string, bool) {
	if service == nil || service.Deps.Engine == nil {
		return "", false
	}
	return service.Deps.Engine.NodeToolResult(nodeID, ref)
}
