package assembly

import (
	"context"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context"
	"github.com/RedHuang-0622/seelex/kernel/task"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── 现场复现（红灯，第一跳：素材）──────────────────────────────────────
//
// 现象：一次压缩在记录面/帧面上是「成功（summary_source=replay）」，
// 而模型在重放请求里**根本没拿到要压缩的对话正文**——它只收到一条 system 行
// 加一句固定压缩指令，于是回了一句元回答（「本次请求未附带任何待压缩的对话正文」），
// 这句元回答被当成了本次压缩的 Chapter 2。
//
// 素材这一跳的判据只有「空 / 非空」（application/assembly/context_runtime/coordinator.go）：
//
//	existing := c.sessionHistory(sessionID)   // 引擎历史 = 上一次真实请求的字节
//	replayMaterial := existing
//	if len(replayMaterial) == 0 {             // ← 形状判据，不是语义判据
//	    replayMaterial = fullContext          // 本次装配从 transcript 投影出的历史
//	}
//
// 两处同款（读数闸 ~L738、推帧 ~L1083）。当引擎历史**非空但没有一对话正文**时，
// 回退不触发：素材就是那条 system 行，而同一时刻 fullContext 里躺着本次要压的
// 全部对话——一个字都没交出去。现场形态：压缩记录的 messages_before=1。
//
// 本用例钉住的是**素材本身**：交给压缩模型的那份历史里必须含本次要压缩的对话正文。
// 修复前（红）：素材只有 system 行，断言在 ① 处失败。

func TestCompactionMaterialMustCarryConversationBody(t *testing.T) {
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-body-missing",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n（本次请求未附带任何对话正文）",
		SummarySource: context_runtime.CompactionSummarySourceReplay,
	}}
	runtime := &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 recorder,
		},
		// 读数闸回执 = 模型对「没有正文」的那份请求给出的回答（现场形态）：
		// summary_source 仍是 replay（重放调用本身成功了），正文却是一句元回答。
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "### 目标 (Goal)\n（本次请求未附带任何待压缩的对话正文，无法生成摘要）",
			SummarySource: context_runtime.CompactionSummarySourceReplay,
		},
	}
	// 现场形态：引擎历史只剩一条 system 行（messages_before=1）——上一次压缩
	// 已把轮次折出保留窗口 / 冷加载刚物化，历史里没有对话行。
	engine := &fakeEngine{history: []contract.EngineMessage{
		{Role: "system", Content: "product system prompt", ContentSet: true},
	}}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "task-body-missing"}
	service.components.tasks.BeginTask("task-body-missing", "inspect", "high", nil, model.TaskCheckpoint{})
	service.ViewMu.Unlock()
	// transcript 里有 4 个真实对话轮——这才是本次要压的正文。
	appendIndexRounds(t, service, "task-body-missing")
	sessionID := service.components.frontendSnapshot.Snapshot().Session.ID

	// 夹具自证（前置）：正文确实在 transcript 里，而引擎历史里确实没有对话行。
	// 少了这两条，红线就说不清「是没东西可给」还是「有东西没给」。
	service.ViewMu.RLock()
	transcriptRows := len(service.components.tasks.TranscriptFor(sessionID))
	service.ViewMu.RUnlock()
	if transcriptRows == 0 {
		t.Fatal("夹具假设不成立：transcript 里没有待压缩的正文")
	}
	if bodies := dialogueRows(engine.History()); len(bodies) != 0 {
		t.Fatalf("夹具假设不成立：引擎历史里本不该有对话行（现场 messages_before=1）：%v", bodies)
	}

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.components.contextCompactionApplication.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("夹具假设不成立：本用例要走的是「真的压缩了」这条路径：%+v", result)
	}
	if result.MessagesBefore != 1 {
		t.Fatalf("夹具假设不成立：现场形态是 messages_before=1，实际 %d", result.MessagesBefore)
	}

	// ① 读数闸拿到的那份素材（＝随后 push 交下去的那份）。
	material := runtime.lastReadback.ReplayHistory
	if bodies := dialogueRows(material); len(bodies) == 0 {
		t.Fatalf("压缩素材里没有对话正文，模型从未被给到要压的内容：\n"+
			"  素材 = %d 条（%s）\n"+
			"  同一时刻 transcript 里有 %d 条事件（含 question-*/助手长文本）没被交出去\n"+
			"  引擎历史非空（messages_before=%d）→ `if len(replayMaterial) == 0` 的回退没有触发，\n"+
			"  模型只收到「system 行 + 固定压缩指令」，于是回一句元回答，还被记成 summary_source=replay。",
			len(material), describeRoles(material), transcriptRows, result.MessagesBefore)
	}

	// ② 推帧素材与读数素材同源：两处各自 existing→fullContext 的同款形状回退，
	// 一处修好而另一处漏改，这里就会替它报红灯。
	requests, _ := recorder.snapshot()
	if len(requests) == 0 {
		t.Fatal("推帧没有发生（夹具假设不成立）")
	}
	if bodies := dialogueRows(requests[0].ReplayHistory); len(bodies) == 0 {
		t.Fatalf("推帧素材里没有对话正文：%s", describeRoles(requests[0].ReplayHistory))
	}
}

// ── 对照组（断言当前行为，不是红灯）────────────────────────────────────
//
// 同一份夹具，只把引擎历史换成**空**：`len(existing) == 0` → 回退触发 → 素材就是
// 本次装配的 fullContext，里面是全部对话正文。
//
// 这与上面那条红灯成对，钉住「分支只由 len(existing) 是否为空决定」——
// 证明红灯的成因不是夹具、也不是"这次本来就没东西可压"，而是这条形状判据本身。
func TestCompactionMaterialContrastEmptyEngineHistory(t *testing.T) {
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-empty-history",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n对照组",
		SummarySource: context_runtime.CompactionSummarySourceReplay,
	}}
	runtime := &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 recorder,
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "### 目标 (Goal)\n对照组",
			SummarySource: context_runtime.CompactionSummarySourceReplay,
		},
	}
	// 唯一的差别：引擎历史为空（还没有物化任何历史）。
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "task-empty-history"}
	service.components.tasks.BeginTask("task-empty-history", "inspect", "high", nil, model.TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-empty-history")
	sessionID := service.components.frontendSnapshot.Snapshot().Session.ID

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.components.contextCompactionApplication.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if result.MessagesBefore != 0 {
		t.Fatalf("对照组：引擎历史应为空（messages_before=0），实际 %d", result.MessagesBefore)
	}
	if bodies := dialogueRows(runtime.lastReadback.ReplayHistory); len(bodies) == 0 {
		t.Fatalf("引擎历史为空时回退本应触发（素材取 fullContext）：%s",
			describeRoles(runtime.lastReadback.ReplayHistory))
	}
}

// dialogueRows 返回引擎历史里的**对话正文行**（role=user/assistant 且内容非空）。
// 要求素材里至少有一行：真实请求的字节里必然有它——一份没有任何对话行的素材
// 不可能是「上一次真实请求的历史」。
func dialogueRows(messages []contract.EngineMessage) []string {
	rows := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
			continue
		}
		rows = append(rows, message.Role+":"+firstLine(message.Content))
	}
	return rows
}

// describeRoles 渲染素材的角色构成（红线里要能一眼看出「全是 system」）。
func describeRoles(messages []contract.EngineMessage) string {
	counts := map[string]int{}
	for _, message := range messages {
		counts[message.Role]++
	}
	roles := make([]string, 0, len(counts))
	for role := range counts {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, role+"×"+strconv.Itoa(counts[role]))
	}
	if len(parts) == 0 {
		return "空"
	}
	return strings.Join(parts, " ")
}

func firstLine(content string) string {
	if index := strings.IndexByte(content, '\n'); index >= 0 {
		content = content[:index]
	}
	if len(content) > 40 {
		content = content[:40] + "…"
	}
	return content
}
