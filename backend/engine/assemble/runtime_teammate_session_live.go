package assemble

// runtime_teammate_session_live.go 是**当前 teammate 会话实时投影**的宿主转发面。
//
// 落点沿革（P21-12「域方法随类型走」）：投影与裁剪/分页判据搬进团队域
// [`backend/multiagent/teamwork`](../../multiagent/teamwork/README.md) 的 `teammate_live.go`；
// 这里只留**一格宿主读数**——角色回合执行面（`roleTurnState` 的角色会话槽）里的会话句柄
// （角色名 / 是否在飞 / 引擎历史），以函数值端口交给域包，域包因此不必认识 `Runtime` 与引擎槽。

import (
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 同值常量别名：槽位大小与单条上限的事实源在域包，既有调用点与用例零改动。
const (
	teammateSessionLiveMaxMessages  = teamwork.TeammateSessionLiveMaxMessages
	teammateSessionLiveMessageLimit = teamwork.TeammateSessionLiveMessageLimit
)

// TeammateSessionLive 实现 contract.TeammateSessionProjection：**默认页**（域包一行转发）。
func (r *Runtime) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView {
	return teamwork.TeammateSessionLive(sessionID, r.teammateLiveDeps())
}

// TeammateSessionLivePage 是同一份读数的**分页读法**（域包一行转发；判据在域包）。
func (r *Runtime) TeammateSessionLivePage(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	return teamwork.TeammateSessionLivePage(sessionID, offset, limit, r.teammateLiveDeps())
}

// teammateLiveDeps 把"当前角色回合执行面"接成域包要的一格读数（nil Runtime ⇒ 空端口，
// 域包据此如实回"执行面不在本进程"）。
func (r *Runtime) teammateLiveDeps() teamwork.TeammateLiveDeps {
	if r == nil {
		return teamwork.TeammateLiveDeps{}
	}
	return teamwork.TeammateLiveDeps{Session: r.teammateLiveSession}
}

// teammateLiveSession 读该会话的执行面句柄（没有 / 没有引擎 ⇒ ok = false）。
func (r *Runtime) teammateLiveSession(sessionID string) (teamwork.TeammateLiveSession, bool) {
	state := r.roleTurnState()
	state.mu.Lock()
	handle := state.sessions[sessionID]
	state.mu.Unlock()
	if handle == nil || handle.engine == nil {
		return teamwork.TeammateLiveSession{}, false
	}
	return teamwork.TeammateLiveSession{
		Role:    handle.role,
		Live:    handle.inFlight(),
		History: handle.engine.History(),
	}, true
}
