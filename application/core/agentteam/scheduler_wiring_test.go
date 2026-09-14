package agentteam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scheduler_wiring_test.go — 钉住 `TurnScheduler` 的**接线状态**，避免它再次被
// 误读成"已生效能力"（2026-09-14 review：本原语在生产是死代码，前端「工作顺序」
// 因此被当成"下一个谁发言"）。
//
// 本用例把两件事绑在一起：
//  1. 源码事实：仓库里除 `scheduler.go` 的定义与 `_test.go` 用例之外，不得出现
//     `NewTurnScheduler` 调用点（= 尚未接线）；
//  2. 文档声明：本包 README 必须显式写着它尚未接线。
//
// 一旦有人真的把它接上（新增生产调用点），本用例会红——这不是阻碍接线，而是
// 提醒同步 README「接线现状」表与本用例的判据（接线是个需要按设计稿落
// "每角色 agent loop" 的决策，不该悄悄发生）。

// schedulerCallSite 是扫描结果里的一条调用点。
type schedulerCallSite struct {
	path string
	test bool
}

// TestTurnSchedulerHasNoProductionCallSite 钉住"尚未接线"。
func TestTurnSchedulerHasNoProductionCallSite(t *testing.T) {
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

	for _, site := range callSites {
		if site.test {
			continue
		}
		t.Fatalf("发现 TurnScheduler 的生产调用点 %s：请同步 (1) 本包 README「接线现状」"+
			"表的该行、(2) 本用例的判据——接线意味着要为每个角色接 agent loop，"+
			"不应在 review 之外悄悄发生", site.path)
	}
	if len(callSites) == 0 {
		t.Fatal("未找到任何 NewTurnScheduler 调用点（连用例都没有？）：扫描逻辑可能失效")
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

// TestTurnSchedulerUnwiredStatusIsDocumented 钉住文档声明：README 必须显式写明
// 「尚未接线」，否则下次 review 又会把"装配得出来"读成"有人在轮转"。
func TestTurnSchedulerUnwiredStatusIsDocumented(t *testing.T) {
	root := schedulerRepoRoot(t)
	readme := filepath.Join(root, "application", "core", "agentteam", "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("读模块 README 失败: %v", err)
	}
	text := string(data)
	for _, want := range []string{"TurnScheduler", "尚未接线", "接线现状"} {
		if !strings.Contains(text, want) {
			t.Fatalf("README 缺少接线状态声明 %q（TurnScheduler 未接线的状态必须写在模块 README 里）", want)
		}
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
	case "tmp", "dist", "node_modules", "vendor":
		return true
	}
	return false
}

// schedulerRepoRoot 从包工作目录向上找到含 go.mod 的仓库根。
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
