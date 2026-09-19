package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// windowEvents 构造 units 个已定稿轮次（user+assistant），每轮 unitTokens 个
// token（事件按半拆分计数），事件序号从 1 连续编号、消息号 message-N。
func windowEvents(units, unitTokens int) []model.TranscriptEvent {
	events := make([]model.TranscriptEvent, 0, units*2)
	half := unitTokens / 2
	for index := 0; index < units; index++ {
		events = append(events,
			model.TranscriptEvent{
				Seq: uint64(index*2 + 1), Role: "user", Content: "question",
				TokenCount: half, MessageID: "message-" + itoa(index*2+1),
			},
			model.TranscriptEvent{
				Seq: uint64(index*2 + 2), Role: "assistant", Content: "answer",
				TokenCount: half, MessageID: "message-" + itoa(index*2+2),
			},
		)
	}
	return events
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestTranscriptTailWindowReportsRetainedBoundary：窗口边界 = 保留段首个事件
// 的下标（events[:start] 是窗口外前缀，events[start:] 是保留窗口），且与
// TranscriptTailHistory 的消息视图一致。保留窗口（压缩后）按 token 预算截断。
func TestTranscriptTailWindowReportsRetainedBoundary(t *testing.T) {
	// 每轮 30000 token，预算 100084：4 轮 = 120000 超预算 → 只保留 3 轮
	// = 90000 token = 6 条事件。
	events := windowEvents(5, 30_000)
	history, start := TranscriptTailWindow(events, 100_084, 4)
	if len(history) != 6 {
		t.Fatalf("retained messages = %d, want 6 (3 units)", len(history))
	}
	if start != 4 {
		t.Fatalf("window start = %d, want 4 (被压前缀 = 前 2 轮 4 条事件)", start)
	}
	if prefix := events[:start]; len(prefix) != 4 || prefix[0].Seq != 1 || prefix[3].Seq != 4 {
		t.Fatalf("被压前缀 = %#v, want seq 1..4", prefix)
	}
	if tail := events[start:]; len(tail) != 6 || tail[0].Seq != 5 {
		t.Fatalf("保留窗口 = %#v, want seq 5..10", tail)
	}
	if got := TranscriptTailHistory(events, 100_084, 4); len(got) != len(history) {
		t.Fatalf("TranscriptTailHistory 与窗口边界不一致：%d vs %d", len(got), len(history))
	}
}

// TestTranscriptTailWindowRecordsUnitCapBoundary：单元上限比 token 预算更紧时
// （maxUnits 生效），边界同样落在完整协议单元起始处。
func TestTranscriptTailWindowRecordsUnitCapBoundary(t *testing.T) {
	events := windowEvents(6, 20_000)
	history, start := TranscriptTailWindow(events, 100_084, 4)
	if len(history) != 8 {
		t.Fatalf("retained messages = %d, want 8 (4 units)", len(history))
	}
	if start != 4 {
		t.Fatalf("window start = %d, want 4 (被压前缀 = 前 2 轮)", start)
	}
}

// TestTranscriptTailWindowDegradesToNewestUnit：单个最新单元自身超预算时仍保留
// 它（不失忆），边界指向该单元起点。
func TestTranscriptTailWindowDegradesToNewestUnit(t *testing.T) {
	events := windowEvents(3, 40_000)
	history, start := TranscriptTailWindow(events, 1_000, 0)
	if len(history) != 2 || start != 4 {
		t.Fatalf("history=%d start=%d, want 2/4 (只保留最新轮)", len(history), start)
	}
	if empty, start := TranscriptTailWindow(nil, 1_000, 0); len(empty) != 0 || start != 0 {
		t.Fatalf("空 transcript：history=%d start=%d, want 0/0（未保留任何事件）", len(empty), start)
	}
}

// TestTranscriptPrefixRangeRecordsMessageNumbers：压缩区间记录消息号与事件
// 序号（从哪到哪），供压缩记录直接落库，不再事后推算。
func TestTranscriptPrefixRangeRecordsMessageNumbers(t *testing.T) {
	events := windowEvents(3, 1_000)
	got := TranscriptPrefixRange(events, 4)
	if got.EventFrom != 1 || got.EventTo != 4 {
		t.Fatalf("事件序号区间 = [%d,%d], want [1,4]", got.EventFrom, got.EventTo)
	}
	if got.MessageFrom != "message-1" || got.MessageTo != "message-4" {
		t.Fatalf("消息号区间 = [%q,%q], want [message-1,message-4]", got.MessageFrom, got.MessageTo)
	}
	if got.Empty() {
		t.Fatal("有内容的区间不应报告为空")
	}
	if empty := TranscriptPrefixRange(events, 0); !empty.Empty() {
		t.Fatalf("空区间应报告为空：%+v", empty)
	}
	// Seq 为 0 的合成事件不参与事件序号区间，但消息号照常记录。
	synthetic := []model.TranscriptEvent{{Seq: 0, Role: "system", Content: "injected", MessageID: "message-9"}}
	only := TranscriptPrefixRange(synthetic, 1)
	if only.EventFrom != 0 || only.EventTo != 0 || only.MessageFrom != "message-9" {
		t.Fatalf("合成事件区间 = %+v, want 只记消息号", only)
	}
}
