package assemble

// subagent_tool_owner_test.go — 钉住"子代理工具读数的归属来自**执行上下文**"
// （2026-10-19 方向 C）。

import (
	"testing"

	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestSubagentToolObservationCarriesExecutionContextOwner：节点注册时随执行 ctx 绑定的
// 主会话必须随事件一起交出去（读侧据此记账），而不是让读侧去猜/去读视图会话。
func TestSubagentToolObservationCarriesExecutionContextOwner(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	var observations []SubagentToolObservation
	runtime.SetSubagentToolCallback(func(observation SubagentToolObservation) {
		observations = append(observations, observation)
	})

	// 注册（执行上下文绑定）→ 工具事件。
	runtime.noteSubagentOwner("node-owner", "sess-main-a")
	runtime.toolEvents.Publish(subagentsession.SubagentToolEvent{
		ID: "subtool-1", NodeID: "node-owner", Name: "bash", Status: dto.ToolEventSuccess,
	})
	// 未注册的节点：归属取不到（空串），读侧丢弃并计数——本层不编造归属。
	runtime.toolEvents.Publish(subagentsession.SubagentToolEvent{
		ID: "subtool-2", NodeID: "node-unknown", Name: "bash", Status: dto.ToolEventSuccess,
	})

	if len(observations) != 2 {
		t.Fatalf("两条事件都要交出去：%d", len(observations))
	}
	if observations[0].OwnerSessionID != "sess-main-a" {
		t.Fatalf("归属必须来自执行上下文：%q", observations[0].OwnerSessionID)
	}
	if observations[0].Event.Name != "bash" || observations[0].Event.ID != "subtool-1" {
		t.Fatalf("事件本体要原样带出去：%+v", observations[0].Event)
	}
	if observations[1].OwnerSessionID != "" {
		t.Fatalf("没有绑定就没有归属（不许回落到视图会话）：%q", observations[1].OwnerSessionID)
	}
}

// TestRoleTurnEndCallbackCarriesOwnerAndRole：员工回合收口的通知要带上
// (主会话, 角色会话) 两格身份——工具记录暂存的提交键就是这两格。
func TestRoleTurnEndCallbackCarriesOwnerAndRole(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	var got [][2]string
	runtime.SetRoleTurnEndCallback(func(mainSessionID, roleSessionID string) {
		got = append(got, [2]string{mainSessionID, roleSessionID})
	})
	runtime.publishRoleTurnEnd("  sess-main-a  ", " role-impl ")
	if len(got) != 1 || got[0] != [2]string{"sess-main-a", "role-impl"} {
		t.Fatalf("收口通知的身份要去空白并原样带出：%+v", got)
	}
	// 未订阅 = 零成本 no-op（不 panic、不排队）。
	runtime.SetRoleTurnEndCallback(nil)
	runtime.publishRoleTurnEnd("sess-main-a", "role-impl")
}
