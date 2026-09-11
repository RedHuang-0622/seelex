package core

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件复现「长会话加载更早历史」的分页缺陷（2026-09-11）。
//
// 现场（生产装配 internal/adapters.SessionPort）：
//   - 冷加载有 record 的会话时，可见窗口 = 会话尾部 window 条，
//     HistoryOffset = total - window，HasMoreHistory = offset > 0；
//   - 前端顶部 sentinel/「加载更早」调用 LoadMoreHistory(limit) 取更早一页，
//     期望：窗口向更早处平移一页、offset 单调减小、内容连续无洞无重。
//
// 旧实现的两个缺陷（本文件即为红灯）：
//  1. LoadMoreHistory 只写 Snapshot 镜像，不写会话自己的 View（事实源）。
//     任何一次镜像（新消息/工具事件/切换）都会把分页结果整体抹掉，offset
//     退回尾部窗口 → 用户点「加载更早」后内容回卷、下次又取同一页。
//  2. 追加新消息时 boundViewTailLocked 无条件把窗口重新贴尾，即使窗口正锚定
//     在更早的历史位置——正在翻旧账的会话一旦有事件就跳回尾部。

// withHistoryWindow 临时把进程级 history_window 调小，让分页行为在少量消息
// 上可复现（core 顺序用例内调用，返回还原函数，与 withResidentLimit 同法）。
func withHistoryWindow(window int) func() {
	previous := Limits()
	applied := previous
	applied.HistoryWindow = window
	ApplyLimits(applied)
	return func() { ApplyLimits(previous) }
}

// pagedSessionStore 仿生产会话端口：record 与 conversation 窗口读的是同一份
// durable 消息（v8 布局里两者都由 message 事件行派生，索引空间一致）。
type pagedSessionStore struct {
	fakeSessions
	mu        sync.Mutex
	sessionID string
	title     string
	messages  []Message
	rangeRead int
}

func newPagedSessionStore(sessionID string, count int) *pagedSessionStore {
	store := &pagedSessionStore{sessionID: sessionID, title: "长会话"}
	for index := 0; index < count; index++ {
		role := "assistant"
		if index%2 == 0 {
			role = "user"
		}
		store.messages = append(store.messages, Message{
			ID:        fmt.Sprintf("message-%d", index+1),
			Role:      role,
			Content:   fmt.Sprintf("durable-%d", index),
			CreatedAt: time.Unix(int64(index), 0),
		})
	}
	return store
}

func (store *pagedSessionStore) countLocked() int {
	return len(store.messages)
}

func (store *pagedSessionStore) SessionsOf(projectID string) []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	return []SessionInfo{{ID: store.sessionID, Name: store.title, UpdatedAt: time.Unix(1, 0)}}
}

func (store *pagedSessionStore) LoadHistory(sessionID string) ([]EngineMessage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	history := make([]EngineMessage, 0, len(store.messages))
	for _, message := range store.messages {
		history = append(history, EngineMessage{Role: message.Role, Content: message.Content})
	}
	return history, nil
}

func (store *pagedSessionStore) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	history, err := store.LoadHistory(sessionID)
	if err != nil {
		return nil, 0, err
	}
	total := len(history)
	return sliceWindow(&history, offset, limit), total, nil
}

func (store *pagedSessionStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if sessionID != store.sessionID {
		return SessionRecord{}, fs.ErrNotExist
	}
	record := SessionRecord{Version: 3, ID: sessionID}
	record.Title.Value = store.title
	record.Conversation.Messages = append([]Message(nil), store.messages...)
	return record, nil
}

// 下面三个方法补齐 SessionRecordPort（生产 internal/adapters.SessionPort 同时
// 实现该面；缺一个方法就不会被当成 record 端口，冷加载会退化到旧的
// provider 历史路径）。
func (store *pagedSessionStore) SaveSessionRecord(sessionID string, record SessionRecord) error {
	return nil
}

func (store *pagedSessionStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record SessionRecord) error {
	return nil
}

func (store *pagedSessionStore) LoadSessionRecord(sessionID string) (SessionRecord, error) {
	return store.LoadSessionRecordWorkspace("", sessionID)
}

// LoadConversationRangeWorkspace 是生产分页读回面（SessionConversationRangePort）：
// offset/limit 与 record 的可见消息序列同空间。
func (store *pagedSessionStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]Message, int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if sessionID != store.sessionID {
		return nil, 0, fs.ErrNotExist
	}
	store.rangeRead++
	total := store.countLocked()
	return sliceWindow(&store.messages, offset, limit), total, nil
}

func (store *pagedSessionStore) appendDurable(role, content string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.messages = append(store.messages, Message{
		ID:        fmt.Sprintf("message-%d", len(store.messages)+1),
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	})
}

func (store *pagedSessionStore) rangeReads() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.rangeRead
}

func sliceWindow[T any](items *[]T, offset, limit int) []T {
	total := len(*items)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return append([]T(nil), (*items)[offset:end]...)
}

// visibleContents 取快照对话里的非 system 消息内容（system 是「已恢复会话」
// 之类的引导标记，不参与 durable 索引空间）。
func visibleContents(snapshot Snapshot) []string {
	contents := []string{}
	for _, message := range snapshot.Conversation {
		if message.Role == "system" {
			continue
		}
		contents = append(contents, message.Content)
	}
	return contents
}

func wantRange(start, count int) []string {
	contents := make([]string, 0, count)
	for index := start; index < start+count; index++ {
		contents = append(contents, fmt.Sprintf("durable-%d", index))
	}
	return contents
}

func assertRange(t *testing.T, label string, snapshot Snapshot, start, count int) {
	t.Helper()
	if got := snapshot.HistoryOffset; got != start {
		t.Fatalf("%s: history_offset = %d, want %d", label, got, start)
	}
	got := visibleContents(snapshot)
	want := wantRange(start, count)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: visible = %v, want %v", label, got, want)
	}
}

// TestLegacySessionWithoutRecordExposesEarlierHistory 旧格式会话（端口只提供
// provider 历史与窗口读，没有 record 面）冷加载时同样必须知道历史总数：修
// 复前可见条数被当成总数（total=尾窗条数、offset 恒为 0、
// has_more_history 恒为 false），长会话的「加载更早」根本点不出来。
func TestLegacySessionWithoutRecordExposesEarlierHistory(t *testing.T) {
	defer withHistoryWindow(6)()

	history := make([]EngineMessage, 20)
	for index := range history {
		history[index] = EngineMessage{Role: "assistant", Content: fmt.Sprintf("durable-%d", index)}
	}
	sessions := &scopedSessions{
		catalog:   map[string][]SessionInfo{"": {{ID: "legacy-session", UpdatedAt: time.Unix(1, 0)}}},
		histories: map[string]map[string][]EngineMessage{"": {"legacy-session": history}},
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := service.ResumeSession("legacy-session"); err != nil {
		t.Fatalf("resume legacy session: %v", err)
	}
	assertRange(t, "legacy cold load", service.Snapshot(), 14, 6)
	if !service.Snapshot().HasMoreHistory {
		t.Fatal("旧格式长会话必须暴露 has_more_history（否则前端「加载更早」不可用）")
	}

	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	assertRange(t, "legacy page back", service.Snapshot(), 8, 6)
}

// openLongSession 打开一个长会话（window 条尾窗 + offset），返回服务与存储。
func openLongSession(t *testing.T, window, total int) (*Service, *pagedSessionStore) {
	t.Helper()
	store := newPagedSessionStore("long-session", total)
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := service.ResumeSession("long-session"); err != nil {
		t.Fatalf("resume long session: %v", err)
	}
	assertRange(t, "cold load tail window", service.Snapshot(), total-window, window)
	return service, store
}

// mirrorOnce 触发一次会话可见投影镜像（真实链路上任何新消息/工具事件都会
// 走到这里），旧实现就在这一步丢掉分页结果。
func mirrorOnce(t *testing.T, service *Service, store *pagedSessionStore) {
	t.Helper()
	store.appendDurable("assistant", "late-event")
	service.ViewMu.Lock()
	service.appendMessageLocked("assistant", "late-event", nil)
	service.ViewMu.Unlock()
}

// TestLoadMoreHistoryPrependsPageAndSurvivesMirror 红灯 1：
// 「加载更早」取到的一页必须留在会话可见投影里，不能被随后的镜像抹回尾部。
func TestLoadMoreHistoryPrependsPageAndSurvivesMirror(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)

	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	assertRange(t, "after one page back", service.Snapshot(), 8, 6)

	mirrorOnce(t, service, store)

	snapshot := service.Snapshot()
	if snapshot.HistoryOffset != 8 {
		t.Fatalf("history_offset after mirror = %d, want 8（分页结果必须随会话可见投影持久，不能只写 Snapshot 镜像）", snapshot.HistoryOffset)
	}
	if got := visibleContents(snapshot); len(got) == 0 || got[0] != "durable-8" {
		t.Fatalf("visible after mirror = %v, want 以 durable-8 开头的窗口", got)
	}
}

// TestLoadMoreHistoryPagesToBeginning 红灯 2：连续翻页必须单调向更早推进，
// 直到 offset=0（能读到会话最早的内容）。每页之间有事件触发镜像也在所不
// 惜——分页态属于会话可见投影，不该被镜像抹掉。
//
// 断言的是窗口模型的不变量，而不是「跨页零重复」：可见窗口是 durable 的一段
// 连续区间，末页不足一整页时窗口与上一页自然重叠（有界窗口的固有语义），
// 但 offset 必须严格向更早推进、相邻/全部窗口的并集必须覆盖每一条消息。
func TestLoadMoreHistoryPagesToBeginning(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)

	covered := map[string]bool{}
	previousOffset := -1
	check := func(label string, snapshot Snapshot) {
		visible := visibleContents(snapshot)
		// 窗口必须是 [offset, offset+len) 的连续区间（第一条内容对得上 offset）。
		if len(visible) > 0 {
			want := fmt.Sprintf("durable-%d", snapshot.HistoryOffset)
			if visible[0] != want {
				t.Fatalf("%s: 窗口首条 = %q, want %q（offset=%d, visible=%v）", label, visible[0], want, snapshot.HistoryOffset, visible)
			}
		}
		if previousOffset >= 0 && snapshot.HistoryOffset >= previousOffset {
			t.Fatalf("%s: history_offset 未向更早推进（%d → %d）——分页态被镜像抹回尾部", label, previousOffset, snapshot.HistoryOffset)
		}
		previousOffset = snapshot.HistoryOffset
		for _, content := range visible {
			covered[content] = true
		}
	}

	check("cold tail window", service.Snapshot())

	pages := 0
	for service.Snapshot().HasMoreHistory {
		pages++
		if pages > 10 {
			t.Fatalf("分页在 %d 页后仍未到底（history_offset = %d）：分页态未随会话可见投影持久", pages, service.Snapshot().HistoryOffset)
		}
		if err := service.LoadMoreHistory(0); err != nil {
			t.Fatalf("load more history page %d: %v", pages, err)
		}
		check(fmt.Sprintf("page %d", pages), service.Snapshot())
		mirrorOnce(t, service, store)
	}

	if got := service.Snapshot().HistoryOffset; got != 0 {
		t.Fatalf("history_offset after paging to beginning = %d, want 0", got)
	}
	for index := 0; index < 20; index++ {
		if content := fmt.Sprintf("durable-%d", index); !covered[content] {
			t.Fatalf("分页跳过了 %q（并集未覆盖全部消息）", content)
		}
	}
}

// TestLoadLatestHistoryReturnsToTail 红灯 3：翻到更早以后必须能一键回到最新
// （否则用户被永久留在历史窗口里，新消息再也看不到）。
func TestLoadLatestHistoryReturnsToTail(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	assertRange(t, "paged back", service.Snapshot(), 8, 6)

	// 回看期间到达的新消息必须由「回到最新」一并带回（不能只把窗口贴回旧尾）。
	for index := 0; index < 3; index++ {
		mirrorOnce(t, service, store)
	}

	// 红灯期占位：LoadLatestHistory 是本轮新增的应用边界（回到最新一页）。
	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("load latest history: %v", err)
	}
	// total=23、窗口 6 → 尾窗 = [17, 23)：前三条是 durable-17..19，末三条是
	// 回看期间到达的新消息（late-event）——回到最新必须把它们一并带回。
	snapshot := service.Snapshot()
	if snapshot.HistoryOffset != 17 {
		t.Fatalf("history_offset = %d, want 17", snapshot.HistoryOffset)
	}
	visible := visibleContents(snapshot)
	if len(visible) != 6 || visible[0] != "durable-17" {
		t.Fatalf("回到最新的窗口 = %v, want 以 durable-17 开头的 6 条", visible)
	}
	if last := visible[len(visible)-1]; last != "late-event" {
		t.Fatalf("回到最新的窗口末条 = %q, want late-event（回看期间的新消息必须带回）", last)
	}
	if !snapshot.HasMoreHistory {
		t.Fatal("回到最新后仍应保留「还有更早历史」状态")
	}
}

// TestAppendWhileBrowsingHistoryKeepsWindow 红灯 4：正在翻更早历史时新到达的
// 消息不得把窗口拽回尾部（否则用户读到一半就被抢走阅读位置）；它应只推进
// total，让前端能判断「下方还有新内容」。
func TestAppendWhileBrowsingHistoryKeepsWindow(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}

	for index := 0; index < 3; index++ {
		mirrorOnce(t, service, store)
	}

	snapshot := service.Snapshot()
	assertRange(t, "appends while browsing", snapshot, 8, 6)
	if snapshot.TotalMessages != 23 {
		t.Fatalf("total_messages = %d, want 23（新消息仍要计入总数）", snapshot.TotalMessages)
	}
	if snapshot.HistoryOffset+len(visibleContents(snapshot)) >= snapshot.TotalMessages {
		t.Fatalf("窗口未贴尾时前端需要能判断下方还有新内容：offset=%d visible=%d total=%d",
			snapshot.HistoryOffset, len(visibleContents(snapshot)), snapshot.TotalMessages)
	}
}
