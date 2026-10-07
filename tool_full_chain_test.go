package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
	workspacerepo "github.com/RedHuang-0622/seelex/workspace"
)

// requireGitBash 断言环境提供真实 bash（git-bash 或非 WSL 的 PATH bash）。
// WSL bash 是子系统启动器（冷启动数秒、弹控制台、localhost 代理警告），
// 已从 bash 工具 shell 探测中排除；缺失时跳过 bash 全链路用例。
func requireGitBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	for _, path := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	if bash, err := exec.LookPath("bash"); err == nil && !security.IsWSLBash(bash) {
		return
	}
	t.Skip("bash 全链路测试需要真实 bash（git-bash）；本机仅 WSL bash，已排除")
}

// TestFullAccessBashToolCompletionReachesApplication exercises the production
// Runtime -> Session -> ToolHookBridge -> Application event path without a
// real provider. The second provider request is held open so tool completion
// must be observable independently of the final assistant response.
func TestFullAccessBashToolCompletionReachesApplication(t *testing.T) {
	requireGitBash(t)
	server := newBashToolChainServer(t)
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 5*time.Second)
	app, events := harness.app, harness.events

	subscription := events.Subscribe(256)
	defer subscription.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := app.Submit(ctx, "Call the bash tool exactly once with command pwd and then report the result."); err != nil {
		t.Fatal(err)
	}

	select {
	case <-server.secondRequestStarted:
	case <-ctx.Done():
		t.Fatalf("second provider request did not start: %v\n%s", ctx.Err(), allGoroutineStacks())
	}

	completed := waitForToolCompleted(t, ctx, subscription.Events, "bash")
	var completedMessage application.Message
	if err := json.Unmarshal(completed.Payload, &completedMessage); err != nil {
		t.Fatalf("decode tool.completed payload: %v", err)
	}
	if completedMessage.Tool == nil || completedMessage.Tool.Result == "" {
		t.Fatalf("tool.completed payload = %#v, want bounded bash result", completedMessage)
	}
	var bashResult struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(completedMessage.Tool.Result), &bashResult); err != nil {
		t.Fatalf("decode bash result: %v", err)
	}
	projectDir := filepath.Base(tempDir)
	if bashResult.ExitCode != 0 || !strings.Contains(strings.ToLower(bashResult.Stdout), strings.ToLower(projectDir)) {
		t.Fatalf("bash completion = %+v, want project directory %q", bashResult, projectDir)
	}

	var toolResult string
	for _, message := range app.Snapshot().Conversation {
		if message.Tool != nil && message.Tool.Name == "bash" && message.Tool.Status == dto.ToolEventSuccess {
			toolResult = message.Tool.Result
			break
		}
	}
	if toolResult == "" {
		t.Fatal("application snapshot did not retain the completed bash result")
	}

	close(server.releaseSecondRequest)
	if err := app.WaitForIdle(ctx); err != nil {
		t.Fatalf("chat did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	if snapshot := app.Snapshot(); snapshot.Chat.Error != "" || snapshot.Chat.Running {
		t.Fatalf("final chat state = %+v", snapshot.Chat)
	} else if !conversationContainsAssistant(snapshot.Conversation, "BASH_CHAIN_OK") {
		t.Fatalf("final assistant response missing from conversation: %#v", snapshot.Conversation)
	}
}

// TestFullAccessUnboundBashFailureReachesApplication keeps project scope
// fail-closed while guaranteeing the user sees a terminal tool event instead
// of an indefinitely running tool card.
func TestFullAccessUnboundBashFailureReachesApplication(t *testing.T) {
	requireGitBash(t)
	server := newBashToolChainServer(t)
	defer server.Close()
	server.expectedToolResult = "project scope"

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newUnboundFullChainHarness(t, accountsPath, tempDir, 5*time.Second)
	subscription := harness.events.Subscribe(256)
	defer subscription.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := harness.app.Submit(ctx, "Call the bash tool exactly once with command pwd and then report the result."); err != nil {
		t.Fatal(err)
	}

	select {
	case <-server.secondRequestStarted:
	case <-ctx.Done():
		t.Fatalf("second provider request did not start: %v\n%s", ctx.Err(), allGoroutineStacks())
	}

	completed := waitForToolStatus(t, ctx, subscription.Events, "bash", "error")
	var completedMessage application.Message
	if err := json.Unmarshal(completed.Payload, &completedMessage); err != nil {
		t.Fatalf("decode tool.completed payload: %v", err)
	}
	if completedMessage.Tool == nil || completedMessage.Tool.Status != dto.ToolEventError || completedMessage.Content == "" {
		t.Fatalf("unbound bash completion = %#v, want visible terminal error", completedMessage)
	}

	close(server.releaseSecondRequest)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("chat did not become idle: %v\n%s", err, allGoroutineStacks())
	}
}

type fullChainHarness struct {
	app    *application.Service
	events *application.EventHub
	// store / workspaces 是组合根的存储面与工作区仓库：**teamwork 编排面**的装配
	// （SetTeamworkBackend 的 PlanStore + KeyFor）要用它们。只读交出，不改 harness 的
	// 既有装配（在 harness 里默认装 teamwork 会改变所有全链路用例的工具面）。
	store      *sessionstore.Router
	workspaces *workspacerepo.Repo
	// runtime 是组合根的 seelebridge 实例：让用例能在装配后重新安装权限配置
	// （harness 缺省装 full_access，权限用例要换成 manual 权责配置 + 审批桩）。
	runtime *seelebridge.Runtime
	// approval 是**生产审批 broker**（与 GUI/TUI 同一个实例）：权限用例可以
	// 通过它对"执行选择页面"做端到端断言（页面是否打开、打开了几次、工具名），
	// 而不是只在工具侧看一个桩被调了几次。
	approval *application.ApprovalBroker
}

func newFullChainHarness(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness {
	return newFullChainHarnessWithProjectBinding(t, accountsPath, projectRoot, toolTimeout, true)
}

func newUnboundFullChainHarness(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness {
	return newFullChainHarnessWithProjectBinding(t, accountsPath, projectRoot, toolTimeout, false)
}

func newFullChainHarnessWithProjectBinding(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration, bindProject bool) fullChainHarness {
	return newFullChainHarnessWithLimits(t, accountsPath, projectRoot, toolTimeout, bindProject, seelexctx.Limits{})
}

// fullChainBackendInstaller 是"在 application 之前"注入编排面的钩子。
//
// 为什么顺序本身是契约的一部分：应用的四个**生命周期消费者**在 `application.New` 里就把
// 信号口读走了（`consumeAsyncRuns` 读 `AsyncRunEvents()` 与 teammate 作业信号口）。若团队
// 编排面在 `application.New` **之后**才注入，消费者读到的是 nil 通道——nil 在 select 里
// 永不触发，"做完自动返回"那条链于是**静默不存在**。组合根的顺序是
// store → SetTeamworkBackend → application.New（main.go），测试基座必须同序装配，
// 否则冒烟会把"装配顺序错了"误报成"链子是坏的"。
type fullChainBackendInstaller func(store *sessionstore.Router, workspaces *workspacerepo.Repo, runtime *seelebridge.Runtime) error

// newFullChainHarnessWithLimits 是同一套装配，只多交出 limits 段：A/B 两臂要求
// 除 limits.async_exec.enabled 外逐字段相同，开关是唯一变量。
//
// installers 按组合根的位置在 store/workspaces 就绪之后、application.New 之前逐个执行。
func newFullChainHarnessWithLimits(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration, bindProject bool, limits seelexctx.Limits, installers ...fullChainBackendInstaller) fullChainHarness {
	t.Helper()
	runtimeBridge, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath:    accountsPath,
		StorePath:       filepath.Join(projectRoot, "runtime"),
		ToolCallTimeout: toolTimeout,
		Limits:          limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimeBridge.Shutdown)
	runtimeBridge.RegisterBuiltins()
	runtimeBridge.SetPermissionConfig(toolspermission.PermissionConfig{Mode: toolspermission.ModeFullAccess}, nil)
	if bindProject {
		if err := runtimeBridge.BindProjectRoot(projectRoot); err != nil {
			t.Fatal(err)
		}
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
	// 子代理 / teammate 的**单元记录**持久化：组合根在同一个位置装了它
	// （main.go:380，紧跟 AttachHistoryRouter）。测试基座缺这一句时
	// `Runtime.teamUnitLedger()` 是 nil，`saveTeamUnitRecord` 直接 return——
	// **记录一律不落盘，恢复链在基座上读的是一个空集**，而所有走 harness 的用例
	// 都跑在这个装配上（2026-10-06 实测：teammate 端到端用例的"单元记录在册"
	// 断言在补上这一句之前恒为空集，冒烟因为读的是**计划**而不是记录，一直没发现）。
	runtimeBridge.AttachSubSessionStore(sessionstore.NewNodeSessionStore(store))
	runtimeBridge.SetEventPersister(sessionstore.NewEventStore(store).Append)

	skills := initSkillSystem()
	plugins, _, err := initPluginSystem(runtimeBridge, skills)
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
	// 编排面按组合根的位置注入：**在 application.New 之前**（见 fullChainBackendInstaller）。
	for _, install := range installers {
		if install == nil {
			continue
		}
		if err := install(store, workspaces, runtimeBridge); err != nil {
			t.Fatalf("在 application 之前注入编排面失败: %v", err)
		}
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
	// 回读类工具（read_tool_result / read_compressed_turn / search_history /
	// compact_context）在生产里由 main.run() 经 registerContextReadTools 登记。
	// 测试装配必须同等登记：否则模型收到「超限输入已外置，请用 read_tool_result
	// 回读」的告示却根本没有这个工具，只能退回 bash/computer_screenshot 去找
	// 内容——用例测到的行为与产品不一致（冒烟里模型把截图当回读入口就是这么来的）。
	registerContextReadTools(runtimeBridge, app)
	// 周期提示词任务的执行器：与组合根同一份（main.scheduledPromptExecutor）。
	// 缺这一句时 scheduler 的 executor 是 nil，prompt 任务**根本创建不出来**
	// （"提示词任务执行器未装配"），冒烟会把装配缺口误报成链子坏了。
	runtimeBridge.SetScheduledPromptExecutor(scheduledPromptExecutor(app))
	t.Cleanup(app.Shutdown)
	hooks.Bind(app)
	return fullChainHarness{
		app: app, events: events, runtime: runtimeBridge, approval: approval,
		store: store, workspaces: workspaces,
	}
}

type bashToolChainServer struct {
	*httptest.Server
	mu                   sync.Mutex
	requests             int
	secondRequestStarted chan struct{}
	releaseSecondRequest chan struct{}
	expectedToolResult   string
}

func newBashToolChainServer(t *testing.T) *bashToolChainServer {
	t.Helper()
	server := &bashToolChainServer{
		secondRequestStarted: make(chan struct{}),
		releaseSecondRequest: make(chan struct{}),
		expectedToolResult:   "exit_code",
	}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.serve(t, writer, request)
	}))
	return server
}

func (server *bashToolChainServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
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

	server.mu.Lock()
	server.requests++
	requestNumber := server.requests
	server.mu.Unlock()
	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if requestNumber == 1 {
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"reasoning_content": "checking project cwd"},
				"finish_reason": nil,
			}},
		})
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id":    "call-bash-1",
						"type":  "function",
						"function": map[string]any{
							"name":      "bash",
							"arguments": `{"command":"pwd"}`,
						},
					}},
				},
				"finish_reason": nil,
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	if requestNumber == 2 {
		if !providerMessagesContainToolResult(payload.Messages, "call-bash-1", server.expectedToolResult) {
			http.Error(writer, "second provider request did not contain the bash tool result", http.StatusBadRequest)
			return
		}
		close(server.secondRequestStarted)
		select {
		case <-server.releaseSecondRequest:
		case <-request.Context().Done():
			return
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": "BASH_CHAIN_OK"},
				"finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	http.Error(writer, fmt.Sprintf("unexpected provider request %d", requestNumber), http.StatusBadRequest)
}

func writeSSE(t *testing.T, writer http.ResponseWriter, flusher http.Flusher, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Fprintf(writer, "data: %s\n\n", data)
	flusher.Flush()
}

func waitForToolCompleted(t *testing.T, ctx context.Context, events <-chan application.Event, name string) application.Event {
	return waitForToolStatus(t, ctx, events, name, "success")
}

func waitForToolStatus(t *testing.T, ctx context.Context, events <-chan application.Event, name, status string) application.Event {
	t.Helper()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("application event subscription closed before tool completion")
			}
			if event.Kind != application.EventToolCompleted {
				continue
			}
			var message application.Message
			if err := json.Unmarshal(event.Payload, &message); err != nil {
				t.Fatalf("decode tool.completed message: %v", err)
			}
			if message.Tool != nil && message.Tool.Name == name && message.Tool.Status.String() == status {
				return event
			}
		case <-ctx.Done():
			t.Fatalf("tool.completed(%s) not observed: %v\n%s", name, ctx.Err(), allGoroutineStacks())
		}
	}
}

func allGoroutineStacks() string {
	buffer := make([]byte, 1<<20)
	return string(buffer[:runtime.Stack(buffer, true)])
}

func providerMessagesContainToolResult(messages []struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}, toolCallID, contentPart string) bool {
	for _, message := range messages {
		content, _ := message.Content.(string)
		if message.Role == "tool" && message.ToolCallID == toolCallID && strings.Contains(content, contentPart) {
			return true
		}
	}
	return false
}

func conversationContainsAssistant(messages []application.Message, content string) bool {
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, content) {
			return true
		}
	}
	return false
}
