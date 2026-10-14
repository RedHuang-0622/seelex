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
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// projectBoardMarker 是写进投影载荷的**唯一标记**：它只可能来自投影文件，
// 一旦出现在 provider 请求里，就说明投影进了上下文。
const projectBoardMarker = "BOARD-PROJECTION-MARKER-9f3a1c"

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
// 作用域取**存储当前工作区**：投影的落点是 `<root>/project-board/project-<hash>/session-<hash>`，
// 没有工作区就没有落点（存储面显式拒绝空 projectID）。harness 不经过 main 的工作区绑定，
// 因此调用点先 SetWorkspace 给它一个工作区——这也是"这份投影真的落在会话所属项目里"的前提。
func writeMarkedProjectBoardProjection(t *testing.T, store *sessionstore.Router, app *application.Service) {
	t.Helper()
	if strings.TrimSpace(store.Workspace()) == "" {
		store.SetWorkspace("ws-i7-marker")
	}
	projectID := app.ProjectIDFor("session-marker")
	if strings.TrimSpace(projectID) == "" {
		projectID = store.Workspace()
	}
	if strings.TrimSpace(projectID) == "" {
		t.Fatal("没有工作区作用域：投影无处可写，这条用例会退化成同义反复")
	}
	rows := func(kind, key string) []sessionstore.ProjectBoardRow {
		return []sessionstore.ProjectBoardRow{{
			Schema: sessionstore.ProjectBoardSchema, Kind: kind, Key: key, Seq: 1, At: time.Now().Unix(),
			Scope:   sessionstore.ProjectBoardScope{Project: projectID, Session: "session-marker"},
			Payload: map[string]any{"task": projectBoardMarker, "status": "doing"},
		}}
	}
	if _, err := store.AppendProjectBoardRows(projectID, "session-marker", sessionstore.ProjectBoardWorkFile,
		rows(sessionstore.ProjectBoardKindWork, "work:marker")); err != nil {
		t.Fatalf("写 worktable 投影: %v", err)
	}
	if _, err := store.AppendProjectBoardRows(projectID, "session-marker", sessionstore.ProjectBoardTeamworkFile,
		rows(sessionstore.ProjectBoardKindMilestone, "milestone:marker")); err != nil {
		t.Fatalf("写 teamwork 投影: %v", err)
	}
	// 真写出去了才谈得上"它没进请求"：
	folded, err := store.ReadProjectBoardRows(projectID, "session-marker", sessionstore.ProjectBoardWorkFile)
	if err != nil || len(folded) != 1 {
		t.Fatalf("回读投影失败（folded=%d, err=%v）：用例前提没成立", len(folded), err)
	}
}

// TestProjectBoardPrefixBytesIdentical（T4.1）：开/关两臂的装配前缀逐字节相同。
func TestProjectBoardPrefixBytesIdentical(t *testing.T) {
	projectRoot := t.TempDir()
	provider := newFullChainPrefixServer(t, "read_file", filepath.Join(projectRoot, "i7_probe.txt"))
	accountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	offRoot := t.TempDir()
	off := newFullChainHarnessWithLimits(t, accountsPath, offRoot, 30*time.Second, true, seelexctx.Limits{})
	offPrefix := promptLayerBytes(t, off.app)

	onRoot := t.TempDir()
	onLimits := seelexctx.Limits{}
	onLimits.ProjectBoard.Enabled = true
	onLimits.ProjectBoard.Hook = "todo_list"
	onLimits.ProjectBoard.FlushIdleMS = 20
	on := newFullChainHarnessWithLimits(t, accountsPath, onRoot, 30*time.Second, true, onLimits)
	// 能力开的这一臂按生产装配补齐两件事：注入订阅口 + 注册读取面。
	sink, closeSink, err := projectBoardSinkFor(
		onLimits.ProjectBoard, on.store,
		on.app.WorkTableRowsFor, on.app.ProjectBoardTeamPlanFor,
		on.app.ProjectBoardTeamStateFor, on.app.ProjectIDFor,
	)
	if err != nil {
		t.Fatalf("projectBoardSinkFor: %v", err)
	}
	defer closeSink()
	on.app.SetProjectBoardSink(sink)
	registerProjectBoardReadTool(on.runtime, on.store, on.app)
	writeMarkedProjectBoardProjection(t, on.store, on.app)

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
	accountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	limits := seelexctx.Limits{}
	limits.ProjectBoard.Enabled = true
	harness := newFullChainHarnessWithLimits(t, accountsPath, projectRoot, 30*time.Second, true, limits)
	sink, closeSink, err := projectBoardSinkFor(
		limits.ProjectBoard, harness.store,
		harness.app.WorkTableRowsFor, harness.app.ProjectBoardTeamPlanFor,
		harness.app.ProjectBoardTeamStateFor, harness.app.ProjectIDFor,
	)
	if err != nil {
		t.Fatalf("projectBoardSinkFor: %v", err)
	}
	defer closeSink()
	harness.app.SetProjectBoardSink(sink)
	registerProjectBoardReadTool(harness.runtime, harness.store, harness.app)
	writeMarkedProjectBoardProjection(t, harness.store, harness.app)

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
