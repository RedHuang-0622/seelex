package sessionstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

func TestSessionContextStoreStackLifecycle(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-stacks")

	if err := store.PushPlan(PlanFrame{PlanID: "plan-1", Title: "inspect", Status: "active", EnteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushTask(TaskFrame{TaskID: "task-1", Objective: "inspect repo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushGoal(GoalFrame{GoalID: "goal-1", Title: "审查 goal 域", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushSkill(SkillFrame{SkillID: "skill-1", Name: "plan"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushCompact(CompactFrame{SegmentID: "seg-1", From: 0, To: 4, Summary: "compressed rounds", CompressedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	record := store.Snapshot()
	if record.SchemaVersion != SessionContextSchemaVersion {
		t.Fatalf("schema version = %d", record.SchemaVersion)
	}
	if len(record.PlanStack) != 1 || len(record.TaskStack) != 1 ||
		len(record.SkillStack) != 1 || len(record.CompactStack) != 1 || len(record.GoalStack) != 1 {
		t.Fatalf("stacks after push = %+v", record)
	}
	if record.GoalStack[0].GoalID != "goal-1" || record.GoalStack[0].Status != "active" {
		t.Fatalf("goal frame = %+v", record.GoalStack[0])
	}
	if record.CompactStack[0].From != 0 || record.CompactStack[0].To != 4 {
		t.Fatalf("compact frame range = %+v", record.CompactStack[0])
	}

	if err := store.CloseTopPlan("plan-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseTopTask("task-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.PopSkill("skill-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseTopGoal("goal-1"); err != nil {
		t.Fatal(err)
	}
	record = store.Snapshot()
	if record.PlanStack[0].Status != "closed" || record.PlanStack[0].ClosedAt == nil {
		t.Fatalf("closed plan frame = %+v", record.PlanStack[0])
	}
	if record.TaskStack[0].Status != "completed" {
		t.Fatalf("closed task frame = %+v", record.TaskStack[0])
	}
	if len(record.SkillStack) != 0 {
		t.Fatalf("skill stack after pop = %+v", record.SkillStack)
	}
	if len(record.GoalStack) != 0 {
		t.Fatalf("goal stack after close = %+v", record.GoalStack)
	}
}

func TestSessionContextStorePersistsAcrossInstances(t *testing.T) {
	router := newTestRouter(t)
	first := NewSessionContextStore(router, "session-persist")
	if err := first.PushPlan(PlanFrame{PlanID: "plan-1", Status: "active", EnteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	second := NewSessionContextStore(router, "session-persist")
	if err := second.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	record := second.Snapshot()
	if len(record.PlanStack) != 1 || record.PlanStack[0].PlanID != "plan-1" {
		t.Fatalf("reloaded record = %+v", record.PlanStack)
	}
}

func TestSessionContextStoreRejectsUnsupportedSchemaVersion(t *testing.T) {
	router := newTestRouter(t)
	if err := router.SaveContextState("session-corrupt", []byte(`{"schema_version":99,"plan_stack":[]}`)); err != nil {
		t.Fatal(err)
	}
	store := NewSessionContextStore(router, "session-corrupt")
	err := store.Load(context.Background())
	if err == nil {
		t.Fatal("unsupported schema version must fail explicitly")
	}
	// 显式失败后不得静默重建内存记录。
	if got := store.Snapshot(); len(got.PlanStack) != 0 {
		t.Fatalf("record after failed load = %+v", got.PlanStack)
	}
}

func TestSessionContextStoreRejectsCorruptState(t *testing.T) {
	router := newTestRouter(t)
	if err := router.SaveContextState("session-garbage", []byte(`not-json`)); err != nil {
		t.Fatal(err)
	}
	store := NewSessionContextStore(router, "session-garbage")
	if err := store.Load(context.Background()); err == nil {
		t.Fatal("corrupt state must fail explicitly")
	}
}

func TestSessionContextStoreValidation(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-validate")
	if err := store.PushPlan(PlanFrame{Status: "active"}); err == nil {
		t.Fatal("plan frame without plan_id must fail")
	}
	if err := store.PushTask(TaskFrame{Objective: "x"}); err == nil {
		t.Fatal("task frame without task_id must fail")
	}
	if err := store.PushSkill(SkillFrame{Name: "x"}); err == nil {
		t.Fatal("skill frame without skill_id must fail")
	}
	if err := store.PushGoal(GoalFrame{Title: "x", Status: "active"}); err == nil {
		t.Fatal("goal frame without goal_id must fail")
	}
	if err := store.PushCompact(CompactFrame{From: 0, To: 1}); err == nil {
		t.Fatal("compact frame without segment_id must fail")
	}
	// 正式定位范围校验（architecture.md §3.3）：半填充/倒置拒绝。
	if err := store.PushCompact(CompactFrame{SegmentID: "seg-x", RoundFrom: 3, RoundTo: 0}); err == nil {
		t.Fatal("half-filled round range must fail")
	}
	if err := store.PushCompact(CompactFrame{SegmentID: "seg-x", RoundFrom: 8, RoundTo: 3}); err == nil {
		t.Fatal("inverted round range must fail")
	}
	if err := store.PushCompact(CompactFrame{SegmentID: "seg-x", EventFrom: 5, EventTo: 0}); err == nil {
		t.Fatal("half-filled event range must fail")
	}
	if err := store.PushCompact(CompactFrame{SegmentID: "seg-x", EventFrom: 50, EventTo: 21}); err == nil {
		t.Fatal("inverted event range must fail")
	}
	if err := store.CloseTopPlan("missing"); err == nil {
		t.Fatal("closing an absent plan must fail")
	}
	if err := store.PopSkill("missing"); err == nil {
		t.Fatal("popping an absent skill must fail")
	}
	if err := store.CloseTopGoal("missing"); err == nil {
		t.Fatal("closing an absent goal must fail")
	}
	if err := store.PushGoal(GoalFrame{GoalID: "goal-1", Title: "A", Status: "active"}); err != nil {
		t.Fatalf("push goal A: %v", err)
	}
	if err := store.PushGoal(GoalFrame{GoalID: "goal-2", Title: "B", Status: "active"}); err != nil {
		t.Fatalf("push goal B: %v", err)
	}
	if err := store.CloseTopGoal("goal-1"); err == nil {
		t.Fatal("closing non-top goal must fail (LIFO)")
	}
	if err := store.CloseTopGoal("goal-2"); err != nil {
		t.Fatalf("close top goal: %v", err)
	}
}

func TestSessionContextStoreSystemPromptInvariant(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-prompt")
	if err := store.SetSystemPrompt("会话级基础提示：永不压缩"); err != nil {
		t.Fatal(err)
	}
	if got := store.SystemPrompt(); got != "会话级基础提示：永不压缩" {
		t.Fatalf("system prompt = %q", got)
	}
	// 新实例 Load 后仍可取回（state blob 持久化）。
	reloaded := NewSessionContextStore(router, "session-prompt")
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.SystemPrompt(); got != "会话级基础提示：永不压缩" {
		t.Fatalf("reloaded system prompt = %q", got)
	}
	// 不变量：SystemPrompt 不写入 ProviderHistory 消息（ChatQueue 只有对话轮次）。
	if err := router.Save("session-prompt", []types.Message{{Role: "user"}}); err != nil {
		t.Fatal(err)
	}
	history, err := router.Load("session-prompt")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range history {
		if message.Role == "system" && message.Content != nil && *message.Content == "会话级基础提示：永不压缩" {
			t.Fatal("system prompt leaked into ProviderHistory")
		}
	}
	// 覆盖设置幂等。
	if err := store.SetSystemPrompt("新提示"); err != nil {
		t.Fatal(err)
	}
	if got := store.SystemPrompt(); got != "新提示" {
		t.Fatalf("updated system prompt = %q", got)
	}
}

func TestSessionContextRecordJSONShape(t *testing.T) {
	record := SessionContextRecord{
		SchemaVersion: SessionContextSchemaVersion,
		SystemPrompt:  "system prompt",
		PlanStack:     []PlanFrame{{PlanID: "plan-1", Status: "active", Nodes: []NodeSummary{{ID: "n1", Status: "pending"}}}},
		GoalStack: []GoalFrame{{
			GoalID: "goal-1", Title: "审查 goal 域", Status: "active",
			Acceptance: []string{"单测全绿"}, Progress: []GoalProgress{{At: 1, Kind: "milestone", Content: "开题"}},
		}},
		GoalAudit: []GoalAuditEntry{{
			Seq: 1, Kind: "goal.begin", GoalID: "goal-1", Title: "审查 goal 域",
			Status: "active", SourceSession: "session-b",
		}},
	}
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SessionContextRecord
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SystemPrompt != "system prompt" || len(decoded.PlanStack) != 1 || decoded.PlanStack[0].Nodes[0].ID != "n1" {
		t.Fatalf("decoded record = %+v", decoded)
	}
	if len(decoded.GoalStack) != 1 || decoded.GoalStack[0].GoalID != "goal-1" ||
		len(decoded.GoalStack[0].Progress) != 1 || decoded.GoalStack[0].Progress[0].Content != "开题" {
		t.Fatalf("decoded goal stack = %+v", decoded.GoalStack)
	}
	if len(decoded.GoalAudit) != 1 || decoded.GoalAudit[0].Seq != 1 ||
		decoded.GoalAudit[0].SourceSession != "session-b" {
		t.Fatalf("decoded goal audit = %+v", decoded.GoalAudit)
	}
}

// TestGoalStackPersistReload 验证 goal 第五栈持久化/恢复：Begin → 落盘 →
// 新 SessionContextStore Load → 栈内容与状态一致（会话恢复重建 goal 治理
// 的数据基础）。
func TestGoalStackPersistReload(t *testing.T) {
	router := newTestRouter(t)
	first := NewSessionContextStore(router, "session-goal-persist")
	frames := []GoalFrame{
		{GoalID: "g-1", Title: "父目标", Status: "paused", EnteredAt: time.Now().Add(-time.Hour)},
		{GoalID: "g-2", Title: "子目标", Status: "active", Acceptance: []string{"go test 全绿"}},
	}
	if err := first.ReplaceGoalStack(frames); err != nil {
		t.Fatalf("replace goal stack: %v", err)
	}

	second := NewSessionContextStore(router, "session-goal-persist")
	if err := second.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := second.GoalStackSnapshot()
	if len(got) != 2 || got[0].GoalID != "g-1" || got[1].GoalID != "g-2" {
		t.Fatalf("reloaded goal stack = %+v", got)
	}
	if got[1].Status != "active" || len(got[1].Acceptance) != 1 || got[1].Acceptance[0] != "go test 全绿" {
		t.Fatalf("reloaded top frame = %+v", got[1])
	}
	record := second.Snapshot()
	if record.SchemaVersion != SessionContextSchemaVersion {
		t.Fatalf("schema version after persist = %d", record.SchemaVersion)
	}
}

// TestGoalStackSessionScopedIsolation 验证 goal 栈按会话隔离：两会话各自
// Begin/Replace 互不串写（恢复/审计不跨会话）。
func TestGoalStackSessionScopedIsolation(t *testing.T) {
	router := newTestRouter(t)
	storeA := NewSessionContextStore(router, "session-goal-a")
	storeB := NewSessionContextStore(router, "session-goal-b")
	if err := storeA.PushGoal(GoalFrame{GoalID: "g-a-1", Title: "A 目标", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := storeB.PushGoal(GoalFrame{GoalID: "g-b-1", Title: "B 目标", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if got := storeB.GoalStackSnapshot(); len(got) != 1 || got[0].GoalID != "g-b-1" {
		t.Fatalf("storeB 不应看到 A 的 goal: %+v", got)
	}
	reloadA := NewSessionContextStore(router, "session-goal-a")
	if err := reloadA.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloadA.GoalStackSnapshot(); len(got) != 1 || got[0].GoalID != "g-a-1" {
		t.Fatalf("reloadA goal stack = %+v", got)
	}
}

// TestGoalStackLiveStackStartsAndEndsEmpty 固化第五栈"活栈投影"语义：
// 持久化 goal 栈初始为空；push 即写入；CloseTopGoal 弹栈即同步删除；
// 全部弹栈后存储恢复为空（重载后也为空，治理收口无残留）。
func TestGoalStackLiveStackStartsAndEndsEmpty(t *testing.T) {
	router := newTestRouter(t)
	sessionID := "session-goal-live"
	store := NewSessionContextStore(router, sessionID)
	if got := store.GoalStackSnapshot(); len(got) != 0 {
		t.Fatalf("goal 栈初始应为空: %+v", got)
	}
	if err := store.PushGoal(GoalFrame{GoalID: "g-1", Title: "父目标", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushGoal(GoalFrame{GoalID: "g-2", Title: "子目标", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if got := store.GoalStackSnapshot(); len(got) != 2 {
		t.Fatalf("压栈后应为活栈 2 帧: %+v", got)
	}
	if err := store.CloseTopGoal("g-2"); err != nil {
		t.Fatal(err)
	}
	if got := store.GoalStackSnapshot(); len(got) != 1 || got[0].GoalID != "g-1" {
		t.Fatalf("弹栈应同步删除 g-2，只剩活栈 g-1: %+v", got)
	}
	if err := store.CloseTopGoal("g-1"); err != nil {
		t.Fatal(err)
	}
	if got := store.GoalStackSnapshot(); len(got) != 0 {
		t.Fatalf("全部弹栈后存储应清空: %+v", got)
	}
	reloaded := NewSessionContextStore(router, sessionID)
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GoalStackSnapshot(); len(got) != 0 {
		t.Fatalf("重载后 goal 栈应为空（终态即什么都不存）: %+v", got)
	}
}

// TestGoalStackSchemaV1MigratesOnLoadAndPersist 验证 schema v1 → v2 迁移：
// v1 记录（无 GoalStack）可加载，内存版本升 v2；任何栈变更后落盘为 v2。
func TestGoalStackSchemaV1MigratesOnLoadAndPersist(t *testing.T) {
	router := newTestRouter(t)
	if err := router.SaveContextState("session-goal-v1", []byte(
		`{"schema_version":1,"system_prompt":"旧","plan_stack":[],"task_stack":[],"skill_stack":[],"compact_stack":[]}`,
	)); err != nil {
		t.Fatal(err)
	}
	store := NewSessionContextStore(router, "session-goal-v1")
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("v1 load 应迁移成功: %v", err)
	}
	record := store.Snapshot()
	if record.SchemaVersion != SessionContextSchemaVersion {
		t.Fatalf("迁移后内存版本 = %d", record.SchemaVersion)
	}
	if len(record.GoalStack) != 0 {
		t.Fatalf("v1 无 goal 栈，迁移后应为空: %+v", record.GoalStack)
	}
	// 首次栈变更触发 Persist，文件应升 v2。
	if err := store.PushGoal(GoalFrame{GoalID: "g-1", Title: "迁移后目标", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	raw, err := router.LoadContextState("session-goal-v1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schema_version":2`) {
		t.Fatalf("落盘 schema 未升 v2: %s", raw)
	}
}

// TestGoalAuditAppendOnlySessionScoped 验证 goal 审计账本按会话隔离且只
// 追加不回改：两会话 Seq 各自从 1 单调；重载后已追加条目原样保留；缺
// kind/goal_id 拒绝。
func TestGoalAuditAppendOnlySessionScoped(t *testing.T) {
	router := newTestRouter(t)
	storeA := NewSessionContextStore(router, "session-goal-audit-a")
	storeB := NewSessionContextStore(router, "session-goal-audit-b")
	entry := func(kind, goalID, title string) GoalAuditEntry {
		return GoalAuditEntry{Kind: kind, GoalID: goalID, Title: title}
	}
	if err := storeA.AppendGoalAudit(entry("goal.begin", "g-1", "A 目标")); err != nil {
		t.Fatal(err)
	}
	first := storeA.GoalAuditSnapshot()
	if err := storeA.AppendGoalAudit(entry("goal.finish", "g-1", "A 目标")); err != nil {
		t.Fatal(err)
	}
	after := storeA.GoalAuditSnapshot()
	if len(after) != 2 || after[0] != first[0] {
		t.Fatalf("账本应只追加不回改: first=%+v after=%+v", first, after)
	}
	if after[1].Seq != 2 {
		t.Fatalf("追加条目 Seq 应续 2: %+v", after[1])
	}
	reloaded := NewSessionContextStore(router, "session-goal-audit-a")
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GoalAuditSnapshot(); len(got) != 2 || got[0] != after[0] || got[1] != after[1] {
		t.Fatalf("重载后审计应原样保留: %+v", got)
	}
	if err := storeB.AppendGoalAudit(entry("goal.begin", "g-1", "B 目标")); err != nil {
		t.Fatal(err)
	}
	if got := storeB.GoalAuditSnapshot(); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("B 会话审计应独立从 Seq=1 开始: %+v", got)
	}
	if err := storeA.AppendGoalAudit(GoalAuditEntry{GoalID: "g-2"}); err == nil {
		t.Fatal("缺少 kind 的审计条目应拒绝")
	}
	if err := storeA.AppendGoalAudit(GoalAuditEntry{Kind: "goal.begin"}); err == nil {
		t.Fatal("缺少 goal_id 的审计条目应拒绝")
	}
}

// TestCompactFrameRangeFieldsPersist 验证 CompactFrame 正式定位字段
// （round/event/message 范围 + revision）持久化往返完整。
func TestCompactFrameRangeFieldsPersist(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-compact-range")
	frame := CompactFrame{
		SegmentID: "compact-r00000003-r00000008-v0002",
		From:      0, To: 7,
		RoundFrom: 3, RoundTo: 8,
		EventFrom: 21, EventTo: 49,
		MessageFrom: "message-31", MessageTo: "message-76",
		EventRevision:        Revision{CommitID: "commit-12", Number: 12},
		ConversationRevision: Revision{CommitID: "commit-12", Number: 12},
		Summary:              "先期摘要",
		CompressedAt:         time.Now(),
	}
	if err := store.PushCompact(frame); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSessionContextStore(router, "session-compact-range")
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	record := reloaded.Snapshot()
	if len(record.CompactStack) != 1 {
		t.Fatalf("compact stack = %+v", record.CompactStack)
	}
	got := record.CompactStack[0]
	if got.RoundFrom != 3 || got.RoundTo != 8 || got.EventFrom != 21 || got.EventTo != 49 {
		t.Fatalf("range fields = %+v", got)
	}
	if got.MessageFrom != "message-31" || got.MessageTo != "message-76" {
		t.Fatalf("message range = %q..%q", got.MessageFrom, got.MessageTo)
	}
	if got.EventRevision.Number != 12 || got.ConversationRevision.Number != 12 || got.EventRevision.CommitID != "commit-12" {
		t.Fatalf("revisions = %+v", got)
	}
	// 兼容字段保留（审计 R6：ChatQueue 单元索引）。
	if got.From != 0 || got.To != 7 {
		t.Fatalf("legacy unit range = [%d,%d]", got.From, got.To)
	}
}

// TestCompactFrameIndexAndChainFieldsPersist 验证 2026-09-06 压缩 DAG 详设
// §3.1 的超上下文索引与链锚点字段持久化往返完整（旧记录无新字段可读）。
func TestCompactFrameIndexAndChainFieldsPersist(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-compact-index")
	first := CompactFrame{
		SegmentID: "compact-sess-1", From: 0, To: 6,
		RequestFrom: "chat-1", RequestTo: "chat-7",
		Summary:      "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n迁移",
		CompressedAt: time.Now(),
	}
	if err := store.PushCompact(first); err != nil {
		t.Fatal(err)
	}
	second := CompactFrame{
		SegmentID: "compact-sess-2", From: 0, To: 8,
		RequestFrom: "chat-1", RequestTo: "chat-9",
		PrevSegmentID:      first.SegmentID,
		PrevRequestFrom:    first.RequestFrom,
		PrevRequestTo:      first.RequestTo,
		PrevSummaryOneLine: "完成了模块 X 的迁移与验收",
		SummarySource:      "local",
		AnchorSource:       "ok",
		Summary:            "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n继续",
		CompressedAt:       time.Now(),
	}
	if err := store.PushCompact(second); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSessionContextStore(router, "session-compact-index")
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := reloaded.Snapshot().CompactStack
	if len(got) != 2 {
		t.Fatalf("compact stack = %d frames, want 2", len(got))
	}
	if got[0].RequestFrom != "chat-1" || got[0].RequestTo != "chat-7" {
		t.Fatalf("first frame request index = %q..%q", got[0].RequestFrom, got[0].RequestTo)
	}
	if got[1].PrevSegmentID != "compact-sess-1" || got[1].PrevSummaryOneLine != "完成了模块 X 的迁移与验收" {
		t.Fatalf("second frame chain anchor = %+v", got[1])
	}
	if got[1].SummarySource != "local" || got[1].AnchorSource != "ok" {
		t.Fatalf("second frame quality markers = %+v", got[1])
	}
	// 旧记录（无新字段）仍可解码：直接写入缺字段的 state blob。
	legacyPayload := `{"schema_version":1,"compact_stack":[{"segment_id":"legacy-1","from":0,"to":3,"summary":"旧摘要"}]}`
	if err := router.SaveContextState("session-legacy", []byte(legacyPayload)); err != nil {
		t.Fatal(err)
	}
	legacy := NewSessionContextStore(router, "session-legacy")
	if err := legacy.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	legacyFrames := legacy.Snapshot().CompactStack
	if len(legacyFrames) != 1 || legacyFrames[0].Summary != "旧摘要" || legacyFrames[0].RequestFrom != "" {
		t.Fatalf("legacy frame = %+v", legacyFrames)
	}
}

// TestPushCompactIndexAndChainValidation 验证详设 §3.4 追加校验：
// request 首尾同空/同非空 + 非倒置；PrevSegmentID 首帧为空、非首帧与栈顶
// 一致；PrevRequestFrom/To 与栈顶 request 索引一致。
func TestPushCompactIndexAndChainValidation(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionContextStore(router, "session-compact-validate")
	base := func() CompactFrame {
		return CompactFrame{SegmentID: "seg-x", From: 0, To: 1, Summary: "s", CompressedAt: time.Now()}
	}
	// request 索引半填充拒绝。
	if err := store.PushCompact(func() CompactFrame {
		frame := base()
		frame.RequestFrom = "chat-1"
		return frame
	}()); err == nil {
		t.Fatal("half-filled request range must fail")
	}
	// request 索引倒置拒绝。
	if err := store.PushCompact(func() CompactFrame {
		frame := base()
		frame.RequestFrom, frame.RequestTo = "chat-9", "chat-1"
		return frame
	}()); err == nil {
		t.Fatal("inverted request range must fail")
	}
	// 首帧携带 prev_segment_id 拒绝。
	if err := store.PushCompact(func() CompactFrame {
		frame := base()
		frame.PrevSegmentID = "ghost"
		return frame
	}()); err == nil {
		t.Fatal("first frame with prev_segment_id must fail")
	}
	// 合法首帧（带 request 索引与质量标记）。
	first := base()
	first.SegmentID = "compact-validate-1"
	first.RequestFrom, first.RequestTo = "chat-1", "chat-2"
	if err := store.PushCompact(first); err != nil {
		t.Fatal(err)
	}
	// 非首帧缺 prev_segment_id 拒绝。
	if err := store.PushCompact(base()); err == nil {
		t.Fatal("non-first frame without prev_segment_id must fail")
	}
	// prev_segment_id 与栈顶不一致拒绝。
	if err := store.PushCompact(func() CompactFrame {
		frame := base()
		frame.PrevSegmentID = "compact-validate-wrong"
		return frame
	}()); err == nil {
		t.Fatal("prev_segment_id mismatch must fail")
	}
	// prev request 范围与栈顶不一致拒绝。
	if err := store.PushCompact(func() CompactFrame {
		frame := base()
		frame.PrevSegmentID = "compact-validate-1"
		frame.PrevRequestFrom, frame.PrevRequestTo = "chat-5", "chat-6"
		return frame
	}()); err == nil {
		t.Fatal("prev request range mismatch must fail")
	}
	// 合法非首帧。
	second := base()
	second.SegmentID = "compact-validate-2"
	second.PrevSegmentID = "compact-validate-1"
	second.PrevRequestFrom, second.PrevRequestTo = "chat-1", "chat-2"
	if err := store.PushCompact(second); err != nil {
		t.Fatal(err)
	}
}
