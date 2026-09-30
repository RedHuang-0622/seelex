package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
)

// 2026-09-30 的决策只有两条，这个文件把它们钉住：
//
//	① 回合内控制器**任何阈值都不折对话**——折叠整条归装配层
//	   （application/core/context_runtime，下一轮开始前那次）；
//	② 循环内唯一的动作仍是超大工具结果的兜底归档（工具结果压缩不动，一条不删）。
//
// 为什么不是"循环里也折，只是把线挪到硬线"：折叠是上下文压缩流程里**产出元数据**
// 的一步，折完必须由模型按章节写出读后感，而控制器只拿得到**装配前**的引擎工作
// 历史（ev.History = ReActLoop.History()，system/项目/记忆/前缀栈/工具面都还没进去），
// 叫不动模型写读后感、也拿不到真实请求的字节前序。旧入口折出来的帧因此没有正文：
// 模型看不到被折内容、要 search_history 回读，而且请求前缀被改写，provider 的
// 前缀缓存整段作废（2026-09-30 重启恢复现场）。

// TestControllerLoopNeverFoldsConversation：喂远超任何阈值线的历史（放大计数器
// 是 len×100，10 轮 + 长查询），两个入口都必须什么都不做——不 ReplaceHistory、
// 不推压缩帧。改动前这条用例是红的（旧入口在软线就越线折叠）。
func TestControllerLoopNeverFoldsConversation(t *testing.T) {
	stacks := NewMemoryCompactStack()
	controller := newController(stacks)
	history := roundHistory(10)
	query := strings.Repeat("继续", 400)

	assertNoFold := func(label string, event seelectx.ContextEvent) {
		t.Helper()
		decision, err := controller.Handle(context.Background(), event)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if decision.ReplaceHistory || len(decision.History) != 0 {
			t.Fatalf("%s: 循环内不得折叠对话，decision=%+v", label, decision)
		}
	}
	assertNoFold("after_tool", seelectx.ContextEvent{
		Kind: seelectx.ContextAfterTool, Turn: 1, Query: query, History: history,
	})
	assertNoFold("after_assistant", seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: query, History: history,
	})
	if frames := stacks.Snapshot().CompactStack; len(frames) != 0 {
		t.Fatalf("循环内不得推压缩帧，实际 %d 条", len(frames))
	}
}

// TestControllerLoopArchivesOversizedToolResult：兜底归档仍在，且**不改历史**。
// 判定预算与 processor / 装配层同源（limits.max_tool_result_chars），未超大的
// 结果不动归档器。
func TestControllerLoopArchivesOversizedToolResult(t *testing.T) {
	archive := NewInMemoryToolResultArchiver()
	controller := &seelexContextController{opts: ControllerOptions{
		Archive:            archive,
		MaxToolResultChars: 64,
		Stacks:             NewMemoryCompactStack(),
	}}
	raw := strings.Repeat("x", 512)

	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterTool, Turn: 1,
		Tool:    &seelectx.ToolResult{CallID: "call-big", Name: "read_file", Raw: raw},
		History: roundHistory(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.ReplaceHistory {
		t.Fatal("兜底归档不得改写历史（折叠归装配层）")
	}
	got, ok := archive.Read("result:call-big")
	if !ok || got != raw {
		t.Fatalf("超大工具结果必须原样入库：ok=%v bytes=%d", ok, len(got))
	}

	if _, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterTool, Turn: 1,
		Tool:    &seelectx.ToolResult{CallID: "call-small", Name: "read_file", Raw: "tiny"},
		History: roundHistory(2),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := archive.Read("result:call-small"); ok {
		t.Fatal("未超大的工具结果不应入库")
	}
}
