//go:build windows

package computer

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ── 真机桌面探针：可滚动面板识别 ─────────────────────────────
//
// 默认跳过；`SEELEX_COMPUTER_DESKTOP_PROBE=1` 才在真实桌面上跑。整个过程**只读**：
// 只做 UI Automation 枚举与属性读取，不点、不滚、不聚焦、不改任何窗口状态。
//
// 目标窗口：`SEELEX_COMPUTER_PROBE_WINDOW` 给的标题子串（不聚焦，直接查后台
// 窗口），未给则用当前前台窗口。
func TestDesktopProbeScrollTargets(t *testing.T) {
	if os.Getenv("SEELEX_COMPUTER_DESKTOP_PROBE") != "1" {
		t.Skip("需要 SEELEX_COMPUTER_DESKTOP_PROBE=1 才在真实桌面上跑")
	}
	window, err := probeWindow(t)
	if err != nil {
		t.Fatalf("找目标窗口: %v", err)
	}
	t.Logf("目标窗口: %q rect=%s handle=0x%X", window.Title, window.Rect, window.Handle)

	start := time.Now()
	targets, err := ListScrollTargets(ScrollTargetOptions{WindowHandle: window.Handle})
	if err != nil {
		t.Fatalf("ListScrollTargets: %v", err)
	}
	t.Logf("枚举耗时 %s，命中 %d 个可滚动面板", time.Since(start), len(targets))
	for index, target := range targets {
		t.Logf("#%d name=%q type=%q class=%q rect=%s center=(%d,%d) vertical=%+v horizontal=%+v",
			index, target.Name, target.ControlType, target.ClassName, target.Rect,
			target.Center().X, target.Center().Y, target.Vertical, target.Horizontal)
	}
	if len(targets) == 0 {
		t.Skip("该窗口没有可滚动面板：换成浏览器/资源管理器窗口再跑")
	}

	// 点命中回读：拿最大的那块面板中心去查，应该查回同一块面板。
	primary := targets[0]
	center := primary.Center()
	found, ok, err := ScrollStateAtPoint(center, DefaultScrollTargetTimeout)
	if err != nil {
		t.Fatalf("ScrollStateAtPoint(%d,%d): %v", center.X, center.Y, err)
	}
	if !ok {
		t.Fatalf("面板中心 (%d,%d) 应命中可滚动面板，实际未命中", center.X, center.Y)
	}
	t.Logf("点命中回读: name=%q rect=%s vertical=%+v horizontal=%+v",
		found.Name, found.Rect, found.Vertical, found.Horizontal)
	if !found.Contains(center) {
		t.Fatalf("回读面板 %s 不包含查询点 (%d,%d)", found.Rect, center.X, center.Y)
	}
}

func probeWindow(t *testing.T) (Window, error) {
	t.Helper()
	match := strings.TrimSpace(os.Getenv("SEELEX_COMPUTER_PROBE_WINDOW"))
	if match == "" {
		return ForegroundWindow()
	}
	windows, err := ListWindows()
	if err != nil {
		return Window{}, err
	}
	needle := strings.ToLower(match)
	for _, window := range windows {
		if !window.Visible || !strings.Contains(strings.ToLower(window.Title), needle) {
			continue
		}
		return window, nil
	}
	for _, window := range windows {
		if window.Visible {
			t.Logf("可见窗口候选: %q", window.Title)
		}
	}
	return ForegroundWindow()
}
