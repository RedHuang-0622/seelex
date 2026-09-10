package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// TestSystemPositionLiveProbe 是 provider 对 system 位置能力的真实实验：
// 在 user 历史之后插入一条 system 状态材料，再继续 user 输入，观察端点是否接受。
//
// 运行：
//
//	$env:SEELEX_LIVE_SMOKE='1'
//	go test ./seelebridge -run TestSystemPositionLiveProbe -v -count=1 -timeout 5m
func TestSystemPositionLiveProbe(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 to run the real-API system position probe")
	}
	accountsPath := os.Getenv("SEELEX_ACCOUNTS_PATH")
	if accountsPath == "" {
		accountsPath = filepath.Join("..", "config", "accounts.yaml")
	}
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    accountsPath,
		ToolCallTimeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Shutdown()
	sess, err := runtime.NewMainSessionWithID("system-position-probe", nil)
	if err != nil {
		t.Fatalf("NewMainSessionWithID: %v", err)
	}

	user1 := "第一轮用户输入。"
	state := "<!-- seelex:task-state:v1 --> 当前 task 状态：正在恢复未完成步骤。"
	sess.AppendHistory(types.Message{Role: "user", Content: &user1})
	sess.AppendHistory(types.Message{Role: "system", Content: &state})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	answer, err := sess.Chat(ctx, "请只回复 OK，不要调用任何工具。")
	if err != nil {
		t.Fatalf("provider rejected mid-history system role: %v", err)
	}
	t.Logf("provider accepted mid-history system role; answer=%q", answer)
}
