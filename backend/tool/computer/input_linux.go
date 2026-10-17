//go:build linux

package computer

import (
	"fmt"
	"time"

	"github.com/jezek/xgb/xproto"
)

// 本文件是 X11 的输入注入面：鼠标（移动/点击/拖拽/滚轮）与键盘（字面文本与
// 组合键）。全部通过 XTEST 扩展（xtest.FakeInputChecked）下发，语义与 Windows
// 侧对齐：定位后留稳定期、滚轮按格下发、按下与释放成对。

const (
	// 与 Windows 侧同一组节奏：定位与点击之间的等待、相邻滚轮事件间隔、
	// 滚轮之后给界面跟上的时间（见 input_windows.go 的同名常量）。
	pointerSettleMS  = 25
	scrollNotchGapMS = 15
	scrollSettleMS   = 120

	// X11 鼠标键号约定：1 左、2 中、3 右；4/5 是纵向滚轮（按一次 = 一格）。
	x11ButtonLeft   = 1
	x11ButtonMiddle = 2
	x11ButtonRight  = 3
	x11WheelUp      = 4
	x11WheelDown    = 5
)

// MoveMouse 把指针移动到指定坐标。
func (x11Desktop) MoveMouse(point Point) error {
	if err := x11Use(func(s *x11Session) error { return s.fakeMotion(point) }); err != nil {
		return err
	}
	Sleep(pointerSettleMS)
	return nil
}

// Click 把指针移动到指定坐标并点击（按下与释放成对，失败也先补释放）。
func (d x11Desktop) Click(point Point, opts ClickOptions) error {
	button, err := x11ButtonCode(opts.Button)
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
	return x11Use(func(s *x11Session) error {
		// 复用与 Windows 同一套点击序列语义（click.go）：按下失败也要补发释放，
		// 绝不把按钮留在按下状态——否则桌面进入拖拽，后续移动变成框选。
		inject := func(eventType uint32, _, _ uintptr, _ uint32) error {
			return s.fakeButton(byte(eventType), button, point)
		}
		sleep := func(delay time.Duration) { Sleep(int(delay / time.Millisecond)) }
		return runClickSequence(inject, uint32(xproto.ButtonPress), uint32(xproto.ButtonRelease), 0, 0, clicks, interval, sleep)
	})
}

// Drag 按住左键从 from 拖到 to 再释放（中途失败也必须释放）。
func (d x11Desktop) Drag(from, to Point, duration time.Duration) error {
	if err := d.MoveMouse(from); err != nil {
		return err
	}
	return x11Use(func(s *x11Session) error {
		if err := s.fakeButton(xproto.ButtonPress, x11ButtonLeft, from); err != nil {
			_ = s.fakeButton(xproto.ButtonRelease, x11ButtonLeft, from)
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
			if err := s.fakeMotion(point); err != nil {
				_ = s.fakeButton(xproto.ButtonRelease, x11ButtonLeft, point)
				return err
			}
			Sleep(int(gap / time.Millisecond))
		}
		return s.fakeButton(xproto.ButtonRelease, x11ButtonLeft, to)
	})
}

// Scroll 在指定坐标滚动滚轮：delta 为 WHEEL_DELTA(120) 的倍数，正数向上。
// X11 的滚轮是"按键 4/5 各按一次算一格"，因此每一格下发一对按下/释放，
// 与 Windows 侧"一格一个事件"的节奏保持一致。
func (d x11Desktop) Scroll(point Point, delta int) error {
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
	err := x11Use(func(s *x11Session) error {
		for _, notch := range notches {
			button := byte(x11WheelUp)
			if notch < 0 {
				button = x11WheelDown
			}
			if err := s.fakeButton(xproto.ButtonPress, button, point); err != nil {
				return err
			}
			if err := s.fakeButton(xproto.ButtonRelease, button, point); err != nil {
				return err
			}
			Sleep(scrollNotchGapMS)
		}
		return nil
	})
	if err != nil {
		return err
	}
	Sleep(scrollSettleMS - scrollNotchGapMS)
	return nil
}

// x11ButtonCode 把工具层的按键名映射成 X11 键号。
func x11ButtonCode(button string) (byte, error) {
	switch button {
	case "", "left":
		return x11ButtonLeft, nil
	case "right":
		return x11ButtonRight, nil
	case "middle":
		return x11ButtonMiddle, nil
	default:
		return 0, fmt.Errorf("computer: 未知鼠标按键 %q", button)
	}
}

// X 键码（keysym）：0xff00 段是功能键与修饰键，0x20-0x7e 段与 ASCII 重合。
const (
	keysymSpace     = 0x20
	keysymBackSpace = 0xff08
	keysymTab       = 0xff09
	keysymReturn    = 0xff0d
	keysymEscape    = 0xff1b
	keysymHome      = 0xff50
	keysymLeft      = 0xff51
	keysymUp        = 0xff52
	keysymRight     = 0xff53
	keysymDown      = 0xff54
	keysymPrior     = 0xff55
	keysymNext      = 0xff56
	keysymEnd       = 0xff57
	keysymInsert    = 0xff63
	keysymF1        = 0xffbe
	keysymShiftL    = 0xffe1
	keysymControlL  = 0xffe3
	keysymCapsLock  = 0xffe5
	keysymAltL      = 0xffe9
	keysymSuperL    = 0xffeb
	keysymDelete    = 0xffff

	// unicodeKeysymBase 是"Unicode 键码"的固定前缀（0x01000000 | 码点）。
	unicodeKeysymBase = 0x01000000
)

// virtualKeyToKeysym 把 keys.go 解析出的虚拟键码映射成 X 键码。
//
// 复用同一套组合键解析（ParseKeyCombo）意味着 "ctrl+shift+t"、"alt+f4" 这类
// 语法的校验与单测在两个平台共享，平台层只负责最后一步的键码翻译。
func virtualKeyToKeysym(vk uint16) (uint32, bool) {
	switch {
	case vk >= 'A' && vk <= 'Z':
		// 字母统一取小写键码：大写由 Shift 表达，组合键（ctrl+shift+t）才成立。
		return uint32(vk - 'A' + 'a'), true
	case vk >= '0' && vk <= '9':
		return uint32(vk), true
	case vk >= 0x70 && vk <= 0x87: // F1..F24
		return uint32(keysymF1) + uint32(vk-0x70), true
	}
	switch vk {
	case VKLShift:
		return keysymShiftL, true
	case VKControl:
		return keysymControlL, true
	case VKMenu:
		return keysymAltL, true
	case VKLWin:
		return keysymSuperL, true
	case VKBack:
		return keysymBackSpace, true
	case VKTab:
		return keysymTab, true
	case VKReturn:
		return keysymReturn, true
	case VKEscape:
		return keysymEscape, true
	case VKSpace:
		return keysymSpace, true
	case VKPrior:
		return keysymPrior, true
	case VKNext:
		return keysymNext, true
	case VKEnd:
		return keysymEnd, true
	case VKHome:
		return keysymHome, true
	case VKLeft:
		return keysymLeft, true
	case VKUp:
		return keysymUp, true
	case VKRight:
		return keysymRight, true
	case VKDown:
		return keysymDown, true
	case VKInsert:
		return keysymInsert, true
	case VKDelete:
		return keysymDelete, true
	case VKCapsLock:
		return keysymCapsLock, true
	default:
		return 0, false
	}
}

// x11Keyslot 是一个键码槽位：哪个键、需不需要 Shift。
type x11Keyslot struct {
	keycode xproto.Keycode
	shift   bool
}

// x11Keymap 是键盘映射的索引（键码 → 槽位），外加一个空闲键码用于输入
// 键盘上根本没有的字符（中文、emoji）。
type x11Keymap struct {
	slots map[uint32]x11Keyslot
	spare xproto.Keycode
}

func (m *x11Keymap) lookup(keysym uint32) (x11Keyslot, bool) {
	slot, ok := m.slots[keysym]
	return slot, ok
}

// keymap 读取并缓存键盘映射。只索引前两档（未按 Shift / 按下 Shift）：
// altgr 档（2、3）需要额外修饰键，与其猜，不如走"空闲键码重映射"那条确定的路。
func (s *x11Session) keymap() (*x11Keymap, error) {
	if s.keys != nil {
		return s.keys, nil
	}
	first := s.setup.MinKeycode
	count := byte(s.setup.MaxKeycode - first + 1)
	reply, err := xproto.GetKeyboardMapping(s.conn, first, count).Reply()
	if err != nil {
		return nil, fmt.Errorf("computer: 读取键盘映射失败: %w", err)
	}
	per := int(reply.KeysymsPerKeycode)
	if per <= 0 {
		return nil, fmt.Errorf("computer: 键盘映射异常（keysyms_per_keycode=%d）", per)
	}
	keymap := &x11Keymap{slots: make(map[uint32]x11Keyslot, len(reply.Keysyms))}
	for index, keysym := range reply.Keysyms {
		if keysym == 0 {
			continue
		}
		level := index % per
		if level > 1 {
			continue
		}
		// 同一键码先出现未按 Shift 档，后出现按下 Shift 档：已有未按档就不要再
		// 被 Shift 档覆盖。
		if _, exists := keymap.slots[uint32(keysym)]; exists && level == 1 {
			continue
		}
		keymap.slots[uint32(keysym)] = x11Keyslot{
			keycode: xproto.Keycode(int(first) + index/per),
			shift:   level == 1,
		}
	}
	// 空闲键码：所有档位都为 0 的最高键码（xdotool 也挑这个位置）。
	for keycode := int(s.setup.MaxKeycode); keycode >= int(first); keycode-- {
		base := (keycode - int(first)) * per
		free := true
		for level := 0; level < per && base+level < len(reply.Keysyms); level++ {
			if reply.Keysyms[base+level] != 0 {
				free = false
				break
			}
		}
		if free {
			keymap.spare = xproto.Keycode(keycode)
			break
		}
	}
	s.keys = keymap
	return keymap, nil
}

// tapKey 按下并释放一个键码；需要 Shift 档时先按下左 Shift，再逆序释放。
func (s *x11Session) tapKey(keycode xproto.Keycode, shift bool) error {
	if !shift {
		if err := s.fakeKey(xproto.KeyPress, byte(keycode)); err != nil {
			return err
		}
		return s.fakeKey(xproto.KeyRelease, byte(keycode))
	}
	keymap, err := s.keymap()
	if err != nil {
		return err
	}
	slot, ok := keymap.lookup(keysymShiftL)
	if !ok {
		return fmt.Errorf("computer: 键盘上没有左 Shift 键")
	}
	if err := s.fakeKey(xproto.KeyPress, byte(slot.keycode)); err != nil {
		return err
	}
	if err := s.fakeKey(xproto.KeyPress, byte(keycode)); err != nil {
		_ = s.fakeKey(xproto.KeyRelease, byte(slot.keycode))
		return err
	}
	if err := s.fakeKey(xproto.KeyRelease, byte(keycode)); err != nil {
		_ = s.fakeKey(xproto.KeyRelease, byte(slot.keycode))
		return err
	}
	return s.fakeKey(xproto.KeyRelease, byte(slot.keycode))
}

// TypeText 逐字注入文本（支持中文与 emoji）。
//
// ASCII/Latin-1 字符直接命中现成键码；其余字符借用 X11 惯例：把最高的一段
// 空闲键码临时重映射成该字符的 Unicode 键码，按一下再还回去（xdotool 同样
// 做法）。重映射请求与按键事件在同一条连接上按序到达服务器，因此不需要在
// 每字之间插入往返——服务器处理顺序本身就保证了"先改映射、再产生按键"。
func (x11Desktop) TypeText(text string) error {
	if text == "" {
		return nil
	}
	return x11Use(func(s *x11Session) error {
		keymap, err := s.keymap()
		if err != nil {
			return err
		}
		var (
			spare    xproto.Keycode
			remapped uint32
		)
		for _, char := range text {
			keysym := keysymForRune(char)
			if slot, ok := keymap.lookup(keysym); ok {
				if err := s.tapKey(slot.keycode, slot.shift); err != nil {
					return err
				}
				continue
			}
			if spare == 0 {
				spare = keymap.spare
				if spare == 0 {
					return fmt.Errorf("computer: 键盘没有空闲键码，无法输入 %q", char)
				}
			}
			if remapped != keysym {
				s.remapKeycode(spare, keysym)
				remapped = keysym
			}
			if err := s.tapKey(spare, false); err != nil {
				return err
			}
		}
		if spare != 0 {
			// 尽力还原：空闲键码用完还回去，别把用户的键盘映射留脏。
			s.remapKeycode(spare, 0)
		}
		return nil
	})
}

// remapKeycode 把一个键码映射成给定键码（keysym=0 表示还原为空闲）。
// 刻意不 Check：错误会在后续 checked 请求处暴露，逐字注入不该为它多花往返。
func (s *x11Session) remapKeycode(keycode xproto.Keycode, keysym uint32) {
	xproto.ChangeKeyboardMapping(s.conn, 1, keycode, 1, []xproto.Keysym{xproto.Keysym(keysym)})
}

// keysymForRune 把 rune 映射成 X 键码：ASCII 与 Latin-1 用码点本身，
// 其余用 Unicode 键码（0x01000000 | 码点）。
func keysymForRune(char rune) uint32 {
	switch {
	case char >= keysymSpace && char < 0x7f:
		return uint32(char)
	case char >= 0xa0 && char <= 0xff:
		return uint32(char)
	default:
		return unicodeKeysymBase | uint32(char)
	}
}

// PressKeys 按下组合键，times 为重复次数（0 视为 1）。
// 顺序与 Windows 侧一致：修饰键按下 → 主键 → 修饰键逆序释放。
func (x11Desktop) PressKeys(combo string, times int) error {
	parsed, err := ParseKeyCombo(combo)
	if err != nil {
		return err
	}
	if times < 1 {
		times = 1
	}
	return x11Use(func(s *x11Session) error {
		keymap, err := s.keymap()
		if err != nil {
			return err
		}
		modifiers := make([]xproto.Keycode, 0, len(parsed.Modifiers))
		for _, vk := range parsed.Modifiers {
			keysym, ok := virtualKeyToKeysym(vk)
			if !ok {
				return fmt.Errorf("computer: 按键组合 %q 含无法映射的修饰键", combo)
			}
			slot, exists := keymap.lookup(keysym)
			if !exists {
				return fmt.Errorf("computer: 键盘上没有 %q 需要的修饰键", combo)
			}
			modifiers = append(modifiers, slot.keycode)
		}
		keysym, ok := virtualKeyToKeysym(parsed.Key)
		if !ok {
			return fmt.Errorf("computer: 无法把按键组合 %q 映射到 X11 键码", combo)
		}
		main, exists := keymap.lookup(keysym)
		if !exists {
			return fmt.Errorf("computer: 键盘上没有 %q 需要的键", combo)
		}
		for repeat := 0; repeat < times; repeat++ {
			for _, modifier := range modifiers {
				if err := s.fakeKey(xproto.KeyPress, byte(modifier)); err != nil {
					return err
				}
			}
			if err := s.tapKey(main.keycode, main.shift); err != nil {
				return err
			}
			for index := len(modifiers) - 1; index >= 0; index-- {
				if err := s.fakeKey(xproto.KeyRelease, byte(modifiers[index])); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
