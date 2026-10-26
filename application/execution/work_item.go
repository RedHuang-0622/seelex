package execution

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// WorkItemDeps supplies Runtime execution reads and the team board reader.
type WorkItemDeps struct {
	Runtime     contract.RuntimePort
	BoardView   func(sessionID string) *dto.TeamworkBoardView
	SessionLive func(sessionID string) dto.TeammateSessionLiveView
	SessionPage func(sessionID string, offset, limit int) dto.TeammateSessionLiveView
	RefreshTodo func()
}

// WorkItemApplication owns WorkItem/teammate reads and the single supported
// WorkItem status mutation.
type WorkItemApplication struct {
	deps WorkItemDeps
}

// NewWorkItem constructs the execution read application.
func NewWorkItem(deps WorkItemDeps) *WorkItemApplication {
	return &WorkItemApplication{deps: deps}
}

// WorkItemExecution reads one WorkItem execution view.
func (application *WorkItemApplication) WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	if application == nil || application.deps.Runtime == nil {
		return dto.WorkItemExecutionView{}, errors.New("workitem execution: runtime is not configured")
	}
	port, ok := application.deps.Runtime.(contract.WorkItemExecutionPort)
	if !ok {
		return dto.WorkItemExecutionView{}, errors.New("workitem execution: adapter is not configured")
	}
	return port.WorkItemExecution(ctx, mainSessionID, itemID, page)
}

// TeammateInspection reads one teammate inspection view.
func (application *WorkItemApplication) TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	if application == nil || application.deps.Runtime == nil {
		return dto.TeammateInspectionView{}, errors.New("teammate inspection: runtime is not configured")
	}
	port, ok := application.deps.Runtime.(contract.TeammateInspectionPort)
	if !ok {
		return dto.TeammateInspectionView{}, errors.New("teammate inspection: adapter is not configured")
	}
	return port.TeammateInspection(ctx, mainSessionID, role)
}

// BoardView returns the team board projection.
func (application *WorkItemApplication) BoardView(sessionID string) *dto.TeamworkBoardView {
	if application == nil {
		return nil
	}
	return application.deps.BoardView(sessionID)
}

// SessionLive returns a teammate's live session view.
func (application *WorkItemApplication) SessionLive(sessionID string) dto.TeammateSessionLiveView {
	if application == nil {
		return dto.TeammateSessionLiveView{SessionID: sessionID}
	}
	return application.deps.SessionLive(sessionID)
}

// SessionPage returns a bounded teammate live-session page.
func (application *WorkItemApplication) SessionPage(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	if application == nil {
		return dto.TeammateSessionLiveView{SessionID: sessionID, Offset: offset, Limit: limit}
	}
	return application.deps.SessionPage(sessionID, offset, limit)
}

// UpdateWorkItemStatus updates the only manually mutable WorkItem kind: todo.
func (application *WorkItemApplication) UpdateWorkItemStatus(id, status string) error {
	kind, index, err := parseWorkItemID(id)
	if err != nil {
		return err
	}
	if kind != "todo" {
		return fmt.Errorf("work_item: %s 任务状态由执行器管理，不支持手动更新（仅支持 todo: 前缀）", kind)
	}
	if err := application.deps.Runtime.SetTodoStatus(index, dto.TodoItemStatus(strings.ToLower(strings.TrimSpace(status)))); err != nil {
		return err
	}
	application.deps.RefreshTodo()
	return nil
}

func parseWorkItemID(id string) (kind string, index int, err error) {
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("work_item: 无效 ID %q（应为 todo:<index> 等）", id)
	}
	kind = parts[0]
	if kind == "todo" {
		index, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || index < 0 {
			return "", 0, fmt.Errorf("work_item: 无效 todo 索引 %q", parts[1])
		}
	}
	return kind, index, nil
}
