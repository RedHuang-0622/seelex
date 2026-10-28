package assemble

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// TestTeamMessageIsRegisteredAndRequiresAWorkItem 钉住工具壳：team_message 与
// 其余 leader 工具同一装配时机注册；即使 role 合法，缺 item 也不能降级成长期角色私聊。
func TestTeamMessageIsRegisteredAndRequiresAWorkItem(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	st := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(st, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if _, ok := r.registry.FindTool(teamwork.TeamMessageToolName); !ok {
		t.Fatalf("%s 应在 leader 工具面注册", teamwork.TeamMessageToolName)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"t","milestones":[{"id":"m-a"}],"members":[{"role":"exec"}]}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if _, err := r.teamMessageHandler(ctx, `{"role":"exec","message":"why"}`); err == nil || !strings.Contains(err.Error(), "item") {
		t.Fatalf("缺 item 必须在进入 Chat 前被拒，得到 %v", err)
	}
}

type blockingConversationEngine struct {
	entered chan context.Context
	release chan struct{}
	once    sync.Once
}

func (e *blockingConversationEngine) SessionID() string        { return "teammate-session" }
func (e *blockingConversationEngine) SetSystemPrompt(string)   {}
func (e *blockingConversationEngine) SetMaxLoops(int)          {}
func (e *blockingConversationEngine) ClearHistory()            {}
func (e *blockingConversationEngine) History() []types.Message { return nil }
func (e *blockingConversationEngine) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	e.once.Do(func() { e.entered <- ctx })
	select {
	case <-e.release:
		return "完整回复", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestTeamMessageTimeoutReturnsJobHandleWithoutCancellingChat 钉住有界等待：
// 等不到回复时只结束本次工具等待，返回可继续读取的 job handle；对话本身仍在跑，
// 其 context 不得被工具超时取消。
func TestTeamMessageTimeoutReturnsJobHandleWithoutCancellingChat(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	st := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(st, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"t","milestones":[{"id":"m-a"}],"members":[{"role":"exec"}]}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	coordinator, err := r.coordinatorFor(ctx)
	if err != nil {
		t.Fatalf("coordinatorFor: %v", err)
	}
	plan, err := coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan.Milestones[0].Items = []storeteamwork.WorkItem{{
		ID: "wi-chat", Milestone: "m-a", Role: "exec", Name: "对话",
		Status: storeteamwork.ItemReview, SessionID: "sess-wi-chat",
	}}
	if err := st.WritePlan(ctx, coordinator.Key(), plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	engine := &blockingConversationEngine{entered: make(chan context.Context, 1), release: make(chan struct{})}
	r.SetRoleEngineFactory(func(string) (roleEngine, error) { return engine, nil })

	type result struct {
		receipt dto.ChatReceipt
		err     error
	}
	done := make(chan result, 1)
	go func() {
		receipt, err := r.ChatAgentConversation(ctx, dto.ConversationSessionRef{
			Kind: dto.ConversationTeammate, MainSessionID: "s-team", TeamID: "t",
			Role: "exec", WorkItemID: "wi-chat", SessionID: "sess-wi-chat",
		}, dto.ConversationChatRequest{Message: "为什么失败？", WaitMS: 20})
		done <- result{receipt: receipt, err: err}
	}()
	var jobCtx context.Context
	select {
	case jobCtx = <-engine.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("对话作业未启动")
	}
	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("team_message 的有界等待未返回")
	}
	if got.err != nil || !got.receipt.TimedOut || got.receipt.JobHandle == "" || got.receipt.Reply != "" {
		t.Fatalf("超时必须返回 handle 而不取消对话: receipt=%+v err=%v", got.receipt, got.err)
	}
	if err := jobCtx.Err(); err != nil {
		t.Fatalf("工具等待超时取消了仍在跑的对话: %v", err)
	}
	close(engine.release)
	r.teamworkMu.Lock()
	manager := r.teamworkJobs
	r.teamworkMu.Unlock()
	if manager == nil {
		t.Fatal("teamwork jobs manager is not assembled")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		text, record, peekErr := manager.Peek(context.Background(), jobs.Handle(got.receipt.JobHandle), jobs.FetchBudget{WaitMS: 50})
		if peekErr != nil && !errors.Is(peekErr, jobs.ErrRetired) {
			t.Fatalf("Peek: %v", peekErr)
		}
		if record.State.Terminal() && strings.Contains(text, "完整回复") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("对话未在后台完成: text=%q record=%+v", text, record)
		}
	}
}
