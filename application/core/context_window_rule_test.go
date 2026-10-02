package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_control"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// 上下文压缩的保留窗口规则（seele.yaml window 段）：
//
//	软压缩：保留前缀 token 预算 = min(token1, token2)
//	        token1 = retain_tokens（配置里硬编码的保留窗口 token 数）
//	        token2 = ratio × 全量上下文 token 数（占比窗口）
//	硬压缩：all_context ≥ force_compact_tokens 即必须自主压缩（不等比例阈值）
//
// 窗口外部分尽数交给 compact_context 压缩：会话里留下压缩记录，记录带**被压
// 区间**（事件序号 + 消息号），边界取自窗口决策本身而不是事后推算。

// applyWindowConfig 应用 window 段并在测试结束恢复（进程内生效配置）。
func applyWindowConfig(t *testing.T, config WindowConfig) {
	t.Helper()
	previous := context_control.Current()
	ApplyWindowConfig(config)
	t.Cleanup(func() { context_control.Apply(previous) })
}

// appendWindowRounds 追加 rounds 个已定稿轮次（短问 + 长答），返回每轮标记。
// 每轮约占 answerChars/4 个 token（ASCII）。
func appendWindowRounds(t *testing.T, service *Service, requestID string, rounds, answerChars int) []string {
	t.Helper()
	marks := make([]string, 0, rounds)
	service.ViewMu.Lock()
	for index := 0; index < rounds; index++ {
		mark := fmt.Sprintf("round-%02d", index)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "user", Content: "q-" + mark,
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "assistant", Content: mark + ":" + strings.Repeat("A", answerChars),
		})
		marks = append(marks, mark)
	}
	service.ViewMu.Unlock()
	return marks
}

// retainedRounds 统计仍留在 provider 历史里的轮次数（按轮标记匹配）。
func retainedRounds(history []EngineMessage, marks []string) int {
	kept := 0
	for _, mark := range marks {
		needle := mark + ":"
		for _, message := range history {
			if strings.Contains(message.Content, needle) {
				kept++
				break
			}
		}
	}
	return kept
}

// compactNow 走显式压缩落点（/compact、compact_context 同一条路径），返回
// 本次压缩记录——自动路径受"同一批进展只压一次"节流，显式路径不受。
func compactNow(t *testing.T, service *Service, sessionID string) ContextCompaction {
	t.Helper()
	outcome, err := service.components.context.CompactContextNow(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if outcome.Outcome != context_runtime.CompactDone {
		t.Fatalf("压缩结果 = %v（want compacted）", outcome.Outcome)
	}
	return outcome.Record
}

// TestCompactRetainedPrefixIsMinOfTwoWindows：保留前缀 = min(token1, token2)。
// 收紧 token1（retain_tokens）或收紧 token2（ratio）都只保留最新 1 轮；两个
// 窗口都宽时保留多轮、但占比窗口仍截断超出的旧轮次——证明取的是较小者。
func TestCompactRetainedPrefixIsMinOfTwoWindows(t *testing.T) {
	const (
		rounds      = 16
		answerChars = 32_000 // ≈ 8000 token/轮
	)
	cases := []struct {
		name     string
		config   WindowConfig
		wantKept int // -1 = 只断言区间（1 < kept < rounds）
	}{
		{name: "两个窗口都宽：占比窗口 token2 截断旧轮次", config: WindowConfig{RetainTokens: 500_000, Ratio: 0.7}, wantKept: -1},
		{name: "token1 收紧：配置保留窗口是较小者", config: WindowConfig{RetainTokens: 9_000, Ratio: 0.7}, wantKept: 1},
		{name: "token2 收紧：占比窗口是较小者", config: WindowConfig{RetainTokens: 500_000, Ratio: 0.05}, wantKept: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			applyWindowConfig(t, testCase.config)
			service, engine, sessionID := compactTestService(t, "task-window")
			marks := appendWindowRounds(t, service, "task-window", rounds, answerChars)

			record := compactNow(t, service, sessionID)
			kept := retainedRounds(engine.History(), marks)
			switch {
			case testCase.wantKept < 0 && !(kept > 1 && kept < rounds):
				t.Fatalf("保留 %d 轮，want 1 < kept < %d（占比窗口应截断旧轮次）", kept, rounds)
			case testCase.wantKept > 0 && kept != testCase.wantKept:
				t.Fatalf("保留 %d 轮，want %d（min(token1, token2) 生效）", kept, testCase.wantKept)
			}

			// 压缩记录带被压区间，且与实际保留窗口一致（记录，不推算）：
			// 被压前缀 = transcript 前 (rounds-kept) 轮的 2*(rounds-kept) 条事件。
			wantEventTo := uint64(2 * (rounds - kept))
			if record.EventFrom != 1 || record.EventTo != wantEventTo {
				t.Fatalf("压缩区间 = [%d,%d], want [1,%d]（保留 %d 轮）",
					record.EventFrom, record.EventTo, wantEventTo, kept)
			}
			if record.Reason != "context_budget" {
				t.Fatalf("压缩原因 = %q, want context_budget", record.Reason)
			}
			t.Logf("kept=%d reason=%s 区间=[%d,%d]", kept, record.Reason, record.EventFrom, record.EventTo)
		})
	}
}

// TestCompactHardThresholdForcesCompression：window.force_compact_tokens 是硬
// 压缩阈值——全量上下文达到即必须压缩；未配置时同样的 transcript 不压缩
// （软阈值远未达到）。
func TestCompactHardThresholdForcesCompression(t *testing.T) {
	const rounds, answerChars = 2, 400 // 远低于软阈值 125106

	// 未配置硬阈值：不产生压缩记录（软阈值未达）。
	applyWindowConfig(t, WindowConfig{})
	softService, softEngine, softSession := compactTestService(t, "task-soft")
	softMarks := appendWindowRounds(t, softService, "task-soft", rounds, answerChars)
	if _, err := softService.components.context.PrepareExecutionContextFor(softSession, "task-soft", "next"); err != nil {
		t.Fatalf("装配 provider 上下文: %v", err)
	}
	if kept := retainedRounds(softEngine.History(), softMarks); kept != rounds {
		t.Fatalf("软路径保留 %d 轮, want %d（未达任何阈值不应压缩）", kept, rounds)
	}
	if records := compactionRecords(softService, softSession); len(records) != 0 {
		t.Fatalf("未达阈值不应留下压缩记录：%#v", records)
	}

	// 配置硬阈值 1：同样的 transcript 必须压缩（自动路径，不带 forceCompact）。
	applyWindowConfig(t, WindowConfig{ForceCompactTokens: 1})
	hardService, _, hardSession := compactTestService(t, "task-hard")
	appendWindowRounds(t, hardService, "task-hard", rounds, answerChars)
	if _, err := hardService.components.context.PrepareExecutionContextFor(hardSession, "task-hard", "next"); err != nil {
		t.Fatalf("装配 provider 上下文: %v", err)
	}
	record := lastCompactionRecord(t, hardService, hardSession)
	if record.EventFrom != 1 || record.EventTo == 0 {
		t.Fatalf("硬压缩记录区间 = [%d,%d], want 非空被压区间", record.EventFrom, record.EventTo)
	}
}

// TestCompactHardThresholdBypassesEpochThrottle：硬压缩不被"同一批进展只压
// 一次"的节流挡住——同一 progress epoch 内达到硬阈值仍必须压。
func TestCompactHardThresholdBypassesEpochThrottle(t *testing.T) {
	applyWindowConfig(t, WindowConfig{ForceCompactTokens: 1})
	service, _, sessionID := compactTestService(t, "task-throttle")
	appendWindowRounds(t, service, "task-throttle", 2, 400)

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-throttle", "next"); err != nil {
			t.Fatalf("第 %d 次装配: %v", attempt+1, err)
		}
	}
	if records := compactionRecords(service, sessionID); len(records) == 0 {
		t.Fatal("硬阈值路径必须产生压缩记录（不被 epoch 节流吞掉）")
	}
}

// TestReadTailBudgetFollowsRetainedWindowRule：读尾（冷恢复装载 transcript /
// history 尾部）与压缩侧同一 min 规则——token1（retain_tokens）或 token2
// （ratio × 账号上下文窗口）任一收紧都直接压低读尾预算，且旧口径（预算 60%
// 的 TargetAfterCompaction）已不再是读尾来源。
//
// 读尾发生在请求装配之前，"all_context" 取账号上下文窗口（会话能携带的上限）：
// 未达上限的会话不会被占比窗口误裁——读尾是重建既有历史，不是新做压缩决策。
func TestReadTailBudgetFollowsRetainedWindowRule(t *testing.T) {
	window := task_context.DefaultContextBudget().Window
	cases := []struct {
		name   string
		config WindowConfig
		want   int
	}{
		{name: "token1 收紧：配置保留窗口是较小者", config: WindowConfig{RetainTokens: window / 4}, want: window / 4},
		{name: "token2 收紧：占比窗口是较小者", config: WindowConfig{Ratio: 0.05}, want: int(float64(window) * 0.05)},
		{name: "两个都宽：占比窗口截断到 70% 账号窗口", config: WindowConfig{RetainTokens: 10 * window}, want: int(float64(window) * 0.7)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			applyWindowConfig(t, testCase.config)
			if got := RetainedReadTailBudget(nil); got != testCase.want {
				t.Fatalf("读尾预算 = %d, want %d（min(token1, token2)）", got, testCase.want)
			}
		})
	}

	applyWindowConfig(t, WindowConfig{})
	if got, old := RetainedReadTailBudget(nil), task_context.DefaultContextBudget().TargetAfterCompaction; got == old {
		t.Fatalf("读尾预算仍是旧的 60%% 预算口径（%d）", old)
	}
}

func compactionRecords(service *Service, sessionID string) []ContextCompaction {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil {
		return nil
	}
	return append([]ContextCompaction(nil), state.ContextCompactions...)
}

func lastCompactionRecord(t *testing.T, service *Service, sessionID string) ContextCompaction {
	t.Helper()
	records := compactionRecords(service, sessionID)
	if len(records) == 0 {
		t.Fatal("缺少压缩记录")
	}
	return records[len(records)-1]
}

// TestCompactContextHandlerReportsRecordedRange：compact_context 工具/命令的
// 结果面带被压区间（消息号 + 事件序号），模型与用户据此回读原始轮次。
func TestCompactContextHandlerReportsRecordedRange(t *testing.T) {
	applyWindowConfig(t, WindowConfig{RetainTokens: 9_000, Ratio: 0.7})
	service, _, sessionID := compactTestService(t, "task-range")
	appendWindowRounds(t, service, "task-range", 16, 32_000)

	raw, err := service.CompactContextHandler(task_context.WithSessionID(context.Background(), sessionID), `{"reason":"准备长任务"}`)
	if err != nil {
		t.Fatalf("compact_context: %v", err)
	}
	var result ContextCompactionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("结果不是 JSON: %v (%s)", err, raw)
	}
	if !result.Compacted {
		t.Fatalf("结果未报告压缩发生：%+v", result)
	}
	if result.EventFrom != 1 || result.EventTo == 0 {
		t.Fatalf("结果面区间 = [%d,%d], want 非空被压区间（从哪到哪）", result.EventFrom, result.EventTo)
	}
	if result.Reason != "context_budget" {
		t.Fatalf("结果面原因 = %q, want context_budget", result.Reason)
	}
}
