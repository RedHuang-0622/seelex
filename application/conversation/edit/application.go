package edit

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// EditApplicationDeps supplies idempotency and the extracted edit/fork process.
type EditApplicationDeps struct {
	Registry *OperationRegistry
	Run      func(context.Context, string, model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error)
}

// EditApplication owns top-level edit input validation and operation waiting.
type EditApplication struct {
	deps EditApplicationDeps
}

// NewEditApplication constructs the edit/fork application.
func NewEditApplication(deps EditApplicationDeps) *EditApplication {
	return &EditApplication{deps: deps}
}

// EditUserMessageAndFork atomically forks a child and submits edited text.
func (application *EditApplication) EditUserMessageAndFork(
	ctx context.Context,
	parentID string,
	request model.EditUserMessageForkRequest,
) (model.EditUserMessageForkResult, error) {
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
	operation, owner, err := application.deps.Registry.Begin(parentID, request)
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	if !owner {
		select {
		case <-ctx.Done():
			return model.EditUserMessageForkResult{}, ctx.Err()
		case <-operation.Done():
			return operation.Outcome()
		}
	}
	result, runErr := application.deps.Run(ctx, parentID, request)
	application.deps.Registry.Finish(operation, result, runErr)
	return result, runErr
}
