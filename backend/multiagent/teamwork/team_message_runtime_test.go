package teamwork

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// TestTeammateConversationRefUsesTheWorkItemSession 钉住 team_message 的窗口：
// role + item 只用于定位，真正对话的 SessionID 必须来自该 WorkItem，不能回退到
// teammate 的长期角色会话；解析本身不得改变 WorkItem 状态。
func TestTeammateConversationRefUsesTheWorkItemSession(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", storeteamwork.ItemReview)

	before := itemState(t, fixture, "wi-req")
	ref, err := fixture.coordinator.TeammateConversationRef(ctx, "pm", "wi-req")
	if err != nil {
		t.Fatalf("TeammateConversationRef: %v", err)
	}
	if ref.Kind != dto.ConversationTeammate ||
		ref.MainSessionID != fixture.coordinator.Key().SessionID ||
		ref.Role != "pm" || ref.WorkItemID != "wi-req" ||
		ref.SessionID != before.SessionID || ref.SessionID == "" {
		t.Fatalf("unexpected conversation ref: %+v", ref)
	}
	after := itemState(t, fixture, "wi-req")
	if after.Status != before.Status || after.SessionID != before.SessionID {
		t.Fatalf("resolving the chat window must not mutate the WorkItem: before=%+v after=%+v", before, after)
	}
}

// TestTeammateConversationRefRejectsRoutingMistakes 钉住显式失败：没有 WorkItem、
// 角色与 WorkItem 归属不一致、或 WorkItem 还没有自己的 Session 时，不得拿角色会话
// 冒充可对话窗口。
func TestTeammateConversationRefRejectsRoutingMistakes(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	if _, err := fixture.coordinator.TeammateConversationRef(ctx, "exec", ""); err == nil || !strings.Contains(err.Error(), "WorkItem") {
		t.Fatalf("missing item must fail, got %v", err)
	}
	if _, err := fixture.coordinator.TeammateConversationRef(ctx, "exec", "wi-missing"); err == nil || !strings.Contains(err.Error(), "wi-missing") {
		t.Fatalf("unknown item must fail, got %v", err)
	}
	if _, err := fixture.coordinator.TeammateConversationRef(ctx, "exec", "wi-req"); err == nil || !strings.Contains(err.Error(), "pm") {
		t.Fatalf("role/item mismatch must fail, got %v", err)
	}
	if _, err := fixture.coordinator.TeammateConversationRef(ctx, "pm", "wi-req"); err == nil || !strings.Contains(err.Error(), "Session") {
		t.Fatalf("item without its own Session must fail, got %v", err)
	}
}
