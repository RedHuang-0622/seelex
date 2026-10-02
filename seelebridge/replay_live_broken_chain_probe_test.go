//go:build replayprobe

// 真账号探针（build tag `replayprobe` + `SEELEX_REPLAY_PROBE=1` 双门禁，默认跳过）：
// 证明「前缀重放素材没做 wire 协议规整」就是
// 现场那个 400 的充分原因，并证明规整后**同一素材**可以成功重放。
//
// 现场（2026-09-29 18:06 / 19:18 两次装配层压缩）的 provider 报文：
//
//	HTTP 400 An assistant message with 'tool_calls' must be followed by tool
//	messages responding to each 'tool_call_id'. (insufficient tool messages
//	following tool_calls message)
//
// 形状来源：会话在工具轮被中断（assistant 宣告了调用、结果没记录），冷加载后
// 引擎历史里留着这条未回执的声明；装配层在"请求出口修复之前"读到它，原样送进
// 重放请求。
//
// 五个臂（A 臂直接发**未规整**的字节序，复现 400 原文；B 臂发规整后的字节序；
// C 臂走生产入口 seelexctx 摘要器，验证修复已在链路里生效）：
//
//	A1-未规整-尾单元未落定      → 期望失败（400 原文）
//	A2-未规整-中段死链          → 期望失败（400 原文）
//	B1-规整后-尾单元丢掉了      → 期望成功
//	B2-规整后-中段补回执占位    → 期望成功
//	C1/C2-生产入口（摘要器）    → 期望成功
//
// 运行：
//
//	$env:SEELEX_REPLAY_PROBE='1'; go test -tags replayprobe ./seelebridge/ -run TestReplayProbeBrokenToolChainShape -v -count=1 -timeout 12m
package seelebridge

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

func TestReplayProbeBrokenToolChainShape(t *testing.T) {
	if os.Getenv("SEELEX_REPLAY_PROBE") == "" {
		t.Skip("set SEELEX_REPLAY_PROBE=1 to run the broken-tool-chain replay probe")
	}
	limitsPath := probeEnvPath("SEELEX_PROBE_LIMITS", `G:\Program\go\seelex\config\seelex.yaml`)
	accountsPath := probeEnvPath("SEELEX_PROBE_ACCOUNTS", `G:\Program\go\seelex\config\accounts.yaml`)

	limits, err := seelexctx.LoadLimits(limitsPath)
	if err != nil {
		t.Fatalf("加载 limits 失败: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath: accountsPath,
		StorePath:    t.TempDir(),
		Limits:       limits,
	})
	if err != nil {
		t.Fatalf("构造 Runtime 失败: %v", err)
	}
	defer runtime.Shutdown()
	t.Logf("provider=%q accounts=%d", runtime.Provider(), len(runtime.Accounts()))

	raw, err := seelectx.NewQuickChat(runtime.completer)
	if err != nil || raw == nil {
		t.Fatalf("构造裸 QuickChat 失败: %v", err)
	}
	summarizer := runtime.compactionSummarizer()
	if summarizer == nil {
		t.Fatalf("摘要器为 nil（开关关闭或 QuickChat 装配失败）")
	}

	// 现场形状：assistant 宣告 t1/t2，只有 t1 落上了结果。
	tailBroken := []types.Message{
		{Role: "user", Content: probeText("任务1：核对装配路径，并给出证据。")},
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "call-read-1", Type: "function", Function: types.ToolCallFunction{Name: "read_file"}},
			{ID: "call-grep-2", Type: "function", Function: types.ToolCallFunction{Name: "grep_search"}},
		}},
		{Role: "tool", ToolCallID: "call-read-1", Name: "read_file", Content: probeText("coordinator.go: PrepareExecutionContextFor ...")},
	}
	// 中段死链：残缺声明之后还有别的消息（不可能是"正在跑"）。
	midBroken := append(append([]types.Message(nil), tailBroken...),
		types.Message{Role: "assistant", Content: probeText("已完成第一步，继续。")},
		types.Message{Role: "user", Content: probeText("任务2：继续。")},
	)

	// 与生产同源：system 同字节 + History + 固定压缩指令尾巴。
	wire := func(system string, history []types.Message) []types.Message {
		messages := make([]types.Message, 0, len(history)+2)
		if system != "" {
			messages = append(messages, types.Message{Role: "system", Content: probeText(system)})
		}
		messages = append(messages, history...)
		messages = append(messages, types.Message{Role: "user", Content: probeText(seelexctx.PrefixReplayInstruction)})
		return messages
	}
	const system = "你是 Seelex 助手。"

	send := func(label string, messages []types.Message) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		started := time.Now()
		reply, err := raw.Complete(ctx, seelectx.QuickChatRequest{Messages: messages})
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			t.Logf("%-28s 失败（%s）err=%v", label, elapsed, err)
			return false
		}
		content := ""
		if reply.Content != nil {
			content = strings.TrimSpace(*reply.Content)
		}
		if len(content) > 80 {
			content = content[:80] + "…"
		}
		t.Logf("%-28s 成功（%s）reply=%q", label, elapsed, content)
		return true
	}

	// ── A 臂：未规整的字节序（复现现场 400）────────────────────────────
	a1 := send("A1-未规整-尾单元未落定", wire(system, tailBroken))
	a2 := send("A2-未规整-中段死链", wire(system, midBroken))
	if a1 || a2 {
		t.Logf("注意：provider 接受了未规整的素材（A 臂未复现 400）——请核对报文里 tool 配对的校验口径")
	}

	// ── B 臂：规整后的同一素材 ────────────────────────────────────────
	tailMaterial, tailReport := seelexctx.PrepareReplayMaterial(tailBroken)
	t.Logf("尾单元未落定 → 规整报告：%s", tailReport.Terse())
	if err := seelexctx.ValidateReplayProtocol(tailMaterial); err != nil {
		t.Fatalf("规整后的尾单元素材仍不合法: %v", err)
	}
	midMaterial, midReport := seelexctx.PrepareReplayMaterial(midBroken)
	t.Logf("中段死链 → 规整报告：%s", midReport.Terse())
	if err := seelexctx.ValidateReplayProtocol(midMaterial); err != nil {
		t.Fatalf("规整后的中段素材仍不合法: %v", err)
	}
	b1 := send("B1-规整后-尾单元丢掉", wire(system, tailMaterial))
	b2 := send("B2-规整后-中段补回执", wire(system, midMaterial))

	// ── C 臂：生产入口（摘要器自己规整）────────────────────────────────
	through := func(label string, history []types.Message) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		started := time.Now()
		result, err := summarizer.Summarize(ctx, seelexctx.ReplayRequest{
			SystemPrompt: system,
			History:      history,
			MaxTokens:    2048,
		})
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			t.Logf("%-28s 失败（%s）err=%v", label, elapsed, err)
			return false
		}
		body := strings.TrimSpace(result.Chapter2)
		if len(body) > 80 {
			body = body[:80] + "…"
		}
		t.Logf("%-28s 成功（%s）chapter2=%q", label, elapsed, body)
		return true
	}
	c1 := through("C1-生产入口-尾单元未落定", tailBroken)
	c2 := through("C2-生产入口-中段死链", midBroken)

	t.Logf("== 结论：未规整 A1=%t A2=%t（期望 false）；规整后 B1=%t B2=%t（期望 true）；生产入口 C1=%t C2=%t（期望 true）==",
		a1, a2, b1, b2, c1, c2)
}
