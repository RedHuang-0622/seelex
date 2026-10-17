package computer

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writeTempPNG 在临时目录写一张 w×h 的 PNG，返回路径与文件原文。
func writeTempPNG(t *testing.T, dir, name string, width, height int) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("写入测试 PNG 失败: %v", err)
	}
	return path, buffer.Bytes()
}

func TestViewImageFileKeepsOriginalBytes(t *testing.T) {
	dir := t.TempDir()
	path, raw := writeTempPNG(t, dir, "shot.png", 800, 400)

	viewed, err := ViewImageFile(ViewImageOptions{Path: path})
	if err != nil {
		t.Fatalf("ViewImageFile 失败: %v", err)
	}
	if viewed.Reencoded {
		t.Fatalf("未降采样时不该重新编码")
	}
	if !bytes.Equal(viewed.Data, raw) {
		t.Fatalf("未缩放时 Data 应等于文件原文")
	}
	if viewed.MimeType != "image/png" {
		t.Fatalf("MimeType = %q, want image/png", viewed.MimeType)
	}
	if viewed.Width != 800 || viewed.Height != 400 {
		t.Fatalf("尺寸 = %dx%d, want 800x400", viewed.Width, viewed.Height)
	}
	if viewed.SourceWidth != 800 || viewed.SourceHeight != 400 {
		t.Fatalf("原尺寸 = %dx%d, want 800x400", viewed.SourceWidth, viewed.SourceHeight)
	}
	if viewed.Scale != 1 {
		t.Fatalf("Scale = %v, want 1", viewed.Scale)
	}
}

func TestViewImageFileDownsamplesWithoutRewritingSource(t *testing.T) {
	dir := t.TempDir()
	path, raw := writeTempPNG(t, dir, "shot.png", 800, 400)

	viewed, err := ViewImageFile(ViewImageOptions{Path: path, MaxWidth: 200})
	if err != nil {
		t.Fatalf("ViewImageFile 失败: %v", err)
	}
	if !viewed.Reencoded {
		t.Fatalf("超过 max_width 时应重新编码")
	}
	if viewed.MimeType != "image/png" {
		t.Fatalf("MimeType = %q, want image/png", viewed.MimeType)
	}
	if viewed.Width != 200 || viewed.Height != 100 {
		t.Fatalf("缩放后尺寸 = %dx%d, want 200x100", viewed.Width, viewed.Height)
	}
	if viewed.Scale != 0.25 {
		t.Fatalf("Scale = %v, want 0.25", viewed.Scale)
	}
	decoded, format, err := image.Decode(bytes.NewReader(viewed.Data))
	if err != nil {
		t.Fatalf("缩放结果无法解码: %v", err)
	}
	if format != "png" {
		t.Fatalf("缩放结果格式 = %q, want png", format)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 200 || bounds.Dy() != 100 {
		t.Fatalf("解码尺寸 = %dx%d, want 200x100", bounds.Dx(), bounds.Dy())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("重读原文件失败: %v", err)
	}
	if !bytes.Equal(after, raw) {
		t.Fatalf("看图改写了原文件")
	}
}

func TestViewImageFileSkipsScaleWhenNarrowEnough(t *testing.T) {
	dir := t.TempDir()
	path, raw := writeTempPNG(t, dir, "shot.png", 400, 200)

	viewed, err := ViewImageFile(ViewImageOptions{Path: path, MaxWidth: 1600})
	if err != nil {
		t.Fatalf("ViewImageFile 失败: %v", err)
	}
	if viewed.Reencoded || !bytes.Equal(viewed.Data, raw) {
		t.Fatalf("宽度不足时应原样返回")
	}
}

func TestViewImageFileErrors(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeTempPNG(t, dir, "shot.png", 40, 20)
	emptyPath := filepath.Join(dir, "empty.png")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatalf("写入空文件失败: %v", err)
	}
	textPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(textPath, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("写入非图片文件失败: %v", err)
	}

	cases := []struct {
		name   string
		opts   ViewImageOptions
		target error
	}{
		{name: "空路径", opts: ViewImageOptions{}},
		{name: "路径不存在", opts: ViewImageOptions{Path: filepath.Join(dir, "missing.png")}, target: ErrImageNotFound},
		{name: "路径是目录", opts: ViewImageOptions{Path: dir}, target: ErrImageNotFile},
		{name: "空文件", opts: ViewImageOptions{Path: emptyPath}, target: ErrImageEmpty},
		{name: "不是图片", opts: ViewImageOptions{Path: textPath}, target: ErrImageFormatUnsupported},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ViewImageFile(testCase.opts)
			if err == nil {
				t.Fatalf("期望报错，实际成功")
			}
			if testCase.target != nil && !errors.Is(err, testCase.target) {
				t.Fatalf("err = %v, want errors.Is(_, %v)", err, testCase.target)
			}
		})
	}

	if _, err := ViewImageFile(ViewImageOptions{Path: path}); err != nil {
		t.Fatalf("正常图片不该报错: %v", err)
	}
}
