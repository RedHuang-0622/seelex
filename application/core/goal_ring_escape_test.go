package core

// goal_ring_escape_test.go — 应用层接线：环逃生 → goal 收口 + b 侧历史归档。
//
// 域层语义由 application/core/goal/escape_test.go 钉住；这里钉的是**接线**：
// 环的记账判定逃生之后，coordinator 必须真的把 goal 收口，并且把归档行经
// tl 角色 draft 通道落下去（role_name=tl，kind=goal_archive）。缺了这条接线，
// "逃生 = 这一轮 goal 结束"就只是域里的能力，运行时仍然是"环停了、goal 挂着"。

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// escapeRecordingSessions 记录落到角色 draft 的行（逃生归档走的就是这条通道）。
type escapeRecordingSessions struct {
	teamRecordingSessions
	draftMu sync.Mutex
	drafts  []dto.RoleDraftRow
}

func (s *escapeRecordingSessions) AppendRoleDraft(_ string, _ string, _ string, rows []dto.RoleDraftRow) error {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	s.drafts = append(s.drafts, rows...)
	return nil
}

func (s *escapeRecordingSessions) draftsOfKind(kind string) []dto.RoleDraftRow {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	var out []dto.RoleDraftRow
	for _, row := range s.drafts {
		if row.Event.Kind == kind {
			out = append(out, row)
		}
	}
	return out
}

// TestRingEscapeClosesGoalAndArchivesTLHistory：逃生后 goal 必须收口，且 tl 角色
// 历史里必须留下收口归档行（带逃生原因与 goal 身份）。
func TestRingEscapeClosesGoalAndArchivesTLHistory(t *testing.T) {
	sessions := &escapeRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.SetGoalTLEvaluator(&capturingTLEvaluator{})

	sessionID := "sess-ring-escape"
	ctx := withSessionID(context.Background(), sessionID)
	// 装配是显式动作：goal 上线不再自动装配团队（2026-10-01，见
	// service.GoalBeginFor）。这条用例要的"团队环 + tl 角色 draft"来自装配本身。
	if _, err := service.MaterializeAgentTeam(sessionID, goalTeamFixture(), 0); err != nil {
		t.Skipf("该夹具不能装配团队: %v", err)
	}
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "逃生目标"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	ring := service.teamRuntimeBySession(sessionID)
	if ring == nil {
		t.Skip("该夹具没有团队环（未装配 AgentTeam 存储）")
	}

	// 走到逃生：环按"有没有推进"记账，这里连续报无产出（默认上限内必然触发）。
	stopped, reason := false, ""
	for attempt := 0; attempt < 512 && !stopped; attempt++ {
		stopped, reason = ring.NoteTurn(false)
	}
	if !stopped {
		t.Fatal("连续无产出必须能触发环逃生（否则轮次上限/无进展兜底形同虚设）")
	}
	switch reason {
	case agentteam.StopNoProgress, agentteam.StopRoundLimit, agentteam.StopEmptyRing, agentteam.StopNoAutomaticTurn:
	default:
		t.Fatalf("逃生原因 %q 不是已知的逃生原因", reason)
	}

	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		t.Fatalf("goalCoordinatorFor: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, sessionID, "本轮无产出"); err != nil {
		t.Fatalf("逃生推进不应报错: %v", err)
	}

	// 1. goal 收口：栈里不再有 active goal，历史 +1。
	status, err := service.GoalStatusFor(sessionID)
	if err != nil {
		t.Fatalf("GoalStatusFor: %v", err)
	}
	if len(status.Stack) != 0 {
		t.Fatalf("逃生后不应还有 active goal，stack=%d（否则面板会一直显示一个不会推进的循环）", len(status.Stack))
	}
	if status.History == 0 {
		t.Fatal("逃生收口必须落进 history（用户要能看到这一轮 goal 到此为止）")
	}

	// 2. b 侧历史归档：tl 角色 draft 里必须有一条 goal_archive 行。
	archives := sessions.draftsOfKind(goaldomain.ArchiveKindEscape)
	if len(archives) != 1 {
		t.Fatalf("逃生归档行数 = %d, want 1（归档要经 tl 角色 draft 留痕）", len(archives))
	}
	archive := archives[0]
	if archive.RoleName != "tl" {
		t.Fatalf("归档行角色 = %q, want tl", archive.RoleName)
	}
	if !strings.Contains(archive.RoleSessionID, "tl") {
		t.Fatalf("归档行必须落在 tl 角色会话上，得到 %q", archive.RoleSessionID)
	}
	if !strings.Contains(archive.Event.Content, reason) {
		t.Fatalf("归档行必须带上逃生原因 %q，实际:\n%s", reason, archive.Event.Content)
	}
}

// TestRingEscapeWithoutGoalIsQuiet：没有 goal 时环逃生不应报错、不应写归档
// （逃生可能被多处触发，重复/无 goal 都必须安全）。
func TestRingEscapeWithoutGoalIsQuiet(t *testing.T) {
	sessions := &escapeRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.SetGoalTLEvaluator(&capturingTLEvaluator{})

	sessionID := "sess-ring-escape-nogoal"
	ctx := withSessionID(context.Background(), sessionID)
	if _, err := service.MaterializeAgentTeam(sessionID, goalTeamFixture(), 0); err != nil {
		t.Skipf("该夹具不能装配团队: %v", err)
	}
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "先建再收"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	ring := service.teamRuntimeBySession(sessionID)
	if ring == nil {
		t.Skip("该夹具没有团队环（未装配 AgentTeam 存储）")
	}
	stopped, reason := false, ""
	for attempt := 0; attempt < 512 && !stopped; attempt++ {
		stopped, reason = ring.NoteTurn(false)
	}
	if !stopped {
		t.Fatal("应先触发逃生")
	}
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		t.Fatalf("goalCoordinatorFor: %v", err)
	}
	// 第一次：正常收口。
	if err := coordinator.AdvanceAfterChat(ctx, sessionID, "无产出"); err != nil {
		t.Fatalf("首次逃生: %v", err)
	}
	// 第二次：goal 已经收口，重复逃生必须安静。
	if err := coordinator.AdvanceAfterChat(ctx, sessionID, "无产出"); err != nil {
		t.Fatalf("重复逃生必须幂等，得到: %v", err)
	}
	if got := len(sessions.draftsOfKind(goaldomain.ArchiveKindEscape)); got != 1 {
		t.Fatalf("重复逃生不应重复归档，归档行数 = %d", got)
	}
	_ = reason
}
