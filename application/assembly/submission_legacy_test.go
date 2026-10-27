package assembly

import "context"

// Submit is a test-only compatibility shim for core integration tests.
func (service *Service) Submit(ctx context.Context, text string) error {
	return service.components.submission.Submit(ctx, text)
}
