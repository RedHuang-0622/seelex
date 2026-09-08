package main

// 复现：A、B 两个会话同时在运行。视图先后停在 A；B 在后台跑一轮
// “纯工具调用（无正文）→ 工具结果 → 最终 llm 正文”的工具链，跑完（idle）
// 之后才热切到 B。用户现象：切到 B 后，工具调用/结果是“后插入”到整个
// 会话的——最终 llm 正文出现在工具消息之前（顺序应为 user → tool →
// tool_result → assistant 最终正文）。
//
// 代码路径嫌疑：handleToolCompleteObserved 只对“活跃”分支在 tool_result
// 后补空 assistant 占位；后台分支不补，随后 appendVisibleDeltaBackground
// 会把下一段 llm 正文追加到工具消息之前的旧 assistant 空消息上。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// newToolInlineHarness 复制 full-chain harness 装配，并额外注册一个必然
// 成功的 inline 工具 ping_tool（避免 read_file 依赖项目根/文件系统细节，
// 让复现只针对“后台工具轮顺序”）。
func newToolInlineHarness(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness {
	t.Helper()
	runtimeBridge, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath:    accountsPath,
		StorePath:       filepath.Join(projectRoot, "runtime"),
		ToolCallTimeout: toolTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimeBridge.Shutdown)
	runtimeBridge.RegisterTool("ping_tool", "always succeeds for ordering repro", map[string]interface{}{"type": "object"}, func(ctx context.Context, argsJSON string) (string, error) {
		return "TOOL_OK", nil
	})
	runtimeBridge.RegisterBuiltins()
	runtimeBridge.SetPermissionConfig(toolspermission.PermissionConfig{Mode: toolspermission.ModeFullAccess}, nil)
	if err := runtimeBridge.BindProjectRoot(projectRoot); err != nil {
		t.Fatal(err)
	}

	originalStorePath := *storePath
	*storePath = filepath.Join(projectRoot, "sessions")
	t.Cleanup(func() { *storePath = originalStorePath })
	store, err := initStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runtimeBridge.AttachHistoryRouter(store)
	runtimeBridge.SetEventPersister(sessionstore.NewEventStore(store).Append)

	skills := initSkillSystem()
	plugins, err := initPluginSystem(runtimeBridge, skills)
	if err != nil {
		t.Fatal(err)
	}
	hooks := application.NewToolHookBridge()
	frameworkEngine, err := initEngine(runtimeBridge, hooks, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := activateDefaultPlugin(plugins, frameworkEngine); err != nil {
		t.Fatal(err)
	}
	appEngine := adapters.NewEnginePort(frameworkEngine, func(sessionID string) adapters.ReactorEngine {
		fresh, createErr := initEngine(runtimeBridge, hooks, sessionID)
		if createErr != nil {
			return nil
		}
		return fresh
	}, runtimeBridge.Tracer())
	appEngine.EnableWorkingHistoryRelease()
	workspaces, err := initWorkspaceRepo()
	if err != nil {
		t.Fatal(err)
	}
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	app, err := initApplication(
		appEngine,
		runtimeBridge,
		plugins,
		initSessionManager(store, appEngine),
		skills,
		workspaces,
		events,
		approval,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Shutdown)
	hooks.Bind(app)
	return fullChainHarness{app: app, events: events}
}

// toolOrderGateProvider 按全局请求序号应答：
//
//	#1/#2 种子会话快速文本；#3 A 长任务阻塞；#4 B 工具轮（read_file）阻塞；
//	#5 B 的第二次 LLM 请求（必须携带 read_file 结果）快速返回最终正文。
type toolOrderGateProvider struct {
	mu       sync.Mutex
	requests int
	seen     map[int]chan struct{}
	release  map[int]chan struct{}
	once     map[int]*sync.Once
}

func newToolOrderGateProvider(blocked ...int) *toolOrderGateProvider {
	p := &toolOrderGateProvider{
		seen:    make(map[int]chan struct{}),
		release: make(map[int]chan struct{}),
		once:    make(map[int]*sync.Once),
	}
	for _, number := range blocked {
		p.seen[number] = make(chan struct{})
		p.release[number] = make(chan struct{})
		p.once[number] = &sync.Once{}
	}
	return p
}

func (p *toolOrderGateProvider) waitBlocked(t *testing.T, ctx context.Context, number int) {
	t.Helper()
	select {
	case <-p.seen[number]:
	case <-ctx.Done():
		t.Fatalf("request %d never blocked: %v", number, ctx.Err())
	}
}

func (p *toolOrderGateProvider) releaseNumber(number int) {
	p.mu.Lock()
	channel := p.release[number]
	once := p.once[number]
	p.mu.Unlock()
	if channel != nil && once != nil {
		once.Do(func() { close(channel) })
	}
}

func (p *toolOrderGateProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role       string `json:"role"`
			Content    any    `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if !payload.Stream {
		http.Error(writer, "expected streaming request", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.requests++
	number := p.requests
	p.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	waitGate := func(number int) bool {
		p.mu.Lock()
		seen, release := p.seen[number], p.release[number]
		p.mu.Unlock()
		if release == nil {
			return true
		}
		select {
		case <-seen:
		default:
			close(seen)
		}
		select {
		case <-release:
			return true
		case <-request.Context().Done():
			return false
		}
	}

	switch number {
	case 1:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "SEED_A_OK"}, "finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	case 2:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "SEED_B_OK"}, "finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	case 3:
		if !waitGate(3) {
			return
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "DONE_A"}, "finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	case 4:
		if !waitGate(4) {
			return
		}
		// 纯工具调用（无正文）：等价于“tool-calling”这一步。
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "id": "call-ping-1", "type": "function",
						"function": map[string]any{
							"name":      "ping_tool",
							"arguments": `{}`,
						},
					}},
				},
				"finish_reason": nil,
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	case 5:
		// 第二次 LLM 请求必须携带 ping_tool 的结果，否则脚本时序错了。
		if !providerMessagesContainToolResult(payload.Messages, "call-ping-1", "TOOL_OK") {
			http.Error(writer, "request #5 missing ping_tool result", http.StatusBadRequest)
			return
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "DONE_B_TOOLS"}, "finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
	default:
		http.Error(writer, fmt.Sprintf("unexpected provider request %d", number), http.StatusBadRequest)
	}
}

func TestHotAttachBackgroundToolRoundKeepsToolBeforeFinalLLM(t *testing.T) {
	provider := newToolOrderGateProvider(3, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	defer func() {
		provider.releaseNumber(3)
		provider.releaseNumber(4)
	}()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newToolInlineHarness(t, accountsPath, tempDir, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	submitAndIdle := func(text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v", text, err)
		}
		return harness.app.Snapshot().Session.ID
	}
	sessionA := submitAndIdle("seed A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle("seed B"); got != sessionB {
		t.Fatalf("seed B active = %q, want %q", got, sessionB)
	}

	// A 长任务在跑（#3 阻塞），视图 = A。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatalf("submit long A: %v", err)
	}
	provider.waitBlocked(t, ctx, 3)

	// 切到 B，B 长任务（工具轮 #4）也在跑 → 两个会话同时在运行。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B while A running: %v", err)
	}
	if err := harness.app.Submit(ctx, "tool B round"); err != nil {
		t.Fatalf("submit tool B: %v", err)
	}
	provider.waitBlocked(t, ctx, 4)

	// 视图切回 A（运行中热挂载），B 进入后台。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A while B running: %v", err)
	}
	if snap := harness.app.Snapshot(); snap.Session.ID != sessionA {
		t.Fatalf("view = %q, want A", snap.Session.ID)
	}

	// 放行 B：B 在后台执行 read_file → 最终 llm 正文 → 完成 idle（A 仍阻塞）。
	provider.releaseNumber(4)
	deadline := time.Now().Add(20 * time.Second)
	for {
		snap, err := harness.app.SnapshotOf(sessionB)
		if err != nil {
			t.Fatalf("SnapshotOf(B): %v", err)
		}
		if !snap.Chat.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("B did not finish in background within 20s\n%s", allGoroutineStacks())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if snap := harness.app.Snapshot(); snap.Session.ID != sessionA {
		t.Fatalf("view drifted while B finished: %q, want A", snap.Session.ID)
	}

	// A 收尾，全部 idle。
	provider.releaseNumber(3)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle: %v\n%s", err, allGoroutineStacks())
	}

	// 关键：B 做完之后才热切到 B（驻留 → hot attach）。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B after background completion: %v", err)
	}
	if snap := harness.app.Snapshot(); snap.Session.ID != sessionB {
		t.Fatalf("view = %q, want B", snap.Session.ID)
	}
	hotConversation := harness.app.Snapshot().Conversation
	hotIssue := toolOrderIssue(hotConversation)
	t.Logf("B hot conversation order:\n%s", formatConversation(hotConversation))

	// 二次验证：卸载后冷恢复（record/transcript 重建），看顺序是否同样错位。
	if err := harness.app.UnloadSession(sessionB); err != nil {
		t.Fatalf("unload B: %v", err)
	}
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("cold resume B: %v", err)
	}
	coldConversation := harness.app.Snapshot().Conversation
	coldIssue := toolOrderIssue(coldConversation)
	t.Logf("B cold conversation order:\n%s", formatConversation(coldConversation))

	if hotIssue != "" || coldIssue != "" {
		t.Fatalf("RED REPRO: 工具被后插入到被切换会话末尾（最终 llm 正文跑到工具之前）。\n"+
			"HOT issue: %s\nHOT conversation:\n%s\n\n"+
			"COLD issue: %s\nCOLD conversation:\n%s",
			hotIssue, formatConversation(hotConversation), coldIssue, formatConversation(coldConversation))
	}
}

func formatConversation(conversation []application.Message) string {
	parts := make([]string, 0, len(conversation))
	for _, message := range conversation {
		tool := ""
		if message.Tool != nil {
			tool = fmt.Sprintf(" [tool:%s status=%s err=%q]", message.Tool.Name, message.Tool.Status, message.Tool.Error)
		}
		parts = append(parts, fmt.Sprintf("%s|%q%s", message.Role, message.Content, tool))
	}
	return strings.Join(parts, "\n")
}

// toolOrderIssue 返回顺序问题的描述；顺序正确（user → tool → tool_result →
// assistant 最终正文）时返回空串。
func toolOrderIssue(conversation []application.Message) string {
	toolIndex, resultIndex, finalIndex := -1, -1, -1
	for index, message := range conversation {
		switch {
		case message.Role == "tool" && message.Tool != nil && message.Tool.Name == "ping_tool":
			if toolIndex == -1 {
				toolIndex = index
			}
		case message.Role == "tool_result":
			resultIndex = index
		case message.Role == "assistant" && strings.Contains(message.Content, "DONE_B_TOOLS"):
			finalIndex = index
		}
	}
	if toolIndex == -1 || resultIndex == -1 || finalIndex == -1 {
		return fmt.Sprintf("missing expected messages: tool=%d result=%d final=%d",
			toolIndex, resultIndex, finalIndex)
	}
	if finalIndex < resultIndex || resultIndex < toolIndex {
		return fmt.Sprintf("顺序 tool=%d result=%d final=%d，期望 final 在 tool_result 之后",
			toolIndex, resultIndex, finalIndex)
	}
	return ""
}
