package core

// 内容 LRU 用例（content_lru.go）：已加载会话内容（View.Conversation 窗口）
// 按 loaded_content_limit 做 LRU，只卸载「视图未切换到的、且不在运行中的」
// 空闲会话的正文；会话事实/元数据/标题/统计与磁盘数据全部保留，再激活或
// 分页时经冷回读恢复同一窗口。
//
// 断言口径：
//   - 超限时卸载最旧的空闲会话正文（最新使用与当前视图保留）；
//   - 运行中（busy）会话与当前视图会话绝不卸载，宁可容忍超限；
//   - 卸载只丢内存副本：磁盘消息数不变、目录（左侧列表）条目与标题不变；
//   - 冷回读（resumeSession 冷路径）与热挂载回读（ensureSessionContent）
//     都还原同一窗口位置（HistoryOffset/内容一致）。

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/session"
)

// withContentLimit 临时把进程级 loaded_content_limit 改成 limit，返回还原
// 函数（与 withResidentLimit / withHistoryWindow 同法，core 顺序用例内调用）。
func withContentLimit(limit int) func() {
	previous := Limits()
	applied := previous
	applied.LoadedContentLimit = limit
	ApplyLimits(applied)
	return func() { ApplyLimits(previous) }
}

// multiPagedStore 是 pagedSessionStore 的多会话版：每个会话各自一份 durable
// 消息与标题，record / conversation 窗口读同一份索引空间。内容 LRU 用例需要
// 同时装载多个会话的可见正文，因此这里按 sessionID 分槽。
type multiPagedStore struct {
	fakeSessions
	mu       sync.Mutex
	sessions []string
	messages map[string][]Message
	titles   map[string]string
}

func newMultiPagedStore(counts map[string]int) *multiPagedStore {
	store := &multiPagedStore{messages: map[string][]Message{}, titles: map[string]string{}}
	ids := make([]string, 0, len(counts))
	for sessionID := range counts {
		ids = append(ids, sessionID)
	}
	sort.Strings(ids)
	for _, sessionID := range ids {
		store.sessions = append(store.sessions, sessionID)
		store.titles[sessionID] = "会话 " + sessionID
		for index := 0; index < counts[sessionID]; index++ {
			role := "assistant"
			if index%2 == 0 {
				role = "user"
			}
			store.messages[sessionID] = append(store.messages[sessionID], Message{
				ID:        fmt.Sprintf("%s-message-%d", sessionID, index+1),
				Role:      role,
				Content:   fmt.Sprintf("durable-%d", index),
				CreatedAt: time.Unix(int64(index), 0),
			})
		}
	}
	return store
}

// SessionInfo 行只带标题（零正文读）：目录刷新不会把正文拉回内存。
func (store *multiPagedStore) SessionsOf(projectID string) []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	infos := make([]SessionInfo, 0, len(store.sessions))
	for _, sessionID := range store.sessions {
		infos = append(infos, SessionInfo{ID: sessionID, Name: store.titles[sessionID], UpdatedAt: time.Unix(1, 0)})
	}
	return infos
}

func (store *multiPagedStore) LoadHistory(sessionID string) ([]EngineMessage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	messages := store.messages[sessionID]
	history := make([]EngineMessage, 0, len(messages))
	for _, message := range messages {
		history = append(history, EngineMessage{Role: message.Role, Content: message.Content})
	}
	return history, nil
}

func (store *multiPagedStore) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	history, err := store.LoadHistory(sessionID)
	if err != nil {
		return nil, 0, err
	}
	total := len(history)
	return sliceWindow(&history, offset, limit), total, nil
}

func (store *multiPagedStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	messages, ok := store.messages[sessionID]
	if !ok {
		return SessionRecord{}, fs.ErrNotExist
	}
	record := SessionRecord{Version: 3, ID: sessionID}
	record.Title.Value = store.titles[sessionID]
	record.Conversation.Messages = append([]Message(nil), messages...)
	return record, nil
}

// SaveSessionRecord* 是 no-op：本夹具的 durable 消息由 messages 槽给出，卸载
// 前的 flush 不改变「磁盘事实」（断言据此检查消息数不变即「不丢数据」）。
func (store *multiPagedStore) SaveSessionRecord(sessionID string, record SessionRecord) error {
	return nil
}

func (store *multiPagedStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record SessionRecord) error {
	return nil
}

func (store *multiPagedStore) LoadSessionRecord(sessionID string) (SessionRecord, error) {
	return store.LoadSessionRecordWorkspace("", sessionID)
}

func (store *multiPagedStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]Message, int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	messages, ok := store.messages[sessionID]
	if !ok {
		return nil, 0, fs.ErrNotExist
	}
	total := len(messages)
	return sliceWindow(&messages, offset, limit), total, nil
}

func (store *multiPagedStore) messageCount(sessionID string) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.messages[sessionID])
}

// contentState 是会话可见正文的装载状态（LRU 断言口径）。
type contentState struct {
	loaded   bool
	unloaded bool
	window   int
	offset   int
	total    int
	contents []string
}

func readContentState(t *testing.T, service *Service, sessionID string) contentState {
	t.Helper()
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		t.Fatalf("session %s has no unit", sessionID)
	}
	state := contentState{}
	unit.View.Read(func(view *session.View) {
		state.unloaded = view.ContentUnloaded
		state.window = len(view.Conversation)
		state.offset = view.HistoryOffset
		state.total = view.TotalMessages
		for _, message := range view.Conversation {
			if message.Role == "system" {
				continue
			}
			state.contents = append(state.contents, message.Content)
		}
	})
	state.loaded = !state.unloaded && state.window > 0
	return state
}

func assertContentLoaded(t *testing.T, service *Service, sessionID string, want bool) contentState {
	t.Helper()
	state := readContentState(t, service, sessionID)
	if state.loaded != want {
		t.Fatalf("%s content loaded = %v, want %v (unloaded=%v window=%d total=%d)",
			sessionID, state.loaded, want, state.unloaded, state.window, state.total)
	}
	return state
}

// assertContentWindow 断言会话当前持有「尾部窗口 offset 起 count 条」的正文。
func assertContentWindow(t *testing.T, service *Service, sessionID string, offset, count int) {
	t.Helper()
	state := assertContentLoaded(t, service, sessionID, true)
	// 计数按 durable 正文（非 system）算：冷加载会附一条「已恢复会话」引导标记。
	if state.offset != offset || len(state.contents) != count {
		t.Fatalf("%s window = offset %d / %d 条, want offset %d / %d 条",
			sessionID, state.offset, len(state.contents), offset, count)
	}
	want := wantRange(offset, count)
	if strings.Join(state.contents, ",") != strings.Join(want, ",") {
		t.Fatalf("%s window contents = %v, want %v", sessionID, state.contents, want)
	}
}

// waitForContentLoaded 轮询等待会话正文装载完成：运行中会话存在时冷加载走
// 后台 goroutine（resumeSession 只先激活 restoring 空壳并立即返回）。
func waitForContentLoaded(t *testing.T, service *Service, sessionID string) contentState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		state := readContentState(t, service, sessionID)
		if state.loaded {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s content never loaded (unloaded=%v window=%d total=%d)",
				sessionID, state.unloaded, state.window, state.total)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitForContentUnloaded 轮询等待内容 LRU 真正把正文卸载（区别于「本来就
// 空」）：卸载由 touchContent 内的 reconcile 同步完成，但触发它的后台冷加载
// 可能晚于断言。
func waitForContentUnloaded(t *testing.T, service *Service, sessionID string) contentState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		state := readContentState(t, service, sessionID)
		if state.unloaded {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s content never unloaded (loaded=%v window=%d total=%d)",
				sessionID, state.loaded, state.window, state.total)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// snapshotSessions 等一轮目录收敛后返回左侧列表行（按 ID 索引）。
func snapshotSessions(t *testing.T, service *Service) map[string]SessionInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle: %v", err)
	}
	rows := map[string]SessionInfo{}
	for _, item := range service.Snapshot().Sessions {
		rows[item.ID] = item
	}
	return rows
}

// TestLoadedContentLimitDefaults 内容上限默认 12（seelexctx 单一事实源），
// 未配置/非法配置回落默认，显式配置生效。
func TestLoadedContentLimitDefaults(t *testing.T) {
	if got := seelexctx.DefaultLimits().LoadedContentLimit; got != 12 {
		t.Fatalf("default loaded content limit = %d, want 12", got)
	}
	if got := Limits().LoadedContentLimit; got != 12 {
		t.Fatalf("active loaded content limit = %d, want 12", got)
	}
	if got := loadedContentLimit(0); got != 12 {
		t.Fatalf("loadedContentLimit(0) = %d, want 12（未配置回落默认）", got)
	}
	if got := loadedContentLimit(-3); got != 12 {
		t.Fatalf("loadedContentLimit(-3) = %d, want 12（非法值回落默认）", got)
	}
	if got := loadedContentLimit(3); got != 3 {
		t.Fatalf("loadedContentLimit(3) = %d, want 3", got)
	}
}

// TestContentLimitEvictsLeastRecentlyUsedIdleContent 上限 1：冷加载第二个会话
// 时，最旧的空闲会话正文被卸载（ContentUnloaded + Conversation 清空），当前
// 视图保留；窗口标志/总数、磁盘消息、左侧列表条目与标题都不受影响。
func TestContentLimitEvictsLeastRecentlyUsedIdleContent(t *testing.T) {
	restoreContent := withContentLimit(1)
	defer restoreContent()
	restoreWindow := withHistoryWindow(4)
	defer restoreWindow()

	store := newMultiPagedStore(map[string]int{"sess-a": 10, "sess-b": 10})
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume sess-a: %v", err)
	}
	assertContentWindow(t, service, "sess-a", 6, 4)

	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume sess-b: %v", err)
	}
	evicted := assertContentLoaded(t, service, "sess-a", false)
	if !evicted.unloaded || evicted.window != 0 {
		t.Fatalf("sess-a evicted state = %#v, want ContentUnloaded + empty window", evicted)
	}
	// 窗口标志保留：冷回读据此还原同一窗口位置（不是重新贴一个新窗口）。
	if evicted.total != 10 || evicted.offset != 6 {
		t.Fatalf("sess-a window flags = total %d offset %d, want total 10 offset 6", evicted.total, evicted.offset)
	}
	assertContentWindow(t, service, "sess-b", 6, 4)

	// 只丢内存副本：磁盘事实不变。
	if got := store.messageCount("sess-a"); got != 10 {
		t.Fatalf("sess-a durable messages = %d, want 10（卸载不得丢数据）", got)
	}
	// 左侧列表配套：驱逐后条目与标题仍在（目录零正文读）。
	rows := snapshotSessions(t, service)
	if len(rows) != 2 {
		t.Fatalf("catalog rows after eviction = %d, want 2", len(rows))
	}
	if rows["sess-a"].Name != "会话 sess-a" || rows["sess-b"].Name != "会话 sess-b" {
		t.Fatalf("catalog titles after eviction = %#v", rows)
	}
}

// TestContentLimitKeepsBusyAndActiveSessions 上限 1：运行中（busy）会话与当前
// 视图会话都不可卸载——没有候选时容忍超限，只有空闲的非当前会话被卸载。
func TestContentLimitKeepsBusyAndActiveSessions(t *testing.T) {
	restoreContent := withContentLimit(1)
	defer restoreContent()
	restoreWindow := withHistoryWindow(4)
	defer restoreWindow()

	store := newMultiPagedStore(map[string]int{"sess-a": 10, "sess-b": 10, "sess-c": 10})
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume sess-a: %v", err)
	}
	// A 变忙（运行中）：busy 会话不可卸载（与 resident LRU 同一守卫）。
	unit := service.sessions.Unit("sess-a")
	if unit == nil {
		t.Fatal("sess-a has no unit")
	}
	unit.SetChatState(ChatState{Running: true, RequestID: "req-a", StartedAt: time.Now()}, nil)

	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume sess-b: %v", err)
	}
	// 运行中会话存在时冷加载走后台 goroutine（先是 restoring 空壳），等正文落地。
	waitForContentLoaded(t, service, "sess-b")
	// 超限但唯一候选 A 忙：容忍，B（当前视图）与 A 都保留正文。
	assertContentWindow(t, service, "sess-a", 6, 4)
	assertContentWindow(t, service, "sess-b", 6, 4)

	if err := service.ResumeSession("sess-c"); err != nil {
		t.Fatalf("resume sess-c: %v", err)
	}
	waitForContentLoaded(t, service, "sess-c")
	// 候选只剩空闲且非当前的 B：卸载 B；A（忙）与 C（当前视图）保留。
	waitForContentUnloaded(t, service, "sess-b")
	assertContentWindow(t, service, "sess-a", 6, 4)
	assertContentWindow(t, service, "sess-c", 6, 4)
}

// TestContentEvictionColdReloadsSameWindow 会话正文被卸载后再激活（冷回读）
// 必须还原同一窗口位置，而不是空会话或重新贴尾的另一窗口。
func TestContentEvictionColdReloadsSameWindow(t *testing.T) {
	restoreContent := withContentLimit(1)
	defer restoreContent()
	restoreWindow := withHistoryWindow(4)
	defer restoreWindow()

	store := newMultiPagedStore(map[string]int{"sess-a": 10, "sess-b": 10})
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume sess-a: %v", err)
	}
	assertContentWindow(t, service, "sess-a", 6, 4)

	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume sess-b: %v", err)
	}
	assertContentLoaded(t, service, "sess-a", false)

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("re-activate sess-a: %v", err)
	}
	assertContentWindow(t, service, "sess-a", 6, 4)
	// 当前视图（会话记录）与窗口一致：回读后前端拿到的就是磁盘尾部窗口。
	assertRange(t, "after cold reload", service.Snapshot(), 6, 4)
}

// TestEnsureSessionContentRebuildsEvictedWindow 热挂载回读面
// （ensureSessionContent → reloadSessionContent）独立成立：正文已卸载的会话
// 不经会话切换即可还原同一窗口。
func TestEnsureSessionContentRebuildsEvictedWindow(t *testing.T) {
	restoreContent := withContentLimit(1)
	defer restoreContent()
	restoreWindow := withHistoryWindow(4)
	defer restoreWindow()

	store := newMultiPagedStore(map[string]int{"sess-a": 10, "sess-b": 10})
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume sess-a: %v", err)
	}
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume sess-b: %v", err)
	}
	assertContentLoaded(t, service, "sess-a", false)
	if !service.sessionContentUnloaded("sess-a") {
		t.Fatal("sess-a should report unloaded content before reload")
	}

	if err := service.ensureSessionContent("sess-a"); err != nil {
		t.Fatalf("ensure content: %v", err)
	}
	if service.sessionContentUnloaded("sess-a") {
		t.Fatal("sess-a still reports unloaded content after reload")
	}
	assertContentWindow(t, service, "sess-a", 6, 4)
}
