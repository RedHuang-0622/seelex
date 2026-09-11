//go:build windows

package computer

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	user32DLL   = syscall.NewLazyDLL("user32.dll")
	gdi32DLL    = syscall.NewLazyDLL("gdi32.dll")
	kernel32DLL = syscall.NewLazyDLL("kernel32.dll")

	procSetProcessDPIAwareness = user32DLL.NewProc("SetProcessDpiAwarenessContext")
	procGetDC                  = user32DLL.NewProc("GetDC")
	procReleaseDC              = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32DLL.NewProc("GetSystemMetrics")

	procCreateCompatibleDC     = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32DLL.NewProc("SelectObject")
	procBitBlt                 = gdi32DLL.NewProc("BitBlt")
	procGetDIBits              = gdi32DLL.NewProc("GetDIBits")
	procDeleteObject           = gdi32DLL.NewProc("DeleteObject")
	procDeleteDC               = gdi32DLL.NewProc("DeleteDC")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// EnableDPIAwareness 把当前进程设置为 Per-Monitor V2 DPI 感知。
// 截图与鼠标坐标都以物理像素为准，必须在使用其它函数前调用。
func EnableDPIAwareness() {
	if err := procSetProcessDPIAwareness.Find(); err != nil {
		return
	}
	const dpiAwarenessPerMonitorV2 = ^uintptr(3) // -4
	_, _, _ = procSetProcessDPIAwareness.Call(dpiAwarenessPerMonitorV2)
}

// VirtualScreen 返回多显示器并集组成的虚拟桌面矩形。
func VirtualScreen() (Rect, error) {
	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	w, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if w == 0 || h == 0 {
		return Rect{}, fmt.Errorf("computer: 无法获取虚拟桌面尺寸")
	}
	return Rect{X: int(int32(x)), Y: int(int32(y)), Width: int(int32(w)), Height: int(int32(h))}, nil
}

// captureRegion 用 GDI BitBlt 抓取指定区域的像素。
func captureRegion(region Rect) (*image.RGBA, error) {
	if region.Width <= 0 || region.Height <= 0 {
		return nil, fmt.Errorf("computer: 截图区域非法 %s", region)
	}
	hdcScreen, _, dcErr := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("computer: GetDC 失败: %v", dcErr)
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, memErr := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("computer: CreateCompatibleDC 失败: %v", memErr)
	}
	defer procDeleteDC.Call(hdcMem)

	hbm, _, bmErr := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(region.Width), uintptr(region.Height))
	if hbm == 0 {
		return nil, fmt.Errorf("computer: CreateCompatibleBitmap 失败: %v", bmErr)
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(hdcMem, hbm)
	defer procSelectObject.Call(hdcMem, old)

	ret, _, bltErr := procBitBlt.Call(
		hdcMem, 0, 0, uintptr(region.Width), uintptr(region.Height),
		hdcScreen, uintptr(region.X), uintptr(region.Y), srcCopy,
	)
	if ret == 0 {
		return nil, fmt.Errorf("computer: BitBlt 失败: %v", bltErr)
	}

	info := bitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(bitmapInfoHeader{}))
	info.Header.Width = int32(region.Width)
	info.Header.Height = -int32(region.Height) // 负值表示自顶向下
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = biRGB
	info.Header.SizeImage = uint32(region.Width * region.Height * 4)

	buf := make([]byte, region.Width*region.Height*4)
	ret, _, dibErr := procGetDIBits.Call(
		hdcMem, hbm, 0, uintptr(region.Height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info)), dibRGBColors,
	)
	if ret == 0 {
		return nil, fmt.Errorf("computer: GetDIBits 失败: %v", dibErr)
	}

	img := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	for i := 0; i+3 < len(buf); i += 4 {
		img.Pix[i] = buf[i+2]
		img.Pix[i+1] = buf[i+1]
		img.Pix[i+2] = buf[i]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}

// CaptureShot 抓取屏幕并按选项缩放。
func CaptureShot(opts ScreenshotOptions) (Capture, error) {
	region := Rect{}
	if opts.Region != nil {
		region = *opts.Region
	} else {
		screen, err := VirtualScreen()
		if err != nil {
			return Capture{}, err
		}
		region = screen
	}
	img, err := captureRegion(region)
	if err != nil {
		return Capture{}, err
	}
	scaled, scale := ScaleNearest(img, opts.MaxWidth)
	cursor, err := CursorPosition()
	if err != nil {
		cursor = Point{}
	}
	return Capture{Image: scaled, Region: region, Scale: scale, Cursor: cursor}, nil
}

// SavePNG 抓屏并写入 PNG 文件，返回实际截取区域与缩放系数。
func SavePNG(path string, opts ScreenshotOptions) (Capture, error) {
	capture, err := CaptureShot(opts)
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
