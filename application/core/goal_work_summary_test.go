package core

import (
	"context"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// ① 的两段验证：
//   - 摘要函数口径（本轮边界 / 单行 / 工具名 / 有界）；
//   - 生产者到 ADVISOR 输入的端到端接线：ChatStream 结束点（goalAdvanceAfterChat）
//     把可见会话里 EXEC 的工作正文喂进 goal 治理，ADVISOR 的回合输入正文里能看到它。

// TestSummarizeTurnWorkKeepsCurrentTurnOnly 钉住"只取最后一条 user 行之后"：
// 上一轮的正文与工具不得混入，换行压成单行，工具名去重。
func TestSummarizeTurnWorkKeepsCurrentTurnOnly(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "上一轮的问题"},
		{Role: "assistant", Content: "上一轮的回答 OLD-MARKER"},
		{Role: "user", Content: "这一轮的问题"},
		{Role: "assistant", Content: "先看代码\n再改", Tool: &ToolCall{Name: "grep_search"}},
		{Role: "tool", Content: "命中 3 处", Tool: &ToolCall{Name: "grep_search"}},
		{Role: "tool", Content: "读到了", Tool: &ToolCall{Name: "read_file"}},
		{Role: "assistant", Content: "结论：NEW-MARKER 已接线"},
	}
	summary := summarizeTurnWork(messages)
	if strings.Contains(summary, "OLD-MARKER") {
		t.Fatalf("跨轮串入上一轮内容: %q", summary)
	}
	if !strings.Contains(summary, "NEW-MARKER") {
		t.Fatalf("缺少本轮终稿正文: %q", summary)
	}
	if strings.Contains(summary, "\n") {
		t.Fatalf("摘要必须是单行: %q", summary)
	}
	if !strings.Contains(summary, "tools: grep_search, read_file") {
		t.Fatalf("工具名应按出现顺序去重: %q", summary)
	}
	if got := summarizeTurnWork([]Message{{Role: "user", Content: "只有输入"}}); got != "" {
		t.Fatalf("本轮无产出应返回空串, 得 %q", got)
	}
}

// TestSummarizeTurnWorkIsBounded 钉住有界：Detail 上限由 goal 域
// MaxSignalDetailRunes 决定（超长正文被截断，不会让信号校验失败）。
func TestSummarizeTurnWorkIsBounded(t *testing.T) {
	long := strings.Repeat("长", goaldomain.MaxSignalDetailRunes*2)
	summary := summarizeTurnWork([]Message{
		{Role: "user", Content: "问题"},
		{Role: "assistant", Content: long},
	})
	if len([]rune(summary)) != goaldomain.MaxSignalDetailRunes {
		t.Fatalf("摘要 rune 长度 = %d, want %d", len([]rune(summary)), goaldomain.MaxSignalDetailRunes)
	}
	// 截断后仍必须是合法信号（Validate 不报错）。
	signal := goaldomain.TLEvalSignal{Kind: goaldomain.SignalTurnCompleted, Detail: summary}
	if err := signal.Validate(); err != nil {
		t.Fatalf("截断后的 Detail 必须通过信号校验: %v", err)
	}
	// 头部保留（模型把结论写在开头时不被截掉）。
	if !strings.HasPrefix(summary, "长长长") {
		t.Fatalf("截断应保留头部: %q", summary[:6])
	}
}

// TestSummarizeTurnWorkCarriesComputerUseEvidence：EXEC 用 computer_screenshot
// 时，ADVISOR 的输入摘要必须带"看到了什么"（截图媒体引用 + 画面尺寸 + 前台窗口
// 标题），而不是只有一个工具名；坏 JSON 与非 media 引用不得掺进来。
func TestSummarizeTurnWorkCarriesComputerUseEvidence(t *testing.T) {
	shot := `{"ref":"media:ce842be8c006587a575a8f81146e3235e945cf1b2c5e2aa0d0e0be34d09ed204",` +
		`"name":"screenshot-20260915-120000.000.png","width":1024,"height":576,` +
		`"foreground":{"title":"Visual Studio Code - seelex"},"cursor":{"x":1,"y":2}}`
	summary := summarizeTurnWork([]Message{
		{Role: "user", Content: "看看屏幕上是什么"},
		{Role: "assistant", Tool: &ToolCall{Name: "computer_screenshot"}},
		{Role: "tool_result", Content: shot, Tool: &ToolCall{Name: "computer_screenshot"}},
		{Role: "tool_result", Content: "not-json-at-all", Tool: &ToolCall{Name: "bash"}},
		{Role: "assistant", Content: "前台窗口是 Visual Studio Code"},
	})
	for _, want := range []string{"screen:", "media:ce842be8", "1024x576", `foreground="Visual Studio Code - seelex"`, "tools: computer_screenshot, bash"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("摘要缺少 %q：%q", want, summary)
		}
	}
	if strings.Contains(summary, shot) {
		t.Fatal("摘要不应内联整段工具结果 JSON")
	}

	// 非 media 引用（例如 blob/compressed）不算屏幕证据。
	noise := summarizeTurnWork([]Message{
		{Role: "user", Content: "看个文件"},
		{Role: "tool_result", Content: `{"ref":"blob:deadbeef","name":"x"}`, Tool: &ToolCall{Name: "read_file"}},
		{Role: "assistant", Content: "读完了"},
	})
	if strings.Contains(noise, "screen:") {
		t.Fatalf("非媒体引用不应产生屏幕证据：%q", noise)
	}
}

// capturingTLEvaluator 记录 ADVISOR 回合收到的嵌入（接线判据：输入正文）。
type capturingTLEvaluator struct {
	embeds []goaldomain.TLSessionEmbed
}

func (e *capturingTLEvaluator) Evaluate(_ context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	e.embeds = append(e.embeds, embed)
	return goaldomain.TLDirective{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}, nil
}

// TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor 是端到端接线用例：
// 会话里有一轮 EXEC 产出（正文带 marker）→ goal 在线 → 登记 turn_completed →
// ADVISOR 回合输入（即送给 LLM 的正文）里必须出现该 marker 与工具名。
//
// 席位轮转退场后回合尾不再自动跑 ADVISOR；这里显式驱动一轮 TL 回合（gate 路径）。
func TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&fakeSessions{}))
	evaluator := &capturingTLEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	sessionID := "sess-work-summary"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{
		Title: "工作内容接线",
	}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}

	service.ViewMu.Lock()
	service.appendSessionMessageLocked(sessionID, "user", "请修最小修复建议", nil)
	service.appendSessionMessageLocked(sessionID, "assistant", "先看代码", &ToolCall{Name: "read_file"})
	service.appendSessionMessageLocked(sessionID, "assistant", "已修：WORK-CONTENT-MARKER-7", nil)
	service.ViewMu.Unlock()

	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		t.Fatalf("goalCoordinatorFor: %v", err)
	}
	if err := coordinator.Notify(context.Background(), sessionID, goaldomain.TLEvalSignal{
		Kind: goaldomain.SignalTurnCompleted, Source: "chat_end",
		Detail: service.goalTurnWorkSummary(sessionID),
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, err := coordinator.bundleFor(sessionID).sup.RunEval(context.Background(), "test"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}

	if len(evaluator.embeds) == 0 {
		t.Fatal("未驱动 ADVISOR 回合（接线缺失）")
	}
	embed := evaluator.embeds[len(evaluator.embeds)-1]
	text := embed.RenderText()
	if !strings.Contains(text, "WORK-CONTENT-MARKER-7") {
		t.Fatalf("ADVISOR 输入缺少 EXEC 工作正文:\n%s", text)
	}
	if !strings.Contains(text, "read_file") {
		t.Fatalf("ADVISOR 输入缺少工具名摘要:\n%s", text)
	}
	if !strings.Contains(text, string(goaldomain.FrameWorkProgress)) {
		t.Fatalf("ADVISOR 输入缺少 %s 帧:\n%s", goaldomain.FrameWorkProgress, text)
	}
	// 锚点不变量：工作正文进 b 输入不得挤掉 goal 锚点。
	if !strings.Contains(text, "[goal-anchor]") || embed.Goal.ID == "" {
		t.Fatalf("锚点丢失:\n%s", text)
	}
}

// TestGoalAdvanceAfterChatWithoutWorkContentStaysQuiet 钉住降级：会话没有可摘要
// 产出（例如只有用户输入）时，信号仍登记但不得凭空空转出 work.progress 帧。
func TestGoalAdvanceAfterChatWithoutWorkContentStaysQuiet(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&fakeSessions{}))
	evaluator := &capturingTLEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	sessionID := "sess-no-work"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{
		Title: "空产出",
	}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}
	service.ViewMu.Lock()
	service.appendSessionMessageLocked(sessionID, "user", "只有输入", nil)
	service.ViewMu.Unlock()

	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		t.Fatalf("goalCoordinatorFor: %v", err)
	}
	if err := coordinator.Notify(context.Background(), sessionID, goaldomain.TLEvalSignal{
		Kind: goaldomain.SignalTurnCompleted, Source: "chat_end",
		Detail: service.goalTurnWorkSummary(sessionID),
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, err := coordinator.bundleFor(sessionID).sup.RunEval(context.Background(), "test"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}

	if len(evaluator.embeds) == 0 {
		t.Fatal("仍应驱动 ADVISOR 回合（TL 在线）")
	}
	for _, frame := range evaluator.embeds[len(evaluator.embeds)-1].Frames {
		if frame.Kind == goaldomain.FrameWorkProgress {
			t.Fatalf("无产出不应产生 work.progress 帧: %+v", frame)
		}
	}
}
