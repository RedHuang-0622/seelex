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
	if err := router.SaveState("session-corrupt", []byte(`{"schema_version":99,"plan_stack":[]}`)); err != nil {
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
	if err := router.SaveState("session-garbage", []byte(`not-json`)); err != nil {
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
