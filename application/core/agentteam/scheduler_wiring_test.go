package agentteam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scheduler_wiring_test.go — 钉住 `TurnScheduler` 的**接线状态**。
//
// 历史（值得记住的教训）：2026-09-14 review 发现本原语在生产是死代码，前端
// 「工作顺序」因此被当成「下一个谁发言」。当时本用例钉的是"没有任何生产调用点"。
//
// 2026-09-15 `runtime.go` 的 `Runtime`（会话级发言调度运行态）落地后，本用例被
// 反转成"生产调用点必须存在，且只能在 runtime.go"，并禁止 README 再出现"尚未
// 接线"。那次反转其实把守卫变成了**虚假保证**：它只证明"有人 new 了一个
// TurnScheduler"，不证明任何消费者——而事实上 `Next()`/`Advance()` 至今没有生产
// 调用者，README 里"Next() 决定下一个该发言的成员"是一句错的声明，且"其实没人
// 消费 Next()"的唯一书面提示被删掉了。
//
// 2026-09-16 改成两条**可证伪**的断言：
//  1. 源码事实：生产调用点必须存在，且**只能**在 `runtime.go`（不允许在别处悄悄
//     再建一条不共享逃生记账的环——那会长出第二份顺序事实）；
//  2. 文档声明：README 必须点名生产消费面（`Order()` / `NoteTurn()`）并**如实**
//     声明 `Advance()`（经 `Runtime.Next`）没有生产消费者。声明与代码漂移就红。
//     注意本用例只断言"文档说了什么"，运行时的真实消费面由 `runtime_test.go` 的
//     行为用例与 `goal_coordinator` 的调用点承担。
//
// 2026-10-01（M4 §9 #3）：`TurnScheduler` 的 channel 投递路径
// （`Requests`/`Request`/`Next`）、顺序编辑三件（`Move`/`Remove`/`Restore`）与只读
// getter（`Prefix`）**已退场**——它们没有生产消费者（投递方"每个角色自己的 agent
// loop"从未落地，前端拖拽调序那条手势也已退场）。两条断言的**形状不变**，"没有
// 生产消费者"从此指 `Advance()`（经 `Runtime.Next`）；另加一条：**退场这件事必须
// 被记下来**（README 与 `scheduler.go` 都要写"已退场"），否则下一个人会照旧措辞
// 把它读成"还活着但没人用"。

// schedulerCallSite 是扫描结果里的一条调用点。
type schedulerCallSite struct {
	path string
	test bool
}

// TestTurnSchedulerHasSingleProductionCallSite 钉住"已接线且只有一处"。
func TestTurnSchedulerHasSingleProductionCallSite(t *testing.T) {
	root := schedulerRepoRoot(t)
	var callSites []schedulerCallSite
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && schedulerSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !schedulerHasCallSite(string(data)) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		callSites = append(callSites, schedulerCallSite{
			path: filepath.ToSlash(relative),
			test: strings.HasSuffix(entry.Name(), "_test.go"),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫描仓库源码失败: %v", err)
	}

	const expected = "application/core/agentteam/runtime.go"
	var production []string
	for _, site := range callSites {
		if site.test {
			continue
		}
		production = append(production, site.path)
	}
	if len(production) == 0 {
		t.Fatal("TurnScheduler 没有任何生产调用点：runtime.go 的 Runtime 必须持有它（顺序同步 + 逃生记账），否则链表顺序只是一张静态表")
	}
	if len(production) != 1 || production[0] != expected {
		t.Fatalf("TurnScheduler 的生产调用点应为且仅为 %s，实际 %v：不许在别处再建一条不共享逃生记账的环", expected, production)
	}

	found := false
	for _, site := range callSites {
		if site.path == "application/core/agentteam/scheduler_test.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("scheduler_test.go 应仍在用例里覆盖该原语: %+v", callSites)
	}
}

// TestTurnSchedulerWiredStatusIsDocumented 钉住文档声明：README 必须写明接线点
// 与逃生路径，也不能再留着"尚未接线"的旧结论（否则下次 review 又要重新推断）。
func TestTurnSchedulerWiredStatusIsDocumented(t *testing.T) {
	root := schedulerRepoRoot(t)
	readme := filepath.Join(root, "application", "core", "agentteam", "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("读模块 README 失败: %v", err)
	}
	text := string(data)
	for _, want := range []string{"TurnScheduler", "runtime.go", "逃生", "接线现状", "Order()", "NoteTurn()", "没有生产消费者", "已退场"} {
		if !strings.Contains(text, want) {
			t.Fatalf("README 缺少接线状态声明 %q（必须写清生产消费面 Order()/NoteTurn()、Advance() 没有生产消费者、已退场的四组，以及逃生路径）", want)
		}
	}
	if strings.Contains(text, "尚未接线") {
		t.Fatal("README 仍写着 TurnScheduler「尚未接线」：它已经被 Runtime 持有（只是 Next()/Advance() 还没人消费），旧措辞与代码事实矛盾")
	}
}

// schedulerHasCallSite 报告源码里是否有 `NewTurnScheduler` 的**调用**（定义不算）。
func schedulerHasCallSite(source string) bool {
	for index := 0; ; {
		offset := strings.Index(source[index:], "NewTurnScheduler(")
		if offset < 0 {
			return false
		}
		position := index + offset
		head := strings.TrimSpace(source[:position])
		if !strings.HasSuffix(head, "func") { // 定义（func NewTurnScheduler(...)）不是调用点
			return true
		}
		index = position + len("NewTurnScheduler(")
	}
}

// schedulerSkipDir 报告扫描时应跳过的目录（非源码树：构建产物/临时现场/依赖缓存）。
func schedulerSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "tmp", "dist", "node_modules", "vendor", "_tmp", "_scratch", "local":
		return true
	}
	return false
}

// schedulerRepoRoot 从包工作目录向上找到含 go.mod 的仓库根。
// TestTurnSchedulerWiredStatusIsDocumentedInSources 钉住**源码面**的接线声明与事实
// 一致。README 面已有同口径断言，但 2026-09-16 review 发现 scheduler.go 的文件头
// 注释仍写着"本原语尚未接线、没有任何生产调用点"——只读一次注释的人（人或子代理）
// 会据此把已接线的原语判成死代码，判据只查 README 时漏的正是这一类漂移。
//
// 两条断言：
//  1. 包内生产源码不得再出现字面量"尚未接线"（旧结论的唯一措辞）；
//  2. scheduler.go 必须点名它的生产调用点（runtime.go）与"接线状态"口径，并记下
//     2026-10-01 的退场（"已退场"）——退场不写在文件头，下一个人就只看得到一张
//     没人消费的接口表。
func TestTurnSchedulerWiredStatusIsDocumentedInSources(t *testing.T) {
	root := schedulerRepoRoot(t)
	pkgDir := filepath.Join(root, "application", "core", "agentteam")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("读调度器包目录失败: %v", err)
	}
	schedulerText := ""
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			t.Fatalf("读 %s 失败: %v", name, err)
		}
		text := string(data)
		scanned++
		if strings.Contains(text, "尚未接线") {
			t.Fatalf("%s 仍写着 TurnScheduler「尚未接线」的旧结论，与 runtime.go 的生产调用点矛盾", name)
		}
		if name == "scheduler.go" {
			schedulerText = text
		}
	}
	if scanned == 0 || schedulerText == "" {
		t.Fatal("未扫描到调度器包的生产源码")
	}
	for _, want := range []string{"接线状态", "生产调用点", "runtime.go", "已退场"} {
		if !strings.Contains(schedulerText, want) {
			t.Fatalf("scheduler.go 缺少接线声明 %q：生产调用点及其唯一性、以及已退场的接口必须写在该文件头", want)
		}
	}
}

func schedulerRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到仓库根（go.mod）")
		}
		dir = parent
	}
}
