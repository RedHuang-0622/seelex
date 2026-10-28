package assemble

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	teamdomain "github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/store"
)

// TestTeamUnitRecordPersistsItsOwnTranscript 钉住 WorkItem 冷恢复的数据源：
// 运行期记录必须带本 WorkItem Session 的完整工作历史，重启读取不依赖主会话。
func TestTeamUnitRecordPersistsItsOwnTranscript(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	router, err := store.NewRouter(t.TempDir())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	nodes := store.NewWorkUnitSessionStore(router)
	runtime.AttachSubSessionStore(nodes)
	mainSessionID := "sess-main"
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, mainSessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	roleSessionID := "sess-main-team-exec-wi-wi-1"
	engine := &conversationEngine{id: roleSessionID, history: []types.Message{
		textMessage("user", "这一轮做了什么"),
		textMessage("assistant", "保留了完整记录"),
	}}
	state := runtime.roleTurnState()
	state.mu.Lock()
	state.sessions = map[string]*roleSessionHandle{
		roleSessionID: {id: roleSessionID, role: "exec", engine: engine},
	}
	state.mu.Unlock()

	runtime.saveTeamUnitRecord(mainSessionID, teamUnitRecordKey{
		NodeID: "exec-wi-1", RoleSessionID: roleSessionID, Role: "exec",
	}, teamUnitStatusRunning, "实现中", "running")
	record, err := nodes.Load("p-team", mainSessionID, roleSessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(record.History) != 2 {
		t.Fatalf("WorkItem 记录必须保存本 Session 的完整历史，得到 %+v", record.History)
	}
	if record.History[0].Content == nil || *record.History[0].Content != "这一轮做了什么" {
		t.Fatalf("历史内容错误: %+v", record.History)
	}

	beforeStatus := record.Status
	engine.history = append(engine.history,
		textMessage("user", "继续解释"),
		textMessage("assistant", "补充说明"),
	)
	runtime.saveTeamUnitTranscript(mainSessionID, teamUnitRecordKey{
		NodeID: "exec-wi-1", RoleSessionID: roleSessionID, Role: "exec",
	})
	record, err = nodes.Load("p-team", mainSessionID, roleSessionID)
	if err != nil {
		t.Fatalf("Load after dialogue: %v", err)
	}
	if len(record.History) != 4 {
		t.Fatalf("DialogueOnly 后也必须持久化完整 WorkItem 历史，得到 %+v", record.History)
	}
	if record.Status != beforeStatus {
		t.Fatalf("对话不得改变 WorkItem 单元状态: before=%q after=%q", beforeStatus, record.Status)
	}

	state = runtime.roleTurnState()
	state.mu.Lock()
	state.sessions = nil
	state.mu.Unlock()
	resume := runtime.teamUnitResumeHistory(mainSessionID, roleSessionID)
	if len(resume) != 4 {
		t.Fatalf("冷恢复必须读回 WorkItem 自己的快照，得到 %+v", resume)
	}

	ctx := teamdomain.WithWorkScope(context.Background(), teamdomain.WorkScope{
		MainSessionID: mainSessionID, RoleName: "exec", RoleSessionID: roleSessionID, WorkItemID: "wi-1",
	})
	engine.history = append(engine.history, textMessage("assistant", "工具调用边界已快照"))
	state.mu.Lock()
	state.sessions = map[string]*roleSessionHandle{
		roleSessionID: {id: roleSessionID, role: "exec", engine: engine},
	}
	state.mu.Unlock()
	runtime.snapshotRoleWorkItemTranscript(ctx)
	record, err = nodes.Load("p-team", mainSessionID, roleSessionID)
	if err != nil {
		t.Fatalf("Load after tool snapshot: %v", err)
	}
	if len(record.History) != 5 {
		t.Fatalf("工具边界必须更新 transcript 快照，得到 %+v", record.History)
	}

	state.mu.Lock()
	state.sessions = nil
	state.mu.Unlock()
	spec := runtime.workerRoleRoundSpec(teamwork.WorkerRequest{
		MainSessionID: mainSessionID, TeamID: "team-1", Role: "exec",
		RoleSessionID: roleSessionID, WorkItemID: "wi-1", Milestone: "m-1", Goal: "继续",
	}, mainSessionID, 4)
	if len(spec.ResumeHistory) != 5 {
		t.Fatalf("中断重跑必须把最近快照注入下一轮，得到 %+v", spec.ResumeHistory)
	}
}
