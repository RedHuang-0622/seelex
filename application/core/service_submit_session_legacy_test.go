package core

import "context"

// SubmitToSession is a test-only compatibility shim.
func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error {
	return service.components.submitApplication.SubmitToSession(ctx, sessionID, text)
}
