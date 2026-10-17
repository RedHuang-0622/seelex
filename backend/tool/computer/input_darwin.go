//go:build darwin

package computer

import (
	"fmt"
	"time"
	"unicode/utf16"
)

// 本文件是 macOS 的输入注入面：鼠标（移动 / 点击 / 拖拽 / 滚轮）与键盘
// （字面文本与组合键）。全部走 CoreGraphics 的 CGEvent 通道，语义与
// Windows / Linux 两端对齐：定位后留稳定期、滚轮按格下发、按下与释放成对。

const (
	// 与 Windows/X11 侧同一组节奏（见 input_linux.go 的同名常量）。
	darwinPointerSettleMS  = 25
	darwinScrollNotchGapMS = 15
	darwinScrollSettleMS   = 120
)

// darwinMouseButton 是一组鼠标按键对应的 CGEvent 类型与按钮号。
type darwinMouseButton struct {
	down    uint32
	up      uint32
	dragged uint32
	button  uint32
}

// darwinMouseButtonFor 把工具层的按键名映射成 CGEvent 的按键三元组。
func darwinMouseButtonFor(name string) (darwinMouseButton, error) {
	switch name {
	case "", "left":
		return darwinMouseButton{kCGEventLeftMouseDown, kCGEventLeftMouseUp, kCGEventLeftMouseDragged, kCGMouseButtonLeft}, nil
	case "right":
		return darwinMouseButton{kCGEventRightMouseDown, kCGEventRightMouseUp, kCGEventRightMouseDragged, kCGMouseButtonRight}, nil
	case "middle":
		return darwinMouseButton{kCGEventOtherMouseDown, kCGEventOtherMouseUp, kCGEventOtherMouseDragged, kCGMouseButtonCenter}, nil
	default:
		return darwinMouseButton{}, fmt.Errorf("computer: 未知鼠标按键 %q", name)
	}
}

// darwinPostMouse 注入一次鼠标事件（失败只能报告"构造失败"：CGEventPost 无返回，
// 系统级投放失败是静默的，见本文件顶部注释与 README 的授权说明）。
func darwinPostMouse(eventType uint32, point Point, button uint32) error {
	event := cgEventCreateMouseEvent(0, eventType, cgPoint{X: float64(point.X), Y: float64(point.Y)}, button)
	if event == 0 {
		return fmt.Errorf("computer: 构造鼠标事件失败")
	}
	defer cfRelease(event)
	cgEventPost(kCGHIDEventTap, event)
	return nil
}

// darwinWarpPointer 把指针移动到指定坐标，并保持指针与鼠标设备关联
// （CGWarpMouseCursorPosition 会短暂断开关联，不恢复会让后续相对移动失真）。
func darwinWarpPointer(point Point) error {
	if status := cgWarpMouseCursorPosition(cgPoint{X: float64(point.X), Y: float64(point.Y)}); status != 0 {
		return fmt.Errorf("computer: CGWarpMouseCursorPosition 失败（CGError=%d）", status)
	}
	if cgAssociateMouse != nil {
		_ = cgAssociateMouse(true)
	}
	return nil
}

// MoveMouse 把指针移动到指定坐标。
func (darwinDesktop) MoveMouse(point Point) error {
	if err := darwinProbe(); err != nil {
		return err
	}
	if err := darwinWarpPointer(point); err != nil {
		return err
	}
	Sleep(darwinPointerSettleMS)
	return nil
}

// Click 把指针移动到指定坐标并点击（按下与释放成对，失败也先补释放）。
func (d darwinDesktop) Click(point Point, opts ClickOptions) error {
	button, err := darwinMouseButtonFor(opts.Button)
	if err != nil {
		return err
	}
	clicks := opts.Clicks
	if clicks < 1 {
		clicks = 1
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultClickInterval
	}
	if err := d.MoveMouse(point); err != nil {
		return err
	}
	// 复用与另两个平台同一套点击序列语义（click.go）。
	inject := func(eventType uint32, _, _ uintptr, _ uint32) error {
		return darwinPostMouse(eventType, point, button.button)
	}
	sleep := func(delay time.Duration) { Sleep(int(delay / time.Millisecond)) }
	return runClickSequence(inject, button.down, button.up, 0, 0, clicks, interval, sleep)
}

// Drag 按住左键从 from 拖到 to 再释放（中途失败也必须释放）。
func (d darwinDesktop) Drag(from, to Point, duration time.Duration) error {
	button, err := darwinMouseButtonFor("left")
	if err != nil {
		return err
	}
	if err := d.MoveMouse(from); err != nil {
		return err
	}
	if err := darwinPostMouse(button.down, from, button.button); err != nil {
		_ = darwinPostMouse(button.up, from, button.button)
		return err
	}
	steps := 12
	if duration >= 200*time.Millisecond {
		steps = 24
	}
	gap := duration / time.Duration(steps)
	for index := 1; index <= steps; index++ {
		ratio := float64(index) / float64(steps)
		point := Point{
			X: from.X + int(float64(to.X-from.X)*ratio),
			Y: from.Y + int(float64(to.Y-from.Y)*ratio),
		}
		if err := darwinPostMouse(button.dragged, point, button.button); err != nil {
			_ = darwinPostMouse(button.up, point, button.button)
			return err
		}
		Sleep(int(gap / time.Millisecond))
	}
	return darwinPostMouse(button.up, to, button.button)
}

// Scroll 在指定坐标滚动滚轮：delta 为 WHEEL_DELTA(120) 的倍数，正数向上。
// CGEvent 的行单位里正数即向上，与工具层约定一致，因此符号原样透传。
func (d darwinDesktop) Scroll(point Point, delta int) error {
	if delta == 0 {
		return fmt.Errorf("computer: scroll 需要非零 delta")
	}
	if err := d.MoveMouse(point); err != nil {
		return err
	}
	notches := scrollNotches(delta)
	if len(notches) == 0 {
		return fmt.Errorf("computer: scroll delta=%d 无法拆成滚轮事件", delta)
	}
	for _, notch := range notches {
		lines := int32(notch / wheelDelta)
		if lines == 0 {
			// 不足一格的余数：按符号给一格，避免"发出去了但为 0"。
			if notch > 0 {
				lines = 1
			} else {
				lines = -1
			}
		}
		event := cgEventCreateScrollWheel(0, kCGScrollEventUnitLine, 1, lines, 0, 0)
		if event == 0 {
			return fmt.Errorf("computer: 构造滚轮事件失败")
		}
		cgEventPost(kCGHIDEventTap, event)
		cfRelease(event)
		Sleep(darwinScrollNotchGapMS)
	}
	Sleep(darwinScrollSettleMS - darwinScrollNotchGapMS)
	return nil
}

// darwinPostUnicode 以「虚拟键 0 + Unicode 字符串」注入一个 UTF-16 单元序列。
func darwinPostUnicode(units []uint16) error {
	if len(units) == 0 {
		return nil
	}
	down := cgEventCreateKeyboard(0, 0, true)
	if down == 0 {
		return fmt.Errorf("computer: 构造键盘事件失败")
	}
	cgEventKeyboardSetUnicode(down, uintptr(len(units)), &units[0])
	cgEventPost(kCGHIDEventTap, down)
	cfRelease(down)
	up := cgEventCreateKeyboard(0, 0, false)
	if up == 0 {
		return fmt.Errorf("computer: 构造键盘事件失败")
	}
	cgEventPost(kCGHIDEventTap, up)
	cfRelease(up)
	return nil
}

// TypeText 逐字注入文本（支持中文与 emoji）。
// macOS 没有 X11 那种"临时重映射键码"的负担：CGEventKeyboardSetUnicodeString
// 直接把任意 UTF-16 序列送给前台应用，因此这里按码点逐个送即可。
func (darwinDesktop) TypeText(text string) error {
	if text == "" {
		return nil
	}
	if err := darwinProbe(); err != nil {
		return err
	}
	for _, char := range text {
		if err := darwinPostUnicode(utf16.Encode([]rune{char})); err != nil {
			return err
		}
	}
	return nil
}

// darwinModifierFlags 把 keys.go 解析出的修饰键虚拟码折成 CGEventFlags。
func darwinModifierFlags(modifiers []uint16) (uint64, error) {
	var flags uint64
	for _, vk := range modifiers {
		switch vk {
		case VKLShift:
			flags |= kCGEventFlagMaskShift
		case VKControl:
			flags |= kCGEventFlagMaskControl
		case VKMenu:
			flags |= kCGEventFlagMaskAlternate
		case VKLWin:
			flags |= kCGEventFlagMaskCommand
		default:
			return 0, fmt.Errorf("computer: 无法映射修饰键 0x%X", vk)
		}
	}
	return flags, nil
}

// darwinKeyCodes 是虚拟键码 → macOS 虚拟键码（ANSI 布局）的映射。
// 字母统一取小写键位：大写由 Shift 修饰表达，组合键（ctrl+shift+t）才成立。
var darwinKeyCodes = map[uint16]uint16{
	'A': 0, 'S': 1, 'D': 2, 'F': 3, 'H': 4, 'G': 5, 'Z': 6, 'X': 7, 'C': 8, 'V': 9,
	'B': 11, 'Q': 12, 'W': 13, 'E': 14, 'R': 15, 'Y': 16, 'T': 17,
	'1': 18, '2': 19, '3': 20, '4': 21, '6': 22, '5': 23, '9': 25, '7': 26, '8': 28, '0': 29,
	'O': 31, 'U': 32, 'I': 34, 'P': 35, 'L': 37, 'J': 38, 'K': 40, 'N': 45, 'M': 46,
	VKReturn: 36, VKTab: 48, VKSpace: 49, VKBack: 51, VKEscape: 53, VKDelete: 117,
	VKHome: 115, VKEnd: 119, VKPrior: 116, VKNext: 121,
	VKLeft: 123, VKRight: 124, VKDown: 125, VKUp: 126, VKInsert: 114, VKCapsLock: 57,
}

// darwinFunctionKeys 是 F1..F20 的 macOS 键码（GB 键盘无 F21..F24）。
var darwinFunctionKeys = [...]uint16{
	122, 120, 99, 118, 96, 97, 98, 100, 101, 109,
	103, 111, 105, 107, 113, 106, 64, 79, 80, 90,
}

// darwinVirtualKeyToCode 把虚拟键码映射成 macOS 虚拟键码。
func darwinVirtualKeyToCode(vk uint16) (uint16, bool) {
	if vk >= 0x70 && vk <= 0x83 { // F1..F20
		return darwinFunctionKeys[vk-0x70], true
	}
	code, ok := darwinKeyCodes[vk]
	return code, ok
}

// darwinPostKey 注入一次带修饰标志的按键。
func darwinPostKey(code uint16, flags uint64, down bool) error {
	event := cgEventCreateKeyboard(0, code, down)
	if event == 0 {
		return fmt.Errorf("computer: 构造键盘事件失败")
	}
	defer cfRelease(event)
	if flags != 0 {
		cgEventSetFlags(event, flags)
	}
	cgEventPost(kCGHIDEventTap, event)
	return nil
}

// PressKeys 按下组合键，times 为重复次数（0 视为 1）。
// 顺序与另两个平台一致：修饰键按下 → 主键 → 修饰键逆序释放；macOS 侧用
// CGEventFlags 表达修饰，而不是逐键注入修饰键事件。
func (darwinDesktop) PressKeys(combo string, times int) error {
	parsed, err := ParseKeyCombo(combo)
	if err != nil {
		return err
	}
	if times < 1 {
		times = 1
	}
	if err := darwinProbe(); err != nil {
		return err
	}
	flags, err := darwinModifierFlags(parsed.Modifiers)
	if err != nil {
		return err
	}
	code, ok := darwinVirtualKeyToCode(parsed.Key)
	if !ok {
		return fmt.Errorf("computer: 无法把按键组合 %q 映射到 macOS 键码", combo)
	}
	for repeat := 0; repeat < times; repeat++ {
		if err := darwinPostKey(code, flags, true); err != nil {
			return err
		}
		if err := darwinPostKey(code, flags, false); err != nil {
			return err
		}
	}
	return nil
}
