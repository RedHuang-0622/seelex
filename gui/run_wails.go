//go:build gui

package gui

import (
	"context"
	"io/fs"
	"strings"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/RedHuang-0622/seelex/headless"
)

func Available() bool { return true }

func Run(app Application, config Options) error {
	// headless 冒烟接口（headlessUI 设计）：仅当设置 SEELEX_HEADLESS_PORT
	// 时在 127.0.0.1 打开 JSON-RPC + 事件流控制面，外部驱动可像前端一样
	// 调用 Bridge 同源能力；未设置时零开销。
	stopHeadless, err := headless.StartIfRequested(app)
	if err != nil {
		return err
	}
	defer stopHeadless()

	bridge, err := NewBridge(app, config)
	if err != nil {
		return err
	}
	assets, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		return err
	}
	width := config.Width
	if width <= 0 {
		width = 1440
	}
	height := config.Height
	if height <= 0 {
		height = 900
	}
	closer := newCloseCoordinator(app, func() { runtime.Quit(bridge.requestContext()) })

	appOptions := &wailsoptions.App{
		Title:     bridge.info.Title,
		Width:     width,
		Height:    height,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 窗底 = 默认外观（qoder 皮肤 · 浅色）的环境渐变起点色，消除首帧闪色。
		BackgroundColour: &wailsoptions.RGBA{R: 235, G: 235, B: 198, A: 1},
		OnStartup: func(ctx context.Context) {
			bridge.Start(ctx, func(ctx context.Context, name string, payload any) {
				runtime.EventsEmit(ctx, name, payload)
			})
		},
		OnDomReady: func(ctx context.Context) {
			if warning := strings.TrimSpace(config.StartupWarning); warning != "" {
				_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
					Type:    runtime.ErrorDialog,
					Title:   "Seelex 配置警告",
					Message: warning,
				})
			}
		},
		OnBeforeClose: func(context.Context) bool { return closer.BeforeClose() },
		OnShutdown:    func(context.Context) { bridge.Stop() },
		Bind:          []interface{}{bridge},
	}

	// 毛玻璃皮肤（window-treatment.js 的责任链确认、Bridge.SetWindowAppearance 落盘）：
	// 窗口透明/亚克力**只能在建窗时设**（Wails 没有运行时开关），所以这里读上次落盘的
	// 外观设置，是 glass 就把窗口让出底色——同时必须清掉 BackgroundColour 那块实色窗底，
	// 否则它会把 WebView 身后的亚克力整个盖掉。其余皮肤一字不动。
	if windowWantsTranslucency(config.AppearancePath) {
		appOptions.BackgroundColour = nil
		appOptions.Windows = &windowoptions.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windowoptions.Acrylic,
		}
	}

	return wails.Run(appOptions)
}
