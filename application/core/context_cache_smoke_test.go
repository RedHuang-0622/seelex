package core

// 上下文前缀缓存冒烟测试（本地可复现，不发网络请求）。
//
// 目标：沿生产装配路径（task_context 注册表 transcript → context_runtime
// PrepareExecutionContextFor → replaceEngineHistory）驱动一个"单会话、多轮、
// 长会话"的执行序列，逐轮把"实际会发给 provider 的请求"固化为本地 trace，
// 再按前缀缓存模型在本地计算命中率：
//
//	hit_i = LCP(request_i, request_{i-1}) 的 token 数 / request_i 的 token 数
//
// 粒度：raw（token 估算）、64-token 块、1024-token 块（对齐真实 provider
// 按块缓存的语义，如 Anthropic ≥1024 / DeepSeek 64 token 对齐）。
//
// 说明：系统提示词与工具 schema 由 provider 序列化在 message 之前或与其
// 共同构成前缀；本测试把 system 放在串行化头部、工具 schema 放尾部（生产
// harness 中工具列表恒定），因此除"常数偏移"外不改变命中率动力学。历史消息
// 用字段级长度前缀串行化，保证字节级可判定；shared 的 token 用生产同款
// seelexctx/tokens 估算器对公共前缀文本计数（估算误差对两侧同向，比值结论
// 不受影响）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
)

// smokeTraceRow 是单轮请求的本地 trace 与命中指标。
type smokeTraceRow struct {
	round     int
	history   int
	totalTok  int // 生产估算 CountRequestTokens（system+history+input+tools）
	sharedTok int // 与上一轮请求公共前缀的 token 数
	ratioRaw  float64
	ratio64   float64
	ratio1k   float64
	cliff     string // 命中备注（如 skill/plan 引入）
}

// smokeService 构建一个可驱动生产装配路径的服务。
func smokeService(t *testing.T, window int) (*Service, *fakeEngine) {
	t.Helper()
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: window, output: 8192,
	}))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = engine.SessionID()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "smoke-task-1"}
	service.components.tasks.BeginTask("smoke-task-1", "smoke multi-turn", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	return service, engine
}

// appendSettledRound 追加一个"已定稿轮次"的 transcript 事件（user → assistant
// 工具调用 → tool 结果 → assistant 正文），模拟单轮 ReAct 完整结束。调用方需
// 已持有 ViewMu（与生产 AppendTranscriptEventLocked 契约一致）。
func appendSettledRound(service *Service, round int, toolResultToken int) {
	input := fmt.Sprintf("round-%d 目标：核对模块 X 的 append-only 前缀假设，并给出验证结论与冒烟测试。", round)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "smoke-task-1", Role: "user", Content: input,
	})
	callID := fmt.Sprintf("smoke-call-%d", round)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "smoke-task-1", Role: "assistant",
		Content: fmt.Sprintf("我先读取 round-%d 相关文件并核对边界。", round),
		ToolCalls: []TranscriptToolCall{{ID: callID, Name: "read_file",
			Arguments: fmt.Sprintf(`{"path":"module/round-%d/source.go","note":"stable"}`, round)}},
	})
	body := strings.Repeat(fmt.Sprintf("file-round-%d line ", round), toolResultToken)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "smoke-task-1", Role: "tool", ToolCallID: callID, Name: "read_file", Content: body,
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "smoke-task-1", Role: "assistant",
		Content: fmt.Sprintf("round-%d 结论：前缀保持稳定、追加部分仅为本轮输出，建议进入下一轮核对。", round),
	})
}

// systemPromptFor 取当前 system（与 Prepare 内部使用的同一组装函数）。
func systemPromptFor(service *Service) string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.components.prompts.SystemPromptForActiveTaskLocked()
}

// serText 生成 JSON 前缀友好串行化片段（固定键序、无长度前缀；内容前缀相等
// ⇒ 序列化前缀相等，避免长度字段在内容变化时提前截断字节级 LCP）。
func serText(label, text string) string {
	buffer, _ := json.Marshal(struct {
		Label string `json:"l"`
		Text  string `json:"t"`
	}{label, text})
	return string(buffer)
}

// serializeRequest 把一次请求（system + history + current input + tools）固化为
// 确定性字节流（本地 trace 的最小事实单位）。历史消息按字段序 JSON 化，前缀
// 相等性即内容前缀相等性。
func serializeRequest(system string, history []EngineMessage, input string, tools []Tool) string {
	var builder strings.Builder
	builder.WriteString(serText("SYSTEM", system))
	for index := range history {
		message := &history[index]
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
	builder.WriteString("\n||\n")
	builder.WriteString(serText("INPUT", input))
	for _, tool := range tools {
		builder.WriteString("\n@TOOL@")
		builder.WriteString(serText(tool.Name, tool.Description))
	}
	return builder.String()
}

// lcpBytes 返回两个字节串的最长公共前缀长度。
func lcpBytes(a, b string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

// blockFloor 按缓存块粒度对齐（provider 只命中整块）。
func blockFloor(shared, block int) int {
	return (shared / block) * block
}

// measure 计算本轮与上一轮请求的命中指标（block 粒度按 provider 整块缓存语义
// 对齐；shared 用生产同款 seelexctx/tokens 估算器对公共前缀文本计数）。
func measure(prev, current string, totalTok int) (shared int, ratioRaw, ratio64, ratio1k float64) {
	shared = tokens.Count(current[:lcpBytes(prev, current)])
	if totalTok > 0 {
		ratioRaw = float64(shared) / float64(totalTok)
		ratio64 = float64(blockFloor(shared, 64)) / float64(totalTok)
		ratio1k = float64(blockFloor(shared, 1024)) / float64(totalTok)
	}
	return shared, ratioRaw, ratio64, ratio1k
}

// smokeCacheHitRow 汇总输出一行。
func logSmokeRow(t *testing.T, row smokeTraceRow, input string) {
	t.Logf("turn=%2d historyMsgs=%3d total=%6d shared=%6d raw=%5.1f%% @64=%5.1f%% @1024=%5.1f%%  %s",
		row.round, row.history, row.totalTok, row.sharedTok,
		row.ratioRaw*100, row.ratio64*100, row.ratio1k*100, input)
}

// TestContextCacheSmoke_SingleSessionLongRun 复现"单会话多轮长会话"的最优可达
// 命中率：系统提示词与技能表稳定、每轮只在尾部追加已定稿轮次与当前输入。
func TestContextCacheSmoke_SingleSessionLongRun(t *testing.T) {
	service, engine := smokeService(t, 1_000_000)
	tools := service.Deps.Runtime.VisibleTools(context.Background())
	const rounds = 16

	var previous string
	var previousRatio float64
	aggregateShared, aggregateTotal := 0, 0
	steadyCount, steadySum := 0, 0.0
	for round := 1; round <= rounds; round++ {
		input := fmt.Sprintf("第 %d 轮：请继续对本会话做整体审查——核对前缀稳定性、命中率与降级路径。", round)

		// 生产顺序：Submit 追加当前 user 事件 → PrepareExecutionContextFor 装配。
		service.ViewMu.Lock()
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "smoke-task-1", Role: "user", Content: input,
		})
		service.ViewMu.Unlock()
		if _, err := service.components.context.PrepareExecutionContext("smoke-task-1", input); err != nil {
			t.Fatalf("round %d prepare: %v", round, err)
		}

		history := engine.History()
		system := systemPromptFor(service)
		current := serializeRequest(system, history, input, tools)
		total := service.components.tasks.CountRequestTokens(system, history, input, tools)
		row := smokeTraceRow{round: round, history: len(history), totalTok: total}
		if previous != "" {
			row.sharedTok, row.ratioRaw, row.ratio64, row.ratio1k = measure(previous, current, total)
		}
		logSmokeRow(t, row, "steady")

		aggregateShared += row.sharedTok
		aggregateTotal += row.totalTok
		if round >= 4 {
			steadyCount++
			steadySum += row.ratioRaw
		}
		if round >= 8 && row.ratioRaw < 0.85 {
			t.Errorf("turn %d raw hit %.3f < 0.85 (long-session append-only should keep prefix)", round, row.ratioRaw)
		}
		if previousRatio > row.ratioRaw+0.02 && round >= 4 {
			t.Errorf("turn %d raw hit %.3f dropped vs previous %.3f (append-only prefix should not regress)", round, row.ratioRaw, previousRatio)
		}
		if round > 3 {
			previousRatio = row.ratioRaw
		}
		if round == rounds && (row.ratioRaw < 0.90 || row.ratio1k < 0.88) {
			t.Errorf("final turn raw=%.3f 1024-block=%.3f, want >=0.90/0.88", row.ratioRaw, row.ratio1k)
		}
		previous = current

		// 已定稿轮次落 transcript（下一轮装配可 append-only 追加）。
		service.ViewMu.Lock()
		appendSettledRound(service, round, 900)
		service.ViewMu.Unlock()
	}

	t.Logf("== aggregate steady: shared=%d total=%d ratio=%.4f | turn>=4 raw avg=%.4f ==",
		aggregateShared, aggregateTotal,
		float64(aggregateShared)/float64(aggregateTotal),
		steadySum/float64(steadyCount))
	if steadyCount == 0 || steadySum/float64(steadyCount) < 0.84 {
		t.Fatalf("steady raw average hit %.3f < 0.84", steadySum/float64(steadyCount))
	}
}

// TestContextCacheSmoke_SkillActivationAppendOnly 对照场景：会话中段激活一个
// 任务级 Skill（TrustedSkillLayers 变化）。技能正文按 append-only 语义作为一条
// internal user 轮次追加进 transcript 尾部（task_context 落事件，不再是
// system/装配头部的重建块）——激活只是尾部新增，前缀（system + 已定稿轮次）
// 一字未动 → 无缓存悬崖，激活轮命中率保持高位；system 保持字节稳定且不含
// 技能正文。后续轮次稳态命中恢复并单调增长。
func TestContextCacheSmoke_SkillActivationAppendOnly(t *testing.T) {
	service, engine := smokeService(t, 1_000_000)
	tools := service.Deps.Runtime.VisibleTools(context.Background())
	const activationTurn = 7

	// 激活技能用的正文（模拟 review 等长指令）。
	skillText := strings.Repeat("review 指令：必须给出文件级证据并区分 Confirmed/Hypothesis。", 120)

	var previous string
	var preActivationSystem string
	skillSeen := false
	t.Logf("turn        | <激活 skill@%d>", activationTurn)
	for round := 1; round <= 12; round++ {
		if round == activationTurn {
			service.ViewMu.Lock()
			state := service.components.tasks.CurrentTaskExecution()
			if state == nil || state.RequestID != "smoke-task-1" {
				service.ViewMu.Unlock()
				t.Fatalf("task execution missing at turn %d", round)
			}
			service.components.tasks.ActivateTaskSkillsLocked(state, []prompt.PromptLayer{{
				Kind: "skill", Name: "review", Text: skillText,
			}})
			service.ViewMu.Unlock()
		}

		input := fmt.Sprintf("第 %d 轮：继续会话内核对。", round)
		service.ViewMu.Lock()
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "smoke-task-1", Role: "user", Content: input,
		})
		service.ViewMu.Unlock()
		if _, err := service.components.context.PrepareExecutionContext("smoke-task-1", input); err != nil {
			t.Fatalf("round %d prepare: %v", round, err)
		}

		history := engine.History()
		system := systemPromptFor(service)
		if round == activationTurn-1 {
			preActivationSystem = system
		}
		if round == activationTurn && (system != preActivationSystem || strings.Contains(system, "## Trusted Active Skill")) {
			t.Fatalf("activation must not rewrite system: before=%q after=%q", preActivationSystem, system)
		}
		for _, message := range history {
			if message.Role == "user" && strings.Contains(message.Content, "## Trusted Active Skill: review") {
				skillSeen = true
			}
		}
		current := serializeRequest(system, history, input, tools)
		total := service.components.tasks.CountRequestTokens(system, history, input, tools)
		row := smokeTraceRow{round: round, history: len(history), totalTok: total}
		if previous != "" {
			row.sharedTok, row.ratioRaw, row.ratio64, row.ratio1k = measure(previous, current, total)
		}
		note := "steady"
		if round == activationTurn {
			note = "SKILL-ACTIVATED(append-only 尾部追加, 前缀未动)"
		}
		if round > activationTurn {
			note = "post-activation steady"
		}
		logSmokeRow(t, row, note)

		// 激活轮不得悬崖：技能事件只是尾部新增 → 与上一轮共享前缀保持高位。
		if round == activationTurn && (row.ratioRaw < 0.7 || row.sharedTok < 15000) {
			t.Errorf("turn %d raw hit %.3f shared %d: activation must not cliff under append-only", round, row.ratioRaw, row.sharedTok)
		}
		// 激活后第一轮分母含新增技能正文，允许 0.80 的过渡带；此后恢复稳态高位。
		if round == activationTurn+1 && row.ratioRaw < 0.8 {
			t.Errorf("turn %d raw hit %.3f < 0.80, expected high hit after tail append", round, row.ratioRaw)
		}
		if round > activationTurn+1 && row.ratioRaw < 0.85 {
			t.Errorf("turn %d raw hit %.3f < 0.85, expected steady high hit", round, row.ratioRaw)
		}
		previous = current

		service.ViewMu.Lock()
		appendSettledRound(service, round, 600)
		service.ViewMu.Unlock()
	}
	if !skillSeen {
		t.Fatal("activated skill body must appear in assembled provider history as an internal turn")
	}
}

// TestContextCacheSmoke_ProviderCacheContentionModel 是一个显式标注的推演模型：
// 用单会话 trace 的实际 token 形状，估算"多会话并行 + 较少长会话"在 provider
// 前缀缓存按最近使用会话 LRU 保留 L 个条目时的聚合命中率。它不模拟真实
// provider（TTL/容量/字节精确匹配未知），只做结构性定量说明。
func TestContextCacheSmoke_ProviderCacheContentionModel(t *testing.T) {
	// 形状来自单会话长会话实测数量级：总 ~140k、共享 ~139k（稳态 ~99%）。
	perSessionTokens := []int{80_000, 40_000, 20_000, 10_000, 6_000, 4_000, 3_000, 2_000}
	sharedTokens := 0.993
	t.Logf("== 结构性推演：provider 前缀缓存只保留最近 L 个活跃会话前缀 ==")
	t.Logf("sessions(K) | per-session prefix retain L -> aggregate hit (token-weighted)")
	for _, K := range []int{1, 2, 3, 4, 6, 8} {
		for _, L := range []int{1, 2, 3, 6} {
			if L > K {
				continue
			}
			total, hit := 0, 0.0
			tokensBySession := make([]int, K)
			for s := 0; s < K; s++ {
				if s < len(perSessionTokens) {
					tokensBySession[s] = perSessionTokens[s]
				} else {
					tokensBySession[s] = 2_000
				}
				total += tokensBySession[s]
			}
			// 稳态轮转下，某会话上一轮前缀在下轮回来前经历 K-1 次其它会话写；
			// 容量 L 的 LRU 只在 K <= L 时必定保留，否则命中概率 ≈ L/(K-1)（其余
			// 情况该会话前缀已被逐出；另假设不同会话前缀互不共享——保守上界）。
			for s := 0; s < K; s++ {
				presence := 1.0
				if K > L {
					presence = float64(L) / float64(K-1)
					if presence > 1 {
						presence = 1
					}
				}
				hit += float64(tokensBySession[s]) * sharedTokens * presence
			}
			t.Logf("K=%-2d L=%-2d -> %6.1f%%", K, L, hit/float64(total)*100)
		}
	}
}
