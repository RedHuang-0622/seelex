package seelebridge

// runtime_teammate_session_live_page_test.go — 「这件事的会话」的**分页读法**
// （Runtime.TeammateSessionLivePage / dto.TeammateSessionLiveView 的 offset/limit/total/has_more）。
//
// 为什么必须分页：teammate 的一轮活跑在**进程内执行面**上（正文不落盘），而引擎历史本来就
// 在内存里——所以"只给最近 40 条、更早的直接丢掉"是可避免的丢内容。默认页仍是"最近若干条"
// （面板是给人扫一眼的地方），翻页读法把整段会话读完。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// TestTeammateSessionLivePageCoversSessionByPages 多轮之后逐页读：
// 首页 / 中页 / 尾页 / 越界页各一条断言，且"逐页合计 = total"。
func TestTeammateSessionLivePageCoversSessionByPages(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	const sessionID = "sess-main-team-exec-wi-wi-paged"
	spec := runtime.employeeSpec("sess-main", "exec", sessionID, "readwrite", "这一轮的工作正文")

	// 多轮历史：先往引擎里塞满（History 读的就是它——teammate 会话只在内存里）。
	rounds := teammateSessionLiveMaxMessages*2 + 3
	for index := 0; index < rounds; index++ {
		engine.inputs = append(engine.inputs, fmt.Sprintf("第 %d 轮的输入", index))
	}
	if _, err := runtime.runRoleRound(context.Background(), spec); err != nil {
		t.Fatalf("runRoleRound: %v", err)
	}
	total := rounds + 1 // 历史 + 本轮的输入

	// 首页。
	first := runtime.TeammateSessionLivePage(sessionID, 0, teammateSessionLiveMaxMessages)
	if !first.Running || first.Role != "exec" {
		t.Fatalf("执行面在册时 Running/Role 必须如实：%+v", first)
	}
	if first.Offset != 0 || first.Limit != teammateSessionLiveMaxMessages || first.Total != total {
		t.Fatalf("首页读数 = offset %d / limit %d / total %d, want 0/%d/%d",
			first.Offset, first.Limit, first.Total, teammateSessionLiveMaxMessages, total)
	}
	if len(first.Messages) != teammateSessionLiveMaxMessages || !first.HasMore {
		t.Fatalf("首页 = %d 条 / has_more=%v, want %d/true",
			len(first.Messages), first.HasMore, teammateSessionLiveMaxMessages)
	}
	if first.Messages[0].Text != "第 0 轮的输入" {
		t.Fatalf("首页应从最旧一条开始：%+v", first.Messages[0])
	}
	if first.Truncated {
		t.Fatalf("首页前面没有内容，不许标 Truncated：%+v", first)
	}

	// 中页。
	middle := runtime.TeammateSessionLivePage(sessionID, teammateSessionLiveMaxMessages, teammateSessionLiveMaxMessages)
	if middle.Offset != teammateSessionLiveMaxMessages || len(middle.Messages) != teammateSessionLiveMaxMessages || !middle.HasMore {
		t.Fatalf("中页 = offset %d / %d 条 / has_more=%v", middle.Offset, len(middle.Messages), middle.HasMore)
	}
	if middle.Messages[0].Text != fmt.Sprintf("第 %d 轮的输入", teammateSessionLiveMaxMessages) {
		t.Fatalf("中页起点不对：%+v", middle.Messages[0])
	}
	if !middle.Truncated {
		t.Fatal("中页前面还有内容 → 必须标 Truncated（读的人要看得出这不是全部）")
	}

	// 尾页（按页对齐的边界）：offset = 2 页大小 —— 只剩尾部的几条 + 本轮输入，后面没有更多。
	tailOffset := teammateSessionLiveMaxMessages * 2
	tail := runtime.TeammateSessionLivePage(sessionID, tailOffset, teammateSessionLiveMaxMessages)
	if tail.Offset != tailOffset || tail.HasMore {
		t.Fatalf("尾页 = offset %d / has_more=%v, want %d/false", tail.Offset, tail.HasMore, tailOffset)
	}
	if len(tail.Messages) != total-tailOffset {
		t.Fatalf("尾页 = %d 条, want %d", len(tail.Messages), total-tailOffset)
	}
	if last := tail.Messages[len(tail.Messages)-1]; !strings.Contains(last.Text, "这一轮的工作正文") {
		// 引擎拿到的是**装配后的回合提示**（正文被包进 <round_input>/<task>），不是裸正文
		// ——所以这一条按"含本轮正文"钉，而不是按"等于正文"钉（红原文见首跑读数）。
		t.Fatalf("尾页最后一条应是本轮的输入：%+v", last)
	}

	// 最后一页（对齐到 total）：默认页读的就是这一页——两者必须同一页（同一份判据的两个调用点）。
	lastPageOffset := total - teammateSessionLiveMaxMessages
	lastPage := runtime.TeammateSessionLivePage(sessionID, lastPageOffset, teammateSessionLiveMaxMessages)
	if lastPage.Offset != lastPageOffset || len(lastPage.Messages) != teammateSessionLiveMaxMessages || lastPage.HasMore {
		t.Fatalf("最后一页 = offset %d / %d 条 / has_more=%v, want %d/%d/false",
			lastPage.Offset, len(lastPage.Messages), lastPage.HasMore,
			lastPageOffset, teammateSessionLiveMaxMessages)
	}

	// 越界页：空页且 has_more=false。
	beyond := runtime.TeammateSessionLivePage(sessionID, total, teammateSessionLiveMaxMessages)
	if beyond.Offset != total || len(beyond.Messages) != 0 || beyond.HasMore {
		t.Fatalf("越界页 = offset %d / %d 条 / has_more=%v, want %d/0/false",
			beyond.Offset, len(beyond.Messages), beyond.HasMore, total)
	}
	if beyond.Total != total {
		t.Fatalf("越界页也要如实报 total：%d, want %d", beyond.Total, total)
	}

	// 归一：offset<0 → 0；limit<=0 → 默认页大小。
	if negative := runtime.TeammateSessionLivePage(sessionID, -3, 5); negative.Offset != 0 || len(negative.Messages) != 5 {
		t.Fatalf("offset<0 未归一：%+v", negative)
	}
	if zero := runtime.TeammateSessionLivePage(sessionID, 0, 0); zero.Limit != teammateSessionLiveMaxMessages {
		t.Fatalf("limit<=0 → %d, want %d", zero.Limit, teammateSessionLiveMaxMessages)
	}

	// 逐页读：合计 = total，不丢条、不重复。
	seen := make(map[string]bool, total)
	paged := 0
	for offset := 0; ; {
		page := runtime.TeammateSessionLivePage(sessionID, offset, 7)
		paged += len(page.Messages)
		for _, message := range page.Messages {
			if seen[message.Text] {
				t.Fatalf("分页重复投递：%q 出现两次", message.Text)
			}
			seen[message.Text] = true
		}
		if !page.HasMore {
			break
		}
		offset = page.Offset + len(page.Messages)
	}
	if paged != total || len(seen) != total {
		t.Fatalf("逐页合计 = %d（去重 %d）, want %d", paged, len(seen), total)
	}

	// 默认页（TeammateSessionLive）退化成"最近若干条" = 最后一页：位置/判据与分页读法同一份。
	legacy := runtime.TeammateSessionLive(sessionID)
	if legacy.Offset != total-teammateSessionLiveMaxMessages || len(legacy.Messages) != teammateSessionLiveMaxMessages {
		t.Fatalf("默认页 = offset %d / %d 条, want %d/%d",
			legacy.Offset, len(legacy.Messages), total-teammateSessionLiveMaxMessages, teammateSessionLiveMaxMessages)
	}
	if legacy.Offset != lastPage.Offset || len(legacy.Messages) != len(lastPage.Messages) {
		t.Fatalf("默认页必须与「最后一页」同一页：%+v vs %+v", legacy, lastPage)
	}
	if !legacy.Truncated || legacy.HasMore {
		t.Fatalf("默认页被裁掉了更早内容 → Truncated 必须为真、且后面没有更多：%+v", legacy)
	}

	// 会话不在本进程：如实说，不假装"会话是空的"。
	missing := runtime.TeammateSessionLivePage("sess-not-in-this-process", 0, 10)
	if missing.Running || missing.Total != 0 || len(missing.Messages) != 0 || missing.HasMore {
		t.Fatalf("不在本进程的会话必须给 Running=false 的空页：%+v", missing)
	}
	if missing.SessionID != "sess-not-in-this-process" {
		t.Fatalf("空的读数也要带上被问的会话号：%+v", missing)
	}
}

// TestTeammateSessionLiveProjectFiltersToolPayloads 钉住投影口径（分页前后同一份）：
// 只留 user/assistant 两类、按历史顺序；工具载荷不进这个面板。
func TestTeammateSessionLiveProjectFiltersToolPayloads(t *testing.T) {
	// 复合字面量后面直接跟方法调用，在**元素表**里必须括起来（`{Role:"user"}.WithText` 不是
	// 合法元素 —— 省略类型的字面量不能接选择子）。
	all, clipped := teammateSessionLiveProject([]types.Message{
		(types.Message{Role: "user"}).WithText("把契约落成代码"),
		(types.Message{Role: "tool"}).WithText("（工具载荷：执行细节，不进面板）"),
		(types.Message{Role: "assistant"}).WithText("改好了"),
		(types.Message{Role: "system"}).WithText("（系统提示：同样不进）"),
	})
	if clipped {
		t.Fatal("没有超长正文，不该标 clipped")
	}
	if len(all) != 2 || all[0].Role != "user" || all[1].Role != "assistant" {
		t.Fatalf("投影必须只留 user/assistant 且保持顺序：%+v", all)
	}
}
