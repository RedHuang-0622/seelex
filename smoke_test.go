// Smoke test — 构建验证 + LLM 连通性
// 运行: go test -run Smoke -v -timeout=10s
package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

func TestSmokeLLM(t *testing.T) {
	cfgPath := accountsPath()

	// 1. 创建框架运行时
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: cfgPath, ToolCallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Skipf("跳过: 加载配置失败: %v", err)
	}
	defer runtime.Shutdown()

	// 2. Engine
	tmpDir, _ := os.MkdirTemp("", "seelex-smoke")
	defer os.RemoveAll(tmpDir)
	store, err := seelebridge.NewSessionStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(runtime.Agent(),
		engine.WithStore(store.FrameworkStore()),
		engine.WithTracer(seelebridge.NewTracer()),
	)

	// 3. 发送并计时
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := eng.Chat(ctx, "你好，请用一句话回复我")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Chat 失败: %v (耗时 %v)", err, elapsed)
	}
	if reply == "" {
		t.Fatal("回复为空")
	}

	fmt.Printf("  回复: %s\n", reply)
	fmt.Printf("  耗时: %v\n", elapsed)

	if elapsed > 3*time.Second {
		t.Logf("⚠ 耗时 %.1fs > 3s，需要优化", elapsed.Seconds())
	}
}
