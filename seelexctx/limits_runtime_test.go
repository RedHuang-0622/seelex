package seelexctx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadLimitsRuntimeAllowMultiProcess 钉住 limits.runtime.allow_multi_process
// 的两臂解析与「零值 = 关」（默认单实例）：
//   - 显式 true → true（放行第二个进程）；
//   - 显式 false / 整块缺失 → false（单实例）；
//   - WithDefaults 不把 false 抬成 true。
func TestLoadLimitsRuntimeAllowMultiProcess(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	loaded, err := LoadLimits(write("on.yaml", "limits:\n  runtime:\n    allow_multi_process: true\n"))
	if err != nil {
		t.Fatalf("LoadLimits(on): %v", err)
	}
	if !loaded.Runtime.AllowMultiProcess {
		t.Fatal("allow_multi_process: true 必须读成 true（放行第二个进程）")
	}

	loaded, err = LoadLimits(write("off.yaml", "limits:\n  runtime:\n    allow_multi_process: false\n"))
	if err != nil {
		t.Fatalf("LoadLimits(off): %v", err)
	}
	if loaded.Runtime.AllowMultiProcess {
		t.Fatal("allow_multi_process: false 必须读成 false（单实例）")
	}

	loaded, err = LoadLimits(write("absent.yaml", "limits:\n  team:\n    max_teammates: 4\n"))
	if err != nil {
		t.Fatalf("LoadLimits(absent): %v", err)
	}
	if loaded.Runtime.AllowMultiProcess {
		t.Fatal("runtime 整块缺失必须落到零值 false（单实例）")
	}
	if loaded.WithDefaults().Runtime.AllowMultiProcess {
		t.Fatal("WithDefaults 不得把 allow_multi_process 抬成 true")
	}
}

// TestShippedConfigShipsSingleProcess 钉住出厂档这一行：config/seelex.yaml 的
// limits.runtime.allow_multi_process 仍是 false。改了这行就等于改变默认启动行为
// （单实例 → 多进程放行），必须是显式决定，而不是被顺手带上。
func TestShippedConfigShipsSingleProcess(t *testing.T) {
	loaded, err := LoadLimits(filepath.Join("..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatalf("LoadLimits(shipped): %v", err)
	}
	if loaded.Runtime.AllowMultiProcess {
		t.Fatal("出厂 config/seelex.yaml 必须仍是单实例（allow_multi_process: false）")
	}
}
