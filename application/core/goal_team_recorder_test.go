package core

// goal_team_recorder_test.go — 接线：TL/EXEC 每回合原文的 role draft 写入与
// **本轮结束即同步**。
//
// 存储层自己钉住 draft 是 sequencer 的 WAL（同步前可见、同步即删、重放幂等）；
// 这里钉的是**接线口径**：每回合原文落 draft 之后，同一回合内必须 sync——
// 只 append 不 sync 的接线会让前端角色快照一直挂着"未同步草稿"（用户看到的
// 草稿行永远同步不掉），而只 sync 不 append 则会丢原文。两条生产者
// （goal_team_recorder.go 的 RecordTLRound / RecordMainTurn）都要走这条通道。

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// draftRecordingSessions 记录 role draft 的两个动作（append / sync）及其顺序，
// 并模拟存储侧 sync 的"发布即删草稿"语义（pending 清空）。它回答的接线问题是：
// 会不会只 append 不 sync（草稿永远同步不掉），或只 sync 不 append（丢原文）。
type draftRecordingSessions struct {
	teamRecordingSessions
	mu      sync.Mutex
	steps   []string
	rows    []dto.RoleDraftRow
	order   []string
	pending int
}

func (s *draftRecordingSessions) AppendRoleDraft(_, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, "append:"+roleName+"@"+roleSessionID)
	s.rows = append(s.rows, rows...)
	s.pending += len(rows)
	return nil
}

func (s *draftRecordingSessions) SyncRoleDraft(_, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, "sync:"+roleName+"@"+roleSessionID)
	s.order = append([]string(nil), order...)
	s.pending = 0 // 存储侧：head+floor 原子发布成功后才删 draft 文件
	return dto.RoleDraftSyncResult{}, nil
}

func (s *draftRecordingSessions) recordedSteps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

func (s *draftRecordingSessions) appendedRows() []dto.RoleDraftRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]dto.RoleDraftRow(nil), s.rows...)
}

func (s *draftRecordingSessions) syncOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

func (s *draftRecordingSessions) pendingRows() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// draftRecordingFixture 装配一个已建 goal（= 已自动装配 goal-a2a 团队）的会话，
// 返回记录器与团队成员视图。
func draftRecordingFixture(t *testing.T, sessionID string) (*draftRecordingSessions, goaldomain.TLRoundRecorder, dto.TeamView) {
	t.Helper()
	sessions := &draftRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	ctx := withSessionID(context.Background(), sessionID)
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "记录回合"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	view, err := service.AgentTeamView(sessionID)
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if !view.Configured {
		t.Skip("该夹具没有装配 AgentTeam（无团队时记录器按设计 no-op）")
	}
	recorder := service.goalTLRecorderFor(sessionID)
	if recorder == nil {
		t.Fatal("goalTLRecorderFor 对非空 sessionID 必须给出记录器")
	}
	return sessions, recorder, view
}

// TestRecordTLRoundAppendsThenSyncsInTheSameRound 钉住 b（ADVISOR）回合的记录口径：
// 一次 append（role_context 上下文 + tl_directive 裁决）紧跟一次 sync，且 sync 的
// 排序键就是会话的顺序策略——本轮裁决已出 = 本轮结束，原文不留在草稿区。
func TestRecordTLRoundAppendsThenSyncsInTheSameRound(t *testing.T) {
	sessionID := "sess-tl-record"
	sessions, recorder, view := draftRecordingFixture(t, sessionID)
	roleSessionID := agentteam.RoleSessionID(view.TeamID, "tl")
	if roleSessionID == "" {
		t.Fatalf("goal-a2a 团队的 tl 角色会话号必须可派生（team_id=%q）", view.TeamID)
	}
	if err := recorder.RecordTLRound(context.Background(), goaldomain.TLRoundRecord{
		Trigger: "signal:step_checkpoint", RefSeq: 2,
		Context: "本轮送给 ADVISOR 的原文", Output: "裁决：continue",
	}); err != nil {
		t.Fatalf("RecordTLRound: %v", err)
	}

	steps := sessions.recordedSteps()
	want := []string{"append:tl@" + roleSessionID, "sync:tl@" + roleSessionID}
	if len(steps) != len(want) || steps[0] != want[0] || steps[1] != want[1] {
		t.Fatalf("回合记录步骤 = %v，want %v（append 在前、同一回合内 sync）", steps, want)
	}
	if got := strings.Join(sessions.syncOrder(), ">"); got != strings.Join(view.OrderRoles, ">") {
		t.Fatalf("sync 排序键 = %q，want 会话顺序 %q（sequencer 的排序凭据）", got, strings.Join(view.OrderRoles, ">"))
	}
	if pending := sessions.pendingRows(); pending != 0 {
		t.Fatalf("本轮结束后仍有 %d 行未同步草稿（前端会一直挂着草稿区）", pending)
	}

	rows := sessions.appendedRows()
	if len(rows) != 2 {
		t.Fatalf("一回合原文 = %d 行，want 2（它看到的上下文 + 它的原始回答）：%+v", len(rows), rows)
	}
	if rows[0].Event.Kind != "role_context" || rows[0].Event.Role != "system" ||
		rows[0].Event.Content != "本轮送给 ADVISOR 的原文" {
		t.Fatalf("第一行必须是它本轮看到的上下文原文：%+v", rows[0])
	}
	if rows[1].Event.Kind != goaldomain.DirectiveRowKind || rows[1].Event.Role != "assistant" ||
		rows[1].Event.Content != "裁决：continue" {
		t.Fatalf("第二行必须是裁决原文：%+v", rows[1])
	}
	// unit_seq 是 sequencer 的排序键，也是前端草稿区「round/unit」标签的来源：
	// 同回合内必须有序且非零。
	if rows[0].UnitSeq == 0 || rows[1].UnitSeq <= rows[0].UnitSeq {
		t.Fatalf("同一回合的 unit_seq 必须递增且非零：%+v", rows)
	}
	for _, row := range rows {
		if row.RoleName != "tl" || row.RoleSessionID != roleSessionID {
			t.Fatalf("草稿行必须带角色归属（否则同步后无法按角色渲染）：%+v", row)
		}
		// seq 由 sequencer 在同步时分配——记录器不推演 seq；前端据此把草稿
		// 与已发布行分开渲染（草稿没有 seq 列）。
		if row.Event.Seq != 0 {
			t.Fatalf("草稿行不该带 message seq（分配 seq 是 sync 的事）：%+v", row)
		}
	}
}

// TestRecordMainTurnPublishesHostMarkerInTheSameRound 钉住 EXEC 主持标记：
// b 交还发言权时落一行 round_host 到 main 角色 draft，并在同一回合内发布。
func TestRecordMainTurnPublishesHostMarkerInTheSameRound(t *testing.T) {
	sessionID := "sess-main-turn"
	sessions, recorder, _ := draftRecordingFixture(t, sessionID)
	if err := recorder.RecordMainTurn(context.Background(), goaldomain.MainTurnRecord{
		Trigger: "signal:step_checkpoint", RoundID: 4, Directive: goaldomain.DirectiveVerdictNotDone,
	}); err != nil {
		t.Fatalf("RecordMainTurn: %v", err)
	}
	steps := sessions.recordedSteps()
	want := []string{"append:main@" + sessionID, "sync:main@" + sessionID}
	if len(steps) != len(want) || steps[0] != want[0] || steps[1] != want[1] {
		t.Fatalf("EXEC 主持标记步骤 = %v，want %v", steps, want)
	}
	rows := sessions.appendedRows()
	if len(rows) != 1 || rows[0].Event.Kind != "round_host" || rows[0].RoundID != 4 {
		t.Fatalf("主持标记行 = %+v，want 单行 round_host / round 4", rows)
	}
	if !strings.Contains(rows[0].Event.Content, "本轮由 EXEC 主持") {
		t.Fatalf("主持标记必须自述身份（前端按 role_name 归到 EXEC 名下）：%q", rows[0].Event.Content)
	}
	if pending := sessions.pendingRows(); pending != 0 {
		t.Fatalf("交还发言权后仍有 %d 行未同步草稿", pending)
	}
}

// TestRecordTLRoundKeepsTheOriginalWhenTheRoundIsCancelled 钉住「取消不丢内容」：
// 记录器刻意不因 ctx 取消而丢原文——只要本回合已出裁决（runRoundLocked 只在
// Evaluate 成功后调用记录器），原文照落、照同步；取消发生在评估阶段时根本走不到
// 记录器，因此不会留下悬空草稿。两种情形都不会出现"取消丢内容"或"取消后草稿挂着"。
func TestRecordTLRoundKeepsTheOriginalWhenTheRoundIsCancelled(t *testing.T) {
	sessionID := "sess-tl-cancelled"
	sessions, recorder, _ := draftRecordingFixture(t, sessionID)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := recorder.RecordTLRound(cancelled, goaldomain.TLRoundRecord{
		Trigger: "signal:step_checkpoint", Context: "被取消的这一轮上下文", Output: "被取消的这一轮裁决",
	}); err != nil {
		t.Fatalf("RecordTLRound（已取消的 ctx）: %v", err)
	}
	steps := sessions.recordedSteps()
	if len(steps) != 2 || !strings.HasPrefix(steps[0], "append:") || !strings.HasPrefix(steps[1], "sync:") {
		t.Fatalf("已出裁决的回合即使 ctx 取消也必须 append + sync（不丢原文）：%v", steps)
	}
	if pending := sessions.pendingRows(); pending != 0 {
		t.Fatalf("取消后不得留下未同步草稿：%d 行", pending)
	}
}

// TestRecordTLRoundWritesNothingWithoutATeam 钉住降级：未装配 team 的会话不落
// role draft（记录器按设计 no-op，不报错、不产生悬空草稿）。
func TestRecordTLRoundWritesNothingWithoutATeam(t *testing.T) {
	sessions := &draftRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	recorder := service.goalTLRecorderFor("sess-without-team")
	if recorder == nil {
		t.Fatal("goalTLRecorderFor 对非空 sessionID 必须给出记录器")
	}
	if err := recorder.RecordTLRound(context.Background(), goaldomain.TLRoundRecord{
		Context: "无团队的会话", Output: "不应落盘",
	}); err != nil {
		t.Fatalf("未装配 team 的会话记录必须 no-op 且不报错: %v", err)
	}
	if steps := sessions.recordedSteps(); len(steps) != 0 {
		t.Fatalf("未装配 team 的会话不得写 role draft，实际步骤 = %v", steps)
	}
}
