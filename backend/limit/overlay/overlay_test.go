package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// 「运行时上限覆盖层」的用例（overlay.go）。
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
	return filepath.Join(t.TempDir(), "data", FileName)
}

func TestRuntimeLimitsOverlayRoundTripsWindow(t *testing.T) {
	path := overlayPath(t)
	if err := WriteSessionReleaseWindow(path, 3); err != nil {
		t.Fatalf("writeRuntimeLimitsOverlay: %v", err)
	}
	if got := Read(path).SessionReleaseWindow; got != 3 {
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
	// 覆盖层文件落在数据根（与 workspace_index.json 同级），临时文件不留在现场。
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
			if got := Read(tc.path); got.SessionReleaseWindow != 0 {
				t.Fatalf("应当按“没有覆盖”继续，读到 %d", got.SessionReleaseWindow)
			}
		})
	}
}

func TestApplyRuntimeLimitsOverlayPrecedence(t *testing.T) {
	base := limit.DefaultLimits()
	configured := base
	configured.SessionReleaseWindow = 4 // 配置里写过的值

	// 合法覆盖：压过配置。
	applied := Apply(configured, Overlay{SessionReleaseWindow: 7})
	if applied.SessionReleaseWindow != 7 {
		t.Fatalf("合法覆盖应当压过配置：%d, want 7", applied.SessionReleaseWindow)
	}
	// 空覆盖：配置原样生效。
	if got := Apply(configured, Overlay{}); got.SessionReleaseWindow != 4 {
		t.Fatalf("空覆盖不该改配置：%d, want 4", got.SessionReleaseWindow)
	}
	// 非法覆盖：忽略该格，仍是配置值（不让一份坏文件把启动拦下）。
	for _, window := range []int{-3, -1, 0, limit.SessionReleaseWindowMax + 1, 999} {
		got := Apply(configured, Overlay{SessionReleaseWindow: window})
		if got.SessionReleaseWindow != 4 {
			t.Fatalf("非法覆盖 %d 应当被忽略：%d, want 4", window, got.SessionReleaseWindow)
		}
	}
}

// TestPathSitsInDataRoot：`Path(dataRoot)` = dataRoot/FileName。
//
// 「数据根从哪来」是组合根的事（main.go 从 -store 参数取）——本包只接收它，
// 因此这里不再断言 flag 绑定，只钉住拼接口径。
func TestPathSitsInDataRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	want := filepath.Join(root, FileName)
	if got := Path(root); got != want {
		t.Fatalf("Path(%q) = %q, want %q", root, got, want)
	}
}

func TestRuntimeLimitsStoreFileWritesOverlay(t *testing.T) {
	path := overlayPath(t)
	store := StoreFile{Path: path}
	if err := store.SaveSessionReleaseWindow(5); err != nil {
		t.Fatalf("SaveSessionReleaseWindow: %v", err)
	}
	if got := Read(path).SessionReleaseWindow; got != 5 {
		t.Fatalf("落盘后读回 = %d, want 5", got)
	}
	// 路径为空（宿主没给数据根）必须显式失败——静默成功会让"界面说存了、重启却没了"。
	if err := (StoreFile{}).SaveSessionReleaseWindow(2); err == nil {
		t.Fatal("空路径必须报错")
	}
}

// TestAccountWindowsOverlayRoundTripKeepsOtherKeys 钉住「每账号上下文窗口」（设置 →
// 账号）与同一份覆盖层里其它参数**互不抹掉**：两个写入器都是读-改-写，谁后写都不会
// 把对方那一格清掉（同一份 JSON 被两个设置面共用的必然要求）。
func TestAccountWindowsOverlayRoundTripKeepsOtherKeys(t *testing.T) {
	path := overlayPath(t)
	store := StoreFile{Path: path}
	if err := store.SaveSessionReleaseWindow(3); err != nil {
		t.Fatalf("SaveSessionReleaseWindow: %v", err)
	}
	if err := store.SaveAccountWindow("agent-1", 400_000); err != nil {
		t.Fatalf("SaveAccountWindow: %v", err)
	}
	if err := store.SaveAccountWindow("subagent-1", 200_000); err != nil {
		t.Fatalf("SaveAccountWindow: %v", err)
	}
	parsed := Read(path)
	if parsed.SessionReleaseWindow != 3 {
		t.Fatalf("写账号窗口把会话驻留窗口抹掉了：%d, want 3", parsed.SessionReleaseWindow)
	}
	if parsed.AccountWindows["agent-1"] != 400_000 || parsed.AccountWindows["subagent-1"] != 200_000 {
		t.Fatalf("账号窗口读回 = %v, want agent-1=400000 / subagent-1=200000", parsed.AccountWindows)
	}
	// 清掉一格（window <= 0）后：只剩另一格，且同一份文件里的其它参数不动。
	if err := store.SaveAccountWindow("agent-1", 0); err != nil {
		t.Fatalf("SaveAccountWindow(清除): %v", err)
	}
	parsed = Read(path)
	if _, ok := parsed.AccountWindows["agent-1"]; ok {
		t.Fatalf("清掉的那一格不该还在：%v", parsed.AccountWindows)
	}
	if parsed.AccountWindows["subagent-1"] != 200_000 || parsed.SessionReleaseWindow != 3 {
		t.Fatalf("清除一格不该动别的格子：%v / %d", parsed.AccountWindows, parsed.SessionReleaseWindow)
	}
	// 路径为空（宿主没给数据根）必须显式失败——与 SessionReleaseWindow 同一条纪律。
	if err := (StoreFile{}).SaveAccountWindow("agent-1", 200_000); err == nil {
		t.Fatal("空路径必须报错")
	}
}
