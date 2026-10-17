package teamwork

// teammate_live_test.go 是「当前 teammate 会话实时投影」的**域级**用例（P21-12：投影与
// 裁剪/分页判据随实现搬进团队域；宿主那条链另在 `backend/engine/assemble` 的端到端用例里跑）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func teammateLiveHistory(count int) []types.Message {
	history := make([]types.Message, 0, count)
	for index := 0; index < count; index++ {
		history = append(history, teammateLiveMessage("user", "第 "+string(rune('a'+index%26))+" 条"))
	}
	return history
}

// teammateLiveMessage 造一条带正文的消息（`Content` 是 `*string`，用 `WithText` 统一构造）。
func teammateLiveMessage(role, text string) types.Message {
	return types.Message{Role: role}.WithText(text)
}

// TestTeammateSessionLiveBoundsMessages：实时读数是有界的（面板是「扫一眼」的地方），
// 超长正文与超多条数都要被裁掉，且**如实标 Truncated**（随实现从装配层搬来的原用例）。
func TestTeammateSessionLiveBoundsMessages(t *testing.T) {
	long := strings.Repeat("很长的正文", TeammateSessionLiveMessageLimit)
	history := make([]types.Message, 0, TeammateSessionLiveMaxMessages+5)
	for index := 0; index < TeammateSessionLiveMaxMessages+5; index++ {
		history = append(history, teammateLiveMessage("user", long))
	}
	messages, truncated := TeammateSessionLiveMessages(history)
	if !truncated {
		t.Fatal("裁掉了内容就必须标 Truncated（否则读的人以为这就是全部）")
	}
	if len(messages) != TeammateSessionLiveMaxMessages {
		t.Fatalf("条数上限没生效：%d", len(messages))
	}
	for _, message := range messages {
		if len([]rune(message.Text)) > TeammateSessionLiveMessageLimit+1 {
			t.Fatalf("单条长度上限没生效：%d", len([]rune(message.Text)))
		}
	}
}

// TestTeammateSessionLiveProjectKeepsOnlyDialogue：只投影 user/assistant 且跳过空正文
// （工具载荷是执行细节，不进这个面板）。
func TestTeammateSessionLiveProjectKeepsOnlyDialogue(t *testing.T) {
	history := []types.Message{
		teammateLiveMessage("system", "系统提示词不进面板"),
		teammateLiveMessage("user", "  这件事要做什么  "),
		teammateLiveMessage("tool", "工具载荷不进面板"),
		teammateLiveMessage("assistant", ""),
		teammateLiveMessage("assistant", "我去实现"),
	}
	messages, clipped := TeammateSessionLiveMessages(history)
	if clipped {
		t.Fatal("没有超长正文时不该标截断")
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("投影结果 = %+v, want 两条（user/assistant）", messages)
	}
	if messages[0].Text != "这件事要做什么" {
		t.Fatalf("正文应去掉首尾空白：%q", messages[0].Text)
	}
}

// TestTeammateSessionLivePageNormalizesWindow：分页归一（offset<0 → 0；limit<=0 → 默认页大小；
// offset>=total → 空页且 HasMore=false），并如实报 Total/HasMore/Truncated。
func TestTeammateSessionLivePageNormalizesWindow(t *testing.T) {
	history := teammateLiveHistory(TeammateSessionLiveMaxMessages*2 + 3)
	deps := TeammateLiveDeps{Session: func(string) (TeammateLiveSession, bool) {
		return TeammateLiveSession{Role: "exec", Live: true, History: history}, true
	}}
	total := TeammateSessionLiveMaxMessages*2 + 3

	first := TeammateSessionLivePage("s-1", 0, TeammateSessionLiveMaxMessages, deps)
	if first.Offset != 0 || first.Limit != TeammateSessionLiveMaxMessages || first.Total != total {
		t.Fatalf("首页窗口 = %d/%d/%d, want 0/%d/%d", first.Offset, first.Limit, first.Total, TeammateSessionLiveMaxMessages, total)
	}
	if len(first.Messages) != TeammateSessionLiveMaxMessages || !first.HasMore || first.Truncated {
		t.Fatalf("首页 = %d 条 / HasMore=%v / Truncated=%v", len(first.Messages), first.HasMore, first.Truncated)
	}

	// 负 offset 归一为 0；limit<=0 归一为默认页大小。
	normalized := TeammateSessionLivePage("s-1", -5, -1, deps)
	if normalized.Offset != 0 || normalized.Limit != TeammateSessionLiveMaxMessages {
		t.Fatalf("负值归一 = %d/%d", normalized.Offset, normalized.Limit)
	}

	// 越界页：空页 + HasMore=false，但窗口读数照报（"还有多少"必须让人看得见）。
	beyond := TeammateSessionLivePage("s-1", total, TeammateSessionLiveMaxMessages, deps)
	if len(beyond.Messages) != 0 || beyond.HasMore || beyond.Total != total || !beyond.Truncated {
		t.Fatalf("越界页 = %d 条 / HasMore=%v / Total=%d / Truncated=%v", len(beyond.Messages), beyond.HasMore, beyond.Total, beyond.Truncated)
	}
}

// TestTeammateSessionLiveDefaultPageIsTail：默认页是**最近若干条**（不足一页从最旧开始），
// 且与分页读法共用同一份判据（默认页 = 起点取尾部的那个调用点）。
func TestTeammateSessionLiveDefaultPageIsTail(t *testing.T) {
	history := teammateLiveHistory(TeammateSessionLiveMaxMessages + 5)
	deps := TeammateLiveDeps{Session: func(string) (TeammateLiveSession, bool) {
		return TeammateLiveSession{Role: "exec", History: history}, true
	}}
	view := TeammateSessionLive("s-1", deps)
	if view.Total != TeammateSessionLiveMaxMessages+5 {
		t.Fatalf("Total = %d", view.Total)
	}
	if view.Offset != 5 || len(view.Messages) != TeammateSessionLiveMaxMessages {
		t.Fatalf("默认页 = offset %d / %d 条", view.Offset, len(view.Messages))
	}
	if !view.Truncated {
		t.Fatal("默认页之前还有内容 ⇒ 必须标 Truncated")
	}
	// 不足一页时从最旧开始。
	short := TeammateSessionLive("s-1", TeammateLiveDeps{Session: func(string) (TeammateLiveSession, bool) {
		return TeammateLiveSession{Role: "exec", History: teammateLiveHistory(3)}, true
	}})
	if short.Offset != 0 || len(short.Messages) != 3 || short.HasMore || short.Truncated {
		t.Fatalf("短会话默认页 = offset %d / %d 条 / HasMore=%v / Truncated=%v", short.Offset, len(short.Messages), short.HasMore, short.Truncated)
	}
}

// TestTeammateSessionLiveReportsMissingExecutionFace：会话不在本进程（端口回 false、端口为 nil）
// ⇒ Running=false 且**带上被问的会话号**；空会话号 ⇒ 空视图。两者都不得 panic。
func TestTeammateSessionLiveReportsMissingExecutionFace(t *testing.T) {
	cases := map[string]TeammateLiveDeps{
		"端口未装配":   {},
		"会话不在本进程": {Session: func(string) (TeammateLiveSession, bool) { return TeammateLiveSession{}, false }},
	}
	for name, deps := range cases {
		view := TeammateSessionLive("sess-not-in-this-process", deps)
		if view.Running || len(view.Messages) != 0 {
			t.Fatalf("%s：不得报成在跑或给出正文：%+v", name, view)
		}
		if view.SessionID != "sess-not-in-this-process" {
			t.Fatalf("%s：空的读数也要带上被问的会话号：%+v", name, view)
		}
	}
	if empty := TeammateSessionLive("  ", TeammateLiveDeps{}); empty.Running || empty.SessionID != "" {
		t.Fatalf("空会话号应给出空视图：%+v", empty)
	}
}

// TestTeammateSessionLiveCarriesRoleAndLive：读数带上"这是谁在干"与"此刻在飞"（面板的两格状态）。
func TestTeammateSessionLiveCarriesRoleAndLive(t *testing.T) {
	deps := TeammateLiveDeps{Session: func(string) (TeammateLiveSession, bool) {
		return TeammateLiveSession{Role: "exec", Live: true, History: teammateLiveHistory(2)}, true
	}}
	view := TeammateSessionLive("s-1", deps)
	if !view.Running || view.Role != "exec" || !view.Live {
		t.Fatalf("读数 = %+v, want running/exec/live", view)
	}
}
