package main

// project_board_i7_test.go — I7 回归：项目看板**不进模型上下文**（M4，T4.1/T4.2）。
//
// 两条判据，各钉一半：
//
//	T4.1 **装配前缀**：能力开/关两臂的 PromptLayers 渲染字节**逐字节相同**——
//	     投影不进 system/project 前缀，开关本身也不改前缀（能开能关的东西不许进缓存前缀）。
//	T4.2 **整份请求**：把一份**带标记**的投影真的写进存储面，再跑一轮真实请求：
//	     标记不得出现在 provider 请求体的**任何位置**（前缀与尾部都不进），投影文件名与
//	     投影 key 前缀同样不出现。
//
// 为什么要有 T4.2 这一半：只看前缀会漏掉"装配层把投影塞进请求尾部"这条路径——而尾部
// 同样是缓存前缀的一部分（前后两轮之间尾部一变，缓存就整段作废）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/wiring/registry"
)

// projectBoardMarker 是写进投影载荷的**唯一标记**：它只可能来自投影文件，
// 一旦出现在 provider 请求里，就说明投影进了上下文。
const projectBoardMarker = "BOARD-PROJECTION-MARKER-9f3a1c"

// projectBoardMarkerSession 是写投影的那条**测试会话**：它也必须是绑了工作区的那条会话
// （落点跟着会话所属项目走，未绑定的会话不落盘）。
const projectBoardMarkerSession = "session-marker"

// promptLayerBytes 把装配前缀（PromptLayers）渲染成可比较的字节。
//
// 为什么渲染而不是直接比较切片：这条判据问的是"发给 provider 的前缀字节一样不一样"，
// 而 PromptStack.Render 的拼接规则（kind 顺序、分隔）正是那串字节的生成规则——按同样
// 的字段顺序拼出来即可，不需要知道分隔符细节。
func promptLayerBytes(t *testing.T, app *application.Service) string {
	t.Helper()
	layers := app.PromptLayers()
	if len(layers) == 0 {
		t.Fatal("装配前缀为空：这条用例会退化成同义反复，先修装配")
	}
	var builder strings.Builder
	for _, layer := range layers {
		fmt.Fprintf(&builder, "%s\x1f%s\x1f%s\x1e", layer.Kind, layer.Name, layer.Text)
	}
	return builder.String()
}

// projectBoardAccountsFor 写一份指向本地 mock provider 的 accounts.yaml。
func projectBoardAccountsFor(t *testing.T, providerURL, root string) string {
	t.Helper()
	path := filepath.Join(root, "accounts.yaml")
	content := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", providerURL)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeMarkedProjectBoardProjection 往存储面写一份带标记的投影（两份文件都写）。
//
// 落点是**会话所属工作区的项目根**：`<项目根>/.seelex/board/`（一个项目一份平铺文件、
// 多会话共用一份，行内 scope.session 区分）。harness 不经过 main 的工作区绑定，所以这里
// 显式把这个会话绑到一个真实工作区（RootPath = 项目根），再按**生产解析器**
// `app.ProjectRootFor` 取落点——不绑就没有落点（契约 §3：未关联会话不落盘），
// "投影真的写出去了"这个前提也就不成立。
func writeMarkedProjectBoardProjection(t *testing.T, harness fullChainHarness, projectRoot string) {
	t.Helper()
	workspace, err := harness.workspaces.Create("board-i7-marker", projectRoot, "")
	if err != nil {
		t.Fatalf("建工作区: %v", err)
	}
	harness.workspaces.BindSession(projectBoardMarkerSession, workspace.ID)
	sinkRoot := harness.app.ProjectRootFor(projectBoardMarkerSession)
	if strings.TrimSpace(sinkRoot) == "" {
		t.Fatal("会话没有工作区绑定（没有落点）：这条用例会退化成同义反复")
	}
	rows := func(kind, key string) []store.ProjectBoardRow {
		return []store.ProjectBoardRow{{
			Schema: store.ProjectBoardSchema, Kind: kind, Key: key, Seq: 1, At: time.Now().Unix(),
			Scope:   store.ProjectBoardScope{Session: projectBoardMarkerSession},
			Payload: map[string]any{"task": projectBoardMarker, "status": "doing"},
		}}
	}
	if _, err := harness.st.AppendProjectBoardRows(sinkRoot, projectBoardMarkerSession, store.ProjectBoardWorkFile,
		rows(store.ProjectBoardKindWork, "work:marker")); err != nil {
		t.Fatalf("写 worktable 投影: %v", err)
	}
	if _, err := harness.st.AppendProjectBoardRows(sinkRoot, projectBoardMarkerSession, store.ProjectBoardTeamworkFile,
		rows(store.ProjectBoardKindMilestone, "milestone:marker")); err != nil {
		t.Fatalf("写 teamwork 投影: %v", err)
	}
	// 真写出去了才谈得上"它没进请求"：
	folded, err := harness.st.ReadProjectBoardRows(sinkRoot, projectBoardMarkerSession, store.ProjectBoardWorkFile)
	if err != nil || len(folded) != 1 {
		t.Fatalf("回读投影失败（folded=%d, err=%v）：用例前提没成立", len(folded), err)
	}
}

// TestProjectBoardPrefixBytesIdentical（T4.1）：开/关两臂的装配前缀逐字节相同。
func TestProjectBoardPrefixBytesIdentical(t *testing.T) {
	projectRoot := t.TempDir()
	provider := newFullChainPrefixServer(t, "read_file", filepath.Join(projectRoot, "i7_probe.txt"))
	AccountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	offRoot := t.TempDir()
	off := newFullChainHarnessWithLimits(t, AccountsPath, offRoot, 30*time.Second, true, limit.Limits{})
	offPrefix := promptLayerBytes(t, off.app)

	onRoot := t.TempDir()
	onLimits := limit.Limits{}
	onLimits.ProjectBoard.Enabled = true
	onLimits.ProjectBoard.Hook = "todo_list"
	onLimits.ProjectBoard.FlushIdleMS = 20
	on := newFullChainHarnessWithLimits(t, AccountsPath, onRoot, 30*time.Second, true, onLimits)
	// 能力开的这一臂按生产装配补齐两件事：注入订阅口 + 注册读取面。
	sink, closeSink, err := registry.ProjectBoardSinkFor(
		onLimits.ProjectBoard, on.st,
		on.app.WorkTableRowsFor, on.app.ProjectBoardTeamPlanFor,
		on.app.ProjectBoardTeamStateFor, on.app.ProjectRootFor,
	)
	if err != nil {
		t.Fatalf("registry.ProjectBoardSinkFor: %v", err)
	}
	defer closeSink()
	on.app.SetProjectBoardSink(sink)
	registry.RegisterProjectBoardReadTool(on.runtime, on.st, on.app)
	writeMarkedProjectBoardProjection(t, on, onRoot)

	onPrefix := promptLayerBytes(t, on.app)
	if offPrefix != onPrefix {
		t.Fatalf("能力开/关的装配前缀必须逐字节相同（I7）\n off=%q\n on =%q",
			clipForFailure(offPrefix, 800), clipForFailure(onPrefix, 800))
	}
	if strings.Contains(onPrefix, projectBoardMarker) {
		t.Fatal("投影标记出现在装配前缀里（投影成了输入）")
	}
}

// TestProjectBoardNeverReachesRequest（T4.2）：带标记的投影不进**任何** provider 请求。
func TestProjectBoardNeverReachesRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectRoot := t.TempDir()
	probePath := filepath.Join(projectRoot, "i7_probe.txt")
	if err := os.WriteFile(probePath, []byte("I7-PROBE-OK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := newFullChainPrefixServer(t, "read_file", probePath)
	AccountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	limits := limit.Limits{}
	limits.ProjectBoard.Enabled = true
	harness := newFullChainHarnessWithLimits(t, AccountsPath, projectRoot, 30*time.Second, true, limits)
	sink, closeSink, err := registry.ProjectBoardSinkFor(
		limits.ProjectBoard, harness.st,
		harness.app.WorkTableRowsFor, harness.app.ProjectBoardTeamPlanFor,
		harness.app.ProjectBoardTeamStateFor, harness.app.ProjectRootFor,
	)
	if err != nil {
		t.Fatalf("registry.ProjectBoardSinkFor: %v", err)
	}
	defer closeSink()
	harness.app.SetProjectBoardSink(sink)
	registry.RegisterProjectBoardReadTool(harness.runtime, harness.st, harness.app)
	writeMarkedProjectBoardProjection(t, harness, projectRoot)

	if err := harness.app.Submit(ctx, "只回复『收到』两个字，不要调用任何工具。"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("turn did not become idle: %v", err)
	}
	if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
		t.Fatalf("turn failed: %s", snapshot.Chat.Error)
	}

	bodies := provider.recorded()
	if len(bodies) == 0 {
		t.Fatal("没有记录到 provider 请求：用例失去意义")
	}
	// 这条判据问的是**投影内容**进不进上下文，而不是"工具面上有没有读取面"：
	// 读取面（project_board_read 的声明）本来就该出现在请求里（模型不知道有这把工具
	// 就永远读不到投影）。因此：
	//   - 正向控制：投影**行内容**的标记一律不得出现；
	//   - 负向控制：读取面本身必须在（否则这一臂没真开起来，用例会假绿）。
	forbidden := []string{
		projectBoardMarker, // 写在 payload.task 里的唯一标记
		`"work:marker"`,    // 投影 key（work 行）
		`"milestone:marker"`,
	}
	sawReadTool := false
	for index, body := range bodies {
		text := string(body)
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Fatalf("第 %d 个 provider 请求里出现了项目看板投影内容 %q（I7：投影不进上下文）\n%s",
					index, needle, clipForFailure(text, 600))
			}
		}
		if strings.Contains(text, "project_board_read") {
			sawReadTool = true
		}
	}
	if !sawReadTool {
		t.Fatal("读取面没出现在请求里：这一臂没真开起来（用例会假绿）")
	}
}
