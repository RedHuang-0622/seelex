package assembly

import "context"

// TaskTerminalHandler is a test-only compatibility shim.
func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return service.components.terminalHandler.Handler(kind)
}
