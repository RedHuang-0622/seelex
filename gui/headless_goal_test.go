package gui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/headless"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

// fakeGoal 满足 headless 的 GoalController 端口（owner：application/goal，按显式
// 会话路由）。方法名逐字等于 owner 的方法名；它把 begin/update/done 映射为治理视图
// 的状态迁移，供 headless 分发表复刻验收（真实 Service 侧行为由 application/goal
// 自己的用例覆盖；本用例钉住 gui → goal.<method> 接线与路由会话）。
type fakeGoal struct {
	mu      sync.Mutex
	active  *goaldomain.GoalRecord
	view    *dto.GoalGovernanceView
	lastSID string
}

func (fake *fakeGoal) BeginFor(_ context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	fake.active = &goaldomain.GoalRecord{ID: "g-1", Title: request.Title, Status: goaldomain.StatusActive}
	fake.view = &dto.GoalGovernanceView{
		Active: true, GoalID: "g-1", Title: request.Title, Status: goaldomain.StatusActive,
	}
	return fake.active, nil
}

func (fake *fakeGoal) UpdateFor(_ context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	if fake.active != nil && request.ProgressContent != "" {
		fake.active.Progress = append(fake.active.Progress, goaldomain.Progress{Kind: request.ProgressKind, Content: request.ProgressContent})
	}
	return fake.active, nil
}

func (fake *fakeGoal) DoneFor(_ context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastSID = sessionID
	if fake.active != nil {
		if abort {
			fake.active.Status = goaldomain.StatusAborted
		} else {
			fake.active.Status = goaldomain.StatusCompleted
		}
		fake.view = &dto.GoalGovernanceView{Active: false}
	}
	return fake.active, nil
}

func (fake *fakeGoal) StatusFor(sessionID string) (goaldomain.StatusView, error) {
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

func (fake *fakeGoal) GovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
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
// 路由一致；goal.done 收口后视图复位；gov_next / gov_break 已随
// 席位轮转退场（与未知方法一样被拒，见文件末尾那条用例）。
func TestHeadlessGoalChainReplication(t *testing.T) {
	fake := &fakeGoal{}
	view := &fakeSnapshot{snap: application.Snapshot{}}
	view.snap.Session.ID = "session-gui-headless"
	serverBase := newHeadlessTestServer(t, headless.Dependencies{Goal: fake, Snapshot: view})

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
	var govView dto.GoalGovernanceView
	if err := remarshal(result.Result, &govView); err != nil {
		t.Fatalf("decode gov_snapshot: %v", err)
	}
	if !govView.Active || govView.GoalID != "g-1" || govView.Status != goaldomain.StatusActive {
		t.Fatalf("gov_snapshot = %+v", govView)
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

	result = headlessRPC(t, serverBase, "goal.done", map[string]any{"result": "单测全绿"})
	if !result.OK {
		t.Fatalf("goal.done failed: %s", result.Error)
	}
	var record2 goaldomain.GoalRecord
	if err := remarshal(result.Result, &record2); err != nil {
		t.Fatalf("decode goal.done result: %v", err)
	}
	if record2.Status != goaldomain.StatusCompleted {
		t.Fatalf("goal.done = %+v", record2)
	}
	viewAfter := fake.GovernanceViewFor("session-gui-headless")
	if viewAfter.Active {
		t.Fatalf("收口后治理视图应复位: %+v", viewAfter)
	}

	// gov_break 已随席位轮转退场：它现在和未知方法一样被拒。
	if result := headlessRPC(t, serverBase, "goal.gov_break", map[string]any{"reason": "外部中断"}); result.OK {
		t.Fatal("goal.gov_break 应随席位轮转退场而被拒")
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
