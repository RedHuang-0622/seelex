package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	storecontext "github.com/RedHuang-0622/seelex/store/context"
)

// 本文件钉住「task 尾帧（plan/task 尾栈）为什么不会重演 plan 帧的位置漂移」。
//
// 背景：docs/arch/context-prefix-stability-l1.md §6.1 把 `assembler.go:120` 的
// tailStacks 与 plan 上下文消息并列成 I2 疑点（"现算现插"）。plan 帧那一半已改成
// 「首现即落 transcript、按记录位置重放」（§6.3.2 / §9 D1 的完整形态）；task 尾帧
// **不需要同一改法**，因为它不满足 plan 帧致漂的两个条件（见 §6.3.3）。本文件把
// 这两条机制钉成可执行判据：以后若有人把尾部帧搬进历史中间、或把装配结果写回
// WorkingHistory，用例立刻红。
//
// 机制（三条，逐条断言）：
//  ① 注入点是「WorkingHistory 之后追加」（assembler.go:120-122：
//     `for _, block := range a.tailStacks() { history = append(history, block.Messages...) }`），
//     而框架委托装配器把 WorkingHistory 排在所有块之后（Seele seelectx 的
//     DefaultRequestAssembler）——尾部帧的位置恒等于「本请求最末格」，与历史长度无关。
//  ② 帧只来自 tailStacks() 的即时投影，装配过程不写回 WorkingHistory，因此下一轮
//     不会从历史中间把它重放出来。plan 帧之所以漂，正是因为它进了历史：上一轮它
//     落在「提问之后、输入之前」那一格，下一轮同一格被新历史占住（§6.3.2）。
//  ③ 由 ①②：相邻两次请求的字节 LCP 必定 ≥「上一条请求减去尾部帧」，断点不可能
//     早于尾部帧起点；损坏面上界就是尾部帧自身的字节数（用例实测打印）。plan 帧
//     旧缺陷相反——它顶掉的是**已经进过 wire 的历史**，于是从该格起到请求尾整段
//     重算（实测 hit=16767/17520=95.7%）。

// tailFrameStep 是一次会真正发往 provider 的请求（生产顺序：装配取当前
// 工作历史快照，历史跨迭代/跨轮只追加）。
type tailFrameStep struct {
	label    string
	history  []types.Message
	plan     storecontext.PlanFrame
	task     storecontext.TaskFrame
	tailWant []string // 期望落在请求最末的尾部帧标识
}

func TestTailFrameStaysAtRequestSuffix(t *testing.T) {
	var snapshot storecontext.PromptStackSnapshot
	withTail := NewAssembler(AssemblerOptions{
		SystemPrompt: func() string { return "system-prompt-stable" },
		PrefixStacks: func() []seelectx.PromptBlock { return RenderStablePrefixBlocks(snapshot) },
		TailStacks:   func() []seelectx.PromptBlock { return RenderTailBlocks(snapshot) },
	})
	// 对照组：同一份 system/前缀/历史，**没有**尾部帧。它是"前缀本体"的字节定义。
	withoutTail := NewAssembler(AssemblerOptions{
		SystemPrompt: func() string { return "system-prompt-stable" },
		PrefixStacks: func() []seelectx.PromptBlock { return RenderStablePrefixBlocks(snapshot) },
	})

	t1Plan := storecontext.PlanFrame{PlanID: "plan-1", Title: "收口 M1", Status: "active"}
	t1Task := storecontext.TaskFrame{TaskID: "task-1", Objective: "task 尾帧同类处理", Status: "active"}
	doneTask := storecontext.TaskFrame{TaskID: "task-1", Objective: "task 尾帧同类处理", Status: "completed"}
	donePlan := storecontext.PlanFrame{PlanID: "plan-1", Title: "收口 M1", Status: "completed"}
	planLabel, taskLabel := "now using plan", "now using task"

	// 生产形态：一轮多次工具迭代（历史只追加），跨轮历史继续变长；帧在同一轮内
	// 还可能变（task 终态）——这恰是最容易让"现算现插"露馅的形状。
	base := []types.Message{
		textMessage("user", "第 1 轮：先看装配顺序。"),
	}
	steps := []tailFrameStep{
		{label: "t1.iter1", history: base, plan: t1Plan, task: t1Task, tailWant: []string{planLabel, taskLabel}},
		{label: "t1.iter2", history: append(cloneTailProbe(base),
			textMessage("assistant", "先跑一次工具。"),
			textMessage("tool", "工具结果：装配顺序已核对。"),
		), plan: t1Plan, task: t1Task, tailWant: []string{planLabel, taskLabel}},
		// 同一轮内 task 进入终态：尾部帧内容变了，位置不能变。
		{label: "t1.iter3", history: append(cloneTailProbe(base),
			textMessage("assistant", "先跑一次工具。"),
			textMessage("tool", "工具结果：装配顺序已核对。"),
			textMessage("assistant", "任务收尾。"),
			textMessage("tool", "工具结果：任务已完成。"),
		), plan: t1Plan, task: doneTask, tailWant: []string{planLabel, taskLabel}},
		{label: "t2.iter1", history: append(cloneTailProbe(base),
			textMessage("assistant", "先跑一次工具。"),
			textMessage("tool", "工具结果：装配顺序已核对。"),
			textMessage("assistant", "任务收尾。"),
			textMessage("tool", "工具结果：任务已完成。"),
			textMessage("assistant", "第 1 轮结束。"),
			textMessage("tool", "工具结果：轮次归档完成。"),
			textMessage("user", "第 2 轮：继续推进。"),
		), plan: donePlan, task: doneTask, tailWant: []string{planLabel, taskLabel}},
	}

	type sample struct {
		label string
		full  string
		plain string
	}
	samples := make([]sample, 0, len(steps))
	for _, step := range steps {
		snapshot = storecontext.PromptStackSnapshot{PlanStack: []storecontext.PlanFrame{step.plan}, TaskStack: []storecontext.TaskFrame{step.task}}
		historyBefore := serializeTailProbe(step.history)
		full, err := withTail.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: step.history})
		if err != nil {
			t.Fatalf("%s: assemble with tail frames: %v", step.label, err)
		}
		// ② 装配只读工作历史：帧不得写回 WorkingHistory（写回 = 下一轮从历史中间重放）。
		if after := serializeTailProbe(step.history); after != historyBefore {
			t.Fatalf("%s: 装配改写了调用方 WorkingHistory（尾部帧被写回历史）：before=%q after=%q",
				step.label, historyBefore, after)
		}
		plain, err := withoutTail.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: step.history})
		if err != nil {
			t.Fatalf("%s: assemble without tail frames: %v", step.label, err)
		}

		plainBytes := serializeTailProbe(plain.Messages)
		fullBytes := serializeTailProbe(full.Messages)
		// ① 尾部帧 = 请求最末格：前缀本体逐字节不变，尾部帧只追加在其后。
		if !strings.HasPrefix(fullBytes, plainBytes) {
			t.Fatalf("%s: 尾部帧不是纯追加（前缀本体被改写）：\nplain=%q\nfull =%q", step.label, plainBytes, fullBytes)
		}
		if len(full.Messages) != len(plain.Messages)+len(step.tailWant) {
			t.Fatalf("%s: 请求消息数 = %d，前缀本体 %d + 尾部帧 %d，期望 %d",
				step.label, len(full.Messages), len(plain.Messages), len(step.tailWant), len(plain.Messages)+len(step.tailWant))
		}
		for index, want := range step.tailWant {
			position := len(plain.Messages) + index
			message := full.Messages[position]
			if message.Content == nil || !strings.Contains(*message.Content, want) {
				t.Fatalf("%s: 第 %d 格（共 %d 格）应当是尾部帧 %q，实际 %#v",
					step.label, position, len(full.Messages), want, message.Content)
			}
			// 尾部帧之后不允许有任何消息（否则"最末格"这条机制不成立）。
			if position != len(full.Messages)-len(step.tailWant)+index {
				t.Fatalf("%s: 尾部帧不在请求最末格（position=%d，total=%d）", step.label, position, len(full.Messages))
			}
		}

		samples = append(samples, sample{label: step.label, full: fullBytes, plain: plainBytes})
		t.Logf("  %-10s msgs=%d prefix=%d tail=%d tailChars=%d",
			step.label, len(full.Messages), len(plain.Messages), len(step.tailWant), len(fullBytes)-len(plainBytes))
	}

	// ③ 相邻两次请求：断点不可能早于尾部帧起点，损坏面上界 = 尾部帧自身字节数。
	for index := 1; index < len(samples); index++ {
		prev, cur := samples[index-1], samples[index]
		shared := tailProbeLCP(prev.full, cur.full)
		loss := len(prev.full) - shared
		prevTailBytes := len(prev.full) - len(prev.plain)
		t.Logf("  %s -> %s: LCP=%d prev=%d(prefix=%d tail=%d) loss=%d unexpected=0",
			prev.label, cur.label, shared, len(prev.full), len(prev.plain), prevTailBytes, loss)
		if shared < len(prev.plain) {
			t.Fatalf("%s -> %s: 前缀本体断了（LCP=%d < 前缀字节 %d）：尾部帧之后出现了被顶掉的历史",
				prev.label, cur.label, shared, len(prev.plain))
		}
		if loss > prevTailBytes {
			t.Fatalf("%s -> %s: 损坏面 %d 字节超出尾部帧自身字节数 %d：有尾部帧之外的内容被拖累",
				prev.label, cur.label, loss, prevTailBytes)
		}
	}
}

// TestTailFrameSurvivesToolProtocolSanitize 钉住第二条机制：尾部帧在协议规整
// （SanitizeProviderToolProtocol，assembler.go:114）**之后**追加，所以出口的
// 修边（剔除孤儿 tool 行、搬回乱序结果）既挪不动也删不掉它，位置仍是"最末格"。
func TestTailFrameSurvivesToolProtocolSanitize(t *testing.T) {
	snapshot := storecontext.PromptStackSnapshot{
		TaskStack: []storecontext.TaskFrame{{TaskID: "task-1", Objective: "收口 M1", Status: "active"}},
	}
	assembler := NewAssembler(AssemblerOptions{
		TailStacks: func() []seelectx.PromptBlock { return RenderTailBlocks(snapshot) },
	})
	// 孤儿 tool 行（没有前置 tool_calls）：出口会被剔除。
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: []types.Message{
			textMessage("user", "第 1 轮：先看装配顺序。"),
			textMessage("tool", "孤儿工具结果"),
		},
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(assembled.Messages) != 2 {
		t.Fatalf("消息数 = %d，期望 2（孤儿 tool 行被剔除，尾部帧保留）", len(assembled.Messages))
	}
	last := assembled.Messages[len(assembled.Messages)-1]
	if last.Content == nil || !strings.Contains(*last.Content, "now using task") {
		t.Fatalf("最末格应当是尾部 task 帧，实际 %#v", last.Content)
	}
	if strings.Contains(serializeTailProbe(assembled.Messages), "孤儿工具结果") {
		t.Fatal("孤儿 tool 行没有被出口规整剔除：尾部帧与规整的先后关系变了")
	}
}

func cloneTailProbe(messages []types.Message) []types.Message {
	return append([]types.Message(nil), messages...)
}

// serializeTailProbe 把消息序列化成确定性字节流（探针比较用：只取 role +
// 正文 + 工具调用/结果标识，够用来判断"两条请求是否互为前缀"）。
func serializeTailProbe(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString("|")
		builder.WriteString(message.Role)
		builder.WriteString("|")
		if message.Content != nil {
			builder.WriteString(*message.Content)
		}
		if message.ToolCallID != "" {
			builder.WriteString("|call=")
			builder.WriteString(message.ToolCallID)
		}
		for _, call := range message.ToolCalls {
			builder.WriteString("|tool=")
			builder.WriteString(call.ID)
			builder.WriteString("/")
			builder.WriteString(call.Function.Name)
			builder.WriteString("/")
			builder.WriteString(call.Function.Arguments)
		}
	}
	return builder.String()
}

// tailProbeLCP 返回两段字节流的最长公共前缀长度。
func tailProbeLCP(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	return index
}
