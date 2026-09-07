package gui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goalRPCFakeApp 复刻 headless goal 链路的 Application 扩展面：记录调用
// 路由到的会话并把 begin/update/propose_finish 映射为治理视图状态迁移，
// 供 headless 分发表复刻验收（真实 Service 侧行为由 application/core 测试
// 覆盖；本测试钉住 gui → goal.<method> 接线）。
type goalRPCFakeApp struct {
	*fakeApplication
	mu      sync.Mutex
	active  *goaldomain.GoalRecord
	view    *dto.GoalGovernanceView
	lastSID string
}

func (fake *goalRPCFakeApp) GoalBeginFor(_ context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	fake.active = &goaldomain.GoalRecord{ID: "g-1", Title: request.Title, Status: goaldomain.StatusActive}
	fake.view = &dto.GoalGovernanceView{
		Active: true, GoalID: "g-1", Title: request.Title, Status: string(goaldomain.StatusActive),
		HeartbeatSeq: 1,
	}
	return fake.active, nil
}

func (fake *goalRPCFakeApp) GoalUpdateFor(_ context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	if fake.active != nil && request.ProgressContent != "" {
		fake.active.Progress = append(fake.active.Progress, goaldomain.Progress{Kind: request.ProgressKind, Content: request.ProgressContent})
	}
	fake.view.HeartbeatSeq++
	return fake.active, nil
}

func (fake *goalRPCFakeApp) GoalProposeFinishFor(_ context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	if fake.active != nil {
		fake.active.Status = goaldomain.StatusCompleted
		fake.view = &dto.GoalGovernanceView{Active: false, HeartbeatSeq: fake.view.HeartbeatSeq + 1}
	}
	return goaldomain.FinishProposalResult{Outcome: goaldomain.OutcomeCompleted, Goal: fake.active}, nil
}

func (fake *goalRPCFakeApp) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	view := goaldomain.StatusView{}
	if fake.active != nil {
		view.Stack = []*goaldomain.GoalRecord{fake.active}
		view.Active = fake.active
	}
	return view, nil
}

func (fake *goalRPCFakeApp) GoalNextFor(context.Context, string) (bool, error) { return false, nil }

func (fake *goalRPCFakeApp) GoalBreakFor(context.Context, string, string) error { return nil }

func (fake *goalRPCFakeApp) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	if fake.view == nil {
		return nil
	}
	copyView := *fake.view
	return &copyView
}

// TestHeadlessGoalChainReplication 复刻验收 goal 治理的 gui headless 链路：
// goal.begin → 视图 active；goal.update/status/gov_snapshot 与当前视图会话
// 路由一致；goal.propose_finish 收口后视图复位；gov_break 可达；未知方法
// 显式报错。
func TestHeadlessGoalChainReplication(t *testing.T) {
	base := newFakeApplication()
	base.snapshot.Session.ID = "session-gui-headless"
	fake := &goalRPCFakeApp{fakeApplication: base}
	serverBase := newHeadlessTestServer(t, fake)

	result := headlessRPC(t, serverBase, "goal.begin", map[string]any{"title": "审查 goal 域"})
	if !result.OK {
		t.Fatalf("goal.begin failed: %s", result.Error)
	}
	var record goaldomain.GoalRecord
	if err := remarshal(result.Result, &record); err != nil {
		t.Fatalf("decode goal.begin result: %v", err)
	}
	if record.ID != "g-1" || record.Title != "审查 goal 域" {
		t.Fatalf("goal.begin record = %+v", record)
	}
	if fake.lastSID != "session-gui-headless" {
		t.Fatalf("goal.begin routed to session %q", fake.lastSID)
	}

	result = headlessRPC(t, serverBase, "goal.gov_snapshot")
	if !result.OK {
		t.Fatalf("goal.gov_snapshot failed: %s", result.Error)
	}
	var view dto.GoalGovernanceView
	if err := remarshal(result.Result, &view); err != nil {
		t.Fatalf("decode gov_snapshot: %v", err)
	}
	if !view.Active || view.GoalID != "g-1" || view.Status != string(goaldomain.StatusActive) {
		t.Fatalf("gov_snapshot = %+v", view)
	}

	result = headlessRPC(t, serverBase, "goal.status")
	if !result.OK {
		t.Fatalf("goal.status failed: %s", result.Error)
	}
	var status goaldomain.StatusView
	if err := remarshal(result.Result, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Active == nil || status.Active.Title != "审查 goal 域" {
		t.Fatalf("status = %+v", status)
	}

	result = headlessRPC(t, serverBase, "goal.update", map[string]any{
		"progress_kind": "milestone", "progress_content": "已补负路径单测",
	})
	if !result.OK {
		t.Fatalf("goal.update failed: %s", result.Error)
	}
	if len(fake.active.Progress) != 1 || fake.active.Progress[0].Content != "已补负路径单测" {
		t.Fatalf("progress after update = %+v", fake.active.Progress)
	}

	result = headlessRPC(t, serverBase, "goal.propose_finish", map[string]any{"result": "单测全绿"})
	if !result.OK {
		t.Fatalf("goal.propose_finish failed: %s", result.Error)
	}
	var proposal goaldomain.FinishProposalResult
	if err := remarshal(result.Result, &proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.Goal == nil || proposal.Goal.Status != goaldomain.StatusCompleted {
		t.Fatalf("proposal = %+v", proposal)
	}
	viewAfter := fake.GoalGovernanceViewFor("session-gui-headless")
	if viewAfter.Active {
		t.Fatalf("收口后治理视图应复位: %+v", viewAfter)
	}

	if result := headlessRPC(t, serverBase, "goal.gov_break", map[string]any{"reason": "外部中断"}); !result.OK {
		t.Fatalf("goal.gov_break failed: %s", result.Error)
	}
	if result := headlessRPC(t, serverBase, "goal.no_such"); result.OK ||
		!strings.Contains(result.Error, "未知 goal headless 方法") {
		t.Fatalf("unknown goal method should fail: ok=%v error=%q", result.OK, result.Error)
	}
}

func remarshal(value any, destination any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, destination)
}
