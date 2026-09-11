//go:build windows

package computer

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

const vkLeftButton = 0x01

var procGetAsyncKeyState = user32DLL.NewProc("GetAsyncKeyState")

// 桌面探针默认跳过：它会在真实桌面上点一下鼠标（会夺走一次点击，也可能
// 打扰正在操作这台机器的人），只在需要验证输入注入语义时显式开启：
//
//	$env:SEELEX_COMPUTER_DESKTOP_PROBE = "1"
//	$env:SEELEX_COMPUTER_DESKTOP_PROBE_POINT = "800,600"   # 可选，默认当前光标处
//	go test ./seelebridge/tools/computer/ -run TestClickReleasesMouseButton -v
//
// 它守的是这条曾经踩过的坑：Click 把 void 返回的 mouse_event 当失败判断，
// 于是只发出按下、从不发出释放——点击既报错，又把鼠标留在按下状态（桌面进入
// 拖拽）。探针同时检查「Click 不再报错」与「左键确实已释放」。
func TestClickReleasesMouseButton(t *testing.T) {
	if os.Getenv("SEELEX_COMPUTER_DESKTOP_PROBE") != "1" {
		t.Skip("真实桌面探针：设置 SEELEX_COMPUTER_DESKTOP_PROBE=1 后运行")
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

	if err := Click(target, ClickOptions{Clicks: 1}); err != nil {
		t.Fatalf("Click 报错（按下/释放必须成对且成功时返回 nil）：%v", err)
	}
	Sleep(120)
	if down, err := leftButtonDown(); err != nil {
		t.Fatalf("GetAsyncKeyState: %v", err)
	} else if down {
		t.Fatalf("Click 之后左键仍处于按下状态：释放事件没有发出，桌面会卡在拖拽")
	}
}

// leftButtonDown 报告左键当前是否物理按下。
func leftButtonDown() (bool, error) {
	ret, _, _ := procGetAsyncKeyState.Call(uintptr(vkLeftButton))
	return ret&0x8000 != 0, nil
}
