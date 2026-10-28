package teamwork

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// TestDialogueDispatchKeepsTheWorkItemState 钉住 team_message 不是派活：
// 对话作业可被 jobs_manage 观察/回收，但不调用 SettleWorkItem、不改变 WorkItem
// 状态，也不再占用一个新的 WorkItem worktree。
func TestDialogueDispatchKeepsTheWorkItemState(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", storeteamwork.ItemReview)
	before := itemState(t, fixture, "wi-req")

	dispatch, err := fixture.coordinator.DeclareTeammateMessage(ctx, dto.ConversationSessionRef{
		Kind: dto.ConversationTeammate, MainSessionID: fixture.coordinator.Key().SessionID,
		TeamID: "v-model", Role: "pm", WorkItemID: "wi-req", SessionID: before.SessionID,
	}, "为什么超时？", "effective prompt")
	if err != nil {
		t.Fatalf("DeclareTeammateMessage: %v", err)
	}
	if dispatch.Handle == "" || dispatch.Request.RoleSessionID != before.SessionID ||
		!dispatch.Request.DialogueOnly || dispatch.Request.WorkItemID != "wi-req" {
		t.Fatalf("unexpected dialogue dispatch: %+v", dispatch)
	}
	waitTerminal(t, fixture.coordinator.jobs, dispatch.Handle)
	after := itemState(t, fixture, "wi-req")
	if after.Status != before.Status || after.SessionID != before.SessionID {
		t.Fatalf("dialogue must not mutate the WorkItem: before=%+v after=%+v", before, after)
	}
	if requests := fixture.runner.requestsSnapshot(); len(requests) < 2 {
		t.Fatalf("dialogue must run through WorkerRunner, requests=%d", len(requests))
	} else if got := requests[len(requests)-1]; got.DialogueMessage != "为什么超时？" || !got.DialogueOnly ||
		got.WorkItemID != "wi-req" || got.SystemPrompt != "effective prompt" {
		t.Fatalf("dialogue payload must stay dialogue-only: %+v", got)
	}
}
