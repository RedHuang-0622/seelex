//go:build !extended

package integration

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// 环境表变体：动态尾块的前缀观测（**当前结论 = 前缀完整**）。
//
// 缺陷与修复（同一夹具的两个阶段，记录在此以便回归时对照）：
//
//	装配把环境表块（`composeRequestTail` 的第三段，拼在用户输入**之后**）放进本次
//	请求的字节流，早先 transcript 里存的仍是**原始输入**。下一轮按 transcript 重投影
//	时那条 user 消息少了整段环境表——「上一条请求」不再像「本条请求的前缀」，从该
//	消息起直到请求尾全部重算。这与工作打点表变体是**同一个** I1（字节冻结）缺陷，
//	只是换了动态尾块的来源：环境表来自项目环境配置，会被用户高频修改，且永远位于
//	请求最尾，因此它的缺失把后面的一切（余下历史 + 新输入）都拖进重算。
//
//	- 修复前（M0 实测）：`prefix BREAK byte=16817 char=16707 segment=truth
//	  msg#0 role=user`，hit=16707/17378（96.1%），UnexpectedMiss=1。
//	- 修复后（M1 `RecordWire` 落地，32cd1897）：`prefix OK hit=17071/17699
//	  （96.5%）`，新增 628 字符全部来自本轮真实内容，UnexpectedMiss=0。
//
// 本用例保留的价值：环境表是**第三个**动态尾块来源（前两个是工作打点表、plan 帧）。
// 它证明字节冻结不是对某一种尾块的特判，而是装配出口的通用约束。
//
// 运行：go test ./application/assembly/integration -run ContextPrefixEnvTable -count=1
func TestContextPrefixEnvTable_PrefixIntact(t *testing.T) {
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

	// 翻转版（M1 字节冻结 RecordWire 已落地，见 32cd1897）：环境表块既然作为
	// 已发出字节写回了 transcript，跨轮就不该再分叉。修复前同一夹具断在
	// byte 16817 / char 16707 / msg#0 role=user，复用仅 96.1%。
	cross := results[len(results)-1]
	if cross.First {
		t.Fatalf("跨轮观测样本不该是首次样本")
	}
	if !cross.Intact() {
		desc := cross.Summary()
		if cross.Divergence != nil {
			desc += " 分叉归因: " + cross.Divergence.String()
		}
		t.Fatalf("环境表变体前缀被打破（I1 字节冻结失效）：%s", desc)
	}

	// ④ 键未变、且全程无意外失效。
	cum := probe.Cumulative()
	t.Logf("== 累计: requests=%d hit=%d/%d (%.1f%%) new=%d unexpected=%d expected=%d ==",
		cum.Requests, cum.HitChars, cum.TotalChars, cum.Ratio()*100,
		int64(cross.TotalChars-cross.HitChars), cum.UnexpectedMiss, cum.ExpectedMiss)
	if cum.UnexpectedMiss != 0 {
		t.Fatalf("前缀完整时不得出现 UnexpectedMiss，实际 %d", cum.UnexpectedMiss)
	}
	if newChars := int64(cross.TotalChars - cross.HitChars); newChars <= 0 {
		t.Fatalf("跨轮请求应有真实新增内容，新增字符=%d 说明夹具退化了", newChars)
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
