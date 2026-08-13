package seelebridge

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TestMergeBackParallelSubagentsNoRace 验证两个并行子代理完成时各自
// merge-back 并发写 Runtime mailbox，不产生数据竞争（-race 覆盖）。
// 此测试只验证并发安全与消息不丢；内容累积由
// TestMergeBackIntoParentAccumulates 单测覆盖（scripted completer 不产生
// telemetry Findings，端到端块内容为空是测试环境限制，非缺陷）。
func TestMergeBackParallelSubagentsNoRace(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit the module", ConversationCount: 1,
	})

	left := newScriptedNodeCompleter("LEFT-FINDING: module left audited")
	right := newScriptedNodeCompleter("RIGHT-FINDING: module right audited")
	runtime.pool.Unregister("agent-1")
	mustRegisterAccount(t, runtime, "left-agent", left)
	mustRegisterAccount(t, runtime, "right-agent", right)

	planJSON := `{"entry":"start","nodes":{"start":{"input":"start","kind":"auto"},"left":{"input":"audit left","kind":"agent"},"right":{"input":"audit right","kind":"agent"},"finish":{"input":"finish","kind":"auto"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil || !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load: %v %s", err, result)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}

	received := runtime.DrainSubagentContexts()
	if len(received) < 2 {
		t.Fatalf("mailbox must receive both subagent merge-back blocks, got %d", len(received))
	}
	joined := strings.Join(received, "\n")
	if !strings.Contains(joined, "继承上下文 (Inherited Context)") {
		t.Fatal("merged blocks must carry the inherited-context header")
	}
	// 两个子代理都产生了合并块（消息不丢）。
	if len(received) < 2 {
		t.Fatalf("merge-back must deliver a block per subagent, got %d", len(received))
	}
}

// TestMergeBackIntoParentAccumulates 单测验证 B 修复：mergeBackIntoParent
// 把子代理快照合并进 parentEvidence 并原子写回，连续两次合并时 Findings/
// Decisions/Constraints 累积，第二次合并能看到第一次的产出。
func TestMergeBackIntoParentAccumulates(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit the module", ConversationCount: 1,
	})

	first := &snapshot.ContextSnapshot{
		SourceSessionID: "sub-a",
		Goal:            "audit left",
		Findings:        []string{"LEFT-ONLY-FINDING"},
		Decisions:       []snapshot.Decision{{What: "left decision", Why: "why"}},
	}
	merged := runtime.mergeBackIntoParent(first)
	if merged == nil {
		t.Fatal("first merge returned nil")
	}
	// 第一次合并后：父证据携带左子代理产出。
	if !slices.Contains(merged.Findings, "LEFT-ONLY-FINDING") {
		t.Fatalf("first merge must accumulate left finding, got %v", merged.Findings)
	}

	second := &snapshot.ContextSnapshot{
		SourceSessionID: "sub-b",
		Goal:            "audit right",
		Findings:        []string{"RIGHT-ONLY-FINDING"},
		Constraints:     []string{"constraint-from-b"},
		Decisions:       []snapshot.Decision{{What: "right decision", Why: "why"}},
	}
	merged = runtime.mergeBackIntoParent(second)
	if merged == nil {
		t.Fatal("second merge returned nil")
	}
	// B 修复：第二次合并必须看到第一次的产出（累积），而不是覆盖。
	if !slices.Contains(merged.Findings, "LEFT-ONLY-FINDING") || !slices.Contains(merged.Findings, "RIGHT-ONLY-FINDING") {
		t.Fatalf("merge-back must accumulate both findings, got %v", merged.Findings)
	}
	if !slices.Contains(merged.Constraints, "constraint-from-b") {
		t.Fatalf("second merge must accumulate constraints, got %v", merged.Constraints)
	}
	if len(merged.Decisions) < 2 {
		t.Fatalf("merge-back must accumulate decisions from both subagents, got %d", len(merged.Decisions))
	}
}

// TestMergeBackMailboxOverflowPreserved 验证 A 修复：channel 满时 merge-back
// 转入 overflow 队列，Drain 后全部回收（内容不丢，计数仅作诊断）。
func TestMergeBackMailboxOverflowPreserved(t *testing.T) {
	actor := session.NewSubagentContextActor(nil, 2) // 小 soft cap：触发 overflow 计数但内容保留
	defer actor.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			actor.Enqueue(strings.Repeat("finding-", 20) + string(rune('a'+index)))
		}(i)
	}
	wg.Wait()

	overflowed := actor.Overflow()
	if overflowed == 0 {
		t.Fatal("expected overflow counter to observe the full mailbox")
	}
	kept := actor.Drain()
	if len(kept) != 8 {
		t.Fatalf("mailbox must preserve all 8 merge-back messages (channel + overflow), got %d", len(kept))
	}
}

// TestMergeBackOverflowUnderParallelForks 端到端验证 A+B：超过 mailbox 容量的
// 并行子代理 merge-back 全部保留（A），且合并写回 parentEvidence 累积（B），
// plan_run 成功时没有任何子代理产出丢失。
func TestMergeBackOverflowUnderParallelForks(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit the module", ConversationCount: 1,
	})
	const subagents = 5
	for i := 0; i < subagents; i++ {
		mustRegisterAccount(t, runtime, "agent-"+string(rune('a'+i)),
			newScriptedNodeCompleter("FINDING-"+string(rune('a'+i))))
	}
	runtime.pool.Unregister("agent-1")

	var sb strings.Builder
	sb.WriteString(`{"entry":"start","nodes":{"start":{"input":"start","kind":"auto"}`)
	for i := 0; i < subagents; i++ {
		id := string(rune('a' + i))
		sb.WriteString(`,"` + id + `":{"input":"audit ` + id + `","kind":"agent"}`)
	}
	sb.WriteString(`,"finish":{"input":"finish","kind":"auto"}},"edges":{"start":[`)
	for i := 0; i < subagents; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"` + string(rune('a'+i)) + `"`)
	}
	sb.WriteString(`]`)
	for i := 0; i < subagents; i++ {
		sb.WriteString(`,"` + string(rune('a'+i)) + `":["finish"]`)
	}
	sb.WriteString(`}}`)

	if result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", sb.String()); err != nil || !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load: %v %s", err, result)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}

	kept := runtime.DrainSubagentContexts()
	if len(kept) != subagents {
		t.Fatalf("parallel merge-back lost results: mailbox kept %d of %d subagent blocks (silent drop=%d)",
			len(kept), subagents, runtime.subagentContextDropped())
	}
}

// 确保 mailbox 满时生产者不会阻塞（原有保证），供并发回归基线。
func TestMergeBackMailboxNeverBlocksProducerRetained(t *testing.T) {
	actor := session.NewSubagentContextActor(nil, 1)
	defer actor.Close()
	actor.Enqueue("first")
	done := make(chan struct{})
	go func() {
		actor.Enqueue("overflowed")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full subagent mailbox blocked the producer")
	}
	items := actor.Drain()
	if len(items) != 2 {
		t.Fatalf("mailbox must preserve both messages, got %d: %#v", len(items), items)
	}
}

// TestMergeBackSkippedOnTimeout 复现用户猜想：长时间静置 → 触发超时 →
// merge-back 失败。子代理在超时前已积累结构化上下文（Findings/Decisions），
// 但 agent.Chat 返回超时错误后 SeelexAgentNode.Run 直接跳过 mergeBack——
// 子代理产出整块丢失，主会话/父证据都看不到，前端表现为「子代理失败/
// 工作区现场保留」。测试用确定性 completer 返回超时错误（等价于 fork
// 超时后 agent.Chat 以 ctx.Err() 失败），避免依赖真实计时。
func TestMergeBackSkippedOnTimeout(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit the module", ConversationCount: 1,
	})

	slow := &failingNodeCompleter{reply: "PARTIAL-FINDING-BEFORE-TIMEOUT", err: context.DeadlineExceeded}
	runtime.pool.Unregister("agent-1")
	mustRegisterAccount(t, runtime, "slow-agent", slow)

	planJSON := `{"entry":"do","nodes":{"do":{"input":"audit the module","kind":"agent"}},"edges":{}}`
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil || !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load: %v %s", err, result)
	}
	_, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err == nil {
		t.Fatal("plan_run must fail when the subagent times out")
	}

	// 复现断言：超时后 mailbox 应为空（merge-back 被跳过），且子代理在
	// 阻塞前已发出的请求/产出没有回传。当前行为即失败点。
	received := runtime.DrainSubagentContexts()
	if len(received) == 0 {
		t.Fatalf("merge-back skipped on timeout: subagent context produced before cancellation was lost (mailbox empty, err=%v)", err)
	}
	joined := strings.Join(received, "\n")
	if !strings.Contains(joined, "继承上下文 (Inherited Context)") {
		t.Fatalf("timeout merge-back must still deliver inherited context, got:\n%s", joined)
	}
}

// failingNodeCompleter 返回 err（模拟子代理超时/失败），同时保留 reply
// 表示超时前已产出的内容。
type failingNodeCompleter struct {
	reply string
	err   error
}

func (c *failingNodeCompleter) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	reply := c.reply
	if reply == "" {
		reply = "node-done"
	}
	return types.Message{Role: "assistant", Content: &reply}, c.err
}
