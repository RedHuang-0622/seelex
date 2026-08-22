// Package limits holds the process-wide application runtime limits (seele.yaml
// limits section). It is a leaf shared by core root and domain subpackages,
// which keeps ApplyLimits/Limits importable without root-package cycles.
package limits

import "github.com/RedHuang-0622/seelex/seelexctx"

// 包级运行时上限：main 启动时经 Apply 注入（seele.yaml limits 段），
// 消费点经 Get 读取。测试与未注入路径使用 DefaultLimits 兜底。
var active = seelexctx.DefaultLimits()

// Apply 应用 seele.yaml limits 段（零值字段自动补默认）。
func Apply(l seelexctx.Limits) {
	active = l.WithDefaults()
}

// Get 返回当前生效的运行时上限（只读拷贝语义）。
func Get() seelexctx.Limits {
	return active
}
