// Smoke test — 构建验证 + LLM 连通性
// 运行: go test -run Smoke -v -timeout=10s
package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/Seele/types"
)

func TestSmokeLLM(t *testing.T) {
	configPath := "config/account-openai.yaml"
	if v := os.Getenv("SEELEX_CONFIG"); v != "" {
		configPath = v
	}

	// 1. 加载配置
	result, err := api.LoadFullAccountsConfig(configPath)
	if err != nil {
		t.Skipf("跳过: 加载配置失败: %v", err)
	}
	first := result.Pool.All()[0]
	if first.APIKey == "" || first.APIKey == "sk-your-api-key-here" {
		t.Skip("跳过: 未配置有效 API Key")
	}

	llmCfg := types.LLMConfig{
		BaseURL: first.BaseURL, APIKey: first.APIKey, Model: first.Model,
		MaxTokens: 128, Timeout: 10, Temperature: 0,
	}

	// 2. Agent
	agt, err := agent.New(agent.Options{LLMConfig: llmCfg, ToolCallTimeOut: 30 * time.Second})
	if err != nil {
		t.Fatalf("Agent 初始化失败: %v", err)
	}
	defer agt.Shutdown()

	// 3. Engine
	tmpDir, _ := os.MkdirTemp("", "seelex-smoke")
	defer os.RemoveAll(tmpDir)
	store, _ := storage.NewStore(tmpDir)
	eng := engine.New(agt,
		engine.WithStore(store),
		engine.WithTracer(tracer.NewSimpleTracer()),
	)

	// 4. 发送并计时
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
