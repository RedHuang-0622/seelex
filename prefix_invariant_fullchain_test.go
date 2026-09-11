package main

// 全链路跨轮前缀不变量回归（本地、确定性、无网络）：
//
//	用脚本化 mock provider + 生产 harness 驱动两轮"先说明 → 再调用工具"的会话
//	（工具轮说明正文 = wire 上被框架丢弃、只进视图的那段），然后断言**相邻两次
//	provider 请求的消息序列保持前缀关系**：上一条请求已发出的每条消息，在下一条
//	请求里必须逐字节不变（追加只发生在尾部）。
//
// 该用例对应真实 API 冒烟（real_api_prefix_live_test.go）在 DeepSeek 上实测到的
// 两类跨轮改写：
//  1. 工具轮 assistant 消息的正文在跨轮投影里从空变成说明文本；
//  2. 工具失败结果的正文从 wire 上的原始输出变成应用呈现文本（presentToolError）。
//
// 两者的共同后果：同一会话的后续请求不再以前一条请求的字节为前缀，provider 的
// 前缀缓存自该消息起全部失效（实测命中率波动 81%–95%）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fullChainPrefixMessage 是一条 wire 消息的紧凑指纹。
type fullChainPrefixMessage struct {
	raw string
}

func (message fullChainPrefixMessage) tag() string {
	var shape struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []any           `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(message.raw), &shape); err != nil {
		return "?"
	}
	suffix := ""
	if len(shape.ToolCalls) > 0 {
		suffix = "+tc"
	}
	content := 0
	if len(shape.Content) > 0 && string(shape.Content) != "null" {
		var text string
		if json.Unmarshal(shape.Content, &text) == nil {
			content = len(text)
		}
	}
	return fmt.Sprintf("%s%s(%d)", shape.Role, suffix, content)
}

func fullChainPrefixMessages(t *testing.T, body []byte) []fullChainPrefixMessage {
	t.Helper()
	var payload struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode provider request: %v", err)
	}
	out := make([]fullChainPrefixMessage, 0, len(payload.Messages))
	for _, raw := range payload.Messages {
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			out = append(out, fullChainPrefixMessage{raw: strings.TrimSpace(string(raw))})
			continue
		}
		out = append(out, fullChainPrefixMessage{raw: compact.String()})
	}
	return out
}

// fullChainPrefixServer 是脚本化 mock provider：奇数次调用回"说明 + 工具调用"，
// 偶数次调用回终答。所有请求体按序记录。
type fullChainPrefixServer struct {
	*httptest.Server

	mu     sync.Mutex
	bodies [][]byte
	tool   string
	path   string
}

func newFullChainPrefixServer(t *testing.T, tool, path string) *fullChainPrefixServer {
	t.Helper()
	server := &fullChainPrefixServer{tool: tool, path: path}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.serve(t, writer, request)
	}))
	t.Cleanup(server.Close)
	return server
}

func (server *fullChainPrefixServer) recorded() [][]byte {
	server.mu.Lock()
	defer server.mu.Unlock()
	out := make([][]byte, len(server.bodies))
	copy(out, server.bodies)
	return out
}

func (server *fullChainPrefixServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	server.mu.Lock()
	server.bodies = append(server.bodies, body)
	call := len(server.bodies)
	tool, path := server.tool, server.path
	server.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if call%2 == 1 {
		// 工具轮：先给一段"说明正文"（wire 上会被框架丢弃，只经 onChunk 进视图），
		// 再宣告工具调用。
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": fmt.Sprintf("第%d次说明正文。", call)},
				"finish_reason": "",
			}},
		})
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id":    fmt.Sprintf("call_%d", call),
						"type":  "function",
						"function": map[string]any{
							"name":      tool,
							"arguments": fmt.Sprintf(`{"path": %q}`, path),
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		})
	} else {
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": fmt.Sprintf("第%d次终答。", call/2)},
				"finish_reason": "",
			}},
		})
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
		})
	}
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func TestFullChainPrefixInvariantAcrossTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	projectRoot := t.TempDir()
	probePath := filepath.Join(projectRoot, "prefix_probe.txt")
	if err := os.WriteFile(probePath, []byte("PREFIX-PROBE-OK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := newFullChainPrefixServer(t, "read_file", probePath)
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", provider.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o644); err != nil {
		t.Fatal(err)
	}

	harness := newFullChainHarness(t, accountsPath, projectRoot, 30*time.Second)
	submit := func(prompt string) {
		t.Helper()
		if err := harness.app.Submit(ctx, prompt); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("turn did not become idle: %v", err)
		}
		if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
			t.Fatalf("turn failed: %s", snapshot.Chat.Error)
		}
	}
	submit("第一轮：先写一句说明，再调用 read_file 读取 prefix_probe.txt，然后只回复『第一轮完成』。")
	submit("第二轮：同样先写一句说明，再调用 read_file 读取 prefix_probe.txt，然后只回复『第二轮完成』。")

	bodies := provider.recorded()
	if len(bodies) < 4 {
		t.Fatalf("recorded %d provider requests, want >= 4", len(bodies))
	}
	type pair struct {
		index int
		prev  []fullChainPrefixMessage
		cur   []fullChainPrefixMessage
	}
	pairs := make([]pair, 0, len(bodies)-1)
	for index := 1; index < len(bodies); index++ {
		pairs = append(pairs, pair{
			index: index,
			prev:  fullChainPrefixMessages(t, bodies[index-1]),
			cur:   fullChainPrefixMessages(t, bodies[index]),
		})
	}
	for _, item := range pairs {
		tags := make([]string, 0, len(item.cur))
		for _, message := range item.cur {
			tags = append(tags, message.tag())
		}
		t.Logf("req#%d n=%d %s", item.index+1, len(item.cur), strings.Join(tags, " "))
	}

	failures := 0
	for _, item := range pairs {
		shared := 0
		for shared < len(item.prev) && shared < len(item.cur) && item.prev[shared].raw == item.cur[shared].raw {
			shared++
		}
		if shared == len(item.prev) {
			continue
		}
		failures++
		t.Errorf("req#%d→#%d 丢失跨轮前缀：共享 %d/%d 条，首处分歧 msg#%d %s → %s\n  prev=%s\n  cur =%s",
			item.index, item.index+1, shared, len(item.prev), shared,
			item.prev[shared].tag(), item.cur[shared].tag(),
			clipForFailure(item.prev[shared].raw, 400), clipForFailure(item.cur[shared].raw, 400))
	}
	if failures > 0 {
		t.Fatalf("跨轮前缀不变量在 %d 处被破坏", failures)
	}
}

func clipForFailure(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
