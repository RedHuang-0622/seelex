package adapters

import (
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	reviewtool "github.com/RedHuang-0622/seelex/backend/tool/review"
)

// NewReviewProcessorFactory injects the backend review policy into the
// Application review boundary.
func NewReviewProcessorFactory() applicationreview.ProcessorFactory {
	return func(host applicationreview.Host) applicationreview.RequestProcessor {
		return reviewtool.NewRequests(host)
	}
}
