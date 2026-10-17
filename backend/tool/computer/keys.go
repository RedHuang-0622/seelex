package computer

import (
	"fmt"
	"strings"
)

// 虚拟键码（Virtual-Key Code），数值与 Windows 一致，供跨平台解析与测试使用。
const (
	VKLShift   = 0x10
	VKControl  = 0x11
	VKMenu     = 0x12
	VKLWin     = 0x5B
	VKBack     = 0x08
	VKTab      = 0x09
	VKReturn   = 0x0D
	VKEscape   = 0x1B
	VKSpace    = 0x20
	VKPrior    = 0x21
	VKNext     = 0x22
	VKEnd      = 0x23
	VKHome     = 0x24
	VKLeft     = 0x25
	VKUp       = 0x26
	VKRight    = 0x27
	VKDown     = 0x28
	VKInsert   = 0x2D
	VKDelete   = 0x2E
	VKCapsLock = 0x14
)

var namedKeys = map[string]uint16{
	"enter": VKReturn, "return": VKReturn,
	"tab": VKTab, "esc": VKEscape, "escape": VKEscape,
	"space": VKSpace, "backspace": VKBack, "delete": VKDelete, "del": VKDelete,
	"insert": VKInsert, "home": VKHome, "end": VKEnd,
	"pageup": VKPrior, "pagedown": VKNext,
	"up": VKUp, "down": VKDown, "left": VKLeft, "right": VKRight,
	"capslock": VKCapsLock, "win": VKLWin, "cmd": VKLWin, "super": VKLWin,
}

func init() {
	for i := 1; i <= 24; i++ {
		namedKeys[fmt.Sprintf("f%d", i)] = uint16(0x6F + i)
	}
}

// KeyCombo 是一次按键组合的解析结果。
type KeyCombo struct {
	// Modifiers 是按顺序按下的修饰键。
	Modifiers []uint16
	// Key 是主键的虚拟键码。
	Key uint16
}

// ParseKeyCombo 解析形如 "ctrl+shift+t"、"alt+f4"、"enter" 的按键组合。
// 修饰键支持 ctrl/control、shift、alt、win/cmd/super。
func ParseKeyCombo(combo string) (KeyCombo, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	if len(parts) == 0 || parts[0] == "" {
		return KeyCombo{}, fmt.Errorf("computer: 空的按键组合")
	}
	var out KeyCombo
	for i, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			return KeyCombo{}, fmt.Errorf("computer: 按键组合 %q 含空段", combo)
		}
		last := i == len(parts)-1
		if vk, ok := modifierKey(token); ok {
			if last && len(parts) == 1 {
				// 单独按修饰键也允许。
				out.Key = vk
				return out, nil
			}
			out.Modifiers = append(out.Modifiers, vk)
			continue
		}
		if !last {
			return KeyCombo{}, fmt.Errorf("computer: %q 不是修饰键，不能出现在组合中间", token)
		}
		vk, ok := singleKey(token)
		if !ok {
			return KeyCombo{}, fmt.Errorf("computer: 无法识别的按键 %q", token)
		}
		out.Key = vk
	}
	if out.Key == 0 {
		return KeyCombo{}, fmt.Errorf("computer: 按键组合 %q 缺少主键", combo)
	}
	return out, nil
}

func modifierKey(token string) (uint16, bool) {
	switch token {
	case "ctrl", "control":
		return VKControl, true
	case "shift":
		return VKLShift, true
	case "alt":
		return VKMenu, true
	case "win", "cmd", "super", "meta":
		return VKLWin, true
	default:
		return 0, false
	}
}

func singleKey(token string) (uint16, bool) {
	if vk, ok := namedKeys[token]; ok {
		return vk, true
	}
	if len(token) == 1 {
		switch c := token[0]; {
		case c >= 'a' && c <= 'z':
			return uint16(c - 'a' + 'A'), true
		case c >= '0' && c <= '9':
			return uint16(c), true
		}
	}
	return 0, false
}
