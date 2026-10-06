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
	"github.com/RedHuang-0622/seelex/seelexctx"
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
		// 回放窗口显式放大：默认 512 是"给人看的一段"，而这一档要按**分页口径**核对
		// "整轮的 stage/tool 都留得住、且逐页读得到"（超出窗口会丢最旧，读数会变小）。
		SubagentLiveWindow: 4096,
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 20 * 60,
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

	// fork 作业化派发：调用立刻返回句柄；这个 goroutine 负责**等它收尾并取回**，
	// 主测试 goroutine 在等待期间即时消费实时流。
	forkDone := make(chan struct{})
	var forkResult string
	var forkErr error
	go func() {
		defer close(forkDone)
		started := time.Now()
		raw, dispatchErr := runtime.Agent().DirectDispatch(ctx, "fork_subagents",
			`{"subagents":[{"id":"`+nodeID+`","goal":"分三步完成：1) 获取当前系统时间；2) 读取仓库根目录 README.md 的前 20 行；3) 用一句话总结 Seelex 是什么"}]}`)
		if dispatchErr != nil {
			forkErr = dispatchErr
			return
		}
		var receipt forkReceipt
		if unmarshalErr := json.Unmarshal([]byte(raw), &receipt); unmarshalErr != nil {
			forkErr = unmarshalErr
			return
		}
		handles := make([]string, 0, len(receipt.Jobs))
		for _, job := range receipt.Jobs {
			handles = append(handles, job.Handle)
		}
		deadline := time.Now().Add(20 * time.Minute)
		for {
			live := false
			for _, record := range runtime.AsyncRunsSnapshot() {
				if record.State != dto.AsyncStateRunning {
					continue
				}
				for _, handle := range handles {
					if record.Handle == handle {
						live = true
					}
				}
			}
			if !live || time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		parts := make([]string, 0, len(handles))
		for _, handle := range handles {
			out, fetchErr := runtime.Agent().DirectDispatch(context.Background(), "job_manage",
				`{"op":"fetch","handle":"`+handle+`","wait_ms":-1}`)
			if fetchErr != nil {
				forkErr = fetchErr
				return
			}
			var payload struct {
				Output string `json:"output"`
			}
			_ = json.Unmarshal([]byte(out), &payload)
			parts = append(parts, payload.Output)
		}
		forkResult = strings.Join(parts, "\n")
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
		if tool.Status == dto.ToolEventSuccess {
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

	// 认证 E：历史回放缓存——**分页口径**（有界窗口 + 分页）。fork 结束后沿窗口逐页读，
	// 能读到从 subagent start 到最新的完整事件流（阶段 + 工具），而不是只有尾巴；
	// 且"逐页合计 = total"（分页不丢条、不重复）。
	// 窗口上限已在上面显式放大到 4096：整轮的 stage/tool 都留在窗口里。
	total := -1
	paged := 0
	pageCount := 0
	historyTools := 0
	for offset := 0; ; {
		page := runtime.SubagentLiveHistoryPage(nodeID, offset, 256)
		if pageCount == 0 {
			total = page.Total
			if total == 0 {
				t.Fatal("history replay window is empty — 回放窗口必须留下这一轮的事件")
			}
		}
		if page.Total != total {
			t.Fatalf("第 %d 页 total = %d, want %d（窗口在翻页过程里被改写？）", pageCount, page.Total, total)
		}
		if page.Offset != offset {
			t.Fatalf("第 %d 页 offset = %d, want %d", pageCount, page.Offset, offset)
		}
		pageCount++
		paged += len(page.Events)
		for _, event := range page.Events {
			if event.Kind == "tool" {
				historyTools++
			}
		}
		if !page.HasMore {
			break
		}
		next := page.Offset + len(page.Events)
		if next <= offset {
			t.Fatalf("has_more=true 但页不前进（offset %d → %d）：分页会死循环", offset, next)
		}
		offset = next
		if pageCount > 64 {
			t.Fatalf("翻页次数 %d 异常（窗口 %d / 页 256）", pageCount, total)
		}
	}
	if paged != total {
		t.Fatalf("逐页合计 = %d, want total = %d（分页必须不丢条、不重复）", paged, total)
	}
	if paged < len(liveStages) {
		t.Fatalf("history replay = %d events, want >= %d stage events", paged, len(liveStages))
	}
	if historyTools < successTools {
		t.Fatalf("history replay tools = %d, want >= %d", historyTools, successTools)
	}
	t.Logf("=== 历史回放验证（分页口径）：%d 页合计 %d 条 = total（stage %d + tool %d）===",
		pageCount, paged, paged-historyTools, historyTools)
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
