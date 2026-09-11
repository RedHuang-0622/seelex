// Package limits holds the process-wide application runtime limits (seele.yaml
// limits section). It is a leaf shared by core root and domain subpackages,
// which keeps ApplyLimits/Limits importable without root-package cycles.
package limits

import (
	"sync/atomic"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 包级运行时上限：main 启动时经 Apply 注入（seele.yaml limits 段），
// 消费点经 Get 读取。测试与未注入路径使用 DefaultLimits 兜底。
//
// 用原子指针保存快照：Apply 与 Get 可能发生在不同 goroutine（后台会话
// goroutine 会在任务收尾时读上限），普通变量会构成数据竞争。指针整体
// 替换保证读者只会看到某个完整版本，不会读到撕裂值。
var active atomic.Pointer[seelexctx.Limits]

func init() {
	defaults := seelexctx.DefaultLimits()
	active.Store(&defaults)
}

// Apply 应用 seele.yaml limits 段（零值字段自动补默认）。
func Apply(l seelexctx.Limits) {
	applied := l.WithDefaults()
	active.Store(&applied)
}

// Get 返回当前生效的运行时上限（只读拷贝语义）。
func Get() seelexctx.Limits {
	if snapshot := active.Load(); snapshot != nil {
		return *snapshot
	}
	return seelexctx.DefaultLimits()
}
