package seelebridge

// fork_job_helpers_test.go — fork_subagents **作业化派发**（后台）的用例助手。
//
// fork_subagents 只走作业面：调用立刻返回每个子代理的句柄，产出经
// job_manage(op=fetch, handle) 取回。这些助手把"派发 → 等终态 → 取回"三段折成一次
// 调用，让既有用例断言产出正文时不必每个文件各写一遍轮询。
//
// 为什么用例要显式打开作业面：`limits.async_exec.enabled` 出厂 true，但
// RuntimeConfig 的结构零值是 false（对齐"旧配置缺这段 = 关"的纪律），测试基座因此
// 必须显式打开——否则 fork_subagents 会按契约**显式报错**（它不静默退化成阻塞调用）。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// newAsyncTestRuntime 造一个打开了作业面（含子代理作业）的测试 Runtime。
func newAsyncTestRuntime(t testing.TB) *Runtime {
	t.Helper()
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
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    path,
		ToolCallTimeout: 30 * time.Second,
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 120,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}

// newAsyncTestRuntimeWithSubagents 同上，但账号池带两个子代理账号（并行分支各占一个）。
func newAsyncTestRuntimeWithSubagents(t testing.TB) *Runtime {
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
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    path,
		ToolCallTimeout: 30 * time.Second,
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 120,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}

// forkBatch 是一次作业化派发的读数：句柄、**取回前**读到的终态、以及各句柄增量拼成的正文。
type forkBatch struct {
	// Handles 是受理回执里的句柄（与 Jobs 同序）。
	Handles []string
	// IDs 是子代理 id → 句柄（按 id 断言某一条作业的终态时用）。
	IDs map[string]string
	// States 是句柄 → **取回前**读到的终态（终态作业一经取回就销项，之后查不到）。
	States map[string]string
	// Output 是各句柄增量按句柄序拼成的正文。
	Output string
}

// forkReceipt 是受理回执的形状（与 fork.acceptanceReceipt 逐字段对应）。
type forkReceipt struct {
	Status string `json:"status"`
	Jobs   []struct {
		Handle string `json:"handle"`
		ID     string `json:"id"`
		State  string `json:"state"`
	} `json:"jobs"`
}

// forkDispatch 派发一批子代理，返回受理回执（句柄表）。
func forkDispatch(t *testing.T, runtime *Runtime, args string) (forkReceipt, error) {
	t.Helper()
	raw, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents", args)
	if err != nil {
		return forkReceipt{}, err
	}
	var receipt forkReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatalf("受理回执不是合法 JSON: %v (%q)", err, raw)
	}
	if receipt.Status != "accepted" || len(receipt.Jobs) == 0 {
		t.Fatalf("受理回执 = %+v, want accepted + 至少一个句柄（%q）", receipt, raw)
	}
	return receipt, nil
}

// forkRun 走一遍完整的三段：派发（作业化）→ 等全部子代理作业终态 → 逐个取回增量。
//
// 批次状态在**取回前**读取：终态作业一经取回就从登记表销项，之后再查就只剩墓碑。
func forkRun(t *testing.T, runtime *Runtime, args string) forkBatch {
	t.Helper()
	receipt, err := forkDispatch(t, runtime, args)
	if err != nil {
		t.Fatalf("fork_subagents 派发失败: %v", err)
	}
	handles := make([]string, 0, len(receipt.Jobs))
	ids := make(map[string]string, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		handles = append(handles, job.Handle)
		ids[job.ID] = job.Handle
	}
	forkWaitTerminal(t, runtime, handles)

	states := make(map[string]string, len(handles))
	for _, record := range runtime.AsyncRunsSnapshot() {
		states[record.Handle] = record.State.String()
	}
	parts := make([]string, 0, len(handles))
	for _, handle := range handles {
		parts = append(parts, forkFetch(t, runtime, handle))
	}
	return forkBatch{Handles: handles, IDs: ids, States: states, Output: strings.Join(parts, "\n")}
}

// forkAndWait 是最常用的形态：只需要子代理的输出正文。
func forkAndWait(t *testing.T, runtime *Runtime, args string) string {
	t.Helper()
	return forkRun(t, runtime, args).Output
}

// forkWaitTerminal 等到给定句柄全部不在 running（终态由执行体判定，这里只观察）。
func forkWaitTerminal(t *testing.T, runtime *Runtime, handles []string) {
	t.Helper()
	forkWaitTerminalFor(t, runtime, handles, 60*time.Second)
}

// forkWaitTerminalFor 是 forkWaitTerminal 的显式预算形态（真实 API 冒烟用更宽的窗口）。
func forkWaitTerminalFor(t *testing.T, runtime *Runtime, handles []string, budget time.Duration) {
	t.Helper()
	wanted := make(map[string]struct{}, len(handles))
	for _, handle := range handles {
		wanted[handle] = struct{}{}
	}
	deadline := time.Now().Add(budget)
	for {
		live := 0
		for _, record := range runtime.AsyncRunsSnapshot() {
			if record.State != dto.AsyncStateRunning {
				continue
			}
			if _, ok := wanted[record.Handle]; ok {
				live++
			}
		}
		if live == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("子代理作业未在预算内收尾（仍在跑 %d 个）", live)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// forkStateOf 读一条作业的当前状态（只读，不消费输出）。句柄已销项时返回空串。
func forkStateOf(t *testing.T, runtime *Runtime, handle string) string {
	t.Helper()
	for _, record := range runtime.AsyncRunsSnapshot() {
		if record.Handle == handle {
			return record.State.String()
		}
	}
	return ""
}

// forkFetch 取回一个子代理作业的增量（消费式）。
func forkFetch(t *testing.T, runtime *Runtime, handle string) string {
	t.Helper()
	out, err := runtime.Agent().DirectDispatch(context.Background(), "job_manage",
		`{"op":"fetch","handle":"`+handle+`","wait_ms":-1}`)
	if err != nil {
		t.Fatalf("job_manage(op=fetch, %s): %v", handle, err)
	}
	var payload struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("取回载荷不是合法 JSON: %v (%q)", err, out)
	}
	return payload.Output
}
