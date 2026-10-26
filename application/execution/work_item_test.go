package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

type workItemRuntimeStub struct {
	contract.RuntimePort
	todoIndex  int
	todoStatus dto.TodoItemStatus
}

func (runtime *workItemRuntimeStub) WorkItemExecution(context.Context, string, string, dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	return dto.WorkItemExecutionView{WorkItem: dto.TeamworkWorkItemView{ID: "item-1"}}, nil
}
func (runtime *workItemRuntimeStub) TeammateInspection(context.Context, string, string) (dto.TeammateInspectionView, error) {
	return dto.TeammateInspectionView{Role: "builder"}, nil
}
func (runtime *workItemRuntimeStub) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	runtime.todoIndex, runtime.todoStatus = index, status
	return nil
}

func TestWorkItemApplicationDelegatesExecutionReads(t *testing.T) {
	runtime := &workItemRuntimeStub{}
	application := NewWorkItem(WorkItemDeps{
		Runtime: runtime,
		BoardView: func(sessionID string) *dto.TeamworkBoardView {
			return &dto.TeamworkBoardView{TeamID: sessionID}
		},
		SessionLive: func(sessionID string) dto.TeammateSessionLiveView {
			return dto.TeammateSessionLiveView{SessionID: sessionID}
		},
		SessionPage: func(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
			return dto.TeammateSessionLiveView{SessionID: sessionID, Offset: offset, Limit: limit}
		},
	})
	execution, err := application.WorkItemExecution(context.Background(), "main", "item-1", dto.ConversationViewRequest{})
	if err != nil || execution.WorkItem.ID != "item-1" {
		t.Fatalf("WorkItemExecution = %+v, %v", execution, err)
	}
	inspection, err := application.TeammateInspection(context.Background(), "main", "builder")
	if err != nil || inspection.Role != "builder" {
		t.Fatalf("TeammateInspection = %+v, %v", inspection, err)
	}
	if board := application.BoardView("session-a"); board == nil || board.TeamID != "session-a" {
		t.Fatalf("BoardView = %+v", board)
	}
	if live := application.SessionLive("session-a"); live.SessionID != "session-a" {
		t.Fatalf("SessionLive = %+v", live)
	}
	if page := application.SessionPage("session-a", 3, 5); page.Offset != 3 || page.Limit != 5 {
		t.Fatalf("SessionPage = %+v", page)
	}
}

func TestWorkItemApplicationOnlyMutatesTodos(t *testing.T) {
	runtime := &workItemRuntimeStub{}
	refreshed := false
	application := NewWorkItem(WorkItemDeps{
		Runtime:     runtime,
		RefreshTodo: func() { refreshed = true },
	})
	if err := application.UpdateWorkItemStatus("todo:2", " DOING "); err != nil {
		t.Fatal(err)
	}
	if runtime.todoIndex != 2 || runtime.todoStatus != dto.TodoItemDoing || !refreshed {
		t.Fatalf("todo mutation = %d, %q, refreshed=%v", runtime.todoIndex, runtime.todoStatus, refreshed)
	}
	if err := application.UpdateWorkItemStatus("task:1", "done"); err == nil ||
		!strings.Contains(err.Error(), "不支持手动更新") {
		t.Fatalf("task mutation error = %v", err)
	}
	if err := application.UpdateWorkItemStatus("todo:-1", "done"); err == nil {
		t.Fatal("negative todo index accepted")
	}
}

func TestWorkItemApplicationPreservesMissingCapabilities(t *testing.T) {
	base := &struct{ contract.RuntimePort }{}
	application := NewWorkItem(WorkItemDeps{Runtime: base})
	if _, err := application.WorkItemExecution(context.Background(), "main", "item", dto.ConversationViewRequest{}); err == nil ||
		!strings.Contains(err.Error(), "adapter is not configured") {
		t.Fatalf("WorkItemExecution error = %v", err)
	}
	if _, err := application.TeammateInspection(context.Background(), "main", "role"); err == nil ||
		!strings.Contains(err.Error(), "adapter is not configured") {
		t.Fatalf("TeammateInspection error = %v", err)
	}
}

func TestParseWorkItemID(t *testing.T) {
	kind, index, err := parseWorkItemID(" todo:12 ")
	if err != nil || kind != "todo" || index != 12 {
		t.Fatalf("parse = %q, %d, %v", kind, index, err)
	}
	if _, _, err := parseWorkItemID("todo:not-int"); err == nil || !strings.Contains(err.Error(), "无效 todo 索引") {
		t.Fatalf("invalid index error = %v", err)
	}
	if _, _, err := parseWorkItemID(""); err == nil {
		t.Fatal("empty work item id accepted")
	}
}
