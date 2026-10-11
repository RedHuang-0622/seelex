package core

// goal_team_recorder_test.go —— 接线：TL/EXEC 每回合原文的 role draft 写入面。
//
// 存储层自己钉住 draft → sequencer → WAL 的语义（同步前可见、同步即删、重放幂等）；
// 这里原本钉的是**接线口径**：每回合原文 append 到 draft 之后，同一回合内必须 sync。
//
// **2026-10-12（J-1a）之后这条接线不可达**：记录器的第一道守卫是"本会话已装配团队"
// （`sessionAgentTeamViewJ1A`），而那个读面的唯一事实来源——团队注册表——已随 Agent
// Team 用户面整条退场，于是读面恒报未装配 ⇒ `RecordTLRound` / `RecordMainTurn` /
// `ArchiveTLHistory` 一律在守卫处 no-op。因此本文件只留：
//
//  1. 一条**仍然成立**的降级断言（未装配团队 → no-op、不写 role draft，逐字未改）；
//  2. 一条**退场守卫**把"记录器不再落任何原文"这件事记下来，免得下一个人看到
//     `AppendRoleDraft` / `SyncRoleDraft` 还在（B1 保留面，`role_session.go`）就以为
//     有一条活的调用链。
//
// 保留面完好：`AppendRoleDraft` / `SyncRoleDraft` / `RoleSnapshot` / 角色 draft 的
// 存储实现与 headless RPC 一字未改（见 application/core/role_session.go、
// sessionstore/role_session*.go、gui/headless.go 的 `role.*`）。

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// draftRecordingSessions 记录 role draft 的两个动作（append / sync）及其顺序，
// 并模拟存储侧 sync 的"发布即删草稿"语义（pending 清空）。
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

func (s *draftRecordingSessions) pendingRows() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// TestRecordTLRoundWritesNothingWithoutATeam 钉住降级：未装配 team 的会话不落
// role draft（记录器按设计 no-op，不报错、不产生悬空草稿）。
//
// J-1a 之后这是**唯一可达**的那一支——每一条来路都走到这里。
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

// TestRoleDraftRecordingIsUnreachableAfterJ1A 是**退场守卫**（2026-10-12 J-1a）：
// 三条记录来路在任何会话上都只走到"未装配团队"那一支，因此都不落任何 role draft。
//
// 为什么值得一条用例：本条目删掉的是 Agent Team 的**装配面**，而"每回合原文落 tl 角色
// draft"是它的下游。不把这件事钉下来，`goal_team_recorder.go` 里那份形状完好的代码会
// 被读成"还在落盘"（它的 append→sync 顺序断言确实还在），下一个人就会照旧去改一条
// 永不执行的路径。
func TestRoleDraftRecordingIsUnreachableAfterJ1A(t *testing.T) {
	sessions := &draftRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	ctx := withSessionID(context.Background(), "sess-tl-record")
	if _, err := service.GoalBeginFor(ctx, "sess-tl-record", goaldomain.BeginRequest{Title: "记录回合"}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}
	recorder := service.goalTLRecorderFor("sess-tl-record")
	if recorder == nil {
		t.Fatal("goalTLRecorderFor 必须给出记录器")
	}
	if err := recorder.RecordTLRound(ctx, goaldomain.TLRoundRecord{
		Trigger: "signal:step_checkpoint", RefSeq: 2,
		Context: "本轮送给 ADVISOR 的原文", Output: "裁决：continue",
	}); err != nil {
		t.Fatalf("RecordTLRound: %v", err)
	}
	if err := recorder.RecordMainTurn(ctx, goaldomain.MainTurnRecord{
		Trigger: "signal:step_checkpoint", RoundID: 4, Directive: goaldomain.DirectiveVerdictNotDone,
	}); err != nil {
		t.Fatalf("RecordMainTurn: %v", err)
	}
	archiver, ok := recorder.(goaldomain.TLHistoryArchiver)
	if !ok {
		t.Fatal("goalTLRecorder 必须同时实现 TLHistoryArchiver（逃生收口要归档 b 侧历史）")
	}
	if err := archiver.ArchiveTLHistory(ctx, goaldomain.TLArchiveRecord{
		Reason: "no_progress", Content: "b 侧历史",
	}); err != nil {
		t.Fatalf("ArchiveTLHistory: %v", err)
	}
	if steps := sessions.recordedSteps(); len(steps) != 0 {
		t.Fatalf("会话团队装配面已退场，角色行记录不该有来路，实际步骤 = %v", steps)
	}
	if pending := sessions.pendingRows(); pending != 0 {
		t.Fatalf("不得留下悬空草稿：%d 行", pending)
	}
}
