//go:build restoreprobe

// 复现探针（build tag `restoreprobe` + `SEELEX_RESTORE_PROBE=1` 双门禁，默认跳过）：
// 「重启恢复出来的上下文，模型拿不到之前的上下文内容」。
//
// 用户口径（现场）：
//
//	① 重启（冷加载）之后，模型对早先的对话没有内容——只剩一份"折叠记录"；
//	② 现象时有时无，疑似与压缩的**时机/链路**有关；
//	③ 现场的帧常常是 summary_source=local：压缩发生了，但**没有调用模型**做内容摘要。
//
// 本探针在同一条会话上把两条折叠链各跑一遍，再在同一份存储上重新装配（= 重启），把
// "模型能看到的压缩上下文块"原样打印，并按需求本身断言（修复前为红；红在哪一行就是
// 缺陷在哪一处）：
//
//	R1 被折轮次的内容必须进模型可见面
//	   ——现在只有 "溢出轮次: 163 个完整协议单元" + 每轮 80 字截断的用户行，
//	     助手侧正文一个字都没有（"压缩完了只是折叠了上下文"）；
//	R2 "这次为什么没有模型摘要"的自答必须与事实一致
//	   ——开关是开的，帧却写"开关关闭或 QuickChat 装配失败"，读帧的人只会去查配置；
//	R3 帧链必须跨重启存活（探针保真度自检：红在 R1/R2 而不是读不到帧）。
//
// 运行：
//
//	$env:SEELEX_RESTORE_PROBE='1'; go test -tags restoreprobe ./seelebridge/ -run TestRestoreProbe -v -count=1
package seelebridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const (
	restoreProbeSession = "session-restore-probe"
	restoreProbeSystem  = "你是 Seelex，按证据工作的工程代理。"
	// restoreProbeFoldedRootCause 只出现在**被折轮次**的助手正文里：它是"模型有没有
	// 拿到被折内容"的判据串。
	restoreProbeFoldedRootCause = "根因：A3 写者在提交临界区里重入了读路径，第二条用例因此拿到过期快照。"
	// restoreProbeSummaryRootCause 只出现在**装配层折叠产出的厚摘要**里。
	restoreProbeSummaryRootCause = "摘要正文：二十七号那批 flaky 用例的高频失败点集中在提交临界区的锁序。"
)

// restoreProbeReply 是确定性 completer 给出的"厚摘要"（小节骨架 + 可辨识正文）。
func restoreProbeReply() string {
	return "### 目标 (Goal)\n" + restoreProbeSummaryRootCause + "\n" +
		"### 错误与修复 (Errors and Fixes)\n提交临界区里重入读路径，修法是把决定与交接拆开\n" +
		"### 下一步 (Next Step)\n复跑 compactlive 三件套"
}

type restoreProbeFixture struct {
	runtime   *Runtime
	router    *sessionstore.Router
	store     *sessionstore.SessionContextStore
	completer *scriptedNodeCompleter
}

// restoreProbeBoot 在同一份 root 上装配 Runtime（重启前 / 重启后各一次）。
//
// resume=true 模拟重启：先 Load 再挂接——生产入口
// internal/adapters.SessionPort.AttachSessionContext 就是这个顺序。顺序不能反：
// SetSystemPrompt 走 mutate 会把 loaded 置真，随后的 Load 直接返回、compact 通道
// 不再回读（那样量到的是探针自己的顺序缺陷，不是产品行为）。
func restoreProbeBoot(t *testing.T, root string, resume bool) *restoreProbeFixture {
	t.Helper()
	runtime := newTestRuntime(t)
	t.Cleanup(runtime.Shutdown)
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	runtime.AttachHistoryRouter(router)
	if _, err := runtime.NewMainSessionWithID(restoreProbeSession, nil); err != nil {
		t.Fatalf("NewMainSessionWithID: %v", err)
	}
	store := sessionstore.NewSessionContextStore(router, restoreProbeSession)
	if resume {
		if err := store.Load(context.Background()); err != nil {
			t.Fatalf("重启后 Load: %v", err)
		}
	} else if err := store.SetSystemPrompt(restoreProbeSystem); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	runtime.AttachSessionContextStore(store)
	completer := newScriptedNodeCompleter(restoreProbeReply())
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent": completer})
	runtime.RegisterBuiltins()
	runtime.limits.ContextCompactionSummary.Enabled = true
	return &restoreProbeFixture{runtime: runtime, router: router, store: store, completer: completer}
}

func (fixture *restoreProbeFixture) calls() int {
	requests, _ := fixture.completer.recorded()
	return len(requests)
}

// roundMessages 造 n 个完整协议单元（user + 大 assistant，答复里带判据串）。同一批
// 消息也要落进会话存储：compact 帧的区间（累计单元区号 → 事件 seq）由 message 事件行
// 反查，没有行就桥不进 compact 通道（json_layout.go 的 commitCompactFrameWorkspace）。
func roundMessages(n, answerChars int) []types.Message {
	messages := make([]types.Message, 0, n*2)
	for index := 0; index < n; index++ {
		question := fmt.Sprintf("第 %d 轮问题：这里的根因是什么？", index)
		answer := fmt.Sprintf("第 %d 轮答复：%s", index, restoreProbeFoldedRootCause+strings.Repeat("C", answerChars))
		messages = append(messages,
			types.Message{Role: "user", Content: &question},
			types.Message{Role: "assistant", Content: &answer})
	}
	return messages
}

// modelVisibleCompactBlock 返回重启后**模型真能看到**的压缩上下文块正文
// （装配顺序里 "压缩上下文 (now using compact context)" 那一个 PromptBlock）。
func modelVisibleCompactBlock(record sessionstore.SessionContextRecord) string {
	for _, block := range seelexctx.RenderStablePrefixBlocks(record) {
		if block.Name != "compact" || len(block.Messages) == 0 || block.Messages[0].Content == nil {
			continue
		}
		return *block.Messages[0].Content
	}
	return ""
}

func dumpProbeReport(t *testing.T, text string) {
	t.Helper()
	path := os.Getenv("SEELEX_RESTORE_PROBE_OUT")
	if path == "" {
		path = filepath.Join("_tmp", "restore-probe-report.md")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("dump report: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Logf("dump report: %v", err)
		return
	}
	t.Logf("现场已写入 %s", path)
}

func pointer(value string) *string { return &value }

func TestRestoreProbeModelSeesNoPriorContent(t *testing.T) {
	if os.Getenv("SEELEX_RESTORE_PROBE") == "" {
		t.Skip("set SEELEX_RESTORE_PROBE=1 to run the restart-restore context probe")
	}
	ctx := context.Background()
	root := t.TempDir()

	// ── 重启前 ────────────────────────────────────────────────────────
	fixture := restoreProbeBoot(t, root, false)
	rounds := roundMessages(200, 12_000)
	if err := sessionstore.NewRouterStorage(fixture.router, restoreProbeSession).Append(ctx, rounds); err != nil {
		t.Fatalf("seed session events: %v", err)
	}
	policy := seelexctx.NewContextWindowPolicy(fixture.runtime.ContextWindow(), fixture.runtime.MaxOutputTokens(), fixture.runtime.limits)
	t.Logf("配置: summary.enabled=%v window=%d budget=%d soft=%d hard=%d carry_tokens=%d",
		fixture.runtime.limits.ContextCompactionSummary.Enabled, policy.Window, policy.Budget(),
		policy.SoftThreshold(), policy.HardThreshold(), fixture.runtime.limits.ContextFrameCarryTokens)

	// 链 ①：装配层折叠（回合开始前那条路径 → seelebridge.MainCompactionDAG）——注入了摘要器。
	assemblyReceipt, err := fixture.runtime.PushCompactionFrame(ctx, restoreProbeSession, CompactionFrameRequest{
		Overflow: []types.Message{
			{Role: "user", Content: pointer("把保留窗口外的轮次折进 checkpoint 帧。")},
			{Role: "assistant", Content: pointer(strings.Repeat("B", 2000))},
		},
		ReplayHistory: []types.Message{
			{Role: "user", Content: pointer("上一轮：先读 seelebridge/runtime_context.go 的 compactionSummarizer。")},
			{Role: "assistant", Content: pointer("已读：开关关闭返回 nil，打开则构造 QuickChat 前缀重放摘要器。")},
		},
	})
	if err != nil {
		t.Fatalf("PushCompactionFrame: %v", err)
	}
	t.Logf("链① 装配层折叠: source=%q note=%q 模型调用累计=%d",
		assemblyReceipt.SummarySource, assemblyReceipt.SummaryNote, fixture.calls())

	// 链 ②：回合内控制器折叠（seelexctx/controller.go 的 after_assistant 软线触发）。
	before := fixture.calls()
	decision, err := fixture.runtime.seelexController().Handle(ctx, seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, History: rounds, Query: "当前输入",
	})
	if err != nil {
		t.Fatalf("controller.Handle: %v", err)
	}
	controllerCalls := fixture.calls() - before
	t.Logf("链② 回合内控制器折叠: replace=%v 本次模型调用=%d（累计 %d）", decision.ReplaceHistory, controllerCalls, fixture.calls())

	stack := fixture.store.Snapshot().CompactStack
	if len(stack) == 0 {
		t.Fatal("两条链都没有落帧：探针前提不成立")
	}
	top := stack[len(stack)-1]
	t.Logf("栈: %d 帧", len(stack))
	for index, frame := range stack {
		t.Logf("  frame[%d] id=%s source=%q evidence=%v", index, frame.SegmentID, frame.SummarySource, frame.Evidence)
	}

	// ── 重启（同一份存储上重新装配）──────────────────────────────────────
	fixture.runtime.Shutdown()
	_ = fixture.router.Close()
	restarted := restoreProbeBoot(t, root, true)
	record := restarted.store.Snapshot()
	block := modelVisibleCompactBlock(record)

	report := &strings.Builder{}
	fmt.Fprintf(report, "# 重启恢复探针现场\n\n## 重启前\n\n- 链① 装配层折叠: source=%q note=%q 模型调用累计=%d\n",
		assemblyReceipt.SummarySource, assemblyReceipt.SummaryNote, fixture.calls())
	fmt.Fprintf(report, "- 链② 回合内控制器折叠: replace=%v 本次模型调用=%d 累计=%d\n", decision.ReplaceHistory, controllerCalls, fixture.calls())
	fmt.Fprintf(report, "- 栈 %d 帧；栈顶 source=%q\n  evidence=%v\n", len(stack), top.SummarySource, top.Evidence)
	fmt.Fprintf(report, "\n## 重启后\n\n- 压缩栈 %d 帧\n", len(record.CompactStack))
	fmt.Fprintf(report, "\n## 重启后模型可见的压缩上下文块\n\n```json\n%s\n```\n", block)
	dumpProbeReport(t, report.String())
	t.Logf("重启后模型可见的压缩上下文块（节选）:\n%s", clipMiddle(block, 1600))
	t.Logf("模型可见面自检: 被折轮次助手正文=%v 装配层厚摘要正文=%v 80 字用户行=%v",
		strings.Contains(block, restoreProbeFoldedRootCause),
		strings.Contains(block, restoreProbeSummaryRootCause),
		strings.Contains(block, "第 0 轮问题"))

	// ── 判据（需求本身）────────────────────────────────────────────────
	// R3：帧链跨重启存活（探针保真度；红了说明后面两条判据无意义）。
	if len(record.CompactStack) != len(stack) {
		t.Fatalf("R3: 重启后压缩栈 %d 帧，重启前 %d 帧（帧链必须跨重启存活）", len(record.CompactStack), len(stack))
	}
	// R2：这一帧没有模型摘要，而它把原因写成"开关关闭或 QuickChat 装配失败"——
	// 开关是开的（上面那行配置日志），自答与事实不符。
	if strings.Contains(fmt.Sprint(top.Evidence), "开关关闭或 QuickChat 装配失败") {
		t.Errorf("R2: 帧把「没有模型摘要」自答成「开关关闭或 QuickChat 装配失败」，但本进程的开关是开的（enabled=%v）。回合内控制器链路本次模型调用=%d 次",
			fixture.runtime.limits.ContextCompactionSummary.Enabled, controllerCalls)
	}
	// R1：被折轮次的内容必须进模型可见面。
	if !strings.Contains(block, restoreProbeFoldedRootCause) {
		t.Errorf("R1: 被折的 163 个轮次的正文（助手侧含判据串 %q）一条都没进模型可见面——模型拿到的只是「溢出轮次: N 个完整协议单元」+ 每轮 80 字用户行",
			restoreProbeFoldedRootCause)
	}
}

// clipMiddle 把过长正文折成"头 + 尾"，让日志可读（探针现场已另有完整落盘）。
func clipMiddle(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	half := limit / 2
	return value[:half] + "\n…(中略)…\n" + value[len(value)-half:]
}
