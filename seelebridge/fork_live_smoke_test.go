package seelebridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
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
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 15 * 60,
		},
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

	// 作业化派发：调用立刻返回句柄，结果经 job_manage(op=fetch) 取回；真实 API 下
	// 等待窗口按 15 分钟给足。
	started := time.Now()
	receipt, err := forkDispatch(t, runtime,
		`{"subagents":[
			{"id":"live_time","goal":"获取当前系统时间并格式化为 yyyy-MM-dd HH:mm:ss"},
			{"id":"live_file","goal":"读取仓库根目录 README.md 的前 20 行，用两句话总结 Seelex 是什么"}
		]}`)
	if err != nil {
		t.Fatalf("fork_subagents live failed (%s): %v", time.Since(started), err)
	}
	handles := make([]string, 0, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		handles = append(handles, job.Handle)
	}
	forkWaitTerminalFor(t, runtime, handles, 15*time.Minute)
	combined := make([]string, 0, len(handles))
	for _, handle := range handles {
		combined = append(combined, forkFetch(t, runtime, handle))
	}
	result := strings.Join(combined, "\n")
	t.Logf("=== 真实 API fork 冒烟（耗时 %s）===\n%s", time.Since(started), result)
	for _, id := range []string{"live_time", "live_file"} {
		if !strings.Contains(result, id) {
			t.Fatalf("取回的产出必须覆盖 %s: %s", id, result)
		}
	}
	t.Logf("耗时: %s；完整会话/打点见工作区子代理树", time.Since(started))
}
