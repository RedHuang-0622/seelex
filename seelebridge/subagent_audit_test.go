package seelebridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

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
// 合并回父会话历史，父代理后续轮次可读到子代理产出。
// 生产接线 = main.go SetNodeParentEvidence（父证据提供者）+
// SeelexAgentNode.mergeBack（执行后回传）。
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
	// 父证据提供者（与 main.go 同款：主会话快照 + 遥测）。
	runtime.SetNodeParentEvidence(func() *snapshot.ContextSnapshot {
		current := runtime.CurrentSession()
		if current == nil {
			return nil
		}
		return seelexctx.ExportSnapshot(current, runtime.Tracer(), "")
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

	// 父会话历史必须包含 merge-back 块（merger.MergeBack → Format 注入），
	// 且携带子代理目标（节点输入）。
	parent := runtime.CurrentSession()
	if parent == nil {
		t.Fatal("runtime has no main session")
	}
	var mergedFound, childGoalFound bool
	for _, msg := range parent.History() {
		if msg.Content == nil {
			continue
		}
		if strings.Contains(*msg.Content, "继承上下文 (Inherited Context)") {
			mergedFound = true
			if strings.Contains(*msg.Content, "audit the module") {
				childGoalFound = true
			}
		}
	}
	if !mergedFound {
		t.Fatal("parent session history must contain the merged child context block (merge-back closed loop)")
	}
	if !childGoalFound {
		t.Error("merged block should carry the child goal from node input")
	}
}
