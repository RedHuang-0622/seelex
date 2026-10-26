package core

import (
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	limit "github.com/RedHuang-0622/seelex/application/policy"
)

// ApplyLimits 应用 seele.yaml limits 段（零值字段自动补默认）；
// 门面导出，实现位于基座包 backend/limit（判据与进程级快照的唯一落点）。
func ApplyLimits(l limit.Limits) {
	limit.Apply(l)
}

// Limits 返回当前生效的运行时上限（只读拷贝语义）。
func Limits() limit.Limits {
	return limit.Get()
}

// workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的
// 纯投影（`middle/view/worktable` 不反向依赖 `backend/limit`）。
//
// 这是本仓库唯一一处"limit.Limits → worktable.Limits"的转换点：投影侧只声明它用到
// 哪些上限，宿主只负责取数（判据仍在 `backend/limit` 一处）。
func workTableBounds() applicationtable.Limits {
	values := Limits()
	return applicationtable.Limits{
		WorkTableRows:  values.WorkTableRows,
		EvidenceChars:  values.EvidenceChars,
		PlanNodeEvents: values.PlanNodeEvents,
	}
}

// ApplySessionReleaseWindow 校验并**立刻生效**一个「会话驻留窗口」，返回真正生效的
// 值。校验在最前：非法值一个字节都不改（不落盘、不动内存）。
//
// 为什么叫 Apply 而不是 Set：它与 ApplyLimits 同族——写的是进程级 limits 快照。
// 消费方**现读**这个值（见 limit.Limits.ReleaseWindow），所以这一步之后的下一次
// 释放决策就用新值，不需要重启、不需要遍历释放调用点。
func ApplySessionReleaseWindow(window int) (int, error) {
	if err := limit.ValidateSessionReleaseWindow(window); err != nil {
		return 0, err
	}
	limit.SetSessionReleaseWindow(window)
	return limit.Get().ReleaseWindow(), nil
}
