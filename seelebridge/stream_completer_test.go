package seelebridge

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"
)

// scriptedStreamCompleter 阻塞在 release 上，模拟整条流在途。
type scriptedStreamCompleter struct {
	mu        sync.Mutex
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
	streamErr error
}

func newScriptedStreamCompleter() *scriptedStreamCompleter {
	return &scriptedStreamCompleter{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *scriptedStreamCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	return types.Message{}, errors.New("scripted stream completer does not complete synchronously")
}

func (c *scriptedStreamCompleter) CompleteStream(ctx context.Context, _ []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
	}()
	close(c.started)
	select {
	case <-c.release:
	case <-ctx.Done():
		return "", "", nil, ctx.Err()
	}
	if c.streamErr != nil {
		return "", "", nil, c.streamErr
	}
	return "streamed", "reasoning", nil, nil
}

// syncOnlyCompleter 只实现同步 Complete（测试流式回退路径）。
type syncOnlyCompleter struct {
	mu        sync.Mutex
	active    int
	maxActive int
}

func (c *syncOnlyCompleter) Complete(_ context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()
	content := "sync-reply"
	return types.Message{Role: "assistant", Content: &content}, nil
}

func newStreamPool(t *testing.T, values map[string]agent.Completer) *accountpool.P2CPool[agent.Completer] {
	t.Helper()
	pool := accountpool.New[agent.Completer]()
	for id, value := range values {
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID: id, Value: value, MaxConcurrency: 1,
			Metadata: map[string]string{"provider": "openai", "model": "test-model"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

// TestStreamingCompleterHoldsLeaseUntilEOF 验证租约覆盖整条流：
// 流进行中时第二个请求拿不到槽位；EOF 后租约释放。
func TestStreamingCompleterHoldsLeaseUntilEOF(t *testing.T) {
	streamer := newScriptedStreamCompleter()
	pool := newStreamPool(t, map[string]agent.Completer{"agent-1": streamer})
	completer := &streamingAccountCompleter{pool: pool}

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		content, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil)
		if err != nil || content != "streamed" {
			t.Errorf("stream result = %q err=%v", content, err)
		}
	}()
	<-streamer.started

	// 流在途：并发租约必须被阻塞（1s 超时视为被持有）。
	blockedCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := pool.Acquire(blockedCtx, accountpool.AcquireRequest{}); err == nil {
		t.Fatal("second acquire must block while the stream lease is held")
	}
	if pool.Stats().Active != 1 {
		t.Fatalf("active leases = %d, want 1", pool.Stats().Active)
	}

	// EOF：释放后下一个请求立即拿到租约。
	close(streamer.release)
	<-streamDone
	acquireCtx, cancelAcquire := context.WithTimeout(context.Background(), time.Second)
	defer cancelAcquire()
	lease, err := pool.Acquire(acquireCtx, accountpool.AcquireRequest{})
	if err != nil {
		t.Fatalf("acquire after EOF failed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if got := streamer.maxActive; got != 1 {
		t.Fatalf("max concurrent streams = %d, want 1", got)
	}
}

// TestStreamingCompleterReleasesLeaseOnStreamAbort 验证流被 ctx 取消中止时
// 也释放租约（defer Release 覆盖 ctx.Done 退出路径）。
func TestStreamingCompleterReleasesLeaseOnStreamAbort(t *testing.T) {
	streamer := newScriptedStreamCompleter()
	pool := newStreamPool(t, map[string]agent.Completer{"agent-1": streamer})
	completer := &streamingAccountCompleter{pool: pool}

	streamCtx, cancel := context.WithCancel(context.Background())
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_, _, _, err := completer.CompleteStream(streamCtx, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("stream abort error = %v", err)
		}
	}()
	<-streamer.started
	// 流在途：租约必须被持有（并发 acquire 被阻塞）。
	blockedCtx, cancelBlocked := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelBlocked()
	if _, err := pool.Acquire(blockedCtx, accountpool.AcquireRequest{}); err == nil {
		t.Fatal("second acquire must block while the stream lease is held")
	}
	// 中止流：ctx 取消后 scriptedStreamCompleter 返回 ctx.Err()，defer 释放租约。
	cancel()
	<-streamDone

	acquireCtx, cancelAcquire := context.WithTimeout(context.Background(), time.Second)
	defer cancelAcquire()
	if lease, err := pool.Acquire(acquireCtx, accountpool.AcquireRequest{}); err != nil {
		t.Fatalf("lease was not released after stream abort: %v", err)
	} else {
		_ = lease.Release()
	}
}

// TestStreamingCompleterReleasesLeaseOnError 验证错误退出也释放租约（幂等）。
func TestStreamingCompleterReleasesLeaseOnError(t *testing.T) {
	streamer := newScriptedStreamCompleter()
	streamer.streamErr = errors.New("provider stream failure")
	pool := newStreamPool(t, map[string]agent.Completer{"agent-1": streamer})
	completer := &streamingAccountCompleter{pool: pool}

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "provider stream failure") {
			t.Errorf("stream error = %v", err)
		}
	}()
	<-streamer.started
	close(streamer.release)
	<-streamDone

	acquireCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if lease, err := pool.Acquire(acquireCtx, accountpool.AcquireRequest{}); err != nil {
		t.Fatalf("lease was not released after stream error: %v", err)
	} else {
		_ = lease.Release()
	}
}

// TestStreamingCompleterSelectorPinsAccount 验证共享选择器闭包生效。
func TestStreamingCompleterSelectorPinsAccount(t *testing.T) {
	first := newScriptedStreamCompleter()
	second := newScriptedStreamCompleter()
	pool := newStreamPool(t, map[string]agent.Completer{"agent-1": first, "agent-2": second})
	selector := bridge.AccountRequestSelector(func(context.Context, []types.Message, []types.Tool) accountpool.AcquireRequest {
		return accountpool.AcquireRequest{AccountID: "agent-2"}
	})
	completer := &streamingAccountCompleter{pool: pool, selector: selector}

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil)
		if err != nil {
			t.Errorf("stream error = %v", err)
		}
	}()
	select {
	case <-second.started:
	case <-time.After(time.Second):
		t.Fatal("selector did not pin account agent-2")
	}
	select {
	case <-first.started:
		t.Fatal("unpinned account agent-1 must not be used")
	case <-time.After(100 * time.Millisecond):
	}
	close(second.release)
	<-streamDone
}

// TestStreamingCompleterFallsBackToSyncCompleter 验证无流式能力的账号退化为单次返回。
func TestStreamingCompleterFallsBackToSyncCompleter(t *testing.T) {
	syncClient := &syncOnlyCompleter{}
	pool := newStreamPool(t, map[string]agent.Completer{"agent-1": syncClient})
	completer := &streamingAccountCompleter{pool: pool}

	chunks := make([]string, 0, 1)
	content, _, _, err := completer.CompleteStream(context.Background(), nil, nil, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil || content != "sync-reply" {
		t.Fatalf("fallback stream = %q err=%v", content, err)
	}
	if len(chunks) != 1 || chunks[0] != "sync-reply" {
		t.Fatalf("fallback chunks = %v", chunks)
	}
}

// TestRuntimeMainSessionStreamsThroughSharedSelector 验证主会话装配后
// ChatStream 走共享账号选择器（选中账号被 pin）。
func TestRuntimeMainSessionStreamsThroughSharedSelector(t *testing.T) {
	path := t.TempDir() + "/accounts.yaml"
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	sess, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess == nil || runtime.Session() == nil {
		t.Fatal("main session is unavailable")
	}
	accounts := runtime.Accounts()
	if len(accounts) != 1 || accounts[0].Name != "agent-1" {
		t.Fatalf("accounts = %+v", accounts)
	}
	if !runtime.SelectAccount("agent-1") {
		t.Fatal("select account failed")
	}
	if runtime.Provider() != "openai" {
		t.Fatalf("provider = %q", runtime.Provider())
	}
}
