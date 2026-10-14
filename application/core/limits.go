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

// ApplySessionReleaseWindow 校验并**立刻生效**一个「会话驻留窗口」，返回真正生效的
// 值。校验在最前：非法值一个字节都不改（不落盘、不动内存）。
//
// 为什么叫 Apply 而不是 Set：它与 ApplyLimits 同族——写的是进程级 limits 快照。
// 消费方**现读**这个值（见 seelexctx.Limits.ReleaseWindow），所以这一步之后的下一次
// 释放决策就用新值，不需要重启、不需要遍历释放调用点。
func ApplySessionReleaseWindow(window int) (int, error) {
	if err := seelexctx.ValidateSessionReleaseWindow(window); err != nil {
		return 0, err
	}
	limits.SetSessionReleaseWindow(window)
	return limits.Get().ReleaseWindow(), nil
}
