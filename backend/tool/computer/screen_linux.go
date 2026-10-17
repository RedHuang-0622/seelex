//go:build linux

package computer

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math/bits"
	"os"
	"path/filepath"

	"github.com/jezek/xgb/xproto"
)

// 本文件是 X11 的观测面：虚拟桌面尺寸、光标位置、截图（XGetImage + ZPixmap
// 解码）。坐标一律是根窗口坐标系下的物理像素。

// VirtualScreen 返回 X11 根窗口的矩形（左上是 0,0）。多显示器在 X 里通常合成
// 一个屏幕，其并集正是根窗口尺寸，因此这里不需要 Xinerama。
func (x11Desktop) VirtualScreen() (Rect, error) {
	var rect Rect
	err := x11Use(func(s *x11Session) error {
		geometry, err := xproto.GetGeometry(s.conn, xproto.Drawable(s.root)).Reply()
		if err != nil {
			return fmt.Errorf("computer: 读取 X11 根窗口尺寸失败: %w", err)
		}
		rect = Rect{X: 0, Y: 0, Width: int(geometry.Width), Height: int(geometry.Height)}
		return nil
	})
	if err != nil {
		return Rect{}, err
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		return Rect{}, fmt.Errorf("computer: X11 根窗口尺寸非法 %s", rect)
	}
	return rect, nil
}

// CursorPosition 返回当前指针位置（根窗口坐标）。
func (x11Desktop) CursorPosition() (Point, error) {
	var point Point
	err := x11Use(func(s *x11Session) error {
		reply, err := xproto.QueryPointer(s.conn, s.root).Reply()
		if err != nil {
			return fmt.Errorf("computer: 读取指针位置失败: %w", err)
		}
		point = Point{X: int(reply.RootX), Y: int(reply.RootY)}
		return nil
	})
	return point, err
}

// CaptureShot 抓取屏幕（或指定区域）并按选项缩放。
//
// 区域先与虚拟桌面求交：XGetImage 对越界坐标会直接报 BadMatch，而模型给的
// 区域未必正好落在屏幕内；截到的就是交集，结果里回的就是交集（不假装截了
// 更大的范围）。
func (d x11Desktop) CaptureShot(opts ScreenshotOptions) (Capture, error) {
	screen, err := d.VirtualScreen()
	if err != nil {
		return Capture{}, err
	}
	region := screen
	if opts.Region != nil {
		region = clipRect(*opts.Region, screen)
	}
	if region.Width <= 0 || region.Height <= 0 {
		return Capture{}, fmt.Errorf("computer: 截图区域 %s 不在虚拟桌面 %s 内", *opts.Region, screen)
	}
	img, err := captureRegion(region)
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
func (d x11Desktop) SavePNG(path string, opts ScreenshotOptions) (Capture, error) {
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

// captureRegion 用 XGetImage 抓根窗口（或其中一块）的像素。
func captureRegion(region Rect) (*image.RGBA, error) {
	if region.Width <= 0 || region.Height <= 0 {
		return nil, fmt.Errorf("computer: 截图区域非法 %s", region)
	}
	var scanned *image.RGBA
	err := x11Use(func(s *x11Session) error {
		shot, err := xproto.GetImage(s.conn, xproto.ImageFormatZPixmap, xproto.Drawable(s.root),
			int16(region.X), int16(region.Y),
			uint16(region.Width), uint16(region.Height), ^uint32(0)).Reply()
		if err != nil {
			return fmt.Errorf("computer: XGetImage 失败: %w", err)
		}
		decoded, err := decodeZPixmap(s, shot.Data, region.Width, region.Height)
		if err != nil {
			return err
		}
		scanned = decoded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return scanned, nil
}

// decodeZPixmap 把 XGetImage 的原始 ZPixmap 数据解码成 RGBA：先按 PixmapFormats
// 的位深与 scanline_pad 定位每一行的起点，再按根 visual 的通道掩码取值。
//
// 只支持真彩（掩码存在）且位深 ≥ 8：depth<24 的伪彩根窗口在现代桌面上已经
// 不存在，遇到就显式报错，而不是给一张颜色对不上的图。
func decodeZPixmap(s *x11Session, data []byte, width, height int) (*image.RGBA, error) {
	bitsPerPixel := int(s.format.BitsPerPixel)
	pad := int(s.format.ScanlinePad)
	if bitsPerPixel < 8 || bitsPerPixel%8 != 0 || pad <= 0 {
		return nil, fmt.Errorf("computer: 不支持的像素格式（%d bpp, scanline_pad %d）", bitsPerPixel, pad)
	}
	if s.visual.RedMask|s.visual.GreenMask|s.visual.BlueMask == 0 {
		return nil, fmt.Errorf("computer: 根窗口 visual 没有通道掩码，无法解码（depth=%d）", s.screen.RootDepth)
	}
	bytesPerPixel := bitsPerPixel / 8
	stride := ((width*bitsPerPixel + pad - 1) / pad) * pad / 8
	if len(data) < stride*height {
		return nil, fmt.Errorf("computer: 截图数据不完整（%d 字节 < %d）", len(data), stride*height)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := data[y*stride:]
		for x := 0; x < width; x++ {
			value := rawPixel(s.order, row[x*bytesPerPixel:(x+1)*bytesPerPixel])
			offset := y*img.Stride + x*4
			img.Pix[offset] = channel(value, s.visual.RedMask)
			img.Pix[offset+1] = channel(value, s.visual.GreenMask)
			img.Pix[offset+2] = channel(value, s.visual.BlueMask)
			img.Pix[offset+3] = 0xFF
		}
	}
	return img, nil
}

// rawPixel 把 1/2/3/4 字节的像素按服务器字节序拼成 32 位值。
func rawPixel(order binary.ByteOrder, pixel []byte) uint32 {
	switch len(pixel) {
	case 1:
		return uint32(pixel[0])
	case 2:
		return uint32(order.Uint16(pixel))
	case 3:
		if order == binary.BigEndian {
			return uint32(pixel[0])<<16 | uint32(pixel[1])<<8 | uint32(pixel[2])
		}
		return uint32(pixel[0]) | uint32(pixel[1])<<8 | uint32(pixel[2])<<16
	default:
		return order.Uint32(pixel)
	}
}

// channel 按掩码取出一个通道并扩展到 8 位。
func channel(value, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	width := bits.OnesCount32(mask)
	narrowed := (value & mask) >> shift
	if width >= 8 {
		return uint8(narrowed >> (width - 8))
	}
	return uint8(narrowed * 255 / ((1 << width) - 1))
}
