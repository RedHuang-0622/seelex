package seelebridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
)

// ── fork 批次失败策略：best-effort（2026-09-29 事故收口）───────────
//
// 事故形状：同批一个子代理的上游 LLM 流被整请求超时掐断（failed）→ 旧默认
// fail-fast 立即 cancel 整批 forkCtx → 还在跑的兄弟节点报出没有来由的
// "accountpool: acquire: context canceled"（session loop 100），已完成产出
// 一起被丢。本用例钉住修复后的行为：单节点失败不再连坐同批兄弟，兄弟节点
// 照常跑完并把产出交回父代理；失败节点仍必须显式报出。

// goalFailingCompleter 按 goal 决定行为：含 "audit module A" 的请求立刻失败
// （模拟上游流错误），其余 goal 阻塞 holdFor 后成功——两个账号共用同一实例，
// 因此无论节点落在哪个账号上，行为都由 goal 决定（与调度顺序无关）。
type goalFailingCompleter struct{ holdFor time.Duration }

func (c *goalFailingCompleter) Complete(ctx context.Context, messages []types.Message, _ []types.Tool) (types.Message, error) {
	if strings.Contains(joinMessageContents(messages), "audit module A") {
		return types.Message{}, errors.New("upstream LLM stream failed: context deadline exceeded" +
			" (Client.Timeout or context cancellation while reading body)")
	}
	select {
	case <-time.After(c.holdFor):
	case <-ctx.Done():
		return types.Message{}, ctx.Err()
	}
	reply := "SIBLING-SURVIVED: module B verified"
	return types.Message{Role: "assistant", Content: &reply}, nil
}

// TestForkSubagentsBestEffortKeepsSiblingAlive 验证一个子代理失败时，同批兄弟
// 不被连坐取消：慢的那个跑完、产出回到各自的作业正文；失败的那个**按它自己的
// 节点**判成 failed（不被整批状态抹平）。
func TestForkSubagentsBestEffortKeepsSiblingAlive(t *testing.T) {
	runtime := newAsyncTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	completer := &goalFailingCompleter{holdFor: 300 * time.Millisecond}
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"sub-1": completer,
		"sub-2": completer,
	})

	batch := forkRun(t, runtime,
		`{"subagents":[{"id":"s1","goal":"audit module A"},{"id":"s2","goal":"audit module B"}]}`)

	// 幸存兄弟节点的产出必须回到它自己的作业正文。
	if !strings.Contains(batch.Output, "SIBLING-SURVIVED") {
		t.Fatalf("幸存兄弟节点的产出必须可取回:\n%s", batch.Output)
	}
	// 终态按节点判定：失败的那个 failed，幸存的那个 done。
	if got := batch.States[batch.IDs["s1"]]; got != "failed" {
		t.Fatalf("失败节点 s1 的作业终态 = %q, want failed（读数 %+v）", got, batch.States)
	}
	if got := batch.States[batch.IDs["s2"]]; got != "done" {
		t.Fatalf("幸存节点 s2 的作业终态 = %q, want done（读数 %+v）", got, batch.States)
	}
	// 失败节点仍必须显式报出原因（整批归零以外的情形：作业正文带失败原因）。
	if !strings.Contains(batch.Output, "failed") {
		t.Fatalf("失败节点仍必须显式报出:\n%s", batch.Output)
	}
	t.Logf("best-effort 批次取回正文: %s", batch.Output)
}
