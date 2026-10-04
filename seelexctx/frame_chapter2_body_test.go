package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// Chapter2Body 是帧摘要两章节边界的唯一归一化件：链上有一个出口（装配层读数闸
// 回执）天然交回**整份两章节摘要**，而它的下游语义是「这次的 Chapter 2 正文」。
// 这道归一化不做，帧会被包成双重标题；再读回来时 frameChapter 在标题后立刻撞上
// 另一个 "## " 标题、正文被裁成空，FrameChapter2 退化为整段摘要——记忆块、
// OneLineSummary、carry 一起拿到不是正文的东西（现场："压缩成功，帧却是空骨架"）。
func TestChapter2BodyNormalizesWholeFrameSummary(t *testing.T) {
	anchor := RenderAnchorChapter(nil)
	body := "### 目标 (Goal)\n把帧正文换成真摘要"
	whole := RenderFrameSummary(anchor, body)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"整份两章节摘要取 Chapter 2", whole, body},
		{"只带 Chapter 2 标题的正文", "## " + CompactChapter2Title + "\n" + body, body},
		{"已经是 Chapter 2 正文时原样", body, body},
		{"空输入给空", "  \n ", ""},
		{"旧记录（无章节标记）退化为整段", "旧的本地摘要正文", "旧的本地摘要正文"},
		{
			// 已经嵌套过的旧帧：取最内层那段真正文（自愈），不把嵌套带走。
			"双重标题帧剥到最内层正文",
			RenderFrameSummary(anchor, whole),
			body,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Chapter2Body(testCase.in); got != testCase.want {
				t.Fatalf("Chapter2Body = %q，want %q", got, testCase.want)
			}
		})
	}
}

// 同一个归一化件必须同时伺候 FrameChapter2：帧摘要恰好两章节时，读回来的正文里
// 不得再有两章节标题——这是"读回来还能用"的充要条件。
func TestFrameChapter2HasNoChapterHeadings(t *testing.T) {
	body := "### 目标 (Goal)\n把帧正文换成真摘要"
	frame := sessionstore.CompactFrame{
		Summary: RenderFrameSummary(RenderAnchorChapter(nil), body),
	}
	got := FrameChapter2(frame)
	if got != body {
		t.Fatalf("FrameChapter2 = %q，want %q", got, body)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "## ") {
			t.Fatalf("Chapter 2 正文里不得再有两章节标题：%q", line)
		}
	}
}

// 归一化后的正文进 OneLineSummary：锚点的一句话必须是正文散文，而不是章节标题。
func TestFrameChapter2FeedsOneLineSummary(t *testing.T) {
	body := "### 目标 (Goal)\n把帧正文换成真摘要"
	frame := sessionstore.CompactFrame{
		Summary: RenderFrameSummary(RenderAnchorChapter(nil), body),
	}
	if got := OneLineSummary(frame); got != "把帧正文换成真摘要" {
		t.Fatalf("OneLineSummary = %q，want 正文首行", got)
	}
}
