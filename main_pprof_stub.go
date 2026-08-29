//go:build !pprof

package main

// startPprofHook 是 pprof 构建钩子的空实现（默认构建不引入 net/http/pprof；
// 需要 Go 侧 heap/goroutine 采样时用 `go build -tags pprof .` 启用）。
func startPprofHook() {}
