package seelebridge

import (
	"os"
	"strings"

	bridgecomputer "github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

// ── computer use 装配面 ─────────────────────────────────────
//
// Seelex 侧的 computer use 工具族（原语在 seelebridge/tools/computer）在这里
// 接到运行时能力上：工具注册面 + 开关闸门。工具实现本身不依赖 seelebridge，
// 全部跨域能力都由闭包注入，因此这层只做装配。
//
// 图像通路（截图落会话媒体分区、随下一次请求送入模型、按 ref 读回）与既有的
// 随图链路同一处：`runtime_image.go` 的 storeSessionMedia / attachSessionImage /
// loadSessionMedia / mediaProjectIDFor。

// computerUseEnv 是 computer use 工具族的开关：缺省开（支持桌面的平台），
// `0/off/false/no` 关闭（无头/CI 或不希望模型碰桌面时用）。
const computerUseEnv = "SEELEX_COMPUTER_USE"

// registerComputerTools 注册 computer use 工具族（见 RegisterBuiltins）。
//
// 平台不支持（Capabilities().Desktop 为 false）或环境变量关闭时整体不注册：
// 宁可不给模型这族工具，也不挂一串必然返回 ErrUnsupported 的摆设。
func (r *Runtime) registerComputerTools() {
	if r == nil || r.registry == nil || !bridgecomputer.Supported() || !computerUseEnabled() {
		return
	}
	// 一次性平台准备（Windows 声明 Per-Monitor V2 DPI 感知，否则 125% 缩放下
	// 注入坐标会落到目标的 80%，见 computer/README.md）。返回错误表示桌面能力
	// 不可用——同样不注册，而不是挂上必然失败的工具。
	if err := bridgecomputer.Prepare(); err != nil {
		return
	}
	bridgecomputer.NewTools(bridgecomputer.Deps{
		RegisterTool: r.RegisterTool,
		StoreMedia:   r.storeSessionMedia,
		AttachImage:  r.attachSessionImage,
	}).Register()
}

// computerUseEnabled 解析 SEELEX_COMPUTER_USE（缺省开）。
func computerUseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(computerUseEnv))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}
