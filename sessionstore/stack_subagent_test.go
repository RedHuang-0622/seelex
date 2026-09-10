package sessionstore

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestStackSubagentBatchSemantics 对应 T-STK-12（§2.4/§8.2、D6、S18）：
// subagent 是第四批栈——一次派发 N 个 = 同一批 N 条目；单个完成只更新、整批
// 完成才整批归档进 subagent/history.jsonl；head 只留水位（metadata/subagent.json
// 不再装条目清单）。
func TestStackSubagentBatchSemantics(t *testing.T) {
	store, key := lifecycleFixture(t)
	if _, err := store.messageCommit(key, "c1", []Event{
		{Role: "user", Content: "go", MessageID: "m-1"},
	}); err != nil {
		t.Fatal(err)
	}
	infoA := subagentInfo{SubagentID: "sa-1", Path: "subagent_x1", Status: "running", CreatedAt: time.Now().UTC()}
	infoB := subagentInfo{SubagentID: "sa-2", Path: "subagent_x2", Status: "running", CreatedAt: time.Now().UTC()}
	for _, info := range []subagentInfo{infoA, infoB} {
		if err := store.registerSubagent(key, "dispatch-x", info); err != nil {
			t.Fatal(err)
		}
	}

	activePath := store.stackActivePath(key, StackKindSubagent)
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if rows := strings.Count(string(data), "\n"); rows != 2 {
		t.Fatalf("subagent active rows = %d, want 2（一次派发 N 个 = 同一批 N 条目）", rows)
	}
	headData, err := os.ReadFile(store.modulePath(key, moduleSubagent))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"subagent_id"`, `"sa-1"`, `"dispatch:"`} {
		if strings.Contains(string(headData), forbidden) {
			t.Fatalf("subagent head 只装水位，发现 %s in %s", forbidden, headData)
		}
	}
	items, err := store.readSubagents(key)
	if err != nil || len(items) != 2 {
		t.Fatalf("readSubagents = %+v err=%v, want 2 running", items, err)
	}

	// 单个完成只更新条目，整批未完成不得归档。
	if err := store.updateSubagentStatus(key, "sa-1", "archived"); err != nil {
		t.Fatal(err)
	}
	if active := mustActiveSubagents(t, store, key); len(active) != 2 {
		t.Fatalf("部分完成必须整批留 active: %+v", active)
	}
	if archived := mustHistorySubagents(t, store, key); len(archived) != 0 {
		t.Fatalf("部分完成不得归档: %+v", archived)
	}

	// 整批完成 → 整批弹栈归档。
	if err := store.updateSubagentStatus(key, "sa-2", "archived"); err != nil {
		t.Fatal(err)
	}
	if active := mustActiveSubagents(t, store, key); len(active) != 0 {
		t.Fatalf("整批完成后 active 应为空: %+v", active)
	}
	if archived := mustHistorySubagents(t, store, key); len(archived) != 2 {
		t.Fatalf("整批完成后 history = %d, want 2", len(archived))
	}
	history, err := os.ReadFile(store.stackHistoryPath(key, StackKindSubagent))
	if err != nil || strings.Count(string(history), "\n") != 2 {
		t.Fatalf("history.jsonl rows = %d err=%v, want 2", strings.Count(string(history), "\n"), err)
	}
	if err := stackVerify(store.stackJournal(), key); err != nil {
		t.Fatalf("verify after subagent batch archive: %v", err)
	}
}

func mustActiveSubagents(t *testing.T, store *storeEngine, key Key) []StackItemRecord {
	t.Helper()
	rows, err := stackReadActive(store.stackJournal(), key, StackKindSubagent)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func mustHistorySubagents(t *testing.T, store *storeEngine, key Key) []StackItemRecord {
	t.Helper()
	rows, err := stackReadHistory(store.stackJournal(), key, StackKindSubagent)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
