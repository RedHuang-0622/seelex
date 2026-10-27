package store

// god_type_ratchet_test.go — M0 门禁 5：God 类型方法面 ratchet。
//
// 为什么需要它：`docs/2026-10-27-store-ddd-split/plan.md` §10.2 规则 4 规定
// `Router`/`jsonRepository`/`storeEngine`/`SessionGranularStore` 的公开方法数
// “只许收缩”。拆分过程中最容易发生的回归不是删错文件，而是**新能力继续挂到
// 上帝 receiver 上**——它编译通过、测试全绿，只是把要拆的东西又加厚一层。
// 计数门槛挡不住这种位移（删一个旧方法再加一个新方法，净零漂移），所以这里
// 钉的是**方法名集合**：任何一处新增都必须显式改基线文件，评审时看得见。
//
// 语义：
//   - 生产文件（`store/*.go`，非递归、不含 `_test.go`）覆盖本包的四个 God 类型；
//   - 基线 `testdata/god_type_methods.json` 是唯一事实源（名字有序去重）；
//   - 新增方法 → 红，要求下沉到领域包，而不是加基线；
//   - 删除方法 → 也红，要求同步收紧基线（ratchet 必须跟着实际面走，否则下次
//     新增可以用“预算还有余量”蒙过去）。
//
// 与 `application/architecture_budget_test.go` 同一手法（testdata 基线 + 集合
// 比对），刻意不另造记账格式。

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// godTypeMethodsBaselinePath 相对本测试文件所在目录。
const godTypeMethodsBaselinePath = "testdata/god_type_methods.json"

// godTypeNames 是被 ratchet 钉住 receiver 的四个上帝类型。
// 顺序即报错顺序，与基线文件的 key 集合必须完全一致。
var godTypeNames = []string{
	"Router",
	"jsonRepository",
	"storeEngine",
	"SessionGranularStore",
}

// TestGodTypeMethodSurfaceOnlyShrinks 是 ratchet 主判据。
func TestGodTypeMethodSurfaceOnlyShrinks(t *testing.T) {
	packageDir := storePackageDir(t)
	baseline := readGodTypeMethodBaseline(t)

	declared := scanGodTypeDeclarations(t, packageDir)
	for _, name := range godTypeNames {
		if _, ok := baseline[name]; !ok {
			t.Fatalf("基线缺少类型 %s：四类上帝类型必须逐个记账", name)
		}
		if !declared[name] {
			t.Fatalf("本包没有声明类型 %s：类型改名/搬走必须同步基线，不能让扫描静默返回空集", name)
		}
	}
	for name := range baseline {
		if !containsGodType(name) {
			t.Fatalf("基线登记了未受 ratchet 管理的类型 %s：请从 %s 删除，或先把它加进 godTypeNames",
				name, godTypeMethodsBaselinePath)
		}
	}

	got := scanGodTypeMethods(t, packageDir)
	for _, name := range godTypeNames {
		added, removed := godTypeMethodDrift(baseline[name], got[name])
		if len(added) > 0 {
			t.Errorf("%s 新增了 %d 个方法（基线 %d → 实际 %d）：\n  %s\n"+
				"上帝类型只许收缩（plan.md §10.2 规则 4）。新能力应放进领域包，"+
				"由领域包拥有实现、根包只留薄转发；禁止直接把方法加在这里再改基线。",
				name, len(added), len(baseline[name]), len(got[name]), strings.Join(added, "\n  "))
		}
		if len(removed) > 0 {
			t.Errorf("%s 少了 %d 个方法（基线 %d → 实际 %d）：\n  %s\n"+
				"删除是对的，但基线必须同步收紧：移动能力时把名字从 %s 里删掉，"+
				"让 ratchet 记录真实剩余面。",
				name, len(removed), len(baseline[name]), len(got[name]),
				strings.Join(removed, "\n  "), godTypeMethodsBaselinePath)
		}
	}
}

// TestGodTypeMethodScannerSeesAdditionsAndSkipsTests 证明门禁确实会“咬”。
//
// 判据自己也要有失败面：如果 AST 扫描压根找不到 receiver 方法（比如扫错目录、
// 过滤条件写反），主判据会永远绿。这里用合成源码钉住“能找到、且只找生产文件”。
func TestGodTypeMethodScannerSeesAdditionsAndSkipsTests(t *testing.T) {
	dir := t.TempDir()
	production := strings.Join([]string{
		"package store",
		"",
		"type storeEngine struct{}",
		"type Router struct{}",
		"",
		"func (store *storeEngine) Existing() {}",
		"func (store *storeEngine) Extra() {}",
		"func (router *Router) Read() {}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "synthetic.go"), []byte(production), 0o600); err != nil {
		t.Fatalf("写合成生产文件: %v", err)
	}
	testOnly := strings.Join([]string{
		"package store",
		"",
		"func (store *storeEngine) TestOnlyHelper() {}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "synthetic_test.go"), []byte(testOnly), 0o600); err != nil {
		t.Fatalf("写合成测试文件: %v", err)
	}

	got := scanGodTypeMethods(t, dir)
	if strings.Join(got["storeEngine"], ",") != "Existing,Extra" {
		t.Fatalf("扫描 storeEngine 方法 = %v，期望 [Existing Extra]", got["storeEngine"])
	}
	if strings.Join(got["Router"], ",") != "Read" {
		t.Fatalf("扫描 Router 方法 = %v，期望 [Read]", got["Router"])
	}

	declared := scanGodTypeDeclarations(t, dir)
	for _, name := range []string{"storeEngine", "Router"} {
		if !declared[name] {
			t.Fatalf("扫描漏掉了类型声明 %s", name)
		}
	}
	if declared["SessionGranularStore"] {
		t.Fatal("扫描把不存在的类型也报成已声明")
	}

	added, removed := godTypeMethodDrift([]string{"Existing"}, got["storeEngine"])
	if strings.Join(added, ",") != "Extra" || len(removed) != 0 {
		t.Fatalf("漂移计算错误：added=%v removed=%v，期望 added=[Extra]", added, removed)
	}
}

// godTypeMethodDrift 返回基线有而实际没有（removed）、实际有而基线没有（added）的方法。
func godTypeMethodDrift(want, got []string) (added, removed []string) {
	wantSet := make(map[string]struct{}, len(want))
	for _, name := range want {
		wantSet[name] = struct{}{}
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, name := range got {
		gotSet[name] = struct{}{}
	}
	for _, name := range got {
		if _, ok := wantSet[name]; !ok {
			added = append(added, name)
		}
	}
	for _, name := range want {
		if _, ok := gotSet[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// readGodTypeMethodBaseline 读取基线并要求每个列表有序去重（手工编辑最容易留下的脏数据）。
func readGodTypeMethodBaseline(t *testing.T) map[string][]string {
	t.Helper()
	path := filepath.Join(storePackageDir(t), filepath.FromSlash(godTypeMethodsBaselinePath))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", godTypeMethodsBaselinePath, err)
	}
	var baseline map[string][]string
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("解析 %s: %v", godTypeMethodsBaselinePath, err)
	}
	for name, methods := range baseline {
		if !sort.StringsAreSorted(methods) {
			t.Errorf("基线 %s 的方法列表未排序：请按字典序排列，便于 diff 定位漂移", name)
		}
		for index := 1; index < len(methods); index++ {
			if methods[index] == methods[index-1] {
				t.Errorf("基线 %s 有重复方法 %s", name, methods[index])
			}
		}
	}
	return baseline
}

// scanGodTypeMethods 返回四个 God 类型在本包生产文件里声明的方法名（各自有序）。
// 只扫非递归目录、只扫非测试文件：`_test.go` 里的辅助方法不属于对外方法面。
func scanGodTypeMethods(t *testing.T, dir string) map[string][]string {
	t.Helper()
	collected := make(map[string][]string, len(godTypeNames))
	for _, name := range godTypeNames {
		collected[name] = []string{}
	}
	fileSet := token.NewFileSet()
	for _, path := range godTypeSourceFiles(t, dir) {
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", filepath.Base(path), err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) == 0 {
				continue
			}
			receiver := godTypeReceiverName(function.Recv.List[0].Type)
			if !containsGodType(receiver) {
				continue
			}
			collected[receiver] = append(collected[receiver], function.Name.Name)
		}
	}
	for name := range collected {
		sort.Strings(collected[name])
	}
	return collected
}

// scanGodTypeDeclarations 返回本包生产文件里声明过的类型名集合。
func scanGodTypeDeclarations(t *testing.T, dir string) map[string]bool {
	t.Helper()
	declared := make(map[string]bool, len(godTypeNames))
	fileSet := token.NewFileSet()
	for _, path := range godTypeSourceFiles(t, dir) {
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", filepath.Base(path), err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				declared[typeSpec.Name.Name] = true
			}
		}
	}
	return declared
}

// godTypeReceiverName 从接收者类型表达式里取出类型名（兼容 `T` 与 `*T`）。
func godTypeReceiverName(expression ast.Expr) string {
	switch receiver := expression.(type) {
	case *ast.StarExpr:
		return godTypeReceiverName(receiver.X)
	case *ast.Ident:
		return receiver.Name
	case *ast.IndexExpr:
		return godTypeReceiverName(receiver.X)
	case *ast.IndexListExpr:
		return godTypeReceiverName(receiver.X)
	default:
		return ""
	}
}

// godTypeSourceFiles 返回目录下的生产 .go 文件（非递归、排除测试文件）。
func godTypeSourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s: %v", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)
	return paths
}

func containsGodType(name string) bool {
	for _, candidate := range godTypeNames {
		if candidate == name {
			return true
		}
	}
	return false
}

// storePackageDir 返回本测试文件所在目录（= store 包根）。
func storePackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 ratchet 测试源码")
	}
	return filepath.Dir(file)
}
