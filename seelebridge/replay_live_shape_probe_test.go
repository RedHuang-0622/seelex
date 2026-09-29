//go:build replayprobe

// 真账号探针（build tag `replayprobe` + `SEELEX_REPLAY_PROBE=1` 双门禁，默认跳过）：
// 用真实账号复现「前缀重放厚摘要」这条调用的
// 请求形态，把摘要器的**真实报错**打出来。
//
// 背景：18:06:19 那次装配层折叠（新进程、已读到 enabled: true）折出的帧仍是
// summary_source=local，而它自己的回执写着 `index 458ms`——比"纯本地折叠"的
// 12~23ms 大二十倍，又远小于一次 ~10 万 token 模型调用该有的耗时。这指向
// "一次或两次**很快失败**的重放调用"，但错误文本被 chapter2Node 吞掉了
// （两次尝试都失败 → 静默落本地折叠）。
//
// 运行：
//
//	$env:SEELEX_REPLAY_PROBE='1'; go test -tags replayprobe ./seelebridge/ -run TestReplayProbeShape -v -count=1 -timeout 12m
package seelebridge

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

func probeText(text string) *string { return &text }

// probeHistory 造一段约 targetTokens 的历史（按英文 4 字符/token 计）。
func probeHistory(targetTokens int, leadingSystem string) []types.Message {
	messages := make([]types.Message, 0, 64)
	if leadingSystem != "" {
		messages = append(messages, types.Message{Role: "system", Content: probeText(leadingSystem)})
	}
	chunk := strings.Repeat("the fold replays the real request prefix so the paid call shares bytes; ", 90) // ~2.3k chars ≈ 570 tokens
	for used := 0; used < targetTokens; used += 600 {
		messages = append(messages,
			types.Message{Role: "user", Content: probeText("轮次材料：\n" + chunk)},
			types.Message{Role: "assistant", Content: probeText("收到。\n" + chunk)},
		)
	}
	return messages
}

func TestReplayProbeShape(t *testing.T) {
	if os.Getenv("SEELEX_REPLAY_PROBE") == "" {
		t.Skip("set SEELEX_REPLAY_PROBE=1 to run the replay-shape probe")
	}
	limitsPath := probeEnvPath("SEELEX_PROBE_LIMITS", `G:\Program\go\seelex\config\seelex.yaml`)
	accountsPath := probeEnvPath("SEELEX_PROBE_ACCOUNTS", `G:\Program\go\seelex\config\accounts.yaml`)

	limits, err := seelexctx.LoadLimits(limitsPath)
	if err != nil {
		t.Fatalf("加载 limits 失败: %v", err)
	}
	t.Logf("limits: enabled=%v input_tokens=%d chapter2_tokens=%d frame_carry=%d",
		limits.ContextCompactionSummary.Enabled,
		limits.ContextCompactionSummary.InputTokens,
		limits.ContextCompactionSummary.Chapter2Tokens,
		limits.ContextFrameCarryTokens)

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

	summarizer := runtime.compactionSummarizer()
	if summarizer == nil {
		t.Fatalf("摘要器为 nil（开关关闭或 QuickChat 装配失败）——这就是 local 折叠的一种原因")
	}
	t.Logf("摘要器就位：%T", summarizer)

	const shortSystem = "你是 Seelex 助手。"
	base := []types.Message{
		{Role: "user", Content: probeText("你好")},
		{Role: "assistant", Content: probeText("在的。")},
	}
	bigSystem := "你是 Seelex 助手。\n" + strings.Repeat("stable prefix block: skills/plugins/compact stack; ", 700) // ~1 万 token
	duplicated := append([]types.Message{{Role: "system", Content: probeText(shortSystem)}}, base...)

	// 工具面：与生产同源（可见工具）。裸 Runtime 未开回合时可能为 nil，那就只证明
	// "不带工具的形态"。
	var toolFace []types.Tool
	if runtime.agt != nil {
		toolFace = runtime.agt.VisibleTools(context.Background())
	}
	t.Logf("可见工具面：%d 个", len(toolFace))

	run := func(name string, system string, history []types.Message, tools []types.Tool) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		started := time.Now()
		result, err := summarizer.Summarize(ctx, seelexctx.ReplayRequest{
			SystemPrompt: system,
			History:      history,
			Tools:        tools,
			MaxTokens:    2048,
		})
		elapsed := time.Since(started)
		if err != nil {
			t.Logf("%s: 失败（%s）err=%v", name, elapsed.Round(time.Millisecond), err)
			return false
		}
		body := strings.TrimSpace(result.Chapter2)
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		t.Logf("%s: 成功（%s）chapter2=%q", name, elapsed.Round(time.Millisecond), body)
		return true
	}

	// P1/P2 只花几十 token，用它们把"形状"与"大小"分开。
	run("P1-小历史-单system", shortSystem, base, nil)
	run("P2-小历史-system重复(生产形态)", shortSystem, duplicated, nil)

	// P3：生产规模（~10 万 token 历史 + 1 万 token system + 重复 system）。
	big := probeHistory(100000, bigSystem)
	t.Logf("P3 材料：%d 条消息（约 10 万 token）", len(big))
	if !run("P3-十万token-重复system-无工具", bigSystem, big, nil) {
		t.Logf("P3 已给出失败文本，跳过带工具面的 P4")
		return
	}
	if len(toolFace) > 0 {
		run("P4-十万token-重复system-带工具面", bigSystem, big, toolFace)
	} else {
		t.Logf("P4 跳过：裸 Runtime 无可见工具面")
	}
	_ = fmt.Sprint()
}

func probeEnvPath(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
