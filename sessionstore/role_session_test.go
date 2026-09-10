package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func roleSessionFixture(t *testing.T) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "project-1", SessionID: "main-session"}
	if _, err := store.messageCommit(key, "init", []Event{{
		Role: "user", Content: "start", Kind: EventKindUserInput,
	}}); err != nil {
		t.Fatal(err)
	}
	return store, key
}

func TestRoleSessionCreateBackupAndLifecycle(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	childStore, childKey, err := store.createRoleSession(mainKey, RoleTL, "tl-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	roleRoot := store.roleSessionRoot(mainKey, RoleTL, "tl-1")
	if _, err := os.Stat(filepath.Join(childStore.sessionRoot(childKey), "metadata")); err != nil {
		t.Fatalf("role session metadata missing: %v", err)
	}
	if roleRoot == store.sessionRoot(mainKey) {
		t.Fatal("role session must be physically isolated from main session root")
	}
	head, err := childStore.readLifecycleHead(childKey)
	if err != nil {
		t.Fatal(err)
	}
	if head.JoinSeqID != 1 {
		t.Fatalf("join_seq_id = %d, want 1", head.JoinSeqID)
	}
	if err := store.appendRoleSessionRows(mainKey, RoleTL, "tl-1", []Event{{
		Role: "assistant", Content: "TL 备份", Kind: EventKindLLM,
	}}); err != nil {
		t.Fatal(err)
	}
	// 主会话 message 删除后，TL 备份行仍可读。
	if err := store.deleteModule(mainKey, moduleMessage); err != nil {
		t.Fatal(err)
	}
	rows, err := store.readRoleSessionRows(mainKey, RoleTL, "tl-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Content != "TL 备份" {
		t.Fatalf("TL backup rows = %+v", rows)
	}
}

func TestGoalStackRecordsRoleSessionAnchor(t *testing.T) {
	store, key := roleSessionFixture(t)
	payload := json.RawMessage(`{"goal_id":"g1","role_name":"tl","role_session_id":"tl-1"}`)
	_, err := stackCommit(store.stackJournal(), key, StackKindGoal,
		stackPushMessage(StackKindGoal, "goal:g1", []StackItemInput{{
			ItemID: "g1", Kind: StackKindGoal, Status: "active", Payload: payload,
			RoleName: "tl", RoleSessionID: "tl-1",
		}}))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := stackReadActive(store.stackJournal(), key, StackKindGoal)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("goal active rows = %d", len(rows))
	}
	if rows[0].RoleName != "tl" || rows[0].RoleSessionID != "tl-1" {
		t.Fatalf("role anchor = %q/%q, want tl/tl-1", rows[0].RoleName, rows[0].RoleSessionID)
	}
	headData, err := os.ReadFile(store.modulePath(key, moduleStackGoal))
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(string(headData), `"role_name"`) {
		t.Fatalf("goal stack head must only carry watermark, got: %s", headData)
	}
}

func TestRoleDraftSyncOrderFloorAndIdempotency(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	if _, _, err := store.createRoleSession(mainKey, RoleTL, "tl-1", 1); err != nil {
		t.Fatal(err)
	}
	rows := []RoleDraftRow{
		{
			RoundID: 1, RoleName: "tl", RoleSessionID: "tl-1", UnitSeq: 2,
			MessageID: "m2", Event: Event{Role: "assistant", Content: "second", Kind: EventKindLLM},
		},
		{
			RoundID: 1, RoleName: "tl", RoleSessionID: "tl-1", UnitSeq: 1,
			MessageID: "m1", Event: Event{Role: "assistant", Content: "first", Kind: EventKindLLM},
		},
	}
	if err := store.syncRoleDraftAndEnsureNoRows(mainKey, "tl", "tl-1", []string{"user", "main", "tl"}, rows); err != nil {
		t.Fatal(err)
	}
	result, err := store.syncRoleDraft(mainKey, "tl", "tl-1", []string{"user", "main", "tl"})
	if err != nil {
		t.Fatal(err)
	}
	if result.AlreadySynced {
		t.Fatal("first sync should not report already synced")
	}
	if result.SyncedRows != 2 {
		t.Fatalf("synced rows = %d, want 2", result.SyncedRows)
	}
	messageRows, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	// 初始 1 行 + 同步 2 行。
	if len(messageRows) != 3 {
		t.Fatalf("main rows = %d, want 3: %+v", len(messageRows), messageRows)
	}
	if messageRows[1].Content != "first" || messageRows[2].Content != "second" {
		t.Fatalf("draft order not preserved: %+v", messageRows[1:])
	}
	for _, row := range messageRows[1:] {
		if row.RoleName != "tl" || row.RoleSessionID != "tl-1" || row.RoundID != 1 {
			t.Fatalf("role fields missing on message row: %+v", row)
		}
	}
	head, err := store.readMessageHead(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	if head.Floor == nil || head.Floor.RoleName != "tl" {
		t.Fatalf("floor = %+v, want tl", head.Floor)
	}
	// 模拟半同步：head 已发布但 draft 残留；同一批 draft 重放不得重复 append。
	if err := store.appendRoleDraftForTest(mainKey, "tl", "tl-1", rows); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.syncRoleDraft(mainKey, "tl", "tl-1", []string{"user", "main", "tl"})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.AlreadySynced {
		t.Fatal("replayed draft should be detected as already synced")
	}
	after, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 {
		t.Fatalf("replay duplicated rows: got %d want 3", len(after))
	}

	// 未同步 draft 只进该角色自己的 wire，不进主文档。
	pending := []RoleDraftRow{{
		RoundID: 2, RoleName: "tl", RoleSessionID: "tl-1", UnitSeq: 1,
		MessageID: "m3", Event: Event{Role: "assistant", Content: "pending", Kind: EventKindLLM},
	}}
	if err := store.appendRoleDraftForTest(mainKey, "tl", "tl-1", pending); err != nil {
		t.Fatal(err)
	}
	beforeWire, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := store.assembleRoleWire(mainKey, "tl", "tl-1", 200_000, 3)
	if err != nil {
		t.Fatal(err)
	}
	if wire.PendingRows != 1 || len(wire.Messages) != 3 {
		t.Fatalf("role wire pending = %d messages=%d, want 1/3", wire.PendingRows, len(wire.Messages))
	}
	if got := wire.Messages[len(wire.Messages)-1].Content; got != "pending" {
		t.Fatalf("role wire last content = %q, want pending", got)
	}
	afterWire, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterWire) != len(beforeWire) {
		t.Fatalf("assembleRoleWire wrote pending draft into main message: before=%d after=%d", len(beforeWire), len(afterWire))
	}

	// compact 已覆盖角色可见区间时，缺 compact_ref 必须显式报错，不伪造帧。
	if _, err := store.compactCommit(mainKey, compactFrameRecord{
		FrameID: "frame-1", MessageFrom: "m1", MessageTo: "m2",
		MessageFromSeq: 1, MessageToSeq: 3, Summary: "summary", BoundaryStatus: "complete",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.assembleRoleWire(mainKey, "tl", "tl-1", 200_000, 3); err == nil {
		t.Fatal("missing compact_ref should fail explicitly")
	}
	// 写入引用后角色 wire 使用 main 帧，不生成第二份帧。
	roleStore, roleKey := store.roleStore(mainKey, "tl", "tl-1")
	if err := roleStore.setRoleLifecycle(roleKey, 1, &CompactRef{FrameID: "frame-1", AppliedSeq: 3}); err != nil {
		t.Fatal(err)
	}
	framed, err := store.assembleRoleWire(mainKey, "tl", "tl-1", 200_000, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !framed.FrameApplied || len(framed.Messages) == 0 || framed.Messages[0].Content != "summary" {
		t.Fatalf("role wire did not apply main compact frame: %+v", framed)
	}
	if _, err := store.assembleRoleWire(mainKey, RoleUser, "user", 200_000, 3); err == nil {
		t.Fatal("user draft must not enter role wire")
	}
}

func (store *storeEngine) syncRoleDraftAndEnsureNoRows(mainKey Key, roleName, roleSessionID string, order []string, rows []RoleDraftRow) error {
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
	if err := appendRoleDraft(roleStore, roleKey, roleName, rows); err != nil {
		return err
	}
	if got, err := readRoleDraft(roleStore, roleKey, roleName); err != nil || len(got) != 2 {
		return err
	}
	return nil
}

func (store *storeEngine) appendRoleDraftForTest(mainKey Key, roleName, roleSessionID string, rows []RoleDraftRow) error {
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
	return appendRoleDraft(roleStore, roleKey, roleName, rows)
}

func TestLifecycleOrderAndRoleRefSurviveDraftCommit(t *testing.T) {
	store, key := roleSessionFixture(t)
	if err := store.setLifecycleOrder(key, "user_main_decided", []string{"user", "main", "tl"}); err != nil {
		t.Fatal(err)
	}
	if err := store.setRoleLifecycle(key, 7, &CompactRef{FrameID: "frame-1", AppliedSeq: 6}); err != nil {
		t.Fatal(err)
	}
	if err := store.saveDraft(key, "草稿内容"); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.OrderPolicy != "user_main_decided" || len(head.OrderRoles) != 3 {
		t.Fatalf("order fields lost: %+v", head)
	}
	if head.JoinSeqID != 7 || head.CompactRef == nil || head.CompactRef.FrameID != "frame-1" {
		t.Fatalf("role lifecycle fields lost: %+v", head)
	}
}

func TestMaterialCacheInvalidation(t *testing.T) {
	cache := NewMaterialCache(4)
	key := MaterialCacheKey{RoleSessionID: "main", RoleName: "main", LastCommitID: "c1"}
	value := MaterialCacheValue{Rows: []Event{{Role: "user", Content: "hello"}}, PrefixDigest: "p1"}
	cache.Put(key, value)
	if got, ok := cache.Get(key); !ok || got.PrefixDigest != "p1" || len(got.Rows) != 1 {
		t.Fatalf("cache get = %+v ok=%v", got, ok)
	}
	cache.InvalidateRole("main", "main")
	if _, ok := cache.Get(key); ok {
		t.Fatal("cache should be invalidated after role invalidation")
	}
}

func TestScheduleEventsAreRecorded(t *testing.T) {
	store, key := roleSessionFixture(t)
	payload := ScheduleEventPayload{
		ScheduleID: "sched-1", RoleName: "tl", RoleSessionID: "tl-1",
		Interval: "10m",
	}
	if err := store.scheduleEventCommit(key, structuralEventScheduleRegistered, payload); err != nil {
		t.Fatal(err)
	}
	events, err := store.readEvents(key, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != structuralEventScheduleRegistered {
		t.Fatalf("schedule events = %+v", events)
	}
	var decoded ScheduleEventPayload
	if err := json.Unmarshal(events[0].Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ScheduleID != "sched-1" || decoded.RoleName != "tl" {
		t.Fatalf("decoded schedule payload = %+v", decoded)
	}
}
