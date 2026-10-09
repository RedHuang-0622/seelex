package seelebridge

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/stream"
)

// ── 账号路由事实（RoutingNotice）的 Runtime 装配面 ────────────────────────────
//
// 口径：观察者把路由事实写成 B 类摘要事件（durable，与 llm/tool 摘要同库）+
// 投进 CSP channel（application 呈现为会话 system 消息）；unpinned_selection
// 按 会话+账号 去重，failover 不去重。

func TestRoutingNoticeObserverPublishesFailoverToChannelAndSummary(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	channel := runtime.RoutingNoticeChannel()
	if channel == nil {
		t.Fatal("RoutingNoticeChannel 未装配")
	}
	notice := stream.RoutingNotice{
		SessionID:   "session-1",
		At:          time.Now(),
		Reason:      stream.ReasonFailover,
		From:        "agent-1",
		To:          "subagent-1",
		Model:       "child-model",
		Provider:    "openai",
		FailureKind: string(seelFailureKindQuota),
	}
	runtime.observeRoutingNotice(notice)

	select {
	case got := <-channel:
		if got.Reason != dto.RoutingReasonFailover || got.From != "agent-1" || got.To != "subagent-1" {
			t.Fatalf("channel 投影 = %+v", got)
		}
		if got.Message() == "" {
			t.Fatal("路由提示文案为空")
		}
	case <-time.After(time.Second):
		t.Fatal("failover 路由事实未进入 channel")
	}
	// B 类摘要：durable 记录一条 route 摘要，Detail 非空。
	summaries := runtime.summaryLog.Summaries()
	if len(summaries) == 0 {
		t.Fatal("路由事实未写入 B 类摘要日志")
	}
	last := summaries[len(summaries)-1]
	if last.Kind != "route" || last.Detail == "" {
		t.Fatalf("route 摘要 = %+v", last)
	}

	// failover 不去重：第二次仍然投递。
	runtime.observeRoutingNotice(notice)
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("failover 不得去重")
	}
}

func TestRoutingNoticeObserverDedupesUnpinnedSelectionPerSessionAndAccount(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	notice := stream.RoutingNotice{
		SessionID: "session-1",
		At:        time.Now(),
		Reason:    stream.ReasonUnpinnedSelection,
		To:        "subagent-1",
		Model:     "child-model",
		Provider:  "openai",
	}
	channel := runtime.RoutingNoticeChannel()
	runtime.observeRoutingNotice(notice)
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("首次 unpinned_selection 必须投递")
	}
	// 同会话同账号：去重。
	runtime.observeRoutingNotice(notice)
	select {
	case got := <-channel:
		t.Fatalf("unpinned_selection 被重复投递：%+v", got)
	case <-time.After(150 * time.Millisecond):
	}
	// 同会话不同账号：再投一次。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-1", At: time.Now(),
		Reason: stream.ReasonUnpinnedSelection, To: "goalplan-1",
		Model: "plan-model", Provider: "openai",
	})
	select {
	case got := <-channel:
		if got.To != "goalplan-1" {
			t.Fatalf("投影 = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("不同账号的 unpinned_selection 必须再投一次")
	}
	// 不同会话：再投一次。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-2", At: time.Now(),
		Reason: stream.ReasonUnpinnedSelection, To: "subagent-1",
		Model: "child-model", Provider: "openai",
	})
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("不同会话的 unpinned_selection 必须再投一次")
	}
}

// seelFailureKindQuota 复用 account.FailureQuota 的词表（同包测试的别名，避免
// 测试文件再 import 一个包——词表唯一来源仍是 seelebridge/account）。
const seelFailureKindQuota = "quota_exhausted"
