//go:build extended

package integration

import (
	"context"
	"fmt"
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	// 跨轮前缀发散探针（本地可复现，不发网络请求，不依赖真实 provider）。
	//
	// 目的：沿**生产装配路径**（task_context transcript → context_runtime
	// PrepareExecutionContextFor → replaceEngineHistory → PrepareProviderHistoryFor）
	// 驱动「第 1 轮含工具调用、第 2 轮继续」的两次会话回合，逐次 LLM 调用把
	// 「实际会发给 provider 的请求」固化为字节流，度量相邻请求的字节 LCP、
	// 首个差异消息（index/role）与失效后缀的 token 占比——这正是 provider 前缀
	// 缓存命中率的上限来源。
	//
	// 与既有 context_cache_smoke_test.go 的差别：冒烟用例用 appendSettledRound
	// 手工写入「已定稿轮次」（理想 append-only），本探针复刻**真实回合边界**：
	//
	//  1. Seele ReAct 循环 per-iteration 追加 assistant（tool_calls 时保留模型实际
	//     生成的 content/reasoning_content/tool_calls）与 tool 消息；
	//  2. OnLLMComplete 钩子（ToolHookBridge.Hooks）落 transcript assistant 事件，
	//     OnToolStart 钩子边界把本轮迭代的说明正文按迭代归位到该事件
	//     （AttributeToolNarrationLocked）；
	//  3. 回合结束的生产收尾两连：EnsureFinalAssistantTranscript →
	//     BackfillAssistantReasoning（说明正文的归位已前移，回合收尾不再事后填充）；
	//  4. 下一轮 PrepareExecutionContextFor 从 transcript 重建，并原样复现工具轮
	//     assistant 的模型输出。
	//
	// 请求序列化沿用冒烟用例的 serializeRequest（system 在头部、工具目录在尾部，
	// 两者跨轮常量），token 估算沿用生产同款 seelexctx/tokens。
	//
	// 运行：
	//
	//	go test ./application/assembly -run ContextCacheDivergenceProbe -v -count=1
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/memory"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
	"strings"
	"testing"
)

// ── H1/H2/H3：两回合（第 1 轮含工具调用）前缀发散 ────────────────────────

// TestContextCacheDivergenceProbe_TwoTurnToolCliff 驱动 1 个含工具调用的回合
// + 1 个纯文本回合并度量：
//   - H1：回合内多 ReAct 迭代是否保持 append-only（prev 字节是 cur 的前缀）；
//   - H2：跨回合重建是否在「尾之前」发生字节改变（失效上一轮整段后缀）；
//   - H3：工作打点表块（请求尾部的动态块）改变是否只影响尾部。
func TestContextCacheDivergenceProbe_TwoTurnToolCliff(t *testing.T) {
	configs := []prefixProbeConfig{
		{name: "A-production(keeps model output)", engineKeepsToolContent: true},
		{name: "D-regression(drops model output)", engineKeepsToolContent: false},
	}
	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) { runPrefixProbeTwoTurns(t, cfg) })
	}
}

func runPrefixProbeTwoTurns(t *testing.T, cfg prefixProbeConfig) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	turnOneInput := "第 1 轮：请核对 context_runtime 装配路径的前缀稳定性，并给出证据。"
	turnOneNarration := "我先读取装配入口与回合收尾代码，核对工具轮的前缀语义。"
	turnOneReply := "第 1 轮结论：装配把可复用段拼为保留前缀，工具轮正文需要单独核对。"
	toolResultOne := strings.Repeat("coordinator.go: PrepareExecutionContextFor 保留段重建路径证据行；", 120)
	toolResultTwo := strings.Repeat("chat.go: mergeStreamedToolNarration 视图正文合并证据行；", 80)

	t.Logf("== config %s ==", cfg.name)
	t.Logf("  constants excluded from byte LCP: system prompt %d tok, tools %d schemas",
		tokens.Count(systemPromptFor(harness.service)), len(harness.tools))
	harness.beginTurn(turnOneInput)
	harness.capture(1, 1, "t1.iter1")

	// 迭代 1：模型先输出说明文本（流式进视图），随后发起工具调用。
	harness.streamText(turnOneNarration)
	callOne := []types.ToolCall{{
		ID: "call-read-1", Type: "function",
		Function: types.ToolCallFunction{Name: "read_file", Arguments: `{"path":"application/assembly/context_runtime/coordinator.go"}`},
	}}
	engineContent := ""
	if cfg.engineKeepsToolContent {
		engineContent = turnOneNarration
	}
	harness.llmIteration(engineContent, "需要先看清装配顺序。", callOne)
	harness.toolRound("read_file", "call-read-1", `{"path":"application/assembly/context_runtime/coordinator.go"}`, toolResultOne)
	harness.capture(1, 2, "t1.iter2")

	// 迭代 2：第二个工具调用（该轮无说明文本）。
	callTwo := []types.ToolCall{{
		ID: "call-grep-2", Type: "function",
		Function: types.ToolCallFunction{Name: "grep_search", Arguments: `{"pattern":"mergeStreamedToolNarration"}`},
	}}
	harness.llmIteration("", "核对收尾三连。", callTwo)
	harness.toolRound("grep_search", "call-grep-2", `{"pattern":"mergeStreamedToolNarration"}`, toolResultTwo)
	harness.capture(1, 3, "t1.iter3")

	// 迭代 3：最终答复（同样流式进视图：视图正文 = 说明 + 答复的拼接）。
	harness.streamText(turnOneReply)
	harness.llmIteration(turnOneReply, "整理结论。", nil)
	harness.finishTurn(turnOneReply)

	// 回合间：工作打点表出现新条目（尾部动态块变化 → H3 观察点）。
	harness.runtime.tasks = map[string]dto.TaskRecord{
		"todo:1": {ID: "todo:1", Phase: dto.TaskPhaseTasklist, Task: "核对前缀失效区间", Status: dto.TaskRunning, Kind: "todo"},
	}

	// 第 2 轮：纯文本回合。
	harness.beginTurn("第 2 轮：继续，并说明上一轮工具结果的失效影响。")
	harness.capture(2, 1, "t2.iter1")
	// H2 直接证据：turn 2 装配出的「上一轮工具调用 assistant 消息」字节。
	for index, message := range harness.engine.History() {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			t.Logf("  H2 rebuilt msg#%d(assistant, tool_calls=%s) content=%q | streamed content=%q",
				index, probeCallIDs(message.ToolCalls), summarizeProbeText(message.Content), probeStreamedContent(cfg))
			break
		}
	}

	for index := 1; index < len(harness.calls); index++ {
		kind := "cross-turn"
		if harness.calls[index-1].turn == harness.calls[index].turn {
			kind = "within-turn"
		}
		logProbeDivergence(t, kind, prefixProbeCompare(harness.calls[index-1], harness.calls[index]))
	}

	// 跨回合：与「之前所有已发请求」的最长公共前缀（provider 可命中的上界）。
	reference := harness.calls[len(harness.calls)-2]
	best := prefixProbeCompare(reference, harness.calls[len(harness.calls)-1])
	for index := 0; index < len(harness.calls)-1; index++ {
		candidate := prefixProbeCompare(harness.calls[index], harness.calls[len(harness.calls)-1])
		if candidate.sharedTok > best.sharedTok {
			best = candidate
			reference = harness.calls[index]
		}
	}
	logProbeDivergence(t, "cross-max", best)

	// H3：尾部动态块（工作打点表）位置——块只在第 2 轮出现，位于请求尾部。
	last := harness.calls[len(harness.calls)-1]
	tailIndex := len(last.messages) - 1
	tailTokens := tokens.Count(last.messages[tailIndex].Content)
	t.Logf("  H3 tail: dynamic work-table block sits inside last message #%d(role=%s, %d chars / %d tok → %.2f%% of prompt)",
		tailIndex, last.messages[tailIndex].Role, len(last.messages[tailIndex].Content), tailTokens,
		100*float64(tailTokens)/float64(last.total))

	t.Logf("== summary %s: cross-turn reference=%s raw=%.1f%% @64=%.1f%% @1024=%.1f%% first_diff=msg#%d(%s) invalidated=%d tok (%.1f%% of %d) ==",
		cfg.name, reference.label, best.ratioRaw*100, best.ratio64*100, best.ratio1k*100,
		best.firstDiffIdx, best.firstDiffRole, best.invalidated, best.invalidShare*100, best.totalTok)
}

// ── H4：记忆块/稳定前缀栈块的位置与随查询变化 ────────────────────────────

// TestContextCacheDivergenceProbe_MemoryBlockPosition 用生产装配器
// （seelexctx.NewAssembler，与 assemble.seelexAssembler 同款选项接线）验证
// 「记忆块 + 稳定前缀栈块」投影顺序，并度量记忆块随当前查询变化时被失效的
// 字节/token 量。
func TestContextCacheDivergenceProbe_MemoryBlockPosition(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	candidates := []memory.Candidate{
		{SegmentID: "compact-s-1", From: 1, To: 6, Summary: "压缩段一：append-only prefix stable conclusion provider cache baseline。"},
		{SegmentID: "compact-s-2", From: 7, To: 14, Summary: "压缩段二：provider cache hit rate versus tool result size baseline。"},
		{SegmentID: "compact-s-3", From: 15, To: 22, Summary: "压缩段三：memory select lexical score with CJK bigram behavior。"},
		{SegmentID: "compact-s-4", From: 23, To: 30, Summary: "压缩段四：plan tail message and worktable trace block position。"},
	}
	options := memory.DefaultOptions()
	queryOne := "第 1 轮：provider cache hit rate versus tool result size。"
	queryTwo := "第 2 轮：memory select lexical score with CJK bigram and worktable position。"

	record := store.SessionContextRecord{
		SkillStack: []store.SkillFrame{{SkillID: "skill-review", Name: "review"}},
		CompactStack: []store.CompactFrame{{
			SegmentID: "compact-s-4", From: 23, To: 30, Summary: candidates[3].Summary,
		}},
	}
	assembler := seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: func() string { return systemPromptFor(harness.service) },
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

	selectedOne := memory.Select(queryOne, candidates, options)
	selectedTwo := memory.Select(queryTwo, candidates, options)
	blockOne := memory.RenderMemoryBlock(selectedOne, options.MaxTokens)
	blockTwo := memory.RenderMemoryBlock(selectedTwo, options.MaxTokens)
	t.Logf("== memory block (backend/engine/assemble/runtime_context.go relatedMemoryBlocks 同款接线) ==")
	t.Logf("  query1 selected=%v memory_block=%d tok", probeSegmentIDs(selectedOne), probeBlockTokens(blockOne))
	t.Logf("  query2 selected=%v memory_block=%d tok", probeSegmentIDs(selectedTwo), probeBlockTokens(blockTwo))
	t.Logf("  selection_changed=%v block_changed=%v", !probeSameSegments(selectedOne, selectedTwo), probeBlockText(blockOne) != probeBlockText(blockTwo))

	bytesOne, bytesTwo := probeSerializeAssembly(requestOne), probeSerializeAssembly(requestTwo)
	shared := lcpBytes(bytesOne, bytesTwo)
	t.Logf("  assembled order: memory block at msg#%d(%s), skill/compact prefix block at msg#%d(%s), accumulated context follows",
		probeMessageIndex(requestOne, "## 相关记忆"), probeRole(requestOne, "## 相关记忆"),
		probeMessageIndex(requestOne, "## 当前技能"), probeRole(requestOne, "## 当前技能"))
	if shared < len(bytesTwo) {
		t.Logf("  divergence starts inside msg#%d(%s) — memory block sits BEFORE the accumulated context, so a selection change invalidates everything after it",
			probeMessageIndexByBytes(requestOne, shared), probeRoleByBytes(requestOne, shared))
	}
	sharedTokens := tokens.Count(bytesTwo[:shared])
	totalTokens := tokens.Count(bytesTwo)
	t.Logf("  memory-block-change cost: lcp=%d/%d bytes (%.1f%% into prompt) shared=%d tok total=%d tok invalidated=%d tok (%.1f%%)",
		shared, len(bytesTwo), 100*float64(shared)/float64(len(bytesTwo)),
		sharedTokens, totalTokens, totalTokens-sharedTokens, 100*float64(totalTokens-sharedTokens)/float64(totalTokens))
}

func probeAccumulatedHistory(messages, charsPerMessage int) []types.Message {
	history := make([]types.Message, 0, messages+1)
	for index := 0; index < messages; index++ {
		content := fmt.Sprintf("第 %d 段已定稿上下文：%s", index, strings.Repeat("前缀保持稳定；", charsPerMessage/8+1))
		history = append(history, types.Message{Role: "assistant", Content: &content})
	}
	return history
}

func probeAssembleRequest(t *testing.T, assembler seelectx.RequestAssembler, query string, accumulated []types.Message) seelectx.AssembledRequest {
	t.Helper()
	working := append([]types.Message(nil), accumulated...)
	working = append(working, types.Message{Role: "user", Content: &query})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: working})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	return assembled
}

func probeSerializeAssembly(request seelectx.AssembledRequest) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		builder.WriteString(serText("ROLE", message.Role))
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		builder.WriteString(serText("CONTENT", content))
	}
	return builder.String()
}

func probeMessageIndex(request seelectx.AssembledRequest, marker string) int {
	for index, message := range request.Messages {
		if message.Content != nil && strings.Contains(*message.Content, marker) {
			return index
		}
	}
	return -1
}

func probeRole(request seelectx.AssembledRequest, marker string) string {
	index := probeMessageIndex(request, marker)
	if index < 0 {
		return "absent"
	}
	return request.Messages[index].Role
}

func probeMessageIndexByBytes(request seelectx.AssembledRequest, offset int) int {
	position := 0
	for index, message := range request.Messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		position += len(serText("ROLE", message.Role)) + len(serText("CONTENT", content))
		if offset < position {
			return index
		}
	}
	return len(request.Messages) - 1
}

func probeRoleByBytes(request seelectx.AssembledRequest, offset int) string {
	index := probeMessageIndexByBytes(request, offset)
	if index < 0 || index >= len(request.Messages) {
		return "absent"
	}
	return request.Messages[index].Role
}

func probeBlockTokens(block *seelectx.PromptBlock) int {
	if block == nil {
		return 0
	}
	return tokens.Count(probeBlockText(block))
}

func probeBlockText(block *seelectx.PromptBlock) string {
	if block == nil {
		return ""
	}
	var builder strings.Builder
	for _, message := range block.Messages {
		if message.Content != nil {
			builder.WriteString(*message.Content)
		}
	}
	return builder.String()
}

func probeSegmentIDs(selected []memory.Candidate) []string {
	ids := make([]string, 0, len(selected))
	for _, candidate := range selected {
		ids = append(ids, candidate.SegmentID)
	}
	return ids
}

func probeSameSegments(left, right []memory.Candidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].SegmentID != right[index].SegmentID {
			return false
		}
	}
	return true
}
