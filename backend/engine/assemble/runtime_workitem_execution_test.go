package assemble

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

type conversationEngine struct {
	id      string
	history []types.Message
}

func (e *conversationEngine) SessionID() string        { return e.id }
func (e *conversationEngine) SetSystemPrompt(string)   {}
func (e *conversationEngine) SetMaxLoops(int)          {}
func (e *conversationEngine) ClearHistory()            { e.history = nil }
func (e *conversationEngine) History() []types.Message { return e.history }
func (e *conversationEngine) ChatStream(context.Context, string, func(string)) (string, error) {
	return "ok", nil
}

func textMessage(role, text string) types.Message {
	return types.Message{Role: role, Content: &text}
}

// TestWorkItemExecutionIsTheSingleProgressAndTranscriptRead 钉住 WorkItem 详情与
// teammate 当前 WorkItem 共用同一个 Runtime adapter：完整 transcript 含 system /
// user / assistant / reasoning / tool call / tool result，并保留分页裁剪事实。
func TestWorkItemExecutionIsTheSingleProgressAndTranscriptRead(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	mainSessionID := "sess-workitem-view"
	backend := teamworkTestBackend(&memPlanStore{}, mainSessionID)
	if err := runtime.SetTeamworkBackend(backend); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	runtime.SetPluginCapabilitiesProvider(func(names []string) []dto.PluginCapabilityView {
		if len(names) != 1 || names[0] != "safe-plugin" {
			t.Fatalf("plugin provider names = %v", names)
		}
		return []dto.PluginCapabilityView{{
			Name: "safe-plugin", Description: "安全能力", Mode: "replace",
			Tools:      []string{"read_file"},
			Skills:     []dto.PluginSkillView{{Name: "review", Description: "复核"}},
			MCPServers: []dto.PluginMCPServerView{{Name: "files", Transport: "stdio"}},
			Source:     "builtin",
		}}
	})
	viewSessionID := "sess-workitem-view-team-exec-wi-wi-1"
	plan := storeteamwork.Plan{
		TeamID: "team-view", Version: 1,
		Members: []storeteamwork.Member{{
			Role: "exec", RoleSessionID: "sess-workitem-view-team-exec",
			ToolsPolicy: "readwrite", Permission: map[string]int{"repo": 7},
			Plugins: []string{"safe-plugin"},
		}},
		Milestones: []storeteamwork.Milestone{{
			ID: "m-impl", Name: "实现", Status: storeteamwork.MilestoneActive,
			Items: []storeteamwork.WorkItem{{
				ID: "wi-1", Milestone: "m-impl", Role: "exec", Name: "实现",
				Description: "写代码", Goal: "测试通过", SessionID: viewSessionID,
				Status: storeteamwork.ItemRunning,
			}},
		}},
	}
	key := dto.SessionKey{ProjectID: "p-team", SessionID: mainSessionID}
	if err := backend.Store.WritePlan(context.Background(), key, plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	history := []types.Message{
		textMessage("user", "实现这个功能"),
		{
			Role: "assistant", ReasoningContent: "先看代码",
			Content: ptrText("开始执行"),
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "bash", Arguments: `{"command":"go test ./..."}`},
			}},
		},
		{Role: "tool", Name: "bash", ToolCallID: "call-1", Content: ptrText("ok")},
	}
	state := runtime.roleTurnState()
	state.mu.Lock()
	state.sessions = map[string]*roleSessionHandle{
		viewSessionID: {
			id: viewSessionID, role: "exec",
			engine: &conversationEngine{id: viewSessionID, history: history},
		},
	}
	state.mu.Unlock()

	view, err := runtime.WorkItemExecution(context.Background(), mainSessionID, "wi-1", dto.ConversationViewRequest{
		Limit: 200, MaxRunes: 100,
	})
	if err != nil {
		t.Fatalf("WorkItemExecution: %v", err)
	}
	if view.WorkItem.ID != "wi-1" || view.Session.SessionID != viewSessionID ||
		view.Session.Recovery != dto.RecoveryLive {
		t.Fatalf("WorkItemExecution 身份/恢复状态错误: %+v", view)
	}
	kinds := map[dto.ConversationRecordKind]bool{}
	for _, record := range view.Session.Records {
		kinds[record.Kind] = true
	}
	for _, kind := range []dto.ConversationRecordKind{
		dto.ConversationRecordSystem,
		dto.ConversationRecordUser,
		dto.ConversationRecordAssistant,
		dto.ConversationRecordReasoning,
		dto.ConversationRecordToolCall,
		dto.ConversationRecordToolResult,
	} {
		if !kinds[kind] {
			t.Fatalf("完整执行记录缺 %q: %+v", kind, view.Session.Records)
		}
	}

	inspection, err := runtime.TeammateInspection(context.Background(), mainSessionID, "exec")
	if err != nil {
		t.Fatalf("TeammateInspection: %v", err)
	}
	if inspection.CurrentWorkItem == nil || inspection.CurrentWorkItem.ID != "wi-1" ||
		inspection.CurrentExecution == nil {
		t.Fatalf("teammate inspection 必须聚合当前 WorkItem 与执行读数: %+v", inspection)
	}
	if inspection.Permissions.ToolsPolicy != "readwrite" ||
		inspection.Permissions.Groups["repo"] != 7 ||
		len(inspection.Permissions.Grants) != 1 ||
		inspection.Permissions.Grants[0].Group != "repo" {
		t.Fatalf("权限向量必须保留 raw bits 并解码: %+v", inspection.Permissions)
	}
	abilities := inspection.Permissions.Grants[0].Abilities
	if len(abilities) != 3 || abilities[0] != "r" || abilities[1] != "w" || abilities[2] != "x" {
		t.Fatalf("bits=7 必须解码成 r/w/x，得到 %v", abilities)
	}
	if inspection.Limits.MaxJobs != DefaultMaxJobsForTest {
		t.Fatalf("teammate limit 必须显示 max_jobs: %+v", inspection.Limits)
	}
	if len(inspection.Plugins) != 1 ||
		inspection.Plugins[0].Tools[0] != "read_file" ||
		len(inspection.Plugins[0].Skills) != 1 ||
		len(inspection.Plugins[0].MCPServers) != 1 ||
		inspection.Plugins[0].Source != "builtin" {
		t.Fatalf("plugin 能力明细必须安全投影 name/tools/skills/mcp/source: %+v", inspection.Plugins)
	}
}

const DefaultMaxJobsForTest = 6

func ptrText(value string) *string { return &value }
