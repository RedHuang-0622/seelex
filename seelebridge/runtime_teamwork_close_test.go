package seelebridge

// runtime_teamwork_close_test.go — 钉住 team_close 在 Runtime 上的接线（S4a）与它的下游。
//
// 三件事必须各自有事实支撑，不能靠"看起来对"：
//   - 工具在面里（未注入 backend 时**不在**面里，与六件套同口径）；
//   - 收口是**域内**事实：计划标 closed + closed_reason=team.close + 一条 close 审计；
//   - 收口是**幂等**的：第二次调用 already_closed=true，且不重复落审计；
//   - 收口**封板存档**：存档那一版落 state=closed / reason=team.close（读侧只认 active，
//     不封板就会在重启恢复时冒充"在册"）。

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"testing"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// memBoardRepo 是最小的看板存档假体：只实现团队那一半，goal 那一半显式不支持。
type memBoardRepo struct {
	mu     sync.Mutex
	boards map[sessionstore.Key]sessionstore.TeamBoardMeta
}

func (m *memBoardRepo) WriteGoalBoard(context.Context, sessionstore.Key, sessionstore.GoalBoardMeta) error {
	return errors.New("memBoardRepo: goal 看板不在本用例的作用域里")
}

func (m *memBoardRepo) ReadGoalBoard(context.Context, sessionstore.Key) (sessionstore.GoalBoardMeta, error) {
	return sessionstore.GoalBoardMeta{}, fs.ErrNotExist
}

func (m *memBoardRepo) WriteTeamBoard(_ context.Context, key sessionstore.Key, meta sessionstore.TeamBoardMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.boards == nil {
		m.boards = map[sessionstore.Key]sessionstore.TeamBoardMeta{}
	}
	m.boards[key] = meta
	return nil
}

func (m *memBoardRepo) ReadTeamBoard(_ context.Context, key sessionstore.Key) (sessionstore.TeamBoardMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.boards[key]
	if !ok {
		return sessionstore.TeamBoardMeta{}, fs.ErrNotExist
	}
	return meta, nil
}

func (m *memBoardRepo) board(key sessionstore.Key) (sessionstore.TeamBoardMeta, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.boards[key]
	return meta, ok
}

func TestTeamCloseToolRegistered(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if _, ok := r.registry.FindTool("team_close"); ok {
		t.Fatal("未注入 backend 时不应注册 team_close（不是注册一堆永远报错的空壳）")
	}
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	r.registerTeamworkTools()
	if _, ok := r.registry.FindTool("team_close"); !ok {
		t.Fatal("注入 backend 后 team_close 必须在 leader 工具面里（整队收口的唯一入口）")
	}
}

func TestTeamCloseSealsBoardAndIsIdempotent(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	boards := &memBoardRepo{}
	backend := teamworkTestBackend(store, "s-team")
	backend.Boards = boards
	if err := r.SetTeamworkBackend(backend); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}

	// 计划落盘（members 为空：本用例只验收口的收口面，逐人退场由 coordinator 用例覆盖）+
	// 一版在册的存档，收口要能把这一版关掉。
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"team-close","stages":[{"id":"impl","roles":["exec"]}],"members":[]}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if err := boards.WriteTeamBoard(ctx, key, sessionstore.TeamBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			Kind: sessionstore.BoardKindTeam, State: sessionstore.BoardStateActive, Seq: 3, OpenedAt: 100,
		},
		TeamID: "team-close", Version: 1,
	}); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}

	receipt, err := r.teamCloseHandler(ctx, "")
	if err != nil {
		t.Fatalf("team_close: %v", err)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(receipt), &first); err != nil {
		t.Fatalf("team_close 回执不是 JSON: %v（%s）", err, receipt)
	}
	if first["already_closed"] != false {
		t.Fatalf("首次收口应回报 already_closed=false：%s", receipt)
	}

	plan, err := store.ReadPlan(ctx, key)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if plan.State.State != sessionstore.TeamworkStateClosed {
		t.Fatalf("收口必须把域内计划标 closed（域内权威）：%+v", plan.State)
	}
	if plan.State.ClosedReason != sessionstore.BoardCloseTeamClose || plan.State.ClosedAt == 0 {
		t.Fatalf("收口原因/时间必须落域内：%+v", plan.State)
	}
	if len(plan.State.Jobs) != 0 {
		t.Fatalf("收口后句柄投影应清空（作业已被回收）：%+v", plan.State.Jobs)
	}

	closed, ok := boards.board(key)
	if !ok {
		t.Fatal("存档那一版必须还在（它是历史面）")
	}
	if !closed.Closed() || closed.ClosedReason != sessionstore.BoardCloseTeamClose || closed.ClosedAt == 0 {
		t.Fatalf("收口必须封板存档（否则重启恢复会拿一块 active 冒充在册）：%+v", closed.BoardLifecycle)
	}

	events, err := store.ReadEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	closeEvents := 0
	for _, event := range events {
		if event.Kind == sessionstore.TeamworkEventClose {
			closeEvents++
		}
	}
	if closeEvents != 1 {
		t.Fatalf("收口应落**一条** close 审计，得 %d 条：%+v", closeEvents, events)
	}

	// 幂等：第二次既不改事实，也不重复落审计。
	second, err := r.teamCloseHandler(ctx, "")
	if err != nil {
		t.Fatalf("第二次 team_close: %v", err)
	}
	var again map[string]any
	if err := json.Unmarshal([]byte(second), &again); err != nil {
		t.Fatalf("第二次回执不是 JSON: %v（%s）", err, second)
	}
	if again["already_closed"] != true {
		t.Fatalf("第二次收口应回报 already_closed=true：%s", second)
	}
	events, err = store.ReadEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	closeEvents = 0
	for _, event := range events {
		if event.Kind == sessionstore.TeamworkEventClose {
			closeEvents++
		}
	}
	if closeEvents != 1 {
		t.Fatalf("重复收口不得重复落审计（收口事实只有一个）：%d 条", closeEvents)
	}
}
