package core

// 红灯复现（用户现象）：**冷加载之后点「回到最新」，看不到最新内容**。
//
// 用户形状（现场）：
//  1. 长会话已落盘，用户切回它 → 冷加载（resumeSessionCold）只装上「尾部窗口」；
//  2. 用户向上翻更早的历史（LoadMoreHistory）读旧账；
//  3. 读完点顶部「回到最新」按钮（前端 returnToLatest → LoadLatestHistory），
//     期望窗口回到最新一页，实际窗口够不到尾：history_offset + 可见条数 <
//     total_messages 恒成立 → 前端 protocol.js:95 的 historyWindowed 分支一直
//     生效（新消息被 reducer 丢掉，界面「长期看不到最新的消息」），后端
//     View.SessionViewBrowsingHistoryLocked 也一直是 true（流式增量、推理挂接、
//     工具结果写回三条路径全被跳过）——按钮点多少次都不解决；尾窗整段被内部行
//     占掉时更直接命中 installVisibleHistory 的 len(page.rows) == 0 分支
//     （session_history.go:780-783），窗口原样留在上一页。
//
// 根因（本文件钉住的是**下标空间混用**）：两个空间的分工与对位规则见 2026-09-29 修复
// 说明（`docs/devlog/2026-09-29-history-paging-visible-space.md`）；下文行号是红灯期
// 快照，改后已随代码漂移，语义以函数名为准。
//   - 冷加载建立的基线在**可见空间**：total = len(RecordConversation(record))
//     （session_history.go:362）、窗口 = RecordConversationTail(record, window)
//     （:469）——RecordConversation 剔掉内部行（role=system / 内部标记，
//     session_runtime/archive.go:482-507）；
//   - 分页读回（LoadMoreHistory / LoadLatestHistory → loadConversationPage，
//     session_history.go:716-739）拿到的 count 却是**未过滤空间**的总数：存储侧
//     派生会话由 message 行直接展开（sessionstore/json_layout.go:295
//     derivedConversationMessages 不剔内部行；internal/adapters/
//     session_workspace_ports.go:921 storeTranscriptEvents 原样落行——
//     application/core/session_archive_test.go:325 就是一条落盘的
//     task-context checkpoint 内部行）；
//   - installVisibleHistory（session_history.go:758-825）把两个空间混着算：
//     TotalMessages 取 max(diskTotal, 内存总数)（diskTotal 是未过滤空间），窗口
//     起点却按 pageStart（未过滤下标）+ len(page.rows)（已过滤行数）推
//     （:786/:806）——尾窗里有多少条内部行，窗口右界就少多少格，于是**永远贴不到
//     尾**，用户被永久留在回看态。
//
// 夹具形状的生产性：pagedSessionStore 的冷读面（record 与 conversation 区间读）
// 与生产同一口径——按发布点闸门给出**同一份未过滤行**；本文件插入的内部行用的
// 是生产真实标记（context_runtime.TaskContextCheckpointPrefix），正是
// service_assembler.go:141-143 交给 RecordConversation 剔除的那一类。

import (
	"fmt"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// internalCheckpointRow 造一条**已落盘**的内部行（上下文 checkpoint 标记）。
// 存储层把它当普通 conversation 行（sessionstore/json_layout.go:295
// derivedConversationMessages），可见会话必须剔除它（session_runtime/archive.go
// isInternalConversationMessage → context_runtime.IsTaskContextCheckpoint）。
func internalCheckpointRow(index int) Message {
	return Message{
		ID:      fmt.Sprintf("internal-%d", index),
		Role:    "user",
		Content: fmt.Sprintf("%s{\"version\":8,\"checkpoint\":%d}", context_runtime.TaskContextCheckpointPrefix, index),
	}
}

// seedDurableRows 铺一整份**已发布**行：visible 条 durable-N + trailingInternal
// 条内部行（内部行落在序列末尾，与「回合收尾先落 checkpoint 再落正文」的现场
// 同形）。
func seedDurableRows(store *pagedSessionStore, visible, trailingInternal int) {
	rows := make([]Message, 0, visible+trailingInternal)
	for index := 0; index < visible; index++ {
		role := "assistant"
		if index%2 == 0 {
			role = "user"
		}
		rows = append(rows, Message{
			ID:        fmt.Sprintf("message-%d", index+1),
			Role:      role,
			Content:   fmt.Sprintf("durable-%d", index),
			CreatedAt: time.Unix(int64(index), 0),
		})
	}
	for index := 0; index < trailingInternal; index++ {
		rows = append(rows, internalCheckpointRow(index))
	}
	store.setPublished(rows...)
}

// coldLoadLongSession 冷加载一个「durable 20 条 + 末尾内部行」的长会话（窗口 6），
// 并断言冷加载基线本身是对的（窗口 = durable-14..19、total = 20、不在回看态）。
func coldLoadLongSession(t *testing.T, trailingInternal int) (*Service, *pagedSessionStore) {
	t.Helper()
	store := newPagedSessionStore("long-session", 0)
	seedDurableRows(store, 20, trailingInternal)
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := service.ResumeSession("long-session"); err != nil {
		t.Fatalf("冷加载会话: %v", err)
	}
	baseline := service.Snapshot()
	if baseline.TotalMessages != 20 || baseline.HistoryOffset != 14 {
		t.Fatalf("冷加载基线 = offset %d / total %d，want offset 14 / total 20（可见空间）",
			baseline.HistoryOffset, baseline.TotalMessages)
	}
	if got := visibleContents(baseline); len(got) != 6 || got[0] != "durable-14" || got[5] != "durable-19" {
		t.Fatalf("冷加载尾窗 = %v，want durable-14..durable-19", got)
	}
	return service, store
}

// assertWindowAtTail 断言「回到最新」之后的三件事：窗口贴尾（前端
// historyWindowed 判据）、尾部一条就是最新一条、回看态已结束（否则流式增量、
// 推理挂接、工具结果写回继续被跳过）。
func assertWindowAtTail(t *testing.T, service *Service, label string) {
	t.Helper()
	snapshot := service.Snapshot()
	visible := visibleContents(snapshot)
	if snapshot.HistoryOffset+len(visible) < snapshot.TotalMessages {
		t.Fatalf("%s：窗口仍够不到尾（offset=%d visible=%d total=%d，可见=%v）——前端 historyWindowed 会一直为真，"+
			"此后新消息被 reducer 丢掉（「看不到最新内容」）",
			label, snapshot.HistoryOffset, len(visible), snapshot.TotalMessages, visible)
	}
	if last := visible[len(visible)-1]; last != "durable-19" {
		t.Fatalf("%s：窗口末条 = %q，want durable-19（最新一条必须能回到视野）", label, last)
	}
	if service.components.view.SessionViewBrowsingHistoryLocked("long-session") {
		t.Fatalf("%s：会话仍被判成「回看中」——流式增量 / 推理挂接 / 工具结果写回会被这条判据跳过", label)
	}
}

// TestReproColdLoadThenReturnToLatestReachesTail 红灯 1：冷加载后翻一页再点
// 「回到最新」，窗口必须回到尾部；今天尾窗里的 1 条内部行让右界少 1 格，
// offset+visible 永远差 total 一格（20 < 21），回看态无法解除。
func TestReproColdLoadThenReturnToLatestReachesTail(t *testing.T) {
	defer withHistoryWindow(6)()

	service, _ := coldLoadLongSession(t, 1)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("加载更早一页: %v", err)
	}
	assertRange(t, "回看更早一页", service.Snapshot(), 8, 6)

	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("回到最新: %v", err)
	}
	assertWindowAtTail(t, service, "冷加载后回到最新")
}

// TestReproColdLoadReturnToLatestWhenTailWindowIsInternalRows 红灯 2：尾窗那一段
// 全部被内部行占掉时（回合收尾刚落 checkpoint / 技能轮次标记），冷读尾页一条可见
// 行都读不出来——installVisibleHistory 的 len(page.rows) == 0 分支会**保留原窗口
// 只校正总数**（session_history.go:780-783），用户点「回到最新」看到的还是上一页
// 的旧内容，且再也回不到最新一页。
func TestReproColdLoadReturnToLatestWhenTailWindowIsInternalRows(t *testing.T) {
	defer withHistoryWindow(6)()

	service, _ := coldLoadLongSession(t, 6)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("加载更早一页: %v", err)
	}
	assertRange(t, "回看更早一页", service.Snapshot(), 8, 6)

	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("回到最新: %v", err)
	}
	assertWindowAtTail(t, service, "尾窗被内部行占掉后回到最新")
}
