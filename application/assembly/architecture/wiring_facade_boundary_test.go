package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 本文件是「旧装配门面退场」的结构门禁，扫描范围是**整个仓库的生产代码与测试代码**。
//
// 用户已物理删除 `wiring/`，并明确裁决：不得恢复、不得整目录换名重建、不得用
// alias 或一行转发让旧面重新编译。因此这里钉四件事：
//
//  1. 任何 `.go`（含 `_test.go`）都不得 import 已退场的装配门面包 `wiring/**`；
//  2. 源码树里不得再出现名为 `wiring` 的包或目录；
//  3. 装配面不得新增「构造 alias」——函数体只有一句 `return other.NewX(...)`；
//  4. 装配面不得新增「单方法 facade」——空壳类型只有一个方法，方法体只有一句转发。
//
// 第 3、4 条只扫描消费者与装配面（根包、boot、gui、tui、console、e2e、
// application/assembly 生产代码）。owner 包内部的合法委托（例如
// `backend/engine/assemble` 已知的薄转发面）由各自的 ratchet 台账管理，
// 不在此重复计一次。

// retiredWiringImport 按两段拼接：门禁文件自身不得在字面量里写全该 import 路径，
// 否则仓库级归零 grep（wiring import = 0）的证据会被门禁自己污染。
const retiredWiringImport = "github.com/RedHuang-0622/seelex/" + "wiring"

// scratchDirs 是仓库内的临时/产物目录：它们不是源码事实来源，扫描时跳过，
// 避免把并行会话或验证副本算进结构门禁。
var scratchDirs = map[string]bool{
	".git":         true,
	".tmp":         true,
	"_logs":        true,
	"_scratch":     true,
	"_tmp":         true,
	"dist":         true,
	"node_modules": true,
	"stage":        true,
	"tmp":          true,
	"vendor":       true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	absolute, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("定位仓库根: %v", err)
	}
	return absolute
}

// walkSources 遍历仓库源码树，跳过临时目录与 .git。
func walkSources(t *testing.T, root string, visit func(path string) error) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && scratchDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path)
	})
	if err != nil {
		t.Fatalf("遍历源码树: %v", err)
	}
}

// TestNoSourceImportsRetiredWiring 是本次迁移的第一条红灯：生产代码与测试代码
// 都不得再引用已删除的 `wiring/**`。
func TestNoSourceImportsRetiredWiring(t *testing.T) {
	root := repoRoot(t)
	fileSet := token.NewFileSet()
	var offenders []string
	walkSources(t, root, func(path string) error {
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("解析 %s 的 import %s: %v", path, imported.Path.Value, err)
			}
			if value == retiredWiringImport || strings.HasPrefix(value, retiredWiringImport+"/") {
				offenders = append(offenders, relativePath(root, path))
			}
		}
		return nil
	})
	if len(offenders) > 0 {
		t.Fatalf(
			"已退场的装配门面仍被引用（生产与测试代码都不得 import %s/**）：\n%s",
			retiredWiringImport,
			strings.Join(offenders, "\n"),
		)
	}
}

// TestNoWiringPackageOrDirectory 禁止以任何形式重建 `wiring` 包：目录名与包名都算。
func TestNoWiringPackageOrDirectory(t *testing.T) {
	root := repoRoot(t)
	fileSet := token.NewFileSet()
	walkSources(t, root, func(path string) error {
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "wiring" {
			t.Errorf("重建了 wiring 目录：%s", relativePath(root, path))
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.PackageClauseOnly)
		if err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		if file.Name != nil && file.Name.Name == "wiring" {
			t.Errorf("重建了 wiring 包：%s", relativePath(root, path))
		}
		return nil
	})
}

// compositionSurfaceDirs 是「装配与消费面」：这里只允许真实行为，不允许转发壳。
func compositionSurfaceDirs(root string) []string {
	return []string{
		root,
		filepath.Join(root, "boot"),
		filepath.Join(root, "console"),
		filepath.Join(root, "e2e"),
		filepath.Join(root, "gui"),
		filepath.Join(root, "tui"),
		filepath.Join(root, "application", "assembly"),
	}
}

// TestNoConstructorAliasForwarders 禁止「构造 alias」：函数体只有一句
// `return <importedPkg>.NewX(...)`，且函数名与被调用名相同。这类函数不拥有任何
// 行为，只是把 owner 换个名字再暴露一次。
func TestNoConstructorAliasForwarders(t *testing.T) {
	root := repoRoot(t)
	fileSet := token.NewFileSet()
	for _, directory := range compositionSurfaceDirs(root) {
		forEachProductionFile(t, directory, func(path string, file *ast.File) {
			imported := importedPackageNames(file)
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Body == nil {
					continue
				}
				alias, called, ok := singleForwardedCall(function.Body)
				if !ok || called != function.Name.Name || !imported[alias] {
					continue
				}
				t.Errorf(
					"构造 alias 未删除：%s:%d %s 只是把 %s.%s 换名转发；调用方应直接调 owner",
					relativePath(root, path), fileSet.Position(function.Pos()).Line,
					function.Name.Name, alias, called,
				)
			}
		})
	}
}

// TestNoSingleMethodFacadeTypes 禁止「单方法 facade」：类型只有 1 个方法，
// 且方法体只有一句转发到别的包的函数。空壳 + 单个转发方法 = 第二层名字，不是 owner。
func TestNoSingleMethodFacadeTypes(t *testing.T) {
	root := repoRoot(t)
	fileSet := token.NewFileSet()
	for _, directory := range compositionSurfaceDirs(root) {
		forEachProductionFile(t, directory, func(path string, file *ast.File) {
			imported := importedPackageNames(file)
			methodsByReceiver := map[string][]*ast.FuncDecl{}
			emptyTypes := map[string]bool{}
			for _, declaration := range file.Decls {
				switch typed := declaration.(type) {
				case *ast.FuncDecl:
					if typed.Recv == nil || len(typed.Recv.List) != 1 {
						continue
					}
					if receiver := receiverName(typed.Recv.List[0].Type); receiver != "" {
						methodsByReceiver[receiver] = append(methodsByReceiver[receiver], typed)
					}
				case *ast.GenDecl:
					if typed.Tok != token.TYPE {
						continue
					}
					for _, specification := range typed.Specs {
						typeSpec, ok := specification.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if structure, ok := typeSpec.Type.(*ast.StructType); ok && len(structure.Fields.List) == 0 {
							emptyTypes[typeSpec.Name.Name] = true
						}
					}
				}
			}
			for receiver, methods := range methodsByReceiver {
				if !emptyTypes[receiver] || len(methods) != 1 || methods[0].Body == nil {
					continue
				}
				method := methods[0]
				alias, _, ok := singleForwardedCall(method.Body)
				if !ok || !imported[alias] {
					continue
				}
				t.Errorf(
					"单方法 facade 未删除：%s:%d %s.%s 的空壳只有一个转发方法（目标包 %s）；调用方应直接调 owner",
					relativePath(root, path), fileSet.Position(method.Pos()).Line,
					receiver, method.Name.Name, alias,
				)
			}
		})
	}
}

func forEachProductionFile(t *testing.T, directory string, visit func(path string, file *ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		visit(path, file)
	}
}

func importedPackageNames(file *ast.File) map[string]bool {
	names := make(map[string]bool, len(file.Imports))
	for _, imported := range file.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		if imported.Name != nil {
			names[imported.Name.Name] = true
			continue
		}
		segments := strings.Split(value, "/")
		names[segments[len(segments)-1]] = true
	}
	return names
}

func receiverName(expression ast.Expr) string {
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

// singleForwardedCall 判断函数体是否「只有一句 return <pkg>.<name>(...)」，
// 返回包选择符与被调用名。
func singleForwardedCall(body *ast.BlockStmt) (alias string, called string, ok bool) {
	if len(body.List) != 1 {
		return "", "", false
	}
	statement, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		return "", "", false
	}
	call, ok := statement.Results[0].(*ast.CallExpr)
	if !ok {
		return "", "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return identifier.Name, selector.Sel.Name, true
}

func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}
