package core

// review_files.go — 「请 GUI 打开待审阅文件」落点的**宿主适配 + 门面转发**。
//
// 判据与落点本体（probe 上限、去重键、读错误分类、会话作用域请求）已在
// M5-R-tool-review 随接收者转移归位 `backend/tool/review`（plan §2.6）。
// 本文件只剩宿主面实现：workspace 读面解析、召唤 ID 派发、会话归属、视图锁内的
// 请求写面与快照发布。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/backend/tool/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// reviewRequests 返回落点（每次调用构造一次值语义的宿主适配；无状态）。
func (service *Service) reviewRequests() *review.Requests {
	return review.NewRequests(serviceReviewHost{service: service})
}

// RequestFileReview 实现 assemble.ReviewFilesPort（判据在 `backend/tool/review`）。
func (service *Service) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	return service.reviewRequests().Request(ctx, files)
}

// ClearReviewRequest 清空指定会话的待审阅请求（工作区切换/解绑的失效面；
// sessionID 为空时按视图会话）。它只清"现在请你看一下"这一格，不动任何会话事实。
func (service *Service) ClearReviewRequest(sessionID string) {
	service.reviewRequests().Clear(sessionID)
}

// serviceReviewHost 把 Service 的能力实现成 review.Host。
type serviceReviewHost struct{ service *Service }

func (host serviceReviewHost) WorkspaceFile() (contract.WorkspaceFilePort, string, error) {
	return host.service.components.workspace.FilePort()
}

// NextRequestID 派一个跨调用唯一的召唤 ID（前端据此区分"新的一次请求"与"同一份快照的补放"）。
// 时间戳在 Windows 上分辨率约 0.5ms，同一 tick 的两次召唤会撞号，因此附加单调序号
// （与 chatSeq 消除同一 tick 内碰撞的理由相同）。
func (host serviceReviewHost) NextRequestID() string {
	return fmt.Sprintf("review-%d-%d", time.Now().UnixNano(), host.service.reviewSeq.Add(1))
}

func (host serviceReviewHost) SessionIDFromContext(ctx context.Context) string {
	return strings.TrimSpace(task_context.SessionIDFromContext(ctx))
}

func (host serviceReviewHost) ViewSessionID() string {
	return host.service.currentViewSessionID()
}

func (host serviceReviewHost) SetReviewRequest(sessionID string, request *dto.ReviewRequest) uint64 {
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	service.components.view.SetReviewRequestForLocked(sessionID, request)
	return service.bumpLocked()
}

func (host serviceReviewHost) ClearReviewRequest(sessionID string) {
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if service.components.view.ReviewRequestForLocked(sessionID) != nil {
		service.components.view.ClearReviewRequestForLocked(sessionID)
		service.bumpLocked()
	}
}

func (host serviceReviewHost) PublishSnapshotChanged(sessionID string, revision uint64) {
	host.service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
}
