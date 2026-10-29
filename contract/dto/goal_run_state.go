package dto

// goal_run_state.go — "这段会话有没有在做一件已登记的目标" + "轮边界要不要自动续"
// 的**两层状态机**词表与**唯一判定点**。
//
// 规格：docs/arch/goal-auto-continue.md §2.8.0。这里只放纯数据与纯函数：
//   - 层一 GoalRunStatus：none / doing / done —— 由 goal 栈现读（Begin/Finish/Abort 写）；
//   - 层二 GoalRunSubstate：continue / paused —— 由用户意图写（⏸ / ⟳），**只在 doing
//     下有意义**；
//   - 两条不变量在这一处判：I1（none/done 下层二无意义）、I2（paused 不得残留到下一张
//     goal：只有 PausedGoalID == 当前 goal 才算暂停）。
//
// 为什么判定必须只有一处：paused 若被写成"一个布尔字段"，读的人就无法回答"这是哪张
// goal 的暂停"——换 goal 之后那个位就变成了新的 goal 上的残留位，而自动续跑的代价是
// 真金白银的回合。会话粒度（I3）由调用方的 sessionID 参数决定，本文件不持有任何状态。

import "strings"

// GoalRunStatus 是状态机的层一：goal 状态。
type GoalRunStatus string

const (
	// GoalRunNone 表示这段会话没有登记中的目标（也从没登记过）。
	GoalRunNone GoalRunStatus = "none"
	// GoalRunDoing 表示栈顶有一张**未收口**的目标。
	GoalRunDoing GoalRunStatus = "doing"
	// GoalRunDone 表示目标已经收口（finish / abort），栈上没有了。
	GoalRunDone GoalRunStatus = "done"
)

// GoalRunSubstate 是状态机的层二：自动续跑的意愿（**只在 doing 下有意义**）。
type GoalRunSubstate string

const (
	// GoalSubstateContinue 表示轮边界可以自动续跑。
	GoalSubstateContinue GoalRunSubstate = "continue"
	// GoalSubstatePaused 表示轮边界不再自动续跑（⏸）。它从不打断当前正在跑的那一轮。
	GoalSubstatePaused GoalRunSubstate = "paused"
)

// GoalRunState 是某个会话此刻的两层状态快照（值类型，无指针、无隐藏状态）。
type GoalRunState struct {
	// GoalID 是层一绑定的目标（GoalRunDoing 时非空）。
	GoalID string
	// Status 是层一。
	Status GoalRunStatus
	// Substate 是层二；Status != doing 时恒为 continue（I1）。
	Substate GoalRunSubstate
}

// Doing 报告层一是否为"在做一件未收口的目标"。
func (state GoalRunState) Doing() bool { return state.Status == GoalRunDoing }

// AutoContinue 报告"轮边界该不该自动续"：层一在跑且层二没被按停。
//
// 这是 pull 通道（责任链状态判定）唯一读的判断；push 通道（即时中断）不看它——
// push 停的是**在跑的子会话**，与"下一轮要不要自动发"是两件事。
func (state GoalRunState) AutoContinue() bool {
	return state.Status == GoalRunDoing && state.Substate == GoalSubstateContinue
}

// Paused 报告层二是否停在 paused。
func (state GoalRunState) Paused() bool { return state.Substate == GoalSubstatePaused }

// DeriveGoalRunState 是两层状态机的**唯一判定点**（纯函数，无副作用）。
//
//   - status 为空串时退化为按 activeGoalID 推断（非空 = doing，空 = none）：只给"手上
//     只有 goal id"的旧调用点用，新代码一律显式给 status；
//   - 认不得的 status 折成 none —— 没有可信的层一就不自动续（**不是默认放行**）；
//   - I1：status != doing 时层二一律读成 continue（"暂停"在没目标的会话上没有意义，
//     也不该拦住下一张 goal）；
//   - I2：只有在 paused 且 pausedGoalID **等于当前 goal** 时才算 paused。绑定的是别的
//     goal（换了目标）或根本没绑定（升级前的旧档）都读成 continue —— 宁可让用户重按一次
//     ⏸，也不把上一张 goal 的暂停套到下一张上。
func DeriveGoalRunState(activeGoalID string, status GoalRunStatus, paused bool, pausedGoalID string) GoalRunState {
	goalID := strings.TrimSpace(activeGoalID)
	switch status {
	case GoalRunNone, GoalRunDoing, GoalRunDone:
	case "":
		// 兼容形态：只给了 goal id。
		if goalID != "" {
			status = GoalRunDoing
		} else {
			status = GoalRunNone
		}
	default:
		status = GoalRunNone
	}
	if status == GoalRunDoing && goalID == "" {
		// 层一说在跑却没有目标可绑定：没有可交付的对象，不自动续。
		status = GoalRunNone
	}
	state := GoalRunState{GoalID: goalID, Status: status, Substate: GoalSubstateContinue}
	if status != GoalRunDoing || !paused {
		return state
	}
	if strings.TrimSpace(pausedGoalID) != goalID {
		return state
	}
	state.Substate = GoalSubstatePaused
	return state
}

// GoalRunStateOf 从 goal 栈读面（GoalStatusView）+ 会话存档里的暂停位合成两层状态。
// 装配侧（goal 栈与会话存档都在手）走这一条，不要自己在别处再算一遍层一。
func GoalRunStateOf(view GoalStatusView, paused bool, pausedGoalID string) GoalRunState {
	switch {
	case view.Active != nil && !view.Active.Status.Terminal():
		return DeriveGoalRunState(view.Active.ID, GoalRunDoing, paused, pausedGoalID)
	case view.Active != nil:
		// 栈顶已收口（还没弹栈的瞬时窗口）：按 done 读，不自动续。
		return DeriveGoalRunState("", GoalRunDone, false, pausedGoalID)
	case view.History > 0:
		return DeriveGoalRunState("", GoalRunDone, false, pausedGoalID)
	default:
		return DeriveGoalRunState("", GoalRunNone, false, pausedGoalID)
	}
}
