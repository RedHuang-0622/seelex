package seelebridge

// runtime_teammate_session_live.go — **当前 teammate 会话**的实时只读投影
// （契约 contract.TeammateSessionProjection，2026-10-04 用户口径）。
//
// 事实源：角色回合执行面（roleTurnState 的角色会话槽）。teammate 的一轮活跑在
// **这件事自己的会话号**上（一 Work Item 一套 Session），而这套会话是**进程内执行面**
// ——刻意不接 DurableHistory（见 Runtime.ResetSession 的注释）。所以"查看这件事的会话"
// 只能在这里读：会话库里没有它的正文，从存储读出来的只会是主会话的历史，那正是用户看到的
// "全是历史会话，不是当前的 teammate 的会话"。
//
// 只读、有界、**可分页**：引擎历史本来就在内存里，所以条数不再靠"尾部截断"丢内容——
// 默认页仍是"最近若干条"（面板是给人扫一眼的地方），翻页读法（TeammateSessionLivePage）
// 能顺着 offset/limit 把整段会话读完；单条长度上限 teammateSessionLiveMessageLimit 不变
// （那是"一条能有多长"的问题，与分页正交）。裁剪与分页判据**只有一份**
// （teammateSessionLiveSlice），默认页只是它的一个调用点。

import (
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// teammateSessionLiveMaxMessages 是**默认页**的对话条数（最近若干条），也是
	// limit<=0 时的页大小。
	teammateSessionLiveMaxMessages = 40
	// teammateSessionLiveMessageLimit 是单条正文的字符上限（面板是"扫一眼"的地方）。
	teammateSessionLiveMessageLimit = 1200
)

// TeammateSessionLive 实现 contract.TeammateSessionProjection：**默认页**。
//
// add but not modify：签名与语义不变（"此刻在说什么" = 最近若干条，最早的在前面），
// 但它退化成 TeammateSessionLivePage 同一份裁剪/分页判据的一层调用——不再有第二份
// "尾部截断"实现。
//
// 会话不在本进程里时返回 Running=false（**不是** nil）："没有这个会话"与"这个会话此刻
// 是空的"是两件事，调用方要分得清（前者该说"这一轮的执行面已不在本进程"，后者才是"没有
// 内容"）。
func (r *Runtime) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView {
	view, all, clipped := r.teammateSessionLiveWindow(sessionID)
	return teammateSessionLiveSlice(view, all, clipped, teammateSessionLiveDefaultOffset(all), teammateSessionLiveMaxMessages)
}

// TeammateSessionLivePage 是**当前 teammate 会话**的分页读法（有界窗口 + 分页）。
//
// 语义：offset 从该会话**可投影的第一条**算起（0 = 最旧）；limit<=0 → 默认
// teammateSessionLiveMaxMessages(40)；offset<0 → 0；offset>=total → 空页且 HasMore=false；
// Total = 可投影的 user/assistant 条数；HasMore = offset+len(messages) < Total。
//
// 只投影 user/assistant 两类（工具载荷是执行细节，不进这个面板）——与默认页同一口径。
func (r *Runtime) TeammateSessionLivePage(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	view, all, clipped := r.teammateSessionLiveWindow(sessionID)
	return teammateSessionLiveSlice(view, all, clipped, offset, limit)
}

// teammateSessionLiveWindow 读执行面并把历史投影成**全量**对话行（尚未分页）。
//
// 返回的第二值是全量行（口径见 teammateSessionLiveProject），第三值是"有单条正文被按
// 长度上限截断"。会话不在本进程时三个返回值都是零值/空——调用方据此如实说明。
func (r *Runtime) teammateSessionLiveWindow(sessionID string) (dto.TeammateSessionLiveView, []dto.TeammateSessionLiveMessage, bool) {
	view := dto.TeammateSessionLiveView{SessionID: strings.TrimSpace(sessionID)}
	if r == nil || view.SessionID == "" {
		return view, nil, false
	}
	state := r.roleTurnState()
	state.mu.Lock()
	handle := state.sessions[view.SessionID]
	state.mu.Unlock()
	if handle == nil || handle.engine == nil {
		return view, nil, false
	}
	view.Running = true
	view.Role = handle.role
	view.Live = handle.inFlight()
	all, clipped := teammateSessionLiveProject(handle.engine.History())
	return view, all, clipped
}

// teammateSessionLiveSlice 是**唯一**一份裁剪/分页判据：全量对话行 + (offset, limit) → 一页。
//
// 归一：offset<0 → 0；limit<=0 → teammateSessionLiveMaxMessages；offset>=total → 空页；
// HasMore = offset+len(page) < total；Truncated = "这一页之前还有内容" 或 "某条正文被按
// 长度上限截断"（两者都是"你看到的不是全部"，读的人必须看得出来）。
func teammateSessionLiveSlice(view dto.TeammateSessionLiveView, all []dto.TeammateSessionLiveMessage, clipped bool, offset, limit int) dto.TeammateSessionLiveView {
	total := len(all)
	if limit <= 0 {
		limit = teammateSessionLiveMaxMessages
	}
	if offset < 0 {
		offset = 0
	}
	view.Offset, view.Limit, view.Total = offset, limit, total
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		view.Messages = append([]dto.TeammateSessionLiveMessage(nil), all[offset:end]...)
	}
	view.HasMore = view.Offset+len(view.Messages) < total
	view.Truncated = offset > 0 || clipped
	return view
}

// teammateSessionLiveDefaultOffset 是默认页的起点：最近若干条（不足一页就从最旧开始）。
func teammateSessionLiveDefaultOffset(all []dto.TeammateSessionLiveMessage) int {
	if len(all) <= teammateSessionLiveMaxMessages {
		return 0
	}
	return len(all) - teammateSessionLiveMaxMessages
}

// teammateSessionLiveMessages 是**默认页**读数的历史入口（历史 → 最近若干条 + 是否截断）。
//
// 它只是 teammateSessionLiveProject + teammateSessionLiveSlice 的组合，判据**不分叉**；
// 独立成函数是为了让"裁剪是有界的、且如实标 Truncated"这件事有一条直接可钉的用例。
func teammateSessionLiveMessages(history []types.Message) ([]dto.TeammateSessionLiveMessage, bool) {
	all, clipped := teammateSessionLiveProject(history)
	view := teammateSessionLiveSlice(dto.TeammateSessionLiveView{}, all, clipped,
		teammateSessionLiveDefaultOffset(all), teammateSessionLiveMaxMessages)
	return view.Messages, view.Truncated
}

// teammateSessionLiveProject 把执行面的历史投影成**全量**对话行（未分页）。
//
// 只留 user/assistant 两类：工具调用的原始载荷是执行细节，面板要的是"这件事在说什么"
// （与团队看板"只画事实、不画流水账"同一口径）。第二值是"有单条正文被按长度上限截断"。
func teammateSessionLiveProject(history []types.Message) ([]dto.TeammateSessionLiveMessage, bool) {
	kept := make([]dto.TeammateSessionLiveMessage, 0, len(history))
	clipped := false
	for _, message := range history {
		role := strings.TrimSpace(message.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		text := strings.TrimSpace(message.Text())
		if text == "" {
			continue
		}
		if len([]rune(text)) > teammateSessionLiveMessageLimit {
			text = string([]rune(text)[:teammateSessionLiveMessageLimit]) + "…"
			clipped = true
		}
		kept = append(kept, dto.TeammateSessionLiveMessage{Role: role, Text: text})
	}
	if len(kept) == 0 {
		return nil, clipped
	}
	return kept, clipped
}
