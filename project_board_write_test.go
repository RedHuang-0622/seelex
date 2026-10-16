package main

// project_board_write_test.go — 项目看板的**写侧端到端**用例：模型真的调 todo_add，
// 项目根的 `.seelex/board/` 里就真的多出东西。
//
// 为什么必须有这一条（2026-10-15 补）：`project_board_e2e_test.go` 钉的是**读**面，
// `TestProjectBoardEnabledWritesArtifacts` 钉的是"同步器 + 真实存储面"，两者之间那段
// ——**工具完成段 → 同步器取数（活体工作表格）→ 落盘**——此前没有任何用例覆盖，于是
// 文档写着"已实现"，而盘面上一个文件都没有也没人发现。它同时把契约 §9.3 R-2 的假设
// 坐实为实测：**按生产口径新建的会话（草稿早分配 SID → 物化复用同一 SID → 视图会话 =
// 运行时当前 task 会话），`WorkTableRowsFor` 取得到该会话的行**；换一条不经过该口径的
// 会话（视图会话 ≠ 运行时当前 task 会话）就会取到空集 ⇒ I6 退场、什么都不落——这条
// 差别正是"文档说实现了、实机没东西"的根源，写在用例里防误读。

import (
	"context"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const projectBoardWriteItem = "写侧端到端标记 4c17b2"

func TestProjectBoardWriteEndToEndFromTodoTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectRoot := t.TempDir()
	provider := newProjectBoardE2EServer(t)
	AccountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	limits := limit.Limits{}
	limits.ProjectBoard.Enabled = true // 装配门：关着时本用例的前提就不成立（另有用例钉"关就是关"）
	harness := newFullChainHarnessWithLimits(t, AccountsPath, projectRoot, 30*time.Second, true, limits)
	// 按**生产装配**挂写侧：同步器 + 会话→项目根解析器（main.run 里是同一对调用）。
	sink, closeSink, err := projectBoardSinkFor(
		limits.ProjectBoard, harness.st,
		harness.app.WorkTableRowsFor, harness.app.ProjectBoardTeamPlanFor,
		harness.app.ProjectBoardTeamStateFor, harness.app.ProjectRootFor,
	)
	if err != nil {
		t.Fatalf("projectBoardSinkFor: %v", err)
	}
	defer closeSink()
	harness.app.SetProjectBoardSink(sink)
	harness.st.SetProjectBoardRootFor(harness.app.ProjectRootFor)

	// 生产口径的新会话：建工作区 → 新建草稿（早分配 SID）→ 绑定工作区 → 首次提交物化。
	// 草稿 SID 就是物化后的会话 ID（openSessionEngine 复用早分配 SID），所以"视图会话 =
	// 运行时当前 task 会话"这条前提在这里是**真的**（不是用例替它假设的）。
	if err := harness.app.CreateWorkspace("board-write", projectRoot, ""); err != nil {
		t.Fatalf("建工作区: %v", err)
	}
	if err := harness.app.BeginNewSession(); err != nil {
		t.Fatalf("新建会话: %v", err)
	}
	draftSession := harness.app.Snapshot().Session.ID
	workspaceID := ""
	for _, item := range harness.app.Snapshot().Workspaces {
		if item.RootPath == projectRoot {
			workspaceID = item.ID
		}
	}
	if workspaceID == "" {
		t.Fatalf("工作区没进快照：%+v", harness.app.Snapshot().Workspaces)
	}
	if err := harness.app.BindWorkspace(workspaceID); err != nil {
		t.Fatalf("绑定工作区: %v", err)
	}

	provider.setScript(
		projectBoardE2EScript{tool: "todo_add", args: `{"item":"` + projectBoardWriteItem + `"}`},
		projectBoardE2EScript{},
	)
	if err := harness.app.Submit(ctx, "请调用 todo_add 加一条待办，然后只回复『好』。"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
	snapshot := harness.app.Snapshot()
	if snapshot.Chat.Error != "" {
		t.Fatalf("回合失败: %s", snapshot.Chat.Error)
	}
	sessionID := snapshot.Session.ID
	if sessionID == "" || sessionID != draftSession {
		t.Fatalf("物化后的会话 ID = %q，草稿 SID = %q（应复用同一 SID）", sessionID, draftSession)
	}
	if root := harness.app.ProjectRootFor(sessionID); root != projectRoot {
		t.Fatalf("会话的项目根 = %q，期望 %q", root, projectRoot)
	}
	if rows := harness.app.WorkTableRowsFor(sessionID); len(rows) == 0 {
		t.Fatal("活体工作表格里没有这个会话的行：写侧一定会退场（I6），本用例的前提不成立")
	}

	// 工具完成 → 去抖 → 落盘（去抖窗是时间驱动的，不能假设立即到）。
	artifacts := waitForProjectBoardArtifacts(t, projectRoot, store.ProjectBoardWorkFile)
	// 全项目扫描：投影**只**该有 `.seelex/board/<会话 ID>/worktable.jsonl` 这一份——
	//   - 不含 teamwork.jsonl：本会话没有团队计划，空则退场（I6）；
	//   - 不含数据根下的副本：落点已经与数据根解耦，只认项目目录。
	want := filepath.ToSlash(filepath.Join(store.ProjectBoardDir(projectRoot), sessionID, store.ProjectBoardWorkFile))
	want, _ = filepath.Rel(projectRoot, filepath.FromSlash(want))
	want = filepath.ToSlash(want)
	if len(artifacts) != 1 || artifacts[0] != want {
		t.Fatalf("全项目扫描到的投影 = %v，期望只有 %q", artifacts, want)
	}

	// 行内容：模型加的那条待办，按会话归属落在行里。
	raw, err := harness.st.ReadProjectBoardRows(projectRoot, sessionID, store.ProjectBoardWorkFile)
	if err != nil {
		t.Fatalf("读回投影: %v", err)
	}
	folded := store.FoldProjectBoardRows(raw, nil)
	if len(folded) != 1 {
		t.Fatalf("折叠读 = %d 行，期望 1：%#v", len(folded), folded)
	}
	if task, _ := folded[0].Payload["task"].(string); !strings.Contains(task, projectBoardWriteItem) {
		t.Fatalf("投影行 = %#v，期望 task 含 %q", folded[0].Payload, projectBoardWriteItem)
	}
	if folded[0].Scope.Session != sessionID {
		t.Fatalf("投影行归属会话 = %q，期望 %q", folded[0].Scope.Session, sessionID)
	}
	// 人读件（外部 agent 不经任何工具、直接读盘的那一份）。
	// 它与两份 jsonl 是同一份投影的两种落法（行先落、人读件随后重渲染），所以按"最终
	// 到齐"等它——不能假设两者在同一瞬可读。
	if markdown := waitForProjectBoardMarkdown(t, projectRoot, projectBoardWriteItem); !strings.Contains(markdown, projectBoardWriteItem) {
		t.Fatalf("BOARD.md 里看不到这条待办（落点目录 = %v）：\n%s",
			projectBoardDirEntries(t, projectRoot), markdown)
	}
}

// waitForProjectBoardMarkdown 等 `BOARD.md` 落到含 needle 的那一版（超时报当前目录内容）。
func waitForProjectBoardMarkdown(t *testing.T, projectRoot, needle string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		markdown := projectBoardMarkdown(t, projectRoot)
		if strings.Contains(markdown, needle) || time.Now().After(deadline) {
			return markdown
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// projectBoardDirEntries 列出看板落点目录的文件名（诊断用）。
func projectBoardDirEntries(t *testing.T, projectRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(store.ProjectBoardDir(projectRoot))
	if err != nil {
		return []string{"<读目录失败: " + err.Error() + ">"}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
