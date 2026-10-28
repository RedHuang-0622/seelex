// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/RedHuang-0622/seelex/boot"
	"github.com/RedHuang-0622/seelex/internal/buildinfo"
	"github.com/RedHuang-0622/seelex/internal/startup"
)

// launchFlags 是本入口的命令行参数：flag 名、默认值与说明文案的权威定义在
// internal/startup（启动项可复用的关键——第二个入口只需 BindFlags 一次）。包级绑定
// 保持「启动参数在任何装配之前就位」的既有语义。
//
// 版本号与默认前端不在这里：它们是构建期 ldflags 注入点，权威值在 internal/buildinfo。
var launchFlags = startup.BindFlags(flag.CommandLine)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "✖ %v\n", err)
		os.Exit(1)
	}
}

// run 只做入口该做的事情：解析参数 → pprof 构建钩子 → 版本查询 → 装配（bootstrap）→
// 跑前端 → 关机。装配顺序、逆序关机与错误文案都在 wiring/bootstrap，本文件不再持有
// 任何装配知识——第二个入口（不同 build tag / 不同前端形态）可以直接复用 Run。
func run() error {
	flag.Parse()
	// pprof 构建钩子（-tags pprof）：Go 侧采样端口，默认 127.0.0.1:6060，
	// 与 GUI 前端性能钩子（window.__seelexPerf / PerfStats）配合做内存对照。
	startPprofHook()
	if launchFlags.ShowVersion {
		fmt.Println(buildinfo.Version)
		return nil
	}
	app, err := boot.Run(boot.Options{Flags: launchFlags, Version: buildinfo.Version})
	if err != nil {
		return err
	}
	defer app.Shutdown()
	return app.Serve()
}
