package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Seelex 侧 computer use 工具面 ────────────────────────────
//
// 原语层（同包 desktop.go 的抽象面 + 平台实现，见 desktop.go 的路由说明）之上的一层：把
// 「看屏幕、动鼠标、敲键盘、找窗口」暴露成 Seelex 自己的工具，模型在会话里
// 直接调用；主要调用方是 seelebridge.Runtime.RegisterBuiltins。
//
// 与 MCP 面（mcp/main.go）的分工是刻意的：
//
//   - MCP 面服务外部宿主（Codex 等），图像以 base64 内联在 tool result 里，
//     宿主自己决定怎么放上下文；
//   - 本层服务 Seelex 自己的 agent：画面**落会话媒体分区**（`media:<hash>`），
//     再经 imageattach 队列随下一次模型请求送入（至多送一次）。工具结果里
//     因此只有引用与几何信息，不内联 base64——宿主上下文只有 200k，不该为
//     了一时便利浪费。
//
// 安全边界：本层不做审批判断，工具的 allow/ask/deny 由权限门控（
// seele.yaml 的 permission.rules）决定；默认兜底规则 `* → ask` 即要求逐次
// 审批。输入注入类工具对子代理不可见（见 seelebridge/tools/policy.go）：
// 并行子代理共享同一块桌面，同时注入输入会互相打断。

// 工具名（稳定契约：权限规则、插件 include/exclude、可见性策略都按名字匹配）。
const (
	ToolScreenshot    = "computer_screenshot"
	ToolWindows       = "computer_windows"
	ToolScrollTargets = "computer_scroll_targets"
	ToolFocus         = "computer_focus"
	ToolClick         = "computer_click"
	ToolMove          = "computer_move"
	ToolDrag          = "computer_drag"
	ToolScroll        = "computer_scroll"
	ToolType          = "computer_type"
	ToolKeys          = "computer_keys"
	ToolWait          = "computer_wait"
)

// 默认上限：截图给模型看的宽度、单张截图的媒体字节上限（与
// sessionstore 的 media.max_item_bytes 默认 8 MiB 对齐）。
const (
	DefaultMaxWidth      = 1600
	DefaultMaxMediaBytes = 8 << 20

	minScreenshotWidth = 320
	maxScreenshotWidth = 4096

	// maxTypeChars 限制一次注入的字符数：键盘注入是逐 UTF-16 单元的系统
	// 调用，过长文本既慢又不可控（贴长文该走剪贴板/文件）。
	maxTypeChars = 4000
	// maxWaitMilliseconds 限制单次等待，避免把一次工具调用变成挂起。
	maxWaitMilliseconds = 10000
	// maxListedWindows 限制 windows 工具返回的窗口行数。
	maxListedWindows = 60
	// maxScrollDelta 是单次滚轮注入的绝对值上限（WHEEL_DELTA=120 的 10 格）。
	maxScrollDelta = 1200
)

// MediaAsset 是一次媒体写入请求（工具层不依赖 sessionstore：由装配层适配）。
type MediaAsset struct {
	// Name 是原始文件名（落盘只取 basename）。
	Name string
	// MimeType 是内容类型，截图固定 image/png。
	MimeType string
	// Kind 是来源标记，截图固定 screenshot（仅用于溯源展示）。
	Kind string
	// Data 是二进制原文（PNG）。
	Data []byte
	// Width/Height 是像素尺寸。
	Width  int
	Height int
	// Scale 是相对原截取区域的缩放系数（1 = 未缩放）。
	Scale float64
}

// StoredMedia 是一条已落盘媒体资产的引用（sessionstore.MediaRef 的窄投影）。
type StoredMedia struct {
	Ref   string
	Hash  string
	Bytes int
	Name  string
}

// PendingImage 是「下一次模型请求随图」的载荷：装配层适配为
// imageattach.Attachment（含 types.FilePart）。
type PendingImage struct {
	Ref      string
	Label    string
	Data     []byte
	MimeType string
	Name     string
	Width    int
	Height   int
}

// Deps 是工具层的装配面：全部由 seelebridge.Runtime 注入闭包，因此本包
// 不反向依赖 seelebridge / sessionstore / imageattach（保持原语包的边界）。
type Deps struct {
	// RegisterTool 注册一个工具（生产为 Runtime.RegisterTool）。
	RegisterTool func(name, description string, schema map[string]any, handler func(context.Context, string) (string, error))
	// StoreMedia 把截图落进会话媒体分区并返回 `media:<hash>` 引用。
	StoreMedia func(context.Context, MediaAsset) (StoredMedia, error)
	// AttachImage 把画面挂进「下一次请求随图」队列；会话由 ctx 决定，
	// 取不到会话时返回错误（宁可不送，也不串会话）。
	AttachImage func(context.Context, PendingImage) error
	// MaxWidth 是截图给模型看的默认最大宽度（0 用 DefaultMaxWidth）。
	MaxWidth int
	// MaxMediaBytes 是单张截图的字节上限（0 用 DefaultMaxMediaBytes）。
	MaxMediaBytes int
	// Now 供截图文件名取时间戳（测试注入；nil 用 time.Now）。
	Now func() time.Time
}

// Tools 是 Seelex 侧 computer use 工具族。
type Tools struct {
	deps Deps
	ops  primitives
}

// primitives 是平台原语的注入面：生产用真实实现（Current()），测试注入假实现，
// 因此工具族的参数校验、媒体落盘与随图语义可以在任何平台上单测。
type primitives struct {
	capabilities  func() Capabilities
	capture       func(ScreenshotOptions) (Capture, error)
	virtualScreen func() (Rect, error)
	cursor        func() (Point, error)
	foreground    func() (Window, error)
	listWindows   func() ([]Window, error)
	scrollTargets func(ScrollTargetOptions) ([]ScrollTarget, error)
	scrollState   func(Point, time.Duration) (ScrollTarget, bool, error)
	focusWindow   func(string) (Window, error)
	moveMouse     func(Point) error
	click         func(Point, ClickOptions) error
	drag          func(Point, Point, time.Duration) error
	scroll        func(Point, int) error
	typeText      func(string) error
	pressKeys     func(string, int) error
	sleep         func(time.Duration)
}

// defaultPrimitives 把平台抽象面（Desktop）的方法绑成工具层的注入面：上层只
// 认这一个接口，底下是 Win32、X11 还是桩实现都与它无关。Current() 返回编译期
// 选定的本平台实现（见 desktop.go 的路由说明）——平台一切换，这里绑到的就是
// 新实现的同名方法，工具层与装配层一行都不用改。
func defaultPrimitives(desktop Desktop) primitives {
	return primitives{
		capabilities:  desktop.Capabilities,
		capture:       desktop.CaptureShot,
		virtualScreen: desktop.VirtualScreen,
		cursor:        desktop.CursorPosition,
		foreground:    desktop.ForegroundWindow,
		listWindows:   desktop.ListWindows,
		scrollTargets: desktop.ListScrollTargets,
		scrollState:   desktop.ScrollStateAtPoint,
		focusWindow:   desktop.FocusWindow,
		moveMouse:     desktop.MoveMouse,
		click:         desktop.Click,
		drag:          desktop.Drag,
		scroll:        desktop.Scroll,
		typeText:      desktop.TypeText,
		pressKeys:     desktop.PressKeys,
		sleep:         func(d time.Duration) { Sleep(int(d / time.Millisecond)) },
	}
}

// NewTools 构造工具族；缺省上限与时间源在此补齐。
func NewTools(deps Deps) *Tools {
	if deps.MaxWidth <= 0 {
		deps.MaxWidth = DefaultMaxWidth
	}
	if deps.MaxMediaBytes <= 0 {
		deps.MaxMediaBytes = DefaultMaxMediaBytes
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Tools{deps: deps, ops: defaultPrimitives(Current())}
}

// Register 注册整个工具族（注册面缺失时静默返回：装配层负责显式装配）。
//
// 逐个工具按 Desktop 的能力声明决定去留：能力做不到的工具**不注册**——例如
// X11 没有 UI Automation 的 ScrollPattern，Linux 上就没有 computer_scroll_targets，
// 而不是挂一个每次调用都必然失败的摆设。
func (t *Tools) Register() {
	if t == nil || t.deps.RegisterTool == nil {
		return
	}
	register := t.deps.RegisterTool
	caps := t.ops.capabilities()
	register(ToolScreenshot, "Capture the desktop (or a region) and hand the picture to your next request so you can see the screen. Returns the stored media ref, region, scale, cursor and foreground window. Coordinates are virtual-desktop physical pixels.", screenshotSchema(), t.screenshot)
	register(ToolWindows, "List top-level desktop windows (title, handle, rect, visible, minimized) plus the virtual desktop size and the current foreground window. Use it to orient before clicking.", windowsSchema(), t.windows)
	if caps.ScrollTargets {
		register(ToolScrollTargets, "List the panels inside a window that can be scrolled with the wheel: each entry carries a readable name, control type, rect, center point, and the current vertical/horizontal scroll position and viewport ratio. Use it to find where the off-screen context lives (a page body, a side list, a chat history) before calling computer_scroll, and to tell how much content is left above/below.", scrollTargetsSchema(), t.scrollTargets)
	}
	register(ToolFocus, "Bring a top-level window to the foreground by title substring (case-insensitive). Empty match reports the current foreground window without switching.", focusSchema(), t.focus)
	register(ToolClick, "Click the desktop: give x/y, or give window (title substring) to click its center. button is left|right|middle; clicks is 1-3.", clickSchema(), t.click)
	register(ToolMove, "Move the mouse pointer to a point without clicking (hover, tooltip, or drag staging).", moveSchema(), t.move)
	register(ToolDrag, "Press the left button at from, drag to to, then release. Use for selecting ranges and moving windows.", dragSchema(), t.drag)
	register(ToolScroll, "Scroll the wheel: give x/y (or give window to scroll that window's largest scrollable panel, or omit both to scroll under the cursor). delta is in WHEEL_DELTA units (120 per notch); positive scrolls up. The result reports the panel under the point and its position before/after, so you know whether content moved and whether you reached the top/bottom.", scrollSchema(), t.scroll)
	register(ToolType, "Type literal text into the focused control (UTF-16 injection: CJK and emoji supported). Focus the target field first.", typeSchema(), t.typeText)
	register(ToolKeys, "Press a key combination such as ctrl+shift+t or enter, optionally repeated (1-10).", keysSchema(), t.keys)
	register(ToolWait, "Wait a bounded number of milliseconds so the UI can settle after an input, before taking the next screenshot.", waitSchema(), t.wait)
}

// decodeArgs 解析工具入参；空串视为「无参数」（全零值）。
func decodeArgs(argsJSON string, target any) error {
	if strings.TrimSpace(argsJSON) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(argsJSON), target); err != nil {
		return fmt.Errorf("bad arguments: %w", err)
	}
	return nil
}

// encodeResult 把工具结果编码为紧凑 JSON（模型可读，字段名稳定）。
func encodeResult(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}
	return string(encoded), nil
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// rectJSON / pointJSON 是工具结果里的几何投影（避免把域类型直接铺进契约）。
type rectJSON struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type pointJSON struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func newRectJSON(rect Rect) rectJSON {
	return rectJSON{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

func newPointJSON(point Point) pointJSON {
	return pointJSON{X: point.X, Y: point.Y}
}

// windowJSON 是窗口观测结果的投影；handle 用十六进制串（日志里可读）。
type windowJSON struct {
	Handle    string   `json:"handle"`
	Title     string   `json:"title"`
	Rect      rectJSON `json:"rect"`
	Visible   bool     `json:"visible"`
	Minimized bool     `json:"minimized"`
}

func newWindowJSON(win Window) windowJSON {
	return windowJSON{
		Handle:    fmt.Sprintf("0x%X", win.Handle),
		Title:     win.Title,
		Rect:      newRectJSON(win.Rect),
		Visible:   win.Visible,
		Minimized: win.Minimized,
	}
}

// storeScreenshot 把 PNG 落进会话媒体分区：没有落点就显式失败，不做无引用的
// 「临时截图」（模型看不到、界面也溯源不到，等于白截）。
func (t *Tools) storeScreenshot(ctx context.Context, name string, data []byte, width, height int, scale float64) (StoredMedia, error) {
	if t.deps.StoreMedia == nil {
		return StoredMedia{}, fmt.Errorf("computer: 当前装配没有会话媒体分区，截屏无法落盘")
	}
	if len(data) > t.deps.MaxMediaBytes {
		return StoredMedia{}, fmt.Errorf("computer: 截图 %d 字节超出上限 %d，请降低 max_width 后重试", len(data), t.deps.MaxMediaBytes)
	}
	stored, err := t.deps.StoreMedia(ctx, MediaAsset{
		Name: name, MimeType: "image/png", Kind: "screenshot",
		Data: data, Width: width, Height: height, Scale: scale,
	})
	if err != nil {
		return StoredMedia{}, fmt.Errorf("computer: 落盘截图失败: %w", err)
	}
	return stored, nil
}
