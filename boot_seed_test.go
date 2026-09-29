package main

// boot_seed_test.go — 启动期"责任链 + 缺失即初始化"的钉子（口径见 internal/bootseed）。
//
// 钉住三件事，都是现场踩过的：
//
//	1. 新解开的包里（CWD 没有 config/、二进制旁边也没有）→ 缺失时用内嵌默认档
//	   初始化到二进制旁边，且与仓库规范档逐字节相同；
//	2. 初始化过之后就"存在即读"：路径稳定、内容不再被改写；
//	3. 白名单命令脚本目录走同一条链（仓库里那份存在时命中，不存在时落骨架）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/bootseed"
)

// executableDir 返回当前测试二进制的目录（等价于"包自己的目录"）：
// ensureConfigFile 的落盘根就取它。
func executableDir(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("拿不到测试二进制路径: %v", err)
	}
	dir := filepath.Dir(exe)
	probe := filepath.Join(dir, "bootseed-writable-probe.tmp")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Skipf("二进制目录不可写（落盘根不可用）: %v", err)
	}
	_ = os.Remove(probe)
	return dir
}

func TestEnsureConfigFileSeedsDefaultThenReadsIt(t *testing.T) {
	exeDir := executableDir(t)
	repo, err := os.Getwd() // go test 的 CWD = 仓库根
	if err != nil {
		t.Fatal(err)
	}
	// 立场：一个刚解开的包——CWD 里没有 config/，二进制旁边也没有。
	t.Chdir(t.TempDir())
	seededPath := filepath.Join(exeDir, "config", bootseed.RuntimeConfigName)
	_ = os.Remove(seededPath) // 上一次跑留下的不算现场
	t.Cleanup(func() { _ = os.Remove(seededPath) })

	got := ensureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack())
	if got != seededPath {
		t.Fatalf("候选链全缺时应初始化到二进制旁边的 config/：期望 %s，得到 %s", seededPath, got)
	}
	seeded, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("缺失即初始化：期望落盘出默认档，读取失败 %v", err)
	}
	canonical, err := os.ReadFile(filepath.Join(repo, "config", bootseed.RuntimeConfigName))
	if err != nil {
		t.Fatalf("读仓库规范档: %v", err)
	}
	if string(seeded) != string(canonical) {
		t.Fatal("初始化出来的默认档必须与仓库 config/seelex.yaml 逐字节相同")
	}

	// 存在即读：用户改过之后，再次启动只能读到它，不许被默认数据盖回去。
	if err := os.WriteFile(got, []byte("# 用户改过的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := ensureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack()); again != got {
		t.Fatalf("存在即读：路径要稳定，期望 %s，得到 %s", got, again)
	}
	kept, err := os.ReadFile(got)
	if err != nil || string(kept) != "# 用户改过的\n" {
		t.Fatalf("存在即读：不得改写已存在的配置（得到 %q, err=%v）", kept, err)
	}
}

func TestEnsureConfigFilePrefersCWDConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "config", bootseed.RuntimeConfigName)
	if err := os.WriteFile(mine, []byte("# 我自己的运行参数\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ensureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack()); got != mine {
		t.Fatalf("CWD 的 config/ 是链上第一候选：期望 %s，得到 %s", mine, got)
	}
}

func TestEnsureConfigFileKeepsLegacyRootFallback(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	legacy := filepath.Join(dir, bootseed.PermissionConfigName)
	if err := os.WriteFile(legacy, []byte("permission:\n  rules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ensureConfigFile(bootseed.PermissionConfigName, bootseed.PermissionConfigPack()); got != legacy {
		t.Fatalf("根目录回退是链上第二候选（历史兼容）：期望 %s，得到 %s", legacy, got)
	}
}

// TestResolveToolDirFollowsChain：仓库里放着脚本时命中它；没放时（CI 等
// 干净检出）按"缺 main.py 就跳过登记"的既有安全口径返回 false——两种情形都不许
// 让命令带着一个不存在的脚本被登记。
func TestResolveAutoGetJobsDirFollowsChain(t *testing.T) {
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoTool := filepath.Join(repo, "local", "tools", "auto_get_jobs")
	_, repoErr := os.Stat(filepath.Join(repoTool, "main.py"))
	dir, ok := resolveAutoGetJobsDir()
	if repoErr == nil {
		if !ok {
			t.Fatal("仓库里放着 main.py，白名单命令脚本目录应当命中")
		}
		if dir != repoTool {
			t.Fatalf("应命中 CWD 相对的那份：期望 %s，得到 %s", repoTool, dir)
		}
		return
	}
	if ok {
		t.Fatal("没有 main.py 时不得登记白名单命令（登记即信任、argv 固定直传）")
	}
}

// TestResolveToolDirSeedsSkeletonWithoutRegistering：工具目录两处都没有时初始化
// 骨架（README + .env.example）——但骨架里没有 main.py，所以命令仍不登记；用户把
// 脚本放进去之后，同一个目录立刻命中。
func TestResolveToolDirSeedsSkeletonWithoutRegistering(t *testing.T) {
	t.Chdir(t.TempDir()) // CWD 里没有 local/
	exeDir := t.TempDir()
	relative := filepath.Join("local", "tools", "auto_get_jobs")
	seededRoot := filepath.Join(exeDir, relative)

	dir, ok := resolveToolDir(relative, exeDir, bootseed.AutoGetJobsPack())
	if ok || dir != "" {
		t.Fatalf("骨架里没有 main.py：不得返回可用目录（ok=%v dir=%q）", ok, dir)
	}
	for _, rel := range []string{"README.md", ".env.example"} {
		data, readErr := os.ReadFile(filepath.Join(seededRoot, rel))
		if readErr != nil {
			t.Fatalf("缺失即初始化：骨架应落盘 %s（%v）", rel, readErr)
		}
		if len(data) == 0 {
			t.Fatalf("骨架 %s 不该是空文件", rel)
		}
	}
	env, err := os.ReadFile(filepath.Join(seededRoot, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "API_KEY=\n") {
		t.Fatal(".env.example 只能是模板：键名留着、值必须为空（真实密钥不进默认数据）")
	}

	if err := os.WriteFile(filepath.Join(seededRoot, "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, ok = resolveToolDir(relative, exeDir, bootseed.AutoGetJobsPack())
	if !ok || dir != seededRoot {
		t.Fatalf("脚本放好之后应命中同一个目录：ok=%v dir=%q 期望 %q", ok, dir, seededRoot)
	}
}

func TestResolveToolDirPrefersCWDCopy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	relative := filepath.Join("local", "tools", "auto_get_jobs")
	cwdTool := filepath.Join(dir, relative)
	exeTool := filepath.Join(t.TempDir(), relative)
	for _, tool := range []string{cwdTool, exeTool} {
		if err := os.MkdirAll(tool, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tool, "main.py"), []byte(tool), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir, ok := resolveToolDir(relative, filepath.Dir(exeTool), bootseed.AutoGetJobsPack())
	if !ok || dir != cwdTool {
		t.Fatalf("候选链第一个存在的胜出（CWD 那份）：期望 %s，得到 %q (ok=%v)", cwdTool, dir, ok)
	}
}
