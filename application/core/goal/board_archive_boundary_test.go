package goal

// board_archive_boundary_test.go — 静态守卫：**看板存档不进 goal 的判定面**（I1）。
//
// 设计契约（docs/arch/session-board-metadata-lifecycle.md §1/§8 I1）：存档是活体投影的
// 下游副本，只服务"重启后的快照恢复"。任何领域代码都不得读它做判定——只允许看板投影
// 的兜底路径读。这条铁律靠行为用例（TestGoalDomainDecisionsIgnoreBoardArchive）钉住了
// "结果不受存档影响"，这里再补一条静态断言：**动到存档类型/读写面的文件是有限的几个**。
//
// 为什么要有静态那条：行为用例只能覆盖已经写出来的路径；将来有人在 gate 或 controller
// 里顺手 `import sessionstore` 读一份 BoardMeta 当判据，行为用例未必立刻变红，而这条
// 断言会在文件级直接报出来（判定面文件名是它唯一的判据，不解析语义）。

import (
	"os"
	"strings"
	"testing"
)

// boardArchivePackageFiles 是允许提到看板存档面的文件（其余文件一概不得出现）。
//
//   - board_archive.go：写侧扇出（Save 之后刷存档、终态审计之后刷存档）+ 兜底的
//     active/history 组装，它就是"看板投影"这一侧的代码；
//   - sessionstore_store.go：把存档面注入适配器（装配，不做判定）。
var boardArchivePackageFiles = []string{"board_archive.go", "sessionstore_store.go"}

// boardArchiveMarkers 是"提到了看板存档面"的记号：类型名 + 读写方法名 + 取用面接口。
var boardArchiveMarkers = []string{"GoalBoardMeta", "ReadGoalBoard", "WriteGoalBoard", "SessionBoards"}

func TestGoalBoardArchiveStaysOutOfDecisionFiles(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录: %v", err)
	}
	allowed := make(map[string]struct{}, len(boardArchivePackageFiles))
	for _, name := range boardArchivePackageFiles {
		allowed[name] = struct{}{}
	}
	seen := make(map[string]int, len(allowed))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		count := 0
		for _, marker := range boardArchiveMarkers {
			count += strings.Count(string(source), marker)
		}
		if count == 0 {
			continue
		}
		if _, ok := allowed[name]; !ok {
			t.Fatalf("%s 提到了看板存档面（%s）：goal 的判定面不得读存档做判据——"+
				"存档只服务快照恢复，兜底读与写侧扇出都只允许待在 board_archive.go / sessionstore_store.go",
				name, strings.Join(boardArchiveMarkers, " / "))
		}
		seen[name] = count
	}
	// 反向断言：两个白名单文件必须真的提到（改名字/删功能后这条守卫不许静默失效）。
	for _, name := range boardArchivePackageFiles {
		if seen[name] == 0 {
			t.Fatalf("%s 里找不到看板存档面（守卫可能已经失效：文件被改名或功能被搬走）", name)
		}
	}
}
