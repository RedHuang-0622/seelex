package context_runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/store"
)

// P21 域级用例：降级原因与探针的判据跟着域搬进本包，这里钉住它们的口径
// （装配层的端到端探针仍在 `backend/engine/assemble`，经薄转发走同一条链）。

func TestFrameLocalDegradeReadsCodeAndNote(t *testing.T) {
	frame := store.CompactFrame{
		Evidence: []store.EvidenceRef{
			{Ref: "turn:abc", Summary: "原文归档"},
			{Ref: seelexctx.LocalCompactEvidenceRefPrefix + seelexctx.LocalCompactReplayFailedCode,
				Summary: "  HTTP 402 余额不足  "},
		},
	}
	code, note := frameLocalDegrade(frame)
	if code != seelexctx.LocalCompactReplayFailedCode {
		t.Fatalf("降级码 = %q, want %q", code, seelexctx.LocalCompactReplayFailedCode)
	}
	if note != "HTTP 402 余额不足" {
		t.Fatalf("降级原因应去掉首尾空白：%q", note)
	}
	if got := frameSummaryNote(frame); got != note {
		t.Fatalf("frameSummaryNote = %q, want %q（同一份判据）", got, note)
	}
}

func TestFrameLocalDegradeWithoutEvidenceKeepsSilence(t *testing.T) {
	frame := store.CompactFrame{Evidence: []store.EvidenceRef{{Ref: "turn:abc", Summary: "原文归档"}}}
	code, note := frameLocalDegrade(frame)
	if code != "" || note != "" {
		t.Fatalf("没有降级证据就不编原因：code=%q note=%q", code, note)
	}
	if got := frameSummaryNote(frame); got != "" {
		t.Fatalf("frameSummaryNote = %q, want 空", got)
	}
}

func TestCompactionStackSummaryAvailableFollowsSummarizer(t *testing.T) {
	var closed *CompactionStack
	if closed.SummaryAvailable() {
		t.Fatal("nil 句柄不得报告可用")
	}
	off := NewCompactionStack(CompactionStackDeps{Summarizer: func() seelexctx.PrefixReplaySummarizer { return nil }})
	if off.SummaryAvailable() {
		t.Fatal("摘要器为 nil（开关关闭）⇒ 探针必须为 false")
	}
	on := NewCompactionStack(CompactionStackDeps{
		Summarizer: func() seelexctx.PrefixReplaySummarizer { return stubSummarizer{} },
	})
	if !on.SummaryAvailable() {
		t.Fatal("摘要器非 nil ⇒ 探针必须为 true")
	}
}

// TestCompactionStackWithoutStoreReportsUnbound：未绑定上下文存储时，推帧/读数都必须
// 报同一句装配缺口错误（文案是用户可见行为，逐字保留）。
func TestCompactionStackWithoutStoreReportsUnbound(t *testing.T) {
	stack := NewCompactionStack(CompactionStackDeps{
		Store: func() *store.SessionContextStore { return nil },
		DAG:   func() *seelexctx.CompactionDAG { return nil },
	})
	if _, err := stack.Push(nil, "session-a", CompactionFrameRequest{}); err == nil ||
		!strings.Contains(err.Error(), "会话上下文存储未绑定") {
		t.Fatalf("Push 错误 = %v, want 未绑定提示", err)
	}
	if _, err := stack.Readback(nil, "session-a", CompactionFrameRequest{}); err == nil ||
		!strings.Contains(err.Error(), "会话上下文存储未绑定") {
		t.Fatalf("Readback 错误 = %v, want 未绑定提示", err)
	}
	// 读数面缺失（未装配 runtime）时给"runtime is unavailable"那一句。
	empty := NewCompactionStack(CompactionStackDeps{})
	if _, err := empty.Push(nil, "session-a", CompactionFrameRequest{}); err == nil ||
		!strings.Contains(err.Error(), "runtime is unavailable") {
		t.Fatalf("未装配时 Push 错误 = %v, want runtime is unavailable", err)
	}
}

// stubSummarizer 只用来让"摘要器非 nil"这一格可判（本用例不真正调用它）。
type stubSummarizer struct{}

func (stubSummarizer) Summarize(context.Context, seelexctx.ReplayRequest) (seelexctx.ReplayResult, error) {
	return seelexctx.ReplayResult{}, nil
}
