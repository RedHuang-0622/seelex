package computer

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	"image/png"
	"os"
	"strings"
)

// MaxViewImageBytes 是「看一张本地图片」允许读入的字节上限，仅作防呆：
// 它不是 wire 上限（那是调用方的事），超限时显式报错而不是把内存吃满。
const MaxViewImageBytes = 32 << 20

// 看图的失败语义：每一种都显式区分，避免调用方把「文件不存在」和
// 「格式不支持」混成一句“看不了”。
var (
	ErrImageNotFound          = errors.New("computer: 图片文件不存在")
	ErrImageNotFile           = errors.New("computer: 路径不是普通文件")
	ErrImageEmpty             = errors.New("computer: 图片文件为空")
	ErrImageTooLargeToRead    = errors.New("computer: 图片文件超出读取上限")
	ErrImageFormatUnsupported = errors.New("computer: 无法解码的图片格式")
)

// ViewImageOptions 描述一次「查看本地图片」。
type ViewImageOptions struct {
	// Path 是要查看的本地图片路径。看图**只读**：不拷贝、不改写、不落盘。
	Path string
	// MaxWidth 大于 0 且原图更宽时，最近邻降采样后重新编码为 PNG；
	// 缩放只影响给模型看的像素，不改动原文件。
	MaxWidth int
}

// ViewedImage 是一次看图的观测结果。
type ViewedImage struct {
	// Path 是实际读取的路径。
	Path string
	// MimeType 是返回字节的内容类型（重编码后为 image/png）。
	MimeType string
	// Data 是 wire 可用的字节原文（未 base64）。
	Data []byte
	// Width/Height 是返回图像的尺寸；SourceWidth/SourceHeight 是原图尺寸。
	Width        int
	Height       int
	SourceWidth  int
	SourceHeight int
	// Scale 是返回图像相对原图的缩放系数，1 表示未缩放。
	Scale float64
	// Reencoded 表示是否因降采样而重新编码（true 时 Data 不再是文件原文）。
	Reencoded bool
}

// ViewImageFile 读取一张本地图片，必要时降采样，返回可直接下发的字节与元数据。
//
// 与截图原语的边界：截图原语**拷贝**到会话资产，看图**不拷贝**——路径失效时
// 只能显式报错，不能靠缓存兜底。
func ViewImageFile(opts ViewImageOptions) (ViewedImage, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return ViewedImage{}, errors.New("computer: 需要图片路径")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ViewedImage{}, fmt.Errorf("%w: %s", ErrImageNotFound, path)
		}
		return ViewedImage{}, fmt.Errorf("computer: 读取图片路径失败 %s: %w", path, err)
	}
	if info.IsDir() {
		return ViewedImage{}, fmt.Errorf("%w: %s", ErrImageNotFile, path)
	}
	if info.Size() == 0 {
		return ViewedImage{}, fmt.Errorf("%w: %s", ErrImageEmpty, path)
	}
	if info.Size() > MaxViewImageBytes {
		return ViewedImage{}, fmt.Errorf("%w: %s 为 %d 字节 > %d", ErrImageTooLargeToRead, path, info.Size(), MaxViewImageBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ViewedImage{}, fmt.Errorf("computer: 读取图片失败 %s: %w", path, err)
	}
	mimeType, decoded, err := decodeImage(data)
	if err != nil {
		return ViewedImage{}, fmt.Errorf("%w: %s: %w", ErrImageFormatUnsupported, path, err)
	}
	bounds := decoded.Bounds()
	original := ViewedImage{
		Path:         path,
		MimeType:     mimeType,
		Data:         data,
		Width:        bounds.Dx(),
		Height:       bounds.Dy(),
		SourceWidth:  bounds.Dx(),
		SourceHeight: bounds.Dy(),
		Scale:        1,
	}
	if opts.MaxWidth <= 0 || bounds.Dx() <= opts.MaxWidth {
		return original, nil
	}
	scaled, scale := ScaleNearest(toRGBA(decoded), opts.MaxWidth)
	if scaled == nil {
		return original, nil
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, scaled); err != nil {
		return ViewedImage{}, fmt.Errorf("computer: 编码降采样后的 PNG 失败: %w", err)
	}
	scaledBounds := scaled.Bounds()
	return ViewedImage{
		Path:         path,
		MimeType:     "image/png",
		Data:         buffer.Bytes(),
		Width:        scaledBounds.Dx(),
		Height:       scaledBounds.Dy(),
		SourceWidth:  bounds.Dx(),
		SourceHeight: bounds.Dy(),
		Scale:        scale,
		Reencoded:    true,
	}, nil
}

// decodeImage 解码图片并归一化 MIME 类型；解码器由标准库注册表提供。
func decodeImage(data []byte) (string, image.Image, error) {
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, err
	}
	switch format {
	case "jpeg":
		return "image/jpeg", decoded, nil
	case "png":
		return "image/png", decoded, nil
	case "gif":
		return "image/gif", decoded, nil
	default:
		return "image/" + format, decoded, nil
	}
}

// toRGBA 把任意解码结果转成 RGBA，供最近邻缩放使用。
func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}
