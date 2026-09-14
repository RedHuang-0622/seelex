package computer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"strings"
)

// ── 观察类工具：截屏 / 窗口枚举 / 窗口聚焦 ─────────────────────
//
// 截屏的落点是「会话媒体分区 + 下一次请求随图」：工具结果里只放引用与几何，
// 图像本体由 imageattach 队列送进模型（至多送一次）。这条链路让模型真的能
// 看到屏幕，而不是收到一串 base64 让它自己去猜。

type regionArg struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (r *regionArg) rect() *Rect {
	if r == nil {
		return nil
	}
	return &Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
}

type screenshotArgs struct {
	Region   *regionArg `json:"region"`
	MaxWidth int        `json:"max_width"`
}

type screenshotResult struct {
	Ref         string      `json:"ref"`
	Name        string      `json:"name"`
	Bytes       int         `json:"bytes"`
	Width       int         `json:"width"`
	Height      int         `json:"height"`
	Scale       float64     `json:"scale"`
	MaxWidth    int         `json:"max_width"`
	Region      rectJSON    `json:"region"`
	Cursor      *pointJSON  `json:"cursor,omitempty"`
	Foreground  *windowJSON `json:"foreground,omitempty"`
	Note        string      `json:"note"`
	Unavailable []string    `json:"unavailable,omitempty"`
}

func (t *Tools) screenshot(ctx context.Context, argsJSON string) (string, error) {
	var args screenshotArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	maxWidth := args.MaxWidth
	if maxWidth <= 0 {
		maxWidth = t.deps.MaxWidth
	}
	maxWidth = clampInt(maxWidth, minScreenshotWidth, maxScreenshotWidth)
	if args.Region != nil && (args.Region.Width <= 0 || args.Region.Height <= 0) {
		return "", errors.New("computer: region 需要正的 width/height")
	}
	capture, err := t.ops.capture(ScreenshotOptions{Region: args.Region.rect(), MaxWidth: maxWidth})
	if err != nil {
		return "", err
	}
	if capture.Image == nil {
		return "", errors.New("computer: 截屏返回空图像")
	}
	if t.deps.AttachImage == nil {
		return "", errors.New("computer: 当前装配没有随图通道，截图无法送达模型")
	}
	bounds := capture.Image.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buffer, capture.Image); err != nil {
		return "", fmt.Errorf("computer: 编码 PNG 失败: %w", err)
	}
	scale := capture.Scale
	if scale <= 0 {
		scale = 1
	}
	name := fmt.Sprintf("screenshot-%s.png", t.deps.Now().Format("20060102-150405.000"))
	stored, err := t.storeScreenshot(ctx, name, buffer.Bytes(), width, height, scale)
	if err != nil {
		return "", err
	}
	if err := t.deps.AttachImage(ctx, PendingImage{
		Ref:      stored.Ref,
		Label:    fmt.Sprintf("screenshot %dx%d", width, height),
		Data:     buffer.Bytes(),
		MimeType: "image/png",
		Name:     name,
		Width:    width,
		Height:   height,
	}); err != nil {
		return "", fmt.Errorf("computer: 画面未能随下一次请求送入模型: %w", err)
	}
	result := screenshotResult{
		Ref:      stored.Ref,
		Name:     name,
		Bytes:    len(buffer.Bytes()),
		Width:    width,
		Height:   height,
		Scale:    scale,
		MaxWidth: maxWidth,
		Region:   newRectJSON(capture.Region),
		Note:     "画面已挂进你的下一次请求（至多送一次）；点击前请结合 region/scale 换算坐标，坐标是虚拟桌面物理像素。",
	}
	// 光标与前台窗口是「当前在哪」的补充观测：单项取不到只标注缺失，
	// 不把已经拿到的画面一起丢掉。
	if point, err := t.ops.cursor(); err == nil {
		value := newPointJSON(point)
		result.Cursor = &value
	} else {
		result.Unavailable = append(result.Unavailable, "cursor")
	}
	if win, err := t.ops.foreground(); err == nil {
		value := newWindowJSON(win)
		result.Foreground = &value
	} else {
		result.Unavailable = append(result.Unavailable, "foreground_window")
	}
	return encodeResult(result)
}

type windowsArgs struct {
	Match         string `json:"match"`
	Limit         int    `json:"limit"`
	IncludeHidden bool   `json:"include_hidden"`
}

type windowsResult struct {
	VirtualScreen rectJSON     `json:"virtual_screen"`
	Foreground    *windowJSON  `json:"foreground,omitempty"`
	Total         int          `json:"total"`
	Returned      int          `json:"returned"`
	Windows       []windowJSON `json:"windows"`
	Unavailable   []string     `json:"unavailable,omitempty"`
}

func (t *Tools) windows(_ context.Context, argsJSON string) (string, error) {
	var args windowsArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = maxListedWindows
	}
	limit = clampInt(limit, 1, maxListedWindows)
	windows, err := t.ops.listWindows()
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(args.Match))
	result := windowsResult{Windows: make([]windowJSON, 0, limit)}
	for _, win := range windows {
		// 缺省只看可见窗口：桌面上的顶层窗口里大量是不可见/工具窗口
		// （实测一台机器 428 个有标题的顶层窗口），全列出来只会淹没有效信息。
		if !win.Visible && !args.IncludeHidden {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(win.Title), needle) {
			continue
		}
		result.Total++
		if len(result.Windows) < limit {
			result.Windows = append(result.Windows, newWindowJSON(win))
		}
	}
	result.Returned = len(result.Windows)
	if screen, err := t.ops.virtualScreen(); err == nil {
		result.VirtualScreen = newRectJSON(screen)
	} else {
		result.Unavailable = append(result.Unavailable, "virtual_screen")
	}
	if win, err := t.ops.foreground(); err == nil {
		value := newWindowJSON(win)
		result.Foreground = &value
	} else {
		result.Unavailable = append(result.Unavailable, "foreground_window")
	}
	return encodeResult(result)
}

type focusArgs struct {
	Match string `json:"match"`
}

type focusResult struct {
	Focused windowJSON `json:"focused"`
	Note    string     `json:"note"`
}

func (t *Tools) focus(_ context.Context, argsJSON string) (string, error) {
	var args focusArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	win, err := t.ops.focusWindow(args.Match)
	if err != nil {
		return "", err
	}
	return encodeResult(focusResult{
		Focused: newWindowJSON(win),
		Note:    "已置于前台；输入前建议再截一张图确认焦点落点。",
	})
}
