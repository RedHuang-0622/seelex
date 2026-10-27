package limit

import (
	"sync/atomic"
)

// 包级运行时上限：main 启动时经 Apply 注入（seele.yaml limits 段），
// 消费点经 Get 读取。测试与未注入路径使用 DefaultLimits 兜底。
//
// 用原子指针保存快照：Apply 与 Get 可能发生在不同 goroutine（后台会话
// goroutine 会在任务收尾时读上限），普通变量会构成数据竞争。指针整体
// 替换保证读者只会看到某个完整版本，不会读到撕裂值。
var active atomic.Pointer[Limits]

func init() {
	defaults := DefaultLimits()
	active.Store(&defaults)
}

// Apply 应用 seele.yaml limits 段（零值字段自动补默认）。
func Apply(l Limits) {
	applied := l.WithDefaults()
	active.Store(&applied)
}

// SetSessionReleaseWindow 只改**一格**并原子发布：读当前快照 → 改这一格 → 整体替换。
//
// 为什么不必通知任何消费方：窗口是**现读**的（消费方在每次释放决策时调
// Limits.ReleaseWindow()），替换之后下一次释放就用新值——不必重启，也不必
// 碰任何 LRU 调用点。写入方（application/assembly 的门面）负责先校验取值。
//
// 不用 Apply 是因为 Apply 会重跑 WithDefaults：这里要的是"只动这一格"，其余格原样
// 保留（快照本来就是补过默认的完整版本）。
func SetSessionReleaseWindow(window int) {
	current := Get()
	current.SessionReleaseWindow = window
	active.Store(&current)
}

// Get 返回当前生效的运行时上限（只读拷贝语义）。
func Get() Limits {
	if snapshot := active.Load(); snapshot != nil {
		return *snapshot
	}
	return DefaultLimits()
}
