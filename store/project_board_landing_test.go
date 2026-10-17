package store

// project_board_landing_test.go — 落点搬迁的判据（**本节即本轮第一要务**）。
//
// 契约（docs/arch/seelex-project-board.md §3）：
// 项目看板的物理落点是**项目目录下按会话分的目录**
// （`<项目根>/.seelex/board/<会话 ID>/{worktable,teamwork}.jsonl`，人读件在
// `<项目根>/.seelex/board/BOARD.md`），而不是数据根下的 `project-board/project-<hash>/…`。
// 本文件把这条判据钉成回归：数据根下不得再出现 `project-board/`，投影必须出现在项目根。

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
	if _, err := router.AppendProjectBoardRows(projectRoot, "session-landing", ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), "session-landing")}); err != nil {
		t.Fatalf("追加投影失败: %v", err)
	}
	legacy := filepath.Join(repository.root, "project-board")
	if _, err := os.Stat(legacy); err == nil {
		t.Fatalf("投影仍落在数据根的 hash 目录（%s）——本轮要求搬到 <项目根>/.seelex/board/", legacy)
	}
}

// TestProjectBoardLandsInProjectBoardDir：正面判据——会话的投影出现在它自己的目录下，
// 人读件出现在看板根。
func TestProjectBoardLandsInProjectBoardDir(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-landing"
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), session)}); err != nil {
		t.Fatalf("追加投影失败: %v", err)
	}
	sessionFile := filepath.Join(projectRoot, ".seelex", "board", session, ProjectBoardWorkFile)
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("会话投影没有出现在 %s：%v", sessionFile, err)
	}
	markdown := filepath.Join(projectRoot, ".seelex", "board", ProjectBoardMarkdownFile)
	if _, err := os.Stat(markdown); err != nil {
		t.Fatalf("人读件没有出现在 %s：%v", markdown, err)
	}
}
