package main

// project_board_wiring_test.go — 项目看板的**装配门**验收（M3，wi-m3-switch）。
//
// 三件事，一件一个判据：
//   - 关就是关（enabled:false）：不返回订阅口、不落任何文件；
//   - 拼错的 hook 档名**显式失败**（不静默降级成"开了但没生效"）；
//   - 开着时：真实存储面（sessionstore.Router）+ 真实同步器 ⇒ 命中触发面的工具完成后
//     投影真的落到 <root>/project-board/... 里，未命中的工具一行都不写，Close 可重入。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const projectBoardWiringSession = "session-board-wiring"

// projectBoardWiringStore 建一个临时 JSON 会话存储（投影落在它的数据根之下）。
func projectBoardWiringStore(t *testing.T) (*sessionstore.Router, string) {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router, root
}

// projectBoardWiringDeps 造一份最小活体投影：一条 todo + 一个里程碑。
//
// 取数一律是**函数**（与生产同形）：本用例不碰 Service，只钉装配门的判据。
func projectBoardWiringDeps() (
	func(string) []core.WorkItem,
	func(string) *sessionstore.TeamworkPlan,
	func(string) *sessionstore.TeamworkState,
	func(string) string,
) {
	rows := func(sessionID string) []core.WorkItem {
		return []core.WorkItem{{
			ID: "todo:1", SessionID: sessionID, Kind: "todo", Phase: "tasklist",
			Task: "调研", Status: "doing",
		}}
	}
	plan := func(string) *sessionstore.TeamworkPlan {
		return &sessionstore.TeamworkPlan{
			TeamID: "team-1", Version: 1,
			Milestones: []sessionstore.TeamworkMilestone{{ID: "m1", Name: "第一步"}},
		}
	}
	state := func(string) *sessionstore.TeamworkState {
		return &sessionstore.TeamworkState{State: sessionstore.TeamworkStateClosed, Milestones: map[string]string{"m1": "done"}}
	}
	return rows, plan, state, func(string) string { return "ws-board-wiring" }
}

// projectBoardArtifacts 列出投影落点下的全部文件（相对数据根的斜杠路径）。
func projectBoardArtifacts(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == sessionstore.ProjectBoardWorkFile || entry.Name() == sessionstore.ProjectBoardTeamworkFile {
			relative, _ := filepath.Rel(root, path)
			found = append(found, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk 存储根: %v", err)
	}
	return found
}

// waitForProjectBoardArtifacts 轮询等投影落盘（去抖窗是时间驱动的，不能假设立即到）。
func waitForProjectBoardArtifacts(t *testing.T, root, file string) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, artifact := range projectBoardArtifacts(t, root) {
			if strings.HasSuffix(artifact, file) {
				return projectBoardArtifacts(t, root)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等 %s 落盘超时（现有落点：%v）", file, projectBoardArtifacts(t, root))
	return nil
}

// TestProjectBoardDisabledInjectsNothing：enabled=false ⇒ 零订阅口、零文件。
func TestProjectBoardDisabledInjectsNothing(t *testing.T) {
	router, root := projectBoardWiringStore(t)
	rows, plan, state, projectID := projectBoardWiringDeps()

	sink, closeSink, err := projectBoardSinkFor(
		seelexctx.ProjectBoardLimits{Enabled: false, Hook: "", FlushIdleMS: 20},
		router, rows, plan, state, projectID,
	)
	if err != nil {
		t.Fatalf("projectBoardSinkFor: %v", err)
	}
	if sink != nil {
		t.Fatal("enabled=false 必须不返回订阅口（关就是关）")
	}
	closeSink() // 空收尾必须可调用（装配根无条件 defer 它）
	if got := projectBoardArtifacts(t, root); len(got) != 0 {
		t.Fatalf("关着的看板写了文件：%v", got)
	}
}

// TestProjectBoardRejectsUnknownHook：拼错的档名显式失败（装配根因此启动失败）。
func TestProjectBoardRejectsUnknownHook(t *testing.T) {
	router, _ := projectBoardWiringStore(t)
	rows, plan, state, projectID := projectBoardWiringDeps()

	sink, closeSink, err := projectBoardSinkFor(
		seelexctx.ProjectBoardLimits{Enabled: true, Hook: "todo-list", FlushIdleMS: 20},
		router, rows, plan, state, projectID,
	)
	if err == nil {
		closeSink()
		t.Fatal("未知 hook 档名必须显式报错（不静默降级）")
	}
	if sink != nil {
		t.Fatal("报错时不得返回订阅口")
	}
	if !strings.Contains(err.Error(), "todo-list") {
		t.Fatalf("报错文案要带上那个错值，得到：%v", err)
	}

	// 空串 = 未配置 ⇒ 缺省档 todo_list（与出厂档一致）。
	for _, hook := range []string{"", " todo_list ", "TOOL_COMPLETE"} {
		if _, closeIdle, err := projectBoardSinkFor(
			seelexctx.ProjectBoardLimits{Enabled: true, Hook: hook, FlushIdleMS: 20},
			router, rows, plan, state, projectID,
		); err != nil {
			t.Fatalf("hook=%q 被拒绝：%v", hook, err)
		} else {
			closeIdle()
		}
	}
}

// TestProjectBoardEnabledWritesArtifacts：开着时命中触发面才落盘，Close 可重入。
func TestProjectBoardEnabledWritesArtifacts(t *testing.T) {
	router, root := projectBoardWiringStore(t)
	rows, plan, state, projectID := projectBoardWiringDeps()

	sink, closeSink, err := projectBoardSinkFor(
		seelexctx.ProjectBoardLimits{Enabled: true, Hook: core.ProjectBoardHookModeTodoList, FlushIdleMS: 20},
		router, rows, plan, state, projectID,
	)
	if err != nil {
		t.Fatalf("projectBoardSinkFor: %v", err)
	}

	// ① 未命中触发面（read_file 不在 todo_list 档）：一行都不写。
	sink.OnToolComplete("read_file", nil, projectBoardWiringSession)
	time.Sleep(80 * time.Millisecond)
	if got := projectBoardArtifacts(t, root); len(got) != 0 {
		t.Fatalf("未命中触发面却落了盘：%v", got)
	}

	// ② 命中触发面（todo_add）：worktable.jsonl 与 teamwork.jsonl 各落一份。
	sink.OnToolComplete("todo_add", nil, projectBoardWiringSession)
	waitForProjectBoardArtifacts(t, root, sessionstore.ProjectBoardWorkFile)
	// 两个文件是同一次 flush 里先后落的（work 先行）：等第二个也要轮询，不能假设它
	// 与第一个在同一瞬间可见。
	artifacts := waitForProjectBoardArtifacts(t, root, sessionstore.ProjectBoardTeamworkFile)
	if !containsArtifact(artifacts, sessionstore.ProjectBoardTeamworkFile) {
		t.Fatalf("里程碑行没落 teamwork.jsonl：%v", artifacts)
	}
	for _, artifact := range artifacts {
		// 落点是 hash 派生的相对路径：不含绝对路径、不含字面会话 id（契约 §3）。
		if strings.Contains(artifact, projectBoardWiringSession) {
			t.Fatalf("落点泄漏字面会话 id：%s", artifact)
		}
		if filepath.IsAbs(artifact) {
			t.Fatalf("落点不是相对路径：%s", artifact)
		}
	}
	// 行的 scope 里写的是**字面 id**（人读需要），落点目录名走 hash——两者都要成立。
	body := readProjectBoardArtifact(t, root, sessionstore.ProjectBoardWorkFile)
	if !strings.Contains(body, projectBoardWiringSession) {
		t.Fatalf("投影行里应当带字面会话 id（scope.session）：%s", body)
	}

	// ③ 失败的工具完成：不标脏（I4：失败的调用不改动"活"）。
	before := readProjectBoardArtifact(t, root, sessionstore.ProjectBoardWorkFile)
	sink.OnToolComplete("todo_add", errProjectBoardWiring, projectBoardWiringSession)
	time.Sleep(80 * time.Millisecond)
	if after := readProjectBoardArtifact(t, root, sessionstore.ProjectBoardWorkFile); after != before {
		t.Fatal("失败的完成调用不该写盘")
	}

	// ④ Close 可重入（装配根会在退出路径上再调一次）。
	closeSink()
	closeSink()
}

var errProjectBoardWiring = &projectBoardWiringError{}

type projectBoardWiringError struct{}

func (*projectBoardWiringError) Error() string { return "tool failed" }

func containsArtifact(artifacts []string, name string) bool {
	for _, artifact := range artifacts {
		if strings.HasSuffix(artifact, name) {
			return true
		}
	}
	return false
}

// readProjectBoardArtifact 读回落点文件正文（追加式 JSONL 的原始字节）。
func readProjectBoardArtifact(t *testing.T, root, name string) string {
	t.Helper()
	for _, artifact := range projectBoardArtifacts(t, root) {
		if !strings.HasSuffix(artifact, name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact)))
		if err != nil {
			t.Fatalf("读投影文件: %v", err)
		}
		return string(data)
	}
	return ""
}
