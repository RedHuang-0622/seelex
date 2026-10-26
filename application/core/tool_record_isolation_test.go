package core

// tool_record_isolation_test.go — 工具记录「阶梯提交与会话隔离」的**会话隔离边界矩阵**
// （docs/2026-10-19-three-defect-fixes 方向 C，§4.4 的 12 条，一条不少）。
//
// 三条纪律各自钉住：
//   - **归属键显式**：读数只认执行上下文给的 owner 主会话，视图会话永不顶替（#8/#13）；
//   - **阶梯提交**：叶子先攒（暂存），收口时一次性同步交给会话累计（同 seq 幂等）；
//   - **落盘门**：草稿一次都不写；落盘前先抽干叶子暂存。
//
// 测试纪律：同步假体（`service_fakes_test.go` 那套），不靠 sleep 定时序——
// 需要等的地方等 channel（`engine.started` / `WaitForIdle`），落盘次数用计数 fake 断言
// （`recordingSnapshotStore.saves`），不读真实文件。

import (
	"context"
	"testing"
	"time"

	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// toolRecordStat 造一格增量读数（失败笔数挂一个原因）。
func toolRecordStat(tool string, success, failure int, reason string) []dto.ToolRecordStat {
	stat := dto.ToolRecordStat{Tool: tool, Success: success, Failure: failure, Total: success + failure}
	if failure > 0 && reason != "" {
		stat.Reasons = []dto.ToolRecordReason{{Reason: reason, Count: failure}}
	}
	return []dto.ToolRecordStat{stat}
}

// subagentReading 造一条子代理工具读数（执行单元 = subagent:<nodeID>）。
func subagentReading(tool string, status dto.ToolEventStatus, errorText string) seelsession.SubagentToolEvent {
	return seelsession.SubagentToolEvent{ID: "evt-" + tool, NodeID: "node-a", Name: tool, Status: status, Error: errorText}
}

// totals 读某会话的累计三数（不存在 ⇒ 全 0）。
func totals(service *Service, sessionID string) (int, int, int) {
	view := service.toolRecords.SessionView(sessionID)
	return view.Total, view.Success, view.Failure
}

// toolRecordHarness 是"两个真实会话 + 计数落盘口"的夹具：A 由一次真回合物化
// （草稿 → 真实会话），B 只登记进引擎与目录（切过去即可，不需要跑回合）。
type toolRecordHarness struct {
	t        *testing.T
	engine   *multiSessionEngine
	sessions *liveCatalogSessions
	service  *Service
	store    *recordingSnapshotStore
	aID      string
	bID      string
}

func newToolRecordHarness(t *testing.T) *toolRecordHarness {
	t.Helper()
	const bID = "sess-view-B"
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	store := &recordingSnapshotStore{}
	enableToolRecord(t, service, store)

	// A：真回合物化（草稿 → 真实会话），随后空闲。
	if err := service.Submit(context.Background(), "A 的任务"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	if aID == "" {
		t.Fatal("A 没有拿到会话 ID")
	}
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatal("A 没有开始跑")
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("A 未收尾：%v", err)
	}
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("视图会话 = %q，want A=%q", got, aID)
	}
	sessions.setInfos([]model.SessionInfo{{ID: aID}, {ID: bID}})
	engine.register(bID)
	return &toolRecordHarness{t: t, engine: engine, sessions: sessions, service: service, store: store, aID: aID, bID: bID}
}

// switchToB 走**生产切换路径**（ResumeSession），不是直接改快照字段。
func (harness *toolRecordHarness) switchToB() {
	harness.t.Helper()
	if err := harness.service.ResumeSession(harness.bID); err != nil {
		harness.t.Fatalf("切到 B: %v", err)
	}
	if got := harness.service.Snapshot().Session.ID; got != harness.bID {
		harness.t.Fatalf("视图会话 = %q，want B=%q", got, harness.bID)
	}
}

func (harness *toolRecordHarness) switchToA() {
	harness.t.Helper()
	if err := harness.service.ResumeSession(harness.aID); err != nil {
		harness.t.Fatalf("切回 A: %v", err)
	}
	if got := harness.service.Snapshot().Session.ID; got != harness.aID {
		harness.t.Fatalf("视图会话 = %q，want A=%q", got, harness.aID)
	}
}

// ── 用例 1：A 的子代理在跑、视图切到 B，A 完成后只归 A，B 一分不动 ─────────────

func TestToolRecordSubagentReadingStaysWithOwnerAcrossViewSwitch(t *testing.T) {
	harness := newToolRecordHarness(t)
	service := harness.service
	harness.switchToB()

	// A 的子代理在跑（视图已经是 B）：读数带的是**执行上下文**的归属 A。
	service.HandleSubagentToolEventFor(harness.aID, seelsession.SubagentToolEvent{
		ID: "evt-1", NodeID: "node-a", Name: "bash", Status: dto.ToolEventSuccess,
	})
	// 节点收口（终态）：一次性提交给 A 的会话累计。
	service.closeSubagentToolRecordUnit(harness.aID, "node-a")

	if total, success, failure := totals(service, harness.aID); total != 1 || success != 1 || failure != 0 {
		t.Fatalf("A 应该 +1：total=%d success=%d failure=%d", total, success, failure)
	}
	if total, _, _ := totals(service, harness.bID); total != 0 {
		t.Fatalf("B 一分都不该动（切会话串写复发）：%d", total)
	}
	if board := service.ToolRecordBoard(); board.Total != 0 {
		t.Fatalf("B 的看板必须是空的：%+v", board)
	}
	// A 的读数要能落盘（会话仍真实存在，只是不在视图上）。
	service.FlushToolRecord(harness.aID)
	saves := harness.store.savesSnapshot()
	if len(saves) != 1 {
		t.Fatalf("A 的读数应在一次落盘里写出去：%d", len(saves))
	}
	if saves[0].Tools[0].Tool != "bash" || saves[0].Tools[0].Success != 1 {
		t.Fatalf("落盘内容不对：%+v", saves[0].Tools)
	}
}

// ── 用例 2：A、B 各自的子代理并发完成，各归各位 ────────────────────────────

func TestToolRecordConcurrentSubagentsStayInOwnSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	const aID, bID = "sess-conc-a", "sess-conc-b"

	service.HandleSubagentToolEventFor(aID, seelsession.SubagentToolEvent{ID: "a1", NodeID: "node-a", Name: "bash", Status: dto.ToolEventSuccess})
	service.HandleSubagentToolEventFor(bID, seelsession.SubagentToolEvent{ID: "b1", NodeID: "node-b", Name: "read_file", Status: dto.ToolEventError, Error: "permission denied"})
	service.HandleSubagentToolEventFor(aID, seelsession.SubagentToolEvent{ID: "a2", NodeID: "node-a", Name: "bash", Status: dto.ToolEventSuccess})
	service.closeSubagentToolRecordUnit(aID, "node-a")
	service.closeSubagentToolRecordUnit(bID, "node-b")

	if total, success, _ := totals(service, aID); total != 2 || success != 2 {
		t.Fatalf("A：total=%d success=%d（want 2/2）", total, success)
	}
	if total, _, failure := totals(service, bID); total != 1 || failure != 1 {
		t.Fatalf("B：total=%d failure=%d（want 1/1）", total, failure)
	}
	stats := service.toolRecords.SessionView(aID).Tools
	if len(stats) != 1 || stats[0].Tool != "bash" {
		t.Fatalf("A 只该有 bash 一格：%+v", stats)
	}
	bStats := service.toolRecords.SessionView(bID).Tools
	if len(bStats) != 1 || bStats[0].Reasons[0].Reason != dto.ToolReasonPermission {
		t.Fatalf("B 只该有 read_file 一格且原因归 permission：%+v", bStats)
	}
}

// ── 用例 3：teammate 的 MainSessionID 为空 ⇒ 丢弃，不落到任何会话 ──────────────

func TestToolRecordTeammateWithoutMainSessionIsDropped(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, &recordingSnapshotStore{})

	// 视图会话在场：它绝不能被当成兜底归属。
	viewSessionID := service.currentViewSessionID()
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "role#1", RoleSessionID: "role-impl", Name: "bash",
		Status: dto.ToolEventError.String(), Error: "permission denied",
	})
	diagnostics := service.toolRecords.Diagnostics()
	if diagnostics.SessionCount != 0 || service.toolRecords.SessionView(viewSessionID).Exists {
		t.Fatalf("没有归属的读数不许落到视图会话（累计表条目 %d）：%v", diagnostics.SessionCount, viewSessionID)
	}
	if total, _, _ := totals(service, viewSessionID); total != 0 {
		t.Fatalf("没有归属的读数不许落到视图会话：%d", total)
	}
	if count := service.toolRecordDroppedReadings()[toolRecordDropNoOwner]; count != 1 {
		t.Fatalf("丢弃必须可见（诊断计数）：%+v", service.toolRecordDroppedReadings())
	}
	if units := service.toolRecords.Diagnostics().UnitCount; units != 0 {
		t.Fatalf("丢弃的读数也不该建出暂存：%d", units)
	}
}

// ── 用例 4：同一单元同 seq 重复提交 ⇒ 只计一次 ────────────────────────────

func TestToolRecordCommitIsIdempotentPerSeq(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	const sessionID = "sess-idem"

	service.commitUnitToolRecord(sessionID, "subagent:node-a", 3, toolRecordStat("bash", 1, 0, ""))
	service.commitUnitToolRecord(sessionID, "subagent:node-a", 3, toolRecordStat("bash", 1, 0, ""))
	if total, _, _ := totals(service, sessionID); total != 1 {
		t.Fatalf("同 seq 重复提交只计一次：%d", total)
	}
	// 往下退的 seq 同样不算（乱序到达的老批次）。
	service.commitUnitToolRecord(sessionID, "subagent:node-a", 2, toolRecordStat("bash", 1, 0, ""))
	if total, _, _ := totals(service, sessionID); total != 1 {
		t.Fatalf("旧 seq 不得覆盖：%d", total)
	}
}

// ── 用例 5：同一单元 seq 递增两次提交 ⇒ 两次都计入（增量语义）────────────────

func TestToolRecordCommitAccumulatesAcrossSeqs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	const sessionID = "sess-seq"

	service.commitUnitToolRecord(sessionID, "role:role-impl", 1, toolRecordStat("bash", 1, 0, ""))
	service.commitUnitToolRecord(sessionID, "role:role-impl", 2, toolRecordStat("bash", 1, 1, dto.ToolReasonTimeout))
	if total, success, failure := totals(service, sessionID); total != 3 || success != 2 || failure != 1 {
		t.Fatalf("两次增量都要计入：total=%d success=%d failure=%d", total, success, failure)
	}
	stats := service.toolRecords.SessionView(sessionID).Tools
	if len(stats) != 1 || stats[0].Total != 3 || stats[0].Reasons[0].Count != 1 {
		t.Fatalf("同一工具名合并成一格：%+v", stats)
	}
}

// ── 用例 6：草稿会话连跑两回合 ⇒ 落盘 0 次 ───────────────────────────────

// draftService 造一个**仍是草稿**的 Service（引擎还没建 bundle：HasSession=false）。
func draftService(t *testing.T) (*Service, *recordingSnapshotStore) {
	t.Helper()
	service := newTestService(t, &fakeEngine{lazyStart: true})
	store := &recordingSnapshotStore{}
	enableToolRecord(t, service, store)
	if !service.Snapshot().Session.Draft {
		t.Fatalf("夹具要求视图仍是草稿：%+v", service.Snapshot().Session)
	}
	return service, store
}

func TestToolRecordDraftSessionNeverWrites(t *testing.T) {
	service, store := draftService(t)
	draftID := service.Snapshot().Session.ID

	// 两回合：每回合都有读数、都在回合末尾那次落盘调用上过一遍。
	for round := 0; round < 2; round++ {
		service.stageUnitToolOutcome(draftID, "subagent:node-a", "bash", true, "")
		service.closeSubagentToolRecordUnit(draftID, "node-a")
		service.FlushToolRecord(draftID)
	}
	if saves := store.savesSnapshot(); len(saves) != 0 {
		t.Fatalf("草稿一次都不许写盘：%d 次 %+v", len(saves), saves)
	}
	// 读数不丢：仍在内存里（物化时才写出去）。
	if total, _, _ := totals(service, draftID); total != 2 {
		t.Fatalf("草稿的读数要在内存里攒着：%d", total)
	}
}

// ── 用例 7：草稿物化后 ⇒ 恰好 1 次写入，且含子代理/teammate 的读数 ────────────

func TestToolRecordDraftMaterializationWritesOnceWithLeafReadings(t *testing.T) {
	service, store := draftService(t)
	draftID := service.Snapshot().Session.ID

	// 物化之前：子代理与 teammate 各记一笔（叶子暂存 → 收口到会话累计）。
	service.stageUnitToolOutcome(draftID, "subagent:node-a", "bash", true, "")
	service.closeSubagentToolRecordUnit(draftID, "node-a")
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "role#1", MainSessionID: draftID, RoleSessionID: "role-impl", Name: "read_file",
		Status: dto.ToolEventError.String(), Error: "permission denied",
	})
	service.CloseRoleToolRecordUnit(draftID, "role-impl")

	// 首条提交 = 物化进真相源（唯一的落盘点）。
	engine, ok := service.Deps.Engine.(*fakeEngine)
	if !ok {
		t.Fatalf("夹具引擎类型不对：%T", service.Deps.Engine)
	}
	if err := service.Submit(context.Background(), "第一条问题"); err != nil {
		t.Fatalf("提交物化：%v", err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("回合未收尾：%v", err)
	}
	if got := service.Snapshot().Session.ID; got != draftID {
		t.Fatalf("物化应复用早分配 SID：%q want %q", got, draftID)
	}
	if !engine.HasSession(draftID) {
		t.Fatalf("物化后引擎必须已有该会话：%q", draftID)
	}
	saves := store.savesSnapshot()
	if len(saves) != 1 {
		t.Fatalf("物化后恰好 1 次写入：%d", len(saves))
	}
	if len(saves[0].Tools) != 2 {
		t.Fatalf("这一份快照要含子代理与 teammate 两笔：%+v", saves[0].Tools)
	}
	names := map[string]int{}
	for _, stat := range saves[0].Tools {
		names[stat.Tool] = stat.Total
	}
	if names["bash"] != 1 || names["read_file"] != 1 {
		t.Fatalf("读数内容不对：%+v", names)
	}
}

// ── 用例 8：离开卖点前未提交的叶子暂存 ⇒ 落盘前被抽干，计数不丢 ────────────────

func TestToolRecordFlushDrainsUnclosedLeafStages(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	store := &recordingSnapshotStore{}
	enableToolRecord(t, service, store)
	sessionID := service.currentViewSessionID()

	// 叶子只攒不收口（节点还在跑）。
	service.stageUnitToolOutcome(sessionID, "subagent:node-a", "bash", true, "")
	service.stageUnitToolOutcome(sessionID, "role:role-impl", "read_file", true, "permission denied")
	if total, _, _ := totals(service, sessionID); total != 0 {
		t.Fatalf("没收口前会话累计不该有读数（阶梯提交的意义）：%d", total)
	}

	service.FlushToolRecord(sessionID)
	saves := store.savesSnapshot()
	if len(saves) != 1 {
		t.Fatalf("落盘前必须先抽干暂存：%d", len(saves))
	}
	if len(saves[0].Tools) != 2 {
		t.Fatalf("抽干后两笔都要在快照里：%+v", saves[0].Tools)
	}
	if total, _, _ := totals(service, sessionID); total != 2 {
		t.Fatalf("抽干 = 入账：%d", total)
	}
	// 暂存已清空：再落一次不会重复计入（没有新读数 ⇒ 不写第二行）。
	service.FlushToolRecord(sessionID)
	if len(store.savesSnapshot()) != 1 {
		t.Fatalf("没有再记过就不该重写：%d", len(store.savesSnapshot()))
	}
	if total, _, _ := totals(service, sessionID); total != 2 {
		t.Fatalf("重复抽干不得重复计数：%d", total)
	}
}

// ── 用例 9：A→B→A 来回切 ⇒ 看板只显示自己，LedgerError 不串 ─────────────────

func TestToolRecordBoardFollowsViewWithoutCrossing(t *testing.T) {
	harness := newToolRecordHarness(t)
	service := harness.service

	service.RecordToolCommandOutcome(harness.aID, "bash", 0)
	service.RecordToolCommandOutcome(harness.aID, "bash", 1)
	service.recordToolOutcome(harness.bID, "read_file", true, "timeout")

	harness.switchToB()
	if board := service.ToolRecordBoard(); board.Total != 1 || board.Tools[0].Tool != "read_file" {
		t.Fatalf("看板在 B 上只显示 B 自己：%+v", board)
	}
	harness.switchToA()
	board := service.ToolRecordBoard()
	if board.Total != 2 || board.Success != 1 || board.Failure != 1 {
		t.Fatalf("看板在 A 上只显示 A 自己：%+v", board)
	}
	if board.LedgerError != "" {
		t.Fatalf("没有失败落盘时不该有告警：%q", board.LedgerError)
	}
}

// ── 用例 10：删除会话 A ⇒ A 的暂存/lastSeq 回收；B 不受影响 ─────────────────

func TestToolRecordReleaseRecyclesStageAndSeq(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, &recordingSnapshotStore{})
	const aID, bID = "sess-del-a", "sess-del-b"

	// A：一笔已入账（占住 lastSeq = 7）+ 一笔还在暂存里。B：一笔已入账。
	service.commitUnitToolRecord(aID, "subagent:node-a", 7, toolRecordStat("bash", 1, 0, ""))
	service.stageUnitToolOutcome(aID, "subagent:node-a", "bash", true, "")
	service.commitUnitToolRecord(bID, "subagent:node-b", 1, toolRecordStat("read_file", 1, 0, ""))

	if err := service.releaseSessionMemory(aID, releaseFull); err != nil {
		t.Fatalf("释放 A：%v", err)
	}
	// A 的台账整格回收：累计、暂存、lastSeq 都不留。
	if service.toolRecords.SessionView(aID).Exists {
		t.Fatal("A 的累计必须随会话一起回收")
	}
	if diagnostics := service.toolRecords.Diagnostics(); diagnostics.UnitCount != 0 {
		t.Fatalf("A 的暂存必须回收：units=%d", diagnostics.UnitCount)
	}
	// 复用同一 ID 的新会话：从 seq=1 重新开始也必须入账（旧 lastSeq 不能把它判成重复）。
	service.commitUnitToolRecord(aID, "subagent:node-a", 1, toolRecordStat("bash", 1, 0, ""))
	if total, _, _ := totals(service, aID); total != 1 {
		t.Fatalf("回收后同 ID 的新会话要能重新记账：%d", total)
	}
	// B 一字未动。
	if total, _, _ := totals(service, bID); total != 1 {
		t.Fatalf("B 不该受影响：%d", total)
	}
}

// ── 用例 11：计数开关关闭 ⇒ 叶子不攒、主不记、不写盘 ────────────────────────

func TestToolRecordDisabledStagesNothing(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	store := &recordingSnapshotStore{}
	service.SetToolRecordSnapshotStore(store)
	if applied, err := service.SetToolRecordEnabled(false); err != nil || applied {
		t.Fatalf("关掉开关：applied=%v err=%v", applied, err)
	}
	sessionID := service.currentViewSessionID()

	service.stageUnitToolOutcome(sessionID, "subagent:node-a", "bash", true, "")
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "role#1", MainSessionID: sessionID, RoleSessionID: "role-impl", Name: "bash",
		Status: dto.ToolEventSuccess.String(),
	})
	service.HandleSubagentToolEventFor(sessionID, subagentReading("bash", dto.ToolEventSuccess, ""))
	service.closeUnitToolRecord(sessionID, "subagent:node-a")
	service.FlushToolRecord(sessionID)

	if units := service.toolRecords.Diagnostics().UnitCount; units != 0 {
		t.Fatalf("关着计数时叶子不攒：%d", units)
	}
	if total, _, _ := totals(service, sessionID); total != 0 {
		t.Fatalf("关着计数时主不记：%d", total)
	}
	if saves := store.savesSnapshot(); len(saves) != 0 {
		t.Fatalf("关着计数时不写盘：%d", len(saves))
	}
}

// ── 用例 12：同一会话内 主代理 + 子代理 + teammate 交替 ⇒ 三类都进同一格 ──────

func TestToolRecordThreeFamiliesMergeInOneSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	sessionID := service.currentViewSessionID()

	// 主代理（命令面退出码判据）→ 成功一笔。
	service.RecordToolCommandOutcome(sessionID, "bash", 0)
	// 子代理 → 失败一笔，原因 permission。
	service.HandleSubagentToolEventFor(sessionID, subagentReading("read_file", dto.ToolEventError, "permission denied"))
	service.closeSubagentToolRecordUnit(sessionID, "node-a")
	// teammate → 失败一笔，同一原因（只归一一次词表，不写第二份判据）。
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "role#1", MainSessionID: sessionID, RoleSessionID: "role-impl", Name: "grep_search",
		Status: dto.ToolEventError.String(), Error: "permission denied",
	})
	service.CloseRoleToolRecordUnit(sessionID, "role-impl")

	total, success, failure := totals(service, sessionID)
	if total != 3 || success != 1 || failure != 2 {
		t.Fatalf("三类读数都要进同一格：total=%d success=%d failure=%d", total, success, failure)
	}
	if units := service.toolRecords.Diagnostics().UnitCount; units != 2 {
		t.Fatalf("两个执行单元各一份暂存（收口后仍在表里、内容已清空）：%d", units)
	}
	for _, stat := range service.toolRecords.SessionView(sessionID).Tools {
		if stat.Failure == 0 {
			continue
		}
		if len(stat.Reasons) != 1 || stat.Reasons[0].Reason != dto.ToolReasonPermission || stat.Reasons[0].Count != 1 {
			t.Fatalf("%s 的失败原因应都归 permission（一份词表）：%+v", stat.Tool, stat.Reasons)
		}
	}
}
