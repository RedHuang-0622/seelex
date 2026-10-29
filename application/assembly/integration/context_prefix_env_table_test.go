//go:build !extended

package integration

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// 环境表变体：动态尾块的前缀观测（**当前结论 = 钉死缺陷**）。
//
// 现状（本用例当前断言的分叉）：
//
//	装配把环境表块（`composeRequestTail` 的第三段，拼在用户输入**之后**）放进
//	本次请求的字节流，但 transcript 里存的仍是**原始输入**。下一轮按 transcript
//	重投影时，那条 user 消息少了整段环境表——于是「上一条请求」不再是「本条请求
//	的前缀」，从该消息起直到请求尾全部重算。这与工作打点表变体是**同一个** I1
//	（字节冻结）缺陷，只是换了动态尾块的来源：环境表来自项目环境配置，会被用户
//	高频修改（`kernel/context/coordinator.go` composeRequestTail 注释），并且
//	永远位于请求最尾，因此它的缺失把后面的一切（余下历史 + 新输入）都拖进重算。
//
// 归因（本用例断言的具体值）：分叉落在 segment=truth（transcript 重建出来的历史
// 段）、msg#0、role=user —— 正是承载环境表块的那条 user 消息；且 prev 该消息 =
// 原始输入 + 环境表块、cur 该消息 = 原始输入，两者只差这一块（字节级前缀关系），
// 因此分叉不能再归给输入长度、assistant 正文或工具轮。
//
// **修复后如何翻转断言**（`RecordWire` 落地：`application/conversation.
// TurnInputApplication.Prepare` 把归一化后真正发出的 user 正文写回 transcript，
// 见 docs/arch/context-prefix-stability-l1.md §6.3 / I1）：
//  1. 本用例 `cross.Intact()` 的那段 `Fatalf` 改成 `if !cross.Intact() { t.Fatalf(...) }`
//     —— 即断言**前缀完整**；预期观测变为 `prefix OK hit=…/…`，新增字符只来自
//     本轮真实新内容（上一轮答复 + 本轮输入 + 本轮环境表）。
//  2. `cross.UnexpectedMiss` 的断言从「必须 > 0」翻转为「必须 == 0」。
//  3. msg 级归因段（prev/cur 只差环境表块）改为断言 `cur msg#0 含环境表块`
//     （已发出字节被冻结回 transcript），而不是断言它不含。
//
// 运行：go test ./application/assembly/integration -run ContextPrefixEnvTable -count=1
func TestContextPrefixEnvTable_KnownDefect(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	// 只扩 fake/workspace port 的**数据面**（项目环境表 + 会话绑定），装配语义一行
	// 未改：环境表仍然由 `service_assembler.go` 的 EnvironmentTraceBlock 端口从
	// workspace 口读出来、由 composeRequestTail 拼进请求尾。
	workspace := &environmentTraceWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		defaultEnv:    "dev",
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev", Note: "local checkout"},
			{Name: "test", Host: "test.example.com", Port: 22, User: "qa", RemoteRoot: "/srv/test"},
		},
	}
	project, err := workspace.Create("app", "/workspace/app", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	workspace.BindSession(harness.session, project.ID)
	harness.service.Deps.Workspace = workspace
	harness.startStream()

	turnOneInput := "第 1 轮：核对环境表在请求尾部的位置"
	turnOneReply := "第 1 轮结论：环境表由 composeRequestTail 拼在用户输入之后"
	harness.beginTurn(turnOneInput)
	harness.capture(1, 1, "t1.iter1")
	harness.streamText(turnOneReply)
	harness.llmIteration(turnOneReply, "整理结论", nil)
	harness.finishTurn(turnOneReply)

	// 项目侧环境表被改了一次（用户加了 prod）：这是环境块"高频修改"的现实形状。
	workspace.environments = append(workspace.environments, model.WorkspaceEnvironmentInfo{
		Name: "prod", Host: "prod.example.com", Port: 2222, User: "deploy", RemoteRoot: "/srv/app",
	})

	turnTwoInput := "第 2 轮：继续核对"
	harness.beginTurn(turnTwoInput)
	harness.capture(2, 1, "t2.iter1")

	// ① 注入证明：装配路径真的产出了环境表块，并且它排在用户输入**之后**（请求尾）。
	//    没有这一段，下面的分叉断言就可能只是"根本没注入"的假绿/假红。
	first, second := harness.calls[0], harness.calls[1]
	if len(first.messages) != 1 {
		t.Fatalf("第 1 轮应当只有一条消息（首轮 user），实际 %d 条", len(first.messages))
	}
	sentUser := first.messages[0].Content
	if !strings.HasPrefix(sentUser, turnOneInput) {
		t.Fatalf("第 1 轮已发出的 user 正文应当以原始输入开头：%q", summarizeProbeText(sentUser))
	}
	if !strings.Contains(sentUser, environmentTraceMarkerOpen) || !strings.HasSuffix(sentUser, environmentTraceMarkerClose) {
		t.Fatalf("装配没有把环境表块拼在请求尾（这是硬判据③：先确认注入，再谈前缀）：%q", summarizeProbeText(sentUser))
	}
	for _, want := range []string{
		"- dev · local · /workspace/app",
		"- test · ssh · qa@test.example.com:22 · remote_root=/srv/test",
	} {
		if !strings.Contains(sentUser, want) {
			t.Fatalf("环境表块缺 %q：\n%s", want, sentUser)
		}
	}
	// 已发出的字节里确实有它（serializeRequest 会 JSON 转义 < >，故只查标记主体）。
	if !strings.Contains(first.bytes, "seelex:environments:v1") {
		t.Fatalf("环境表块没有进入已发出字节流: bytes=%d", len(first.bytes))
	}
	// 第 2 轮的尾部块确实换成了新环境表（项目侧改动真的进了本轮请求尾）。
	sentUserTwo := second.messages[len(second.messages)-1].Content
	if !strings.Contains(sentUserTwo, "deploy@prod.example.com:2222") {
		t.Fatalf("第 2 轮请求尾没有带上新增的 prod 环境：%q", summarizeProbeText(sentUserTwo))
	}

	// ② 跨两轮观测：M0 阶段还没有真正的 prefix_key（M2 落地），用零值键表达
	//    "键未变"，于是任何断开都会被归为 UnexpectedMiss —— 本用例要求它 > 0。
	var key prefixprobe.Key
	probe := prefixprobe.New()
	results := make([]prefixprobe.Result, 0, len(harness.calls))
	for index := range harness.calls {
		call := harness.calls[index]
		segments, refs := envProbeSegmentsAndMessages(systemPromptFor(harness.service), call.messages, call.bytes)
		res := probe.Observe(uint64(call.turn), prefixprobe.Sample{
			Wire: call.bytes, Segments: segments, Messages: refs, Key: key,
		})
		results = append(results, res)
		t.Logf("  %s  (msgs=%d wire=%dB)", res.Summary(), len(call.messages), len(call.bytes))
	}

	cross := results[len(results)-1]
	if cross.First || cross.Intact() || cross.Divergence == nil {
		t.Fatalf("环境表变体当前应当暴露 I1 分叉（已发出字节没写回 transcript），实际：%s\n"+
			"  若刚落地字节冻结（RecordWire），请按文件头把断言翻转为「前缀完整」", cross.Summary())
	}
	divergence := cross.Divergence
	t.Logf("  分叉归因: %s", divergence.String())
	if cross.HitChars >= cross.TotalChars {
		t.Fatalf("分叉时命中字符必须小于请求总字符：hit=%d total=%d", cross.HitChars, cross.TotalChars)
	}
	if divergence.Role != "user" {
		t.Fatalf("分叉必须落在 user 消息上，实际 role=%q（%s）", divergence.Role, divergence.String())
	}
	if divergence.MessageIndex != 0 {
		t.Fatalf("分叉必须落在 msg#0（承载第 1 轮输入 + 环境表块的那条），实际 msg#%d（%s）",
			divergence.MessageIndex, divergence.String())
	}
	if divergence.Segment != "truth" {
		t.Fatalf("分叉必须落在 transcript 重建出来的历史段（truth），实际 segment=%q（%s）",
			divergence.Segment, divergence.String())
	}

	// ③ 归因到消息级：两侧该消息只差"环境表块"这一块——分叉来自尾块没被冻结，
	//    不是输入长度、assistant 正文或工具轮。
	sentBefore := first.messages[divergence.MessageIndex]
	rebuiltNow := second.messages[divergence.MessageIndex]
	if !strings.Contains(sentBefore.Content, environmentTraceMarkerOpen) {
		t.Fatalf("分叉点所在消息必须承载环境表块：%q", summarizeProbeText(sentBefore.Content))
	}
	if rebuiltNow.Content == sentBefore.Content {
		t.Fatalf("两侧消息不该相同（相同就不存在分叉，上面的断言自相矛盾）")
	}
	if !strings.HasPrefix(sentBefore.Content, rebuiltNow.Content) {
		t.Fatalf("重投影出来的那条消息应当是已发出字节的前缀（只差尾部环境表块）：\n  sent=%q\n  rebuilt=%q",
			summarizeProbeText(sentBefore.Content), summarizeProbeText(rebuiltNow.Content))
	}
	tailBlock := strings.TrimPrefix(strings.TrimPrefix(sentBefore.Content, rebuiltNow.Content), "\n\n")
	if !strings.HasPrefix(tailBlock, environmentTraceMarkerOpen) || !strings.HasSuffix(tailBlock, environmentTraceMarkerClose) {
		t.Fatalf("两侧差值应当正好是环境表块：%q", summarizeProbeText(tailBlock))
	}

	// ④ 键未变却断开 = 缺陷，不是"预期失效"。
	cum := probe.Cumulative()
	t.Logf("== 累计: requests=%d hit=%d/%d (%.1f%%) new=%d unexpected=%d expected=%d ==",
		cum.Requests, cum.HitChars, cum.TotalChars, cum.Ratio()*100,
		int64(cross.TotalChars-cross.HitChars), cum.UnexpectedMiss, cum.ExpectedMiss)
	if cum.UnexpectedMiss == 0 {
		t.Fatalf("键未变却断开必须计入 UnexpectedMiss（否则分叉被当成预期失效）")
	}
}

// envProbeSegmentsAndMessages 把一次请求快照切成命名段与消息起点表，供 prefixprobe
// 归因分叉位置（段口径：system 常量前缀 / truth transcript 重建的历史 / input 当前
// 输入即本轮请求尾）。
//
// 消息起点的字节偏移由 serializeRequest 自身推导（对前 k 条消息序列化取长度），
// 因此与真正发出去的字节流同源，不会因格式改动而错位。
func envProbeSegmentsAndMessages(system string, messages []contract.EngineMessage, wire string) ([]prefixprobe.Segment, []prefixprobe.MessageRef) {
	systemLen := len(serText("SYSTEM", system))
	segments := []prefixprobe.Segment{{Name: "system", Offset: 0, Len: systemLen}}
	refs := make([]prefixprobe.MessageRef, 0, len(messages))
	for index := range messages {
		refs = append(refs, prefixprobe.MessageRef{
			Index: index,
			Role:  messages[index].Role,
			Start: envProbeMessageStart(system, messages, index),
		})
	}
	if len(messages) == 0 {
		if block := len(wire) - systemLen; block > 0 {
			segments = append(segments, prefixprobe.Segment{Name: "draft", Offset: systemLen, Len: block})
		}
		return segments, refs
	}
	lastStart := refs[len(refs)-1].Start
	if lastStart > systemLen {
		segments = append(segments, prefixprobe.Segment{Name: "truth", Offset: systemLen, Len: lastStart - systemLen})
	}
	if end := len(wire); end > lastStart {
		segments = append(segments, prefixprobe.Segment{Name: "input", Offset: lastStart, Len: end - lastStart})
	}
	return segments, refs
}

// envProbeMessageStart 返回第 index 条消息在 serializeRequest 输出里的起始字节偏移。
func envProbeMessageStart(system string, messages []contract.EngineMessage, index int) int {
	if index <= 0 {
		return len(serText("SYSTEM", system)) + len("\n||\n")
	}
	head := serializeRequest(system, messages[:index], "", nil)
	return len(head) - probeInputTrailerLen
}
