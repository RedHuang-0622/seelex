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

// TestRoleSnapshotProjectsPendingDraftUntilSync 钉住「未同步草稿」的整条生命周期：
//
//	本轮进行中 / 上一次同步失败 → 行只存在于 draft/<role>.jsonl，快照以 draft_rows
//	                              单列出来；草稿**没有** message seq（seq 由 sync 分配），
//	                              它的身份是 round_id / role_name / unit_seq / kind。
//	本轮结束（完成或取消收口）   → sequencer sync 原子发布 head+floor 并把草稿文件删掉，
//	                              快照的 draft_rows 清空、同样的行变成 message 行。
//
// 前端 gui/frontend/dist/agent-team-view.js 的「未同步草稿」区就是照着这份投影
// 分开渲染的：它拿不到也不推演"同步后的样子"，草稿区消失只由 draft_rows 变空驱动。
func TestRoleSnapshotProjectsPendingDraftUntilSync(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	if _, _, err := store.createRoleSession(mainKey, RoleTL, "tl-1", 1); err != nil {
		t.Fatal(err)
	}
	// 形状与 application/core/goal_team_recorder.go:RecordTLRound 一致：一次 append
	// 落上下文（system）+ 裁决（assistant）两行，unit_seq 就是它们在同一回合里的顺序。
	pending := []RoleDraftRow{
		{
			RoundID: 3, RoleName: RoleTL, RoleSessionID: "tl-1", UnitSeq: 1,
			Event: Event{Role: "system", Content: "本轮送给 ADVISOR 的原文", Kind: "role_context"},
		},
		{
			RoundID: 3, RoleName: RoleTL, RoleSessionID: "tl-1", UnitSeq: 2,
			Event: Event{Role: "assistant", Content: "裁决原文", Kind: "tl_directive"},
		},
	}
	if err := store.appendRoleDraftForTest(mainKey, RoleTL, "tl-1", pending); err != nil {
		t.Fatal(err)
	}
	before, err := store.readRoleSnapshot(mainKey, RoleTL, "tl-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(before.DraftRows) != 2 {
		t.Fatalf("同步前 draft_rows = %d，want 2（未同步草稿必须是独立投影，前端才能区分）", len(before.DraftRows))
	}
	if len(before.RoleRows) != 0 {
		t.Fatalf("草稿不是已发布行：role_rows = %d，want 0", len(before.RoleRows))
	}
	for _, row := range before.DraftRows {
		if row.Event.Seq != 0 {
			t.Fatalf("草稿不该带 message seq（seq 由 sync 分配）：%+v", row)
		}
		if row.RoleName != RoleTL || row.RoleSessionID != "tl-1" || row.UnitSeq == 0 || row.RoundID == 0 {
			t.Fatalf("草稿必须带 sequencer 的排序凭据（round/role/unit）：%+v", row)
		}
	}
	if len(before.MainRows) != 1 {
		t.Fatalf("草稿不得进主文档：main_rows = %d，want 1（只有 fixture 的 start）", len(before.MainRows))
	}

	// 本轮结束：sync 发布 → 草稿文件被删（收敛），快照里的草稿消失、同样的行成为
	// message 行（这一步没有"丢内容"的分支：只有发布成功后才删草稿）。
	result, err := store.syncRoleDraft(mainKey, RoleTL, "tl-1", []string{"user", "main", "tl"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncedRows != 2 || result.AlreadySynced {
		t.Fatalf("首次 sync 结果 = %+v，want 2 行 / 非重放", result)
	}
	after, err := store.readRoleSnapshot(mainKey, RoleTL, "tl-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.DraftRows) != 0 {
		t.Fatalf("同步后仍有未同步草稿（悬空 draft）：%+v", after.DraftRows)
	}
	if len(after.MainRows) != 3 {
		t.Fatalf("同步后 main_rows = %d，want 3（start + 本轮两行）", len(after.MainRows))
	}
	published := after.MainRows[1:]
	for index, row := range published {
		wantContent := pending[index].Event.Content
		if row.Content != wantContent || row.RoleName != RoleTL || row.RoleSessionID != "tl-1" || row.RoundID != 3 {
			t.Fatalf("发布行 %d 错位：%+v，want 内容 %q / tl / tl-1 / round 3", index, row, wantContent)
		}
		if row.Seq == 0 {
			t.Fatalf("发布行必须拿到 seq（草稿被「吞掉」的症状就是发布行仍无 seq）：%+v", row)
		}
	}

	// 半同步窗口（head 已发布、draft 残留）重放：只清理、不重复 append，快照依旧无草稿。
	if err := store.appendRoleDraftForTest(mainKey, RoleTL, "tl-1", pending); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.syncRoleDraft(mainKey, RoleTL, "tl-1", []string{"user", "main", "tl"})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.AlreadySynced {
		t.Fatalf("重放必须被识别为已同步（否则主文档出现重复行）：%+v", replayed)
	}
	replayedSnapshot, err := store.readRoleSnapshot(mainKey, RoleTL, "tl-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(replayedSnapshot.DraftRows) != 0 || len(replayedSnapshot.MainRows) != 3 {
		t.Fatalf("重放后草稿没清空或主文档被重复写入：draft=%d main=%d",
			len(replayedSnapshot.DraftRows), len(replayedSnapshot.MainRows))
	}
}

// TestRoleSnapshotMarksRowsOutsidePrefixMatch 钉住「teammate 自己那份 team work
// 记录的起点」：装配时写入的 join_seq_id 就是它的前缀匹配切点，主会话在该切点
// 之前的行不属于它的记录（面板以占位呈现）；main 复用主会话本身，切点恒为 0。
func TestRoleSnapshotMarksRowsOutsidePrefixMatch(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	if _, _, err := store.createRoleSession(mainKey, RoleTL, "tl-1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.messageCommit(mainKey, "turn-2", []Event{
		{Role: "assistant", Content: "goal 这一回合", Kind: EventKindLLM},
		{Role: "assistant", Content: "exec 这一回合", Kind: EventKindLLM},
	}); err != nil {
		t.Fatal(err)
	}

	role, err := store.readRoleSnapshot(mainKey, RoleTL, "tl-1")
	if err != nil {
		t.Fatal(err)
	}
	if role.PrefixCutSeq != 1 {
		t.Fatalf("tl 记录起点 = %d，want 1（装配时的 join_seq_id）", role.PrefixCutSeq)
	}
	if role.OutsidePrefixMainRows != 1 || role.VisibleMainRows != 2 {
		t.Fatalf("tl 记录切分 = 区间外 %d / 区间内 %d，want 1/2",
			role.OutsidePrefixMainRows, role.VisibleMainRows)
	}

	// main 复用主会话本身：不受 join 闸门，整段都是它的上下文。
	main, err := store.readRoleSnapshot(mainKey, RoleMain, mainKey.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if main.PrefixCutSeq != 0 || main.OutsidePrefixMainRows != 0 || main.VisibleMainRows != 3 {
		t.Fatalf("main 记录切分错位：cut=%d 区间外 %d / 区间内 %d，want 0/0/3",
			main.PrefixCutSeq, main.OutsidePrefixMainRows, main.VisibleMainRows)
	}
}

// TestAssembleRoleWireForMainIsMainSessionContext 钉住「team work 前缀的作者」：
// roleName=main 复用主会话自身的引擎与 key（roleStore），所以 main 的 wire 就是
// 主会话上下文本身——起点（主会话第一条已发布行）+ 当前位置 + 主会话自身 pending
// draft（<main session root>/draft/main.jsonl），且装配只读：不把 pending 写进主
// 文档。TL 的对话记录是同一条 engine loop 写出的行，故前缀与对话记录同口径。
func TestAssembleRoleWireForMainIsMainSessionContext(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	if _, err := store.messageCommit(mainKey, "turn-2", []Event{{
		Role: "assistant", Content: "改了 chat.go 的注入顺序", Kind: EventKindLLM,
	}}); err != nil {
		t.Fatal(err)
	}
	// 主会话自己的 draft：main 角色的 roleStore 就是主会话引擎。
	if err := store.appendRoleDraftForTest(mainKey, RoleMain, mainKey.SessionID, []RoleDraftRow{{
		RoundID: 2, RoleName: RoleMain, RoleSessionID: mainKey.SessionID, UnitSeq: 1,
		MessageID: "m-main-1", Event: Event{Role: "assistant", Content: "主会话待同步行", Kind: EventKindLLM},
	}}); err != nil {
		t.Fatal(err)
	}
	before, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := store.assembleRoleWire(mainKey, RoleMain, mainKey.SessionID, 200_000, 3)
	if err != nil {
		t.Fatal(err)
	}
	if wire.RoleName != RoleMain || wire.MainSessionID != mainKey.SessionID {
		t.Fatalf("main wire 坐标错位：role=%q main=%q", wire.RoleName, wire.MainSessionID)
	}
	// 起点→当前位置：main 不受其它角色的 join/compact_ref 闸门限制，起点必须在。
	if len(wire.Messages) < 3 {
		t.Fatalf("main wire 行数 = %d，主会话上下文不完整：%+v", len(wire.Messages), wire.Messages)
	}
	if got := wire.Messages[0].Content; got != "start" {
		t.Fatalf("main wire 起点 = %q，want start（main 不应被 join 闸门截掉起点）", got)
	}
	if wire.PendingRows != 1 {
		t.Fatalf("main wire pending = %d，want 1（主会话 draft 必须进前缀）", wire.PendingRows)
	}
	if got := wire.Messages[len(wire.Messages)-1].Content; got != "主会话待同步行" {
		t.Fatalf("main wire 末行 = %q，want 主会话待同步行", got)
	}
	// 装配是读：pending draft 不得被写进主文档。
	after, err := store.readAllRows(mainKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("assembleRoleWire(main) 把 pending draft 写进了主文档：before=%d after=%d", len(before), len(after))
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
