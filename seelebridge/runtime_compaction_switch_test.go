package seelebridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 压缩处厚摘要开关（limits.context_compaction_summary）的两臂钉子。
//
// 审查报告 docs/2026-09-29-context-compaction-fold-review.md 的 P1-B 记的是"打开也不
// 生效"；推帧接线（application/core/context_runtime 的 pushCompactionFrame →
// seelebridge/runtime_compaction_index.go 的 PushCompactionFrame → MainCompactionDAG）
// 之后它变成"开就生效"，但**全仓没有一条测试构造 Enabled: true**：DAG 单测只用 fake
// Summarizer，application/core/context_compact_index_test.go 那三条用的是"返回固定回执"
// 的索引面桩。于是这条链
//
//	开关 → compactionSummarizer() 非 nil → 推帧产出的栈帧 summary_source=replay
//
// 此前既没有牙、也没人知道它断了——本文件补的就是这一跳。
//
// 判别力放在**两臂的差异**上，而不是任一臂的绝对值：
//   - 关：摘要器为 nil、completer 一次都没被调用、栈帧 summary_source=local；
//   - 开：摘要器非 nil、completer 恰好被调用一次，请求是"system 同源 + 历史字节原样 +
//     可见工具 + 固定指令尾巴"的前缀重放形态，回执与栈顶都是 summary_source=replay。
//
// 两臂都走**生产入口**（Runtime.PushCompactionFrame，即装配层压缩推帧那一跳），用真
// Runtime + 绑定到该会话的 SessionContextStore + 确定性 completer（无网络）：不拿桩替换
// 被测对象，也不第二次装配 completer（QuickChat 与 seelexCompressor 同一条构造路径）。

// compactionSwitchReply 是确定性 completer 给出的"厚摘要"回复：带小节骨架，因此
// 帧里出现它就等于"真摘要进了帧"，而不是"帧里只有任务台账的元数据投影"。
const compactionSwitchReply = "### 目标 (Goal)\n把压缩处的 Chapter 2 换成真摘要\n" +
	"### 错误与修复 (Errors and Fixes)\n开关关着时按 replay 断言必然失败\n" +
	"### 下一步 (Next Step)\n复跑 compactlive 三件套"

// compactionSwitchSystemPrompt 是会话 system prompt（前缀重放要求与真实请求同字节）。
const compactionSwitchSystemPrompt = "你是 Seelex，按证据工作的工程代理。"

type compactionSwitchFixture struct {
	runtime  *Runtime
	store    *sessionstore.SessionContextStore
	scripted *scriptedNodeCompleter
	session  string
}

// newCompactionSwitchFixture 装配两臂共用的生产接线。enabled 只动
// limits.ContextCompactionSummary.Enabled 一个字段，其余（input_tokens /
// chapter2_tokens 的零值语义）保持出厂。
func newCompactionSwitchFixture(t *testing.T, enabled bool) *compactionSwitchFixture {
	t.Helper()
	return newCompactionSwitchFixtureWith(t, enabled, newScriptedNodeCompleter(compactionSwitchReply))
}

// newCompactionSwitchFixtureWith 是上面那条接线的参数化版本：只换 completer。
// 失败臂要的就是"同一个生产入口 + 一个稳定失败的 completer"。
func newCompactionSwitchFixtureWith(t *testing.T, enabled bool, completer agent.Completer) *compactionSwitchFixture {
	t.Helper()
	runtime := newTestRuntime(t)
	t.Cleanup(runtime.Shutdown)

	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	runtime.AttachHistoryRouter(router)

	sessionID := "session-compaction-switch"
	if _, err := runtime.NewMainSessionWithID(sessionID, nil); err != nil {
		t.Fatalf("NewMainSessionWithID: %v", err)
	}
	store := sessionstore.NewSessionContextStore(router, sessionID)
	if err := store.SetSystemPrompt(compactionSwitchSystemPrompt); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	runtime.AttachSessionContextStore(store)

	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent": completer})
	// 注册内联工具面：重放请求的 Tools 取自 VisibleTools（与真实请求同 schema），没有
	// 工具面就只剩一个"0 == 0"的空断言，钉不住"工具被原样带上"。
	runtime.RegisterBuiltins()

	runtime.limits.ContextCompactionSummary.Enabled = enabled
	scriptedCompleter, _ := completer.(*scriptedNodeCompleter)
	return &compactionSwitchFixture{runtime: runtime, store: store, scripted: scriptedCompleter, session: sessionID}
}

// push 走生产推帧入口：溢出区原文 + "上一次真实请求"的历史字节。
func (fixture *compactionSwitchFixture) push(t *testing.T) CompactionFrameReceipt {
	t.Helper()
	receipt, err := fixture.runtime.PushCompactionFrame(context.Background(), fixture.session, CompactionFrameRequest{
		Overflow:      compactionSwitchOverflow(),
		ReplayHistory: compactionSwitchReplayHistory(),
	})
	if err != nil {
		t.Fatalf("PushCompactionFrame: %v", err)
	}
	return receipt
}

// stackTop 返回推帧后的栈顶帧（推帧没落栈直接失败：摘要来源只有进了栈才算数）。
func (fixture *compactionSwitchFixture) stackTop(t *testing.T) sessionstore.CompactFrame {
	t.Helper()
	stack := fixture.store.Snapshot().CompactStack
	if len(stack) != 1 {
		t.Fatalf("推帧后压缩栈应有 1 帧，实际 %d 帧", len(stack))
	}
	return stack[0]
}

// TestCompactionSummarySwitchClosedKeepsLocalCompact：关臂 = "关就是关"的全部含义——
// 摘要器为 nil（chapter2Node 的显式判据 `d.opts.Summarizer != nil`）、付费调用一次都不
// 发、栈帧 summary_source=local；不报错，也不静默降级成"发一次没有缓存的调用"。
func TestCompactionSummarySwitchClosedKeepsLocalCompact(t *testing.T) {
	fixture := newCompactionSwitchFixture(t, false)
	if summarizer := fixture.runtime.compactionSummarizer(); summarizer != nil {
		t.Fatalf("开关关闭时摘要器应为 nil，实际 %T", summarizer)
	}

	receipt := fixture.push(t)
	if receipt.SummarySource != seelexctx.CompactSummarySourceLocal {
		t.Fatalf("关臂 summary_source = %q，want %q", receipt.SummarySource, seelexctx.CompactSummarySourceLocal)
	}
	if strings.Contains(receipt.Summary, "把压缩处的 Chapter 2 换成真摘要") {
		t.Fatalf("关臂不该把模型回复写进帧：\n%s", receipt.Summary)
	}
	requests, tools := fixture.scripted.recorded()
	if len(requests) != 0 || len(tools) != 0 {
		t.Fatalf("关臂一次模型调用都不该发，实际 %d 次请求", len(requests))
	}
	if top := fixture.stackTop(t); top.SummarySource != seelexctx.CompactSummarySourceLocal {
		t.Fatalf("关臂栈顶 summary_source = %q，want local", top.SummarySource)
	}
	// 关臂也要能自答"模型为什么没被叫到"：note 说明是开关关闭，而不是笼统一句
	// "可能没开、也可能失败"（local 的三种来路必须分得开）。
	if !strings.Contains(receipt.SummaryNote, "开关关闭") {
		t.Fatalf("关臂 summary_note 应说明开关关闭，实际 %q", receipt.SummaryNote)
	}
	if !hasLocalCompactEvidence(fixture.stackTop(t), "no-summarizer") {
		t.Fatalf("关臂栈帧应留 compact-local:no-summarizer 证据，实际 %+v", fixture.stackTop(t).Evidence)
	}
}

// 失败臂复用同包既有的 failingCompleter（fork_smoke_test.go）：它只做一件事——
// 让每一次模型调用稳定报错。用例钉的是"重放失败时帧里必须写出真实报错"，
// 不是网络行为。

func hasLocalCompactEvidence(frame sessionstore.CompactFrame, code string) bool {
	for _, evidence := range frame.Evidence {
		if evidence.Ref == seelexctx.LocalCompactEvidenceRefPrefix+code {
			return true
		}
	}
	return false
}

// TestCompactionSummarySwitchOpenReplayFailureWritesReason：开臂 + 重放调用失败——
// 帧必须写出"这次为什么没有模型摘要"（含真实报错），而不是只留一个
// summary_source=local 让人猜。
//
// 这条用例来自一次现场：新进程（已确认读到 enabled: true）折出的帧仍是 local，而
// 它的回执只有 `index 458ms` 与 `summary_source=local`——"没调用"与"调用失败"读不出
// 来，失败原因被 chapter2Node 吞掉。静默降级本身就是缺陷：两条来路的处置完全不同
// （一个是配置，一个是故障）。
func TestCompactionSummarySwitchOpenReplayFailureWritesReason(t *testing.T) {
	const failure = "account lease refused: rate limited"
	fixture := newCompactionSwitchFixtureWith(t, true, &failingCompleter{err: errors.New(failure)})
	if summarizer := fixture.runtime.compactionSummarizer(); summarizer == nil {
		t.Fatal("开关打开时摘要器不该为 nil（失败臂要钉的是调用失败，不是没装配）")
	}

	receipt := fixture.push(t)
	if receipt.SummarySource != seelexctx.CompactSummarySourceLocal {
		t.Fatalf("重放失败后应回退本地压缩，summary_source = %q", receipt.SummarySource)
	}
	for _, want := range []string{"前缀重放两次调用均失败", failure} {
		if !strings.Contains(receipt.SummaryNote, want) {
			t.Fatalf("回执 summary_note 应含 %q，实际 %q", want, receipt.SummaryNote)
		}
	}
	top := fixture.stackTop(t)
	if !hasLocalCompactEvidence(top, "replay-failed") {
		t.Fatalf("栈帧应留 compact-local:replay-failed 证据，实际 %+v", top.Evidence)
	}
	if !strings.Contains(top.Evidence[len(top.Evidence)-1].Summary, failure) {
		t.Fatalf("证据正文应带真实报错，实际 %q", top.Evidence[len(top.Evidence)-1].Summary)
	}
}

// TestCompactionSummarySwitchOpenReplaysPrefixIntoStackFrame：开臂——摘要器非 nil、
// 恰好一次付费调用、回执与栈顶都是 summary_source=replay，且帧正文原样嵌入该摘要。
//
// 请求形态一并钉住，因为"打开"的收益全押在前缀缓存上：system 与真实请求同源、历史字节
// 原样在前、可见工具一并带上、固定压缩指令是唯一新增的尾巴。任何一处被重拼或改写，
// 这次调用就从"几乎全命中缓存"变成"付全价却换不来前缀"。
func TestCompactionSummarySwitchOpenReplaysPrefixIntoStackFrame(t *testing.T) {
	fixture := newCompactionSwitchFixture(t, true)
	if summarizer := fixture.runtime.compactionSummarizer(); summarizer == nil {
		t.Fatal("开关打开时摘要器不该为 nil（否则打开也不生效）")
	}

	receipt := fixture.push(t)
	if receipt.SummarySource != seelexctx.CompactSummarySourceReplay {
		t.Fatalf("开臂 summary_source = %q，want replay", receipt.SummarySource)
	}
	if !strings.Contains(receipt.Summary, "把压缩处的 Chapter 2 换成真摘要") {
		t.Fatalf("开臂帧正文应原样嵌入模型厚摘要：\n%s", receipt.Summary)
	}
	if top := fixture.stackTop(t); top.SummarySource != seelexctx.CompactSummarySourceReplay {
		t.Fatalf("开臂栈顶 summary_source = %q，want replay", top.SummarySource)
	}

	requests, tools := fixture.scripted.recorded()
	if len(requests) != 1 {
		t.Fatalf("开臂每次压缩恰好一次付费调用，实际 %d 次", len(requests))
	}
	request := requests[0]
	history := compactionSwitchReplayHistory()
	if len(request) != len(history)+2 {
		t.Fatalf("重放请求消息数 = %d，want system 1 + 历史 %d + 指令 1", len(request), len(history))
	}
	head := request[0]
	if head.Role != "system" || head.Content == nil || *head.Content != compactionSwitchSystemPrompt {
		t.Fatalf("重放请求首条应是会话 system 原字节：%+v", head)
	}
	for index, want := range history {
		got := request[index+1]
		if got.Role != want.Role || got.Content == nil || want.Content == nil || *got.Content != *want.Content {
			t.Fatalf("重放请求第 %d 条不等于真实请求历史字节：got %+v want %+v", index+1, got, want)
		}
	}
	tail := request[len(request)-1]
	if tail.Role != "user" || tail.Content == nil || *tail.Content != seelexctx.PrefixReplayInstruction {
		t.Fatalf("重放请求末尾应是固定压缩指令（唯一新增内容）：%+v", tail)
	}
	if len(tools) != 1 || len(tools[0]) == 0 {
		t.Fatalf("重放请求应带上可见工具（与真实请求同 schema），实际 %d 次记录、工具 %v", len(tools), tools)
	}
	if want := fixture.runtime.agt.VisibleTools(context.Background()); len(tools[0]) != len(want) {
		t.Fatalf("重放请求工具数 = %d，want 可见工具数 %d（工具面不同 = 前缀不同字节）", len(tools[0]), len(want))
	} else {
		for index := range want {
			if tools[0][index].Function.Name != want[index].Function.Name {
				t.Fatalf("重放请求工具面第 %d 个 = %q，want %q（工具面不同 = 前缀不同字节）",
					index, tools[0][index].Function.Name, want[index].Function.Name)
			}
		}
	}
}

// TestShippedCompactionSummarySwitchShipsOpen：出厂配置"打开"这一事实本身也要有牙。
// 字段语义没变（代码零值仍是关，见上面第一条用例），变的是 config/seelex.yaml 这一行；
// 没有这条用例，一次静默回滚（改回 false 或把整块注释回去）不会有任何红。
func TestShippedCompactionSummarySwitchShipsOpen(t *testing.T) {
	path := filepath.Join("..", "config", "seelex.yaml")
	limits, err := seelexctx.LoadLimits(path)
	if err != nil {
		t.Fatalf("LoadLimits(%s): %v", path, err)
	}
	if !limits.ContextCompactionSummary.Enabled {
		t.Fatalf("出厂配置应打开压缩处厚摘要（%s 的 limits.context_compaction_summary.enabled）", path)
	}
}

// compactionSwitchOverflow 是被折出保留窗口的协议单元原文（user + assistant 一个完整
// 单元）。体积刻意很小：这条用例钉的是开关，不是分片重放链（溢出区超过片预算才会分片，
// 那需要另一条带牙用例）。
func compactionSwitchOverflow() []types.Message {
	question := "把保留窗口外的轮次折进 checkpoint 帧。"
	answer := strings.Repeat("A", 4096)
	return []types.Message{
		{Role: "user", Content: &question},
		{Role: "assistant", Content: &answer},
	}
}

// compactionSwitchReplayHistory 是"上一次真实请求"的历史字节：前缀重放要求它与产出该
// 请求的装配路径同源；这里只需要可辨识——逐条原样比对足以发现重拼或改写。
func compactionSwitchReplayHistory() []types.Message {
	question := "上一轮：先读 seelebridge/runtime_context.go 的 compactionSummarizer。"
	answer := "已读：开关关闭返回 nil，打开则构造 QuickChat 前缀重放摘要器。"
	return []types.Message{
		{Role: "user", Content: &question},
		{Role: "assistant", Content: &answer},
	}
}

// recorded 返回确定性 completer 收到的全部请求（消息与可见工具的成对快照）。
// scriptedNodeCompleter 的字段由它自己的 mu 保护；这里加读侧访问器，免得用例直接摸锁。
func (c *scriptedNodeCompleter) recorded() ([][]types.Message, [][]types.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	messages := make([][]types.Message, len(c.requests))
	for index := range c.requests {
		messages[index] = cloneMessages(c.requests[index])
	}
	tools := make([][]types.Tool, len(c.seenTools))
	for index := range c.seenTools {
		tools[index] = cloneTools(c.seenTools[index])
	}
	return messages, tools
}
