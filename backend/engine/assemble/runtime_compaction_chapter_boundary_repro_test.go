package assemble

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
)

// 复现「压缩成功，帧却是空骨架」。
//
// 边界的错位只有一处：装配层读数闸（compactionReadbackProbe）交回的回执里，
// Summary 是**整份两章节摘要**（Chapter 1 锚点 + Chapter 2 正文），而它对下游的
// 语义是「这次的 Chapter 2 正文」——装配层把它原样存成 precomputedSummary，再经
// CompactionIndexRequest.PrecomputedSummary 交到推帧这一跳。推帧的 chapter2Node
// 直接把它当 Chapter 2（旧代码只剥一个领先的 `## 压缩内容 (Compacted Context)`，
// 而整份摘要以 `## 上一压缩栈摘要 (Previous Compact Stack)` 开头，剥不掉）。
//
// 后果有两层，正好就是现场那两句话：
//  1. Chapter 1 锚点被包进 Chapter 2 里 → 帧正文里同一个标题出现两次；
//  2. 更糟的是再读回来时 `frameChapter` 在 Chapter 2 标题后立刻撞上另一个
//     "## " 标题，正文被裁成空串、FrameChapter2 退化为「整段摘要」——记忆块、
//     OneLineSummary、carry 这些读者拿到的都不是正文。读数闸那一次若落在本地
//     确定性压缩上（模型调用失败），被当正文塞进来的就正是那具全 (none) 骨架：
//     **帧里有 frame_id、summary_source 还写着 replay，正文却是一具空骨架**。
//
// 判别力：帧摘要必须恰好两章节（每个标题一次），且 Chapter 2 正文里不得再有
// 章节标题——后者是"读回来还能用"的充要条件。
func TestReproCompactionReadbackSummaryIsChapter2NotWholeFrame(t *testing.T) {
	fixture := newCompactionSwitchFixture(t, true)
	ctx := context.Background()
	readback, err := fixture.runtime.ReadbackCompactionSummary(ctx, fixture.session, CompactionFrameRequest{
		Overflow:      compactionSwitchOverflow(),
		ReplayHistory: compactionSwitchReplayHistory(),
	})
	if err != nil {
		t.Fatalf("ReadbackCompactionSummary: %v", err)
	}
	if readback.SummarySource != seelexctx.CompactSummarySourceReplay {
		t.Fatalf("读数闸应拿到模型读后感，summary_source = %q", readback.SummarySource)
	}
	// ★ 读数回执是 Chapter 2 正文：不得把 Chapter 1 锚点一起交出来。
	if strings.Contains(readback.Summary, "## "+seelexctx.CompactChapter1Title) {
		t.Fatalf("读数回执不得把整份两章节摘要当 Chapter 2 交出来：\n%s", readback.Summary)
	}

	receipt, err := fixture.runtime.PushCompactionFrame(ctx, fixture.session, CompactionFrameRequest{
		Overflow:           compactionSwitchOverflow(),
		ReplayHistory:      compactionSwitchReplayHistory(),
		PrecomputedSummary: readback.Summary,
	})
	if err != nil {
		t.Fatalf("PushCompactionFrame: %v", err)
	}
	top := fixture.stackTop(t)
	if got := strings.Count(top.Summary, "## "+seelexctx.CompactChapter1Title); got != 1 {
		t.Fatalf("帧摘要里 Chapter 1 标题出现 %d 次（want 1）：\n%s", got, top.Summary)
	}
	if got := strings.Count(top.Summary, "## "+seelexctx.CompactChapter2Title); got != 1 {
		t.Fatalf("帧摘要里 Chapter 2 标题出现 %d 次（want 1）：\n%s", got, top.Summary)
	}
	chapter2 := seelexctx.FrameChapter2(top)
	for _, line := range strings.Split(chapter2, "\n") {
		if strings.HasPrefix(line, "## ") {
			t.Fatalf("Chapter 2 正文里不得再有两章节标题（读回来会被裁空）：%q\n%s", line, chapter2)
		}
	}
	if !strings.Contains(chapter2, "把压缩处的 Chapter 2 换成真摘要") {
		t.Fatalf("Chapter 2 正文应是模型读后感，实际：\n%s", chapter2)
	}
	if !strings.Contains(receipt.Summary, "把压缩处的 Chapter 2 换成真摘要") {
		t.Fatalf("推帧回执仍应是整份帧摘要（前端/门禁读它）：\n%s", receipt.Summary)
	}
}

// 复现同一边界的第二半（2026-10-04 口径更新后拆成两段）：
//
//  1. 读数闸拿不到模型读后感（模型调用失败）→ **报错返回**，错误携带真实报错——
//     旧契约是交回 source=local 的回执；读后感为空不折上下文（2026-10-02）与
//     「失败以错误返回」（2026-10-04）之后，调用方需要的是失败原因本身。
//
//  2. 任何人若仍把一份**本地**预读回执（PrecomputedSummarySource=local）交到推帧，
//     推帧不得把它盖成 replay、也不得丢掉那条真实报错（chapter2Node 预读支路的
//     不变量，与"预读回执从哪来"无关）。
func TestReproPrecomputedLocalSummaryMustNotBeRelabelledReplay(t *testing.T) {
	const failure = "connect: connection refused"
	fixture := newCompactionSwitchFixtureWith(t, true, &failingCompleter{err: errors.New(failure)})
	ctx := context.Background()

	// 第一段：读数闸报错返回，错误带真实报错。
	readback, err := fixture.runtime.ReadbackCompactionSummary(ctx, fixture.session, CompactionFrameRequest{
		Overflow:      compactionSwitchOverflow(),
		ReplayHistory: compactionSwitchReplayHistory(),
	})
	if err == nil {
		t.Fatalf("模型调用失败时读数闸必须报错返回，实际 err=nil receipt=%+v", readback)
	}
	if !strings.Contains(err.Error(), failure) {
		t.Fatalf("读数错误应带真实报错，实际 %q", err.Error())
	}

	// 第二段：预读回执是本地压缩时，推帧不得盖成 replay（预读支路的不变量）。
	localChapter2 := "### 目标 (Goal)\n(none)\n\n### 当前工作 (Current Work)\n本地压缩骨架（非模型读后感）"
	receipt, err := fixture.runtime.PushCompactionFrame(ctx, fixture.session, CompactionFrameRequest{
		Overflow:                 compactionSwitchOverflow(),
		ReplayHistory:            compactionSwitchReplayHistory(),
		PrecomputedSummary:       localChapter2,
		PrecomputedSummarySource: seelexctx.CompactSummarySourceLocal,
		PrecomputedSummaryNote:   failure,
	})
	if err != nil {
		t.Fatalf("PushCompactionFrame: %v", err)
	}
	if receipt.SummarySource != seelexctx.CompactSummarySourceLocal {
		t.Fatalf("预读回执是 local，推帧不得盖成 %q", receipt.SummarySource)
	}
	if !strings.Contains(receipt.SummaryNote, failure) {
		t.Fatalf("帧/回执必须带读数闸的真实报错，实际 note=%q", receipt.SummaryNote)
	}
	top := fixture.stackTop(t)
	if top.SummarySource != seelexctx.CompactSummarySourceLocal {
		t.Fatalf("栈帧 summary_source = %q，want local", top.SummarySource)
	}
	if !hasLocalCompactEvidence(top, seelexctx.PrecomputedLocalDegradeCode) {
		t.Fatalf("栈帧应留 compact-local:%s 证据，实际 %+v",
			seelexctx.PrecomputedLocalDegradeCode, top.Evidence)
	}
	if got := strings.Count(top.Summary, "## "+seelexctx.CompactChapter1Title); got != 1 {
		t.Fatalf("帧摘要里 Chapter 1 标题出现 %d 次（want 1）：\n%s", got, top.Summary)
	}
}
