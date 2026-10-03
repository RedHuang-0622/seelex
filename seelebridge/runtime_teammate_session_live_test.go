package seelebridge

// runtime_teammate_session_live_test.go — 钉住「当前 teammate 会话」的实时读面
// （2026-10-04 用户口径：查看 teammate 的会话看到的"全是历史会话，不是当前这位的会话"）。
//
// 事实是：teammate 的一轮活跑在**这件事自己的会话号**上，而那是**进程内执行面**（刻意不接
// DurableHistory）。所以这个读面必须 ① 在读得到的时候给出这一轮的对话、② 在读不到的时候
// 如实说"执行面不在本进程"（不是假装"会话是空的"）。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestTeammateSessionLiveReadsCurrentRound(t *testing.T) {
	runtime, _, _ := newRoleTurnRuntime(t)
	ctx := context.Background()
	spec := runtime.employeeSpec("sess-main", "exec", "sess-main-team-exec-wi-wi-impl", "readwrite", "实现渲染件")
	if _, err := runtime.runRoleRound(ctx, spec); err != nil {
		t.Fatalf("runRoleRound: %v", err)
	}

	view := runtime.TeammateSessionLive("sess-main-team-exec-wi-wi-impl")
	if !view.Running {
		t.Fatal("刚跑完一轮的会话仍在执行面在册（本进程内）：Running 必须为真")
	}
	if view.Role != "exec" {
		t.Fatalf("实时读数必须带上「这是谁在干」：%+v", view)
	}
	if view.Live {
		t.Fatal("回合已经跑完：Live（此刻在飞）必须为假")
	}
	if len(view.Messages) == 0 {
		t.Fatalf("这一轮的对话必须读得出来（否则面板只能从会话库读到主会话历史）：%+v", view)
	}
	if !strings.Contains(view.Messages[0].Text, "实现渲染件") {
		t.Fatalf("第一行应为本轮的输入（这件事的工作正文）：%+v", view.Messages)
	}
}

func TestTeammateSessionLiveReportsMissingExecutionFace(t *testing.T) {
	runtime, _, _ := newRoleTurnRuntime(t)

	view := runtime.TeammateSessionLive("sess-not-in-this-process")
	if view.Running {
		t.Fatalf("不在本进程的会话不得被报成「在跑」：%+v", view)
	}
	if len(view.Messages) != 0 {
		t.Fatalf("不在本进程的会话没有正文可读：%+v", view)
	}
	if view.SessionID != "sess-not-in-this-process" {
		t.Fatalf("空的读数也要带上被问的会话号（调用方要能如实说明）：%+v", view)
	}
	// 空会话号同样是"没有这个执行面"，不是崩溃。
	if empty := runtime.TeammateSessionLive("  "); empty.Running || empty.SessionID != "" {
		t.Fatalf("空会话号应给出空视图：%+v", empty)
	}
}

// TestTeammateSessionLiveBoundsMessages：实时读数是有界的（面板是「扫一眼」的地方），
// 超长正文与超多条数都要被裁掉，且**如实标 Truncated**。
func TestTeammateSessionLiveBoundedMessages(t *testing.T) {
	long := strings.Repeat("很长的正文", teammateSessionLiveMessageLimit)
	history := make([]types.Message, 0, teammateSessionLiveMaxMessages+5)
	for index := 0; index < teammateSessionLiveMaxMessages+5; index++ {
		history = append(history, types.Message{Role: "user"}.WithText(long))
	}
	messages, truncated := teammateSessionLiveMessages(history)
	if !truncated {
		t.Fatal("裁掉了内容就必须标 Truncated（否则读的人以为这就是全部）")
	}
	if len(messages) != teammateSessionLiveMaxMessages {
		t.Fatalf("条数上限没生效：%d", len(messages))
	}
	for _, message := range messages {
		if len([]rune(message.Text)) > teammateSessionLiveMessageLimit+1 {
			t.Fatalf("单条长度上限没生效：%d", len([]rune(message.Text)))
		}
	}
}
