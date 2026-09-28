package task_context

import (
	"strings"
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

// TestTranscriptTailWindowByUsesInjectedEstimator：选窗必须按注入的估算器
// （压缩判据/保留窗口同款）计价，而不是事件自带的记录值。校准因子变化后两者
// 会漂移：按记录值裁出的窗口在重新估算时会“膨胀”回软阈值以上，导致长会话
// 每回合都判成越线、每回合重压。
func TestTranscriptTailWindowByUsesInjectedEstimator(t *testing.T) {
	// 5 轮 × 30000 记录值；注入 2× 估算后每轮按 60000 计。
	events := windowEvents(5, 30_000)
	history, start := TranscriptTailWindowBy(events, 100_000, 4, func(unit []model.TranscriptEvent) int {
		return recordedUnitTokens(unit) * 2
	})
	if len(history) != 2 {
		t.Fatalf("retained messages = %d, want 2 (1 unit × 2 messages)", len(history))
	}
	if start != 8 {
		t.Fatalf("window start = %d, want 8 (只保留最新 1 轮)", start)
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

// TestTranscriptTailWindowKeepsRoundStartAtBoundary：保留窗口的边界必须落在
// **轮次起点**，绝不能落在一轮中间。
//
// 现场（用户报告）：压缩之后模型「丢了目标」——它看得见自己刚才干到哪一步，
// 却看不见用户到底在要求什么。根因是边界被判在一轮中间：该轮的用户提问被判进
// 压缩区间（原文折进帧摘要），它的续写留在保留窗口里。于是压缩记录的区间终点
// 越过了一条用户提问行（"界限的判断包含了用户那一轮的提问"），而保留下来的
// 续写失去了它的来由。
//
// 一轮被拆成多段是常态而不是异常：轮内的技能正文/内部材料注入会把一轮切开，
// 每段各自都是**协议合法**的单元（所以单元级看一切正常，问题只在"轮次"这一层），
// 半途中断的工具链同理。边界因此不能只看"单元起点"，还要看"轮次起点"。
func TestTranscriptTailWindowKeepsRoundStartAtBoundary(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "第一轮提问", TokenCount: 10, MessageID: "message-1"},
		{Seq: 2, Role: "assistant", Content: "第一轮答复", TokenCount: 10, MessageID: "message-2"},
		{Seq: 3, Role: "user", Content: "第二轮提问（本轮目标）", TokenCount: 10, MessageID: "message-3"},
		// 该轮的第一个工具链在这里断了（结果缺失）→ 与提问同段收尾。
		{Seq: 4, Role: "assistant", ToolCalls: []model.TranscriptToolCall{{ID: "c1", Name: "read"}}, TokenCount: 10, MessageID: "message-4"},
		// 轮内注入的技能正文（provider role = system）：把这一轮切成多段。
		{Seq: 5, Role: "user", Content: ActiveSkillMarker + "\n## Trusted Active Skill: review\nbody", TokenCount: 10, MessageID: "message-5"},
		// 该轮提问之后被保留的续写段。
		{Seq: 6, Role: "assistant", ToolCalls: []model.TranscriptToolCall{{ID: "c2", Name: "bash"}}, TokenCount: 10, MessageID: "message-6"},
		{Seq: 7, Role: "tool", ToolCallID: "c2", Name: "bash", Content: "结果", TokenCount: 10, MessageID: "message-7"},
	}
	// 预算 20 只装得下最新一段（seq 6..7 的续写）：边界必须被推回该轮的起点
	// （seq 3 的提问），而不是停在 seq 6 —— 停在 seq 6 就等于把提问判给了压缩。
	history, start := TranscriptTailWindowBy(events, 20, 0, recordedUnitTokens)
	if start != 2 {
		t.Fatalf("窗口边界 = %d, want 2（第二轮提问的下标）：边界落在了一轮中间 —— "+
			"该轮提问会被判进压缩区间，而它的续写留在窗口里（模型从此失去目标）", start)
	}
	if prefix := events[:start]; len(prefix) != 2 || prefix[len(prefix)-1].Role == "user" {
		t.Fatalf("被压前缀 = %#v, want 以非用户行收尾（压缩区间终点不得落在提问行上）", prefix)
	}
	if len(history) == 0 || !strings.Contains(history[0].Content, "第二轮提问（本轮目标）") {
		t.Fatalf("保留窗口必须以该轮的用户提问开头，实际 = %#v", history)
	}
}

// TestTranscriptTailWindowKeepsRoundStartAcrossMaterialInjection：轮内的**内部
// 材料行**（Role=user + WireMaterial，provider role 映射为 system）不是轮次起点。
//
// 现场（用户报告）：压缩之后模型「丢了目标」——界限的判断把**用户这一轮的提问**
// 判进了压缩区间（"界限的判断包含了用户下一轮的提问"）。根因是它把轮内的内部材料
// 行误当成新的轮次起点：材料行在提问**之后**，窗口边界就停在材料行上，而这一轮
// 真正的提问（在材料行之前）被折走，保留窗口里只剩"续写"，模型从此不知道用户
// 要什么。
//
// 判据与同包 sessionMaintenanceObjective 的"什么算真实用户输入"必须同源：那里
// 已经用 `Role != "user" || WireMaterial || isActiveSkillEvent` 排除材料行。
func TestTranscriptTailWindowKeepsRoundStartAcrossMaterialInjection(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Kind: model.TranscriptEventKindUserInput, Content: "第一轮提问", TokenCount: 10, MessageID: "message-1"},
		{Seq: 2, Role: "assistant", Kind: model.TranscriptEventKindLLM, Content: "第一轮回答", TokenCount: 10, MessageID: "message-2"},
		{Seq: 3, Role: "user", Kind: model.TranscriptEventKindUserInput, Content: "第二轮提问（本轮目标）", TokenCount: 10, MessageID: "message-3"},
		// 轮内注入的内部材料：给模型看的检查点/状态材料（生产方置 wire_material）。
		// 正文不带 `<!-- seelex:` 前缀时，Kind 会被归类成 user_input —— 单看 Kind
		// 分不出"材料"与"提问"，只有 WireMaterial 能。
		{Seq: 4, Role: "user", Kind: model.TranscriptEventKindUserInput, WireMaterial: true,
			Content: "任务 active 状态材料", TokenCount: 10, MessageID: "message-4"},
		{Seq: 5, Role: "assistant", ToolCalls: []model.TranscriptToolCall{{ID: "c1", Name: "read"}}, TokenCount: 10, MessageID: "message-5"},
		{Seq: 6, Role: "tool", ToolCallID: "c1", Name: "read", Content: "结果", TokenCount: 10, MessageID: "message-6"},
	}
	// 预算 20 只装得下最新一段（seq 5..6）：边界必须被推回该轮的起点（seq 3），
	// 不能停在材料行（seq 4）—— 停在材料行就等于把提问判给了压缩区间。
	history, start := TranscriptTailWindowBy(events, 20, 0, recordedUnitTokens)
	if start != 2 {
		t.Fatalf("窗口边界 = %d, want 2（第二轮提问的下标）：边界停在了轮内材料行上，"+
			"该轮提问会被判进压缩区间（模型从此失去目标）", start)
	}
	if prefix := events[:start]; len(prefix) != 2 || prefix[len(prefix)-1].Role == "user" {
		t.Fatalf("被压前缀 = %#v, want 以非用户行收尾（压缩区间终点不得落在提问行上）", prefix)
	}
	if len(history) == 0 || !strings.Contains(history[0].Content, "第二轮提问（本轮目标）") {
		t.Fatalf("保留窗口必须以该轮的用户提问开头，实际 = %#v", history)
	}
}

// TestIsUserQuestionEventMatchesMaintenanceObjective：轮次起点判据与"最后一条
// 真实用户输入"（sessionMaintenanceObjective）必须同源 —— 两处口径一分叉，压缩
// 边界就会把材料行当轮次起点。
func TestIsUserQuestionEventMatchesMaintenanceObjective(t *testing.T) {
	cases := []struct {
		name  string
		event model.TranscriptEvent
		want  bool
	}{
		{"用户提问", model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindUserInput}, true},
		{"旧数据（Kind 空）", model.TranscriptEvent{Role: "user"}, true},
		{"内部材料（wire_material）", model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindUserInput, WireMaterial: true}, false},
		{"内部材料（Kind internal）", model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindInternal}, false},
		{"激活技能正文", model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindUserInput, Content: ActiveSkillMarker + "\n正文"}, false},
		{"逻辑归属 system", model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindUserInput, RoleName: "system"}, false},
		{"非用户角色", model.TranscriptEvent{Role: "assistant", Kind: model.TranscriptEventKindLLM}, false},
	}
	for _, testCase := range cases {
		if got := isUserQuestionEvent(testCase.event); got != testCase.want {
			t.Errorf("%s: isUserQuestionEvent = %v, want %v", testCase.name, got, testCase.want)
		}
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
