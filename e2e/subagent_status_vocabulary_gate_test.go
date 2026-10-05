package e2e

// subagent_status_vocabulary_gate_test.go — ③U6 的机械门禁（源码扫描断言）。
//
// 判据（③U6「'在跑'状态字面量残留点」）：**"这一轮跑到哪"这一格的状态词只有两份定义**
// ——"还在不在跑"那两个词在契约里（`workunit.StatusQueued/StatusRunning`，经 `InFlight` 判），
// 四个取值面在对外契约里（`dto.SubAgentQueued/Running/Done/Failed`，`session` 包再导出成
// `SubAgent*`）。记录状态那一格的写方与读方只许**引用**它们，不许再写一份字面量。
//
// 为什么要有门禁而不是"我记得"：状态词漂移不会编译报错，也不会让既有用例变红——写的那一处
// 从 "done" 换成别的词，读的那一处照样编译、照样通过，直到"记录说已完成、看板说还在跑"。
// 所以门禁按**形态**查四处最容易被漏掉的位置：
//
//	① 状态比较：`status == "done"` / `!= "failed"` …（`case status == "x":` 同形）
//	② 状态赋值：`status := "done"` / `status = "failed"` …
//	③ 状态字段：`subagentOutcome{status: "done"}` …
//	④ switch 状态分支：`switch nr.Status { case "failed": … }`
//
// **门禁的边界写在这里**（免得它变成一块越界的大毯子）：只扫**记录状态那一格**的写方与读方
// （`recordStatusChainFiles`，逐文件写了它在这一格里干什么）。同名同形的另几张词表
// ——工具事件状态（running|success|error）、后台作业状态（running|done|failed|killed）、
// 计划批次结果状态（completed|failed|…）、todo 三态、统一事件摘要状态——**不是这一格**，
// 它们的字面量由各自的一批收口，本门禁不越界判它们（越界判 = 只能靠白名单放过，白名单一长
// 就等于没有判据）。

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

// recordStatusChainFiles 是记录状态那一格的写方与读方（仓库相对路径 → 它在这一格里干什么）。
// 新增一个"读/写会话记录 status"的文件时，把它加进来——门禁自己会检查清单里的文件都还在。
var recordStatusChainFiles = map[string]string{
	"seelebridge/node/coordinator.go":          "写：节点结束 → Sessions.NoteOutcome 的终态词",
	"seelebridge/session/subagent_sessions.go": "写：节点结束兜底终态（subagentOutcome.status）",
	"seelebridge/session/subagent_tree.go":     "读：恢复时判「记录本身已终结」（restoredSubAgentStatus）",
	"seelebridge/runtime_subagent_recovery.go": "读：残留记录判终态（Locate/ensureConclusion）",
	"seelebridge/runtime_subagent_resume.go":   "读+本层常量：子代理那一层的终态词表",
	"seelebridge/workunit_team.go":             "写：teammate 那一层的终态词表（收尾分类折成记录词）",
	"seelebridge/workunit_team_records.go":     "写：teammate 单元记录的落盘写点",
	"seelebridge/workunit_parent.go":           "写：生命周期实现落盘时的记录状态",
}

// recordStatusWords 是这一格的取值面（= dto.SubAgent 的四个 + 契约那两个字面量同值）。
var recordStatusWords = map[string]bool{
	"queued": true, "running": true, "done": true, "failed": true,
}

// statusAllowedLiteral 是白名单条目：另一张词表的一处字面量 + 理由（"为什么像却不并"）。
type statusAllowedLiteral struct {
	file   string
	word   string
	reason string
}

// allowedRecordStatusLiterals 目前**为空**：这一格里一个字面量都不该有。
// 条目只在"确认是另一张表、且不打算本轮收"时才允许加，并且必须写明理由与它的去向；
// 过期条目（指不到现存的命中）也会红——白名单不是藏东西的地毯。
var allowedRecordStatusLiterals = []statusAllowedLiteral{}

// statusWordViolation 是一次命中。
type statusWordViolation struct {
	file    string
	line    int
	form    string
	word    string
	snippet string
}

func (v statusWordViolation) String() string {
	return fmt.Sprintf("%s:%d [%s] 状态词字面量 %q：%s", v.file, v.line, v.form, v.word, v.snippet)
}

// TestSubagentStatusVocabularyGate 是门禁本体：扫真源码，命中必须整体落在白名单里。
func TestSubagentStatusVocabularyGate(t *testing.T) {
	root := repoRoot()

	files := make([]string, 0, len(recordStatusChainFiles))
	for file := range recordStatusChainFiles {
		files = append(files, file)
	}
	sort.Strings(files)

	violations := []statusWordViolation{}
	for _, file := range files {
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			// 清单里的文件不见了 = 门禁自己的失败：宁可红，不要静默少扫一个。
			t.Fatalf("清单里的 %s 读不到（%s）：%v", file, recordStatusChainFiles[file], err)
		}
		found, err := scanRecordStatusLiterals(file, source)
		if err != nil {
			t.Fatalf("%s 解析失败：%v", file, err)
		}
		violations = append(violations, found...)
	}

	remaining := make([]statusWordViolation, 0, len(violations))
	used := make([]bool, len(allowedRecordStatusLiterals))
	for _, violation := range violations {
		matched := false
		for index, allowed := range allowedRecordStatusLiterals {
			if used[index] {
				continue
			}
			if allowed.file == violation.file && allowed.word == violation.word {
				used[index] = true
				matched = true
				break
			}
		}
		if !matched {
			remaining = append(remaining, violation)
		}
	}

	for _, violation := range remaining {
		t.Errorf("%v\n  记录状态那一格只有两份词表（workunit.Status* 判\"还在不在跑\"、"+
			"dto.SubAgent* 给四个取值面）。引它们，或把\"为什么像却不并\"写进 allowedRecordStatusLiterals",
			violation)
	}
	for index, allowed := range allowedRecordStatusLiterals {
		if !used[index] {
			t.Errorf("白名单过期：%s 的 %q 已经不再命中（条目理由：%s）——请删掉这一条",
				allowed.file, allowed.word, allowed.reason)
		}
	}

	if len(remaining) > 0 || t.Failed() {
		t.Log("口径见 docs/arch/workunit-duplication-inventory.md §四 U6 与本包交付记录（③U6 段）")
	}
}

// TestSubagentStatusVocabularyGateCatchesViolations 是门禁的**阴性对照**：故意违规的样例
// 必须被同一套扫描函数判红，引用契约常量的样例必须判绿。没有这一条，门禁就只是"一段没人
// 验过的正则"。
func TestSubagentStatusVocabularyGateCatchesViolations(t *testing.T) {
	cases := []struct {
		name   string
		source string
		word   string
	}{
		{"状态比较", `package p

func done(status string) bool { return status == "done" }
`, "done"},
		{"状态赋值", `package p

func mark(err error) string {
	status := "failed"
	return status
}
`, "failed"},
		{"状态字段", `package p

type outcome struct{ status string }

func makeOutcome() outcome { return outcome{status: "done"} }
`, "done"},
		{"switch 状态分支", `package p

type record struct{ Status string }

func bucket(r record) bool {
	switch r.Status {
	case "queued":
		return true
	}
	return false
}
`, "queued"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := scanRecordStatusLiterals("sample.go", []byte(testCase.source))
			if err != nil {
				t.Fatalf("样例必须能解析：%v", err)
			}
			for _, violation := range found {
				if violation.word == testCase.word {
					return
				}
			}
			t.Fatalf("样例必须被判红（%q），实际命中：%v", testCase.word, found)
		})
	}

	// 反向对照：引契约常量不是违规。
	clean := `package p

import "github.com/RedHuang-0622/seelex/application/contract/dto"

func done(status string) bool { return status == string(dto.SubAgentDone) }
`
	found, err := scanRecordStatusLiterals("clean.go", []byte(clean))
	if err != nil {
		t.Fatalf("样例必须能解析：%v", err)
	}
	if len(found) != 0 {
		t.Fatalf("引用契约常量不该被判红，实际命中：%v", found)
	}
}

// scanRecordStatusLiterals 在一份源码里找"记录状态词字面量"的四种形态（判据见文件头）。
func scanRecordStatusLiterals(relative string, source []byte) ([]statusWordViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relative, source, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: 解析失败：%w", relative, err)
	}

	violations := []statusWordViolation{}
	report := func(literal *ast.BasicLit, form string) {
		word, unquoteErr := strconv.Unquote(literal.Value)
		if unquoteErr != nil || !recordStatusWords[word] {
			return
		}
		position := fset.Position(literal.Pos())
		violations = append(violations, statusWordViolation{
			file: relative, line: position.Line, form: form, word: word,
			snippet: strings.TrimSpace(lineAt(source, position.Line)),
		})
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BinaryExpr:
			if typed.Op != token.EQL && typed.Op != token.NEQ {
				return true
			}
			if literal, ok := typed.Y.(*ast.BasicLit); ok && isStatusExpression(typed.X, source) {
				report(literal, "状态比较")
			}
			if literal, ok := typed.X.(*ast.BasicLit); ok && isStatusExpression(typed.Y, source) {
				report(literal, "状态比较")
			}
		case *ast.AssignStmt:
			for index, left := range typed.Lhs {
				identifier, ok := left.(*ast.Ident)
				if !ok || !isStatusName(identifier.Name) || index >= len(typed.Rhs) {
					continue
				}
				if literal, ok := typed.Rhs[index].(*ast.BasicLit); ok {
					report(literal, "状态赋值")
				}
			}
		case *ast.KeyValueExpr:
			identifier, ok := typed.Key.(*ast.Ident)
			if !ok || !isStatusName(identifier.Name) {
				return true
			}
			if literal, ok := typed.Value.(*ast.BasicLit); ok {
				report(literal, "状态字段")
			}
		case *ast.SwitchStmt:
			if typed.Tag == nil || !isStatusExpression(typed.Tag, source) {
				return true
			}
			for _, clause := range typed.Body.List {
				caseClause, ok := clause.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expression := range caseClause.List {
					if literal, ok := expression.(*ast.BasicLit); ok {
						report(literal, "switch 状态分支")
					}
				}
			}
		}
		return true
	})
	return violations, nil
}

// isStatusName 报告一个标识符名是不是"状态位"（status / state 大小写不敏感的子串）。
func isStatusName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "status") || strings.Contains(lower, "state")
}

// isStatusExpression 报告一个表达式是不是"状态位"（用它自己的源码文本判）。
func isStatusExpression(expression ast.Expr, source []byte) bool {
	if expression == nil {
		return false
	}
	start := int(expression.Pos()) - 1
	end := int(expression.End()) - 1
	if start < 0 || end > len(source) || start >= end {
		return false
	}
	return isStatusName(string(source[start:end]))
}

// lineAt 取源码第 line 行（1 起算；越界返回空串）。
func lineAt(source []byte, line int) string {
	lines := strings.Split(string(source), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}
