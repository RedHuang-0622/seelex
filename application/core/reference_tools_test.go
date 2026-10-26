package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
)

func TestReferenceApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("referenceApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 referenceApplication")
	}
	if field.Type != reflect.TypeOf((*applicationreference.Application)(nil)) {
		t.Fatalf("referenceApplication 字段类型 = %s", field.Type)
	}
}

func TestReferenceFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "reference_tools.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"ReadToolResultHandler": false,
		"ToolResultContent":     false,
		"ReadPlanHandler":       false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Service") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 referenceApplication 转发", function.Name.Name, len(function.Body.List))
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

// TestReadToolResultResolvesNodeRef 验证 P1 桥：read_tool_result 对
// node:<nodeID>: 前缀的 ref 经引擎桥读回节点归档内容（子代理 ref 可读）。
func TestReadToolResultResolvesNodeRef(t *testing.T) {
	engine := &fakeEngine{}
	engine.nodeToolResultFn = func(nodeID, ref string) (string, bool) {
		if nodeID == "agent-1" && ref == "node:agent-1:result:call-1" {
			return "子代理工具输出内容-0123456789", true
		}
		return "", false
	}
	service := newTestService(t, engine)

	page, err := service.ReadToolResultHandler(t.Context(), `{"result_ref":"node:agent-1:result:call-1","offset":0,"limit":64}`)
	if err != nil {
		t.Fatalf("read node ref: %v", err)
	}
	if !strings.Contains(page, "子代理工具输出内容") {
		t.Fatalf("page must contain node tool content, got: %s", page)
	}
	if !strings.Contains(page, `"result_ref":"node:agent-1:result:call-1"`) {
		t.Fatalf("page must echo node ref, got: %s", page)
	}

	// 未知节点 ref → 显式错误（不静默空结果）。
	if _, err := service.ReadToolResultHandler(t.Context(), `{"result_ref":"node:ghost:result:x"}`); err == nil {
		t.Fatal("unknown node ref must error")
	}
}

// TestReadToolResultResolvesCallAlias 验证 result:call_<callID> 别名映射：
// 工具结果过大被省略归档后，模型自造 result:call_... 引用（而非占位中
// 的 tr- ref）仍能经 resultRefsByToolCallID 映射回真实 ref 读回。
func TestReadToolResultResolvesCallAlias(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	raw := strings.Repeat("x", 4000) + "alias-target-content"
	stored := service.components.tasks.StoreToolResultLocked("fork_subagents", raw)
	service.components.tasks.SetResultRefByCallIDLocked("call_00_Ohu6emGEZr0gV4m2JU4t0987", stored.Ref)

	page, err := service.ReadToolResultHandler(t.Context(),
		`{"result_ref":"result:call_00_Ohu6emGEZr0gV4m2JU4t0987","offset":4000,"limit":64}`)
	if err != nil {
		t.Fatalf("read alias ref: %v", err)
	}
	if !strings.Contains(page, `"result_ref":"`+stored.Ref+`"`) {
		t.Fatalf("page must echo the real tr- ref, got: %s", page)
	}
	if !strings.Contains(page, "alias-target-content") {
		t.Fatalf("page must contain archived content, got: %s", page)
	}

	// 未知 callID：别名保留原值 → 仍返回明确错误（不静默）。
	if _, err := service.ReadToolResultHandler(t.Context(),
		`{"result_ref":"result:call_ghost","limit":32}`); err == nil {
		t.Fatal("unknown call alias must error")
	}
}
