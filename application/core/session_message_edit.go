package core

import (
	"context"
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	editForkOperationTTL = 5 * time.Minute
	editForkMissingSeq   = "这条消息缺少可定位的持久事件序号"
	editForkCompressed   = "该消息已在上下文压缩前缀中，不能编辑"
	editForkRunning      = "父会话正在运行；等待本轮结束后再编辑"
	editForkNoWindow     = "当前上下文没有可编辑的保留窗口"
)

// EditUserMessageAndFork 原子地完成“编辑用户消息 → 从上一完整段落边界 fork →
// 切换到 child → 提交修改后的文本”。调用方不能把它拆成 ForkSession + Submit：
// 本方法用 operation_id 收口双击/重试，并在 child 首轮启动失败时回收本次 child。

func (service *Service) editForkApplicationComponent() *applicationedit.EditApplication {
	if service == nil {
		return nil
	}
	return service.components.editForkApplication
}

func (service *Service) beginEditForkOperation(parentID string, request protocol.EditUserMessageForkRequest) (*editForkOperation, bool, error) {
	return service.components.editForkOperationRegistry.Begin(parentID, request)
}

func (service *Service) finishEditForkOperation(operation *editForkOperation, result protocol.EditUserMessageForkResult, err error) {
	service.components.editForkOperationRegistry.Finish(operation, result, err)
}

func (service *Service) runEditUserMessageFork(ctx context.Context, parentID string, request protocol.EditUserMessageForkRequest) (protocol.EditUserMessageForkResult, error) {
	return service.components.editForkProcess.Run(ctx, parentID, request)
}

func (service *Service) resolveEditForkTarget(parentID string, targetSeq uint64) (protocol.TranscriptEvent, uint64, uint64, error) {
	return service.components.editForkTargetResolver.Resolve(parentID, targetSeq)
}

func (service *Service) validateEditForkParent(parentID string, parentRevision uint64, expectedRevision uint64, targetSeq uint64) error {
	return service.components.editForkValidator.Validate(parentID, parentRevision, expectedRevision, targetSeq)
}

func (service *Service) rollbackEditFork(parentID, childID string, cause error) error {
	return service.components.editForkRollback.Rollback(parentID, childID, cause)
}

// annotateConversationForEditLocked 给可见用户消息补权威 EventSeq 与编辑判据。
// 调用方持有 Core.ViewMu。
func (service *Service) annotateConversationForEditLocked(sessionID string, messages []protocol.Message, running bool) []protocol.Message {
	return service.components.editForkAnnotation.Annotate(sessionID, messages, running)
}

func (service *Service) loadSessionForkRef(location session_runtime.Location, sessionID string) *protocol.SessionForkRef {
	return service.components.sessionForkLineage.Load(location, sessionID)
}
