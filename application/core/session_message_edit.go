package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

const (
	editForkOperationTTL = 5 * time.Minute
	editForkMissingSeq   = "这条消息缺少可定位的持久事件序号"
	editForkCompressed   = "该消息已在上下文压缩前缀中，不能编辑"
	editForkRunning      = "父会话正在运行；等待本轮结束后再编辑"
	editForkNoWindow     = "当前上下文没有可编辑的保留窗口"
)

type editForkOperation struct {
	done chan struct{}
	id   string

	parentID    string
	request     model.EditUserMessageForkRequest
	result      model.EditUserMessageForkResult
	err         error
	completedAt time.Time
}

// EditUserMessageAndFork 原子地完成“编辑用户消息 → 从上一完整段落边界 fork →
// 切换到 child → 提交修改后的文本”。调用方不能把它拆成 ForkSession + Submit：
// 本方法用 operation_id 收口双击/重试，并在 child 首轮启动失败时回收本次 child。
func (service *Service) EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	parentID = strings.TrimSpace(parentID)
	request.NewText = strings.TrimSpace(request.NewText)
	request.OperationID = strings.TrimSpace(request.OperationID)
	if parentID == "" {
		return model.EditUserMessageForkResult{}, errors.New("会话 ID 不能为空")
	}
	if request.TargetEventSeq == 0 {
		return model.EditUserMessageForkResult{}, errors.New("目标用户消息缺少 EventSeq")
	}
	if request.NewText == "" {
		return model.EditUserMessageForkResult{}, errors.New("编辑后的用户输入不能为空")
	}
	if request.OperationID == "" {
		return model.EditUserMessageForkResult{}, errors.New("编辑操作缺少 operation_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	operation, owner, err := service.beginEditForkOperation(parentID, request)
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	if !owner {
		select {
		case <-ctx.Done():
			return model.EditUserMessageForkResult{}, ctx.Err()
		case <-operation.done:
			return operation.result, operation.err
		}
	}

	result, runErr := service.runEditUserMessageFork(ctx, parentID, request)
	service.finishEditForkOperation(operation, result, runErr)
	return result, runErr
}

func (service *Service) beginEditForkOperation(parentID string, request model.EditUserMessageForkRequest) (*editForkOperation, bool, error) {
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if service.editForkOps == nil {
		service.editForkOps = make(map[string]*editForkOperation)
	}
	now := time.Now()
	for id, existing := range service.editForkOps {
		if existing.completedAt.IsZero() || now.Sub(existing.completedAt) <= editForkOperationTTL {
			continue
		}
		delete(service.editForkOps, id)
	}
	if existing := service.editForkOps[request.OperationID]; existing != nil {
		if existing.parentID != parentID || existing.request != request {
			return nil, false, errors.New("同一 operation_id 不能复用于不同的编辑请求")
		}
		return existing, false, nil
	}
	operation := &editForkOperation{done: make(chan struct{}), id: request.OperationID, parentID: parentID, request: request}
	service.editForkOps[request.OperationID] = operation
	return operation, true, nil
}

func (service *Service) finishEditForkOperation(operation *editForkOperation, result model.EditUserMessageForkResult, err error) {
	service.ViewMu.Lock()
	operation.result = result
	operation.err = err
	if err == nil {
		operation.completedAt = time.Now()
	} else {
		delete(service.editForkOps, operation.id)
	}
	service.ViewMu.Unlock()
	close(operation.done)
}

func (service *Service) runEditUserMessageFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	target, cut, parentRevision, err := service.resolveEditForkTarget(parentID, request.TargetEventSeq)
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	if err := service.validateEditForkParent(parentID, parentRevision, request.ExpectedRevision, request.TargetEventSeq); err != nil {
		return model.EditUserMessageForkResult{}, err
	}

	transition := service.transitionForKey(parentID)
	transition.Lock()
	childID, err := service.forkSessionLocked(parentID, model.ForkRequest{
		EventSeq:         cut,
		SourceEventSeq:   target.Seq,
		SourceMessageID:  target.MessageID,
		ExpectedEventSeq: request.ExpectedRevision,
	})
	transition.Unlock()
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}

	if err := service.resumeSession(childID); err != nil {
		return model.EditUserMessageForkResult{}, service.rollbackEditFork(parentID, childID, fmt.Errorf("切换编辑分支失败: %w", err))
	}
	if err := service.submitConversationFor(ctx, childID, request.NewText); err != nil {
		return model.EditUserMessageForkResult{}, service.rollbackEditFork(parentID, childID, fmt.Errorf("提交编辑后的输入失败: %w", err))
	}
	return model.EditUserMessageForkResult{ChildSessionID: childID, ForkPoint: cut}, nil
}

func (service *Service) resolveEditForkTarget(parentID string, targetSeq uint64) (store.Event, uint64, uint64, error) {
	location := service.components.sessions.LocateSession(parentID)
	port, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
	if !ok {
		return store.Event{}, 0, 0, errors.New("会话 fork 存储不可用")
	}
	events, err := port.LoadEventRangeWorkspace(location.WorkspaceID, parentID, 1, math.MaxUint64)
	if err != nil {
		return store.Event{}, 0, 0, fmt.Errorf("读取父会话事件失败: %w", err)
	}
	var parentRevision uint64
	for _, event := range events {
		if event.Seq > parentRevision {
			parentRevision = event.Seq
		}
	}
	var target *store.Event
	for index := range events {
		if events[index].Seq == targetSeq {
			target = &events[index]
			break
		}
	}
	if target == nil {
		return store.Event{}, 0, parentRevision, fmt.Errorf("找不到 EventSeq=%d 的用户消息", targetSeq)
	}
	if target.Role != "user" || target.WireMaterial || target.Kind == model.TranscriptEventKindInternal ||
		inputIndexUserText(model.Message{Role: target.Role, Content: target.Content}) == "" {
		return store.Event{}, 0, parentRevision, errors.New("目标事件不是可编辑的用户输入")
	}

	var cut uint64
	for _, paragraph := range store.EventParagraphs(events) {
		if target.Seq < paragraph.EventFrom || target.Seq > paragraph.EventTo {
			if paragraph.EventTo < target.Seq {
				cut = paragraph.EventTo
			}
			continue
		}
		if target.Seq != paragraph.EventFrom {
			return store.Event{}, 0, parentRevision, errors.New("目标用户消息不是完整段落起点")
		}
		return *target, cut, parentRevision, nil
	}
	return store.Event{}, 0, parentRevision, errors.New("目标用户消息不属于完整段落")
}

func (service *Service) validateEditForkParent(parentID string, parentRevision uint64, expectedRevision uint64, targetSeq uint64) error {
	service.ViewMu.RLock()
	currentSessionID := service.Core.Snapshot.Session.ID
	unit := service.sessions.Unit(parentID)
	running := false
	queued := false
	if unit != nil {
		chat := unit.ChatState()
		running = chat.Running
		queued = chat.QueuedCount > 0 || len(unit.PendingRequests()) > 0
	}
	floor, hasWindow := service.components.tasks.RetainedWindowFirstEventSeqFor(parentID)
	service.ViewMu.RUnlock()

	if currentSessionID != parentID {
		return errors.New("只能编辑当前会话中的用户消息")
	}
	if running {
		return ErrChatRunning
	}
	if queued {
		return errors.New("父会话还有排队输入；先等待或处理完队列再编辑")
	}
	if expectedRevision != 0 && expectedRevision != parentRevision {
		return fmt.Errorf("父会话已推进（期望事件 revision %d，当前 %d），请重新打开编辑器", expectedRevision, parentRevision)
	}
	if !hasWindow {
		return errors.New(editForkNoWindow)
	}
	if floor > 0 && targetSeq < floor {
		return errors.New(editForkCompressed)
	}
	return nil
}

func (service *Service) rollbackEditFork(parentID, childID string, cause error) error {
	deleteErr := service.DeleteSession(childID)
	restoreErr := service.resumeSession(parentID)
	switch {
	case deleteErr != nil && restoreErr != nil:
		return fmt.Errorf("%w（回滚 child 失败: %v；恢复父会话失败: %v）", cause, deleteErr, restoreErr)
	case deleteErr != nil:
		return fmt.Errorf("%w（回滚 child 失败: %v）", cause, deleteErr)
	case restoreErr != nil:
		return fmt.Errorf("%w（恢复父会话失败: %v）", cause, restoreErr)
	default:
		return cause
	}
}

// annotateConversationForEditLocked 给可见用户消息补权威 EventSeq 与编辑判据。
// 调用方持有 Core.ViewMu。
func (service *Service) annotateConversationForEditLocked(sessionID string, messages []model.Message, running bool) []model.Message {
	annotated := append([]model.Message(nil), messages...)
	transcript := service.components.tasks.TranscriptFor(sessionID)
	seqByMessageID := make(map[string]uint64, len(transcript))
	for _, event := range transcript {
		if event.Role == "user" && event.MessageID != "" && event.Seq > 0 {
			seqByMessageID[event.MessageID] = event.Seq
		}
	}
	floor, hasWindow := service.components.tasks.RetainedWindowFirstEventSeqFor(sessionID)
	for index := range annotated {
		message := &annotated[index]
		message.Editable = false
		message.EditDisabledReason = ""
		if message.Role != "user" || inputIndexUserText(*message) == "" {
			continue
		}
		if message.EventSeq == 0 && message.ID != "" {
			message.EventSeq = seqByMessageID[message.ID]
		}
		switch {
		case running:
			message.EditDisabledReason = editForkRunning
		case message.EventSeq == 0:
			message.EditDisabledReason = editForkMissingSeq
		case !hasWindow:
			message.EditDisabledReason = editForkNoWindow
		case floor > 0 && message.EventSeq < floor:
			message.EditDisabledReason = editForkCompressed
		default:
			message.Editable = true
		}
	}
	return annotated
}

func (service *Service) loadSessionForkRef(location session_runtime.Location, sessionID string) *model.SessionForkRef {
	port, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
	if !ok {
		return nil
	}
	payload, err := port.LoadContextStateWorkspace(location.WorkspaceID, sessionID)
	if err != nil || len(payload) == 0 {
		return nil
	}
	var record store.SessionContextRecord
	if json.Unmarshal(payload, &record) != nil || len(record.ForkedFrom) == 0 {
		return nil
	}
	var ref model.SessionForkRef
	if json.Unmarshal(record.ForkedFrom, &ref) != nil || strings.TrimSpace(ref.ParentSessionID) == "" {
		return nil
	}
	return &ref
}
