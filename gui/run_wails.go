//go:build gui

package gui

import (
	"context"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func Available() bool { return true }

func Run(deps Dependencies, config Options) error {
	bridge, err := NewBridge(deps, config)
	if err != nil {
		return err
	}
	assets, err := frontendAssets()
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
	closer := newCloseCoordinator(deps.Turn, func() { runtime.Quit(bridge.requestContext()) })

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
		// 启动配置警告已经由 bootstrap 写入会话可见区。这里不再使用原生
		// 原生对话框：它会形成阻塞模态框，配置异常时旧会话看起来“打不开”。
		OnDomReady:    func(context.Context) {},
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
