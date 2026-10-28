package role

import (
	"strings"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

func TestSnapshotWarningsKeepRoleAttributionDiagnostics(t *testing.T) {
	snapshot := Snapshot{
		MainSessionID: "main-1",
		RoleName:      RoleTL,
		RoleSessionID: "tl-1",
		JoinSeqID:     2,
		MainHeadSeq:   4,
		Floor:         &storesession.Floor{RoleName: RoleTL, Seq: 5},
		OrderRoles:    []string{RoleMain},
		MainRows: []protocol.TranscriptEvent{
			{Seq: 1, RoleName: ""},
			{Seq: 3, RoleName: RoleTL, RoleSessionID: "other"},
			{Seq: 4, RoleName: ""},
		},
		RoleRows: []protocol.TranscriptEvent{{Seq: 1, RoleName: RoleTL}},
	}
	warnings := strings.Join(SnapshotWarnings(snapshot), "\n")
	for _, want := range []string{
		"floor.seq 越界",
		"floor.role_name 不在 lifecycle.order_roles",
		"join_seq_id 之后的 message 行缺 role_name",
		"role_session_id 与查询角色不一致",
		"生产者尚未覆盖全部行",
	} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q:\n%s", want, warnings)
		}
	}
}

func TestWireMessagesPreserveProviderShape(t *testing.T) {
	rows := WireMessages([]storecontext.WireMessage{{
		Role: "assistant", Content: "answer", ReasoningContent: "why",
		ToolCalls:  []protocol.TranscriptToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}},
		ToolCallID: "call-1", Name: "read_file", ResultRef: "blob:1", Seq: 9,
		Internal: true, Repair: true, Attempt: true,
	}})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Role != "assistant" || got.Content != "answer" || got.ReasoningContent != "why" ||
		len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "call-1" ||
		got.Seq != 9 || !got.Internal || !got.Repair || !got.Attempt {
		t.Fatalf("wire projection lost fields: %+v", got)
	}
}
