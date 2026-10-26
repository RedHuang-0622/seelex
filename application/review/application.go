// Package review owns the file-review request application boundary. Workspace
// visibility and request promotion remain behind narrow host ports.
package review

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	reviewtool "github.com/RedHuang-0622/seelex/backend/tool/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// Deps supplies the workspace file port and the host's session-scoped request
// projection. The decision rules remain in backend/tool/review.
type Deps struct {
	WorkspaceFile          func() (contract.WorkspaceFilePort, string, error)
	SessionIDFromContext   func(context.Context) string
	ViewSessionID          func() string
	SetReviewRequest       func(sessionID string, request *dto.ReviewRequest) uint64
	ClearReviewRequest     func(sessionID string)
	PublishSnapshotChanged func(sessionID string, revision uint64)
}

// Application owns only the review request adapter and request identity.
type Application struct {
	deps      Deps
	reviewSeq atomic.Uint64
}

// New constructs the file-review application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// RequestFileReview asks the GUI to open a bounded set of files.
func (application *Application) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	return reviewtool.NewRequests(reviewHost{application: application}).Request(ctx, files)
}

// ClearReviewRequest clears one session's current request projection.
func (application *Application) ClearReviewRequest(sessionID string) {
	reviewtool.NewRequests(reviewHost{application: application}).Clear(sessionID)
}

type reviewHost struct {
	application *Application
}

func (host reviewHost) WorkspaceFile() (contract.WorkspaceFilePort, string, error) {
	return host.application.deps.WorkspaceFile()
}

func (host reviewHost) NextRequestID() string {
	return fmt.Sprintf("review-%d-%d", time.Now().UnixNano(), host.application.reviewSeq.Add(1))
}

func (host reviewHost) SessionIDFromContext(ctx context.Context) string {
	return strings.TrimSpace(host.application.deps.SessionIDFromContext(ctx))
}

func (host reviewHost) ViewSessionID() string {
	return host.application.deps.ViewSessionID()
}

func (host reviewHost) SetReviewRequest(sessionID string, request *dto.ReviewRequest) uint64 {
	return host.application.deps.SetReviewRequest(sessionID, request)
}

func (host reviewHost) ClearReviewRequest(sessionID string) {
	host.application.deps.ClearReviewRequest(sessionID)
}

func (host reviewHost) PublishSnapshotChanged(sessionID string, revision uint64) {
	host.application.deps.PublishSnapshotChanged(sessionID, revision)
}
