package main

// 插件根责任链的守卫（与 plugin/loader.go 的"多根 first-wins"合起来是"一处发现、
// 处处可用"的两半）。旧口径是纯 CWD 相对的单个 `-plugins`（缺省 "plugins"）：发行
// 包里 plugins/ 放在二进制旁边也找不到，于是静默降级成"零插件零技能启动且无提示"。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
)

func TestPluginRootChainOrderAndDedup(t *testing.T) {
	exe := filepath.Join("bin", "seelex.exe")
	got := pluginRootChain("a, b ,a", "c", exe)
	want := []string{
		"a", "b", "c",
		filepath.Join("bin", "plugins"),
		filepath.Join("bin", "..", "plugins"),
	}
	if len(got) != len(want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chain[%d] = %q, want %q (chain=%v)", i, got[i], want[i], got)
		}
	}
	// 显式旗标必须排在环境变量之前：`-plugins <开发目录>` 要能覆盖发行包里那份。
	if indexOf(got, "a") > indexOf(got, "c") {
		t.Fatalf("explicit -plugins must precede $SEELEX_PLUGINS: %v", got)
	}
}

func TestPluginRootChainWithoutFlagFallsBackToExeThenCwd(t *testing.T) {
	exe := filepath.Join(string(filepath.Separator), "app", "bin", "seelex")
	got := pluginRootChain("", "", exe)
	want := []string{
		filepath.Join(string(filepath.Separator), "app", "bin", "plugins"),
		filepath.Join(string(filepath.Separator), "app", "plugins"),
		"plugins",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chain = %v, want %v", got, want)
	}

	// 重复根只留一份：CWD 就是二进制所在目录时，<exe>/../plugins 与收尾的 plugins 是同一个。
	deduped := pluginRootChain("plugins", "", filepath.Join("plugins", "seelex.exe"))
	if len(deduped) != 2 {
		t.Fatalf("deduped chain = %v, want 2 entries", deduped)
	}
	if deduped[0] != "plugins" {
		t.Fatalf("deduped[0] = %q, want the explicit flag root first", deduped[0])
	}
}

// TestPluginRootsUsesFlagThenEnvThenExe 是真实环境下的链序（旗标 / $SEELEX_PLUGINS /
// 可执行文件位置）。
func TestPluginRootsUsesFlagThenEnvThenCwd(t *testing.T) {
	original := *pluginsPaths
	defer func() { *pluginsPaths = original }()

	t.Setenv("SEELEX_PLUGINS", filepath.Join("env", "plugins"))
	*pluginsPaths = filepath.Join("flag", "plugins")
	roots := pluginRoots()
	if len(roots) == 0 || roots[0] != filepath.Join("flag", "plugins") {
		t.Fatalf("roots = %v, want the flag root first", roots)
	}
	if indexOf(roots, filepath.Join("env", "plugins")) != 1 {
		t.Fatalf("roots = %v, want $SEELEX_PLUGINS second", roots)
	}
	if roots[len(roots)-1] != "plugins" {
		t.Fatalf("roots = %v, want the CWD-relative plugins fallback last", roots)
	}

	// 旗标为空时链首落到环境变量（defer 恢复见上）。
	*pluginsPaths = ""
	if roots := pluginRoots(); roots[0] != filepath.Join("env", "plugins") {
		t.Fatalf("roots = %v, want $SEELEX_PLUGINS first when -plugins is empty", roots)
	}
}

func TestRequirePluginsIsExplicitAboutZeroPlugins(t *testing.T) {
	loaded := []plugin.Plugin{{Name: "default"}}
	if err := requirePlugins([]string{"plugins"}, loaded); err != nil {
		t.Fatalf("non-empty plugin set must pass: %v", err)
	}

	err := requirePlugins([]string{"a/plugins", "b/plugins"}, nil)
	if err == nil {
		t.Fatal("zero plugins must be an explicit error, not a silent downgrade")
	}
	message := err.Error()
	for _, want := range []string{"零插件", "2 个根", "a/plugins", "b/plugins", "-plugins", "SEELEX_PLUGINS"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q must contain %q", message, want)
		}
	}

	if err := requirePlugins(nil, nil); err == nil || !strings.Contains(err.Error(), "责任链为空") {
		t.Fatalf("empty chain error = %v, want it to spell out that nothing was tried", err)
	}
}

// TestPluginRootChainIsTheOnlySourceOfLoaderRoots 钉住"根解析只有一条路"：main.go 里
// 除了 pluginRootChain 不该再有第二处拼插件根的地方（旧口径直接 splitPaths(*pluginsPaths)）。
func TestPluginRootChainIsTheOnlySourceOfLoaderRoots(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "plugin.NewLoader(splitPaths(*pluginsPaths)...)") {
		t.Fatal("main.go 仍在直接消费 -plugins：根解析必须走 pluginRootChain（否则链上其余根是死的）")
	}
	if !strings.Contains(text, "plugin.NewLoader(roots...)") {
		t.Fatal("initPluginSystem 应当用 pluginRootChain 解析出的 roots 构造 loader")
	}
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
