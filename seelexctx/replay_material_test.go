package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// 重放素材规整的单元用例集。现场形状取自 2026-09-29（装配层压缩，素材来自"请求出口
// 修复之前"的引擎历史快照：被中断的工具轮宣告了调用但没有结果）与 2026-09-28（同一
// call_id 被两次宣告，逐次配对）、2026-09-20（孤儿结果 / 乱序结果）三次实测 400。

// replayAssistantCall 构造一条只带工具调用的 assistant 声明行（wire 上正文为空）。
func replayAssistantCall(calls ...types.ToolCall) types.Message {
	return types.Message{Role: "assistant", ToolCalls: calls}
}

// replayToolCall 构造一条工具调用声明。
func replayToolCall(id, name string) types.ToolCall {
	return types.ToolCall{ID: id, Type: "function", Function: types.ToolCallFunction{Name: name}}
}

// replayToolResult 构造一条工具结果行。
func replayToolResult(id, name, content string) types.Message {
	return types.Message{Role: "tool", ToolCallID: id, Name: name, Content: stringPtr(content)}
}

func sameReplayMessages(left, right []types.Message) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		one, other := left[index], right[index]
		if one.Role != other.Role || one.ToolCallID != other.ToolCallID || one.Name != other.Name {
			return false
		}
		if (one.Content == nil) != (other.Content == nil) {
			return false
		}
		if one.Content != nil && *one.Content != *other.Content {
			return false
		}
		if len(one.ToolCalls) != len(other.ToolCalls) {
			return false
		}
		for position := range one.ToolCalls {
			if one.ToolCalls[position].ID != other.ToolCalls[position].ID ||
				one.ToolCalls[position].Function.Name != other.ToolCalls[position].Function.Name {
				return false
			}
		}
	}
	return true
}

// TestPrepareReplayMaterialDropsUnsettledTailUnit：尾巴上的声明还有未回执调用
// （工具正在跑，或上一轮被中断且没有后续）→ 整段丢掉：它不属于任何已发出的请求
// 字节，补占位等于对一个可能正在执行的调用宣布"结果丢了"。
func TestPrepareReplayMaterialDropsUnsettledTailUnit(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1：核对装配路径"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "file body"),
	}
	prepared, report := PrepareReplayMaterial(history)
	if report.DroppedTailMessages != 2 {
		t.Fatalf("dropped tail messages = %d, want 2: %+v", report.DroppedTailMessages, report)
	}
	if strings.Join(report.DroppedTailCallIDs, ",") != "t2" {
		t.Fatalf("unanswered call ids = %v, want [t2]", report.DroppedTailCallIDs)
	}
	if !report.Repaired() {
		t.Fatal("report must mark the material as repaired")
	}
	if len(prepared) != 1 || prepared[0].Role != "user" {
		t.Fatalf("material must keep the user turn only: %+v", prepared)
	}
	if err := ValidateReplayProtocol(prepared); err != nil {
		t.Fatalf("normalized material must be legal: %v", err)
	}
	// 幂等：规整过的素材再跑一次逐字不变。
	again, againReport := PrepareReplayMaterial(prepared)
	if !sameReplayMessages(again, prepared) || againReport.Repaired() {
		t.Fatalf("normalization must be idempotent: %+v", againReport)
	}
}

// TestPrepareReplayMaterialFillsDeadChainPlaceholder：中段死链（声明之后还有别的
// 消息 → 不可能是"正在跑"）按请求出口口径补回执占位，位置在声明的结果块内。
func TestPrepareReplayMaterialFillsDeadChainPlaceholder(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1：重构并验证"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "file body"),
		textMessage("assistant", "已完成第一步，继续"),
		textMessage("user", "任务2：继续"),
		textMessage("assistant", "任务2完成"),
	}
	prepared, report := PrepareReplayMaterial(history)
	if report.FilledPlaceholders != 1 {
		t.Fatalf("filled placeholders = %d, want 1: %+v", report.FilledPlaceholders, report)
	}
	if report.DroppedTailMessages != 0 {
		t.Fatalf("mid-history dead chain must not drop the tail: %+v", report)
	}
	if err := ValidateReplayProtocol(prepared); err != nil {
		t.Fatalf("normalized material must be legal: %v", err)
	}
	if len(prepared) != len(history)+1 {
		t.Fatalf("prepared length = %d, want %d", len(prepared), len(history)+1)
	}
	filled := prepared[3]
	if filled.Role != "tool" || filled.ToolCallID != "t2" || filled.Content == nil ||
		!strings.HasPrefix(*filled.Content, interruptedToolResultPrefix) {
		t.Fatalf("placeholder must sit inside the declaration result block: %+v", filled)
	}
	if prepared[4].Role != "assistant" {
		t.Fatalf("suffix text must stay after the filled result: %+v", prepared[4])
	}
	again, againReport := PrepareReplayMaterial(prepared)
	if !sameReplayMessages(again, prepared) || againReport.Repaired() {
		t.Fatalf("normalization must be idempotent: %+v", againReport)
	}
}

// TestPrepareReplayMaterialPairsRepeatedDeclarationOccurrence：同一 call_id 被两次
// 宣告（中断后重发复用 ID）。逐次配对后，中段那次声明缺回执 → 补占位；尾巴那次
// （有回执）逐字保留。旧口径"首个声明认领全部结果"会留下一条零回执的声明 → 400。
func TestPrepareReplayMaterialPairsRepeatedDeclarationOccurrence(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("c1", "read_file")),
		replayToolResult("c1", "read_file", "first body"),
		textMessage("assistant", "先看文件"),
		textMessage("user", "任务2：重发同一次调用"),
		replayAssistantCall(replayToolCall("c1", "read_file")),
		replayToolResult("c1", "read_file", "second body"),
	}
	prepared, report := PrepareReplayMaterial(history)
	if report.FilledPlaceholders != 0 || report.DroppedTailMessages != 0 {
		t.Fatalf("both declarations carry their own receipt: %+v", report)
	}
	if !sameReplayMessages(prepared, history) {
		t.Fatal("legal history must stay byte-for-byte")
	}
	if err := ValidateReplayProtocol(prepared); err != nil {
		t.Fatalf("occurrence pairing must be legal: %v", err)
	}

	// 第二次声明缺回执（记录残缺）且不在尾巴 → 补占位，不能把第一次的结果抢走。
	broken := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("c1", "read_file")),
		replayToolResult("c1", "read_file", "first body"),
		textMessage("assistant", "先看文件"),
		textMessage("user", "任务2：重发同一次调用"),
		replayAssistantCall(replayToolCall("c1", "read_file")),
		textMessage("user", "任务3"),
		textMessage("assistant", "完成"),
	}
	preparedBroken, brokenReport := PrepareReplayMaterial(broken)
	if brokenReport.FilledPlaceholders != 1 {
		t.Fatalf("second declaration must get a placeholder: %+v", brokenReport)
	}
	if err := ValidateReplayProtocol(preparedBroken); err != nil {
		t.Fatalf("repaired repeated declaration must be legal: %v", err)
	}
}

// TestPrepareReplayMaterialDropsOrphanAndRealignsMisordered：孤儿结果（没有声明可
// 回应）剔除；乱序结果搬回声明的相邻块——两者都是 provider 直接 400 的形状。
func TestPrepareReplayMaterialDropsOrphanAndRealignsMisordered(t *testing.T) {
	orphan := []types.Message{
		textMessage("user", "任务1"),
		replayToolResult("t9", "read_file", "no declaration owns me"),
		textMessage("assistant", "回答"),
	}
	prepared, report := PrepareReplayMaterial(orphan)
	if report.DroppedOrphanResults != 1 {
		t.Fatalf("orphan rows = %d, want 1: %+v", report.DroppedOrphanResults, report)
	}
	if len(prepared) != 2 || prepared[0].Role != "user" || prepared[1].Role != "assistant" {
		t.Fatalf("orphan result must be dropped: %+v", prepared)
	}
	if err := ValidateReplayProtocol(prepared); err != nil {
		t.Fatalf("dropping the orphan must be legal: %v", err)
	}

	misordered := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file")),
		textMessage("user", "插进来的消息"),
		replayToolResult("t1", "read_file", "body"),
	}
	prepared, report = PrepareReplayMaterial(misordered)
	if !report.ReorderedResults {
		t.Fatalf("misordered result must be reported: %+v", report)
	}
	if err := ValidateReplayProtocol(prepared); err != nil {
		t.Fatalf("realigned material must be legal: %v", err)
	}
	if len(prepared) != 4 || prepared[2].Role != "tool" {
		t.Fatalf("result must sit directly behind its declaration: %+v", prepared)
	}
	if prepared[3].Role != "user" || *prepared[3].Content != "插进来的消息" {
		t.Fatalf("displaced message must survive after the result block: %+v", prepared[3])
	}
}

// TestPrepareReplayMaterialKeepsLegalHistoryByteForByte：合法素材逐字不变（前缀
// 缓存口径）且不留任何"被动过"的报告。
func TestPrepareReplayMaterialKeepsLegalHistoryByteForByte(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "one"),
		replayToolResult("t2", "grep_search", "two"),
		textMessage("assistant", "第一步完成"),
		textMessage("user", "任务2"),
	}
	prepared, report := PrepareReplayMaterial(history)
	if report.Repaired() {
		t.Fatalf("legal history must not be touched: %+v", report)
	}
	if !sameReplayMessages(prepared, history) {
		t.Fatal("legal history must stay byte-for-byte")
	}
	if evidence := ReplayMaterialEvidence(report); len(evidence) != 0 {
		t.Fatalf("no repair → no evidence, got %+v", evidence)
	}
	if terse := report.Terse(); terse != "replay material replayed byte-for-byte" {
		t.Fatalf("terse = %q", terse)
	}
}

// TestReplayMaterialEvidenceNamesWhatChanged：素材被动过就必须能从帧读出来。
func TestReplayMaterialEvidenceNamesWhatChanged(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "one"),
	}
	_, report := PrepareReplayMaterial(history)
	evidence := ReplayMaterialEvidence(report)
	if len(evidence) != 1 {
		t.Fatalf("evidence = %+v, want one row", evidence)
	}
	if !strings.HasPrefix(evidence[0].Ref, CompactReplayMaterialEvidenceRefPrefix) {
		t.Fatalf("ref = %q", evidence[0].Ref)
	}
	if !strings.Contains(evidence[0].Summary, "t2") || !strings.Contains(evidence[0].Summary, "dropped unsettled tail unit") {
		t.Fatalf("summary must name the unanswered call: %q", evidence[0].Summary)
	}
}

// TestValidateReplayProtocolRejectsProviderViolations：校验器按 provider 数回执的
// 口径点名违规位置（回执不足只报"insufficient tool messages"，读不出是哪一条）。
func TestValidateReplayProtocolRejectsProviderViolations(t *testing.T) {
	legal := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file")),
		replayToolResult("t1", "read_file", "body"),
		textMessage("user", "任务2"),
	}
	if err := ValidateReplayProtocol(legal); err != nil {
		t.Fatalf("legal material must pass: %v", err)
	}
	cases := []struct {
		name     string
		messages []types.Message
		contains string
	}{
		{
			name: "回执不足",
			messages: []types.Message{
				replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
				replayToolResult("t1", "read_file", "body"),
			},
			contains: `msg#0 assistant 宣告的 tool_call "t2"`,
		},
		{
			name:     "孤儿结果",
			messages: []types.Message{textMessage("user", "任务1"), replayToolResult("t9", "read_file", "x")},
			contains: "msg#1 role=tool",
		},
		{
			name: "乱序结果（回执不在声明的相邻块内）",
			messages: []types.Message{
				replayAssistantCall(replayToolCall("t1", "read_file")),
				textMessage("user", "插进来的消息"),
				replayToolResult("t1", "read_file", "body"),
			},
			contains: `msg#0 assistant 宣告的 tool_call "t1"`,
		},
		{
			name:     "空调用 ID",
			messages: []types.Message{replayAssistantCall(replayToolCall("", "read_file"))},
			contains: "含空 ID 调用",
		},
		{
			name: "行内重复 call id",
			messages: []types.Message{
				replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t1", "grep_search")),
			},
			contains: "行内重复 call id",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateReplayProtocol(testCase.messages)
			if err == nil {
				t.Fatal("violation must be rejected")
			}
			if !strings.Contains(err.Error(), testCase.contains) {
				t.Fatalf("error = %q, want substring %q", err.Error(), testCase.contains)
			}
		})
	}
}

// TestSummarizeNormalizesAndRefusesEmptyMaterial：摘要器出口是最后一道闸——
// 直接喂破损素材给它也发不出非法请求；素材被规整到空则显式失败（不空发）。
func TestSummarizeNormalizesAndRefusesEmptyMaterial(t *testing.T) {
	chat := &recordingQuickChat{reply: "### 目标 (Goal)\n完成"}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatal(err)
	}
	broken := []types.Message{
		textMessage("user", "任务1"),
		replayAssistantCall(replayToolCall("t1", "read_file"), replayToolCall("t2", "grep_search")),
		replayToolResult("t1", "read_file", "body"),
	}
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{History: broken}); err != nil {
		t.Fatalf("unsettled tail must be dropped and the replay proceed: %v", err)
	}
	request := chat.snapshot()[0]
	if err := ValidateReplayProtocol(request.Messages); err != nil {
		t.Fatalf("wire request must be legal: %v", err)
	}
	if len(request.Messages) != 2 || request.Messages[1].Role != "user" {
		t.Fatalf("unsettled tail must not reach the wire: %+v", request.Messages)
	}

	empty := []types.Message{replayAssistantCall(replayToolCall("t1", "read_file"))}
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{History: empty}); err == nil {
		t.Fatal("material normalized to empty must fail loudly, not send a body-less replay")
	} else if !strings.Contains(err.Error(), "empty after normalization") {
		t.Fatalf("error = %q", err.Error())
	}
}
