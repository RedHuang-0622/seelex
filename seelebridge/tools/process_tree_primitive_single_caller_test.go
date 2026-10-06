package tools

// process_tree_primitive_single_caller_test.go — 机械门禁：进程树装配只有一份（③C 的另一半）。
//
// `process_tree_assembly_test.go` 钉的是"两条链构造出的 cmd 在起命令位 / 整组终止 /
// WaitDelay / 进程组位上一致"；这条钉的是**原语的生产调用点**这件事本身：
//
//	① `security.NewProcessTree` / `security.ConfigureProcessTree` 在 seelebridge/ 的非
//	   测试代码里**只允许出现在 `newProcessTreeCommand` 里面**——即"装配序列只有一处"；
//	② 两条链都转调了这份装配（同步 `router.go`、后台 `async_run.go`），且后台链里
//	   **不再自己 `exec.CommandContext`**（旧的那份内联装配是"删除"不是"另留一份"）。
//
// 为什么要有它：装配回归用例只能钉住"传进去的输入没变"，钉不住"没人偷偷在第三处再拼一遍"
// ——那正是本波要收掉的东西（AGENTS §8：证据是"只剩一份实现"，不是"用例全绿"）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// sceneRootFromToolsPkg 是包目录（seelebridge/tools）的上一级 = 本域根。
	sceneRootFromToolsPkg = ".."
	// assemblyHome 是允许出现进程树原语的那一份文件（装配的**家**）。
	assemblyHome = "router.go"
	// assemblyFunc 是那一份装配（原语只许在它里面出现）。
	assemblyFunc = "func newProcessTreeCommand("
)

// scanPrimitiveCallers 扫 seelebridge/ 的非测试 .go 文件，回报每处进程树原语的
// 文件、行号与**所在函数**（向上找最近一个 `func ` 起始行）。
func scanPrimitiveCallers(t *testing.T) [][3]string {
	t.Helper()
	var hits [][3]string
	err := filepath.Walk(sceneRootFromToolsPkg, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path == sceneRootFromToolsPkg {
				return nil // 根就是 ".."（名字以 '.' 开头），不是要跳过的隐藏目录
			}
			if info.Name() == "testdata" || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			if !strings.Contains(line, "security.NewProcessTree") && !strings.Contains(line, "security.ConfigureProcessTree") {
				continue
			}
			hits = append(hits, [3]string{filepath.ToSlash(path), line, enclosingFunc(lines, index)})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫 %s 失败: %v", sceneRootFromToolsPkg, err)
	}
	return hits
}

// enclosingFunc 向上找第 index 行所属的函数签名行（找不到回空串）。
func enclosingFunc(lines []string, index int) string {
	for scan := index; scan >= 0; scan-- {
		if strings.HasPrefix(lines[scan], "func ") {
			return lines[scan]
		}
	}
	return ""
}

// seamNewTreeLine 是唯一允许出现在装配助手**之外**的那一行：建树工厂的注入缝
// （只为让"Job 建不出来"这条退化路径可测）。它是**一处引用**，不是第二份装配。
const seamNewTreeLine = "var newExecProcessTree = security.NewProcessTree"

func TestProcessTreePrimitivesHaveOneProductionCaller(t *testing.T) {
	hits := scanPrimitiveCallers(t)
	if len(hits) == 0 {
		t.Fatal("没扫到任何进程树原语调用点：装配助手不见了？")
	}
	inAssembly, seams := 0, 0
	for _, hit := range hits {
		file, line, fn := hit[0], hit[1], hit[2]
		if filepath.Base(file) != assemblyHome {
			t.Fatalf("进程树原语只许出现在 %s 的装配助手里，实际另有 %s:%s", assemblyHome, file, line)
		}
		if strings.TrimSpace(line) == seamNewTreeLine {
			seams++
			continue // 注入缝：见 seamNewTreeLine 注释
		}
		if !strings.HasPrefix(fn, assemblyFunc) {
			t.Fatalf("%s 的 %s 不在 %s 里（所在函数 %q）：装配又分家了", file, line, assemblyFunc, fn)
		}
		inAssembly++
	}
	// 装配里恰好一处原语（进程组位 `security.ConfigureProcessTree`；建树走注入缝
	// `newExecProcessTree` 那一行，见下）——少了说明装配被拆散，多了说明有人加料。
	if inAssembly != 1 {
		t.Fatalf("装配里的原语调用点 = %d 处，want 1（ConfigureProcessTree；建树经注入缝）", inAssembly)
	}
	if seams != 1 {
		t.Fatalf("建树工厂注入缝 = %d 处，want 1（%q）", seams, seamNewTreeLine)
	}
}

func TestBothChainsCallTheSingleAssembly(t *testing.T) {
	for name, mustHave := range map[string][]string{
		"router.go":    {"newProcessTreeCommand(", "startWithProcessTree("},
		"async_run.go": {"newProcessTreeCommand(", "startWithProcessTree("},
	} {
		source := readPackageFile(t, name)
		for _, needle := range mustHave {
			if !strings.Contains(source, needle) {
				t.Fatalf("%s 必须转调同一份装配：找不到 %q", name, needle)
			}
		}
	}
	// 后台链里旧的那份内联装配是**删除**：它不许再自己起命令。
	if source := readPackageFile(t, "async_run.go"); strings.Contains(source, "exec.CommandContext(") {
		t.Fatal("async_run.go 还有自己的 exec.CommandContext：旧装配没删干净（只收一份，不是两边都留）")
	}
}

func readPackageFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	return string(data)
}
