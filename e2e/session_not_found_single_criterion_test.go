package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 本文件是 P10「会话不存在判据漂移」的机械收口（AGENTS §8.3：同一判据、同一语义
// 只允许一处）。修复前实测到的漂移面是 **7 处、4 个包**，比提案里写的"两份"更宽：
//
//	store/durable_history.go                  func isSessionNotFound （只看 fs.ErrNotExist）
//	seelexctx/search/search.go                func isSessionNotFound （fs + sql.ErrNoRows）
//	application/core/session_runtime/archive.go              内联 （fs + sql.ErrNoRows）
//	wiring/adapters/session_workspace_ports.go               4 处内联（fs + sql.ErrNoRows）
//
// 收口方向：判据归「未找到」错误的**编码者** store——唯一实现是
// `store.IsSessionNotFound`（store/durable_history.go），其余消费方改为调用它。
// 为什么保留 store 的语义（fs-only）而不是 search 的：SQL 后端已退役
// （store/sessionstore.go 的 `ErrBackendRetired`，Open 只接受 BackendJSON），
// 今天没有任何代码路径能产生 `sql.ErrNoRows`；多认它只会让同一份输入得到两种答案。
//
// 两条机械断言：
//
//	1. 全仓只有一个「会话不存在」判据函数，且它就是 store.IsSessionNotFound；
//	2. 退役后端的 `sql.ErrNoRows` 与 `database/sql` 不许再作为**代码**出现，
//	   允许清单里只准放"把它当输入数据喂给判据"的用例文件（只许收缩）。
//
// 断言边界（诚实说明，别把这条门禁当成全覆盖）：本文件只能证明"判据函数唯一 +
// 退役后端的错误码不再被代码引用"。一个**新的**内联 `errors.Is(err, fs.ErrNotExist)`
// 副本在语法上与其它合法用法不可区分，机械手段抓不到——那部分靠 review：需要判定
// 「会话不存在」时应扩展 store 的那一处，而不是就地内联（见 store/README.md）。

const (
	// sessionNotFoundCriterionPath 是唯一判据所在文件（相对仓库根）。
	sessionNotFoundCriterionPath = "store/durable_history.go"
	// sessionNotFoundCriterionName 是唯一判据的函数名。
	sessionNotFoundCriterionName = "IsSessionNotFound"
	// sessionNotFoundCriterionAllowListMax 是 `sql.ErrNoRows` 用例数据允许清单上限。
	// 只许收缩：今天只有一条（把已退役后端的错误码当输入喂给判据的用例）。
	sessionNotFoundCriterionAllowListMax = 1
)

// sessionNotFoundCriterionAllowList 登记**允许**出现 `sql.ErrNoRows` / `database/sql`
// 的文件（相对仓库根），值写清它为什么不是"第二份判据"。
var sessionNotFoundCriterionAllowList = map[string]string{
	"backend/engine/context/seelectx/search/session_not_found_test.go": "把已退役后端可能产生的 sql.ErrNoRows 当**输入数据**喂给唯一判据，" +
		"证明它必须判 false（红灯先行用例）。这是用例数据，不是第二份判据。",
}

// criterionDeclarationNames 是"会话不存在判据"可能出现的全部命名形态：
// 改名不改变判据身份，所以这里按名字集合扫，而不是只扫当前那一个名字。
var criterionDeclarationNames = []string{
	"IsSessionNotFound",
	"isSessionNotFound",
	"sessionNotFound",
	"SessionNotFound",
	"isSessionAbsent",
}

// TestSessionNotFoundCriterionIsDeclaredExactlyOnce 钉判据唯一：全仓只允许一个
// 「会话不存在」判据函数，且必须就是 store.IsSessionNotFound。
func TestSessionNotFoundCriterionIsDeclaredExactlyOnce(t *testing.T) {
	var found []string
	for _, path := range repoGoSourceFiles(t) {
		parsed := parseGoFile(t, path)
		for _, decl := range parsed.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			if containsString(criterionDeclarationNames, function.Name.Name) {
				found = append(found, repoRelPath(t, path)+":"+function.Name.Name)
			}
		}
	}
	sort.Strings(found)
	want := sessionNotFoundCriterionPath + ":" + sessionNotFoundCriterionName
	if len(found) == 1 && found[0] == want {
		return
	}
	t.Errorf("「会话不存在」判据必须只有一处，实测 %d 处：\n  %s\n期望恰好一处：%s\n"+
		"同一判据、同一语义出现第二处即缺陷（AGENTS §8.3）。"+
		"需要判定会话不存在时，调用 store.IsSessionNotFound，不要就地复制判据。",
		len(found), strings.Join(found, "\n  "), want)
}

// TestRetiredSQLNoRowsIsNotReferencedByCode 钉退役后端的错误码不再被代码引用：
// `sql.ErrNoRows` 与 `database/sql` 只允许出现在清单登记的用例数据里。
func TestRetiredSQLNoRowsIsReferencedOnlyByListedTestData(t *testing.T) {
	if len(sessionNotFoundCriterionAllowList) > sessionNotFoundCriterionAllowListMax {
		t.Errorf("允许清单有 %d 条，超过上限 %d：这张表只许收缩",
			len(sessionNotFoundCriterionAllowList), sessionNotFoundCriterionAllowListMax)
	}
	referenced := map[string][]string{}
	for _, path := range repoGoSourceFiles(t) {
		rel := repoRelPath(t, path)
		parsed := parseGoFile(t, path)
		for _, imported := range parsed.Imports {
			if imported.Path == nil || imported.Path.Value != `"database/sql"` {
				continue
			}
			referenced[rel] = append(referenced[rel], "import database/sql")
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "ErrNoRows" {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if ok && ident.Name == "sql" {
				referenced[rel] = append(referenced[rel], "sql.ErrNoRows")
			}
			return true
		})
	}
	var unexpected []string
	for rel, reasons := range referenced {
		if _, ok := sessionNotFoundCriterionAllowList[rel]; ok {
			continue
		}
		unexpected = append(unexpected, rel+"（"+strings.Join(reasons, ", ")+"）")
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("已退役的 SQL 后端错误码被代码引用（P10 收口后判据只看 fs.ErrNotExist）：\n  %s\n"+
			"会话不存在一律经 store.IsSessionNotFound 判定；真的需要保留这类引用，"+
			"必须在 sessionNotFoundCriterionAllowList 里登记并写清为什么它不是第二份判据。",
			strings.Join(unexpected, "\n  "))
	}
	var stale []string
	for rel := range sessionNotFoundCriterionAllowList {
		if _, ok := referenced[rel]; !ok {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("允许清单里的条目已经不再引用退役后端错误码，请删掉这几条（清单只许收缩）：\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// repoGoSourceFiles 返回仓库里全部 Go 源文件（含用例），跳过非源码目录：
// 版本库元数据、依赖、构建产物、临时/草稿目录。
func repoGoSourceFiles(t *testing.T) []string {
	t.Helper()
	root := repoRoot()
	skip := map[string]bool{
		".git": true, "vendor": true, "node_modules": true, "dist": true,
		"tmp": true, "_scratch": true, "_logs": true,
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描仓库 Go 源文件失败: %v", err)
	}
	sort.Strings(files)
	return files
}

func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	return parsed
}

func repoRelPath(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot(), path)
	if err != nil {
		t.Fatalf("计算 %s 的相对路径失败: %v", path, err)
	}
	return filepath.ToSlash(rel)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
