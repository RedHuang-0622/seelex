package main

// 复现：多轮会话（第 1 轮纯文本、第 2 轮工具链、第 3 轮纯文本），每轮都
// 有独立的最终 llm 正文。逐轮完成后分别走热恢复（驻留切回）与冷恢复
// （Unload → Resume），对比 Snapshot 里的可见 llm 正文行是否全部保留、
// 顺序是否一致。
//
// 用户反馈现象：恢复后“第一轮之后的 llm 消失了”——多轮正文只留下第一轮。

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

// multiRoundLLMProvider 请求序号即轮内次序：
// 1=round1 llm 正文；2=round2 tool_call；3=round2 最终正文；4=round3 正文。
type multiRoundLLMProvider struct {
	mu       sync.Mutex
	requests int
}

func (p *multiRoundLLMProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
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
	switch number {
	case 1:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "R1_LLM"}, "finish_reason": "stop",
			}},
		})
	case 2:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "id": "call-round2-1", "type": "function",
						"function": map[string]any{"name": "ping_tool", "arguments": `{}`},
					}},
				},
				"finish_reason": nil,
			}},
		})
	case 3:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "R2_LLM"}, "finish_reason": "stop",
			}},
		})
	case 4:
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "R3_LLM"}, "finish_reason": "stop",
			}},
		})
	default:
		http.Error(writer, fmt.Sprintf("unexpected provider request %d", number), http.StatusBadRequest)
		return
	}
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func visibleAssistantTexts(conversation []application.Message) []string {
	texts := make([]string, 0, len(conversation))
	for _, message := range conversation {
		if message.Role != "assistant" {
			continue
		}
		if message.Tool != nil {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		texts = append(texts, content)
	}
	return texts
}

func TestMultiRoundLLMPreservedAfterHotAndColdResume(t *testing.T) {
	provider := &multiRoundLLMProvider{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newToolInlineHarness(t, accountsPath, tempDir, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	submitRound := func(text string) {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
	}

	submitRound("round one")
	submitRound("round two with tool")
	submitRound("round three")

	sessionID := harness.app.Snapshot().Session.ID
	live := visibleAssistantTexts(harness.app.Snapshot().Conversation)
	t.Logf("live assistant texts: %v", live)

	// 热恢复：切到一个无关会话再切回（驻留 → hot attach）。
	other, err := harness.app.ForkSessionLatest(sessionID)
	if err != nil {
		t.Fatalf("fork other: %v", err)
	}
	if err := harness.app.ResumeSession(other); err != nil {
		t.Fatalf("resume other: %v", err)
	}
	if err := harness.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("hot resume: %v", err)
	}
	hot := visibleAssistantTexts(harness.app.Snapshot().Conversation)
	t.Logf("hot assistant texts: %v", hot)
	if strings.Join(hot, "\n") != strings.Join(live, "\n") {
		t.Fatalf("RED: hot resume changed assistant texts\nlive=%v\nhot =%v", live, hot)
	}

	// 冷恢复：Unload 后从磁盘重建。
	if err := harness.app.UnloadSession(sessionID); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if err := harness.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("cold resume: %v", err)
	}
	cold := visibleAssistantTexts(harness.app.Snapshot().Conversation)
	t.Logf("cold assistant texts: %v", cold)
	if strings.Join(cold, "\n") != strings.Join(live, "\n") {
		t.Fatalf("RED: cold resume changed assistant texts\nlive=%v\ncold=%v", live, cold)
	}
}
