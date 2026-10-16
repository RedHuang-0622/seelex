package context_runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	seelexctx "github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/store"
)

// TestReplayInputTokensPrefersConfiguredKnob 钉住旋钮的**零值语义**：未配置
// （`input_tokens` 未设或 ≤0）才走窗口推导（3/4），否则配置值优先——加了旋钮不能
// 静默改掉片预算。
func TestReplayInputTokensPrefersConfiguredKnob(t *testing.T) {
	if got := ReplayInputTokens(0, 200_000); got != 150_000 {
		t.Fatalf("未配置时应取窗口 3/4：%d", got)
	}
	if got := ReplayInputTokens(-5, 200_000); got != 150_000 {
		t.Fatalf("非正值同样视为未配置：%d", got)
	}
	if got := ReplayInputTokens(64_000, 200_000); got != 64_000 {
		t.Fatalf("配置值优先：%d", got)
	}
	// 窗口未知（未装配账号窗口）⇒ 不分片（0），走原有单次重放保住前缀缓存。
	if got := ReplayInputTokens(0, 0); got != 0 {
		t.Fatalf("窗口未知应回 0（不分片）：%d", got)
	}
	if got := ReplayInputTokens(0, -1); got != 0 {
		t.Fatalf("负窗口应回 0：%d", got)
	}
}

// TestNewCompactionSummarizerReportsWhyMissing 钉住三条 nil 出口**各带一条理由**：
// 关闭 / QuickChat 装配失败 / 摘要器构造失败——"模型为什么没被叫到"必须在帧里自答。
func TestNewCompactionSummarizerReportsWhyMissing(t *testing.T) {
	summarizer, note := NewCompactionSummarizer(nil, false)
	if summarizer != nil || !strings.Contains(note, "开关关闭") {
		t.Fatalf("关闭时应给 nil + 理由：%v %q", summarizer, note)
	}
	// 开关打开但没有 completer ⇒ QuickChat 装配失败（这一支不 panic，且点明原因）。
	summarizer, note = NewCompactionSummarizer(nil, true)
	if summarizer != nil || !strings.Contains(note, "QuickChat 装配失败") {
		t.Fatalf("装配失败时应给 nil + 理由：%v %q", summarizer, note)
	}
}

// stubWindowPolicy 是窗口策略替身：返回预设轮数（或错误）。
type stubWindowPolicy struct {
	rounds int
	err    error
	info   seelexctx.ProviderContextInfo
}

func (p *stubWindowPolicy) WindowRounds(_ context.Context, info seelexctx.ProviderContextInfo) (int, error) {
	p.info = info
	return p.rounds, p.err
}

// TestWindowTailBudgetFallsBackToDefaultRounds：读尾预算的两维——tokenBudget 取窗口，
// maxUnits 取策略给的轮数；策略给不出（0 或报错）时**必须回退默认 min_rounds**
// （调用方把 maxUnits<=0 判为"不读"，回 0 等于静默不读历史）。
func TestWindowTailBudgetFallsBackToDefaultRounds(t *testing.T) {
	policy := &stubWindowPolicy{rounds: 12}
	budget, rounds := WindowTailBudget(200_000, policy)
	if budget != 200_000 || rounds != 12 {
		t.Fatalf("预算 = %d/%d, want 200000/12", budget, rounds)
	}
	if policy.info.ContextTokens != 200_000 {
		t.Fatalf("策略必须收到窗口读数：%+v", policy.info)
	}

	fallback := seelexctx.DefaultWindowConfig().MinRounds
	for name, stub := range map[string]*stubWindowPolicy{
		"策略给 0": {rounds: 0},
		"策略报错":  {rounds: 0, err: errors.New("provider 未就绪")},
		"策略给负数": {rounds: -3},
	} {
		if _, rounds := WindowTailBudget(200_000, stub); rounds != fallback {
			t.Fatalf("%s：必须回退默认 min_rounds=%d，得到 %d", name, fallback, rounds)
		}
	}
}

// stubCompactStacks 是压缩栈替身（读快照 + 记推帧）。
type stubCompactStacks struct {
	record store.SessionContextRecord
	pushed []store.CompactFrame
}

func (s *stubCompactStacks) Snapshot() store.SessionContextRecord { return s.record }

func (s *stubCompactStacks) PushCompact(frame store.CompactFrame) error {
	s.pushed = append(s.pushed, frame)
	s.record.CompactStack = append(s.record.CompactStack, frame)
	return nil
}

// TestCoverHistoryGapSkipsWithoutBaseline 钉住这条判据：**没有压缩基线就不补真空区**
// （冷恢复只装载尾窗，与"未重启继续运行"的上下文组成一致；凭空补一帧会让恢复后首请求
// 多出模型从未见过的前缀）。
func TestCoverHistoryGapSkipsWithoutBaseline(t *testing.T) {
	stacks := &stubCompactStacks{}
	events := []store.Event{
		{Seq: 1, Role: "user", Content: "第一轮"},
		{Seq: 2, Role: "assistant", Content: "答复"},
	}
	if err := CoverHistoryGap(context.Background(), GapCoverageDeps{
		AllEvents: events, TailStartUnit: len(events), Stacks: stacks,
	}); err != nil {
		t.Fatalf("无基线应无副作用地返回 nil：%v", err)
	}
	if len(stacks.pushed) != 0 {
		t.Fatalf("无基线不得推帧：%+v", stacks.pushed)
	}

	// 空事件流 / 没有栈：同样是无事路径。
	if err := CoverHistoryGap(context.Background(), GapCoverageDeps{Stacks: stacks}); err != nil {
		t.Fatalf("空事件流应返回 nil：%v", err)
	}
	if err := CoverHistoryGap(context.Background(), GapCoverageDeps{AllEvents: events}); err != nil {
		t.Fatalf("没有栈应返回 nil：%v", err)
	}
}

// TestRelatedMemoryBlocksSelection 钉住选取判据：无记录/空查询/无命中都不注入
// （请求内容不变），命中时渲染出一块。
func TestRelatedMemoryBlocksSelection(t *testing.T) {
	if got := RelatedMemoryBlocks(store.SessionContextRecord{}, "渲染件的契约"); got != nil {
		t.Fatalf("空记录不该注入块：%+v", got)
	}
	record := store.SessionContextRecord{CompactStack: []store.CompactFrame{{
		SegmentID: "seg-1", Summary: "讨论了渲染件的契约与测试", From: 0, To: 3,
	}}}
	if got := RelatedMemoryBlocks(record, "   "); got != nil {
		t.Fatalf("空查询不该注入块：%+v", got)
	}
	if got := RelatedMemoryBlocks(record, "与历史完全无关的词组"); got != nil {
		t.Fatalf("无命中不该注入块：%+v", got)
	}
	blocks := RelatedMemoryBlocks(record, "渲染件 契约")
	if len(blocks) != 1 || blocks[0].Name != "memory" {
		t.Fatalf("命中时应渲染一块：%+v", blocks)
	}
	if len(blocks[0].Messages) != 1 || blocks[0].Messages[0].Content == nil ||
		!strings.Contains(*blocks[0].Messages[0].Content, "相关记忆") {
		t.Fatalf("块的正文应当带上小节标题：%+v", blocks[0].Messages)
	}
}
