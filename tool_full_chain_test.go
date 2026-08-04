package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestFullAccessBashToolCompletionReachesApplication exercises the production
// Runtime -> Session -> ToolHookBridge -> Application event path without a
// real provider. The second provider request is held open so tool completion
// must be observable independently of the final assistant response.
func TestFullAccessBashToolCompletionReachesApplication(t *testing.T) {
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
	if bashResult.ExitCode != 0 || !strings.Contains(strings.ToLower(bashResult.Stdout), strings.ToLower(tempDir)) {
		t.Fatalf("bash completion = %+v, want project cwd %q", bashResult, tempDir)
	}

	var toolResult string
	for _, message := range app.Snapshot().Conversation {
		if message.Tool != nil && message.Tool.Name == "bash" && message.Tool.Status == "success" {
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

type fullChainHarness struct {
	app    *application.Service
	events *application.EventHub
}

func newFullChainHarness(t *testing.T, accountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness {
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
	appEngine := newEnginePort(frameworkEngine, func(sessionID string) reactorEngine {
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

type bashToolChainServer struct {
	*httptest.Server
	mu                   sync.Mutex
	requests             int
	secondRequestStarted chan struct{}
	releaseSecondRequest chan struct{}
}

func newBashToolChainServer(t *testing.T) *bashToolChainServer {
	t.Helper()
	server := &bashToolChainServer{
		secondRequestStarted: make(chan struct{}),
		releaseSecondRequest: make(chan struct{}),
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
		if !providerMessagesContainToolResult(payload.Messages, "call-bash-1", "exit_code") {
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
			if message.Tool != nil && message.Tool.Name == name && message.Tool.Status == "success" {
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
