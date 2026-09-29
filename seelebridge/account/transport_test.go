package account

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ── 事故复现夹具（2026-09-29）────────────────────────────────────
//
// 事故形状：账号 client 带 http.Client.Timeout=300s——它是**整请求 wall-clock
// 上限，含 SSE body 读**。一个健康但缓慢的长流（22:02:33 起）恰好在 22:07:33
// 满 300s 被 net/http 砍断，报出
//   session loop 34: seelebridge: stream with account "subagent-1":
//   ChatClient stream: read SSE: context deadline exceeded
//   (Client.Timeout or context cancellation while reading body)
// 措辞本身区分不了「上游停滞」与「仍在正常推进」——这就是病灶。

// sseServer 是 OpenAI 兼容的流式测试上游：按 every 的节奏逐帧推 content；
// stall 非零时推完后**停滞**（不写也不关，模拟上游挂住）。
func sseServer(t *testing.T, chunks []string, every, stall time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
			flush()
			select {
			case <-time.After(every):
			case <-r.Context().Done():
				return
			}
		}
		if stall > 0 {
			select {
			case <-time.After(stall):
			case <-r.Context().Done():
			}
			return
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flush()
	}))
	t.Cleanup(server.Close)
	return server
}

func testAccountSpec(baseURL string) model.AccountSpec {
	return model.AccountSpec{
		Name: "agent-1", Provider: "openai", BaseURL: baseURL, APIKey: "test-key",
		Model: "test-model", MaxTokens: 16,
	}
}

func tokens(count int) []string {
	chunks := make([]string, 0, count)
	for index := 0; index < count; index++ {
		chunks = append(chunks, fmt.Sprintf("tok-%d", index))
	}
	return chunks
}

// TestClientForHasNoWholeRequestTimeout 钉住病灶本身：账号 client 不得带
// http.Client.Timeout（整请求 wall-clock 上限），并且必须装配 Transport 级
// 看门狗（响应头超时 + body 空闲看门狗）——否则「停滞保护」要么没有、要么又
// 退回成按总时长掐流。
func TestClientForHasNoWholeRequestTimeout(t *testing.T) {
	completer := ClientFor(testAccountSpec("http://127.0.0.1:1"))
	client, ok := completer.(*api.ChatClient)
	if !ok {
		t.Fatalf("ClientFor must return *api.ChatClient, got %T", completer)
	}
	if client.Client.Timeout != 0 {
		t.Fatalf("账号 client 不得带整请求超时（当前 %s）：长流会被总时长判死而非被停滞判定",
			client.Client.Timeout)
	}
	if client.Client.Transport == nil {
		t.Fatal("账号 client 必须装配流式看门狗 Transport（nil 表示退回 net/http 默认传输层，无停滞保护）")
	}
}

// TestWholeRequestTimeoutKillsHealthySlowStream 复现事故措辞（机制钉）：同一个
// 健康但缓慢的 SSE 上游，只要 client 带整请求 Timeout，就会在读 body 时报出
// 事故那句 `(Client.Timeout or context cancellation while reading body)`。
// 该用例锁定的是 net/http 的机制（事故前 ClientFor 正是这个形状），不是我们的
// 修复产物——它必须在修复前后都成立，作为「病灶能复现」的活证据。
func TestWholeRequestTimeoutKillsHealthySlowStream(t *testing.T) {
	server := sseServer(t, tokens(12), 150*time.Millisecond, 0) // 总时长 ≈1.8s，全程在推进

	// 事故前的构造形状：LLMConfig.Timeout 是整秒，1s 即足以掐断这条 1.8s 的健康流。
	legacy := api.NewChatClient(types.LLMConfig{
		BaseURL: server.URL, APIKey: "test-key", Model: "test-model", Timeout: 1,
	})
	legacy.SetProvider(api.ProviderType("openai"))

	_, _, _, err := legacy.CompleteStream(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("整请求 Timeout 必须掐断这条超过上限的健康流（事故形状）")
	}
	if !strings.Contains(err.Error(), "(Client.Timeout or context cancellation while reading body)") {
		t.Fatalf("事故措辞未复现，got: %v", err)
	}
}

// TestSlowHealthyStreamSurvivesIdleWatchdog 见 transport_watchdog_test.go
// （修复后补入：它需要看门狗的可注入超时旋钮）。
