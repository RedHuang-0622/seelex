package core

import (
	"testing"
)

// 红灯复现（用户现象）：**回看历史时发出的输入被吞**（看不见自己的消息，也看不见
// 这一轮的回复）。
//
// 用户形状（现场）：
//  1. 用户在会话里向上翻更早的历史（`LoadMoreHistory`：可见窗口锚定在更早位置，
//     `history_offset + 可见条数 < total_messages`）；
//  2. 用户直接在这个状态下发出下一条消息；
//  3. 输入框清空了、后端也收到了这一轮，但对话区里什么都没有——自己的消息与
//     整个回复都不出现，「界面刷新都不刷新一下」，只有等回合收尾落盘、或手动
//     「回到最新」之后才看得到。
//
// 根因（两条判据叠在一起）：
//   - 后端：`AppendMessageWithOriginLockedFor` 在窗口未贴尾时只把新行计进
//     `TotalMessages`，**不写进可见窗口**（这条规则本身是对的：写进去会在窗口
//     与尾之间留一道谁也不显示的断层）；
//   - 前端：`protocol.js` 的 reducer 在 `historyWindowed` 时同样**不把
//     message.added 落进列表**（同样是对的：避免断层），只提示「下方还有新内容」，
//     内容留给「回到最新」的基线刷新带回。
//
// 两条规则合起来的缺口是「**用户主动发起的回合**」：回看是用户的阅读手势，发言
// 是用户的参与手势——发言意味着「从现在起看最新」。当前实现把两者一律当成
// 「用户在读旧账，别抢他的阅读位置」，于是这一轮自己的输入落进了窗口之外，
// 只能等下一条消息 / 下一次「回到最新」。
//
// 本文件钉住：**用户行（新一轮对话）到达时，可见窗口必须回到尾部**，且不能
// 谎报「没有更早历史」（翻回去的入口必须还在）。

// TestReproSubmitWhileBrowsingKeepsUserRowInWindow 回看历史时提交一轮对话，
// 用户行与紧随其后的助手行必须出现在可见窗口里、窗口必须贴尾。
func TestReproSubmitWhileBrowsingKeepsUserRowInWindow(t *testing.T) {
	defer withHistoryWindow(6)()

	service, _ := openLongSession(t, 6, 20)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	assertRange(t, "回看更早一页", service.Snapshot(), 8, 6)

	// 提交路径同形：一轮对话先落用户行，再落一条空的助手行（正文随后流式写入）。
	service.ViewMu.Lock()
	service.appendMessageLocked("user", "刚发出去的这一句", nil)
	service.appendMessageLocked("assistant", "", nil)
	service.ViewMu.Unlock()

	snapshot := service.Snapshot()
	visible := visibleContents(snapshot)
	if !containsContent(visible, "刚发出去的这一句") {
		t.Fatalf("回看状态下提交的用户行不在可见窗口里（前端 reducer 会吞掉 message.added）：offset=%d visible=%v total=%d",
			snapshot.HistoryOffset, visible, snapshot.TotalMessages)
	}
	if !containsContent(visible, "") {
		t.Fatalf("这一轮的助手行也不在可见窗口里（回复将无处流式写入）：offset=%d visible=%v total=%d",
			snapshot.HistoryOffset, visible, snapshot.TotalMessages)
	}
	if snapshot.HistoryOffset+len(visible) != snapshot.TotalMessages {
		t.Fatalf("提交后窗口应贴尾（前端的 historyWindowed 判据）：offset=%d visible=%d total=%d",
			snapshot.HistoryOffset, len(visible), snapshot.TotalMessages)
	}
	if !snapshot.HasMoreHistory {
		t.Fatal("提交后仍应保留「还有更早历史」入口，用户要能翻回刚才读的位置")
	}
}

// TestReproSubmitWhileBrowsingEndsBrowsingState 提交之后会话不得再被判成
// 「回看中」——`SessionViewBrowsingHistoryLocked` 是流式增量、推理挂接、
// 工具结果写回三条路径的总闸门；它还判成回看，界面在整个回合里就是一动不动
// （「只有后续对话完成了才做出刷新」）。
func TestReproSubmitWhileBrowsingEndsBrowsingState(t *testing.T) {
	defer withHistoryWindow(6)()

	service, _ := openLongSession(t, 6, 20)
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}
	if !service.components.view.SessionViewBrowsingHistoryLocked("long-session") {
		t.Fatal("夹具前提：翻更早一页之后应处于回看态")
	}

	service.ViewMu.Lock()
	service.appendMessageLocked("user", "问一句", nil)
	service.appendMessageLocked("assistant", "", nil)
	service.ViewMu.Unlock()

	if service.components.view.SessionViewBrowsingHistoryLocked("long-session") {
		t.Fatal("用户已发言，会话不该再被当成「回看中」——流式增量与推理挂接都会被这条判据跳过")
	}
}

func containsContent(contents []string, want string) bool {
	for _, content := range contents {
		if content == want {
			return true
		}
	}
	return false
}
