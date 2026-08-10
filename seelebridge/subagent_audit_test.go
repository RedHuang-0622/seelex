package seelebridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
)

// TestSubAgentMailboxOverflowPreservesMessages 验证 A 修复：merge-back 队列
// 永不静默丢弃——连续写入多条后 Drain 全部回收（修复前 channel 满直接
// drop + 计数，导致子代理合并结果丢失）。
func TestSubAgentMailboxOverflowPreservesMessages(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.enqueueSubagentContext("first")
	runtime.enqueueSubagentContext("second")
	runtime.enqueueSubagentContext("third")
	done := make(chan struct{})
	go func() {
		runtime.enqueueSubagentContext("fourth")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full subagent mailbox blocked the producer")
	}
	items := runtime.DrainSubagentContexts()
	if len(items) != 4 {
		t.Fatalf("mailbox must preserve all merge-back messages, got %d: %#v", len(items), items)
	}
}

// TestSubAgentStartupEndToEnd 验证子代理完整启动路径：
// plan_load → plan_run → workplan 核 → SeelexAgentNode.Run → nodeSession.Chat。
// 使用确定性 scripted completer 避免网络调用，验证子代理会话正确构造与 Chat 入口可达。
func TestSubAgentStartupEndToEnd(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.RegisterTool("task_complete", "complete task", nil, func(ctx context.Context, args string) (string, error) {
		return `{"status":"completed"}`, nil
	})

	scripted := newScriptedNodeCompleter("子代理执行完成：已读取文件并返回结果。")
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"agent-1": scripted,
	})

	// plan_load: 加载含 agent 节点的最小 DAG
	planJSON := `{"entry":"do","nodes":{"do":{"input":"read the file and return findings","kind":"agent"}},"edges":{}}`
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON)
	if err != nil {
		t.Fatalf("plan_load failed: %v", err)
	}
	if !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load unexpected: %s", result)
	}

	// plan_run: 执行 DAG —— 这应启动子代理
	result, err = runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	t.Logf("plan_run result: %s", result)

	// 验证 scripted completer 被调用（子代理确实启动了 Chat）
	scripted.mu.Lock()
	called := len(scripted.requests) > 0
	reqCount := len(scripted.requests)
	scripted.mu.Unlock()
	if !called {
		t.Fatal("sub-agent was never started: scripted completer received no Chat requests")
	}
	t.Logf("sub-agent completer received %d Chat requests", reqCount)

	// 验证子代理的 system prompt 包含节点目标
	// scripted.requests 是 [][]types.Message，每个请求是一组 messages
	scripted.mu.Lock()
	hasGoal := false
	for _, reqMsgs := range scripted.requests {
		for _, msg := range reqMsgs {
			if msg.Content != nil && strings.Contains(*msg.Content, "read the file") {
				hasGoal = true
			}
		}
	}
	scripted.mu.Unlock()
	if !hasGoal {
		t.Error("sub-agent messages should contain node goal 'read the file and return findings'")
	}
}

// TestSubAgentParallelBranches 验证两个 agent 节点并行执行
// （workplan kernel 并发分支调度 + 独立会话）。
func TestSubAgentParallelBranches(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	blockingLeft := newScriptedNodeCompleter("left-done")
	blockingRight := newScriptedNodeCompleter("right-done")

	// 同一个账号 pin 两个子代理：验证并行调度（两个 completer 并发进入）
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"agent-1": newScriptedNodeCompleter("main-done"),
	})

	// 两分支独立账号
	runtime.pool.Unregister("agent-1")
	leftAcc := mustRegisterAccount(t, runtime, "left-agent", blockingLeft)
	rightAcc := mustRegisterAccount(t, runtime, "right-agent", blockingRight)
	_ = leftAcc
	_ = rightAcc

	planJSON := `{"entry":"start","nodes":{"start":{"input":"start","kind":"auto"},"left":{"input":"do left","kind":"agent"},"right":{"input":"do right","kind":"agent"},"finish":{"input":"finish","kind":"auto"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON)
	if err != nil {
		t.Fatalf("plan_load failed: %v", err)
	}
	if !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load unexpected: %s", result)
	}

	result, err = runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	t.Logf("plan_run parallel result: %s", result)

	// 两个子代理都应被启动
	select {
	case <-blockingLeft.started:
		t.Log("left sub-agent started")
	case <-time.After(10 * time.Second):
		t.Error("left sub-agent was never started")
	}
	select {
	case <-blockingRight.started:
		t.Log("right sub-agent started")
	case <-time.After(10 * time.Second):
		t.Error("right sub-agent was never started")
	}
}

func mustRegisterAccount(t *testing.T, runtime *Runtime, id string, value agent.Completer) accountpool.Account[agent.Completer] {
	t.Helper()
	acc := accountpool.Account[agent.Completer]{
		ID: id, Value: value, MaxConcurrency: 1,
		Metadata: map[string]string{"provider": "openai", "model": "test-model"},
	}
	if err := runtime.pool.Register(acc); err != nil {
		t.Fatal(err)
	}
	return acc
}

// TestSubAgentMergeBackToParent 验证"父证据注入 → 执行 → 合并回传"闭环：
// 子代理执行后其结构化上下文（Goal/Findings/Decisions）经 merger.MergeBack
// 合并，Format 结果经 sink 回传（application 侧排队，ChatStream 外注入）。
// 生产接线 = Application 单向发布父证据投影 + Runtime mailbox。
func TestSubAgentMergeBackToParent(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.RegisterTool("task_complete", "complete task", nil, func(ctx context.Context, args string) (string, error) {
		return `{"status":"completed"}`, nil
	})
	// 建主会话（plan_run 的父侧；NewRuntime 不自动创建）。
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	// 父子上下文消息通道（Actor 边界，与 main.go 同款：无锁数据面）：
	// ParentEvidence 从遥测构造快照；MergeBack 捕获 merge-back 结果
	// （生产 = app.AppendSubagentContext 排队 mailbox）。
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit the module", ConversationCount: 1,
	})

	scripted := newScriptedNodeCompleter("子代理结论：模块审计完成。")
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent-1": scripted})

	planJSON := `{"entry":"do","nodes":{"do":{"input":"audit the module and return findings","kind":"agent"}},"edges":{}}`
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil || !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load: %v %s", err, result)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}

	// sink 必须收到 merge-back 块（merger.MergeBack → Format），且携带子代理
	// 目标（节点输入）。不得直接读主会话 History（ChatStream 锁内会死锁）。
	received := runtime.DrainSubagentContexts()
	if len(received) == 0 {
		t.Fatal("runtime mailbox must receive the merged child context block (closed loop)")
	}
	var mergedFound, childGoalFound bool
	for _, content := range received {
		if strings.Contains(content, "继承上下文 (Inherited Context)") {
			mergedFound = true
			if strings.Contains(content, "audit the module") {
				childGoalFound = true
			}
		}
	}
	if !mergedFound {
		t.Fatal("merged block must carry the inherited-context header")
	}
	if !childGoalFound {
		t.Error("merged block should carry the child goal from node input")
	}
}
