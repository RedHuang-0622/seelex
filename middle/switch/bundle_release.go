package switching

// bundle_release.go 是**桥侧会话本体释放**的判据（P21-28「域方法随类型走」：从
// `backend/engine/assemble/runtime_session_release.go` 搬进本层；plan §2.4 明确把
// `seelebridge/runtime_session_release.go` 列为 `middle/switch/` 的目标）。
//
// 它与同目录的 `release.go`（"忙不忙"/"是否受保护"）是**释放面的两条不同判据**：那两条是
// **驻留层**的取舍（哪个已静默的会话可以被放掉），本条是**本体回收**的取舍（这个会话最重的
// 那份内存——框架 Session + 上下文存储——现在能不能真的拆掉）。
//
// 三种情形**拒绝释放**（返回 false，调用方保持驻留、不做半途释放）：
//   - 该会话是桥的活跃别名（legacy 读面仍指着它）—— 这条在宿主的 **TakeBundle** 里判，
//     因为它必须与"移出注册表"在同一把锁内（判完再移就变成了 window）；
//   - fork_subagents 在飞（含这一批子代理作业还在跑）：子代理要写回父会话；
//   - 还有在途后台命令：它们的结果要落回该会话。
//
// "已经不在注册表里 ⇒ false（幂等）" 同样由 TakeBundle 表达（没取到 = 没释放）。

import "strings"

// BundleReleaseDeps 是"释放桥侧会话本体"的宿主面（值语义）。
type BundleReleaseDeps struct {
	// ForkInFlight 报告该会话有没有在飞的 fork 子代理作业（含这一批还在跑）。
	ForkInFlight func(sessionID string) bool
	// AsyncPending 报告该会话还有几条在途后台命令。
	AsyncPending func(sessionID string) int
	// TakeBundle 在**注册表锁内**做两件事：活跃别名 / 在册两项检查，命中即移出并返回 true；
	// 是活跃别名或不在册 ⇒ false。合成一格端口是为了让那份检查与移出保持原子——拆成
	// "先读后写"就是一个可被并发切换插进来的窗口。
	TakeBundle func(sessionID string) bool
	// DropResidue 清掉同会话的 per-会话残格（只在**确实移出之后**调用一次）。
	DropResidue func(sessionID string)
}

// ReleaseBundle 释放一个会话的桥侧内存占用：返回是否真的释放。
//
// 顺序即判据：先问两份"还在忙吗"的读数（它们在各自锁内）→ 再在注册表锁内取走本体 →
// 最后清残格。任何一步说不行就**原样退回**（调用方保持驻留，不做半途释放）。
func ReleaseBundle(sessionID string, deps BundleReleaseDeps) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	if deps.ForkInFlight != nil && deps.ForkInFlight(sessionID) {
		return false
	}
	if deps.AsyncPending != nil && deps.AsyncPending(sessionID) > 0 {
		return false
	}
	if deps.TakeBundle == nil || !deps.TakeBundle(sessionID) {
		return false
	}
	if deps.DropResidue != nil {
		deps.DropResidue(sessionID)
	}
	return true
}
