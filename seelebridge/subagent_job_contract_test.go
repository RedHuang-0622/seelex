package seelebridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 子代理作业契约（打点 L-5）的端到端判据：**模型侧**能把一批子代理当作业派发，
// 并在它们跑动期间 observe / 之后 fetch / 最后 done。
//
// 为什么必须是端到端：这条能力唯一的价值就是"模型有下一次调用"——单元测试能证明
// 登记表与状态机对，只有走一次真实工具调用才能证明"派发即返回 + 之后还能管"这条
// 链路是通的。
func TestForkSubagentsAsyncRunsAsJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key-not-used
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// 作业面（含子代理作业）由 limits.async_exec.enabled 总闸控制：这里显式打开。
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    path,
		ToolCallTimeout: 30 * time.Second,
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 60,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"s1": newScriptedNodeCompleter("async-sub-1 findings"),
		"s2": newScriptedNodeCompleter("async-sub-2 findings"),
	})
	ctx := context.Background()

	// ① 派发即返回：回执里是句柄，不是子代理结果。
	startedAt := time.Now()
	raw, err := runtime.Agent().DirectDispatch(ctx, "fork_subagents",
		`{"async":true,"subagents":[{"id":"s1","goal":"audit module A"},{"id":"s2","goal":"audit module B"}]}`)
	if err != nil {
		t.Fatalf("async fork 派发失败: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 20*time.Second {
		t.Fatalf("派发用了 %v：async 模式必须派发即返回", elapsed)
	}
	var receipt struct {
		Status string `json:"status"`
		Async  bool   `json:"async"`
		Jobs   []struct {
			Handle string `json:"handle"`
			ID     string `json:"id"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatalf("受理回执不是合法 JSON: %v (%q)", err, raw)
	}
	if receipt.Status != "accepted" || !receipt.Async || len(receipt.Jobs) != 2 {
		t.Fatalf("回执 = %+v, want accepted/async/2 句柄", receipt)
	}
	if strings.Contains(raw, "findings") {
		t.Fatalf("受理回执不得携带子代理产出: %q", raw)
	}
	handles := make([]string, 0, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		if job.Handle == "" {
			t.Fatalf("句柄缺失: %+v", receipt.Jobs)
		}
		handles = append(handles, job.Handle)
	}

	// ② 同一张投影：子代理行带着 Kind=subagent 出现（界面上与后台命令同一张表）。
	records := runtime.AsyncRunsSnapshot()
	kinds := map[string]string{}
	for _, record := range records {
		kinds[record.Handle] = record.Kind
	}
	for _, handle := range handles {
		if kinds[handle] != "subagent" {
			t.Fatalf("句柄 %s 的投影类别 = %q, want subagent（投影：%+v）", handle, kinds[handle], records)
		}
	}

	// ③ observe：只读看它在干什么（不消费输出）。
	observed, err := runtime.Agent().DirectDispatch(ctx, "job_manage", `{"op":"observe"}`)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !strings.Contains(observed, "subagent") {
		t.Fatalf("observe 读数里没有子代理行: %q", observed)
	}

	// ④ 等编排收尾，再 fetch 取回产出（消费式）。
	deadline := time.Now().Add(60 * time.Second)
	for {
		done := true
		for _, record := range runtime.AsyncRunsSnapshot() {
			if record.Kind == "subagent" && record.State == "running" {
				done = false
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("子代理作业未在预算内收尾")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var fetched []string
	for _, handle := range handles {
		out, err := runtime.Agent().DirectDispatch(ctx, "job_manage",
			`{"op":"fetch","handle":"`+handle+`","wait_ms":-1}`)
		if err != nil {
			t.Fatalf("fetch %s: %v", handle, err)
		}
		fetched = append(fetched, out)
	}
	joined := strings.Join(fetched, "\n")
	for _, want := range []string{"async-sub-1 findings", "async-sub-2 findings"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("取回的产出缺 %q:\n%s", want, joined)
		}
	}
	// 终态取回之后句柄销项：投影里不再有这两行。
	for _, record := range runtime.AsyncRunsSnapshot() {
		for _, handle := range handles {
			if record.Handle == handle {
				t.Fatalf("取回之后 %s 仍在投影里: %+v", handle, record)
			}
		}
	}
}
