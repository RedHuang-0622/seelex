package core

// Seelex vs Codex 上下文策略「对照组」探针（本地可复现，不发网络请求）。
//
// 对照组设计——唯一变量是**上下文管理策略**：
//
//	同一脚本（同样的用户输入、工具调用、工具结果、assistant 正文）
//	+ 同一串行化器（serializeRequest / abSerializeAssembled，字段序固定）
//	+ 同一 token 估算器（生产同款 seelexctx/tokens）
//	+ 同一工具目录（按 name 排序后序列化，消除"工具序"这个额外变量）
//
// 三个 arm：
//
//	arm S-prod   Seelex 生产：每轮收尾清空工作历史 → 下一轮从 durable transcript
//	             全量重投影；且 wire 上工具轮 assistant 正文被丢弃、收尾把视图正文
//	             事后合并回 transcript（`./Seele/session/loop.go` callLLM +
//	             `application/core/chat.go:792-812`）。
//	arm S-fix    仅修「wire 与 durable 逐字节一致」这一件事（等价既有探针 C 组）。
//	arm C        Codex 策略模型：append-only item 流；已发布的 item 永不改写、
//	             不编造占位；动态事实只在变化时作为**尾部**片段追加/替换
//	             （`openai/codex@main`：`core/src/client.rs` 常量 instructions、
//	             `core/src/context_manager/updates.rs:32-60` merge_contextual_fragments、
//	             `core/tests/suite/prompt_caching.rs` 前缀断言）。
//
// 注意：arm C 是「按 Codex 公开文档/源码语义实现的策略模型」，度量的是该策略在同一
// 脚本上的**可达上界**，不是对真实 Codex 进程的抓包。arm S-* 是沿生产装配路径的
// 真实字节（本地复现）。
//
// 运行：
//
//	go test ./application/core -run ContextStrategyAB -v -count=1

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 脚本 ────────────────────────────────────────────────────────────────

type abToolScript struct {
	id, name, args, result string
}

type abTurnScript struct {
	input     string
	narration string
	reply     string
	// env 非空表示本轮回合发生时"世界状态"发生了变化（cwd / 权限 / AGENTS.md /
	// 记忆选择等），用于观察两侧策略如何安置这类动态事实。
	env   string
	tools []abToolScript
}

func abBody(line string, repeat int) string { return strings.Repeat(line, repeat) }

// abDemoScript 是一条 4 轮脚本会话，每轮都发 2 次工具调用（与生产 ReAct 形状一致）。
func abDemoScript() []abTurnScript {
	return []abTurnScript{
		{
			input:     "第 1 轮：核对 seelex 上下文装配路径的前缀稳定性，给出文件级证据。",
			narration: "我先读取装配入口与回合收尾代码，核对工具轮的前缀语义。",
			reply:     "第 1 轮结论：装配顺序与文档一致；回合内为纯追加，跨轮待核。",
			tools: []abToolScript{
				{"call-read-1", "read_file", `{"path":"application/core/context_runtime/coordinator.go"}`,
					abBody("coordinator.go: PrepareExecutionContextFor 保留段重建路径证据行；", 60)},
				{"call-grep-2", "grep_search", `{"pattern":"mergeStreamedToolNarration"}`,
					abBody("chat.go: mergeStreamedToolNarration 视图正文合并证据行；", 40)},
			},
		},
		{
			input:     "第 2 轮：继续，说明上一轮工具结果在新轮首个请求里的失效影响。",
			narration: "我核对 transcript 重建后的 assistant 工具轮正文与 wire 的差异。",
			reply:     "第 2 轮结论：重建字节在上一轮首个工具调用处即与 wire 分叉。",
			tools: []abToolScript{
				{"call-read-3", "read_file", `{"path":"../Seele/session/loop.go"}`,
					abBody("loop.go: callLLM tool_calls 分支 Content=nil 证据行；", 55)},
				{"call-read-4", "read_file", `{"path":"application/core/context_runtime/history.go"}`,
					abBody("history.go: RepairEmptyHistoryContent 空正文修复证据行；", 35)},
			},
		},
		{
			input:     "第 3 轮：进入新模块，核对 provider 侧缓存键与工具序。",
			narration: "我先确认 provider 缓存键与会话边界，再看工具序是否确定。",
			reply:     "第 3 轮结论：缓存键与命中观测双缺，工具序依赖 map 迭代。",
			env:       "[环境片段] cwd=G:\\Program\\go\\seelex；权限=workspace-write；date=2026-09-12",
			tools: []abToolScript{
				{"call-grep-5", "grep_search", `{"pattern":"prompt_cache_key"}`,
					abBody("docs: prompt_cache_key 与 session 级缓存桶证据行；", 45)},
				{"call-read-6", "read_file", `{"path":"../Seele/tools/tools.go"}`,
					abBody("tools.go: rebuildLocked 遍历 providers 顺序非确定证据行；", 40)},
			},
		},
		{
			input:     "第 4 轮：给出收口结论与修复顺序。",
			narration: "整理跨轮悬崖的成因链与修复优先级。",
			reply:     "第 4 轮结论：先补命中观测，再消除跨轮重投影。",
			tools: []abToolScript{
				{"call-read-7", "read_file", `{"path":"docs/research/2026-09-12-cache-hit-vs-codex-root-cause.md"}`,
					abBody("research: 跨轮悬崖与修复顺序证据行；", 50)},
				{"call-read-8", "read_file", `{"path":"docs/arch/context-prefix-chain.md"}`,
					abBody("arch: 已定稿轮次 append-only 不变量声明证据行；", 30)},
			},
		},
	}
}

// ── 请求快照与度量 ──────────────────────────────────────────────────────

type abCall struct {
	arm   string
	turn  int
	iter  int
	label string
	items int
	msgs  []EngineMessage
	bytes string
	total int
}

type abRun struct {
	label   string
	system  string
	tools   []Tool
	calls   []abCall
	durable [][]TranscriptEvent
}

type abDivergence struct {
	prevLabel     string
	curLabel      string
	prevTok       int
	curTok        int
	sharedTok     int
	invalidated   int
	prefixIntact  bool
	firstDiffIdx  int
	firstDiffRole string
	raw           float64
	r64           float64
	r1k           float64
	mode          string
}

// abSerialize 把一次"provider 可见请求"固化为确定性字节流：
//
//	[常量头] 工具 schema 签名 + instructions(system)
//	[消息流] item 0..n（字段序固定，前缀相等 ⇒ 字节前缀相等）
//
// 工具 schema 与 system 都是跨轮常量，放在头部与真实 provider 的渲染顺序一致
// （tools 字段独立于 messages，渲染在消息之前）；这样"回合内追加消息"不会被
// 尾部常量块打断，字节 LCP 只反映消息流的真实发散。
func abSerialize(system string, items []EngineMessage, tools []Tool) string {
	var builder strings.Builder
	builder.WriteString(serText("TOOLS", abToolsSignature(tools)))
	builder.WriteString(serText("SYSTEM", system))
	for index := range items {
		message := &items[index]
		builder.WriteString("\n||\n")
		builder.WriteString(serText("ROLE", message.Role))
		builder.WriteString(serText("CONTENT", message.Content))
		builder.WriteString(serText("REASON", message.ReasoningContent))
		builder.WriteString(serText("TOOL", message.ToolCallID+":"+message.Name))
		for _, call := range message.ToolCalls {
			builder.WriteString("\n@TC@")
			builder.WriteString(serText("ID", call.ID))
			builder.WriteString(serText("NAME", call.Name))
			builder.WriteString(serText("ARGS", call.Arguments))
		}
	}
	return builder.String()
}

func abToolsSignature(tools []Tool) string {
	var builder strings.Builder
	for _, tool := range tools {
		builder.WriteString("\n@TOOL@")
		builder.WriteString(serText(tool.Name, tool.Description))
	}
	return builder.String()
}

func abSortedTools(tools []Tool) []Tool {
	out := append([]Tool(nil), tools...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func abCapture(arm string, turn, iter int, system string, items []EngineMessage, tools []Tool) abCall {
	serialized := abSerialize(system, items, tools)
	return abCall{
		arm: arm, turn: turn, iter: iter,
		label: fmt.Sprintf("%s.t%d.i%d", arm, turn, iter),
		items: len(items), msgs: append([]EngineMessage(nil), items...),
		bytes: serialized, total: tokens.Count(serialized),
	}
}

func abCompare(prev, cur abCall) abDivergence {
	shared := lcpBytes(prev.bytes, cur.bytes)
	index, role := prefixProbeFirstDiff(prev.msgs, cur.msgs)
	out := abDivergence{
		prevLabel: prev.label, curLabel: cur.label,
		prevTok: prev.total, curTok: cur.total,
		sharedTok:    tokens.Count(cur.bytes[:shared]),
		prefixIntact: shared == len(prev.bytes),
		firstDiffIdx: index, firstDiffRole: role,
	}
	out.invalidated = cur.total - out.sharedTok
	if cur.total > 0 {
		out.raw = float64(out.sharedTok) / float64(cur.total)
		out.r64 = float64(blockFloor(out.sharedTok, 64)) / float64(cur.total)
		out.r1k = float64(blockFloor(out.sharedTok, 1024)) / float64(cur.total)
	}
	return out
}

func abLog(t *testing.T, kind string, d abDivergence) {
	t.Helper()
	t.Logf("  %-16s %-12s -> %-12s prefix_intact=%-5v first_diff=msg#%d(%s) shared=%6d/%6d invalidated=%6d raw=%5.1f%% @1024=%5.1f%%",
		kind, d.prevLabel, d.curLabel, d.prefixIntact, d.firstDiffIdx, d.firstDiffRole,
		d.sharedTok, d.curTok, d.invalidated, d.raw*100, d.r1k*100)
}

// ── arm S-*：沿生产装配路径驱动脚本 ─────────────────────────────────────

func runABProductionArm(t *testing.T, label string, script []abTurnScript, cfg prefixProbeConfig) *abRun {
	t.Helper()
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()
	run := &abRun{label: label, tools: abSortedTools(harness.tools)}
	for turnIndex, turn := range script {
		turnNo := turnIndex + 1
		harness.beginTurn(turn.input)
		run.system = systemPromptFor(harness.service)
		run.calls = append(run.calls, abCapture(label, turnNo, 1, run.system, harness.history, run.tools))

		if turn.narration != "" {
			harness.streamText(turn.narration)
		}
		for callIndex, tool := range turn.tools {
			engineContent := ""
			if cfg.engineKeepsToolContent {
				engineContent = turn.narration
			}
			call := []types.ToolCall{{
				ID: tool.id, Type: "function",
				Function: types.ToolCallFunction{Name: tool.name, Arguments: tool.args},
			}}
			harness.llmIteration(engineContent, "", call)
			harness.toolRound(tool.name, tool.id, tool.args, tool.result)
			run.calls = append(run.calls, abCapture(label, turnNo, callIndex+2, run.system, harness.history, run.tools))
		}
		harness.streamText(turn.reply)
		harness.llmIteration(turn.reply, "", nil)
		harness.finishTurn(turn.reply)
		run.durable = append(run.durable, harness.service.components.tasks.TranscriptFor(harness.session))
	}
	return run
}

// ── arm C：Codex 策略模型（append-only item 流） ────────────────────────

// runABCodexArm 按 Codex 的发布策略构造同一脚本的请求序列：
//   - 头部是常量 instructions（本模型里与 Seelex 的 system 用同一串，保证内容同源）；
//   - item 只在"发生当时"追加一次，追加后永不改写（无"事后合并视图正文"、
//     无"空正文占位/修复注记"）；
//   - 世界状态变化渲染为尾部片段（变化时才写，写在尾部，不回头改头部）；
//   - 每次请求 = 当前已知 item 列表的确定性串行化。
func runABCodexArm(t *testing.T, label string, script []abTurnScript, system string, tools []Tool) *abRun {
	t.Helper()
	sorted := abSortedTools(tools)
	run := &abRun{label: label, system: system, tools: sorted}
	items := make([]EngineMessage, 0, 64)
	for turnIndex, turn := range script {
		turnNo := turnIndex + 1
		if turn.env != "" {
			items = append(items, EngineMessage{Role: "user", Content: turn.env, ContentSet: true})
		}
		items = append(items, EngineMessage{Role: "user", Content: turn.input, ContentSet: true})
		run.calls = append(run.calls, abCapture(label, turnNo, 1, system, items, sorted))

		for callIndex, tool := range turn.tools {
			narration := ""
			if callIndex == 0 {
				// Codex：assistant 的正文与它的 function_call 同属该回合输出，
				// 在同一步追加，之后不再回填。
				narration = turn.narration
			}
			items = append(items, EngineMessage{
				Role: "assistant", Content: narration, ContentSet: narration != "",
				ToolCalls: []EngineToolCall{{ID: tool.id, Name: tool.name, Arguments: tool.args}},
			})
			items = append(items, EngineMessage{
				Role: "tool", ToolCallID: tool.id, Name: tool.name, Content: tool.result, ContentSet: true,
			})
			run.calls = append(run.calls, abCapture(label, turnNo, callIndex+2, system, items, sorted))
		}
		items = append(items, EngineMessage{Role: "assistant", Content: turn.reply, ContentSet: true})
	}
	return run
}

// ── 对照组主实验 ────────────────────────────────────────────────────────

func TestContextStrategyAB_CrossTurnScript(t *testing.T) {
	script := abDemoScript()

	productions := []struct {
		run *abRun
		cfg prefixProbeConfig
	}{
		{cfg: prefixProbeConfig{name: "S-prod", engineKeepsToolContent: false}},
		{cfg: prefixProbeConfig{name: "S-fix", engineKeepsToolContent: true}},
	}
	for index := range productions {
		productions[index].run = runABProductionArm(t, productions[index].cfg.name, script, productions[index].cfg)
	}
	system := productions[0].run.system
	tools := productions[0].run.tools
	codex := runABCodexArm(t, "Codex", script, system, tools)

	runs := []*abRun{productions[0].run, productions[1].run, codex}
	t.Logf("== 对照组：同一 4 轮脚本 / 同一串行化器 / 同一估算器 / 同一工具目录（%d 个，按 name 排序） ==", len(tools))
	t.Logf("  唯一变量 = 下一轮请求的消息列表怎么来（尾部追加 vs 全量重投影）")
	t.Logf("  system(instructions) %d tok 常量；脚本 %d 轮、每轮 2 次工具调用", tokens.Count(system), len(script))

	for _, run := range runs {
		t.Logf("-- arm %s --", run.label)
		crossTotal, crossShared := 0, 0
		allTotal, allShared := 0, 0
		for index, call := range run.calls {
			t.Logf("   call=%-12s items=%3d total=%6d tok", call.label, call.items, call.total)
			if index == 0 {
				continue
			}
			d := abCompare(run.calls[index-1], call)
			kind := "within-turn"
			if run.calls[index-1].turn != call.turn {
				kind = "CROSS-TURN"
				crossTotal += d.curTok
				crossShared += d.sharedTok
			}
			abLog(t, kind, d)
			allTotal += d.curTok
			allShared += d.sharedTok
		}
		t.Logf("   aggregate: all-pairs hit %5.1f%% (shared=%d/total=%d) | cross-turn-first-request hit %5.1f%% (shared=%d/total=%d) invalidated=%d tok",
			100*float64(allShared)/float64(allTotal), allShared, allTotal,
			100*float64(crossShared)/float64(crossTotal), crossShared, crossTotal, crossTotal-crossShared)
	}

	// 不变量对比：arm C 的每一次请求都必须是上一次请求的字节前缀扩展；
	// arm S-prod 的跨轮首请求必然违反（这正是 hit 掉下来的机制）。
	t.Logf("-- 不变量：上一请求是下一请求的字节前缀？ --")
	for _, run := range runs {
		withinViolations, crossViolations, crossCount := 0, 0, 0
		for index := 1; index < len(run.calls); index++ {
			sameTurn := run.calls[index-1].turn == run.calls[index].turn
			intact := lcpBytes(run.calls[index-1].bytes, run.calls[index].bytes) == len(run.calls[index-1].bytes)
			if sameTurn {
				if !intact {
					withinViolations++
				}
				continue
			}
			crossCount++
			if !intact {
				crossViolations++
			}
		}
		t.Logf("   %-8s within-turn violations=%d/%d | cross-turn violations=%d/%d",
			run.label, withinViolations, len(run.calls)-len(script), crossViolations, crossCount)
	}

	// 对照组有效性自检：arm C 的 item 内容必须与 arm S 重建出的内容同源
	// （差异只允许出现在"改写/占位"处，且必须被显式打印出来，不能藏起来）。
	t.Logf("-- 内容同源自检（arm S-prod 重建序列 vs arm C 发布序列，逐轮首请求） --")
	for turn := 1; turn <= len(script); turn++ {
		sCall := abCallAt(productions[0].run, turn, 1)
		cCall := abCallAt(codex, turn, 1)
		if sCall == nil || cCall == nil {
			continue
		}
		mismatch := 0
		limit := len(sCall.msgs)
		if len(cCall.msgs) < limit {
			limit = len(cCall.msgs)
		}
		for index := 0; index < limit; index++ {
			if !probeMessagesEqual(sCall.msgs[index], cCall.msgs[index]) {
				mismatch++
				if mismatch <= 3 {
					t.Logf("   turn%d msg#%d role=%s:", turn, index, sCall.msgs[index].Role)
					t.Logf("        seelex 重建: content=%d bytes set=%v %q",
						len(sCall.msgs[index].Content), sCall.msgs[index].ContentSet, abPreview(sCall.msgs[index].Content, 40))
					t.Logf("        codex  发布: content=%d bytes set=%v %q",
						len(cCall.msgs[index].Content), cCall.msgs[index].ContentSet, abPreview(cCall.msgs[index].Content, 40))
				}
			}
		}
		t.Logf("   turn%d: items seelex_rebuilt=%d codex_published=%d content-mismatches=%d",
			turn, len(sCall.msgs), len(cCall.msgs), mismatch)
	}

	// durable transcript 的形状（看"事后合并"到底写进了什么）。
	t.Logf("-- durable transcript（arm S-prod 每轮收尾后的落盘序列） --")
	seen := 0
	for turnIndex, events := range productions[0].run.durable {
		for _, event := range events[seen:] {
			t.Logf("   turn%d role=%-9s toolcalls=%d content=%d chars: %s",
				turnIndex+1, event.Role, len(event.ToolCalls), len(event.Content), abPreview(event.Content, 44))
		}
		seen = len(events)
	}
}

func abCallAt(run *abRun, turn, iter int) *abCall {
	for index := range run.calls {
		if run.calls[index].turn == turn && run.calls[index].iter == iter {
			return &run.calls[index]
		}
	}
	return nil
}

func abPreview(text string, limit int) string {
	flat := strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\r", "")
	if flat == "" {
		return `""`
	}
	if len([]rune(flat)) > limit {
		return string([]rune(flat)[:limit]) + "…"
	}
	return flat
}

// ── 举例：第 1 轮 → 第 2 轮 的逐消息对照 ───────────────────────────────

// TestContextStrategyAB_WorkedExample 把同一个回合边界（第 1 轮最后一次请求 →
// 第 2 轮首个请求）在两侧策略下的消息序列逐条打印出来，并给出该边界按 miss
// 计费的 token 数——即"上下文管理不同"最直接的用户可见后果。
func TestContextStrategyAB_WorkedExample(t *testing.T) {
	script := abDemoScript()
	prod := runABProductionArm(t, "S-prod", script,
		prefixProbeConfig{name: "S-prod", engineKeepsToolContent: false})
	codex := runABCodexArm(t, "Codex", script, prod.system, prod.tools)

	type boundary struct {
		run  *abRun
		prev *abCall
		cur  *abCall
	}
	boundaries := []boundary{
		{run: prod, prev: abCallAt(prod, 1, lastIter(prod, 1)), cur: abCallAt(prod, 2, 1)},
		{run: codex, prev: abCallAt(codex, 1, lastIter(codex, 1)), cur: abCallAt(codex, 2, 1)},
	}

	t.Logf("== 举例：同一条第 2 轮输入「%s」 ==", script[1].input)
	for _, b := range boundaries {
		d := abCompare(*b.prev, *b.cur)
		t.Logf("-- arm %s：第 1 轮最后一请求(%s, %d tok) → 第 2 轮首请求(%s, %d tok) --",
			b.run.label, b.prev.label, b.prev.total, b.cur.label, b.cur.total)
		t.Logf("   命中上界：shared=%d tok / total=%d tok = %.1f%%（@1024 块对齐 %.1f%%）；按 miss 计费 %d tok",
			d.sharedTok, d.curTok, d.raw*100, d.r1k*100, d.invalidated)
		t.Logf("   首字节一致到 %d 字节处（prev %d 字节）；首个差异 msg#%d(%s)；prefix_intact=%v",
			lcpBytes(b.prev.bytes, b.cur.bytes), len(b.prev.bytes), d.firstDiffIdx, d.firstDiffRole, d.prefixIntact)
		for index, message := range b.cur.msgs {
			mark := "  "
			note := ""
			switch {
			case index >= len(b.prev.msgs):
				mark = "+>" // 尾部新增
				note = "尾部新增（只按新增部分计费）"
			case !probeMessagesEqual(b.prev.msgs[index], message):
				mark = "!!" // 旧消息被改写
				note = fmt.Sprintf("旧消息被改写（wire 时 content=%d chars/set=%v → 本次 %d chars/set=%v）",
					len(b.prev.msgs[index].Content), b.prev.msgs[index].ContentSet,
					len(message.Content), message.ContentSet)
			}
			t.Logf("   %s msg#%d role=%-9s tc=%d content=%d chars %-22s | %s",
				mark, index, message.Role, len(message.ToolCalls), len(message.Content), abPreview(message.Content, 22), note)
		}
	}
}

// ── 说明正文的按迭代归位（修复前：回合收尾事后填充 → 叙述漂到别的轮次） ──

// TestStrategyAB_NarrationAttributionAcrossTurns 沿生产归位路径
// （harness.toolRound → task_context.AttributeToolNarrationLocked）驱动 3 轮 ×
// 每轮 2 次工具调用，打印每个工具轮 assistant 事件实际承载的说明正文。
//
// 修复前（回合收尾 mergeStreamedToolNarration：候选 = 整个会话视图的 assistant
// 正文，填充时从 transcript 头部找第一个空事件）同一场景实测：
//
//	call-1-1 = "T1 第1次…T1 第2次…T1 结论…"   ← 整轮正文压到本轮第一个事件
//	call-1-2 = ""
//	call-2-1 = "T2 第1次…T2 第2次…T2 结论…"
//	call-2-2 = "T1 第1次…T1 第2次…T1 结论…"   ← 叙述漂到别的轮次
//	call-3-1 = "T2 第1次…T2 第2次…T2 结论…"   ← 每轮都在改写更早的旧事件
//	call-3-2 = "T3 第1次…T3 第2次…T3 结论…"
//
// 修复后：每个事件恰好等于它自己那次迭代的说明文本（严格逐字断言见
// TestToolNarrationStaysWithOwningIteration）。
func TestStrategyAB_NarrationAttributionAcrossTurns(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	const turns = 3
	const rounds = 2
	owned := make(map[string]string, turns*rounds)
	for turn := 1; turn <= turns; turn++ {
		harness.beginTurn(fmt.Sprintf("第 %d 轮输入", turn))
		for round := 1; round <= rounds; round++ {
			callID := fmt.Sprintf("call-%d-%d", turn, round)
			narration := fmt.Sprintf("T%d 第%d次工具前的说明。", turn, round)
			arguments := fmt.Sprintf(`{"path":"file-%d-%d.go"}`, turn, round)
			owned[callID] = narration
			harness.streamText(narration)
			harness.llmIteration("", "读取装配入口。", []types.ToolCall{{
				ID: callID, Type: "function",
				Function: types.ToolCallFunction{Name: "read_file", Arguments: arguments},
			}})
			harness.toolRound("read_file", callID, arguments, fmt.Sprintf("round-%d tool %d result", turn, round))
		}
		reply := fmt.Sprintf("T%d 结论：本轮结束。", turn)
		harness.streamText(reply)
		harness.llmIteration(reply, "整理结论。", nil)
		harness.finishTurn(reply)
	}

	t.Logf("== 落盘 transcript 里的工具轮 assistant 事件 ==")
	drifted := 0
	for _, event := range harness.service.components.tasks.TranscriptFor(harness.session) {
		if event.Role != "assistant" || len(event.ToolCalls) == 0 {
			continue
		}
		callID := event.ToolCalls[0].ID
		mark := "ok(本迭代)"
		if event.Content != owned[callID] {
			mark = "DRIFT(正文不属于本迭代)"
			drifted++
		}
		t.Logf("  %s content=%q → %s", callID, abPreview(event.Content, 46), mark)
	}
	if drifted != 0 {
		t.Fatalf("%d 个工具轮事件的说明正文不属于产生它的那次迭代", drifted)
	}
	t.Logf("== 结论：全部 %d 个工具轮事件正文归属正确（按迭代归位，无跨轮漂移） ==", len(owned))
}

func lastIter(run *abRun, turn int) int {
	last := 1
	for index := range run.calls {
		if run.calls[index].turn == turn && run.calls[index].iter > last {
			last = run.calls[index].iter
		}
	}
	return last
}

// ── 动态事实的安置位置：头部块 vs 尾部片段 ─────────────────────────────

// TestContextStrategyAB_DynamicFactsPlacement 用**同一份动态内容**、**同一次
// 变化事件**（"相关记忆/世界状态重新选择"），只改变投影位置：
//
//	arm S：走生产装配器，记忆块渲染在累积 context 之前（头部），内容随本轮 query 变化；
//	arm C：指令/已发布内容不变，变化内容作为尾部片段写入。
//
// 度量两种安置方式下"本次变化让多少已发字节失效"。
func TestContextStrategyAB_DynamicFactsPlacement(t *testing.T) {
	candidates := []memory.Candidate{
		{SegmentID: "compact-s-1", From: 1, To: 6, Summary: "压缩段一：append-only prefix stable conclusion provider cache baseline。"},
		{SegmentID: "compact-s-2", From: 7, To: 14, Summary: "压缩段二：provider cache hit rate versus tool result size baseline。"},
		{SegmentID: "compact-s-3", From: 15, To: 22, Summary: "压缩段三：memory select lexical score with CJK bigram behavior。"},
		{SegmentID: "compact-s-4", From: 23, To: 30, Summary: "压缩段四：plan tail message and worktable trace block position。"},
	}
	options := memory.DefaultOptions()
	queryOne := "第 1 轮：provider cache hit rate versus tool result size。"
	queryTwo := "第 2 轮：memory select lexical score with CJK bigram and worktable position。"
	record := sessionstore.SessionContextRecord{
		SkillStack: []sessionstore.SkillFrame{{SkillID: "skill-review", Name: "review"}},
		CompactStack: []sessionstore.CompactFrame{{
			SegmentID: "compact-s-4", From: 23, To: 30, Summary: candidates[3].Summary,
		}},
	}
	harness := newPrefixProbeHarness(t, 1_000_000)
	system := systemPromptFor(harness.service)
	assembler := seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: func() string { return system },
		PrefixStacks: func() []seelectx.PromptBlock { return seelexctx.RenderStablePrefixBlocks(record) },
		Memories: func(_ context.Context, query string) []seelectx.PromptBlock {
			selected := memory.Select(query, candidates, options)
			if block := memory.RenderMemoryBlock(selected, options.MaxTokens); block != nil {
				return []seelectx.PromptBlock{*block}
			}
			return nil
		},
	})

	accumulated := probeAccumulatedHistory(40, 2400)
	requestOne := probeAssembleRequest(t, assembler, queryOne, accumulated)
	requestTwo := probeAssembleRequest(t, assembler, queryTwo, accumulated)

	blockOne := memory.RenderMemoryBlock(memory.Select(queryOne, candidates, options), options.MaxTokens)
	blockTwo := memory.RenderMemoryBlock(memory.Select(queryTwo, candidates, options), options.MaxTokens)

	t.Logf("== 动态事实安置对照：同一份内容 + 同一次「重新选择」事件 ==")
	t.Logf("  selection changed=%v memory block changed=%v (%d tok → %d tok)",
		probeSegmentIDs(memory.Select(queryOne, candidates, options))[0] != probeSegmentIDs(memory.Select(queryTwo, candidates, options))[0],
		probeBlockText(blockOne) != probeBlockText(blockTwo), probeBlockTokens(blockOne), probeBlockTokens(blockTwo))
	t.Logf("  arm S 装配投影序（生产装配器实际输出）: %s", abRoleSeq(requestOne.Messages))

	// arm S：记忆块在头部（累积 context 之前），内容变化 → 其后全部失效。
	bytesS1, bytesS2 := abSerializeAssembled(requestOne.Messages), abSerializeAssembled(requestTwo.Messages)
	sharedS := lcpBytes(bytesS1, bytesS2)
	tokS1, tokS2 := tokens.Count(bytesS1), tokens.Count(bytesS2)
	sharedTokS := tokens.Count(bytesS2[:sharedS])
	t.Logf("  arm S   : lcp=%d/%d bytes shared=%d/%d tok (上一请求 %d tok) → 失效 %d tok (%.1f%% of prompt)",
		sharedS, len(bytesS2), sharedTokS, tokS2, tokS1, tokS2-sharedTokS, 100*float64(tokS2-sharedTokS)/float64(tokS2))
	t.Logf("            （记忆块内容 = %d tok，重新选择后从头部起失效；其后累积 context 一并重新计费）",
		probeBlockTokens(memory.RenderMemoryBlock(memory.Select(queryTwo, candidates, options), options.MaxTokens)))

	// arm C：同一份内容渲染为尾部片段；指令与已发布内容不变。
	fragmentOne := probeBlockText(blockOne)
	fragmentTwo := probeBlockText(blockTwo)
	codexS1 := abSerializeAssembled(abCodexPlacementMessages(accumulated, fragmentOne, queryOne))
	codexS2 := abSerializeAssembled(abCodexPlacementMessages(accumulated, fragmentTwo, queryTwo))
	sharedC := lcpBytes(codexS1, codexS2)
	tokC1, tokC2 := tokens.Count(codexS1), tokens.Count(codexS2)
	sharedTokC := tokens.Count(codexS2[:sharedC])
	t.Logf("  arm C   : lcp=%d/%d bytes shared=%d/%d tok → 失效 %d tok (%.1f%% of prompt)",
		sharedC, len(codexS2), sharedTokC, tokC2, tokC2-sharedTokC, 100*float64(tokC2-sharedTokC)/float64(tokC2))
	t.Logf("            （同一片段内容 = %d tok；失效量 = 尾部片段 + 本轮查询，头部与已发布内容一字未动）",
		probeBlockTokens(blockTwo))
	t.Logf("  比值：同一事件下 arm S 失效 %.1f tok vs arm C 失效 %.1f tok（×%.1f）",
		float64(tokS2-sharedTokS), float64(tokC2-sharedTokC),
		float64(tokS2-sharedTokS)/float64(tokC2-sharedTokC))
	_ = tokC1
}

// abCodexPlacementMessages 是 arm C 的投影：常量指令 + 已发布累积 context +
// 尾部动态片段 + 本轮输入。动态片段按"仅变化时写尾部"的语义整段替换即可，
// 因为它始终在尾部，替换不动头部（Codex context_manager 的 merge_contextual_fragments）。
func abCodexPlacementMessages(accumulated []types.Message, fragment, query string) []types.Message {
	messages := make([]types.Message, 0, len(accumulated)+2)
	messages = append(messages, accumulated...)
	if fragment != "" {
		text := "[world/memory state]\n" + fragment
		messages = append(messages, types.Message{Role: "user", Content: &text})
	}
	messages = append(messages, types.Message{Role: "user", Content: &query})
	return messages
}

func abSerializeAssembled(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(serText("ROLE", message.Role))
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		builder.WriteString(serText("CONTENT", content))
	}
	return builder.String()
}

func abRoleSeq(messages []types.Message) string {
	parts := make([]string, 0, len(messages))
	for index, message := range messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		label := message.Role
		switch {
		case strings.Contains(content, "## 相关记忆"):
			label = "memory-block"
		case strings.Contains(content, "## 当前技能"):
			label = "skill/compact-block"
		case index == len(messages)-1:
			label = "current-input"
		}
		parts = append(parts, fmt.Sprintf("%d:%s", index, label))
	}
	return strings.Join(parts, " ")
}
