//go:build gui

package gui

import (
	"context"
	"io/fs"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func Available() bool { return true }

func Run(app Application, config Options) error {
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
			bridge.start(ctx, func(ctx context.Context, name string, payload any) {
				runtime.EventsEmit(ctx, name, payload)
			})
		},
		OnShutdown: func(context.Context) { bridge.stop() },
		Bind:       []interface{}{bridge},
	})
}
