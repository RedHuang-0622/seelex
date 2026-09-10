package sessionstore

import (
	"os"
	"strings"
	"testing"
)

// TestGuideNotRewrittenAfterModuleHeadWrites 对应 T-DP-04（§2.0 规则 3、
// D9、S14）：guide.json 只做布局/版本判定，不登记模块地址；首写各模块
// head 时 guide 不得被重写（写次数 = 0）。
func TestGuideNotRewrittenAfterModuleHeadWrites(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p-dp04", SessionID: "s-dp04"}

	// 会话创建 = 布局创建，允许写一次 guide（空 message head 发布）。
	if _, err := store.messageCommit(key, "c-init", []Event{
		{Role: "user", Content: "first", MessageID: "m-1"},
	}); err != nil {
		t.Fatal(err)
	}
	guidePath := store.guidePath(key)
	before, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "module_index") {
		t.Fatalf("guide.json 仍带 module_index（D9/S14 已删除）: %s", before)
	}

	// 后续各模块首写：message 追加 + 各模块 head 发布，均不得触碰 guide。
	if _, err := store.messageCommit(key, "c-m2", []Event{
		{Role: "assistant", Content: "second", MessageID: "m-2"},
	}); err != nil {
		t.Fatal(err)
	}
	for module, payload := range map[storageModule]any{
		moduleEvent:     eventHeadRecord{SessionID: key.SessionID, LastID: 1},
		moduleCompact:   compactHeadRecord{SessionID: key.SessionID, FrameCount: 1},
		moduleStackPlan: stackModuleHead{SessionID: key.SessionID, Kinds: map[StackKind]stackWatermark{}},
		moduleStackTask: stackModuleHead{SessionID: key.SessionID, Kinds: map[StackKind]stackWatermark{}},
		moduleStackGoal: stackModuleHead{SessionID: key.SessionID, Kinds: map[StackKind]stackWatermark{}},
		moduleLifecycle: lifecycleHead{SessionID: key.SessionID},
		moduleRetention: retentionHead{SessionID: key.SessionID, Mode: retentionModeManual},
		moduleSubagent:  stackModuleHead{SessionID: key.SessionID, Kinds: map[StackKind]stackWatermark{}},
	} {
		if err := store.commitModuleHead(key, module, "c-"+string(module), payload); err != nil {
			t.Fatalf("commit %s head: %v", module, err)
		}
	}

	after, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("guide.json 被模块写入重写（T-DP-04）:\nbefore=%s\nafter=%s", before, after)
	}
}
