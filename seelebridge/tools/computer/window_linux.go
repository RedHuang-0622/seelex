//go:build linux

package computer

import (
	"fmt"
	"strings"

	"github.com/jezek/xgb/xproto"
)

// 本文件是 X11 的窗口面：枚举顶层窗口、读前台窗口、按标题激活窗口。
// 全部走 EWMH（freedesktop 的窗口管理器约定）而不是猜某个 WM 的私有属性。

const (
	atomClientList     = "_NET_CLIENT_LIST"
	atomActiveWindow   = "_NET_ACTIVE_WINDOW"
	atomWmName         = "_NET_WM_NAME"
	atomWmNameLegacy   = "WM_NAME"
	atomWmState        = "_NET_WM_STATE"
	atomWmStateHidden  = "_NET_WM_STATE_HIDDEN"
	atomFrameExtents   = "_NET_FRAME_EXTENTS"
	maxClientWindowIDs = 4096
)

// describeWindow 读一个顶层窗口的标题、矩形与状态。
func (s *x11Session) describeWindow(window xproto.Window) Window {
	win := Window{Handle: uintptr(window), Title: s.windowTitle(window)}
	if attributes, err := xproto.GetWindowAttributes(s.conn, window).Reply(); err == nil {
		win.Visible = attributes.MapState == xproto.MapStateViewable
	}
	if states, err := s.atomListOf(window, atomWmState, 64); err == nil {
		if hidden, ok := s.atoms[atomWmStateHidden]; ok {
			for _, state := range states {
				if state == uint32(hidden) {
					win.Minimized = true
					break
				}
			}
		}
	}
	if rect, ok := s.windowRect(window); ok {
		win.Rect = rect
	}
	return win
}

// windowTitle 取窗口标题：优先 _NET_WM_NAME（UTF-8），退回 WM_NAME。
func (s *x11Session) windowTitle(window xproto.Window) string {
	for _, name := range []string{atomWmName, atomWmNameLegacy} {
		value, err := s.propertyOf(window, name, 1024)
		if err != nil || len(value) == 0 {
			continue
		}
		title := strings.TrimRight(string(value), "\x00")
		if strings.TrimSpace(title) != "" {
			return title
		}
	}
	return ""
}

// windowRect 用 GetGeometry + TranslateCoordinates 得到窗口在根窗口坐标系里的
// 矩形，再加上 _NET_FRAME_EXTENTS 描述的窗口装饰——与 Windows 的
// GetWindowRect 语义对齐：模型拿到的是"看得见的那一块"，不是没算标题栏的
// 客户区（否则点窗口中心会系统性偏上）。
func (s *x11Session) windowRect(window xproto.Window) (Rect, bool) {
	geometry, err := xproto.GetGeometry(s.conn, xproto.Drawable(window)).Reply()
	if err != nil {
		return Rect{}, false
	}
	translated, err := xproto.TranslateCoordinates(s.conn, window, s.root, 0, 0).Reply()
	if err != nil {
		return Rect{}, false
	}
	rect := Rect{
		X:      int(translated.DstX),
		Y:      int(translated.DstY),
		Width:  int(geometry.Width),
		Height: int(geometry.Height),
	}
	if extents, err := s.atomListOf(window, atomFrameExtents, 4); err == nil && len(extents) >= 4 {
		rect.X -= int(extents[0])
		rect.Y -= int(extents[2])
		rect.Width += int(extents[0] + extents[1])
		rect.Height += int(extents[2] + extents[3])
	}
	return rect, true
}

// clientWindows 列出顶层客户端窗口：优先 EWMH 的 _NET_CLIENT_LIST；没有窗口
// 管理器时退回根窗口的直接子窗口（此时标题多半取不到，窗口会被过滤掉）。
func (s *x11Session) clientWindows() ([]xproto.Window, error) {
	list, err := s.atomListOf(s.root, atomClientList, maxClientWindowIDs)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		windows := make([]xproto.Window, 0, len(list))
		for _, id := range list {
			windows = append(windows, xproto.Window(id))
		}
		return windows, nil
	}
	tree, err := xproto.QueryTree(s.conn, s.root).Reply()
	if err != nil {
		return nil, fmt.Errorf("computer: 查询 X11 窗口树失败: %w", err)
	}
	return tree.Children, nil
}

// ListWindows 枚举有标题的顶层窗口（与 Windows 侧同语义：标题为空的不列出，
// 可见性与最小化状态如实带回，交给调用方过滤）。
func (x11Desktop) ListWindows() ([]Window, error) {
	var windows []Window
	err := x11Use(func(s *x11Session) error {
		ids, err := s.clientWindows()
		if err != nil {
			return err
		}
		windows = make([]Window, 0, len(ids))
		for _, id := range ids {
			win := s.describeWindow(id)
			if win.Title == "" {
				continue
			}
			windows = append(windows, win)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return windows, nil
}

// ForegroundWindow 返回当前前台窗口（EWMH 的 _NET_ACTIVE_WINDOW）。
func (x11Desktop) ForegroundWindow() (Window, error) {
	var win Window
	err := x11Use(func(s *x11Session) error {
		active, err := s.activeWindow()
		if err != nil {
			return err
		}
		if active == 0 {
			return fmt.Errorf("computer: 当前没有前台窗口（_NET_ACTIVE_WINDOW 为空）")
		}
		win = s.describeWindow(active)
		return nil
	})
	if err != nil {
		return Window{}, err
	}
	return win, nil
}

func (s *x11Session) activeWindow() (xproto.Window, error) {
	list, err := s.atomListOf(s.root, atomActiveWindow, 1)
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, nil
	}
	return xproto.Window(list[0]), nil
}

// FocusWindow 按标题子串（大小写不敏感）查找可见窗口并置于前台。
// match 为空时只报告当前前台窗口而不做切换（与 Windows 侧一致）。
func (d x11Desktop) FocusWindow(match string) (Window, error) {
	if strings.TrimSpace(match) == "" {
		return d.ForegroundWindow()
	}
	windows, err := d.ListWindows()
	if err != nil {
		return Window{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(match))
	var target *Window
	for index := range windows {
		win := windows[index]
		if !win.Visible {
			continue
		}
		if !strings.Contains(strings.ToLower(win.Title), needle) {
			continue
		}
		candidate := win
		target = &candidate
		if !win.Minimized {
			break
		}
	}
	if target == nil {
		return Window{}, fmt.Errorf("computer: 未找到标题包含 %q 的可见窗口", match)
	}
	handle := xproto.Window(target.Handle)
	if err := x11Use(func(s *x11Session) error { return s.activateWindow(handle) }); err != nil {
		return Window{}, err
	}
	Sleep(120)
	var activated Window
	if err := x11Use(func(s *x11Session) error {
		activated = s.describeWindow(handle)
		return nil
	}); err != nil {
		return Window{}, err
	}
	return activated, nil
}

// activateWindow 用 EWMH 的 _NET_ACTIVE_WINDOW 客户端消息请窗口管理器激活窗口
// （多数 WM 处理该消息时会顺带把最小化的窗口还原），再发一次 Raise 与
// SetInputFocus 兜底：前者对合规 WM 生效，后者保证只有 SetInputFocus 一条路的
// 极简 WM 也能被驱动。
func (s *x11Session) activateWindow(window xproto.Window) error {
	activeAtom, err := s.atom(atomActiveWindow)
	if err != nil {
		return err
	}
	// 先取一次 _NET_WM_STATE_HIDDEN 原子，让 describeWindow 之后的状态读取
	// 有缓存可用（也就顺手确认了 WM 支持 EWMH 的这一项）。
	if _, err := s.atom(atomWmStateHidden); err != nil {
		return err
	}
	request := xproto.ClientMessageEvent{
		Format: 32,
		Window: window,
		Type:   activeAtom,
		Data: xproto.ClientMessageDataUnionData32New([]uint32{
			1, // source indication: 1 = 普通应用（2 = 分页器）
			xproto.TimeCurrentTime,
			0, 0, 0,
		}),
	}
	xproto.SendEvent(s.conn, false, s.root,
		xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(request.Bytes()))
	xproto.ConfigureWindow(s.conn, window, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove})
	xproto.SetInputFocus(s.conn, xproto.InputFocusPointerRoot, window, xproto.TimeCurrentTime)
	return nil
}
