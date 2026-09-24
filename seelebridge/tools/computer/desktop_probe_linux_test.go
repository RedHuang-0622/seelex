//go:build linux

package computer

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jezek/xgb/xproto"
)

// 桌面探针默认跳过：它会在真实桌面上移动一次指针并点一下（会夺走一次点击，
// 也可能打扰正在操作这台机器的人），只在需要验证输入注入语义时显式开启：
//
//	DISPLAY=:0 SEELEX_COMPUTER_DESKTOP_PROBE=1 \
//	  SEELEX_COMPUTER_DESKTOP_PROBE_POINT="800,600" \
//	  go test ./seelebridge/tools/computer/ -run TestLinuxClickReleasesMouseButton -v
//
// 它守的是 X11 侧的两条底线：
//
//  1. XTEST 真把事件送进了服务器——指针确实落在指定坐标（只"没报错"不够，
//     扩展没接上时请求也可能被静默丢弃）；
//  2. 点击之后左键没有停在按下状态（QueryPointer 的 Mask 里不含 Button1Mask），
//     否则桌面会卡在拖拽——这与 Windows 探针守的是同一条坑。
func TestLinuxClickReleasesMouseButton(t *testing.T) {
	if os.Getenv("SEELEX_COMPUTER_DESKTOP_PROBE") != "1" {
		t.Skip("真实桌面探针：设置 SEELEX_COMPUTER_DESKTOP_PROBE=1 后运行")
	}
	if !Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	origin, err := CursorPosition()
	if err != nil {
		t.Fatalf("CursorPosition: %v", err)
	}
	target := origin
	if raw := strings.TrimSpace(os.Getenv("SEELEX_COMPUTER_DESKTOP_PROBE_POINT")); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) != 2 {
			t.Fatalf("SEELEX_COMPUTER_DESKTOP_PROBE_POINT 需要 \"x,y\"：%q", raw)
		}
		x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errX != nil || errY != nil {
			t.Fatalf("SEELEX_COMPUTER_DESKTOP_PROBE_POINT 需要 \"x,y\"：%q", raw)
		}
		target = Point{X: x, Y: y}
	}
	t.Cleanup(func() { _ = MoveMouse(origin) })

	if err := MoveMouse(target); err != nil {
		t.Fatalf("MoveMouse: %v", err)
	}
	moved, err := CursorPosition()
	if err != nil {
		t.Fatalf("CursorPosition: %v", err)
	}
	if moved != target {
		t.Fatalf("指针没有落在指定坐标：want %+v got %+v（XTEST 没生效？）", target, moved)
	}

	if err := Click(target, ClickOptions{Clicks: 1}); err != nil {
		t.Fatalf("Click 报错（按下/释放必须成对且成功时返回 nil）：%v", err)
	}
	Sleep(120)
	down, err := leftButtonDown()
	if err != nil {
		t.Fatalf("QueryPointer: %v", err)
	}
	if down {
		t.Fatalf("Click 之后左键仍处于按下状态：释放事件没有发出，桌面会卡在拖拽")
	}
	t.Logf("真机输入注入: 指针 %+v → %+v，左键已释放", origin, target)
}

// leftButtonDown 报告左键当前是否按下（QueryPointer 的按键掩码）。
func leftButtonDown() (bool, error) {
	down := false
	err := x11Use(func(s *x11Session) error {
		reply, err := xproto.QueryPointer(s.conn, s.root).Reply()
		if err != nil {
			return err
		}
		down = reply.Mask&xproto.KeyButMaskButton1 != 0
		return nil
	})
	return down, err
}
