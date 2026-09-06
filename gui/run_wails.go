//go:build gui

package gui

import (
	"context"
	"io/fs"
	"strings"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func Available() bool { return true }

func Run(app Application, config Options) error {
	// headless 冒烟接口（headlessUI 设计）：仅当设置 SEELEX_HEADLESS_PORT
	// 时在 127.0.0.1 打开 JSON-RPC + 事件流控制面，外部驱动可像前端一样
	// 调用 Bridge 同源能力；未设置时零开销。
	stopHeadless, err := startHeadlessIfRequested(app)
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

	return wails.Run(&wailsoptions.App{
		Title:     bridge.info.Title,
		Width:     width,
		Height:    height,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &wailsoptions.RGBA{R: 19, G: 22, B: 31, A: 1},
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
	})
}
