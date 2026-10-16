package session

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// P21-2 域级用例：实时流分发器的窗口归一化、订阅面（历史快照 + 实时投递 + 幂等取消）、
// 广播窗口上限与分页读法。装配层的端到端用例（`runtime_live_page_test.go` /
// `node_assistant_live_test.go`）仍走宿主转发面，一行未改。

func TestNormalizeLiveWindow(t *testing.T) {
	cases := []struct{ configured, want int }{
		{0, LiveHistoryCap},
		{-1, LiveHistoryMinWindow},
		{10, LiveHistoryMinWindow},
		{49, LiveHistoryMinWindow},
		{50, 50},
		{4096, 4096},
	}
	for _, item := range cases {
		if got := NormalizeLiveWindow(item.configured); got != item.want {
			t.Errorf("NormalizeLiveWindow(%d) = %d, want %d", item.configured, got, item.want)
		}
	}
}

func TestLiveDispatcherWindowFollowsNormalization(t *testing.T) {
	if got := NewLiveDispatcher(0, LiveDispatcherDeps{}).Window(); got != LiveHistoryCap {
		t.Fatalf("未配置窗口 = %d, want %d", got, LiveHistoryCap)
	}
	if got := NewLiveDispatcher(10, LiveDispatcherDeps{}).Window(); got != LiveHistoryMinWindow {
		t.Fatalf("小于下限的窗口 = %d, want %d", got, LiveHistoryMinWindow)
	}
	if got := (*LiveDispatcher)(nil).Window(); got != LiveHistoryCap {
		t.Fatalf("nil 分发器窗口 = %d, want %d", got, LiveHistoryCap)
	}
}

func TestLiveDispatcherSubscribeReplaysHistoryAndDeliversLive(t *testing.T) {
	dispatcher := NewLiveDispatcher(50, LiveDispatcherDeps{})
	defer dispatcher.Stop()
	nodeID := "node-live"

	// 先播两条（无人订阅也要进窗口：回放面靠它）。
	dispatcher.Broadcast(dto.SubagentLiveEvent{NodeID: nodeID, Kind: "stage", At: time.Now()})
	dispatcher.Broadcast(dto.SubagentLiveEvent{NodeID: nodeID, Kind: "tool", At: time.Now()})

	history, live, cancel, err := dispatcher.Subscribe(nodeID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("订阅前的事件必须回放：%+v", history)
	}
	// 订阅之后的新事件走实时通道。
	dispatcher.Broadcast(dto.SubagentLiveEvent{NodeID: nodeID, Kind: "assistant", At: time.Now()})
	select {
	case event := <-live:
		if event.Kind != "assistant" {
			t.Fatalf("实时通道收到 %q, want assistant", event.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("实时通道没有收到订阅之后的事件")
	}
	// 取消幂等：重复调用不 panic，通道关闭。
	cancel()
	cancel()
	if _, ok := <-live; ok {
		t.Fatal("取消后订阅通道应已关闭")
	}
	// 取消后继续广播不该 panic（订阅表已摘除）。
	dispatcher.Broadcast(dto.SubagentLiveEvent{NodeID: nodeID, Kind: "stage", At: time.Now()})
}

func TestLiveDispatcherHistoryPageKeepsWindowBound(t *testing.T) {
	const nodeID = "node-paged"
	dispatcher := NewLiveDispatcher(50, LiveDispatcherDeps{})
	defer dispatcher.Stop()
	for turn := 0; turn < 70; turn++ {
		dispatcher.Broadcast(dto.SubagentLiveEvent{
			NodeID: nodeID, Kind: "stage", At: time.Now(),
			Stage: &dto.NodeStageLog{Stage: "turn", NodeID: nodeID, Turn: turn},
		})
	}
	page := dispatcher.HistoryPage(nodeID, 0, 20)
	if page.Total != 50 {
		t.Fatalf("窗口上限没生效：total = %d, want 50", page.Total)
	}
	if len(page.Events) != 20 || !page.HasMore {
		t.Fatalf("第一页 = %d 条, hasMore=%v, want 20/true", len(page.Events), page.HasMore)
	}
	// 越界页：空页、HasMore=false、offset 原样回带。
	beyond := dispatcher.HistoryPage(nodeID, 50, 10)
	if len(beyond.Events) != 0 || beyond.HasMore || beyond.Offset != 50 {
		t.Fatalf("越界页 = %+v, want 空页 + hasMore=false + offset=50", beyond)
	}
	// limit 收敛到窗口；offset < 0 → 0；未知节点 → 空页。
	clamped := dispatcher.HistoryPage(nodeID, -5, 4096)
	if clamped.Offset != 0 || clamped.Limit != 50 {
		t.Fatalf("归一化失败：offset=%d limit=%d, want 0/50", clamped.Offset, clamped.Limit)
	}
	if empty := dispatcher.HistoryPage("", 0, 10); len(empty.Events) != 0 || empty.Total != 0 {
		t.Fatalf("空 nodeID 应给空页：%+v", empty)
	}
}

func TestLiveDispatcherRecordAssistantNeedsSubscriber(t *testing.T) {
	dispatcher := NewLiveDispatcher(50, LiveDispatcherDeps{})
	defer dispatcher.Stop()
	// 没有订阅者时（分发器未启动）不积累：窗口保持空。
	dispatcher.RecordAssistant("node-idle", "no subscriber")
	if page := dispatcher.HistoryPage("node-idle", 0, 10); page.Total != 0 {
		t.Fatalf("无人订阅时不应积累实时面：total = %d", page.Total)
	}
	// 有订阅者之后（Start 由 Subscribe 触发）正文增量进窗口。
	_, _, cancel, err := dispatcher.Subscribe("node-busy")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()
	dispatcher.RecordAssistant("node-busy", "hello")
	// 正文增量经**统一通道 → 广播循环**才进窗口，因此这里等一小段（同生产语义：
	// 事件到达即投递，但不保证调用返回时已被循环消费）。
	deadline := time.Now().Add(2 * time.Second)
	page := dispatcher.HistoryPage("node-busy", 0, 10)
	for page.Total == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		page = dispatcher.HistoryPage("node-busy", 0, 10)
	}
	if page.Total != 1 {
		t.Fatalf("订阅后正文增量应进窗口：total = %d", page.Total)
	}
	if page.Events[0].Kind != "assistant" || page.Events[0].Assistant == nil ||
		page.Events[0].Assistant.Text != "hello" {
		t.Fatalf("正文增量事件形状不对：%+v", page.Events[0])
	}
	// 空白增量不入窗（避免运行路径白跑）。
	dispatcher.RecordAssistant("node-busy", "   ")
	time.Sleep(50 * time.Millisecond)
	if again := dispatcher.HistoryPage("node-busy", 0, 10); again.Total != 1 {
		t.Fatalf("空白增量不应入窗：total = %d", again.Total)
	}
}

func TestLiveDispatcherStageAndToolEventProjection(t *testing.T) {
	stage := StageLiveEvent(dto.NodeStageLog{NodeID: "node-a", Stage: "thinking", At: time.Unix(7, 0)})
	if stage.Kind != "stage" || stage.Stage == nil || stage.Stage.Stage != "thinking" {
		t.Fatalf("阶段事件投影不对：%+v", stage)
	}
	if !stage.At.Equal(time.Unix(7, 0)) {
		t.Fatalf("阶段事件应沿用日志时间戳：%v", stage.At)
	}
	tool := ToolLiveEvent(SubagentToolEvent{
		ID: "tool-1", NodeID: "node-a", Name: "bash", Status: dto.ToolEventRunning,
		StartedAt: time.Unix(9, 0), Duration: 1500 * time.Millisecond,
	})
	if tool.Kind != "tool" || tool.Tool == nil || tool.Tool.Name != "bash" {
		t.Fatalf("工具事件投影不对：%+v", tool)
	}
	if tool.Tool.DurationMS != 1500 {
		t.Fatalf("Duration → DurationMS 换算不对：%d", tool.Tool.DurationMS)
	}
}
