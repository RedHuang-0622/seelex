package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// state_enum_cast_gate_test.go — 状态**枚举化之后的静默陷阱**门禁。
//
// 背景（这一条是踩出来的）：枚举化把 `type X string` 换成 `type X uint8` 之后，老的
// `string(dto.SubAgentDone)` 这类转换**照样编译过、vet 也不报**，但它得到的是**控制字符**
// （`string(3)` = "\x03"），不是那个词。于是一处比较静默地永远不成立——"记录说已完成、
// 恢复逻辑说不算完"——正是这一整轮要消灭的那类错误，只是换了个长相。
//
// 判据：仓库里**任何地方**都不许再把状态枚举值直接 `string(...)`；要对外词就用 `.String()`
// （它才是"枚举 ↔ 词"那一处）。这一条不需要白名单：合法写法只有一种。
func TestStateEnumNeverCastToString(t *testing.T) {
	sources := map[string]string{
		"违规样例（枚举直接转字符串）": `package p

import "github.com/RedHuang-0622/seelex/application/contract/dto"

func mark() string { return string(dto.SubAgentDone) }
`,
		"违规样例（本层别名同样是枚举）": `package p

func mark() bool { return record.Status == string(subagentNodeStatusFailed) }
`,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			if found := stateEnumStringCasts("sample.go", []byte(source)); len(found) == 0 {
				t.Fatal("这条写法必须被判红：枚举值直接 string(...) 得到的是控制字符，不是词")
			}
		})
	}

	// 反向对照：`.String()` 是唯一合法写法，不许误伤。
	clean := `package p

import "github.com/RedHuang-0622/seelex/application/contract/dto"

func mark() string { return dto.SubAgentDone.String() }

func same(status string) bool { return status == dto.TaskCompleted.String() }
`
	if found := stateEnumStringCasts("clean.go", []byte(clean)); len(found) != 0 {
		t.Fatalf("`.String()` 是合法写法，不该被判红：%v", found)
	}

	// 生产源码：全仓扫一遍（含测试源码——同一类陷阱在用例里也算错误）。
	root := repoRoot()
	violations := []string{}
	for _, dir := range []string{"application", "seelebridge", "e2e", "sessionstore", "seelexctx", "tui", "gui"} {
		found, err := scanTreeForStateEnumCasts(root, dir)
		if err != nil {
			t.Fatalf("扫 %s：%v", dir, err)
		}
		violations = append(violations, found...)
	}
	for _, violation := range violations {
		t.Errorf("%s\n  枚举化之后不许 `string(枚举值)`：用 `.String()` 取对外词（词只在枚举那一处）", violation)
	}
}

// stateEnumName 判断一个标识符（或 `pkg.Name` 选择式）是不是**已枚举化的状态值**。
// 命中即"它的 string(...) 转换是陷阱"（有 String() 方法，整数值转字符串只会得到控制字符）。
func stateEnumName(name string) bool {
	tail := name
	if index := strings.LastIndex(name, "."); index >= 0 {
		tail = name[index+1:]
	}
	if strings.HasPrefix(tail, "subagentNodeStatus") || strings.HasPrefix(tail, "teamUnitStatus") {
		return true
	}
	for _, prefix := range []string{"AsyncState", "PlanRunStatus", "SubAgent", "ToolEvent", "Task", "JobReceipt"} {
		if strings.HasPrefix(tail, prefix) {
			return true
		}
	}
	return false
}

// stateEnumStringCasts 用 AST 找源码里的 `string(<状态枚举值>)`，返回可读的命中清单。
func stateEnumStringCasts(relative string, source []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relative, source, 0)
	if err != nil {
		return []string{relative + ": 解析失败：" + err.Error()}
	}
	found := []string{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if function, ok := call.Fun.(*ast.Ident); !ok || function.Name != "string" {
			return true
		}
		var name string
		switch argument := call.Args[0].(type) {
		case *ast.Ident:
			name = argument.Name
		case *ast.SelectorExpr:
			if qualifier, ok := argument.X.(*ast.Ident); ok {
				name = qualifier.Name + "." + argument.Sel.Name
			}
		default:
			return true
		}
		if !stateEnumName(name) {
			return true
		}
		position := fset.Position(call.Pos())
		found = append(found, relative+":"+strconv.Itoa(position.Line)+"  string("+name+")  "+strings.TrimSpace(lineAt(source, position.Line)))
		return true
	})
	return found
}

// scanTreeForStateEnumCasts 递归扫一棵源码树。
func scanTreeForStateEnumCasts(root, dir string) ([]string, error) {
	found := []string{}
	err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		found = append(found, stateEnumStringCasts(filepath.ToSlash(relative), source)...)
		return nil
	})
	return found, err
}

// stateFieldCastWhitelist 登记"在声明过状态面的文件里，`string(x.Status)` 读的却**不是**我们
// 这一格"的位置。每条写清理由；没有条目时它就该是空的——不许当藏东西的地毯。
var stateFieldCastWhitelist = []struct {
	file     string
	receiver string
	reason   string
}{}

// stateFieldStringCasts 找 `string(<x>.Status/State/Phase)`，**只在声明过状态面的文件里**判。
//
// 为什么这样划界：`string(x.Status)` 在 x 是普通字符串字段时是无害的 no-op，在 x 是枚举字段时
// 才是控制字符陷阱——没有类型信息就分不清。所以这一条**不是类型系统**，它是"在最可能出事的
// 文件里查这一种形态"（本波踩到的正是 application/core/work_table.go 里的 `string(record.Status)`）。
// 真正的兜底顺序：字段类型（编译器）→ 用例 → 这条启发式。
func stateFieldStringCasts(relative string, source []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relative, source, 0)
	if err != nil {
		return []string{relative + ": 解析失败：" + err.Error()}
	}
	found := []string{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if function, ok := call.Fun.(*ast.Ident); !ok || function.Name != "string" {
			return true
		}
		selector, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "Status", "State", "Phase":
		default:
			return true
		}
		receiver := selectorText(selector)
		if receiver == "" {
			return true
		}
		for _, allowed := range stateFieldCastWhitelist {
			if allowed.file == relative && allowed.receiver == receiver {
				return true
			}
		}
		position := fset.Position(call.Pos())
		found = append(found, relative+":"+strconv.Itoa(position.Line)+"  string("+receiver+")  "+strings.TrimSpace(lineAt(source, position.Line)))
		return true
	})
	return found
}

// statusVocabularyFileSet 收集门禁已经声明过的文件（那一批文件里确定存在状态枚举值）。
func statusVocabularyFileSet() map[string]bool {
	files := map[string]bool{}
	for _, scope := range statusVocabularyScopes {
		for file := range scope.files {
			files[file] = true
		}
	}
	return files
}

// TestStateEnumFieldCastInDeclaredFiles：在声明过状态面的文件里，不许出现 string(x.Status)。
func TestStateEnumFieldCastInDeclaredFiles(t *testing.T) {
	root := repoRoot()
	declared := statusVocabularyFileSet()
	files := make([]string, 0, len(declared))
	for file := range declared {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatalf("清单里的 %s 读不到：%v", file, err)
		}
		for _, violation := range stateFieldStringCasts(file, source) {
			t.Errorf("%s\n  枚举字段要对外词就用 `.String()`（string(枚举值) 得到的是控制字符）", violation)
		}
	}
}

// selectorText 把 `x.Status` / `a.b.Status` 还原成源码文本；认不出的形态给空串（跳过）。
func selectorText(selector *ast.SelectorExpr) string {
	prefix := ""
	switch qualifier := selector.X.(type) {
	case *ast.Ident:
		prefix = qualifier.Name
	case *ast.SelectorExpr:
		prefix = selectorText(qualifier)
	default:
		return ""
	}
	if prefix == "" {
		return ""
	}
	return prefix + "." + selector.Sel.Name
}
