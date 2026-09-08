package main

// headless 恢复前缀一致性探针（2026-09-08 工作包）
//
// 目的：不靠代码推断，用真实装配（full chain harness + 本地 mock provider +
// 同一 store 双进程）跑一遍，检查“进程重启 → ResumeSession → 下一轮提交”
// 发往 provider 的上下文，是否等于重启前同会话继续运行时应发送的内容
// （即会话事件原序 + 新输入，不丢前缀、不重复、不乱序）。
//
// 运行（无真实 API，全部走本地 mock）：
//
//	go test . -run TestHeadlessRestorePrefixProbe -count=1 -v -timeout 5m
//
// 说明：旧会话记录无真实价值，探针全部使用 t.TempDir() 独立 store。

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

// probeWireMessage 是 provider 请求里一条消息的归一化形态（只比较顺序与
// 正文，不比较时间戳/请求元数据）。
type probeWireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// prefixRecordingServer 记录每次发往 mock provider 的完整请求消息序列，
// 让测试能直接比较“重启前最后一条真实请求”与“重启后第一条真实请求”。
type prefixRecordingServer struct {
	*httptest.Server
	mu      sync.Mutex
	records [][]probeWireMessage
	next    int
}

func newPrefixRecordingServer(t *testing.T) *prefixRecordingServer {
	t.Helper()
	recorder := &prefixRecordingServer{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorder.serve(t, writer, request)
	}))
	t.Cleanup(server.Close)
	recorder.Server = server
	return recorder
}

func (recorder *prefixRecordingServer) reset() {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.records = nil
	recorder.next = 0
}

func (recorder *prefixRecordingServer) snapshot() [][]probeWireMessage {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	out := make([][]probeWireMessage, len(recorder.records))
	for index := range recorder.records {
		out[index] = append([]probeWireMessage(nil), recorder.records[index]...)
	}
	return out
}

func (recorder *prefixRecordingServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, "decode request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !payload.Stream {
		http.Error(writer, "expected streaming request", http.StatusBadRequest)
		return
	}
	messages := make([]probeWireMessage, 0, len(payload.Messages))
	for _, message := range payload.Messages {
		content := string(message.Content)
		if len(content) >= 2 && content[0] == '"' {
			var decoded string
			if json.Unmarshal(message.Content, &decoded) == nil {
				content = decoded
			}
		}
		messages = append(messages, probeWireMessage{Role: message.Role, Content: content})
	}
	recorder.mu.Lock()
	recorder.next++
	index := recorder.next
	recorder.records = append(recorder.records, messages)
	recorder.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": fmt.Sprintf("answer-%d", index)},
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
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

// TestHeadlessRestorePrefixProbe 是“恢复前缀一致性”的 headless 复现：
//
//	对照 A3（未重启继续跑）：同一进程连续提交 12 轮落盘后，再提交第 13 轮，
//	         记录“如果没重启，第 13 轮真实请求长什么样”；
//	对照 B （重启后恢复）：另一份同一内容的 store 上，跑完同样 12 轮后
//	         shutdown → 重启应用 → ResumeSession 同一会话 → 提交第 13 轮，
//	         记录重启后第一条真实请求；
//	断言：两条真实请求的消息序列一致（同一轮次、同一新输入；原序、不丢、
//	      不重、不乱、不夹带运行期没有的合成前缀）。
func TestHeadlessRestorePrefixProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const settledRounds = 12
	nextPrompt := "round-13-next"

	// 对照 A3：同一进程继续跑（不重启）第 13 轮。
	continuationRecorder := newPrefixRecordingServer(t)
	continuationStore := t.TempDir()
	continuationAccounts := filepath.Join(continuationStore, "accounts.yaml")
	if err := os.WriteFile(continuationAccounts, []byte(fmt.Sprintf(
		"roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n",
		continuationRecorder.URL,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessContinue := newFullChainHarness(t, continuationAccounts, continuationStore, 10*time.Second)
	runSettledRounds(t, ctx, harnessContinue.app, settledRounds)
	if err := harnessContinue.app.Submit(ctx, nextPrompt); err != nil {
		t.Fatalf("继续运行提交第 13 轮: %v", err)
	}
	if err := harnessContinue.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("继续运行等待空闲: %v", err)
	}
	continuationRecords := continuationRecorder.snapshot()
	if len(continuationRecords) != settledRounds+1 {
		t.Fatalf("对照进程请求数 = %d, want %d", len(continuationRecords), settledRounds+1)
	}
	continued := continuationRecords[len(continuationRecords)-1]
	harnessContinue.app.Shutdown()
	t.Logf("对照（不重启）第13轮请求消息数=%d", len(continued))
	for index, message := range continued {
		t.Logf("对照[%02d] %s: %.120s", index, message.Role, message.Content)
	}

	// 对照 B：同样 12 轮后重启恢复，再提交第 13 轮。
	restartRecorder := newPrefixRecordingServer(t)
	restartStore := t.TempDir()
	restartAccounts := filepath.Join(restartStore, "accounts.yaml")
	if err := os.WriteFile(restartAccounts, []byte(fmt.Sprintf(
		"roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n",
		restartRecorder.URL,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessFirst := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
	runSettledRounds(t, ctx, harnessFirst.app, settledRounds)
	sessionID := harnessFirst.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("session did not materialize")
	}
	harnessFirst.app.Shutdown()
	restartRecorder.reset()

	harnessRestarted := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
	if err := harnessRestarted.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession: %v", err)
	}
	resumed := harnessRestarted.app.Snapshot()
	if resumed.Session.ID != sessionID {
		t.Fatalf("重启后视图会话 = %q, want %q", resumed.Session.ID, sessionID)
	}
	t.Logf("重启后可见会话消息数=%d（期望 %d）", len(visiblePairMessages(resumed.Conversation)), settledRounds*2)
	if err := harnessRestarted.app.Submit(ctx, nextPrompt); err != nil {
		t.Fatalf("重启后提交第 13 轮: %v", err)
	}
	if err := harnessRestarted.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("重启后等待空闲: %v", err)
	}
	restartRecords := restartRecorder.snapshot()
	if len(restartRecords) != 1 {
		t.Fatalf("重启进程请求数 = %d, want 1", len(restartRecords))
	}
	afterRestart := restartRecords[0]
	t.Logf("重启后首请求消息数=%d", len(afterRestart))
	for index, message := range afterRestart {
		t.Logf("重启[%02d] %s: %.120s", index, message.Role, message.Content)
	}

	mismatches := compareWirePrefix(afterRestart, continued)
	if len(mismatches) > 0 {
		t.Fatalf(
			"恢复前缀不一致：同一轮次下，重启后请求 != 未重启继续运行的请求（continued=%d restarted=%d）\n%s",
			len(continued), len(afterRestart), strings.Join(mismatches, "\n"),
		)
	}
	t.Logf("恢复前缀一致：重启后首请求与未重启继续运行的第13轮请求逐条相同（消息数=%d）", len(afterRestart))
}

func runSettledRounds(t *testing.T, ctx context.Context, app interface {
	Submit(context.Context, string) error
	WaitForIdle(context.Context) error
}, rounds int) {
	t.Helper()
	for round := 0; round < rounds; round++ {
		prompt := fmt.Sprintf("round-%02d", round)
		if err := app.Submit(ctx, prompt); err != nil {
			t.Fatalf("submit round %d: %v", round, err)
		}
		if err := app.WaitForIdle(ctx); err != nil {
			t.Fatalf("wait idle round %d: %v", round, err)
		}
	}
}

// visiblePairMessages 从快照可见会话里提取 user/assistant 对（跳过 system
// 与恢复横幅），作为“重启前已定稿前缀”的期望序列。
func visiblePairMessages(messages []application.Message) []probeWireMessage {
	out := make([]probeWireMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		out = append(out, probeWireMessage{Role: message.Role, Content: strings.TrimSpace(message.Content)})
	}
	return out
}

func compareWirePrefix(got, want []probeWireMessage) []string {
	var mismatches []string
	if len(got) != len(want) {
		mismatches = append(mismatches, fmt.Sprintf("消息数: got=%d want=%d", len(got), len(want)))
	}
	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	for index := 0; index < limit; index++ {
		g, w := got[index], want[index]
		if g.Role != w.Role || strings.TrimSpace(g.Content) != w.Content {
			mismatches = append(mismatches, fmt.Sprintf(
				"位置 %d: got=%s:%q want=%s:%q", index, g.Role, g.Content, w.Role, w.Content,
			))
		}
	}
	if len(mismatches) == 0 && len(got) != len(want) {
		mismatches = append(mismatches, "前缀逐条相同但长度不同（存在缺失/多余）")
	}
	return mismatches
}
