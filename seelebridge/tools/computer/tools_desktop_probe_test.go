package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"testing"
)

// TestScreenshotThroughRealDesktop 是默认跳过的真机冒烟：SEELEX_COMPUTER_DESKTOP_PROBE=1
// 时用**真实桌面**跑完整链路（真实原语截屏 → PNG 编码 → 落盘 → 随图队列），
// 证明工具层接到生产原语上仍然成立。CI/无交互桌面环境保持跳过。
//
// 用法：
//
//	$env:SEELEX_COMPUTER_DESKTOP_PROBE=1; go test ./seelebridge/tools/computer -run RealDesktop -v
func TestScreenshotThroughRealDesktop(t *testing.T) {
	if os.Getenv("SEELEX_COMPUTER_DESKTOP_PROBE") != "1" {
		t.Skip("设置 SEELEX_COMPUTER_DESKTOP_PROBE=1 才跑真机桌面冒烟")
	}
	if !Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	var (
		stored   []MediaAsset
		attached []PendingImage
	)
	tools := NewTools(Deps{
		RegisterTool: func(string, string, map[string]any, func(context.Context, string) (string, error)) {},
		StoreMedia: func(_ context.Context, asset MediaAsset) (StoredMedia, error) {
			stored = append(stored, asset)
			return StoredMedia{Ref: "media:probe", Hash: "probe", Bytes: len(asset.Data), Name: asset.Name}, nil
		},
		AttachImage: func(_ context.Context, image PendingImage) error {
			attached = append(attached, image)
			return nil
		},
	})
	handler := tools.screenshot

	result, err := handler(context.Background(), `{"max_width":1024}`)
	if err != nil {
		t.Fatalf("真机截屏失败: %v", err)
	}
	if len(stored) != 1 || len(attached) != 1 {
		t.Fatalf("落盘 %d / 随图 %d, want 1/1", len(stored), len(attached))
	}
	var payload screenshotResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("结果不是 JSON: %v (%s)", err, result)
	}
	if payload.Ref == "" || payload.Width == 0 || payload.Height == 0 {
		t.Fatalf("截图结果缺少引用/尺寸: %+v", payload)
	}
	if payload.Width > 1024 {
		t.Fatalf("max_width 未生效: %dx%d", payload.Width, payload.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(stored[0].Data))
	if err != nil {
		t.Fatalf("落盘字节不是可解码 PNG: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != payload.Width || bounds.Dy() != payload.Height {
		t.Fatalf("PNG 尺寸 %v 与结果 %dx%d 不一致", bounds, payload.Width, payload.Height)
	}
	t.Logf("真机截屏: %dx%d scale=%.3f region=%+v cursor=%+v foreground=%v bytes=%d",
		payload.Width, payload.Height, payload.Scale, payload.Region, payload.Cursor,
		payload.Foreground != nil && payload.Foreground.Title != "", len(stored[0].Data))

	// 窗口枚举走真实原语：只要有交互桌面就应当至少能拿到前台窗口信息。
	windows, err := NewTools(Deps{
		RegisterTool: func(string, string, map[string]any, func(context.Context, string) (string, error)) {},
	}).windows(context.Background(), `{"limit":5}`)
	if err != nil {
		t.Fatalf("窗口枚举失败: %v", err)
	}
	var listed windowsResult
	if err := json.Unmarshal([]byte(windows), &listed); err != nil {
		t.Fatalf("窗口结果不是 JSON: %v", err)
	}
	if listed.VirtualScreen.Width == 0 {
		t.Fatalf("虚拟桌面尺寸缺失: %+v", listed)
	}
	t.Logf("真机窗口: 虚拟桌面=%+v 总数=%d 返回=%d 前台=%v",
		listed.VirtualScreen, listed.Total, listed.Returned, listed.Foreground != nil)
}
