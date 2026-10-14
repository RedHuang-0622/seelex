package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 「运行时上限覆盖层」的用例（runtime_limits_overlay.go）。
//
// 这里钉住三件事：
//  1. 落盘读回的**闭环**（写进数据根 → 启动时叠回 limits 之上）；
//  2. 坏文件/缺文件都不阻断启动——按"没有覆盖"继续（界面上拧过的参数永远不该让
//     应用开不起来）；
//  3. 覆盖层的优先级与**校验**：合法值压过配置，非法值被忽略后仍是配置值。

// overlayPath 给每个用例一份独立的落盘路径（数据根下的覆盖层文件），顺带证明
// 落盘会在缺失的目录层级上建目录。
func overlayPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "data", runtimeLimitsOverlayFile)
}

func TestRuntimeLimitsOverlayRoundTripsWindow(t *testing.T) {
	path := overlayPath(t)
	if err := writeRuntimeLimitsOverlay(path, 3); err != nil {
		t.Fatalf("writeRuntimeLimitsOverlay: %v", err)
	}
	if got := readRuntimeLimitsOverlay(path).SessionReleaseWindow; got != 3 {
		t.Fatalf("读回窗口 = %d, want 3", got)
	}
	// 正文是给人看的一份 JSON（缩进 + 结尾换行），且**只有拧过的格子**在里面。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("落盘正文应以换行收尾：%q", string(raw))
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("落盘正文不是 JSON：%v", err)
	}
	if decoded["session_release_window"] != float64(3) {
		t.Fatalf("落盘正文 = %v, want session_release_window=3", decoded)
	}
	// 覆盖层文件与 session-storage.json 同级（数据根），临时文件不留在现场。
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("落盘留下了临时文件：%s", entry.Name())
			}
		}
	}
}

func TestReadRuntimeLimitsOverlayFallsBackToNoOverlay(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
	}{
		{"路径为空", ""},
		{"文件不存在", filepath.Join(dir, "nope.json")},
		{"内容损坏", broken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := readRuntimeLimitsOverlay(tc.path); got.SessionReleaseWindow != 0 {
				t.Fatalf("应当按“没有覆盖”继续，读到 %d", got.SessionReleaseWindow)
			}
		})
	}
}

func TestApplyRuntimeLimitsOverlayPrecedence(t *testing.T) {
	base := seelexctx.DefaultLimits()
	configured := base
	configured.SessionReleaseWindow = 4 // 配置里写过的值

	// 合法覆盖：压过配置。
	applied := applyRuntimeLimitsOverlay(configured, runtimeLimitsOverlay{SessionReleaseWindow: 7})
	if applied.SessionReleaseWindow != 7 {
		t.Fatalf("合法覆盖应当压过配置：%d, want 7", applied.SessionReleaseWindow)
	}
	// 空覆盖：配置原样生效。
	if got := applyRuntimeLimitsOverlay(configured, runtimeLimitsOverlay{}); got.SessionReleaseWindow != 4 {
		t.Fatalf("空覆盖不该改配置：%d, want 4", got.SessionReleaseWindow)
	}
	// 非法覆盖：忽略该格，仍是配置值（不让一份坏文件把启动拦下）。
	for _, window := range []int{-3, -1, 0, seelexctx.SessionReleaseWindowMax + 1, 999} {
		got := applyRuntimeLimitsOverlay(configured, runtimeLimitsOverlay{SessionReleaseWindow: window})
		if got.SessionReleaseWindow != 4 {
			t.Fatalf("非法覆盖 %d 应当被忽略：%d, want 4", window, got.SessionReleaseWindow)
		}
	}
}

func TestRuntimeLimitsPathSitsInDataRoot(t *testing.T) {
	before := *storePath
	t.Cleanup(func() { *storePath = before })

	root := t.TempDir()
	*storePath = filepath.Join(root, "sessions", "store.json")
	want := filepath.Join(root, "sessions", runtimeLimitsOverlayFile)
	if got := runtimeLimitsPath(); got != want {
		t.Fatalf("runtimeLimitsPath() = %q, want %q", got, want)
	}
}

func TestRuntimeLimitsStoreFileWritesOverlay(t *testing.T) {
	path := overlayPath(t)
	store := runtimeLimitsStoreFile{path: path}
	if err := store.SaveSessionReleaseWindow(5); err != nil {
		t.Fatalf("SaveSessionReleaseWindow: %v", err)
	}
	if got := readRuntimeLimitsOverlay(path).SessionReleaseWindow; got != 5 {
		t.Fatalf("落盘后读回 = %d, want 5", got)
	}
	// 路径为空（宿主没给数据根）必须显式失败——静默成功会让"界面说存了、重启却没了"。
	if err := (runtimeLimitsStoreFile{}).SaveSessionReleaseWindow(2); err == nil {
		t.Fatal("空路径必须报错")
	}
}
