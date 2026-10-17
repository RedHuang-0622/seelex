package account

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
)

// setStreamTimeouts 注入两道看门狗的上限（复用 sseServer 夹具，不必真的等 5 分钟），
// 返回还原函数。测试串行执行（无 t.Parallel），无需额外同步。
func setStreamTimeouts(t *testing.T, header, idle time.Duration) func() {
	t.Helper()
	oldHeader, oldIdle := streamHeaderTimeout, streamIdleTimeout
	streamHeaderTimeout, streamIdleTimeout = header, idle
	return func() {
		streamHeaderTimeout, streamIdleTimeout = oldHeader, oldIdle
	}
}

// TestSlowHealthyStreamSurvivesIdleWatchdog 进度敏感：只要流还在推进，就不该被任何
// 总时长上限掐断。空闲看门狗调成 300ms（远小于本流 1.5s 的总时长）也不得触发——
// 这正是与事故里那把「整请求 300s」刀的差别。
func TestSlowHealthyStreamSurvivesIdleWatchdog(t *testing.T) {
	restore := setStreamTimeouts(t, 2*time.Second, 300*time.Millisecond)
	defer restore()

	server := sseServer(t, tokens(10), 150*time.Millisecond, 0)
	completer := ClientFor(testAccountSpec(server.URL))
	streamer, ok := completer.(agent.StreamCompleter)
	if !ok {
		t.Fatalf("ClientFor must produce a streaming completer, got %T", completer)
	}
	content, _, _, err := streamer.CompleteStream(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("健康但缓慢的流被掐断：%v", err)
	}
	if content != strings.Join(tokens(10), "") {
		t.Fatalf("stream content = %q, want %q", content, strings.Join(tokens(10), ""))
	}
}

// TestStalledStreamFailsWithReadableError 停滞保护仍在：上游真挂住（不再有数据）时，
// 看门狗必须在空闲上限附近失败，且错误文本自带语义——不再需要从
// `(Client.Timeout or context cancellation while reading body)` 里猜是「挂了」还是「在跑」。
func TestStalledStreamFailsWithReadableError(t *testing.T) {
	restore := setStreamTimeouts(t, 2*time.Second, 250*time.Millisecond)
	defer restore()

	server := sseServer(t, tokens(2), 20*time.Millisecond, 3*time.Second)
	completer := ClientFor(testAccountSpec(server.URL))
	streamer, ok := completer.(agent.StreamCompleter)
	if !ok {
		t.Fatalf("ClientFor must produce a streaming completer, got %T", completer)
	}
	start := time.Now()
	_, _, _, err := streamer.CompleteStream(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("停滞的流必须失败")
	}
	if !strings.Contains(err.Error(), "LLM stream stalled") {
		t.Fatalf("停滞错误必须自带语义，got: %v", err)
	}
	if strings.Contains(err.Error(), "(Client.Timeout or context cancellation while reading body)") {
		t.Fatalf("不得退回按总时长掐流的措辞，got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("停滞必须在空闲上限附近被发现，实际等待 %s", elapsed)
	}
}
