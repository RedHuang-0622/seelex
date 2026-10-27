package switching

// release.go 收纳**释放面**的两条判据（plan §2.4 的 `session_release.go` → `middle/switch/`）：
// "这个会话忙不忙"与"这个会话是否属于受保护集（绝不释放）"。
//
// 为什么搬到这里：这两条判据的消费方横跨引擎驻留层（`resident_lru`）、正文层（`content_lru`）与
// 释放面本身——放在中间层可以让它们共享同一份实现，且不依赖任何宿主状态（宿主把读数装配成值传进来）。
//
// 落点沿革：2026-10-16 随 M5-R6-release-surface **接收者转移**从 `application/assembly/{resident_lru,session_release}.go`
// 归位；"从哪个会话单元读这四个数"仍是宿主的事（装配缝）。

// BusyState 是"会话是否正忙"需要的四个读数（由宿主从会话单元读出）。
type BusyState struct {
	// PendingApprovals 是待审批笔数（外部事件的等待方：审批在等用户点头）。
	PendingApprovals int
	// Running 是回合是否在跑。
	Running bool
	// QueuedCount 是排队输入数（回合运行期间承接的输入）。
	QueuedCount int
	// PendingRequests 是在飞请求数（已提交、尚未收尾）。
	PendingRequests int
}

// Busy 报告会话是否正忙：待审批、运行中、排队输入、在飞请求都算忙。
//
// 它是释放面守卫判据之一（**忙的会话绝不释放**）；零值 `BusyState`（会话不在单元表里）判为不忙，
// 与迁移前 `sessionBusy(nil) == false` 同口径。
func Busy(state BusyState) bool {
	if state.PendingApprovals > 0 {
		return true
	}
	if state.Running || state.QueuedCount > 0 {
		return true
	}
	return state.PendingRequests > 0
}

// ProtectedInput 是"受保护集"判据的输入。
type ProtectedInput struct {
	SessionID     string
	ViewSessionID string
	// Restoring 表示该会话正在后台冷加载（恢复中）。
	Restoring bool
	State     BusyState
}

// Protected 报告目标会话是否属于**受保护集**（绝不释放）：当前视图会话、待审批会话、
// 运行/排队/在飞会话、正在恢复的会话。
//
// 这是释放面的**唯一**判据——引擎层与正文层都必须调它，不允许各自复制一份条件
// （两层漂移会让"哪个会话被留着"变得不可解释）。
//
// 空会话 ID 判为受保护（与迁移前一致：没有可释放的目标就什么都不动）。
func Protected(input ProtectedInput) bool {
	if input.SessionID == "" {
		return true
	}
	if input.SessionID == input.ViewSessionID {
		return true
	}
	if input.Restoring {
		return true
	}
	return Busy(input.State)
}
