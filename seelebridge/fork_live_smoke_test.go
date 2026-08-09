package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestForkSubagentsLiveSmoke 真实 API 冒烟（非默认运行）：
//   - 需要真实账号配置：SEELEX_ACCOUNTS_PATH（默认 ../config/accounts.yaml，
//     只传路径给 Runtime，不读取内容）；
//   - 运行：$env:SEELEX_LIVE_SMOKE=1; go test ./seelebridge -run TestForkSubagentsLiveSmoke -v
//
// 场景：双子代理 fork——一个取时间、一个读 README 总结，summary 节点合并。
func TestForkSubagentsLiveSmoke(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 to run the real-API smoke")
	}
	accountsPath := os.Getenv("SEELEX_ACCOUNTS_PATH")
	if accountsPath == "" {
		accountsPath = filepath.Join("..", "config", "accounts.yaml")
	}
	root, err := filepath.Abs(filepath.Join(".."))
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:      accountsPath,
		ToolCallTimeout:   5 * time.Minute,
		ApprovalTimeout:   10 * time.Minute,
		HeartbeatInterval: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(root); err != nil {
		t.Fatalf("BindProjectRoot: %v", err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	started := time.Now()
	result, err := runtime.Agent().DirectDispatch(ctx, "fork_subagents",
		`{"subagents":[
			{"id":"live_time","goal":"获取当前系统时间并格式化为 yyyy-MM-dd HH:mm:ss"},
			{"id":"live_file","goal":"读取仓库根目录 README.md 的前 20 行，用两句话总结 Seelex 是什么"}
		]}`)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("fork_subagents live failed (%s): %v", elapsed, err)
	}
	t.Logf("=== 真实 API fork 冒烟（耗时 %s）===\n%s", elapsed, result)
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("fork result must be completed, got: %s", result)
	}
	if !strings.Contains(result, "live_time") || !strings.Contains(result, "live_file") {
		t.Fatalf("result must carry both subagent outputs: %s", result)
	}
	t.Logf("耗时: %s；完整会话/打点见工作区子代理树", elapsed)
}
