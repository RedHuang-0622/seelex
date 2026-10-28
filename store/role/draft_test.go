package role

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// draftFixture 提供一个独立角色根目录与一批可复用的草稿行。
func draftFixture(t *testing.T) (string, []DraftRow) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "goal_deadbeef")
	rows := []DraftRow{
		{
			RoundID: 1, RoleName: RoleTL, RoleSessionID: "role-session-tl", UnitSeq: 1,
			MessageID: "m-1",
			Event: protocol.TranscriptEvent{
				Role: "assistant", Kind: protocol.TranscriptEventKindLLM, Content: "先看代码",
			},
		},
		{
			RoundID: 1, RoleName: RoleTL, RoleSessionID: "role-session-tl", UnitSeq: 2,
			MessageID: "m-2",
			Event: protocol.TranscriptEvent{
				Role: "tool", Kind: protocol.TranscriptEventKindToolOutput,
				ToolCallID: "c-1", Name: "bash", Content: "ok",
			},
		},
	}
	return root, rows
}

// TestDraftPathIsUnchanged 钉住零数据迁移：草稿仍在 <roleRoot>/draft/<roleName>.jsonl。
func TestDraftPathIsUnchanged(t *testing.T) {
	got := DraftPath(filepath.Join("root", "goal_abc"), RoleTL)
	want := filepath.Join("root", "goal_abc", "draft", "tl.jsonl")
	if got != want {
		t.Fatalf("DraftPath = %q, want %q", got, want)
	}
}

// TestAppendReadRoundTripKeepsFields：写入再读回必须逐字段相等，且磁盘行的 JSON
// 键名不得漂移（老进程写的草稿只有老键名）。
func TestAppendReadRoundTripKeepsFields(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatalf("AppendDraft: %v", err)
	}
	got, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("读回 %d 行，want %d", len(got), len(rows))
	}
	for index, want := range rows {
		if got[index].RoundID != want.RoundID || got[index].RoleName != want.RoleName ||
			got[index].RoleSessionID != want.RoleSessionID || got[index].UnitSeq != want.UnitSeq ||
			got[index].MessageID != want.MessageID {
			t.Fatalf("第 %d 行元数据漂移：got %+v, want %+v", index, got[index], want)
		}
		if got[index].Event.Kind != want.Event.Kind || got[index].Event.Role != want.Event.Role ||
			got[index].Event.Content != want.Event.Content ||
			got[index].Event.ToolCallID != want.Event.ToolCallID ||
			got[index].Event.Name != want.Event.Name {
			t.Fatalf("第 %d 行事件漂移：got %+v, want %+v", index, got[index].Event, want.Event)
		}
	}
	// JSON 键名逐字检查（用 marshal 输出对照，避免只靠 Unmarshal 掩盖改名）。
	payload, err := os.ReadFile(DraftPath(root, RoleTL))
	if err != nil {
		t.Fatalf("读取草稿文件：%v", err)
	}
	for _, key := range []string{`"round_id"`, `"role_name"`, `"role_session_id"`, `"unit_seq"`, `"event"`, `"message_id"`} {
		if !strings.Contains(string(payload), key) {
			t.Fatalf("草稿行缺 JSON 键 %s：%s", key, payload)
		}
	}
	if !strings.Contains(string(payload), `"content":"先看代码"`) {
		t.Fatalf("event 未被内联序列化：%s", payload)
	}
}

// TestAppendFillsMissingOwnership：空归属必须由参数补齐，已有归属绝不覆盖。
func TestAppendFillsMissingOwnership(t *testing.T) {
	root, _ := draftFixture(t)
	rows := []DraftRow{
		{RoundID: 1, UnitSeq: 1, Event: protocol.TranscriptEvent{Role: "assistant"}},
		{RoundID: 1, UnitSeq: 2, RoleName: "reviewer", RoleSessionID: "explicit", Event: protocol.TranscriptEvent{Role: "assistant"}},
	}
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatalf("AppendDraft: %v", err)
	}
	got, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if got[0].RoleName != RoleTL || got[0].RoleSessionID != "role-session-tl" {
		t.Fatalf("空归属未补齐：%+v", got[0])
	}
	if got[1].RoleName != "reviewer" || got[1].RoleSessionID != "explicit" {
		t.Fatalf("已有归属被覆盖：%+v", got[1])
	}
}

// TestAppendRequiresRoleNameAndToleratesEmpty：空 role 名是调用错误；空行集是空操作。
func TestAppendRequiresRoleNameAndToleratesEmpty(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, "", "role-session-tl", rows); err == nil {
		t.Fatal("空 roleName 必须报错，不得猜一个角色写盘")
	}
	if err := AppendDraft(root, RoleTL, "role-session-tl", nil); err != nil {
		t.Fatalf("空行集必须是空操作：%v", err)
	}
	if _, err := os.Stat(DraftPath(root, RoleTL)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("空行集不得创建文件：%v", err)
	}
}

// TestReadDraftMissingFileIsEmpty：文件不存在 = 没有待同步行，不是错误。
func TestReadDraftMissingFileIsEmpty(t *testing.T) {
	root, _ := draftFixture(t)
	rows, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("缺文件必须返回空集而不是错误：%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("缺文件读到 %d 行", len(rows))
	}
}

// TestReadDraftSkipsTruncatedTailAndBadLines：末尾无换行的半行是崩溃残尾，只读完整行；
// 坏 JSON 行跳过，不让一行埋掉整批草稿。
func TestReadDraftSkipsTruncatedTailAndBadLines(t *testing.T) {
	root, _ := draftFixture(t)
	path := DraftPath(root, RoleTL)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"round_id":1,"role_name":"tl","role_session_id":"r","unit_seq":1,"event":{"role":"assistant"}}` + "\n" +
		`{"round_id":1,"role_name":"tl","role_session_id":"r","unit_seq":2,"event":{"role":"tool"}}` + "\n" +
		`{"round_id":1,"role_name":"tl","role_sess`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("读回 %d 行，want 2（残尾必须丢弃）", len(rows))
	}
	if rows[0].UnitSeq != 1 || rows[1].UnitSeq != 2 {
		t.Fatalf("行序/内容漂移：%+v", rows)
	}

	// 坏行（完整换行但非法 JSON）跳过，不影响其它行。
	body += "\nnot-json\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err = ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("坏行未被跳过：读回 %d 行，want 2", len(rows))
	}
}

// TestAppendTruncatesCrashTailThenAppends：残尾先被截断，再追加新行——既不丢已完整
// 的行，也不把残尾和新行拼成坏 JSON。
func TestAppendTruncatesCrashTailThenAppends(t *testing.T) {
	root, _ := draftFixture(t)
	path := DraftPath(root, RoleTL)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	complete := `{"round_id":1,"role_name":"tl","role_session_id":"r","unit_seq":1,"event":{"role":"assistant","content":"old"}}` + "\n"
	if err := os.WriteFile(path, []byte(complete+`{"round_id":1,"role_name":"tl","unit_s`), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := []DraftRow{{
		RoundID: 2, UnitSeq: 1, Event: protocol.TranscriptEvent{Role: "assistant", Content: "new"},
	}}
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatalf("AppendDraft: %v", err)
	}
	got, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("读回 %d 行，want 2（旧完整行 + 新行，残尾不得复活）", len(got))
	}
	if got[0].Event.Content != "old" || got[1].Event.Content != "new" {
		t.Fatalf("追加后内容漂移：%+v", got)
	}
}

// TestConcurrentAppendKeepsEveryRow：同角色并发追加必须一行不丢、一行不重。
func TestConcurrentAppendKeepsEveryRow(t *testing.T) {
	root, _ := draftFixture(t)
	const writers = 16
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := 0; index < writers; index++ {
		index := index
		go func() {
			defer wait.Done()
			row := DraftRow{
				RoundID: 1, UnitSeq: uint64(index),
				Event: protocol.TranscriptEvent{Role: "assistant", Content: "row"},
			}
			if err := AppendDraft(root, RoleTL, "role-session-tl", []DraftRow{row}); err != nil {
				t.Errorf("AppendDraft(%d): %v", index, err)
			}
		}()
	}
	wait.Wait()
	rows, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if len(rows) != writers {
		t.Fatalf("读回 %d 行，want %d", len(rows), writers)
	}
	seen := make(map[uint64]struct{}, writers)
	for _, row := range rows {
		if _, dup := seen[row.UnitSeq]; dup {
			t.Fatalf("unit_seq %d 重复：并发追加把同一行写了两次", row.UnitSeq)
		}
		seen[row.UnitSeq] = struct{}{}
		if row.RoleName != RoleTL || row.RoleSessionID != "role-session-tl" {
			t.Fatalf("并发追加丢了归属补齐：%+v", row)
		}
	}
}

// TestSortDraftRowsOrder：round → 顺序表内排名 → 未上表角色名 → unit_seq，且入参不被改。
func TestSortDraftRowsOrder(t *testing.T) {
	input := []DraftRow{
		{RoundID: 2, RoleName: RoleTL, UnitSeq: 1},
		{RoundID: 1, RoleName: "reviewer", UnitSeq: 9},
		{RoundID: 1, RoleName: RoleTL, UnitSeq: 3},
		{RoundID: 1, RoleName: RoleTL, UnitSeq: 1},
		{RoundID: 1, RoleName: "goalplan", UnitSeq: 1},
		{RoundID: 1, RoleName: RoleMain, UnitSeq: 1},
	}
	original := append([]DraftRow(nil), input...)
	got := SortDraftRows(input, []string{RoleMain, RoleTL})
	want := []struct {
		round uint64
		role  string
		seq   uint64
	}{
		{1, RoleMain, 1},
		{1, RoleTL, 1},
		{1, RoleTL, 3},
		{1, "goalplan", 1},
		{1, "reviewer", 9},
		{2, RoleTL, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("排序后 %d 行，want %d", len(got), len(want))
	}
	for index, expected := range want {
		if got[index].RoundID != expected.round || got[index].RoleName != expected.role || got[index].UnitSeq != expected.seq {
			t.Fatalf("第 %d 位 = (%d,%s,%d)，want (%d,%s,%d)",
				index, got[index].RoundID, got[index].RoleName, got[index].UnitSeq,
				expected.round, expected.role, expected.seq)
		}
	}
	for index := range input {
		if !reflect.DeepEqual(input[index], original[index]) {
			t.Fatalf("入参切片被排序修改（第 %d 位）：%+v != %+v", index, input[index], original[index])
		}
	}
}

// TestDraftCommitIDIgnoresOrderAndFollowsContent：身份对行序不敏感，对内容敏感。
func TestDraftCommitIDIgnoresOrderAndFollowsContent(t *testing.T) {
	_, rows := draftFixture(t)
	sorted := SortDraftRows(rows, []string{RoleTL})
	identity := DraftCommitID(RoleTL, "role-session-tl", sorted)
	shuffled := []DraftRow{sorted[1], sorted[0]}
	if got := DraftCommitID(RoleTL, "role-session-tl", shuffled); got != identity {
		t.Fatalf("顺序不同导致身份漂移：%q != %q", got, identity)
	}
	if DraftCommitID("reviewer", "role-session-tl", sorted) == identity {
		t.Fatal("roleName 变化后身份必须变化")
	}
	if DraftCommitID(RoleTL, "role-session-other", sorted) == identity {
		t.Fatal("roleSessionID 变化后身份必须变化")
	}
	changed := append([]DraftRow(nil), sorted...)
	changed[0] = sorted[0]
	changed[0].Event.Content = "改过了"
	if got := DraftCommitID(RoleTL, "role-session-tl", changed); got == identity {
		t.Fatal("内容变化后身份必须变化")
	}
	if !strings.HasPrefix(identity, "role-draft-"+RoleTL+"-") {
		t.Fatalf("身份前缀漂移：%q", identity)
	}
}

// TestRowsToEventsFillsOwnershipAndKind：draft 行转事件时必须补齐归属/round/unit 与
// message_id 兜底，旧数据 Kind 为空按回退判据补齐。
func TestRowsToEventsFillsOwnershipAndKind(t *testing.T) {
	rows := []DraftRow{
		{
			RoundID: 3, RoleName: RoleTL, RoleSessionID: "role-session-tl", UnitSeq: 7,
			MessageID: "m-fallback",
			Event:     protocol.TranscriptEvent{Role: "assistant", Content: "x"},
		},
	}
	events := RowsToEvents(rows)
	if len(events) != 1 {
		t.Fatalf("转换出 %d 行", len(events))
	}
	event := events[0]
	if event.RoleName != RoleTL || event.RoleSessionID != "role-session-tl" ||
		event.RoundID != 3 || event.UnitSeq != 7 {
		t.Fatalf("归属/round/unit 未补齐：%+v", event)
	}
	if event.MessageID != "m-fallback" {
		t.Fatalf("message_id 兜底失败：%q", event.MessageID)
	}
	if event.Kind != protocol.TranscriptEventKindLLM {
		t.Fatalf("Kind 回退失败：%q", event.Kind)
	}
	// 行上已有的 message_id 优先，不被行元数据覆盖。
	explicit := RowsToEvents([]DraftRow{{
		MessageID: "row-id",
		Event:     protocol.TranscriptEvent{Role: "assistant", MessageID: "event-id"},
	}})
	if explicit[0].MessageID != "event-id" {
		t.Fatalf("事件自带 message_id 被行元数据覆盖：%q", explicit[0].MessageID)
	}
}

// fakeSessionPort 是 SyncDraft 的假端口：只记录调用事实，不碰任何真实存储。
type fakeSessionPort struct {
	head        storesession.MessageHead
	headErr     error
	commitErr   error
	nextHead    storesession.MessageHead
	headCalls   int
	commitCalls int
	lastCommit  string
	lastRows    []protocol.TranscriptEvent
	lastFloor   *storesession.Floor
}

func (port *fakeSessionPort) MessageHead() (storesession.MessageHead, error) {
	port.headCalls++
	if port.headErr != nil {
		return storesession.MessageHead{}, port.headErr
	}
	return port.head, nil
}

func (port *fakeSessionPort) CommitSync(
	commitID string,
	rows []protocol.TranscriptEvent,
	floor *storesession.Floor,
) (storesession.MessageHead, error) {
	port.commitCalls++
	port.lastCommit = commitID
	port.lastRows = rows
	port.lastFloor = floor
	if port.commitErr != nil {
		return storesession.MessageHead{}, port.commitErr
	}
	if port.nextHead.LastCommitID == "" {
		port.nextHead.LastCommitID = commitID
	}
	return port.nextHead, nil
}

// TestSyncDraftEmptyDraftIsNoop：没有待同步行时零结果、零端口调用。
func TestSyncDraftEmptyDraftIsNoop(t *testing.T) {
	root, _ := draftFixture(t)
	port := &fakeSessionPort{}
	result, err := SyncDraft(root, RoleTL, "role-session-tl", nil, port)
	if err != nil {
		t.Fatalf("SyncDraft: %v", err)
	}
	if result != (SyncResult{}) {
		t.Fatalf("空草稿结果 = %+v，want 零值", result)
	}
	if port.headCalls != 0 || port.commitCalls != 0 {
		t.Fatalf("空草稿不得调用端口：head=%d commit=%d", port.headCalls, port.commitCalls)
	}
}

// TestSyncDraftPublishesAndDeletesDraft：端口成功后草稿删除，计数与 head 读数透传。
func TestSyncDraftPublishesAndDeletesDraft(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatal(err)
	}
	port := &fakeSessionPort{nextHead: storesession.MessageHead{LastSeq: 42, LastMessageID: "m-2"}}
	result, err := SyncDraft(root, RoleTL, "role-session-tl", []string{RoleTL}, port)
	if err != nil {
		t.Fatalf("SyncDraft: %v", err)
	}
	if result.AlreadySynced {
		t.Fatal("首次发布不得报 AlreadySynced")
	}
	if result.SyncedRows != len(rows) {
		t.Fatalf("SyncedRows = %d, want %d", result.SyncedRows, len(rows))
	}
	if result.LastSeq != 42 || result.LastMessageID != "m-2" {
		t.Fatalf("head 读数未透传：%+v", result)
	}
	if port.commitCalls != 1 {
		t.Fatalf("CommitSync 调用 %d 次，want 1", port.commitCalls)
	}
	if len(port.lastRows) != len(rows) {
		t.Fatalf("发布 %d 行，want %d", len(port.lastRows), len(rows))
	}
	if port.lastFloor == nil || port.lastFloor.RoleName != RoleTL ||
		port.lastFloor.RoleSessionID != "role-session-tl" || port.lastFloor.RoundID != 1 {
		t.Fatalf("floor 未随发布交给端口：%+v", port.lastFloor)
	}
	if port.lastCommit != result.CommitID {
		t.Fatalf("端口收到的 commit_id %q != 结果 %q", port.lastCommit, result.CommitID)
	}
	if _, err := os.Stat(DraftPath(root, RoleTL)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("发布成功后草稿必须删除：%v", err)
	}
}

// TestSyncDraftKeepsDraftWhenCommitFails：端口失败 → 返回错误且草稿**仍在**
// （半失败不许丢证据）。
func TestSyncDraftKeepsDraftWhenCommitFails(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatal(err)
	}
	port := &fakeSessionPort{commitErr: errors.New("磁盘抖动")}
	if _, err := SyncDraft(root, RoleTL, "role-session-tl", nil, port); err == nil {
		t.Fatal("端口失败必须向上报错")
	}
	if _, err := os.Stat(DraftPath(root, RoleTL)); err != nil {
		t.Fatalf("发布失败时草稿不得删除：%v", err)
	}
	got, err := ReadDraft(root, RoleTL)
	if err != nil || len(got) != len(rows) {
		t.Fatalf("发布失败后草稿必须完整保留：len=%d err=%v", len(got), err)
	}
}

// TestSyncDraftHeadErrorKeepsDraft：幂等判定读 head 失败也不得动草稿。
func TestSyncDraftHeadErrorKeepsDraft(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatal(err)
	}
	port := &fakeSessionPort{headErr: errors.New("head 读不出来")}
	if _, err := SyncDraft(root, RoleTL, "role-session-tl", nil, port); err == nil {
		t.Fatal("head 读失败必须向上报错")
	}
	if port.commitCalls != 0 {
		t.Fatalf("head 都没读到时不得发布：commit=%d", port.commitCalls)
	}
	if _, err := os.Stat(DraftPath(root, RoleTL)); err != nil {
		t.Fatalf("head 读失败时草稿不得删除：%v", err)
	}
}

// TestSyncDraftAlreadySyncedOnlyCleansDraft：head 已带同一 commit_id（半同步窗口）→
// 只删草稿，不重复 append。
func TestSyncDraftAlreadySyncedOnlyCleansDraft(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatal(err)
	}
	// 用与 SyncDraft 相同的排序/身份判据算出期望 commit_id。
	stored, err := ReadDraft(root, RoleTL)
	if err != nil {
		t.Fatal(err)
	}
	commitID := DraftCommitID(RoleTL, "role-session-tl", SortDraftRows(stored, []string{RoleTL}))
	port := &fakeSessionPort{
		head: storesession.MessageHead{LastCommitID: commitID, LastSeq: 7, LastMessageID: "m-2"},
	}
	result, err := SyncDraft(root, RoleTL, "role-session-tl", []string{RoleTL}, port)
	if err != nil {
		t.Fatalf("SyncDraft: %v", err)
	}
	if !result.AlreadySynced {
		t.Fatal("head 已有同一 commit_id 时必须报 AlreadySynced")
	}
	if result.CommitID != commitID || result.LastSeq != 7 || result.LastMessageID != "m-2" {
		t.Fatalf("幂等结果漂移：%+v", result)
	}
	if port.commitCalls != 0 {
		t.Fatalf("幂等分支不得调用 CommitSync：%d 次", port.commitCalls)
	}
	if _, err := os.Stat(DraftPath(root, RoleTL)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("幂等分支必须清理残留草稿：%v", err)
	}
}

// TestDeleteDraftRemovesFile：删除后读面回到空集。
func TestDeleteDraftRemovesFile(t *testing.T) {
	root, rows := draftFixture(t)
	if err := AppendDraft(root, RoleTL, "role-session-tl", rows); err != nil {
		t.Fatal(err)
	}
	if err := DeleteDraft(root, RoleTL); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	got, err := ReadDraft(root, RoleTL)
	if err != nil || len(got) != 0 {
		t.Fatalf("删除后必须读到空集：len=%d err=%v", len(got), err)
	}
	if err := DeleteDraft(root, RoleTL); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("重复删除应返回 fs.ErrNotExist 交给调用方判定：%v", err)
	}
}
