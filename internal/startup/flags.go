package startup

import (
	"flag"
	"time"

	"github.com/RedHuang-0622/seelex/internal/buildinfo"
)

// Flags 是一次启动的全部命令行参数。它是**启动项可复用**的关键：任何入口
// （`go run .`、发布二进制、GUI 宿主、诊断入口）都只需 `BindFlags` 一次再把自己那份
// Options 交给 wiring/bootstrap，不必各自复述一遍 flag 名与默认值。
//
// 默认值即安全基线（见 e2e/release 契约：`-permission` 默认 manual、`-frontend`
// 默认 tui）。版本号与默认前端**不在这里**：它们是构建期 ldflags 注入点，权威值在
// internal/buildinfo（`-X github.com/RedHuang-0622/seelex/internal/buildinfo.Version`）。
type Flags struct {
	StorePath      string
	PluginsPaths   string
	PermissionMode string
	FrontendMode   string
	BackendPrompt  string
	BackendTimeout time.Duration
	BackendLogPath string
	BackendProject string
	ShowVersion    bool
}

// BindFlags 把启动参数注册到 fs 并返回读写句柄；flag 名、默认值与说明文案是
// 对外契约（脚本、文档与 e2e 契约都读它），改动必须同步 README 与用例。
func BindFlags(fs *flag.FlagSet) *Flags {
	flags := &Flags{}
	fs.StringVar(&flags.StorePath, "store", ".seelex/sessions", "持久化存储路径")
	fs.StringVar(&flags.PluginsPaths, "plugins", "", "Plugin 根目录（逗号分隔，可多根）；留空走责任链：$SEELEX_PLUGINS > <exe>/plugins > <exe>/../plugins > plugins(CWD)")
	fs.StringVar(&flags.PermissionMode, "permission", "manual", "权限档位: manual(默认) | edit(自动改文件) | auto(自动执行) | full(全权，旧别名 full_access)")
	fs.StringVar(&flags.FrontendMode, "frontend", buildinfo.DefaultFrontend, "前端模式: tui | gui | headless | backend")
	fs.StringVar(&flags.BackendPrompt, "backend-prompt", "", "后端诊断请求（仅 -frontend backend；为空时从标准输入逐行读取）")
	fs.DurationVar(&flags.BackendTimeout, "backend-timeout", 2*time.Minute, "后端单次诊断请求的最大等待时间")
	fs.StringVar(&flags.BackendLogPath, "backend-log", "", "后端诊断日志文件（仅 -frontend backend；仍同步输出到标准输出）")
	fs.StringVar(&flags.BackendProject, "backend-project", "", "后端诊断绑定的项目根目录（仅 -frontend backend；显式提供才会绑定）")
	fs.BoolVar(&flags.ShowVersion, "version", false, "显示版本号并退出")
	return flags
}
