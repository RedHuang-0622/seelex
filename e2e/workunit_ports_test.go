package e2e

// workunit_ports_test.go — 作业面与装配的**机械门禁**（源码扫描断言）。
//
// 它把 docs/arch/workunit-ports-and-assembly.md §4 的三条判据变成机器能查的事实，而不是"信我
// 一句代码里没有"：
//
//	① 生命周期实现（seelebridge/workunit_parent.go）里**没有按层分支**（不出现 KindTeammate /
//	   KindSubagent 这类"按层判断"的证据）；
//	② 生命周期实现里**没有 teamwork / worktree / sessionstore 的具体类型**（只有端口）；
//	③ **每个作业面/生命周期实现都带编译期断言**（契约那份 Jobs 的实现、tools 自建表的同形那一格、
//	   生命周期实现本身、装配处三格端口）。
//
// 外加一条**依赖方向**门禁：契约包（seelebridge/workunit）**不得** import `worktree`（判据是
// 零命中）。这一条原先是"已登记例外"（只允许 `classify.go` 用 worktree 的两个哨兵错误），
// 步骤③E 把哨兵搬进了契约（`workunit.ErrUncommittedChanges` / `ErrMergeBlockedByMain`）——
// 依赖方向正了过来（实现包 import 契约，而不是反过来），例外因此撤掉，门禁改成硬判据。
//
// 判据本体的**阴性对照在本文件里**（TestWorkunitPortGateCatchesViolations 用故意违规的样例喂
// 给同一套扫描函数，断言它真的会红）——门禁不靠"我肉手改一次源码试过了"。

import (
	"fmt"
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

// lifecycleImplementationFile 是"一份实现"的落点：生命周期实现（唯一一份）。
const lifecycleImplementationFile = "seelebridge/workunit_parent.go"

// assemblyFile 是装配处：具体类型只允许出现在这里。
const assemblyFile = "seelebridge/workunit_assembly.go"

// forbiddenPackages 是生命周期实现**不许 import** 的包（它们的具体类型只允许出现在装配处）。
var forbiddenPackages = map[string]string{
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork": "teamwork",
	"github.com/RedHuang-0622/seelex/seelebridge/worktree": "worktree",
	"github.com/RedHuang-0622/seelex/sessionstore":         "sessionstore",
	"github.com/RedHuang-0622/seelex/seelebridge/session":  "session",
}

// forbiddenQualifiers 是同一件事在源码里的第二种形态：包名限定的选择器
// （`teamwork.WorkerRequest` 这种）。import 与限定符两条都查，漏一条就等于没查。
var forbiddenQualifiers = map[string]string{
	"teamwork":     "teamwork",
	"worktree":     "worktree",
	"sessionstore": "sessionstore",
	"session":      "session",
}

// layerBranchIdentifiers 是"按层判断"的字面证据：实现里出现它们，说明层差异漏进了实现
// （层差异只允许出现在装配表与策略的调用点上；Kind 是描述性标注，不是分支判据）。
var layerBranchIdentifiers = map[string]bool{
	"KindTeammate": true,
	"KindSubagent": true,
}

// portGateViolation 是一条违规：文件名 + 人话理由。
type portGateViolation struct {
	file   string
	reason string
}

func (v portGateViolation) String() string { return v.file + ": " + v.reason }

// scanLifecycleImplementation 扫一个实现文件的**两个否定判据**：不许按层分支、不许出现具体类型。
//
// 用 go/parser 走 AST 而不是拿正则扫文本：注释与字符串里的同名字样不该拦人（文档里当然会写
// "teamwork.WorkerRequest 不出本文件"），而代码里的每一次出现都必须被逮住。
func scanLifecycleImplementation(file, source string) []portGateViolation {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, source, 0)
	if err != nil {
		return []portGateViolation{{file: file, reason: "源码解析失败（门禁自身也要能看出文件坏了）：" + err.Error()}}
	}
	var violations []portGateViolation
	for _, imported := range parsed.Imports {
		path, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		if name, banned := forbiddenPackages[path]; banned {
			violations = append(violations, portGateViolation{
				file:   file,
				reason: "生命周期实现不得 import " + name + "（具体类型只允许出现在装配处）：" + path,
			})
		}
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			if layerBranchIdentifiers[typed.Name] {
				violations = append(violations, portGateViolation{
					file:   file,
					reason: "生命周期实现里出现按层分支证据 " + typed.Name + "（层差异只允许出现在装配表与策略的调用点上）",
				})
			}
		case *ast.SelectorExpr:
			qualified, ok := typed.X.(*ast.Ident)
			if !ok {
				return true
			}
			if name, banned := forbiddenQualifiers[qualified.Name]; banned {
				violations = append(violations, portGateViolation{
					file:   file,
					reason: "生命周期实现里出现具体类型 " + qualified.Name + "." + typed.Sel.Name + "（" + name + " 只允许出现在装配处）",
				})
			}
		}
		return true
	})
	return violations
}

// requireSnippet 是"编译期断言仍在原处"的**存在性**判据：断言是一行源码，源码里必须找得到它。
func requireSnippet(file, source, snippet, reason string) []portGateViolation {
	if strings.Contains(source, snippet) {
		return nil
	}
	return []portGateViolation{{file: file, reason: reason + "（找不到 " + snippet + "）"}}
}

// requireSnippetCount 是"至少 N 处"的存在性判据（装配处三格端口的断言不怕排版变，只怕没了）。
func requireSnippetCount(file, source, snippet string, want int, reason string) []portGateViolation {
	if got := strings.Count(source, snippet); got >= want {
		return nil
	}
	return []portGateViolation{{file: file, reason: reason + "（断言 " + snippet + " 只找到 " + strconv.Itoa(strings.Count(source, snippet)) + " 处，少于 " + strconv.Itoa(want) + "）"}}
}

// worktreeDependentsIn 返回 dir 下 import worktree 的 `.go` 文件名（不含 `_test.go`）。
//
// dir 就是契约包目录（生产：`<root>/seelebridge/workunit`）。做成"给目录"而不是"给仓库根"，
// 是为了让阴性对照能拿一个**临时假目录**喂它（门禁必须有对照，不能只靠肉手试一次）。
func worktreeDependentsIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dependents []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("%s 解析失败：%w", path, err)
		}
		for _, imported := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr == nil && importPath == "github.com/RedHuang-0622/seelex/seelebridge/worktree" {
				dependents = append(dependents, name)
			}
		}
	}
	sort.Strings(dependents)
	return dependents, nil
}

// TestWorkunitPortGate 是门禁本体：扫真源码，任何一条不成立就红。
func TestWorkunitPortGate(t *testing.T) {
	root := repoRoot()
	read := func(relative string) string {
		t.Helper()
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("读 %s 失败：%v", relative, err)
		}
		return string(source)
	}

	var violations []portGateViolation
	lifecycle := read(lifecycleImplementationFile)
	violations = append(violations, scanLifecycleImplementation(lifecycleImplementationFile, lifecycle)...)

	// ③ 每个实现都带编译期断言（契约那份 Jobs 的实现 / tools 自建表同形的那一格 / 生命周期
	// 实现本身 / 装配处三格端口）。
	violations = append(violations, requireSnippet(
		"seelebridge/workunit/contract.go", read("seelebridge/workunit/contract.go"),
		"var _ Jobs = (jobs.Manager)(nil)",
		"契约必须钉住『作业面的实现 = jobs.Manager』")...)
	violations = append(violations, requireSnippet(
		"seelebridge/tools/async_exec.go", read("seelebridge/tools/async_exec.go"),
		"var _ workunit.JobSignals = (*asyncRegistry)(nil)",
		"tools 自建作业表必须钉住它与作业面同形的那一格")...)
	violations = append(violations, requireSnippet(
		lifecycleImplementationFile, lifecycle,
		"var _ workunit.Lifecycle = (*lifecycleHost)(nil)",
		"生命周期实现必须钉住『这就是契约那份实现』")...)
	violations = append(violations, requireSnippetCount(
		assemblyFile, read(assemblyFile),
		"(*hostPorts)(nil)", 3,
		"装配处建的三格端口（现场/编排/记录）必须逐格钉住实现")...)

	// 依赖方向（③E 起是硬判据）：契约包**不得** import worktree——哨兵错误已经在契约里
	// （`workunit.ErrUncommittedChanges` / `ErrMergeBlockedByMain`），现场实现反过来 import
	// 契约去构造它们。这里判"零命中"，没有例外名单。
	dependents, err := worktreeDependentsIn(filepath.Join(root, "seelebridge", "workunit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dependents) != 0 {
		violations = append(violations, portGateViolation{
			file:   "seelebridge/workunit",
			reason: "契约包不得 import worktree（哨兵错误已搬进契约；依赖方向是 实现 → 契约），实际依赖它的文件：" + strings.Join(dependents, ", "),
		})
	}

	for _, violation := range violations {
		t.Error(violation.String())
	}
	if t.Failed() {
		t.Log("口径见 docs/arch/workunit-ports-and-assembly.md §2/§4 与 seelebridge/workunit/README.md")
	}
}

// TestWorkunitPortGateCatchesViolations 是门禁的**阴性对照**：故意违规的样例必须让它变红。
//
// 三条样例各对应上面的一条判据（按层分支 / 具体类型 / 断言缺席）；没有这一条，门禁就只是
// "一段没人验过的正则"，而不是"拦得住人"。
func TestWorkunitPortGateCatchesViolations(t *testing.T) {
	// ① 按层分支 + ② 具体类型（import 与限定符两种形态都在一份样例里）。
	branching := `package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/teamwork"

func (h *lifecycleHost) broken(u workunit.Unit) {
	if u.Kind() == workunit.KindTeammate {
		_ = teamwork.WorkerRequest{}
	}
}
`
	violations := scanLifecycleImplementation("seelebridge/workunit_parent.go", branching)
	if len(violations) == 0 {
		t.Fatal("故意违规样例（按层分支 + teamwork 具体类型）没有被门禁逮住")
	}
	reasons := make([]string, 0, len(violations))
	for _, violation := range violations {
		reasons = append(reasons, violation.String())
	}
	joined := strings.Join(reasons, "\n")
	for _, want := range []string{"KindTeammate", "teamwork", "import"} {
		if !strings.Contains(joined, want) {
			t.Errorf("违规理由里应当能看到 %q，实际：\n%s", want, joined)
		}
	}

	// 干净样例必须过（否则门禁只会永远报红，"红了也没人看"）。
	clean := `package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/workunit"

// 注释里可以照常讨论 teamwork.WorkerRequest 与 kind == 这类东西（AST 扫描不看注释）。
func (h *lifecycleHost) clean(u workunit.Unit) {
	_ = h.scenes.BeginScene(context.Background(), u.Owns(), u.ID(), u.SessionPath())
}
var _ workunit.Lifecycle = (*lifecycleHost)(nil)
`
	if violations := scanLifecycleImplementation("seelebridge/workunit_parent.go", clean); len(violations) != 0 {
		t.Fatalf("干净样例被误报：%+v", violations)
	}

	// ③ 断言缺席：把断言行拿掉，存在性判据必须报。
	if violations := requireSnippet("seelebridge/workunit/contract.go", "package workunit\n", "var _ Jobs = (jobs.Manager)(nil)", "契约必须钉住作业面的实现"); len(violations) == 0 {
		t.Fatal("契约少了编译期断言，门禁没有报出来")
	}
	if violations := requireSnippetCount(assemblyFile, "package seelebridge\n", "(*hostPorts)(nil)", 3, "装配处三格端口都要钉住"); len(violations) == 0 {
		t.Fatal("装配处少了端口断言，门禁没有报出来")
	}

	// ④ 依赖方向：一个**真的** import 了 worktree 的假契约包目录必须被逮住（③E 撤掉例外之后
	// 这条判据是"零命中"，对照用临时目录喂同一个扫描函数）。
	fakeRoot := t.TempDir()
	fakeUnit := filepath.Join(fakeRoot, "seelebridge", "workunit")
	if err := os.MkdirAll(fakeUnit, 0o755); err != nil {
		t.Fatal(err)
	}
	dirty := `package workunit

import "github.com/RedHuang-0622/seelex/seelebridge/worktree"

var _ = worktree.ErrUncommittedChanges
`
	if err := os.WriteFile(filepath.Join(fakeUnit, "classify.go"), []byte(dirty), 0o600); err != nil {
		t.Fatal(err)
	}
	dependents, err := worktreeDependentsIn(fakeUnit)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependents) != 1 || dependents[0] != "classify.go" {
		t.Fatalf("契约包 import worktree 的样例没有被门禁逮住，得到 %v", dependents)
	}
	// 干净目录（只有注释里提到 worktree）不得误报。
	cleanUnit := filepath.Join(t.TempDir(), "seelebridge", "workunit")
	if err := os.MkdirAll(cleanUnit, 0o755); err != nil {
		t.Fatal(err)
	}
	innocent := "package workunit\n\n// 注释里可以照常讨论 worktree.ErrUncommittedChanges 的去向。\nvar _ = 1\n"
	if err := os.WriteFile(filepath.Join(cleanUnit, "classify.go"), []byte(innocent), 0o600); err != nil {
		t.Fatal(err)
	}
	if dependents, err := worktreeDependentsIn(cleanUnit); err != nil || len(dependents) != 0 {
		t.Fatalf("干净样例被误报：%v err=%v", dependents, err)
	}
}
