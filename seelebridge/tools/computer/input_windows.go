//go:build windows

package computer

import (
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	procSetCursorPos = user32DLL.NewProc("SetCursorPos")
	procGetCursorPos = user32DLL.NewProc("GetCursorPos")
	procSendInput    = user32DLL.NewProc("SendInput")
)

const (
	mouseEventMove        uint32 = 0x0001
	mouseEventLeftDown    uint32 = 0x0002
	mouseEventLeftUp      uint32 = 0x0004
	mouseEventRightDown   uint32 = 0x0008
	mouseEventRightUp     uint32 = 0x0010
	mouseEventMiddleDown  uint32 = 0x0020
	mouseEventMiddleUp    uint32 = 0x0040
	mouseEventWheel       uint32 = 0x0800
	mouseEventAbsolute    uint32 = 0x8000
	mouseEventVirtualDesk uint32 = 0x4000

	// 定位与点击之间的等待，避免移动消息未处理就发出按键。
	pointerSettleMS = 25

	keyEventKeyUp   = 0x0002
	keyEventUnicode = 0x0004

	inputMouse    = 0
	inputKeyboard = 1
)

type cursorPoint struct {
	X int32
	Y int32
}

type keybdInput struct {
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
}

// mouseInput 对应 Win32 MOUSEINPUT：Extra 需要 8 字节对齐，Go 会自动在 Time
// 之后补 4 字节填充，整体 32 字节，与 x64 的 INPUT union 一致。
type mouseInput struct {
	X         int32
	Y         int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

// input 对应 Win32 INPUT：4 字节类型 + 4 字节对齐 + 32 字节 union（x64 共 40 字节）。
type input struct {
	Type uint32
	_    uint32
	Data [32]byte
}

func wrapProc(name string, err error) error {
	return fmt.Errorf("computer: %s 失败: %v", name, err)
}

// CursorPosition 返回当前光标位置（虚拟桌面坐标）。
func CursorPosition() (Point, error) {
	var p cursorPoint
	ret, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if ret == 0 {
		return Point{}, wrapProc("GetCursorPos", err)
	}
	return Point{X: int(p.X), Y: int(p.Y)}, nil
}

// MoveMouse 把光标移动到指定坐标。
func MoveMouse(p Point) error {
	if err := setCursor(p); err != nil {
		return err
	}
	Sleep(pointerSettleMS)
	return nil
}

func setCursor(p Point) error {
	ret, _, err := procSetCursorPos.Call(uintptr(int32(p.X)), uintptr(int32(p.Y)))
	if ret == 0 {
		return wrapProc("SetCursorPos", err)
	}
	return nil
}

// absolutePoint 把虚拟桌面坐标归一化为 0..65535，供 MOUSEEVENTF_ABSOLUTE 使用。
func absolutePoint(p Point) (uintptr, uintptr) {
	screen, err := VirtualScreen()
	if err != nil || screen.Width <= 1 || screen.Height <= 1 {
		return uintptr(int32(p.X)), uintptr(int32(p.Y))
	}
	nx := (p.X - screen.X) * 65535 / (screen.Width - 1)
	ny := (p.Y - screen.Y) * 65535 / (screen.Height - 1)
	return uintptr(int32(nx)), uintptr(int32(ny))
}

func mouseButtonFlags(button string) (down, up uint32, err error) {
	switch button {
	case "", "left":
		return mouseEventLeftDown, mouseEventLeftUp, nil
	case "right":
		return mouseEventRightDown, mouseEventRightUp, nil
	case "middle":
		return mouseEventMiddleDown, mouseEventMiddleUp, nil
	default:
		return 0, 0, fmt.Errorf("computer: 未知鼠标按键 %q", button)
	}
}

// mouseInputEvent 构造一次绝对坐标的鼠标输入事件（虚拟桌面坐标系）。
func mouseInputEvent(flags uint32, x, y uintptr, data uint32) input {
	var event input
	event.Type = inputMouse
	*(*mouseInput)(unsafe.Pointer(&event.Data[0])) = mouseInput{
		X:         int32(x),
		Y:         int32(y),
		MouseData: data,
		Flags:     flags | mouseEventAbsolute | mouseEventVirtualDesk,
	}
	return event
}

// sendMouse 注入一次鼠标事件。走 SendInput 而不是 mouse_event：SendInput
// 返回真正插入队列的事件数，调用方因此能判断成功/失败；mouse_event 返回
// void，把它当失败判断会让每一次点击都「假失败」并跳过释放。
func sendMouse(flags uint32, x, y uintptr, data uint32) error {
	return sendInputs([]input{mouseInputEvent(flags, x, y, data)})
}

// Click 把光标移动到指定坐标并点击（按下与释放成对，失败也先补释放）。
func Click(p Point, opts ClickOptions) error {
	down, up, err := mouseButtonFlags(opts.Button)
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
	if err := MoveMouse(p); err != nil {
		return err
	}
	nx, ny := absolutePoint(p)
	return runClickSequence(sendMouse, down, up, nx, ny, clicks, interval, func(d time.Duration) {
		Sleep(int(d / time.Millisecond))
	})
}

// Drag 按住左键从 from 拖到 to，用于框选、拖拽窗口等。中途失败也必须释放，
// 否则桌面停在按住状态。
func Drag(from, to Point, duration time.Duration) error {
	if err := MoveMouse(from); err != nil {
		return err
	}
	nx, ny := absolutePoint(from)
	if err := sendMouse(mouseEventLeftDown, nx, ny, 0); err != nil {
		_ = sendMouse(mouseEventLeftUp, nx, ny, 0)
		return err
	}
	steps := 12
	if duration >= 200*time.Millisecond {
		steps = 24
	}
	moveErr := error(nil)
	for i := 1; i <= steps; i++ {
		ratio := float64(i) / float64(steps)
		x := from.X + int(float64(to.X-from.X)*ratio)
		y := from.Y + int(float64(to.Y-from.Y)*ratio)
		if err := MoveMouse(Point{X: x, Y: y}); err != nil {
			moveErr = err
			break
		}
		Sleep(int(duration / time.Duration(steps) / time.Millisecond))
	}
	nx, ny = absolutePoint(to)
	if err := sendMouse(mouseEventLeftUp, nx, ny, 0); err != nil {
		return err
	}
	return moveErr
}

// Scroll 在指定坐标滚动滚轮，delta 为 WHEEL_DELTA(120) 的倍数，正数向上。
func Scroll(p Point, delta int) error {
	if delta == 0 {
		return fmt.Errorf("computer: scroll 需要非零 delta")
	}
	if err := MoveMouse(p); err != nil {
		return err
	}
	nx, ny := absolutePoint(p)
	return sendMouse(mouseEventWheel, nx, ny, uint32(int32(delta)))
}

func sendInputs(inputs []input) error {
	if len(inputs) == 0 {
		return nil
	}
	ret, _, err := procSendInput.Call(
		uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(input{}),
	)
	if int(ret) != len(inputs) {
		return fmt.Errorf("computer: SendInput 只注入 %d/%d 个事件: %v", int(ret), len(inputs), err)
	}
	return nil
}

func keyInput(vk uint16, up bool) input {
	var in input
	in.Type = inputKeyboard
	kb := keybdInput{Vk: vk}
	if up {
		kb.Flags |= keyEventKeyUp
	}
	*(*keybdInput)(unsafe.Pointer(&in.Data[0])) = kb
	return in
}

func unicodeInput(unit uint16, up bool) input {
	var in input
	in.Type = inputKeyboard
	kb := keybdInput{Scan: unit, Flags: keyEventUnicode}
	if up {
		kb.Flags |= keyEventKeyUp
	}
	*(*keybdInput)(unsafe.Pointer(&in.Data[0])) = kb
	return in
}

// TypeText 以 Unicode 方式输入文本（支持中文与 emoji）。
func TypeText(text string) error {
	if text == "" {
		return nil
	}
	units := utf16.Encode([]rune(text))
	batch := make([]input, 0, 128)
	for _, unit := range units {
		batch = append(batch, unicodeInput(unit, false), unicodeInput(unit, true))
		if len(batch) >= 128 {
			if err := sendInputs(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	return sendInputs(batch)
}

// PressKeys 按下组合键，times 为重复次数（0 视为 1）。
func PressKeys(combo string, times int) error {
	parsed, err := ParseKeyCombo(combo)
	if err != nil {
		return err
	}
	if times < 1 {
		times = 1
	}
	batch := make([]input, 0, (len(parsed.Modifiers)*2+2)*times)
	for i := 0; i < times; i++ {
		for _, mod := range parsed.Modifiers {
			batch = append(batch, keyInput(mod, false))
		}
		batch = append(batch, keyInput(parsed.Key, false), keyInput(parsed.Key, true))
		for j := len(parsed.Modifiers) - 1; j >= 0; j-- {
			batch = append(batch, keyInput(parsed.Modifiers[j], true))
		}
	}
	return sendInputs(batch)
}
