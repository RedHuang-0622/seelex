package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// TestEveryRegisteredToolDeclaresGroups 是打点 K-0 的判据（TC-K0-1）：
// **凡 seelex 自己注册的工具，都必须在路由组表里分封**。
//
// 为什么用源码扫描而不是遍历运行时注册表：装配完整的 Runtime 需要 accounts /
// 角色 / plan executor 等一堆注入面，而"注册了什么名字"这件事**静态就在源码里**
// ——每个注册点都写着工具名（字面量或包级常量）。扫描源码因此比"跑起来再问"
// 更完整：main.go 注册的工具（read_tool_result / switch_plugin / …）不会出现在
// RegisterBuiltins 的路径上，运行时快照反而看不到它们。
//
// 判据：每个名字都能被 RoutePermissionGroup 路由到某个组。未分封的名字拿不到
// 任何权限策略（框架口径是"默认 ask"）也拿不到任何并发分类，而它**不会有任何
// 报错面**——所以这条断言就是那个报错面。
//
// 动态第三方工具（MCP server 工具、插件工具）不在这条判据内：它们的名字在装配
// 期才存在，口径仍是"未分封 = 按框架默认走审批"（既有语义，未改）。
//
// 扫描器只认字面量与包级常量：新增注册点时若用变量拼名字，本用例会主动报
// "名字无法静态解析"——那是**有意**的（K-0 的判据必须能在源码里被看见）。
func TestEveryRegisteredToolDeclaresGroups(t *testing.T) {
	sites, unresolved, files := scanToolRegistrations(t, ".")
	if len(sites) == 0 {
		t.Fatalf("没扫到任何工具注册点（扫了 %d 个文件）：用例本身失效，比断言失败更糟", files)
	}
	names := make([]string, 0, len(sites))
	for name := range sites {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		group, routed := seeltools.RoutePermissionGroup(seeltools.DefaultPermissionGroupList(), name)
		if !routed {
			t.Errorf("工具 %s（注册于 %s）未分封：它没有权限策略、也没有并发分类。"+
				"请在 tools/permission_policy.go 的 DefaultPermissionGroupList 里给它一个组",
				name, sites[name])
			continue
		}
		if meta := seeltools.DeclaredToolMeta(name); len(meta.Groups) != 1 || meta.Groups[0] != group.Name {
			t.Errorf("工具 %s 的簇属声明 %v 与路由组 %q 不一致（声明路径必须与路由组表同源）",
				name, meta.Groups, group.Name)
		}
	}
	for _, item := range unresolved {
		t.Errorf("注册点的工具名无法静态解析（%s）：请改用字面量或包级常量——"+
			"拼出来的名字绕过了 K-0 的判据，等于没有声明", item)
	}
	t.Logf("已分封工具 %d 个（扫过 %d 个源文件）", len(names), files)
}

// scanToolRegistrations 扫描仓库源码里的工具注册点，返回
// (工具名 → 首个注册处, 无法静态解析的注册处, 扫过的文件数)。
//
// 认两种调用形态（生产里各有一处，缺哪一种都会漏掉一整族工具）：
//   - `x.RegisterTool("name", …)`（router.go / task/tools.go / main.go）；
//   - 先把注册面存进局部变量再调用（`register := t.deps.RegisterTool; register(ToolClick, …)`，
//     见 seelebridge/tools/computer/tools.go）——别名从赋值语句里识别。
func scanToolRegistrations(t *testing.T, root string) (map[string]string, []string, int) {
	t.Helper()
	sites := map[string]string{}
	var unresolved []string
	files := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if path == root {
				return nil
			}
			if strings.HasPrefix(name, ".") || scanSkipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil
		}
		files++
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)

		consts := constStringsOf(file)
		aliases := registrationAliases(file)

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isRegistrationCall(call, aliases) || len(call.Args) == 0 {
				return true
			}
			switch name, ok := literalOrConst(call.Args[0], consts); {
			case !ok:
				unresolved = append(unresolved, relative+":"+strconv.Itoa(token.NewFileSet().Position(call.Pos()).Line))
			case sites[name] == "":
				sites[name] = relative
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫描注册点失败: %v", err)
	}
	return sites, unresolved, files
}

// scanSkipDirs 是扫描时跳过的目录：第三方（vendor）与仓库里不参与装配的副本
// （构建产物、临时目录、打包检查留下的目录树）。
var scanSkipDirs = map[string]bool{
	"vendor": true, "dist": true, "_tmp": true, "tmp": true, "bin": true,
	"node_modules": true, "workspace": true, "local": true, "coverage": true,
	"gui-pkg-check": true, "gui-verify": true,
}

// constStringsOf 收集文件里的包级字符串常量（`Name = "value"`），供解析
// `register(ToolClick, …)` 这类"常量即工具名"的注册点。
func constStringsOf(file *ast.File) map[string]string {
	consts := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				if literal, ok := value.Values[index].(*ast.BasicLit); ok && literal.Kind == token.STRING {
					if text, err := strconv.Unquote(literal.Value); err == nil {
						consts[name.Name] = text
					}
				}
			}
		}
	}
	return consts
}

// registrationAliases 收集"注册面的局部别名"：任何被赋值为 `<…>.RegisterTool`
// 的标识符都算。见 seelebridge/tools/computer/tools.go 的 `register := t.deps.RegisterTool`。
func registrationAliases(file *ast.File) map[string]bool {
	aliases := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		bound := false
		for _, right := range assign.Rhs {
			if containsRegisterToolSelector(right) {
				bound = true
			}
		}
		if !bound {
			return true
		}
		for _, left := range assign.Lhs {
			if ident, ok := left.(*ast.Ident); ok {
				aliases[ident.Name] = true
			}
		}
		return true
	})
	return aliases
}

func containsRegisterToolSelector(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel != nil && selector.Sel.Name == "RegisterTool" {
			found = true
		}
		return !found
	})
	return found
}

func isRegistrationCall(call *ast.CallExpr, aliases map[string]bool) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel != nil && fun.Sel.Name == "RegisterTool"
	case *ast.Ident:
		return aliases[fun.Name]
	default:
		return false
	}
}

// literalOrConst 解析注册点的第一个实参：字面量、包内常量、或 `包.常量`。
func literalOrConst(expr ast.Expr, consts map[string]string) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.Ident:
		text, ok := consts[value.Name]
		return text, ok
	case *ast.SelectorExpr:
		text, ok := consts[value.Sel.Name]
		return text, ok
	default:
		return "", false
	}
}

// TestRegistrationScannerSeesEveryFamily 钉住扫描器本身：漏掉一族工具就等于
// K-0 的判据出现盲区（"测试通过"变成假象）。这里对若干必须被扫到的代表工具
// 逐一点名——它们来自不同的注册路径（router / task / computer / main）。
func TestRegistrationScannerSeesEveryFamily(t *testing.T) {
	sites, _, _ := scanToolRegistrations(t, ".")
	for _, name := range []string{
		"read_file", "bash", "write_file", // seelebridge/tools/router.go
		"todo_init", "task_add", // seelebridge/task/tools.go
		"computer_click", "computer_screenshot", // seelebridge/tools/computer/tools.go（别名形态）
		"fork_subagents",              // seelebridge/runtime_plan.go
		"read_tool_result",            // main.go
		"compact_context",             // main.go
		"goal_begin",                  // register_goal_tools.go
		"skill_activate",              // skill_activate_tool.go
		"web_search",                  // seelebridge/tools/websearch
		"switch_plugin", "mcp_create", // main.go（能力面）
	} {
		if _, ok := sites[name]; !ok {
			t.Errorf("扫描器没扫到 %s 的注册点：K-0 判据对它有盲区", name)
		}
	}
}
