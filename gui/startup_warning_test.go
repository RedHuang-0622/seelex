package gui

import (
	"os"
	"strings"
	"testing"
)

// TestGUIStartupWarningsAreNonBlocking：账号档异常不能让旧会话“看起来打不开”。
// bootstrap 已把警告写入会话可见区；GUI 不再弹原生 MessageDialog 阻塞界面。
func TestGUIStartupWarningsAreNonBlocking(t *testing.T) {
	source, err := os.ReadFile("run_wails.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "MessageDialog") {
		t.Fatal("run_wails.go 不得用原生 MessageDialog 阻塞启动；启动警告应留在会话可见区")
	}
}
