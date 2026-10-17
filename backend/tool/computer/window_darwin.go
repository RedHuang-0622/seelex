//go:build darwin

package computer

import (
	"fmt"
	"strings"
)

// 本文件是 macOS 的窗口面：枚举顶层窗口、读前台窗口、按标题置前窗口。
//
// 枚举走 CGWindowListCopyWindowInfo（CoreGraphics，不需要授权即可读到窗口
// 几何与标题）；置前必须走 Accessibility API（AXUIElement），因为 CoreGraphics
// 只能"看"窗口，不能"抬"窗口。

// darwinWindowRecord 是一条窗口观测记录。
type darwinWindowRecord struct {
	handle   uintptr
	title    string
	pid      int32
	rect     Rect
	onscreen bool
	layer    int
}

// darwinListWindowRecords 枚举屏幕上的普通顶层窗口（layer 0）。
//
// 只取 on-screen 的窗口：CGWindowList 的 OptionAll 会把其它 Space、被最小化的
// 窗口一并列出，标题与几何都还在缓存里，用来"点窗口中心"会系统性点错。
func darwinListWindowRecords() ([]darwinWindowRecord, error) {
	if err := darwinProbe(); err != nil {
		return nil, err
	}
	array := cgWindowListCopyWindowInfo(cgWindowListOptionOnScreenOnly|cgWindowListExcludeDesktopElements, cgNullWindowID)
	if array == 0 {
		return nil, fmt.Errorf("computer: 枚举窗口失败（CGWindowListCopyWindowInfo 返回空）")
	}
	defer cfRelease(array)
	count := cfArrayGetCount(array)
	records := make([]darwinWindowRecord, 0, count)
	for index := int64(0); index < count; index++ {
		dict := cfArrayGetValueAtIndex(array, index)
		if dict == 0 {
			continue
		}
		record := darwinReadWindowRecord(dict)
		// 与 Windows/X11 同口径：没有标题的、非普通层级的（菜单栏、Dock、
		// 悬浮工具条等）不进列表。
		if record.title == "" || record.layer != 0 {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// darwinReadWindowRecord 从一个窗口信息字典里读字段。
func darwinReadWindowRecord(dict uintptr) darwinWindowRecord {
	var record darwinWindowRecord
	if number, ok := darwinCFNumber(darwinDictionaryValue(dict, cgWindowKeyNumber)); ok {
		record.handle = uintptr(number)
	}
	if pid, ok := darwinCFNumber(darwinDictionaryValue(dict, cgWindowKeyOwnerPID)); ok {
		record.pid = int32(pid)
	}
	if layer, ok := darwinCFNumber(darwinDictionaryValue(dict, cgWindowKeyLayer)); ok {
		record.layer = int(layer)
	}
	record.title = darwinCFStringValue(darwinDictionaryValue(dict, cgWindowKeyName))
	if onscreen, ok := darwinCFBool(darwinDictionaryValue(dict, cgWindowKeyOnscreen)); ok {
		record.onscreen = onscreen
	}
	if bounds := darwinDictionaryValue(dict, cgWindowKeyBounds); bounds != 0 && cgRectMakeWithDictionary != nil {
		var rect cgRect
		if cgRectMakeWithDictionary(bounds, &rect) {
			record.rect = Rect{
				X:      int(rect.Origin.X),
				Y:      int(rect.Origin.Y),
				Width:  int(rect.Size.Width),
				Height: int(rect.Size.Height),
			}
		}
	}
	return record
}

// ListWindows 枚举有标题的顶层窗口。
func (darwinDesktop) ListWindows() ([]Window, error) {
	records, err := darwinListWindowRecords()
	if err != nil {
		return nil, err
	}
	windows := make([]Window, 0, len(records))
	for _, record := range records {
		windows = append(windows, Window{
			Handle:    record.handle,
			Title:     record.title,
			Rect:      record.rect,
			Visible:   record.onscreen,
			Minimized: !record.onscreen,
		})
	}
	return windows, nil
}

// ForegroundWindow 返回当前前台窗口。
// CGWindowList 按"从前到后"排序，因此 layer 0 的第一条就是最前面的普通窗口。
func (darwinDesktop) ForegroundWindow() (Window, error) {
	records, err := darwinListWindowRecords()
	if err != nil {
		return Window{}, err
	}
	if len(records) == 0 {
		return Window{}, fmt.Errorf("computer: 当前没有前台窗口")
	}
	record := records[0]
	return Window{
		Handle:    record.handle,
		Title:     record.title,
		Rect:      record.rect,
		Visible:   record.onscreen,
		Minimized: !record.onscreen,
	}, nil
}

// FocusWindow 按标题子串（大小写不敏感）查找窗口并置于前台。
// match 为空时只报告当前前台窗口而不做切换（与 Windows/X11 一致）。
func (d darwinDesktop) FocusWindow(match string) (Window, error) {
	if strings.TrimSpace(match) == "" {
		return d.ForegroundWindow()
	}
	records, err := darwinListWindowRecords()
	if err != nil {
		return Window{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(match))
	var target *darwinWindowRecord
	for index := range records {
		if strings.Contains(strings.ToLower(records[index].title), needle) {
			candidate := records[index]
			target = &candidate
			break
		}
	}
	if target == nil {
		return Window{}, fmt.Errorf("computer: 未找到标题包含 %q 的可见窗口", match)
	}
	if err := darwinRaiseWindow(target.pid, target.title); err != nil {
		return Window{}, err
	}
	Sleep(120)
	if foreground, err := d.ForegroundWindow(); err == nil {
		return foreground, nil
	}
	return Window{Handle: target.handle, Title: target.title, Rect: target.rect, Visible: true}, nil
}

// darwinRaiseWindow 用 Accessibility API 把目标应用的匹配窗口置前。
//
// macOS 没有"抬某个窗口"的 CoreGraphics 入口，AX 是唯一正路；AX 需要用户在
// 「隐私与安全性 › 辅助功能」里授权，未授权时显式报错而不是静默失败。
func darwinRaiseWindow(pid int32, title string) error {
	if axUIElementCreateApp == nil || axUIElementCopyAttribute == nil || axUIElementPerformAction == nil {
		return fmt.Errorf("computer: 当前系统缺少 Accessibility API，无法把窗口置前")
	}
	if axIsProcessTrusted != nil && !axIsProcessTrusted() {
		return fmt.Errorf("computer: 未授予「辅助功能」权限（系统设置 › 隐私与安全性 › 辅助功能），无法把窗口置前")
	}
	app := axUIElementCreateApp(pid)
	if app == 0 {
		return fmt.Errorf("computer: 创建 AX 应用元素失败（pid=%d）", pid)
	}
	defer cfRelease(app)

	windowsRef, err := darwinAXCopy(app, axWindowsAttribute)
	if err != nil {
		return err
	}
	if windowsRef == 0 {
		return fmt.Errorf("computer: 目标应用没有窗口")
	}
	defer cfRelease(windowsRef)

	count := cfArrayGetCount(windowsRef)
	var chosen uintptr
	for index := int64(0); index < count; index++ {
		element := cfArrayGetValueAtIndex(windowsRef, index)
		if element == 0 {
			continue
		}
		elementTitle, err := darwinAXCopy(element, axTitleAttribute)
		if err != nil || elementTitle == 0 {
			continue
		}
		matched := strings.Contains(strings.ToLower(darwinCFStringValue(elementTitle)), strings.ToLower(title))
		cfRelease(elementTitle)
		if matched {
			chosen = element
			break
		}
	}
	if chosen == 0 && count > 0 {
		// 标题对不上（AX 标题与 CG 标题口径常有差异）时退回目标应用的最前窗口。
		chosen = cfArrayGetValueAtIndex(windowsRef, 0)
	}
	if chosen == 0 {
		return fmt.Errorf("computer: 目标应用没有可置前的窗口")
	}

	action := darwinCFString(axRaiseAction)
	if action == 0 {
		return fmt.Errorf("computer: 构造 AX 动作名失败")
	}
	defer cfRelease(action)
	if status := axUIElementPerformAction(chosen, action); status != 0 {
		return fmt.Errorf("computer: 置前窗口失败（AXError=%d）", status)
	}
	return nil
}

// darwinAXCopy 按属性名取一个 AX 值（返回的是调用方拥有的 CFTypeRef）。
func darwinAXCopy(element uintptr, attribute string) (uintptr, error) {
	attr := darwinCFString(attribute)
	if attr == 0 {
		return 0, fmt.Errorf("computer: 构造 AX 属性名失败")
	}
	defer cfRelease(attr)
	var value uintptr
	if status := axUIElementCopyAttribute(element, attr, &value); status != 0 {
		return 0, fmt.Errorf("computer: 读取 AX 属性 %s 失败（AXError=%d）", attribute, status)
	}
	return value, nil
}
