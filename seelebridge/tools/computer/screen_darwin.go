//go:build darwin

package computer

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"unsafe"
)

// 本文件是 macOS 的观测面：虚拟桌面尺寸、光标位置、截屏。
//
// 坐标语义与 Windows/Linux 两端对齐：一律用 CoreGraphics 的全局坐标空间
// （多显示器并集，左上角可能为负）。macOS 的全局空间单位是"点"，Retina 屏上
// 1 点 = 2 像素；为了让"模型看到的画面像素"与"computer_click 给的坐标"落在
// 同一套数上，截图在采样时就按显示器的 backing scale 折回点空间——画面 1 像素
// 永远等于 1 点，模型不需要额外换算。

// darwinActiveDisplays 枚举在线显示器（返回 displayID 列表）。
func darwinActiveDisplays() ([]uint32, error) {
	// 先按 16 个问一次；count 被顶到上限说明还有更多，再按返回的 count 扩容重问。
	displays := make([]uint32, 16)
	var count uint32
	if status := cgGetActiveDisplayList(uint32(len(displays)), &displays[0], &count); status != 0 {
		return nil, fmt.Errorf("computer: CGGetActiveDisplayList 失败（CGError=%d）", status)
	}
	if count <= uint32(len(displays)) {
		return displays[:count], nil
	}
	displays = make([]uint32, count)
	if status := cgGetActiveDisplayList(count, &displays[0], &count); status != 0 {
		return nil, fmt.Errorf("computer: CGGetActiveDisplayList 失败（CGError=%d）", status)
	}
	return displays[:count], nil
}

// VirtualScreen 返回全部在线显示器的并集矩形（CoreGraphics 全局坐标，单位：点）。
func (darwinDesktop) VirtualScreen() (Rect, error) {
	if err := darwinProbe(); err != nil {
		return Rect{}, err
	}
	displays, err := darwinActiveDisplays()
	if err != nil {
		return Rect{}, err
	}
	if len(displays) == 0 {
		return Rect{}, fmt.Errorf("computer: 没有在线显示器")
	}
	union, ok := darwinUnionBounds(displays)
	if !ok {
		return Rect{}, fmt.Errorf("computer: 无法读取显示器边界")
	}
	if union.Width <= 0 || union.Height <= 0 {
		return Rect{}, fmt.Errorf("computer: 显示器并集尺寸非法 %s", union)
	}
	return union, nil
}

// darwinUnionBounds 求显示器边界的并集。
func darwinUnionBounds(displays []uint32) (Rect, bool) {
	union := Rect{}
	first := true
	for _, id := range displays {
		bounds := cgDisplayBounds(id)
		rect := Rect{X: int(bounds.Origin.X), Y: int(bounds.Origin.Y),
			Width: int(bounds.Size.Width), Height: int(bounds.Size.Height)}
		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}
		if first {
			union, first = rect, false
			continue
		}
		if rect.X < union.X {
			union.Width += union.X - rect.X
			union.X = rect.X
		}
		if rect.Y < union.Y {
			union.Height += union.Y - rect.Y
			union.Y = rect.Y
		}
		if right := rect.X + rect.Width; right > union.X+union.Width {
			union.Width = right - union.X
		}
		if bottom := rect.Y + rect.Height; bottom > union.Y+union.Height {
			union.Height = bottom - union.Y
		}
	}
	return union, !first
}

// CursorPosition 返回当前指针位置（CoreGraphics 全局坐标，单位：点）。
func (darwinDesktop) CursorPosition() (Point, error) {
	if err := darwinProbe(); err != nil {
		return Point{}, err
	}
	event := cgEventCreate(0)
	if event == 0 {
		return Point{}, fmt.Errorf("computer: CGEventCreate 失败")
	}
	defer cfRelease(event)
	location := cgEventGetLocation(event)
	return Point{X: int(location.X), Y: int(location.Y)}, nil
}

// CaptureShot 抓取屏幕（或指定区域）并按选项缩放。
// 区域先与虚拟桌面求交（越界坐标不假装截到了屏幕外）。
func (d darwinDesktop) CaptureShot(opts ScreenshotOptions) (Capture, error) {
	screen, err := d.VirtualScreen()
	if err != nil {
		return Capture{}, err
	}
	region := screen
	if opts.Region != nil {
		region = clipRect(*opts.Region, screen)
	}
	if region.Width <= 0 || region.Height <= 0 {
		return Capture{}, fmt.Errorf("computer: 截图区域不在虚拟桌面 %s 内", screen)
	}
	img, err := darwinCaptureRegion(region)
	if err != nil {
		return Capture{}, err
	}
	scaled, scale := ScaleNearest(img, opts.MaxWidth)
	cursor, err := d.CursorPosition()
	if err != nil {
		cursor = Point{}
	}
	return Capture{Image: scaled, Region: region, Scale: scale, Cursor: cursor}, nil
}

// SavePNG 抓屏并写入 PNG 文件，返回实际截取区域与缩放系数。
func (d darwinDesktop) SavePNG(path string, opts ScreenshotOptions) (Capture, error) {
	capture, err := d.CaptureShot(opts)
	if err != nil {
		return Capture{}, err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Capture{}, fmt.Errorf("computer: 创建截图目录失败: %w", err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return Capture{}, fmt.Errorf("computer: 创建截图文件失败: %w", err)
	}
	defer file.Close()
	if err := png.Encode(file, capture.Image); err != nil {
		return Capture{}, fmt.Errorf("computer: 编码 PNG 失败: %w", err)
	}
	return capture, nil
}

// darwinCaptureRegion 抓取一块"点空间"区域：逐显示器取像素图，再按 backing
// scale 采样回点空间，拼进同一张 RGBA。
func darwinCaptureRegion(region Rect) (*image.RGBA, error) {
	displays, err := darwinActiveDisplays()
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	covered := false
	for _, id := range displays {
		bounds := cgDisplayBounds(id)
		display := Rect{X: int(bounds.Origin.X), Y: int(bounds.Origin.Y),
			Width: int(bounds.Size.Width), Height: int(bounds.Size.Height)}
		inter := clipRect(region, display)
		if inter.Width <= 0 || inter.Height <= 0 {
			continue
		}
		imageRef := cgDisplayCreateImage(id)
		if imageRef == 0 {
			return nil, fmt.Errorf("computer: CGDisplayCreateImage(%d) 返回空（可能是缺少「屏幕录制」授权）", id)
		}
		source, err := darwinImageToRGBA(imageRef)
		cfRelease(uintptr(imageRef))
		if err != nil {
			return nil, err
		}
		darwinBlitDisplay(out, region, display, source)
		covered = true
	}
	if !covered {
		return nil, fmt.Errorf("computer: 区域 %s 没有落在任何在线显示器上", region)
	}
	return out, nil
}

// darwinBlitDisplay 把一块显示器的像素图按 backing scale 采样进输出图。
func darwinBlitDisplay(out *image.RGBA, region, display Rect, source *image.RGBA) {
	ratioX, ratioY := 1.0, 1.0
	if display.Width > 0 && source.Rect.Dx() > 0 {
		ratioX = float64(source.Rect.Dx()) / float64(display.Width)
	}
	if display.Height > 0 && source.Rect.Dy() > 0 {
		ratioY = float64(source.Rect.Dy()) / float64(display.Height)
	}
	inter := clipRect(region, display)
	for y := inter.Y; y < inter.Y+inter.Height; y++ {
		for x := inter.X; x < inter.X+inter.Width; x++ {
			sourceX := int((float64(x-display.X) + 0.5) * ratioX)
			sourceY := int((float64(y-display.Y) + 0.5) * ratioY)
			sourceX = clampInt(sourceX, 0, source.Rect.Dx()-1)
			sourceY = clampInt(sourceY, 0, source.Rect.Dy()-1)
			offset := sourceY*source.Stride + sourceX*4
			target := (y-region.Y)*out.Stride + (x-region.X)*4
			copy(out.Pix[target:target+4], source.Pix[offset:offset+4])
			out.Pix[target+3] = 0xFF
		}
	}
}

// darwinImageToRGBA 把一张 CGImage 解码成 RGBA。
//
// CGDisplayCreateImage 通常给 32 位的 BGRA（little-endian、alpha first）；
// 这里显式读 bitmapInfo 判断字节序，遇到不认识的格式显式报错，而不是给一张
// 颜色错位的图。
func darwinImageToRGBA(imageRef uintptr) (*image.RGBA, error) {
	width := int(cgImageGetWidth(imageRef))
	height := int(cgImageGetHeight(imageRef))
	bitsPerPixel := int(cgImageGetBitsPerPixel(imageRef))
	bytesPerRow := int(cgImageGetBytesPerRow(imageRef))
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("computer: CGImage 尺寸非法（%dx%d）", width, height)
	}
	if bitsPerPixel != 32 {
		return nil, fmt.Errorf("computer: 不支持的 CGImage 位深（%d）", bitsPerPixel)
	}
	provider := cgImageGetDataProvider(imageRef)
	if provider == 0 {
		return nil, fmt.Errorf("computer: CGImage 没有 data provider")
	}
	data := cgDataProviderCopyData(provider)
	if data == 0 {
		return nil, fmt.Errorf("computer: 复制 CGImage 像素失败")
	}
	defer cfRelease(data)
	length := int(cfDataGetLength(data))
	if length < bytesPerRow*height {
		return nil, fmt.Errorf("computer: CGImage 像素不完整（%d < %d）", length, bytesPerRow*height)
	}
	base := cfDataGetBytePtr(data)
	if base == nil {
		return nil, fmt.Errorf("computer: CGImage 像素指针为空")
	}
	info := cgImageGetBitmapInfo(imageRef)
	little := (info & kCGBitmapByteOrderMask) == kCGBitmapByteOrder32Little
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := unsafe.Add(base, y*bytesPerRow)
		for x := 0; x < width; x++ {
			pixel := (*[4]byte)(unsafe.Add(row, x*4))
			target := y*out.Stride + x*4
			if little {
				// 内存顺序 B,G,R,A
				out.Pix[target] = pixel[2]
				out.Pix[target+1] = pixel[1]
				out.Pix[target+2] = pixel[0]
			} else {
				// 内存顺序 A,R,G,B
				out.Pix[target] = pixel[1]
				out.Pix[target+1] = pixel[2]
				out.Pix[target+2] = pixel[3]
			}
			out.Pix[target+3] = 0xFF
		}
	}
	return out, nil
}
