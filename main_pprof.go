//go:build pprof

package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" // 注册 /debug/pprof/* 到 DefaultServeMux
	"os"
	"runtime"
)

// startPprofHook 在 build tag pprof 下启动 Go 侧采样端口（默认
// 127.0.0.1:6060，可用 SEELEX_PPROF_ADDR 覆盖）。用于排除法对照：
// GUI 内存大头在 WebView2（Chromium）渲染进程，Go 侧 pprof 只能确认
// Go 主进程很小，救不了渲染进程；与快照截断/前端折叠配合做基线对比。
//
// 构建：go build -tags pprof .
func startPprofHook() {
	// pprof 构建默认采集全部 mutex/block 事件，供 headless 冒烟观察锁等待与
	// 阻塞链（普通构建不引入这两个采样点）。
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
	addr := os.Getenv("SEELEX_PPROF_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6060"
	}
	go func() {
		log.Printf("pprof: Go 侧采样端口 %s（curl %s/debug/pprof/heap 抓 heap）", addr, addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("pprof: listen %s: %v", addr, err)
		}
	}()
}
