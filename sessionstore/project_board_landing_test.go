package sessionstore

// project_board_landing_test.go — 落点搬迁的判据（**本节即本轮第一要务**）。
//
// 契约（docs/arch/seelex-project-board.md §3）：
// 项目看板的物理落点是**项目目录**（`<项目根>/.seelex/board/`），而不是数据根下的
// `project-board/project-<hash>/…`。本文件把这条判据钉成回归：数据根下不得再出现
// `project-board/`，投影必须出现在项目根的 `.seelex/board/` 下。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjectBoardMustNotLandUnderDataRoot：投影不得再出现在数据根下。
//
// 这条用例就是本轮的**红灯先行**用例：搬迁前它报的正是
// "投影仍落在数据根的 hash 目录（…/sessions-json/project-board）"。
func TestProjectBoardMustNotLandUnderDataRoot(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatalf("追加投影失败: %v", err)
	}
	legacy := filepath.Join(repository.root, "project-board")
	if _, err := os.Stat(legacy); err == nil {
		t.Fatalf("投影仍落在数据根的 hash 目录（%s）——本轮要求搬到 <项目根>/.seelex/board/", legacy)
	}
}

// TestProjectBoardLandsInProjectBoardDir：正面判据——投影出现在项目目录下。
func TestProjectBoardLandsInProjectBoardDir(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatalf("追加投影失败: %v", err)
	}
	boardDir := filepath.Join(projectRoot, ".seelex", "board")
	for _, name := range []string{ProjectBoardWorkFile, ProjectBoardMarkdownFile} {
		if _, err := os.Stat(filepath.Join(boardDir, name)); err != nil {
			t.Fatalf("投影文件 %s 没有出现在 %s：%v", name, boardDir, err)
		}
	}
}
