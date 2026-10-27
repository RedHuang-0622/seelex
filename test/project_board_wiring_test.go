package test

import (
	"github.com/RedHuang-0622/seelex/kernel/board"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"

	// project_board_wiring_test.go — 项目看板的**装配门**验收（M3，wi-m3-switch）。
	//
	// 三件事，一件一个判据：
	//   - 关就是关（enabled:false）：不返回订阅口、不落任何文件；
	//   - 拼错的 hook 档名**显式失败**（不静默降级成"开了但没生效"）；
	//   - 开着时：真实存储面（store.Router）+ 真实同步器 ⇒ 命中触发面的工具完成后
	//     投影真的落到 `<项目根>/.seelex/board/` 里（**数据根下一份都不落**），未命中的工具
	//     一行都不写，Close 可重入。
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/wiring/registry"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const projectBoardWiringSession = "session-board-wiring"

// projectBoardWiringStore 建一个临时 JSON 会话存储**与一个临时项目根**。
//
// 两者刻意是两个不同的目录：本轮的判据正是"投影不再落在数据根下，而是落在项目根的
// `.seelex/board/` 里"——把两个夹做成同一个临时目录，这半条判据就退化成同义反复。
func projectBoardWiringStore(t *testing.T) (router *store.Router, dataRoot, projectRoot string) {
	t.Helper()
	dataRoot = t.TempDir()
	projectRoot = t.TempDir()
	router, err := store.NewRouter(dataRoot)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router, dataRoot, projectRoot
}

// projectBoardWiringDeps 造一份最小活体投影：一条 todo + 一个里程碑，外加**落点解析器**。
//
// 取数一律是**函数**（与生产同形）：本用例不碰 Service，只钉装配门的判据。解析器与生产
// 同形——装配段注入的就是 `app.ProjectRootFor`（会话 → 它所属工作区的项目根）。
func projectBoardWiringDeps(projectRoot string) (
	func(string) []model.WorkItem,
	func(string) *store.TeamworkPlan,
	func(string) *store.TeamworkState,
	func(string) string,
) {
	rows := func(sessionID string) []model.WorkItem {
		return []model.WorkItem{{
			ID: "todo:1", SessionID: sessionID, Kind: "todo", Phase: "tasklist",
			Task: "调研", Status: "doing",
		}}
	}
	plan := func(string) *store.TeamworkPlan {
		return &store.TeamworkPlan{
			TeamID: "team-1", Version: 1,
			Milestones: []store.TeamworkMilestone{{ID: "m1", Name: "第一步"}},
		}
	}
	state := func(string) *store.TeamworkState {
		return &store.TeamworkState{State: store.TeamworkStateClosed, Milestones: map[string]string{"m1": "done"}}
	}
	return rows, plan, state, func(string) string { return projectRoot }
}

// projectBoardMarkdown 读回人读件（`BOARD.md`，落在**看板根**、不分会话）：它是
// "投影真的落在项目目录里"的第二张判据面（外部 agent 直接读它，不经任何工具）。
func projectBoardMarkdown(t *testing.T, projectRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.ProjectBoardDir(projectRoot), store.ProjectBoardMarkdownFile))
	if err != nil {
		return ""
	}
	return string(data)
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
		if entry.Name() == store.ProjectBoardWorkFile || entry.Name() == store.ProjectBoardTeamworkFile {
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

// TestProjectBoardDisabledInjectsNothing：enabled=false ⇒ 零订阅口、零文件（两个根都没有）。
func TestProjectBoardDisabledInjectsNothing(t *testing.T) {
	router, dataRoot, projectRoot := projectBoardWiringStore(t)
	rows, plan, state, projectRootFor := projectBoardWiringDeps(projectRoot)

	sink, closeSink, err := registry.ProjectBoardSinkFor(
		limit.ProjectBoardLimits{Enabled: false, Hook: "", FlushIdleMS: 20},
		router, rows, plan, state, projectRootFor, func() int { return limit.Get().EvidenceChars },
	)
	if err != nil {
		t.Fatalf("registry.ProjectBoardSinkFor: %v", err)
	}
	if sink != nil {
		t.Fatal("enabled=false 必须不返回订阅口（关就是关）")
	}
	closeSink() // 空收尾必须可调用（装配根无条件 defer 它）
	if got := projectBoardArtifacts(t, projectRoot); len(got) != 0 {
		t.Fatalf("关着的看板写了文件（项目根）：%v", got)
	}
	if got := projectBoardArtifacts(t, dataRoot); len(got) != 0 {
		t.Fatalf("关着的看板写了文件（数据根）：%v", got)
	}
}

// TestProjectBoardRejectsUnknownHook：拼错的档名显式失败（装配根因此启动失败）。
func TestProjectBoardRejectsUnknownHook(t *testing.T) {
	router, _, projectRoot := projectBoardWiringStore(t)
	rows, plan, state, projectRootFor := projectBoardWiringDeps(projectRoot)

	sink, closeSink, err := registry.ProjectBoardSinkFor(
		limit.ProjectBoardLimits{Enabled: true, Hook: "todo-list", FlushIdleMS: 20},
		router, rows, plan, state, projectRootFor, func() int { return limit.Get().EvidenceChars },
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
		if _, closeIdle, err := registry.ProjectBoardSinkFor(
			limit.ProjectBoardLimits{Enabled: true, Hook: hook, FlushIdleMS: 20},
			router, rows, plan, state, projectRootFor, func() int { return limit.Get().EvidenceChars },
		); err != nil {
			t.Fatalf("hook=%q 被拒绝：%v", hook, err)
		} else {
			closeIdle()
		}
	}
}

// TestProjectBoardEnabledWritesArtifacts：开着时命中触发面才落盘；落点在**项目根**的
// `.seelex/board/`（数据根零文件）；Close 可重入。
func TestProjectBoardEnabledWritesArtifacts(t *testing.T) {
	router, dataRoot, projectRoot := projectBoardWiringStore(t)
	rows, plan, state, projectRootFor := projectBoardWiringDeps(projectRoot)

	sink, closeSink, err := registry.ProjectBoardSinkFor(
		limit.ProjectBoardLimits{Enabled: true, Hook: board.ProjectBoardHookModeTodoList, FlushIdleMS: 20},
		router, rows, plan, state, projectRootFor, func() int { return limit.Get().EvidenceChars },
	)
	if err != nil {
		t.Fatalf("registry.ProjectBoardSinkFor: %v", err)
	}

	// ① 未命中触发面（read_file 不在 todo_list 档）：一行都不写。
	sink.OnToolComplete("read_file", nil, projectBoardWiringSession)
	time.Sleep(80 * time.Millisecond)
	if got := projectBoardArtifacts(t, projectRoot); len(got) != 0 {
		t.Fatalf("未命中触发面却落了盘：%v", got)
	}

	// ② 命中触发面（todo_add）：worktable.jsonl 与 teamwork.jsonl 各落一份。
	sink.OnToolComplete("todo_add", nil, projectBoardWiringSession)
	waitForProjectBoardArtifacts(t, projectRoot, store.ProjectBoardWorkFile)
	// 两个文件是同一次 flush 里先后落的（work 先行）：等第二个也要轮询，不能假设它
	// 与第一个在同一瞬间可见。
	artifacts := waitForProjectBoardArtifacts(t, projectRoot, store.ProjectBoardTeamworkFile)
	if !containsArtifact(artifacts, store.ProjectBoardWorkFile) {
		t.Fatalf("work 行没落 worktable.jsonl：%v", artifacts)
	}
	boardPrefix := filepath.ToSlash(filepath.Join(".seelex", "board")) + "/"
	for _, artifact := range artifacts {
		// 落点是**项目目录 + 会话目录**（`<项目根>/.seelex/board/<会话 ID>/<名字>`）：
		// 相对路径、无 hash 目录、无数据根副本。会话 ID 就在目录名上（用户口径：会话
		// 通过 sessionid 自维护自己的内容），但**工作区 ID（`ws-`）不许进路径**。
		if !strings.HasPrefix(artifact, boardPrefix) {
			t.Fatalf("落点不在 <项目根>/.seelex/board/ 之下：%s", artifact)
		}
		if !strings.HasPrefix(artifact, boardPrefix+projectBoardWiringSession+"/") {
			t.Fatalf("落点不是这个会话自己的目录：%s", artifact)
		}
		if strings.Contains(artifact, "ws-") {
			t.Fatalf("落点泄漏工作区 ID：%s", artifact)
		}
		if filepath.IsAbs(artifact) {
			t.Fatalf("落点不是相对路径：%s", artifact)
		}
	}
	// ③ 数据根取反：搬走之后那份**不再出现、也不双写**。
	if got := projectBoardArtifacts(t, dataRoot); len(got) != 0 {
		t.Fatalf("投影仍落在数据根下（本轮要求搬走且不双写）：%v", got)
	}
	// ④ 人读件在**看板根**（全项目汇总，不分会话），两份 jsonl 在会话目录里
	// （外部 agent 直接读 BOARD.md，不经任何工具）。
	if markdown := projectBoardMarkdown(t, projectRoot); markdown == "" {
		t.Fatal("BOARD.md 没落盘")
	} else if !strings.Contains(markdown, projectBoardWiringSession) {
		t.Fatalf("BOARD.md 没带上会话列（人读件应当能指到会话）：%q", markdown)
	}
	// 行的 scope 里也写**字面 id**：产物自描述（外部 agent 光看 jsonl 不必反推目录名）。
	body := readProjectBoardArtifact(t, projectRoot, store.ProjectBoardWorkFile)
	if !strings.Contains(body, projectBoardWiringSession) {
		t.Fatalf("投影行里应当带字面会话 id（scope.session）：%s", body)
	}

	// ③ 失败的工具完成：不标脏（I4：失败的调用不改动"活"）。
	before := readProjectBoardArtifact(t, projectRoot, store.ProjectBoardWorkFile)
	sink.OnToolComplete("todo_add", errProjectBoardWiring, projectBoardWiringSession)
	time.Sleep(80 * time.Millisecond)
	if after := readProjectBoardArtifact(t, projectRoot, store.ProjectBoardWorkFile); after != before {
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
