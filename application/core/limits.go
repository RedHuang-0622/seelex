package core

import (
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// ApplyLimits 应用 seele.yaml limits 段（零值字段自动补默认）；
// 门面导出，实现位于 core/internal/limits 叶子包。
func ApplyLimits(l seelexctx.Limits) {
	limits.Apply(l)
}

// Limits 返回当前生效的运行时上限（只读拷贝语义）。
func Limits() seelexctx.Limits {
	return limits.Get()
}
