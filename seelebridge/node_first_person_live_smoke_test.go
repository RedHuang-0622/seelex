package seelebridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// TestNodeFirstPersonLiveSmoke 真实 API 冒烟（非默认运行）：
//   - 需要真实账号：SEELEX_ACCOUNTS_PATH（默认 ../config/accounts.yaml）；
//   - 运行：$env:SEELEX_LIVE_SMOKE=1; go test ./seelebridge -run TestNodeFirstPersonLiveSmoke -v -timeout=25m
//
// 认证目标（与 GUI 子代理详情"第一视角"tab 同一条实时路径
// Runtime.SubscribeSubagentLive → dto.SubagentLiveEvent）：
//  1. 第一视角是**即时推送**的：订阅后每收到一条（阶段/工具）立即打印，
//     子代理每走一步（调用什么工具、拿到什么结果、进入新一轮）立刻可见，
//     而非运行结束后一次性 dump；
//  2. 同一 subagent 的多阶段共享同一 SessionID（不是多个 subagent 拼凑）；
//  3. 语义结果返回是预定义结构（NodeSemanticResult），经语义结果队列可读取。
func TestNodeFirstPersonLiveSmoke(t *testing.T) {
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

	const nodeID = "live_view"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// 订阅统一实时流（产品路径；必须先于 fork 启动，避免漏事件）。
	historyBefore, liveEvents, cancelLive, err := runtime.SubscribeSubagentLive(nodeID)
	if err != nil {
		t.Fatalf("SubscribeSubagentLive: %v", err)
	}
	defer cancelLive()
	if len(historyBefore) != 0 {
		t.Fatalf("history before fork = %d, want 0", len(historyBefore))
	}

	// fork 在后台运行，主测试 goroutine 即时消费实时流。
	forkDone := make(chan struct{})
	var forkResult string
	var forkErr error
	go func() {
		defer close(forkDone)
		started := time.Now()
		forkResult, forkErr = runtime.Agent().DirectDispatch(ctx, "fork_subagents",
			`{"subagents":[{"id":"`+nodeID+`","goal":"分三步完成：1) 获取当前系统时间；2) 读取仓库根目录 README.md 的前 20 行；3) 用一句话总结 Seelex 是什么"}]}`)
		t.Logf("=== 真实 API fork 完成（耗时 %s）===", time.Since(started))
	}()

	t.Logf("=== %s 第一视角即时输出（订阅实时流，收到即打印）===", nodeID)
	var liveStages []dto.NodeStageLog
	var liveTools []dto.SubagentTool
	consume := func(event dto.SubagentLiveEvent) {
		if event.NodeID != nodeID {
			return
		}
		switch event.Kind {
		case "tool":
			if event.Tool == nil {
				return
			}
			liveTools = append(liveTools, *event.Tool)
			t.Logf("[即时] %s 工具 %-10s status=%s result=%s",
				time.Now().Format("15:04:05.000"), event.Tool.Name, event.Tool.Status,
				livePreview(event.Tool.Result))
		default:
			if event.Stage == nil {
				return
			}
			liveStages = append(liveStages, *event.Stage)
			t.Logf("[即时] %s 收到 stage=%-8s turn=%d session=%s preview=%s",
				time.Now().Format("15:04:05.000"), event.Stage.Stage, event.Stage.Turn,
				event.Stage.SessionID, event.Stage.Preview)
		}
	}
	for {
		select {
		case event := <-liveEvents:
			consume(event)
		case <-forkDone:
			for {
				select {
				case event := <-liveEvents:
					consume(event)
				default:
					goto forkFinished
				}
			}
		case <-ctx.Done():
			t.Fatalf("fork did not finish before ctx deadline: %v", ctx.Err())
		}
	}

forkFinished:
	if forkErr != nil {
		t.Fatalf("fork_subagents live failed: %v", forkErr)
	}
	t.Logf("%s", forkResult)

	// 认证 A：即时输出成立——阶段与工具事件都经统一实时流收到。
	if len(liveStages) < 3 {
		t.Fatalf("live stage events = %d, want >= 3", len(liveStages))
	}
	successTools := 0
	for _, tool := range liveTools {
		if tool.Status == "success" {
			successTools++
		}
	}
	if successTools == 0 {
		t.Fatal("no successful tool event received live — 工具调用与结果必须即时输出")
	}

	// 认证 B：同一 subagent 的多阶段共享 SessionID + 逐步时间。
	if liveStages[0].SessionID == "" {
		t.Fatal("live stage events carry empty session id")
	}
	for index, stage := range liveStages {
		if stage.SessionID != liveStages[0].SessionID {
			t.Fatalf("live stage %d session id = %q, want %q — 分阶段上下文必须出自同一 subagent",
				index, stage.SessionID, liveStages[0].SessionID)
		}
	}
	for index := 1; index < len(liveStages); index++ {
		if liveStages[index].At.Before(liveStages[index-1].At) {
			t.Fatalf("live stage %d at %s before previous — 阶段必须随时间逐步产出",
				index, liveStages[index].At.Format("15:04:05.000"))
		}
	}

	// 认证 C：预定义语义结果返回（对象结构由 seelex 制定，非 subagent 自拟）。
	view := runtime.NodeFirstPersonView(nodeID)
	if view == nil {
		t.Fatal("first-person view missing")
	}
	res := view.Result
	if res == nil {
		t.Fatal("semantic result missing")
	}
	if res.SchemaVersion != model.NodeSemanticSchemaVersion {
		t.Fatalf("schema version = %d, want %d", res.SchemaVersion, model.NodeSemanticSchemaVersion)
	}
	if res.NodeID != nodeID || res.SessionID != liveStages[0].SessionID {
		t.Fatalf("result identity node/session = %q/%q, want %q/%q",
			res.NodeID, res.SessionID, nodeID, liveStages[0].SessionID)
	}
	if res.Status == "" {
		t.Fatal("semantic result status empty")
	}
	if res.Output == "" && res.Summary == "" {
		t.Fatal("semantic result has no output/summary")
	}

	// 认证 D：消息队列路径可读取语义结果。
	drained := runtime.DrainSubagentSemanticResults()
	if len(drained) == 0 {
		t.Fatal("semantic result queue is empty — 消息队列路径未收到结果")
	}
	found := false
	for _, item := range drained {
		if item.NodeID == nodeID {
			found = true
			t.Logf("=== 语义结果经消息队列返回（schema v%d, status=%s）===\n%s",
				item.SchemaVersion, item.Status, mustIndentJSON(t, item))
			break
		}
	}
	if !found {
		t.Fatalf("semantic result queue has no entry for %q", nodeID)
	}

	// 认证 E：历史回放缓存——fork 结束后再订阅，仍能看到从 subagent start
	// 到最新的完整事件流（阶段 + 工具），而非只从打开时刻开始。
	history, _, cancelHistory, err := runtime.SubscribeSubagentLive(nodeID)
	if err != nil {
		t.Fatalf("re-subscribe for history: %v", err)
	}
	defer cancelHistory()
	if len(history) < len(liveStages) {
		t.Fatalf("history replay = %d events, want >= %d stage events", len(history), len(liveStages))
	}
	historyTools := 0
	for _, event := range history {
		if event.Kind == "tool" {
			historyTools++
		}
	}
	if historyTools < successTools {
		t.Fatalf("history replay tools = %d, want >= %d", historyTools, successTools)
	}
	t.Logf("=== 历史回放验证：重新订阅仍可见 %d 条事件（stage %d + tool %d）===",
		len(history), len(history)-historyTools, historyTools)
}

// livePreview 文本的有界单行预览（换行折叠，≤120 字符）。
func livePreview(value string) string {
	compact := strings.Join(strings.Fields(value), " ")
	if len(compact) > 120 {
		return compact[:120] + "…"
	}
	return compact
}

func mustIndentJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
