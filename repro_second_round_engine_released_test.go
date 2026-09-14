package main

// 红灯复现（用户现象）：同一会话进入“第二轮”时，runChat 在没有发出任何
// LLM 请求的情况下失败，UI 只显示兜底文案「代理运行时 / runChat / 当前任务
// 未能完成」（= classifyPresentedError 的 unclassifiedPresentation）。
//
// 本文件用生产装配（真实 store / 真实引擎 / 假 provider）复刻两种“第二轮”
// 形状，逐条排除/坐实：
//   A. 第一轮结束后会话引擎被释放（驻留驱逐 / 空闲后未挂载）→ 用户直接发
//      第二轮；
//   B. 第一轮以终态工具 task_needs_user_decision 收口（任务停在
//      needs_user_decision，非完成）→ 用户直接发第二轮。

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

	"github.com/RedHuang-0622/seelex/application"
)

// scriptedResponse 是一次 provider 应答：纯文本或一个工具调用。
type scriptedResponse struct {
	text      string
	toolName  string
	toolArgs  string
	toolID    string
	finishRun bool
}

type scriptedProvider struct {
	mu        sync.Mutex
	requests  int
	responses []scriptedResponse
	seen      []string
}

func (p *scriptedProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream bool `json:"stream"`
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
	response := scriptedResponse{text: fmt.Sprintf("R%d", number)}
	if number <= len(p.responses) {
		response = p.responses[number-1]
	}
	if response.toolName != "" {
		callID := response.toolID
		if callID == "" {
			callID = fmt.Sprintf("call-%d", number)
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "id": callID, "type": "function",
						"function": map[string]any{"name": response.toolName, "arguments": response.toolArgs},
					}},
				},
				"finish_reason": nil,
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"content": response.text}, "finish_reason": "stop",
		}},
	})
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func (p *scriptedProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests
}

func errorMessages(conversation []application.Message) []string {
	errors := []string{}
	for _, message := range conversation {
		if message.Role != "error" {
			continue
		}
		if text := strings.TrimSpace(message.Content); text != "" {
			errors = append(errors, text)
		}
	}
	return errors
}

func toolResultTexts(conversation []application.Message) []string {
	results := []string{}
	for _, message := range conversation {
		if message.Tool == nil || message.Tool.Result == "" {
			continue
		}
		results = append(results, message.Tool.Name+" -> "+message.Tool.Result)
	}
	return results
}

func newScriptedHarness(t *testing.T, responses []scriptedResponse) (*fullChainHarness, *scriptedProvider) {
	t.Helper()
	provider := &scriptedProvider{responses: responses}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newToolInlineHarness(t, accountsPath, tempDir, 10*time.Second)
	return &harness, provider
}

func submitAndWait(t *testing.T, harness *fullChainHarness, ctx context.Context, text string) {
	t.Helper()
	if err := harness.app.Submit(ctx, text); err != nil {
		t.Fatalf("submit %q: %v", text, err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
	}
}

// 情形 A：第一轮结束后会话引擎被释放（驻留 LRU 驱逐 / 空闲后未挂载），用户
// 直接发第二轮。
func TestSecondRoundStartsAfterSessionEngineReleased(t *testing.T) {
	harness, provider := newScriptedHarness(t, []scriptedResponse{{text: "R1_TEXT"}, {text: "R2_TEXT"}})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	submitAndWait(t, harness, ctx, "round one")
	sessionID := harness.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("first round produced no session")
	}
	if err := harness.app.UnloadSession(sessionID); err != nil {
		t.Fatalf("unload session %s: %v", sessionID, err)
	}
	before := provider.count()
	submitAndWait(t, harness, ctx, "round two")
	t.Logf("A: provider requests %d -> %d, chat.error=%q", before, provider.count(), harness.app.Snapshot().Chat.Error)
	if provider.count() == before {
		t.Fatalf("RED(A): 第二轮没有发出 LLM 请求；错误=%v", errorMessages(harness.app.Snapshot().Conversation))
	}
	if errors := errorMessages(harness.app.Snapshot().Conversation); len(errors) > 0 {
		t.Fatalf("RED(A): 第二轮出现可见错误：%v", errors)
	}
}

// 情形 B：第一轮以终态工具 task_needs_user_decision 收口（任务停在
// needs_user_decision，未完成），用户直接发第二轮。
func TestSecondRoundStartsAfterTerminalDecisionTool(t *testing.T) {
	harness, provider := newScriptedHarness(t, []scriptedResponse{
		{
			toolName: "task_needs_user_decision",
			toolArgs: `{"summary":"需要用户决策","decision_question":"选 A 还是 B？","decision_options":["A","B"]}`,
		},
		{text: "R1_AFTER_DECISION"},
		{text: "R2_TEXT"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	submitAndWait(t, harness, ctx, "round one")
	t.Logf("B: round 1 tool results=%v errors=%v", toolResultTexts(harness.app.Snapshot().Conversation), errorMessages(harness.app.Snapshot().Conversation))
	before := provider.count()
	submitAndWait(t, harness, ctx, "A")
	t.Logf("B: provider requests %d -> %d, chat.error=%q", before, provider.count(), harness.app.Snapshot().Chat.Error)
	t.Logf("B: conversation errors=%v", errorMessages(harness.app.Snapshot().Conversation))
	if provider.count() == before {
		t.Fatalf("RED(B): 第二轮没有发出 LLM 请求；错误=%v", errorMessages(harness.app.Snapshot().Conversation))
	}
	if errors := errorMessages(harness.app.Snapshot().Conversation); len(errors) > 0 {
		t.Fatalf("RED(B): 第二轮出现可见错误：%v", errors)
	}
}
