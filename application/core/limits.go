package core

import "github.com/RedHuang-0622/seelex/seelexctx"

// 包级运行时上限：main 启动时经 ApplyLimits 注入（seele.yaml limits 段），
// 消费点经 activeLimits() 读取。测试与未注入路径使用 DefaultLimits 兜底，
// 与旧的硬编码常量行为一致。
var activeLimits = seelexctx.DefaultLimits()

// ApplyLimits 应用 seele.yaml limits 段（零值字段自动补默认）。
func ApplyLimits(limits seelexctx.Limits) {
	activeLimits = limits.WithDefaults()
}

// Limits 返回当前生效的运行时上限（只读拷贝语义）。
func Limits() seelexctx.Limits {
	return activeLimits
}
