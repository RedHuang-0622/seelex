//go:build windows

package computer

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procEnumWindows              = user32DLL.NewProc("EnumWindows")
	procGetWindowTextW           = user32DLL.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32DLL.NewProc("GetWindowTextLengthW")
	procGetWindowRect            = user32DLL.NewProc("GetWindowRect")
	procIsWindowVisible          = user32DLL.NewProc("IsWindowVisible")
	procIsIconic                 = user32DLL.NewProc("IsIconic")
	procSetForegroundWindow      = user32DLL.NewProc("SetForegroundWindow")
	procShowWindow               = user32DLL.NewProc("ShowWindow")
	procBringWindowToTop         = user32DLL.NewProc("BringWindowToTop")
	procGetWindowThreadProcessID = user32DLL.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32DLL.NewProc("AttachThreadInput")
	procGetCurrentThreadID       = kernel32DLL.NewProc("GetCurrentThreadId")
	procGetForegroundWindow      = user32DLL.NewProc("GetForegroundWindow")
)

const (
	swRestore = 9
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func windowTitle(handle uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(handle)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	copied, _, _ := procGetWindowTextW.Call(handle, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if copied == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:copied])
}

func windowRect(handle uintptr) (Rect, bool) {
	var r winRect
	ret, _, _ := procGetWindowRect.Call(handle, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return Rect{}, false
	}
	return Rect{
		X:      int(r.Left),
		Y:      int(r.Top),
		Width:  int(r.Right - r.Left),
		Height: int(r.Bottom - r.Top),
	}, true
}

func describeWindow(handle uintptr) Window {
	visible, _, _ := procIsWindowVisible.Call(handle)
	iconic, _, _ := procIsIconic.Call(handle)
	win := Window{
		Handle:    handle,
		Title:     windowTitle(handle),
		Visible:   visible != 0,
		Minimized: iconic != 0,
	}
	if rect, ok := windowRect(handle); ok {
		win.Rect = rect
	}
	return win
}

// ListWindows 枚举有标题的顶层窗口。
func ListWindows() ([]Window, error) {
	windows := make([]Window, 0, 32)
	callback := syscall.NewCallback(func(handle uintptr, _ uintptr) uintptr {
		win := describeWindow(handle)
		if win.Title == "" {
			return 1
		}
		windows = append(windows, win)
		return 1
	})
	ret, _, err := procEnumWindows.Call(callback, 0)
	if ret == 0 {
		return nil, fmt.Errorf("computer: EnumWindows 失败: %v", err)
	}
	return windows, nil
}

// ForegroundWindow 返回当前前台窗口。
func ForegroundWindow() (Window, error) {
	handle, _, err := procGetForegroundWindow.Call()
	if handle == 0 {
		return Window{}, fmt.Errorf("computer: 无法获取前台窗口: %v", err)
	}
	return describeWindow(handle), nil
}

// FocusWindow 按标题子串（大小写不敏感）查找窗口并置于前台。
// match 为空时返回当前前台窗口而不做切换。返回被激活的窗口信息。
func FocusWindow(match string) (Window, error) {
	if strings.TrimSpace(match) == "" {
		return ForegroundWindow()
	}
	windows, err := ListWindows()
	if err != nil {
		return Window{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(match))
	var target *Window
	for i := range windows {
		win := windows[i]
		if !win.Visible {
			continue
		}
		if needle == "" || strings.Contains(strings.ToLower(win.Title), needle) {
			candidate := win
			target = &candidate
			if !win.Minimized {
				break
			}
		}
	}
	if target == nil {
		return Window{}, fmt.Errorf("computer: 未找到标题包含 %q 的可见窗口", match)
	}
	if target.Minimized {
		procShowWindow.Call(target.Handle, swRestore)
	}

	targetThread, _, _ := procGetWindowThreadProcessID.Call(target.Handle, 0)
	currentThread, _, _ := procGetCurrentThreadID.Call()
	attached := false
	if targetThread != 0 && targetThread != currentThread {
		ret, _, _ := procAttachThreadInput.Call(currentThread, targetThread, 1)
		attached = ret != 0
	}
	procBringWindowToTop.Call(target.Handle)
	procSetForegroundWindow.Call(target.Handle)
	if attached {
		procAttachThreadInput.Call(currentThread, targetThread, 0)
	}

	Sleep(120)
	activated := describeWindow(target.Handle)
	return activated, nil
}
