package dto

// bash_diagnostic.go —— scoped bash 的阶段诊断事件（metadata-only）。
//
// 形状放契约层的原因：它是**前端诊断面**（console / GUI 日志 / 冒烟驱动）与
// 工具执行面之间的稳定结构；放在工具实现包里会让前端为了一个只读事件形状
// 直接 import `backend/tool/registry`（D-M1-7 的越层边）。

// BashDiagnosticEvent 是一条 scoped bash 阶段事件。
type BashDiagnosticEvent struct {
	Stage string
	Shell string
	// Session 是发起这条命令的会话（工具记录按会话归类用；诊断日志忽略它）。
	// 只填会话 ID，不含任何正文/路径/命令文本。
	Session  string
	ExitCode int
	Err      error
}

// BashDiagnosticObserver 接收 best-effort 的阶段事件；实现方必须快速返回，
// 观察者失败与工具调用本身隔离。
type BashDiagnosticObserver func(BashDiagnosticEvent)
