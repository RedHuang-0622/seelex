package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestProviderRoleAuditOnlyUserInputIsUser 钉住 provider role 口径（设计
// docs/arch/a2a-agent-team-factory.md §2.1 / §9 AT2、AT9）：
//   - 只有真实用户输入是 user（provider role 与 role_name 同时为 user）；
//   - task/goal/plan/subagent 的 active/状态材料（internal/context 行、system 行）
//     逻辑归属一律 system，且**绝不伪装成 assistant**；
//   - 角色会话的真实模型发言是 assistant、工具结果是 tool，归属 main（或显式角色）。
//
// 事实源：`applyTranscriptRoleFieldsLocked` 是生产者侧唯一收口点；
// provider 侧 role 在 durable_history/plan_transcript/wire_assembler 统一映射：
// internal/context 状态材料 → system，真实用户输入 → user。
func TestProviderRoleAuditOnlyUserInputIsUser(t *testing.T) {
	coordinator := &Coordinator{}
	st := newSessionTaskRuntime("session-main")

	cases := []struct {
		name         string
		event        model.TranscriptEvent
		wantRoleName string
		wantProvider string
	}{
		{
			name:         "real user input",
			event:        model.TranscriptEvent{Role: "user", Content: "把仓库重构一下"},
			wantRoleName: "user",
			wantProvider: "user",
		},
		{
			name: "task/goal/plan active material (internal row)",
			event: model.TranscriptEvent{
				Role: "user", Kind: model.TranscriptEventKindInternal,
				Content: "<!-- seelex:context-checkpoint:v1 -->{}",
			},
			wantRoleName: "system",
			wantProvider: "system",
		},
		{
			name: "legacy internal_user row",
			event: model.TranscriptEvent{
				Role: "internal_user", Content: "task state snapshot",
			},
			wantRoleName: "system",
			wantProvider: "system",
		},
		{
			name: "legacy context row",
			event: model.TranscriptEvent{
				Role: "context", Content: "plan state snapshot",
			},
			wantRoleName: "system",
			wantProvider: "system",
		},
		{
			name:         "system status row",
			event:        model.TranscriptEvent{Role: "system", Content: "subagent recovery note"},
			wantRoleName: "system",
			wantProvider: "system",
		},
		{
			name:         "role model output",
			event:        model.TranscriptEvent{Role: "assistant", Content: "OK"},
			wantRoleName: "main",
			wantProvider: "assistant",
		},
		{
			name:         "tool result",
			event:        model.TranscriptEvent{Role: "tool", ToolCallID: "t1", Content: "body"},
			wantRoleName: "main",
			wantProvider: "tool",
		},
	}

	for _, testCase := range cases {
		event := testCase.event
		coordinator.applyTranscriptRoleFieldsLocked(st, &event)
		providerRole := providerRoleForTranscriptEvent(event)
		if event.RoleName != testCase.wantRoleName {
			t.Fatalf("%s: role_name = %q, want %q", testCase.name, event.RoleName, testCase.wantRoleName)
		}
		if testCase.wantProvider != "" && providerRole != testCase.wantProvider {
			t.Fatalf("%s: provider role = %q, want %q", testCase.name, providerRole, testCase.wantProvider)
		}
		// 反伪装断言：编排态材料不得以 assistant 身份进入 provider 历史，
		// 否则模型会把它当成自己之前说过的话（幻觉来源）。
		if providerRole == "assistant" && event.RoleName == "system" {
			t.Fatalf("%s: 编排态材料伪装成 assistant 发言: %+v", testCase.name, event)
		}
		// 编排态材料（internal/context/system）不得登记成用户输入：否则 UI/审计/
		// 群聊排序会把它当成真实用户消息并多开一个 round。
		statusMaterial := event.Kind == model.TranscriptEventKindInternal ||
			event.Role == "internal_user" || event.Role == "context" || event.Role == "system"
		if statusMaterial && event.RoleName == "user" {
			t.Fatalf("%s: 编排态材料被登记为用户输入: %+v", testCase.name, event)
		}
		if event.RoleSessionID == "" {
			t.Fatalf("%s: 角色会话归属缺失: %+v", testCase.name, event)
		}
	}
}

// TestProviderRoleAuditInternalMaterialIsNotUserOwned：internal 材料即使 provider
// role 仍是 user（R2-FILTER 的 internal user 轮次），逻辑归属也必须是 system——
// 否则 UI/审计/群聊排序会把它当成真实用户输入而多开一个 round。
func TestProviderRoleAuditInternalMaterialIsNotUserOwned(t *testing.T) {
	coordinator := &Coordinator{}
	st := newSessionTaskRuntime("session-main")

	user := model.TranscriptEvent{Role: "user", Content: "真输入"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &user)
	before := st.roleRoundID
	internal := model.TranscriptEvent{
		Role: "user", Kind: model.TranscriptEventKindInternal, Content: "任务 active 状态",
	}
	coordinator.applyTranscriptRoleFieldsLocked(st, &internal)
	if internal.RoleName != "system" {
		t.Fatalf("internal 材料归属 = %q, want system", internal.RoleName)
	}
	if st.roleRoundID != before {
		t.Fatalf("internal 材料不该开启新 round: before=%d after=%d", before, st.roleRoundID)
	}
}
