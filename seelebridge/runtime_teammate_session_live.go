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
// 只读、有界：条数与每条长度都按 teammateSessionLive 的上限裁剪——它是给人看的一小段
// "此刻在说什么"，不是把整轮上下文倒进面板。

import (
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

const (
	// teammateSessionLiveMaxMessages 是实时读数保留的对话条数上限（最近若干条）。
	teammateSessionLiveMaxMessages = 40
	// teammateSessionLiveMessageLimit 是单条正文的字符上限（面板是"扫一眼"的地方）。
	teammateSessionLiveMessageLimit = 1200
)

// TeammateSessionLive 实现 contract.TeammateSessionProjection。
//
// 会话不在本进程里时返回 Running=false（**不是** nil）："没有这个会话"与"这个会话此刻
// 是空的"是两件事，调用方要分得清（前者该说"这一轮的执行面已不在本进程"，后者才是"没有
// 内容"）。
func (r *Runtime) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView {
	view := dto.TeammateSessionLiveView{SessionID: strings.TrimSpace(sessionID)}
	if r == nil || view.SessionID == "" {
		return view
	}
	state := r.roleTurnState()
	state.mu.Lock()
	handle := state.sessions[view.SessionID]
	state.mu.Unlock()
	if handle == nil || handle.engine == nil {
		return view
	}
	view.Running = true
	view.Role = handle.role
	view.Live = handle.inFlight()
	view.Messages, view.Truncated = teammateSessionLiveMessages(handle.engine.History())
	return view
}

// teammateSessionLiveMessages 把执行面的历史裁剪成有界的实时读数。
//
// 只留 user/assistant 两类：工具调用的原始载荷是执行细节，面板要的是"这件事在说什么"
// （与团队看板"只画事实、不画流水账"同一口径）。
func teammateSessionLiveMessages(history []types.Message) ([]dto.TeammateSessionLiveMessage, bool) {
	kept := make([]dto.TeammateSessionLiveMessage, 0, len(history))
	truncated := false
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
			truncated = true
		}
		kept = append(kept, dto.TeammateSessionLiveMessage{Role: role, Text: text})
	}
	if len(kept) > teammateSessionLiveMaxMessages {
		kept = kept[len(kept)-teammateSessionLiveMaxMessages:]
		truncated = true
	}
	if len(kept) == 0 {
		return nil, truncated
	}
	return kept, truncated
}
