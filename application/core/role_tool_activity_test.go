package core

// role_tool_activity_test.go — 钉住「员工工具活动 → 会话级实时事件」这一跳（2026-10-03）。
//
// 它是 subagent.tool.* 的员工侧对称面：子代理那条落进 Plan 节点的有界 tool_events
// （`subagent_view.HandleSubagentToolEvent`），员工这条不落快照、只作"它刚动了"的推送。
// 因此这里的判据是**事件本身**：kind 对（started/completed）、sid = 主会话、revision=0
// （载荷不进快照，带 revision 会被"快照比事件新"的陈旧判据吃掉）、载荷按 EvidenceChars 截断。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// waitRoleToolEvent 等一条 teammate.tool.* 事件（超时即失败）。
func waitRoleToolEvent(t *testing.T, subscription Subscription, wantKind EventKind) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case received := <-subscription.Events:
			if received.Kind != wantKind {
				continue
			}
			return received
		case <-deadline:
			t.Fatalf("没收到 %s：员工在做工时的工具活动没有实时出口，前端只能等这一轮跑完", wantKind)
		}
	}
}

func TestHandleRoleToolActivityPublishesSessionEvent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	subscription, err := service.SubscribeSession("s-main", 32)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "read_file#1#0a1b2c3d", MainSessionID: "s-main",
		RoleName: "impl_ui", RoleSessionID: "role-impl-ui",
		Name: "read_file", Arguments: `{"path":"gui/app.js"}`, Status: "running", Turn: 1,
	})
	started := waitRoleToolEvent(t, subscription, EventTeammateToolStarted)
	if started.SessionID != "s-main" {
		t.Fatalf("事件必须按主会话路由：sid=%q", started.SessionID)
	}
	if started.Revision != 0 {
		t.Fatalf("载荷不进快照 → revision 必须为 0（否则被陈旧判据吃掉）：%d", started.Revision)
	}
	var decoded dto.RoleToolActivity
	if err := json.Unmarshal(started.Payload, &decoded); err != nil {
		t.Fatalf("载荷不是 RoleToolActivity：%v（%s）", err, started.Payload)
	}
	if decoded.RoleSessionID != "role-impl-ui" || decoded.Name != "read_file" || decoded.Status != "running" {
		t.Fatalf("载荷必须原样带上身份与工具读数：%+v", decoded)
	}

	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "read_file#1#0a1b2c3d", MainSessionID: "s-main",
		RoleName: "impl_ui", RoleSessionID: "role-impl-ui",
		Name: "read_file", Status: "success", Result: "…正文…", Turn: 1,
	})
	completed := waitRoleToolEvent(t, subscription, EventTeammateToolCompleted)
	if completed.SessionID != "s-main" {
		t.Fatalf("completed 帧同样按主会话路由：sid=%q", completed.SessionID)
	}
}

func TestHandleRoleToolActivityTruncatesAndDropsUnroutable(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	subscription, err := service.SubscribeSession("s-main", 32)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	// 没有主会话归属 = 没有可以追加的尾部：宁可丢弃，也不回填到别的会话。
	service.HandleRoleToolActivity(dto.RoleToolActivity{ID: "x#1#0", RoleSessionID: "role-impl", Status: "running"})
	// 没有调用 ID = 认不出这是哪一次工具调用：同样不发。
	service.HandleRoleToolActivity(dto.RoleToolActivity{MainSessionID: "s-main", RoleSessionID: "role-impl", Status: "running"})

	long := strings.Repeat("证", Limits().EvidenceChars+50)
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "bash#2#0f0f0f0f", MainSessionID: "s-main", RoleName: "impl", RoleSessionID: "role-impl",
		Name: "bash", Arguments: long, Result: long, Status: "success",
	})
	received := waitRoleToolEvent(t, subscription, EventTeammateToolCompleted)
	var decoded dto.RoleToolActivity
	if err := json.Unmarshal(received.Payload, &decoded); err != nil {
		t.Fatalf("载荷解码失败：%v", err)
	}
	if runes := []rune(decoded.Arguments); len(runes) != Limits().EvidenceChars+1 {
		t.Fatalf("入参必须按 EvidenceChars 截断（与子代理同一把尺子）：%d runes", len(runes))
	}
	if !strings.HasSuffix(decoded.Result, "…") {
		t.Fatalf("结果截断要留省略号（看得出这是截断）：%q…", decoded.Result)
	}
	// 上一条"无法路由"的调用不该留下任何事件：下一条收到的必须是我们刚发的那条。
	if decoded.ID != "bash#2#0f0f0f0f" {
		t.Fatalf("无法路由的调用不该发事件：收到 %+v", decoded)
	}
}
