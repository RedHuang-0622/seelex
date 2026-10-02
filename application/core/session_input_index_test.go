package core

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/session"
)

// inputIndexService 用「durable 消息 + record + conversation 窗口读」的仿真
// 端口（pagedSessionStore，见 session_history_pagination_test.go）装配服务：
// 两条通道索引空间一致，正好验证"全量索引 = 整会话用户输入"。
func inputIndexService(t *testing.T, store *pagedSessionStore) *Service {
	t.Helper()
	service := mustNew(t, Dependencies{
		Engine:    &fakeEngine{},
		Runtime:   &fakeRuntime{},
		Plugins:   &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:    fakeSkills{},
		Sessions:  store,
		Workspace: newFakeWorkspace(),
	})
	t.Cleanup(service.Shutdown)
	return service
}

// setLoadedWindow 模拟"前端只加载了尾部一窗"：把可见窗口锚定在 [start,end)。
func setLoadedWindow(t *testing.T, service *Service, sessionID string, start, end int, messages []Message) {
	t.Helper()
	service.ViewMu.Lock()
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		view.Conversation = append([]Message(nil), messages[start:end]...)
		view.TotalMessages = len(messages)
		view.HistoryOffset = start
		view.HasMoreHistory = start > 0
		view.ConversationWindow = end - start
	})
	service.ViewMu.Unlock()
}

// TestSessionInputIndexCoversUnloadedEarlyRounds 全量索引必须包含尚未加载到
// 前端的早期轮次，并按窗口标注 Loaded（右侧轨道因此代表整个会话）。
func TestSessionInputIndexCoversUnloadedEarlyRounds(t *testing.T) {
	store := newPagedSessionStore("index-session", 12) // 偶数下标 = user（共 6 条输入）
	service := inputIndexService(t, store)
	// 前端只加载了最后 4 条（durable-8..11）：早 4 条输入不在窗口里。
	setLoadedWindow(t, service, "index-session", 8, 12, store.messages)

	index, err := service.SessionInputIndex("index-session")
	if err != nil {
		t.Fatalf("SessionInputIndex: %v", err)
	}
	if index.Total != 12 {
		t.Fatalf("total = %d, want 12", index.Total)
	}
	if index.InputCount != 6 || len(index.Items) != 6 {
		t.Fatalf("input count = %d, want 6（整会话用户输入）", index.InputCount)
	}
	// 早期的用户输入必须在索引里（可以回读定位），且标为未加载。
	if index.Items[0].Offset != 0 || index.Items[0].MessageID != "message-1" {
		t.Fatalf("first row = %+v, want offset 0 / message-1", index.Items[0])
	}
	if index.Items[0].Summary != "durable-0" {
		t.Fatalf("first summary = %q, want durable-0", index.Items[0].Summary)
	}
	wantLoaded := []bool{false, false, false, false, true, true}
	for position, want := range wantLoaded {
		if index.Items[position].Loaded != want {
			t.Fatalf("items[%d] (offset %d) loaded = %v, want %v",
				position, index.Items[position].Offset, index.Items[position].Loaded, want)
		}
	}
	if index.Items[4].Offset != 8 || index.Items[5].Offset != 10 {
		t.Fatalf("loaded offsets = %d/%d, want 8/10", index.Items[4].Offset, index.Items[5].Offset)
	}
	// 轮次序号是 1 起的全量序：前端直接用它在轨道上编号与回读换算。
	for position, row := range index.Items {
		if row.Round != position+1 {
			t.Fatalf("round = %d at %d, want %d", row.Round, position, position+1)
		}
	}
	// 窗口元数据：前端据此把"目标偏移与窗口的差"换算成回读页数。
	if index.Window.Offset != 8 || index.Window.Count != 4 || index.Window.Total != 12 || !index.Window.HasMore {
		t.Fatalf("window = %+v, want offset 8 / count 4 / total 12 / has_more", index.Window)
	}
	if index.Window.WindowSize != 4 {
		t.Fatalf("window size = %d, want 4（回读步长 = 当前窗口）", index.Window.WindowSize)
	}
	// 索引读取只有一次全量扫描，且不建引擎/不驻留会话。
	if reads := store.rangeReads(); reads != 1 {
		t.Fatalf("conversation range reads = %d, want 1（轻量：一次扫描）", reads)
	}
	service.ViewMu.RLock()
	unit := service.sessions.Unit("index-session")
	service.ViewMu.RUnlock()
	if unit == nil {
		t.Fatal("索引读取不得新建会话单元之外的副作用（窗口由调用方预置）")
	}
}

// TestSessionInputIndexEmptySession 空会话（无消息、无窗口）返回空索引且不报错。
func TestSessionInputIndexEmptySession(t *testing.T) {
	store := newPagedSessionStore("empty-session", 0)
	service := inputIndexService(t, store)

	index, err := service.SessionInputIndex("empty-session")
	if err != nil {
		t.Fatalf("SessionInputIndex(empty): %v", err)
	}
	if index.Total != 0 || index.InputCount != 0 || len(index.Items) != 0 {
		t.Fatalf("empty index = %+v, want no items", index)
	}
	if index.Window.HasMore {
		t.Fatalf("empty window = %+v, want has_more=false", index.Window)
	}
	if index.SessionID != "empty-session" {
		t.Fatalf("session id = %q", index.SessionID)
	}
}

// TestSessionInputIndexRequiresSessionID 空 sessionID 显式失败（不静默成空索引）。
func TestSessionInputIndexRequiresSessionID(t *testing.T) {
	store := newPagedSessionStore("index-session", 2)
	service := inputIndexService(t, store)
	if _, err := service.SessionInputIndex("   "); err == nil {
		t.Fatal("empty session ID must fail")
	}
}

// TestSessionInputIndexTruncatesSummary 摘要长度有界（rune 截断 + 原文长度留痕）。
func TestSessionInputIndexTruncatesSummary(t *testing.T) {
	store := newPagedSessionStore("summary-session", 0)
	long := strings.Repeat("长", inputIndexSummaryLimit+140)
	store.setPublished(
		Message{ID: "m1", Role: "user", Content: long, CreatedAt: time.Now()},
		Message{ID: "m2", Role: "assistant", Content: "回答", CreatedAt: time.Now()},
	)
	service := inputIndexService(t, store)

	index, err := service.SessionInputIndex("summary-session")
	if err != nil {
		t.Fatalf("SessionInputIndex: %v", err)
	}
	if len(index.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(index.Items))
	}
	row := index.Items[0]
	summary := []rune(row.Summary)
	if len(summary) != inputIndexSummaryLimit+1 {
		t.Fatalf("summary runes = %d, want %d（上限 + 省略号）", len(summary), inputIndexSummaryLimit+1)
	}
	if !strings.HasSuffix(row.Summary, "…") {
		t.Fatalf("summary = %q, want ellipsis suffix", row.Summary)
	}
	if row.Chars != len([]rune(long)) {
		t.Fatalf("chars = %d, want %d（原文长度）", row.Chars, len([]rune(long)))
	}

	// 纯函数逐项：短文本不截断、空白压缩计入 chars、边界值不截断。
	cases := []struct {
		name      string
		text      string
		limit     int
		wantSum   string
		wantChars int
	}{
		{name: "short", text: "  你好   世界\n", limit: 10, wantSum: "你好 世界", wantChars: 5},
		{name: "exact limit", text: strings.Repeat("a", 10), limit: 10, wantSum: strings.Repeat("a", 10), wantChars: 10},
		{name: "over limit", text: strings.Repeat("a", 11), limit: 10, wantSum: strings.Repeat("a", 10) + "…", wantChars: 11},
		{name: "default limit", text: strings.Repeat("b", 200), limit: 0, wantSum: strings.Repeat("b", inputIndexSummaryLimit) + "…", wantChars: 200},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			summary, chars := summarizeInputIndexText(testCase.text, testCase.limit)
			if summary != testCase.wantSum || chars != testCase.wantChars {
				t.Fatalf("summarize(%q, %d) = %q/%d, want %q/%d",
					testCase.text, testCase.limit, summary, chars, testCase.wantSum, testCase.wantChars)
			}
		})
	}
}

// TestSessionInputIndexSkipsNonUserAndInternalRows 只索引真正的用户输入：
// 助手/工具/系统行、内部标记（task-context 检查点、会话恢复摘要）、空白行
// 都不产生刻度；skill 包装输入按"用户看到的正文"还原摘要。
func TestSessionInputIndexSkipsNonUserAndInternalRows(t *testing.T) {
	store := newPagedSessionStore("filter-session", 0)
	display := "请把右侧索引改成全量用户输入索引"
	envelope := skillContextEnvelopePrefix +
		base64.RawURLEncoding.EncodeToString([]byte(display)) +
		skillContextEnvelopeSuffix + "\n内部材料正文（不应出现在摘要里）"
	store.setPublished(
		Message{ID: "m1", Role: "user", Content: "真正的输入", CreatedAt: time.Now()},
		Message{ID: "m2", Role: "assistant", Content: "回答", CreatedAt: time.Now()},
		Message{ID: "m3", Role: "tool", Content: "工具调用", CreatedAt: time.Now()},
		Message{ID: "m4", Role: "tool_result", Content: "工具结果", CreatedAt: time.Now()},
		Message{ID: "m5", Role: "system", Content: "系统行", CreatedAt: time.Now()},
		Message{ID: "m6", Role: "user", Content: context_runtime.TaskContextCheckpointPrefix + "\n内部检查点", CreatedAt: time.Now()},
		Message{ID: "m7", Role: "user", Content: session_runtime.SessionArchiveResumePrefix + "\n恢复摘要", CreatedAt: time.Now()},
		Message{ID: "m8", Role: "user", Content: "   \n  ", CreatedAt: time.Now()},
		Message{ID: "m9", Role: "user", Content: envelope, CreatedAt: time.Now()},
		Message{ID: "m10", Role: "user", Content: "第二条真输入", CreatedAt: time.Now()},
	)
	service := inputIndexService(t, store)

	index, err := service.SessionInputIndex("filter-session")
	if err != nil {
		t.Fatalf("SessionInputIndex: %v", err)
	}
	if len(index.Items) != 3 {
		t.Fatalf("items = %+v, want 3 user inputs", index.Items)
	}
	wantIDs := []string{"m1", "m9", "m10"}
	wantSummaries := []string{"真正的输入", display, "第二条真输入"}
	for position, row := range index.Items {
		if row.MessageID != wantIDs[position] || row.Summary != wantSummaries[position] {
			t.Fatalf("items[%d] = %+v, want id %s / summary %q", position, row, wantIDs[position], wantSummaries[position])
		}
		if row.Offset != []int{0, 8, 9}[position] {
			t.Fatalf("items[%d].offset = %d, want %d（分页偏移空间）", position, row.Offset, []int{0, 8, 9}[position])
		}
	}
	if index.Total != len(store.messages) {
		t.Fatalf("total = %d, want %d", index.Total, len(store.messages))
	}
}

// TestSessionInputIndexFallsBackToRecord 会话存储没有 message 行（旧布局）时
// 退回 record 的可见会话：索引仍然全量、仍然可用。
func TestSessionInputIndexFallsBackToRecord(t *testing.T) {
	sessions := newArchiveCommandSessions()
	sessions.add("", "record-session")
	service := archiveTestService(t, sessions)

	index, err := service.SessionInputIndex("record-session")
	if err != nil {
		t.Fatalf("SessionInputIndex(record): %v", err)
	}
	if index.InputCount != 1 || len(index.Items) != 1 {
		t.Fatalf("record fallback items = %+v, want 1", index.Items)
	}
	if index.Items[0].MessageID != "m1" || index.Items[0].Summary != "record-session content" {
		t.Fatalf("record fallback row = %+v", index.Items[0])
	}
	if index.Items[0].Loaded {
		t.Fatal("没有已加载窗口的会话不得把输入标成 loaded")
	}
	if index.Total != 1 {
		t.Fatalf("record fallback total = %d, want 1", index.Total)
	}
}

// TestAlignLoadedInputTexts 对齐算法：尾部窗口整段命中；最新一条输入尚未进入
// 索引时退一步用更短前缀；完全对不上时不标（宁可不标也不错位）。
func TestAlignLoadedInputTexts(t *testing.T) {
	texts := []string{"q1", "q2", "q3", "q4", "q5"}
	cases := []struct {
		name       string
		loaded     []string
		wantStart  int
		wantLength int
	}{
		{name: "tail window", loaded: []string{"q4", "q5"}, wantStart: 3, wantLength: 2},
		{name: "whole session", loaded: texts, wantStart: 0, wantLength: 5},
		{name: "newest not indexed yet", loaded: []string{"q3", "q4", "q5", "q6"}, wantStart: 2, wantLength: 3},
		{name: "single earliest", loaded: []string{"q1"}, wantStart: 0, wantLength: 1},
		{name: "no match", loaded: []string{"zz"}, wantStart: 0, wantLength: 0},
		{name: "longer than index", loaded: []string{"q1", "q2", "q3", "q4", "q5", "q6", "q7"}, wantStart: 0, wantLength: 0},
		{name: "empty window", loaded: nil, wantStart: 0, wantLength: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			start, length := alignLoadedInputTexts(texts, testCase.loaded)
			if start != testCase.wantStart || length != testCase.wantLength {
				t.Fatalf("align(%v) = %d/%d, want %d/%d", testCase.loaded, start, length, testCase.wantStart, testCase.wantLength)
			}
		})
	}
}

// TestBuildSessionInputIndexMarksLoadedSection 纯函数口径：只有命中已加载段
// 的输入被标成 Loaded，且序号/偏移/摘要随行保留。
func TestBuildSessionInputIndexMarksLoadedSection(t *testing.T) {
	conversation := []Message{
		{ID: "m1", Role: "user", Content: "第一问"},
		{ID: "m2", Role: "assistant", Content: "答一"},
		{ID: "m3", Role: "user", Content: "第二问"},
		{ID: "m4", Role: "user", Content: "第三问"},
	}
	rows := buildSessionInputIndex(conversation, []string{"第二问", "第三问"}, inputIndexSummaryLimit)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	if rows[0].Loaded || !rows[1].Loaded || !rows[2].Loaded {
		t.Fatalf("loaded flags = %v/%v/%v, want false/true/true", rows[0].Loaded, rows[1].Loaded, rows[2].Loaded)
	}
	if rows[1].Offset != 2 || rows[2].Offset != 3 {
		t.Fatalf("offsets = %d/%d, want 2/3", rows[1].Offset, rows[2].Offset)
	}
}
