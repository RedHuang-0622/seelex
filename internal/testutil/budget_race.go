//go:build race

package testutil

import "time"

// Budget 把测试里的"预算 / 超时"在 -race 下放宽。
//
// `go test -race` 自带 race 构建标签，因此这份文件只在 race 构建里生效：race
// 插桩会让并发路径慢好几倍，而这些数字的意图是**抓挂死**（liveness），不是量
// 性能；用同一组数字量两种构建，得到的是"race 下必现的假红"。
//
// 见 run 37105792167：race-and-coverage 里 application/core 的
// TestCompactionChurnOnOneSessionDoesNotHang 就踩在 15s 的固定预算上。
func Budget(d time.Duration) time.Duration { return d * 4 }
