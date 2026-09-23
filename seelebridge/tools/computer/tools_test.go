package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

// ── 工具层单测（平台无关：原语注入假实现）────────────────────

type registeredTool struct {
	description string
	schema      map[string]any
	handler     func(context.Context, string) (string, error)
}

type clickCall struct {
	point Point
	opts  ClickOptions
}

type dragCall struct {
	from, to Point
	duration time.Duration
}

type scrollCall struct {
	point Point
	delta int
}

type keysCall struct {
	combo string
	times int
}

// recorder 收集假原语收到的调用，同时可注入单项失败。
type recorder struct {
	captureOpts   []ScreenshotOptions
	captureErr    error
	cursor        Point
	cursorErr     error
	virtual       Rect
	virtualErr    error
	foreground    Window
	foregroundErr error
	windows       []Window
	windowsErr    error
	focusCalls    []string
	focusResult   Window
	focusErr      error
	moves         []Point
	clickCalls    []clickCall
	dragCalls     []dragCall
	scrollCalls   []scrollCall
	// 可滚动面板：查询记录 + 可注入的结果/错误/逐次回读函数。
	scrollTargetQueries []ScrollTargetOptions
	scrollTargets       []ScrollTarget
	scrollTargetsErr    error
	scrollStateQueries  []Point
	scrollStateFn       func(Point) (ScrollTarget, bool, error)
	typed               []string
	keyCalls            []keysCall
	sleeps              []time.Duration
}

type harness struct {
	tools       *Tools
	toolsByName map[string]registeredTool
	stored      []MediaAsset
	attached    []PendingImage
	storeErr    error
	recorder    *recorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{toolsByName: map[string]registeredTool{}, recorder: &recorder{
		cursor:     Point{X: 42, Y: 24},
		virtual:    Rect{X: 0, Y: 0, Width: 1920, Height: 1080},
		foreground: Window{Handle: 0xAA, Title: "Seelex", Rect: Rect{X: 0, Y: 0, Width: 1280, Height: 720}, Visible: true},
		windows: []Window{
			{Handle: 0x1, Title: "Seelex", Rect: Rect{X: 0, Y: 0, Width: 1280, Height: 720}, Visible: true},
			{Handle: 0x2, Title: "Notes - Editor", Rect: Rect{X: 10, Y: 10, Width: 800, Height: 600}, Visible: true},
			{Handle: 0x3, Title: "Hidden", Rect: Rect{}, Visible: false, Minimized: true},
		},
		focusResult: Window{Handle: 0x2, Title: "Notes - Editor", Rect: Rect{X: 100, Y: 200, Width: 800, Height: 600}, Visible: true},
	}}
	h.tools = NewTools(Deps{
		RegisterTool: func(name, description string, schema map[string]any, handler func(context.Context, string) (string, error)) {
			h.toolsByName[name] = registeredTool{description: description, schema: schema, handler: handler}
		},
		StoreMedia: func(_ context.Context, asset MediaAsset) (StoredMedia, error) {
			if h.storeErr != nil {
				return StoredMedia{}, h.storeErr
			}
			h.stored = append(h.stored, asset)
			return StoredMedia{Ref: "media:deadbeef", Hash: "deadbeef", Bytes: len(asset.Data), Name: asset.Name}, nil
		},
		AttachImage: func(_ context.Context, image PendingImage) error {
			h.attached = append(h.attached, image)
			return nil
		},
		Now: func() time.Time { return time.Date(2026, 9, 14, 10, 30, 0, 0, time.Local) },
	})
	h.tools.ops = h.recorder.primitives()
	h.tools.Register()
	return h
}

func (r *recorder) primitives() primitives {
	return primitives{
		capture: func(opts ScreenshotOptions) (Capture, error) {
			r.captureOpts = append(r.captureOpts, opts)
			if r.captureErr != nil {
				return Capture{}, r.captureErr
			}
			image := image.NewRGBA(image.Rect(0, 0, 320, 180))
			for x := 0; x < 320; x++ {
				image.Set(x, 0, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xFF})
			}
			region := Rect{X: 0, Y: 0, Width: 640, Height: 360}
			if opts.Region != nil {
				region = *opts.Region
			}
			return Capture{Image: image, Region: region, Scale: 0.5, Cursor: r.cursor}, nil
		},
		virtualScreen: func() (Rect, error) { return r.virtual, r.virtualErr },
		cursor:        func() (Point, error) { return r.cursor, r.cursorErr },
		foreground:    func() (Window, error) { return r.foreground, r.foregroundErr },
		listWindows:   func() ([]Window, error) { return r.windows, r.windowsErr },
		focusWindow: func(match string) (Window, error) {
			r.focusCalls = append(r.focusCalls, match)
			if r.focusErr != nil {
				return Window{}, r.focusErr
			}
			return r.focusResult, nil
		},
		moveMouse: func(p Point) error { r.moves = append(r.moves, p); return nil },
		click: func(p Point, opts ClickOptions) error {
			r.clickCalls = append(r.clickCalls, clickCall{point: p, opts: opts})
			return nil
		},
		drag: func(from, to Point, d time.Duration) error {
			r.dragCalls = append(r.dragCalls, dragCall{from: from, to: to, duration: d})
			return nil
		},
		scroll: func(p Point, delta int) error {
			r.scrollCalls = append(r.scrollCalls, scrollCall{point: p, delta: delta})
			return nil
		},
		scrollTargets: func(opts ScrollTargetOptions) ([]ScrollTarget, error) {
			r.scrollTargetQueries = append(r.scrollTargetQueries, opts)
			return r.scrollTargets, r.scrollTargetsErr
		},
		scrollState: func(p Point, _ time.Duration) (ScrollTarget, bool, error) {
			r.scrollStateQueries = append(r.scrollStateQueries, p)
			if r.scrollStateFn != nil {
				return r.scrollStateFn(p)
			}
			return ScrollTarget{}, false, nil
		},
		typeText: func(text string) error { r.typed = append(r.typed, text); return nil },
		pressKeys: func(combo string, times int) error {
			r.keyCalls = append(r.keyCalls, keysCall{combo: combo, times: times})
			return nil
		},
		sleep: func(d time.Duration) { r.sleeps = append(r.sleeps, d) },
	}
}

func (h *harness) call(t *testing.T, name, argsJSON string) string {
	t.Helper()
	tool, ok := h.toolsByName[name]
	if !ok {
		t.Fatalf("tool %s 未注册（已注册：%v）", name, h.toolNames())
	}
	result, err := tool.handler(context.Background(), argsJSON)
	if err != nil {
		t.Fatalf("%s(%s) 失败: %v", name, argsJSON, err)
	}
	return result
}

func (h *harness) callErr(t *testing.T, name, argsJSON string) error {
	t.Helper()
	tool, ok := h.toolsByName[name]
	if !ok {
		t.Fatalf("tool %s 未注册", name)
	}
	if _, err := tool.handler(context.Background(), argsJSON); err == nil {
		t.Fatalf("%s(%s) 期望报错，实际通过", name, argsJSON)
	} else {
		return err
	}
	return nil
}

func (h *harness) toolNames() []string {
	names := make([]string, 0, len(h.toolsByName))
	for name := range h.toolsByName {
		names = append(names, name)
	}
	return names
}

func TestRegisterExposesWholeComputerToolFamily(t *testing.T) {
	h := newHarness(t)
	want := []string{
		ToolScreenshot, ToolWindows, ToolFocus, ToolClick, ToolMove,
		ToolDrag, ToolScroll, ToolScrollTargets, ToolType, ToolKeys, ToolWait,
	}
	for _, name := range want {
		tool, ok := h.toolsByName[name]
		if !ok {
			t.Fatalf("工具 %s 未注册（已注册：%v）", name, h.toolNames())
		}
		if strings.TrimSpace(tool.description) == "" {
			t.Fatalf("工具 %s 缺少描述", name)
		}
		if tool.schema["type"] != "object" {
			t.Fatalf("工具 %s 的 schema 不是 object: %v", name, tool.schema)
		}
		if _, ok := tool.schema["properties"].(map[string]any); !ok {
			t.Fatalf("工具 %s 的 schema 缺少 properties: %v", name, tool.schema)
		}
	}
	if len(h.toolsByName) != len(want) {
		t.Fatalf("注册工具数 = %d, want %d", len(h.toolsByName), len(want))
	}
}

func TestScreenshotStoresMediaAndAttachesToNextRequest(t *testing.T) {
	h := newHarness(t)
	result := h.call(t, ToolScreenshot, `{"max_width":800,"region":{"x":10,"y":20,"width":200,"height":100}}`)

	if len(h.recorder.captureOpts) != 1 {
		t.Fatalf("capture 调用 = %d, want 1", len(h.recorder.captureOpts))
	}
	opts := h.recorder.captureOpts[0]
	if opts.MaxWidth != 800 {
		t.Fatalf("max_width = %d, want 800", opts.MaxWidth)
	}
	if opts.Region == nil || *opts.Region != (Rect{X: 10, Y: 20, Width: 200, Height: 100}) {
		t.Fatalf("region = %+v", opts.Region)
	}
	if len(h.stored) != 1 || len(h.attached) != 1 {
		t.Fatalf("落盘 %d 张 / 随图 %d 张, want 1/1", len(h.stored), len(h.attached))
	}
	asset := h.stored[0]
	if asset.Kind != "screenshot" || asset.MimeType != "image/png" {
		t.Fatalf("媒体来源标记 = %q / %q", asset.Kind, asset.MimeType)
	}
	if asset.Width != 320 || asset.Height != 180 || asset.Scale != 0.5 {
		t.Fatalf("媒体尺寸/缩放 = %dx%d scale=%v", asset.Width, asset.Height, asset.Scale)
	}
	decoded, err := png.Decode(bytes.NewReader(asset.Data))
	if err != nil {
		t.Fatalf("落盘字节不是可解码 PNG: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 320 || bounds.Dy() != 180 {
		t.Fatalf("PNG 尺寸 = %v", bounds)
	}
	if !strings.HasPrefix(asset.Name, "screenshot-20260914-103000") || !strings.HasSuffix(asset.Name, ".png") {
		t.Fatalf("截图文件名 = %q", asset.Name)
	}
	attached := h.attached[0]
	if attached.Ref != "media:deadbeef" || attached.MimeType != "image/png" || attached.Width != 320 || attached.Height != 180 {
		t.Fatalf("随图载荷 = %+v", attached)
	}
	if !bytes.Equal(attached.Data, asset.Data) {
		t.Fatal("随图字节与落盘字节不一致")
	}
	if !strings.Contains(attached.Label, "320x180") {
		t.Fatalf("随图来源标签 = %q", attached.Label)
	}

	var payload screenshotResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("结果不是 JSON: %v (%s)", err, result)
	}
	if payload.Ref != "media:deadbeef" || payload.Width != 320 || payload.Height != 180 || payload.MaxWidth != 800 {
		t.Fatalf("结果字段 = %+v", payload)
	}
	if payload.Region != (rectJSON{X: 10, Y: 20, Width: 200, Height: 100}) {
		t.Fatalf("结果 region = %+v", payload.Region)
	}
	if payload.Cursor == nil || *payload.Cursor != (pointJSON{X: 42, Y: 24}) {
		t.Fatalf("结果 cursor = %+v", payload.Cursor)
	}
	if payload.Foreground == nil || payload.Foreground.Title != "Seelex" || payload.Foreground.Handle != "0xAA" {
		t.Fatalf("结果前台窗口 = %+v", payload.Foreground)
	}
}

func TestScreenshotClampsWidthAndRejectsBadRegion(t *testing.T) {
	h := newHarness(t)
	h.call(t, ToolScreenshot, `{"max_width":100000}`)
	if got := h.recorder.captureOpts[0].MaxWidth; got != maxScreenshotWidth {
		t.Fatalf("max_width 上限钳制 = %d, want %d", got, maxScreenshotWidth)
	}
	h.call(t, ToolScreenshot, `{"max_width":1}`)
	if got := h.recorder.captureOpts[1].MaxWidth; got != minScreenshotWidth {
		t.Fatalf("max_width 下限钳制 = %d, want %d", got, minScreenshotWidth)
	}
	h.callErr(t, ToolScreenshot, `{"region":{"width":0,"height":10}}`)
	h.callErr(t, ToolScreenshot, `{"max_width":"wide"}`)
}

func TestScreenshotFailsLoudWithoutMediaOrAttach(t *testing.T) {
	h := newHarness(t)
	if _, err := h.tools.storeScreenshot(context.Background(), "x.png", []byte("png"), 1, 1, 1); err != nil {
		t.Fatalf("落盘桩失败: %v", err)
	}
	// 落盘失败要显式冒泡（不做无引用的临时截图）。
	h.storeErr = errors.New("quota exceeded")
	if _, err := h.tools.storeScreenshot(context.Background(), "x.png", []byte("png"), 1, 1, 1); err == nil {
		t.Fatal("落盘失败必须报错")
	}
	// 超出字节上限不落盘。
	h.storeErr = nil
	h.tools.deps.MaxMediaBytes = 4
	if _, err := h.tools.storeScreenshot(context.Background(), "x.png", []byte("12345"), 1, 1, 1); err == nil {
		t.Fatal("超出字节上限必须报错")
	}
	// 没有随图通道时截屏整体失败（截了也没人看得到）。
	h.tools.deps.MaxMediaBytes = 1 << 20
	h.tools.deps.AttachImage = nil
	h.callErr(t, ToolScreenshot, `{}`)
	// 原语错误原样冒泡。
	h.tools.deps.AttachImage = func(context.Context, PendingImage) error { return nil }
	h.recorder.captureErr = ErrUnsupported
	if err := h.callErr(t, ToolScreenshot, `{}`); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("原语错误应冒泡，得到 %v", err)
	}
}

func TestWindowsFiltersLimitsAndDegrades(t *testing.T) {
	h := newHarness(t)
	var all windowsResult
	if err := json.Unmarshal([]byte(h.call(t, ToolWindows, `{}`)), &all); err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 || all.Returned != 2 {
		t.Fatalf("缺省只看可见窗口: 总数/返回 = %d/%d, want 2/2（不可见窗口被过滤）", all.Total, all.Returned)
	}
	if all.VirtualScreen != (rectJSON{X: 0, Y: 0, Width: 1920, Height: 1080}) {
		t.Fatalf("虚拟桌面 = %+v", all.VirtualScreen)
	}
	if all.Foreground == nil || all.Foreground.Title != "Seelex" {
		t.Fatalf("前台窗口 = %+v", all.Foreground)
	}

	var withHidden windowsResult
	if err := json.Unmarshal([]byte(h.call(t, ToolWindows, `{"include_hidden":true}`)), &withHidden); err != nil {
		t.Fatal(err)
	}
	if withHidden.Total != 3 || withHidden.Returned != 3 {
		t.Fatalf("include_hidden 应含不可见窗口: %d/%d, want 3/3", withHidden.Total, withHidden.Returned)
	}

	var filtered windowsResult
	if err := json.Unmarshal([]byte(h.call(t, ToolWindows, `{"match":"notes","limit":1}`)), &filtered); err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || filtered.Returned != 1 || filtered.Windows[0].Handle != "0x2" {
		t.Fatalf("过滤结果 = %+v", filtered)
	}

	// 单项观测失败只标注缺失，不让整次调用失败。
	h.recorder.virtualErr = errors.New("no desktop")
	h.recorder.foregroundErr = errors.New("no foreground")
	var degraded windowsResult
	if err := json.Unmarshal([]byte(h.call(t, ToolWindows, `{}`)), &degraded); err != nil {
		t.Fatal(err)
	}
	if len(degraded.Unavailable) != 2 {
		t.Fatalf("缺失项 = %v, want virtual_screen + foreground_window", degraded.Unavailable)
	}
}

func TestFocusDelegatesMatch(t *testing.T) {
	h := newHarness(t)
	var result focusResult
	if err := json.Unmarshal([]byte(h.call(t, ToolFocus, `{"match":"notes"}`)), &result); err != nil {
		t.Fatal(err)
	}
	if len(h.recorder.focusCalls) != 1 || h.recorder.focusCalls[0] != "notes" {
		t.Fatalf("focus 调用 = %v", h.recorder.focusCalls)
	}
	if result.Focused.Handle != "0x2" || result.Focused.Title != "Notes - Editor" {
		t.Fatalf("聚焦结果 = %+v", result.Focused)
	}
	h.recorder.focusErr = errors.New("窗口不存在")
	h.callErr(t, ToolFocus, `{"match":"nope"}`)
}

func TestClickResolvesCoordinatesAndValidates(t *testing.T) {
	h := newHarness(t)
	h.call(t, ToolClick, `{"x":111,"y":222}`)
	if len(h.recorder.clickCalls) != 1 || h.recorder.clickCalls[0].point != (Point{X: 111, Y: 222}) {
		t.Fatalf("点击坐标 = %+v", h.recorder.clickCalls)
	}
	if opts := h.recorder.clickCalls[0].opts; opts.Button != "left" || opts.Clicks != 1 || opts.Interval != 0 {
		t.Fatalf("点击参数 = %+v", opts)
	}

	// 只给 window：先聚焦，再点窗口中心。
	h.call(t, ToolClick, `{"window":"notes","button":"RIGHT","clicks":2,"interval_ms":50}`)
	if len(h.recorder.focusCalls) != 1 || h.recorder.focusCalls[0] != "notes" {
		t.Fatalf("window 参数未聚焦: %v", h.recorder.focusCalls)
	}
	center := h.recorder.focusResult.Rect.Center()
	last := h.recorder.clickCalls[len(h.recorder.clickCalls)-1]
	if last.point != center {
		t.Fatalf("窗口中心点击 = %+v, want %+v", last.point, center)
	}
	if last.opts.Button != "right" || last.opts.Clicks != 2 || last.opts.Interval != 50*time.Millisecond {
		t.Fatalf("点击参数 = %+v", last.opts)
	}

	// window + x/y：聚焦用 window，坐标用显式值。
	h.call(t, ToolClick, `{"x":5,"y":6,"window":"notes"}`)
	last = h.recorder.clickCalls[len(h.recorder.clickCalls)-1]
	if last.point != (Point{X: 5, Y: 6}) {
		t.Fatalf("显式坐标应优先: %+v", last.point)
	}

	h.callErr(t, ToolClick, `{}`)      // 既无坐标也无窗口
	h.callErr(t, ToolClick, `{"x":5}`) // 只有一个坐标
	h.callErr(t, ToolClick, `{"x":1,"y":1,"button":"wheel"}`)
	h.callErr(t, ToolClick, `{"x":1,"y":1,"clicks":4}`)
	h.callErr(t, ToolClick, `{"x":1,"y":1,"clicks":-1}`)
}

func TestInputToolsValidateAndDelegate(t *testing.T) {
	h := newHarness(t)

	h.call(t, ToolMove, `{"x":7,"y":8}`)
	if len(h.recorder.moves) != 1 || h.recorder.moves[0] != (Point{X: 7, Y: 8}) {
		t.Fatalf("move 调用 = %+v", h.recorder.moves)
	}
	h.callErr(t, ToolMove, `{"x":7}`)

	h.call(t, ToolDrag, `{"from":{"x":1,"y":2},"to":{"x":30,"y":40},"duration_ms":500}`)
	if len(h.recorder.dragCalls) != 1 {
		t.Fatalf("drag 调用 = %+v", h.recorder.dragCalls)
	}
	drag := h.recorder.dragCalls[0]
	if drag.from != (Point{X: 1, Y: 2}) || drag.to != (Point{X: 30, Y: 40}) || drag.duration != 500*time.Millisecond {
		t.Fatalf("drag 参数 = %+v", drag)
	}
	h.call(t, ToolDrag, `{"from":{"x":0,"y":0},"to":{"x":1,"y":1}}`)
	if got := h.recorder.dragCalls[1].duration; got != 300*time.Millisecond {
		t.Fatalf("drag 缺省时长 = %v, want 300ms", got)
	}
	h.callErr(t, ToolDrag, `{"from":{"x":0,"y":0}}`)

	h.call(t, ToolScroll, `{"delta":240}`)
	if len(h.recorder.scrollCalls) != 1 || h.recorder.scrollCalls[0].point != (Point{X: 42, Y: 24}) {
		t.Fatalf("滚轮缺省落点应为光标: %+v", h.recorder.scrollCalls)
	}
	h.call(t, ToolScroll, `{"x":9,"y":9,"delta":-120}`)
	if last := h.recorder.scrollCalls[1]; last.point != (Point{X: 9, Y: 9}) || last.delta != -120 {
		t.Fatalf("滚轮参数 = %+v", last)
	}
	h.callErr(t, ToolScroll, `{"delta":0}`)
	h.callErr(t, ToolScroll, `{"delta":100000}`)
	h.callErr(t, ToolScroll, `{"x":1,"delta":120}`)

	h.call(t, ToolType, `{"text":"你好 Seelex 🚀"}`)
	if len(h.recorder.typed) != 1 || h.recorder.typed[0] != "你好 Seelex 🚀" {
		t.Fatalf("type 调用 = %+v", h.recorder.typed)
	}
	h.callErr(t, ToolType, `{"text":""}`)
	h.callErr(t, ToolType, `{"text":"`+strings.Repeat("a", maxTypeChars+1)+`"}`)

	h.call(t, ToolKeys, `{"keys":"ctrl+shift+t","times":3}`)
	if len(h.recorder.keyCalls) != 1 || h.recorder.keyCalls[0] != (keysCall{combo: "ctrl+shift+t", times: 3}) {
		t.Fatalf("keys 调用 = %+v", h.recorder.keyCalls)
	}
	h.call(t, ToolKeys, `{"keys":"enter"}`)
	if h.recorder.keyCalls[1].times != 1 {
		t.Fatalf("keys 缺省次数 = %d, want 1", h.recorder.keyCalls[1].times)
	}
	h.callErr(t, ToolKeys, `{"keys":"  "}`)
	h.callErr(t, ToolKeys, `{"keys":"enter","times":11}`)

	h.call(t, ToolWait, `{"milliseconds":1500}`)
	if len(h.recorder.sleeps) != 1 || h.recorder.sleeps[0] != 1500*time.Millisecond {
		t.Fatalf("wait 调用 = %+v", h.recorder.sleeps)
	}
	h.call(t, ToolWait, `{"milliseconds":999999}`)
	if got := h.recorder.sleeps[1]; got != maxWaitMilliseconds*time.Millisecond {
		t.Fatalf("wait 上限钳制 = %v", got)
	}
	h.callErr(t, ToolWait, `{"milliseconds":-1}`)
}
