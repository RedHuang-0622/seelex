package core

import (
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	limit "github.com/RedHuang-0622/seelex/application/policy"
)

// workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的
// 纯投影（`middle/view/worktable` 不反向依赖 `backend/limit`）。
//
// 这是本仓库唯一一处"limit.Limits → worktable.Limits"的转换点：投影侧只声明它用到
// 哪些上限，宿主只负责取数（判据仍在 `backend/limit` 一处）。
func workTableBounds() applicationtable.Limits {
	values := limit.Get()
	return applicationtable.Limits{
		WorkTableRows:  values.WorkTableRows,
		EvidenceChars:  values.EvidenceChars,
		PlanNodeEvents: values.PlanNodeEvents,
	}
}
