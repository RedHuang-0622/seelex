package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context"
	"github.com/RedHuang-0622/seelex/kernel/task"
)

// 非空压缩栈变体：**跨压缩边界**那一次请求的前缀观测。
//
// ── 本轮结论（实测数据见用例日志）───────────────────────────────────
//
// 结论一：跨压缩边界那一次请求**不以历史字节为前缀**（整段重算）。字节 LCP 只有
// 16775/258743 字符 = 6.5%：这 16775 字符 = system 层 16719 + 首条历史消息 JSON 的
// 公共前半截 56 字符（`{"l":"ROLE","t":"user"}` 与 `q-round-0` 这些同字节部分），
// 即**没有任何一条历史消息被完整共享**，`first_diff=msg#0(role=user)`。归因是结构性
// 替换而非改写：压缩把保留窗口起点前移（`ContextRetainedFrom` 0 → 8），被折出的轮次
// （round-00..03）从 provider 历史里消失，历史首条从 `q-round-00` 换成保留窗口首轮
// `q-round-04`。provider 前缀缓存因此自第一条历史消息起全部作废——这是压缩的固有代价
// （折前缀换余量），不是装配缺陷；本轮要钉的是"它到底发生在哪一条消息上"。
//
// 结论二：被折出区间**确实落成了非空压缩帧**（不是"只挪指针不出帧"）：压缩记录 1 条、
// `ContextRetainedFrom` 0 → 8、`ContextVersion` 1 → 2、帧正文可经 FrameRef 读回
// （含 segment_id 与 summary_source），且装配层推帧素材（Overflow）8 条消息非空——
// 框架确实生效，观测点是真边界而不是一次空压缩。
//
// 结论三：边界**只发生一次**。新 epoch 内部立刻恢复 append-only：压缩后相邻两次请求
// post ⊑ post2 逐字节成立（共享 137259/137409 字符 = 99.9%，只差尾部新增），token 侧
// 命中 137259/137409。"整段重算"的代价被限制在跨边界那一跳，不蔓延到后续轮次。
//
// ── 判据与翻转方式（每条都实测过红/绿，均不可恒真）───────────────────
//
// 判据 ②（框架生效）：`result.Compacted/Recorded` + `ContextCompactions`=1 +
// `ContextRetainedFrom`>0 + 版本 +1 + 推帧恰好一次且 Overflow 非空 + 帧正文可读回。
//
// 判据 ③（跨边界分叉）：`system` 两侧字节相等（分叉不能归因于 system 漂移）且
// `system层 ≤ LCP < system层+首条历史消息长度`——偏小=system 也被重建，偏大=有整条
// 历史消息活着（追加/部分保留，不是折前缀换 epoch）；再叠加 `first_diff=msg#0/user`、
// 被折轮次消失、保留窗口逐条字节相等（窗口不被重写）。
//
// 判据 ④（边界只一次）：post ⊑ post2。
//
// 翻转方式（都已实际跑过）：
//   - 长轮字数 `60_000 → 4_000`（保留窗口装不下任何轮次、全量被折出）→ 新 epoch 里
//     一条轮次都不剩，本用例在"新 epoch 首轮"处红（文件内 `appendRound(index, 60_000)`
//     那一行）；
//   - 把 runtime 换回**不实现** `CompactionIndexPort` 的 `runtimeWithContextLimits`
//     （= 去掉 `compactionIndexRuntime` 那层）→ 推帧 0 次，判据 ② 先红（"没有帧可观测"
//     的压缩不构成本用例的边界）；
//   - 反过来，若把保留窗口撑到装下全部轮次，则 `shared` 会变不回"整段替换"，
//     判据 ③ 的两端夹逼先红——即"没有边界"时本用例不会假绿。
//
// 运行：go test ./application/assembly/integration -run TestCompactStackPrefixAcrossEpochBoundary -count=1 -v

// epochProbeCall 是一次「会发给 provider 的请求」的字节快照。
type epochProbeCall struct {
	label string
	// system 是这一跳的 system 层（字节），body 是 system + 历史消息的串行化字节
	// （**不含**尾部 INPUT/tools 常量块，LCP 只度量历史前缀）。
	system   string
	input    string
	body     string
	messages []contract.EngineMessage
}

// epochProbe 驱动生产装配路径（transcript → PrepareExecutionContextFor），每轮把
// "实际会发给 provider 的请求"固化成字节快照。
type epochProbe struct {
	t       *testing.T
	service *legacyTestService
	engine  *fakeEngine
	session string
	request string
	tools   []model.Tool
	calls   []epochProbeCall
}

func newEpochProbe(t *testing.T, service *legacyTestService, engine *fakeEngine, session, request string) *epochProbe {
	t.Helper()
	return &epochProbe{
		t: t, service: service, engine: engine, session: session, request: request,
		tools: service.Deps.Runtime.VisibleTools(context.Background()),
	}
}

// appendRound 追加一个已定稿轮次（短问 + 长答），返回该轮标记。
func (probe *epochProbe) appendRound(index, answerChars int) string {
	probe.t.Helper()
	mark := probeRoundMark(index)
	probe.service.ViewMu.Lock()
	probe.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: probe.request, MessageID: fmt.Sprintf("message-%d", index*2+1),
		Role: "user", Content: "q-" + mark,
	})
	probe.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: probe.request, MessageID: fmt.Sprintf("message-%d", index*2+2),
		Role: "assistant", Content: mark + ":" + strings.Repeat("A", answerChars),
	})
	probe.service.ViewMu.Unlock()
	return mark
}

// capture 提交当前输入 → 装配（生产顺序：transcript user 事件 → Prepare）→
// 固化这次请求的字节。
func (probe *epochProbe) capture(label, input string) epochProbeCall {
	probe.t.Helper()
	probe.service.ViewMu.Lock()
	probe.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: probe.request, Role: "user", Content: input,
	})
	probe.service.ViewMu.Unlock()
	current, err := probe.service.components.context.PrepareExecutionContextFor(probe.session, probe.request, input)
	if err != nil {
		probe.t.Fatalf("%s: prepare execution context: %v", label, err)
	}
	history := append([]contract.EngineMessage(nil), probe.engine.History()...)
	system := systemPromptFor(probe.service)
	serialized := serializeRequest(system, history, current, probe.tools)
	// 尾部 INPUT（当前输入）+ tools 是跨轮常量/新增项，LCP 只度量 system + 历史。
	body := serialized[:len(serialized)-probeTailLen(current, probe.tools)]
	call := epochProbeCall{
		label: label, system: system, input: current,
		body: body, messages: history,
	}
	probe.calls = append(probe.calls, call)
	probe.t.Logf("  call=%-5s msgs=%2d body=%7d chars bodyTok=%6d firstRound=%d",
		label, len(history), len(body), tokens.Count(body), probeFirstRound(call.messages))
	return call
}

// probeTailLen 返回 serializeRequest 尾部（"\n||\n" + INPUT 段 + 各 tool 段）的长度。
func probeTailLen(input string, tools []model.Tool) int {
	size := len("\n||\n") + len(serText("INPUT", input))
	for _, tool := range tools {
		size += len("\n@TOOL@") + len(serText(tool.Name, tool.Description))
	}
	return size
}

// probeRoundMark 是第 index 轮的稳定标记（与 round 编号一一对应）。
func probeRoundMark(index int) string { return fmt.Sprintf("round-%02d", index) }

// probeRoundIndex 从消息正文里解析出轮次标记编号（无标记 → -1）。
func probeRoundIndex(content string) int {
	for index := 0; index < 100; index++ {
		if strings.Contains(content, probeRoundMark(index)) {
			return index
		}
	}
	return -1
}

// probeFirstRound 返回历史里第一条带轮次标记的消息的编号（无 → -1）。
func probeFirstRound(messages []contract.EngineMessage) int {
	for _, message := range messages {
		if index := probeRoundIndex(message.Content); index >= 0 {
			return index
		}
	}
	return -1
}

// probeHasRound 报告历史里是否有第 index 轮的标记。
func probeHasRound(messages []contract.EngineMessage, index int) bool {
	mark := probeRoundMark(index)
	for _, message := range messages {
		if strings.Contains(message.Content, mark) {
			return true
		}
	}
	return false
}

// probeMessageSerializedLen 返回一条历史消息在 serializeRequest 字节流里的长度
// （与 serializeRequest 的字段序逐字对应：用它可以判定"某条消息是否被完整共享"）。
func probeMessageSerializedLen(message contract.EngineMessage) int {
	size := len("\n||\n") + len(serText("ROLE", message.Role)) + len(serText("CONTENT", message.Content)) +
		len(serText("REASON", message.ReasoningContent)) + len(serText("TOOL", message.ToolCallID+":"+message.Name))
	for _, call := range message.ToolCalls {
		size += len("\n@TC@") + len(serText("ID", call.ID)) + len(serText("NAME", call.Name)) +
			len(serText("ARGS", call.Arguments))
	}
	return size
}

// probeMessagesCarriedOver 统计 after 里逐条（role + 正文逐字节相等）能在 before 里
// 找到同一条消息的数量。
func probeMessagesCarriedOver(before, after []contract.EngineMessage) int {
	carried := 0
	for _, current := range after {
		for _, previous := range before {
			if previous.Role == current.Role && previous.Content == current.Content {
				carried++
				break
			}
		}
	}
	return carried
}

// TestCompactStackPrefixAcrossEpochBoundary 造出「已有压缩帧」的会话，观测跨压缩
// 边界那一次请求与之上的前缀关系。
func TestCompactStackPrefixAcrossEpochBoundary(t *testing.T) {
	// 压缩栈的写入面：装配层推帧（`PushCompactionFrame`）用既有的窄可选端口夹具
	// （context_compact_index_test.go 的 compactionIndexRuntime，形态与生产
	// adapters.RuntimePort 一致），因此"这次压缩真的生成了一帧"在用例里可断言。
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-epoch-1",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n跨压缩边界前缀观测：被折轮次 round-00..03",
		SummarySource: "replay",
	}}
	runtime := &compactionIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
		recorder:                 recorder,
	}
	engine := &fakeEngine{}
	service := newLegacyTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "task-epoch-1"}
	service.components.tasks.BeginTask("task-epoch-1", "epoch prefix probe", "high", nil, model.TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.components.frontendSnapshot.Snapshot().Session.ID
	probe := newEpochProbe(t, service, engine, sessionID, "task-epoch-1")

	// ── 压缩前：两短轮 + 四长轮（合计约 6.5 万 token < 硬线 163471，装配不会
	// 自动折；折出由下面的显式压缩造成，边界因而是可控的那一跳） ──
	const lastRound = 5
	probe.appendRound(0, 400)
	probe.appendRound(1, 400)
	for index := 2; index <= lastRound; index++ {
		probe.appendRound(index, 60_000)
	}
	before := probe.capture("pre", "压缩前：核对前缀与保留窗口。")
	service.ViewMu.RLock()
	versionBefore := service.components.tasks.CurrentTaskExecutionFor(sessionID).ContextVersion
	service.ViewMu.RUnlock()

	// ── 压缩（/compact 同一落点）：折出保留窗口之外的前缀 ──
	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.components.contextCompactionApplication.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	service.ViewMu.RLock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.ViewMu.RUnlock()

	// ── 压缩后：跨边界那一次请求，以及新 epoch 的下一跳 ──
	after := probe.capture("post", "压缩后：跨压缩边界继续，并说明保留窗口。")
	after2 := probe.capture("post2", "压缩后再下一轮：核对新 epoch 是否恢复 append-only。")

	// ── 判据 ① 夹具有效性：压缩前的请求里确有**将被折出**的轮次 ──
	if !probeHasRound(before.messages, 0) || !probeHasRound(before.messages, lastRound) {
		t.Fatalf("夹具无效：压缩前历史应同时含首轮与末轮（folded 候选与保留候选），实际 first=%d msgs=%d",
			probeFirstRound(before.messages), len(before.messages))
	}

	// ── 判据 ② 非空压缩帧真的存在（框架已生效，否则这条用例观测的不是边界） ──
	if !result.Compacted || !result.Recorded {
		t.Fatalf("压缩未成立（compacted=%v recorded=%v），跨边界观测没有对象：%+v", result.Compacted, result.Recorded, result)
	}
	if state == nil || len(state.ContextCompactions) != 1 {
		t.Fatalf("压缩栈应为 1 帧（压缩记录 = 摘要事件的存在证据），实际 %v", state)
	}
	if state.ContextRetainedFrom == 0 {
		t.Fatal("保留窗口起点没有前移（ContextRetainedFrom=0）：这不是压缩边界")
	}
	if state.ContextVersion != versionBefore+1 {
		t.Fatalf("上下文版本 = %d，want %d（压缩应推进一个 epoch）", state.ContextVersion, versionBefore+1)
	}
	pushes, pushSessions := recorder.snapshot()
	if len(pushes) != 1 || len(pushes[0].Overflow) == 0 {
		t.Fatalf("装配层应推帧恰好一次且折出区间非空（空区间 = 没有帧可观测），实际 %d 次：%+v", len(pushes), pushes)
	}
	if pushSessions[0] != sessionID {
		t.Fatalf("推帧会话 = %q，want %q", pushSessions[0], sessionID)
	}
	framePage, err := service.components.referenceApplication.ToolResultContent(result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	for _, want := range []string{"compact-epoch-1", `"summary_source": "replay"`} {
		if !strings.Contains(framePage.Content, want) {
			t.Fatalf("帧正文缺少 %q（压缩帧不成立）：\n%s", want, framePage.Content)
		}
	}
	t.Logf("  frame: segment=%s overflow_msgs=%d event_range=%d..%d retained_from=%d version %d→%d records=%d",
		recorder.receipt.SegmentID, len(pushes[0].Overflow), pushes[0].EventFrom, pushes[0].EventTo,
		state.ContextRetainedFrom, versionBefore, state.ContextVersion, len(state.ContextCompactions))

	// ── 判据 ③ 跨边界那一次请求：不是历史字节的前缀，而是整段重算 ──
	if before.system != after.system {
		t.Fatalf("system 层跨边界发生漂移（分叉因此无法归因于压缩）:\n before=%q\n after=%q",
			summarizeProbeText(before.system), summarizeProbeText(after.system))
	}
	systemOnly := len(serText("SYSTEM", before.system))
	shared := lcpBytes(before.body, after.body)
	firstMessageHead := probeMessageSerializedLen(before.messages[0])
	messageIndex, role := prefixProbeFirstDiff(before.messages, after.messages)
	t.Logf("  boundary: shared=%d/%d chars (%.1f%%, system层=%d, 首条历史消息=%d chars) sharedTok=%d/%d tok first_diff=msg#%d(role=%s)",
		shared, len(before.body), 100*float64(shared)/float64(len(before.body)), systemOnly, firstMessageHead,
		tokens.Count(after.body[:shared]), tokens.Count(after.body), messageIndex, role)
	// 前缀存活区**恰好是 system 层**：不进也不出。
	//   偏小（< systemOnly）= system 也被重建了；偏大（≥ systemOnly + 首条历史消息）=
	//   有整条历史消息活着，即历史没有被整段替换。
	if shared < systemOnly {
		t.Fatalf("system 层都未被完整共享：shared=%d < system=%d（system 也跨边界重建了，分叉无法归因于压缩）",
			shared, systemOnly)
	}
	if shared >= systemOnly+firstMessageHead {
		t.Fatalf("历史首条被完整共享：shared=%d ≥ system(%d)+首条消息(%d)——历史是被追加/部分保留，不是折出前缀换新 epoch",
			shared, systemOnly, firstMessageHead)
	}
	if strings.HasPrefix(after.body, before.body) {
		t.Fatal("压缩后的请求以压缩前请求为字节前缀：跨边界没有发生前缀替换")
	}
	// 归因：历史首条被换掉（旧 epoch 的 q-round-00 → 保留窗口首轮）。
	if messageIndex != 0 || role != "user" {
		t.Fatalf("首个差异消息 = msg#%d(role=%s)，want msg#0(user)：折出点应落在历史头部",
			messageIndex, role)
	}
	if probeHasRound(after.messages, 0) {
		t.Fatal("被折出的 round-00 仍在压缩后历史里：保留窗口没有前移")
	}
	firstRetained := probeFirstRound(after.messages)
	if firstRetained <= 0 || firstRetained > lastRound {
		t.Fatalf("新 epoch 首轮 = %d，want 落在 1..%d（保留窗口应是被折区间的紧邻后继）", firstRetained, lastRound)
	}
	// 保留窗口逐条字节相等：窗口内的消息必须与压缩前发出的同一条一字不差
	// （重排 / 补占位 / 改正文都会让"折前缀"变成"重写窗口"）。
	carried := probeMessagesCarriedOver(before.messages, after.messages[:len(after.messages)-1])
	if carried != len(after.messages)-1 {
		t.Fatalf("保留窗口只有 %d/%d 条消息与压缩前已发出字节相等：窗口被重写了，不只是前缀被折出",
			carried, len(after.messages)-1)
	}

	// ── 判据 ④ 边界只发生一次：新 epoch 内部立刻恢复 append-only ──
	if !strings.HasPrefix(after2.body, after.body) {
		newEpochIndex, newEpochRole := prefixProbeFirstDiff(after.messages, after2.messages)
		t.Fatalf("压缩后的相邻两跳不再保持字节前缀：shared=%d/%d chars，first_diff=msg#%d(role=%s)",
			lcpBytes(after.body, after2.body), len(after.body), newEpochIndex, newEpochRole)
	}
	inEpochShared := lcpBytes(after.body, after2.body)
	t.Logf("  new epoch: post ⊑ post2 shared=%d/%d chars (%.1f%%) — 边界之后回到 append-only",
		inEpochShared, len(after2.body), 100*float64(inEpochShared)/float64(len(after2.body)))
}
