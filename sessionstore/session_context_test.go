package sessionstore

import (
	"context"
	"encoding/json"
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
	if len(record.PlanStack) != 1 || len(record.TaskStack) != 1 || len(record.SkillStack) != 1 || len(record.CompactStack) != 1 {
		t.Fatalf("stacks after push = %+v", record)
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
