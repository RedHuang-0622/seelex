package core

import (
	"context"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store/workspace"
	"github.com/RedHuang-0622/seelex/wiring/adapters"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	applicationreview "github.com/RedHuang-0622/seelex/application/review"
)

func TestReviewApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("reviewApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 reviewApplication")
	}
	if field.Type != reflect.TypeOf((*applicationreview.Application)(nil)) {
		t.Fatalf("reviewApplication 字段类型 = %s", field.Type)
	}
	if _, exists := reflect.TypeOf(serviceState{}).FieldByName("reviewSeq"); exists {
		t.Fatal("reviewSeq 应归 reviewApplication，不再属于 serviceState")
	}
}

func TestReviewFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"RequestFileReview": false}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Frontend") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 reviewApplication 转发", function.Name.Name, len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

// reviewFixture 在一个真实临时工作区里摆出各类文件（正常 / 敏感 / 二进制 /
// 目录 / 不存在），并返回装配在 workspace.Repo 上的服务。
//
// 刻意走**真实** workspace.Repo（而不是 fake ReadFile）：本用例钉住的正是"沙盒
// 边界由 workspace 域统一裁决"——用 fake 就只能验证"我们问了"，验证不了"边界
// 真的拦得住"。
func reviewFixture(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	mustWrite := func(rel string, content []byte) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("docs/readme.md", []byte("# 待审阅\n正文\n"))
	mustWrite("config/accounts.yaml", []byte("token: x\n"))
	mustWrite("assets/blob.bin", []byte{0x00, 0x01, 0x02, 0x00, 0xff})
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
		deps.Workspace = adapters.WorkspacePort{Repo: workspace.NewRepo()}
	})
	if err := frontendOfTestService(service).CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	return service, root
}

// TestRequestFileReviewClassifiesByWorkspaceBoundary：一次批处理召唤的裁决口径——
// 通过沙盒/敏感/二进制检查的进清单，其余按契约词表回报原因（不静默丢弃）。
func TestRequestFileReviewClassifiesByWorkspaceBoundary(t *testing.T) {
	service, _ := reviewFixture(t)

	request, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{
		{Path: "docs/readme.md", Note: "先看这份", StartLine: 1, EndLine: 2},
		{Path: "config/accounts.yaml"},
		{Path: "assets/blob.bin"},
		{Path: "ghost.md"},
		{Path: "src"},
		{Path: "../escape.md"},
		{Path: "docs/readme.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Files) != 1 || request.Files[0].Path != "docs/readme.md" {
		t.Fatalf("只有正常文件该进清单：%+v", request.Files)
	}
	if request.Files[0].Note != "先看这份" || request.Files[0].StartLine != 1 || request.Files[0].EndLine != 2 {
		t.Fatalf("备注/行区间必须原样带回：%+v", request.Files[0])
	}
	if request.RequestID == "" {
		t.Fatal("召唤必须有身份（前端据此判定新请求 vs 补放）")
	}
	reasons := make(map[string]string, len(request.Skipped))
	for _, skip := range request.Skipped {
		reasons[skip.Path] = skip.Reason
	}
	want := map[string]string{
		"config/accounts.yaml": dto.ReviewSkipSensitive,
		"assets/blob.bin":      dto.ReviewSkipBinary,
		"ghost.md":             dto.ReviewSkipMissing,
		"src":                  dto.ReviewSkipSandbox,
		"../escape.md":         dto.ReviewSkipSandbox,
		"docs/readme.md":       dto.ReviewSkipDuplicate,
	}
	if len(request.Skipped) != len(want) {
		t.Fatalf("跳过清单条数 = %d, want %d（%+v）", len(request.Skipped), len(want), request.Skipped)
	}
	for path, reason := range want {
		if reasons[path] != reason {
			t.Errorf("%s 的跳过原因 = %q, want %q", path, reasons[path], reason)
		}
	}
}

// TestRequestFileReviewProjectsIntoSnapshotAndClears：落点是**会话作用域的可重放
// 快照段**（不是一次性事件）——GUI 未开/被重连时按快照补放；清空走同一条投影。
func TestRequestFileReviewProjectsIntoSnapshotAndClears(t *testing.T) {
	service, _ := reviewFixture(t)

	request, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{{Path: "docs/readme.md"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.ReviewRequest == nil {
		t.Fatal("快照必须带上待审阅请求（落点是快照只读段，不是一次性事件）")
	}
	if snapshot.ReviewRequest.RequestID != request.RequestID || len(snapshot.ReviewRequest.Files) != 1 {
		t.Fatalf("快照里的请求与返回值不一致：%+v", snapshot.ReviewRequest)
	}
	// 快照是并发读者的共享值：改一份不得影响另一份。
	snapshot.ReviewRequest.Files[0].Path = "tampered.md"
	if service.Snapshot().ReviewRequest.Files[0].Path != "docs/readme.md" {
		t.Fatal("快照深拷贝失效：外部改写污染了权威快照")
	}

	service.components.reviewApplication.ClearReviewRequest("")
	if service.Snapshot().ReviewRequest != nil {
		t.Fatal("ClearReviewRequest 之后快照不得残留请求")
	}
}

// TestRequestFileReviewClearedOnWorkspaceSwitch：失效语义对齐既有口径——工作区
// 切换时抽屉随树清空（清单里是工作区相对路径，换根之后不再指向"同一份文件"）。
func TestRequestFileReviewClearedOnWorkspaceSwitch(t *testing.T) {
	service, _ := reviewFixture(t)
	if _, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{{Path: "docs/readme.md"}}); err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().ReviewRequest == nil {
		t.Fatal("前置条件：请求应当已在快照里")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "other.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := frontendOfTestService(service).CreateWorkspace("other", other, ""); err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().ReviewRequest != nil {
		t.Fatal("工作区切换必须清空待审阅请求（路径基准换了根）")
	}
}

// TestRequestFileReviewRejectsOverCapAndEmpty：超限显式报错（不静默截断），空清单
// 是形状错误；两者都不写快照（不把用户的抽屉关掉）。
func TestRequestFileReviewRejectsOverCapAndEmpty(t *testing.T) {
	service, _ := reviewFixture(t)

	if _, err := frontendOfTestService(service).RequestFileReview(context.Background(), nil); err == nil {
		t.Fatal("空清单必须报错")
	}
	tooMany := make([]dto.ReviewFileRef, 0, dto.MaxReviewFilesPerRequest+1)
	for index := 0; index <= dto.MaxReviewFilesPerRequest; index++ {
		tooMany = append(tooMany, dto.ReviewFileRef{Path: "docs/readme.md"})
	}
	if _, err := frontendOfTestService(service).RequestFileReview(context.Background(), tooMany); err == nil {
		t.Fatal("超限必须报错")
	}
	if service.Snapshot().ReviewRequest != nil {
		t.Fatal("被拒的召唤不得写快照")
	}
}

// TestRequestFileReviewWithoutWorkspaceFailsExplicitly：没有绑定工作区时明确失败
// （而不是"看起来打开了、前端去读却读不到"）。
func TestRequestFileReviewWithoutWorkspaceFailsExplicitly(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
		deps.Workspace = adapters.WorkspacePort{Repo: workspace.NewRepo()}
	})
	if _, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{{Path: "docs/readme.md"}}); err == nil {
		t.Fatal("未绑定工作区必须报错")
	}
}

// TestRequestFileReviewAllSkippedKeepsSnapshotUntouched：全部被跳过时不写快照——
// "叫了 10 份、一份都没成"不该把用户已经开着的详情抽屉关掉。
func TestRequestFileReviewAllSkippedKeepsSnapshotUntouched(t *testing.T) {
	service, _ := reviewFixture(t)
	first, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{{Path: "docs/readme.md"}})
	if err != nil {
		t.Fatal(err)
	}

	allSkipped, err := frontendOfTestService(service).RequestFileReview(context.Background(), []dto.ReviewFileRef{
		{Path: "config/accounts.yaml"}, {Path: "assets/blob.bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allSkipped.Files) != 0 || len(allSkipped.Skipped) != 2 {
		t.Fatalf("全部跳过时收据仍要如实回报：%+v", allSkipped)
	}
	if got := service.Snapshot().ReviewRequest; got == nil || got.RequestID != first.RequestID {
		t.Fatalf("全部被跳过不得改写既有请求：%+v", got)
	}
}
