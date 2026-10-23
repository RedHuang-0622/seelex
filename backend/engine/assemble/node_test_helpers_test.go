package assemble

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/store"
)

// ── 测试辅助 ─────────────────────────────────────────────────────────

// newTestRouter 创建 JSON 后端测试 Router（事件库落盘目标）。
func newTestRouter(t testing.TB) *store.Router {
	t.Helper()
	baseDir := t.TempDir()
	router, err := store.NewRouter(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// scriptedNodeCompleter 是确定性节点 completer（无网络）：记录每次请求的完整
// 消息与可见工具；release 非 nil 时阻塞到关闭（模拟慢节点）；probeTool 非空时
// 首轮发出该工具调用（验证 Dispatch 可见性）；failErr 非空时直接失败。
type scriptedNodeCompleter struct {
	mu        sync.Mutex
	startOnce sync.Once
	started   chan struct{}
	release   chan struct{}
	requests  [][]types.Message
	seenTools [][]types.Tool
	probeTool string
	reply     string
	failErr   error
}

func newScriptedNodeCompleter(reply string) *scriptedNodeCompleter {
	return &scriptedNodeCompleter{started: make(chan struct{}), reply: reply}
}

func (c *scriptedNodeCompleter) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	c.mu.Lock()
	c.requests = append(c.requests, cloneMessages(messages))
	c.seenTools = append(c.seenTools, cloneTools(tools))
	c.mu.Unlock()
	c.startOnce.Do(func() { close(c.started) })
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return types.Message{}, ctx.Err()
		}
	}
	if c.failErr != nil {
		return types.Message{}, c.failErr
	}
	// 首轮：发出 probeTool 工具调用（下一轮返回最终文本）。
	if c.probeTool != "" && len(c.requests) == 1 {
		return types.Message{
			Role: "assistant",
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: c.probeTool, Arguments: "{}"},
			}},
		}, nil
	}
	reply := c.reply
	if reply == "" {
		reply = "node-done"
	}
	return types.Message{Role: "assistant", Content: &reply}, nil
}

// newTestRuntimeWithSubagents 构造带两个子代理账号的测试 Runtime。
func newTestRuntimeWithSubagents(t testing.TB) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
  subagent:
    - model: child-one
      base_url: http://localhost
      api_key: test-key
    - model: child-two
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}

// injectScriptedCompleters 用确定性 completer 替换账号池（不发起网络调用）。
func injectScriptedCompleters(t *testing.T, runtime *Runtime, completers map[string]agent.Completer) {
	t.Helper()
	for _, entry := range runtime.pool.Entries() {
		if err := runtime.pool.Unregister(entry.Snapshot.ID); err != nil {
			t.Fatalf("unregister %q: %v", entry.Snapshot.ID, err)
		}
	}
	for id, value := range completers {
		if err := runtime.pool.Register(accountpool.Account[agent.Completer]{
			ID: id, Value: value, MaxConcurrency: 1,
			Metadata: map[string]string{"provider": "openai", "model": "test-model"},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func cloneMessages(messages []types.Message) []types.Message {
	cloned := make([]types.Message, len(messages))
	copy(cloned, messages)
	for index := range cloned {
		if cloned[index].Content != nil {
			value := *cloned[index].Content
			cloned[index].Content = &value
		}
	}
	return cloned
}

func cloneTools(tools []types.Tool) []types.Tool {
	return append([]types.Tool(nil), tools...)
}

func joinMessageContents(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Content != nil {
			builder.WriteString(*message.Content)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func toolNamesFromTypes(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
