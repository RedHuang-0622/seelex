package core

// 本文件复现「加载着加载着就只剩冷加载内容，尾部没了；一出个工具结果又像是恢复
// 了正常」这条现场缺陷（2026-09-25）。
//
// 现场形状（生产装配）：一个会话回合里，可见行先进内存窗口（append / 流式 /
// 工具结果），durable record 要到 runChat 收尾的 PersistCurrentSession 才落盘
// （chat.go:271）；而存储侧冷读面的右界是**发布点**（sessionstore 以
// head.LastSeq 为读者闸门，见 message_rows.go decodePublishedRows）。于是运行中
// 的回合里「内存已可见总数 > 磁盘已发布数」是常态，差额就是本轮尚未落盘的行。
//
// 三个加载方向的入口（LoadMoreHistory / LoadLatestHistory / 热挂载后的
// reloadSessionContent）都只走冷读面，且 installVisibleHistory 把冷读结果当成
// 全部事实：
//  1. 整窗替换把窗口里「磁盘给不出」的尾部行直接删掉，可见正文只剩已发布段
//     ——即「只能看见冷加载内容、后缀没了」；
//  2. TotalMessages 被改写成磁盘已发布数（倒退），窗口因此被误判为「已贴尾」，
//     下一条工具结果就被塞进一个中间缺段的窗口——「一出工具结果又正常了」其实
//     是把洞盖住；
//  3. 冷读一页短于请求（读到发布点就没了）时，offset 仍按调用方估计值写，
//     窗口内容与窗口起点从此各说各话。

import (
	"fmt"
	"strings"
	"testing"
)

// beginInFlightTurn 造「本轮已进内存窗口、尚未落盘」的在飞尾部：store 只物理
// append（不推进发布点），service 经真实 append 路径写可见窗口。
func beginInFlightTurn(t *testing.T, service *Service, store *pagedSessionStore, count int) []string {
	t.Helper()
	contents := make([]string, 0, count)
	for index := 0; index < count; index++ {
		content := fmt.Sprintf("inflight-%d", index)
		contents = append(contents, content)
		store.appendDraft("assistant", content)
		service.ViewMu.Lock()
		service.appendMessageLocked("assistant", content, nil)
		service.ViewMu.Unlock()
	}
	return contents
}

// assertVisibleWindow 断言快照里的「窗口起点 + 窗口内容 + 已可见总数」。
func assertVisibleWindow(t *testing.T, label string, snapshot Snapshot, wantOffset int, wantContents []string, wantTotal int) {
	t.Helper()
	if got := snapshot.HistoryOffset; got != wantOffset {
		t.Fatalf("%s: history_offset = %d, want %d（visible=%v total=%d）", label, got, wantOffset, visibleContents(snapshot), snapshot.TotalMessages)
	}
	got := visibleContents(snapshot)
	if strings.Join(got, ",") != strings.Join(wantContents, ",") {
		t.Fatalf("%s: visible = %v, want %v（offset=%d total=%d has_more=%v）",
			label, got, wantContents, snapshot.HistoryOffset, snapshot.TotalMessages, snapshot.HasMoreHistory)
	}
	if snapshot.TotalMessages != wantTotal {
		t.Fatalf("%s: total_messages = %d, want %d", label, snapshot.TotalMessages, wantTotal)
	}
}

// TestLoadLatestHistoryKeepsInFlightTail 红灯 1（缺陷 1）：回合进行中，内存窗口
// 已经贴着有效尾部（含尚未落盘的行），「回到最新」就该保持这一窗——它已经在
// 最新处。今天它先按内存总数估一个起点去冷读，读回的是更旧的已发布一页，于是
// 把在飞尾部整窗换掉。
func TestLoadLatestHistoryKeepsInFlightTail(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	inFlight := beginInFlightTurn(t, service, store, 4)
	tail := append(wantRange(18, 2), inFlight...)
	assertVisibleWindow(t, "在飞窗口基线", service.Snapshot(), 18, tail, 24)

	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("load latest history: %v", err)
	}
	assertVisibleWindow(t, "回到最新不得丢掉未落盘尾部", service.Snapshot(), 18, tail, 24)
}

// TestColdPageReadDoesNotRegressVisibleTotal 红灯 2（缺陷 2）：「加载更早」滑走
// 一页后，已可见总数不得从内存里的 24 退回到磁盘发布点 20——总数是前端
// 「下方还有 N 条新内容」的唯一依据，倒退即谎报已经到底。
func TestColdPageReadDoesNotRegressVisibleTotal(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	beginInFlightTurn(t, service, store, 4)

	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	snapshot := service.Snapshot()
	assertVisibleWindow(t, "加载更早一页", snapshot, 12, wantRange(12, 6), 24)
	if below := snapshot.TotalMessages - (snapshot.HistoryOffset + len(visibleContents(snapshot))); below != 6 {
		t.Fatalf("窗口下方仍有 %d 行未可见，want 6（已发布未入窗 2 行 + 在飞 4 行）", below)
	}
}

// TestToolResultAfterPagingDoesNotPunchHole 红灯 3（缺陷 1+2 的后果）：内存窗口
// 跨在发布点上、右界只到 21（在飞 1 行）而有效总数 24 时，窗口并未贴尾；新到达
// 的消息只能记进总数，不得插进窗口——插进去就是在列表中间缺一段（用户视角：
// 内容闪没了，一出工具结果又「自己好了」）。
func TestToolResultAfterPagingDoesNotPunchHole(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	inFlight := beginInFlightTurn(t, service, store, 4)
	// 半页翻页：窗口落在 [15,21) —— 15..19 已发布，20 起在飞，跨发布点且不贴尾。
	if err := service.LoadMoreHistory(3); err != nil {
		t.Fatalf("load more history (half page): %v", err)
	}
	halfPage := append(wantRange(15, 5), inFlight[:1]...)
	assertVisibleWindow(t, "跨发布点的半页窗口", service.Snapshot(), 15, halfPage, 24)

	store.appendDurable("tool_result", "tool-result")
	service.ViewMu.Lock()
	service.appendMessageLocked("tool_result", "tool-result", nil)
	service.ViewMu.Unlock()

	snapshot := service.Snapshot()
	assertVisibleWindow(t, "翻页后的新行只推进总数", snapshot, 15, halfPage, 25)
	for _, content := range visibleContents(snapshot) {
		if content == "tool-result" {
			t.Fatal("未贴有效尾的窗口被塞进了新行（窗口中间缺一段）")
		}
	}
}

// TestLoadLatestHistorySplicesStraddlingHotTail 红灯 4（缺陷 1+3）：内存窗口正好
// 跨在发布点上时，「回到最新」必须把冷读页与内存里发布点之后的行拼成连续一窗，
// 而不是只装冷读页（在飞行被丢掉），也不是按估计值写 offset（窗口与起点脱节）。
// 拼不出来的那段（既不在窗口里也还没落盘）本轮只能等提交后回读——断言里以
// 「 servable 右界 = 21」为准。
func TestLoadLatestHistorySplicesStraddlingHotTail(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	inFlight := beginInFlightTurn(t, service, store, 4)
	if err := service.LoadMoreHistory(3); err != nil {
		t.Fatalf("load more history (half page): %v", err)
	}
	halfPage := append(wantRange(15, 5), inFlight[:1]...)
	assertVisibleWindow(t, "跨发布点的半页窗口", service.Snapshot(), 15, halfPage, 24)

	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("load latest history: %v", err)
	}
	snapshot := service.Snapshot()
	// 冷读到发布点 20 为止，内存能把右界再往前带到 21：窗口 = [15,21)。
	assertVisibleWindow(t, "回到最新拼回在飞尾部", snapshot, 15, halfPage, 24)
	if below := snapshot.TotalMessages - (snapshot.HistoryOffset + len(visibleContents(snapshot))); below != 3 {
		t.Fatalf("窗口下方仍有 %d 行未可见，want 3（尚未落盘的在飞行）", below)
	}
}

// TestInFlightTailReturnsAfterCommit 收口断言：在飞行落盘（发布点推进）之后，
// 「回到最新」纯靠冷读就能给出完整尾部——分页不丢数据，只丢过一次显示。
func TestInFlightTailReturnsAfterCommit(t *testing.T) {
	defer withHistoryWindow(6)()

	service, store := openLongSession(t, 6, 20)
	inFlight := beginInFlightTurn(t, service, store, 4)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history (second page): %v", err)
	}

	store.publishDraftTail()
	if err := service.LoadLatestHistory(); err != nil {
		t.Fatalf("load latest history after commit: %v", err)
	}
	tail := append(wantRange(18, 2), inFlight...)
	assertVisibleWindow(t, "落盘后回到最新", service.Snapshot(), 18, tail, 24)
}
