package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// reviewProbeLimit 是裁决一份待审阅文件时**实际读回**的上限（字节）。
//
// 只用于 containment / 敏感 / 二进制探测，字节读完即弃（不进快照、不进工具结果、
// 不进对话）。取 64 KiB 而不是 1 字节：二进制探测（workspace.textLikeBytes）看的是
// 本次读取字节的分布，读 1 字节会把 PNG（89 50 4E 47…）之类的文件误判成文本。
// 取 64 KiB 而不是 workspace 的预览默认上限（4 MiB）：裁决只需要"能不能给人看"，
// 不需要把整份文件拖进内存——真要读的是前端（按路径走既有的预览通道）。
const reviewProbeLimit int64 = 64 << 10

// RequestFileReview 实现 seelebridge.ReviewFilesPort：把一次「请 GUI 在侧边资源
// 管理器打开这些待审阅文件」的批处理召唤落成**会话作用域的可重放请求**，并回一份
// 契约收据（已接受清单 + 被跳过清单及原因）。
//
// 它是沙盒边界的**唯一裁决点**，而它本身不实现任何边界：
//
//   - 逐份文件走 workspace 域的 ReadFile（`workspaceFilePort()` 一处解析 root）——
//     与前端点开文件行时用的**同一个 Port、同一处 root**。因此"agent 叫出来的文件"
//     与人点开的文件共享同一条 containment / 敏感过滤 / 上限 / 二进制边界；
//   - 本函数没有读磁盘的旁路，也没有一条"agent 专用"的更宽边界：清单里只有路径，
//     内容仍由前端按路径读回。
//
// 返回值即契约（模型面工具结果会进对话并被后续回合读到）：请求了几份、哪几份进去了、
// 哪几份没进去以及为什么。**超限显式报错、不静默截断**（见 MaxReviewFilesPerRequest）。
//
// 落点口径：只有**至少一份通过裁决**时才写快照——全部被跳过时不碰既有请求，
// 免得"叫了 10 份、一份都没成"反而把用户已经开着的详情抽屉关掉。
func (service *Service) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	if len(files) == 0 {
		return dto.ReviewRequest{}, errors.New("show_files: files 不能为空（一次调用 = 一次批处理召唤）")
	}
	if len(files) > dto.MaxReviewFilesPerRequest {
		return dto.ReviewRequest{}, fmt.Errorf(
			"show_files: 一次最多召唤 %d 份文件（一次调用 = 一次批处理召唤，本次请求 %d 份）；请分批调用，不要靠截断",
			dto.MaxReviewFilesPerRequest, len(files))
	}
	port, root, err := service.workspaceFilePort()
	if err != nil {
		return dto.ReviewRequest{}, fmt.Errorf("show_files: %w", err)
	}

	request := dto.ReviewRequest{
		RequestID:   service.newReviewRequestID(),
		RequestedAt: time.Now(),
	}
	seen := make(map[string]struct{}, len(files))
	for _, raw := range files {
		path := strings.TrimSpace(raw.Path)
		if path == "" {
			request.Skipped = append(request.Skipped, dto.ReviewSkip{Path: raw.Path, Reason: dto.ReviewSkipMissing})
			continue
		}
		key := normalizeReviewKey(path)
		if _, duplicate := seen[key]; duplicate {
			request.Skipped = append(request.Skipped, dto.ReviewSkip{Path: path, Reason: dto.ReviewSkipDuplicate})
			continue
		}
		seen[key] = struct{}{}

		content, readErr := port.ReadFile(root, path, reviewProbeLimit)
		if readErr != nil {
			request.Skipped = append(request.Skipped, dto.ReviewSkip{Path: path, Reason: classifyReviewReadError(readErr)})
			continue
		}
		if !content.TextLike {
			request.Skipped = append(request.Skipped, dto.ReviewSkip{Path: path, Reason: dto.ReviewSkipBinary})
			continue
		}
		ref := dto.ReviewFileRef{Path: content.Path, Note: strings.TrimSpace(raw.Note)}
		// 行区间只在两端都成区间时才带上（单端或倒序视为"不指定"，不猜）。
		if raw.StartLine > 0 && raw.EndLine >= raw.StartLine {
			ref.StartLine, ref.EndLine = raw.StartLine, raw.EndLine
		}
		request.Files = append(request.Files, ref)
	}

	if len(request.Files) == 0 {
		return request, nil
	}
	// 请求归属会话：工具调度 ctx 里的会话优先（后台会话也能把请求落在**自己**名下，
	// 用户切过去时才看见），未注入时回退视图会话。
	sessionID := strings.TrimSpace(task_context.SessionIDFromContext(ctx))
	if sessionID == "" {
		sessionID = service.currentViewSessionID()
	}
	if sessionID == "" {
		return request, nil
	}
	service.ViewMu.Lock()
	service.components.view.SetReviewRequestForLocked(sessionID, &request)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	// 整份快照事件（不是一次性事件）：payload 是快照里的只读段，前端按它补放/开 chip。
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	return request, nil
}

// ClearReviewRequest 清空指定会话的待审阅请求（工作区切换/解绑的失效面；
// sessionID 为空时按视图会话）。它只清"现在请你看一下"这一格，不动任何会话事实。
func (service *Service) ClearReviewRequest(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = service.currentViewSessionID()
	}
	if sessionID == "" {
		return
	}
	service.ViewMu.Lock()
	if service.components.view.ReviewRequestForLocked(sessionID) != nil {
		service.components.view.ClearReviewRequestForLocked(sessionID)
		service.bumpLocked()
	}
	service.ViewMu.Unlock()
}

// newReviewRequestID 派一个跨调用唯一的召唤 ID（前端据此区分"新的一次请求"与
// "同一份快照的补放"）。时间戳在 Windows 上分辨率约 0.5ms，同一 tick 的两次召唤
// 会撞号，因此附加单调序号（与 chatSeq 消除同一 tick 内碰撞的理由相同）。
func (service *Service) newReviewRequestID() string {
	return fmt.Sprintf("review-%d-%d", time.Now().UnixNano(), service.reviewSeq.Add(1))
}

// normalizeReviewKey 归一化用于"同批次去重"的路径键（分隔符归一；不做路径语义
// 解析——真正的边界判定在 workspace 域，本函数只管"字面上是不是同一份"）。
func normalizeReviewKey(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

// classifyReviewReadError 把 workspace 域的读取拒绝归到契约词表里的原因格。
//
// 为什么按报错文案分类而不是新加类型化错误：可见性边界的**唯一实现**在 workspace
// 域（sanitizeWorkspaceRelPath / resolveFileTarget），它的报错文案已经是前端与文档
// 依赖的既有契约。为这一处读面在 workspace 域再引入一族导出错误类型，会让"边界只有
// 一份"变成"边界一份、分类一份"，且两边都可能漂移。代价是分类依赖文案——因此这里
// 的判据集中在一个函数里、有单测钉住，新增文案时只需改这一处。
func classifyReviewReadError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "sensitive name"):
		return dto.ReviewSkipSensitive
	case strings.Contains(message, "ignored directory"),
		strings.Contains(message, "escapes workspace root"),
		strings.Contains(message, "absolute path not allowed"),
		strings.Contains(message, "symbolic link"),
		strings.Contains(message, "not a regular file"),
		strings.Contains(message, "is a directory"),
		strings.Contains(message, "path required"):
		return dto.ReviewSkipSandbox
	default:
		return dto.ReviewSkipMissing
	}
}
