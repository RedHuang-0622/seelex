package assembly

import (
	"encoding/base64"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"github.com/RedHuang-0622/seelex/middle/residency"
	"strings"
	"testing"
	"time"
)

// inputIndexService 用「durable 消息 + record + conversation 窗口读」的仿真
// 端口（pagedSessionStore，见 session_history_pagination_test.go）装配服务：
// 两条通道索引空间一致，正好验证"全量索引 = 整会话用户输入"。

func inputIndexService(t *testing.T, store *pagedSessionStore) *Service {
	t.Helper()
	service := mustNew(t, contract.Dependencies{
		Engine:    &fakeEngine{},
		Runtime:   &fakeRuntime{},
		Plugins:   &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills:    fakeSkills{},
		Sessions:  store,
		Workspace: newFakeWorkspace(),
	})
	t.Cleanup(service.components.lifecycleApplication.Shutdown)
	return service
}

// setLoadedWindow 模拟"前端只加载了尾部一窗"：把可见窗口锚定在 [start,end)。
func setLoadedWindow(t *testing.T, service *Service, sessionID string, start, end int, messages []model.Message) {
	t.Helper()
	service.ViewMu.Lock()
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		view.Conversation = append([]model.Message(nil), messages[start:end]...)
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

	index, err := service.components.sessionQuery.SessionInputIndex("index-session")
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

	index, err := service.components.sessionQuery.SessionInputIndex("empty-session")
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
	if _, err := service.components.sessionQuery.SessionInputIndex("   "); err == nil {
		t.Fatal("empty session ID must fail")
	}
}

// TestSessionInputIndexTruncatesSummary 摘要长度有界（rune 截断 + 原文长度留痕）。
func TestSessionInputIndexTruncatesSummary(t *testing.T) {
	store := newPagedSessionStore("summary-session", 0)
	long := strings.Repeat("长", residency.InputIndexSummaryLimit+140)
	store.setPublished(
		model.Message{ID: "m1", Role: "user", Content: long, CreatedAt: time.Now()},
		model.Message{ID: "m2", Role: "assistant", Content: "回答", CreatedAt: time.Now()},
	)
	service := inputIndexService(t, store)

	index, err := service.components.sessionQuery.SessionInputIndex("summary-session")
	if err != nil {
		t.Fatalf("SessionInputIndex: %v", err)
	}
	if len(index.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(index.Items))
	}
	row := index.Items[0]
	summary := []rune(row.Summary)
	if len(summary) != residency.InputIndexSummaryLimit+1 {
		t.Fatalf("summary runes = %d, want %d（上限 + 省略号）", len(summary), residency.InputIndexSummaryLimit+1)
	}
	if !strings.HasSuffix(row.Summary, "…") {
		t.Fatalf("summary = %q, want ellipsis suffix", row.Summary)
	}
	if row.Chars != len([]rune(long)) {
		t.Fatalf("chars = %d, want %d（原文长度）", row.Chars, len([]rune(long)))
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
		model.Message{ID: "m1", Role: "user", Content: "真正的输入", CreatedAt: time.Now()},
		model.Message{ID: "m2", Role: "assistant", Content: "回答", CreatedAt: time.Now()},
		model.Message{ID: "m3", Role: "tool", Content: "工具调用", CreatedAt: time.Now()},
		model.Message{ID: "m4", Role: "tool_result", Content: "工具结果", CreatedAt: time.Now()},
		model.Message{ID: "m5", Role: "system", Content: "系统行", CreatedAt: time.Now()},
		model.Message{ID: "m6", Role: "user", Content: context_runtime.TaskContextCheckpointPrefix + "\n内部检查点", CreatedAt: time.Now()},
		model.Message{ID: "m7", Role: "user", Content: session_runtime.SessionArchiveResumePrefix + "\n恢复摘要", CreatedAt: time.Now()},
		model.Message{ID: "m8", Role: "user", Content: "   \n  ", CreatedAt: time.Now()},
		model.Message{ID: "m9", Role: "user", Content: envelope, CreatedAt: time.Now()},
		model.Message{ID: "m10", Role: "user", Content: "第二条真输入", CreatedAt: time.Now()},
	)
	service := inputIndexService(t, store)

	index, err := service.components.sessionQuery.SessionInputIndex("filter-session")
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

	index, err := service.components.sessionQuery.SessionInputIndex("record-session")
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
