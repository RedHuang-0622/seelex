package e2e

// subagent_status_vocabulary_gate_test.go — ③U6 的机械门禁（源码扫描断言）。
//
// 判据（③U6「'在跑'状态字面量残留点」）：**每一格状态词只有一份定义（在契约里），写方与读方
// 只许引用，不许再写一份字面量**。门禁按"格子"声明范围：每一格给出它的写方/读方文件清单
// （逐文件写清它在这一格里干什么）与它的取值面。
//
// 为什么要有门禁而不是"我记得"：状态词漂移不会编译报错，也不会让既有用例变红——写的那一处
// 从 "done" 换成别的词，读的那一处照样编译、照样通过，直到"记录说已完成、看板说还在跑"。
// 所以门禁按**形态**查五处最容易被漏掉的位置：
//
//	① 状态比较：`status == "done"` / `!= "failed"` …（`case status == "x":` 同形）
//	② 状态赋值：`status := "done"` / `status = "failed"` …
//	③ 状态字段：`subagentOutcome{status: "done"}` …
//	④ switch 状态分支：`switch nr.Status { case "failed": … }`
//	⑤ 状态常量声明：`asyncStateDone = "done"`（**第二份定义**的典型长相，前四种都抓不到它）
//
// **门禁的边界写在这里**（免得它变成一块越界的大毯子）：只扫下面 `statusVocabularyScopes` 里
// 逐条声明的文件。没进清单的同形词表由各自的一批收口，本门禁不越界判它们（越界判 = 只能靠
// 白名单放过，白名单一长就等于没有判据）。已经登记、尚未收口的格子：
//
//   - 子代理工具事件状态（running|success|error，`dto.SubagentToolEvent.Status`）与
//     工具调用状态（running|completed|failed，`contract.ToolCall.Status`）在
//     `session/tool_events.go`、`application/core/tool_hooks.go`、
//     `application/core/subagent_view/coordinator.go` 三处交织，**收口前先把读方点清点**；
//   - 计划节点状态（queued|running|completed|failed|skipped|canceled|aborted|panicked）来自
//     框架 workplan 的 `NodeBase.Status`，**不是我们这一格的词**——所以
//     `application/core/plan_tools.go`（同一文件里既读计划批次结果、又读节点状态）**故意不进
//     计划批次那一格的清单**；它读批次结果的那三个分支改成引契约常量，由编译器钉住；
//   - 统一事件摘要状态（failed|completed）是 Seele 框架 `SummaryEvent.Status` 的词；
//   - todo 三态（`application/core/work_table.go`）与"回执状态"（finished|already_finished）。

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

// statusVocabularyScope 是"一格状态词"：名字 + 写方/读方清单（仓库相对路径 → 它在这一格里
// 干什么）+ 取值面。清单里少一个文件 = 门禁少看一眼；文件不存在 = 门禁自己红（不静默跳过）。
type statusVocabularyScope struct {
	name  string
	files map[string]string
	words map[string]bool
}

// statusVocabularyScopes 是已收口的三格。加一格 = 先写清楚"谁写、谁读、取值面是什么"。
var statusVocabularyScopes = []statusVocabularyScope{
	{
		name: "记录状态",
		files: map[string]string{
			"seelebridge/node/coordinator.go":          "写：节点结束 → Sessions.NoteOutcome 的终态词",
			"seelebridge/session/subagent_sessions.go": "写：节点结束兜底终态（subagentOutcome.status）",
			"seelebridge/session/subagent_tree.go":     "读：恢复时判「记录本身已终结」（restoredSubAgentStatus）",
			"seelebridge/runtime_subagent_recovery.go": "读：残留记录判终态（Locate/ensureConclusion）",
			"seelebridge/runtime_subagent_resume.go":   "读+本层常量：子代理那一层的终态词表",
			"seelebridge/workunit_team.go":             "写：teammate 那一层的终态词表（收尾分类折成记录词）",
			"seelebridge/workunit_team_records.go":     "写：teammate 单元记录的落盘写点",
			"seelebridge/workunit_parent.go":           "写：生命周期实现落盘时的记录状态",
		},
		// 这一格的取值面 = dto.SubAgent* 的四个（"还在不在跑"那半份在 workunit.Status*）。
		words: map[string]bool{"queued": true, "running": true, "done": true, "failed": true},
	},
	{
		name: "后台作业状态",
		files: map[string]string{
			"seelebridge/tools/async_exec.go":      "写：登记表的状态词表（本层常量）与状态迁移",
			"seelebridge/tools/async_probe.go":     "读：探针把状态折进工作表格/观察行",
			"seelebridge/tools/async_run.go":       "写：执行体落终态",
			"seelebridge/tools/job_contract.go":    "读：作业面把执行域状态映射成契约状态",
			"seelebridge/tools/job_run.go":         "写：作业面执行体落终态",
			"seelebridge/tools/job_subagent.go":    "读：子代理作业的终态判定",
			"seelebridge/tools/job_tools.go":       "读：job_manage 的回答里带状态",
			"application/core/async_completion.go": "读：终态触发对话（done/failed 触发，killed 不触发）",
			"application/core/work_table_async.go": "读：折成工作表格的权威状态（killed 归 failed）",
		},
		// 这一格的取值面 = dto.AsyncState*（AsyncRunRecord.State 的注释写死的四个词）。
		words: map[string]bool{"running": true, "done": true, "failed": true, "killed": true},
	},
	{
		name: "计划批次结果状态",
		files: map[string]string{
			"seelebridge/plan/tool_provider.go": "写：plan_run 结果 JSON 的 status（读侧见文件头：故意不进清单）",
		},
		// 这一格的取值面 = dto.PlanRunStatus*（plan_run 工具结果的 status）。
		words: map[string]bool{"completed": true, "failed": true, "aborted": true},
	},
	{
		name: "工具事件状态",
		files: map[string]string{
			"seelebridge/session/tool_events.go":            "写：工具调用事件的三种状态（发布/落态）",
			"application/core/tool_hooks.go":                "写：工具完成钩子把结果折成成功/失败",
			"application/core/subagent_view/coordinator.go": "读：详情投影判「在跑」",
			"tui/state.go": "读：TUI 判「在跑」（转调契约枚举的对外词）",
		},
		// 这一格的取值面 = dto.ToolEvent*（SubagentTool/SubagentToolEvent.Status）。
		words: map[string]bool{"running": true, "success": true, "error": true},
	},
	{
		name: "task 状态",
		files: map[string]string{
			"seelebridge/task/task.go":       "写：task 注册表的状态迁移与打点状态",
			"application/core/work_table.go": "读：折成工作表格行（子代理行沿用 done 的显示映射）",
		},
		// 这一格的取值面 = dto.Task*（TaskRecord.Status / TaskTracePoint.Status）。
		words: map[string]bool{
			"pending": true, "queued": true, "running": true, "doing": true,
			"completed": true, "failed": true, "retry": true, "interrupted": true,
		},
	},
}

// statusAllowedLiteral 是白名单条目：另一张词表的一处字面量 + 理由（"为什么像却不并"）。
type statusAllowedLiteral struct {
	scope  string
	file   string
	word   string
	reason string
}

// allowedStatusLiterals 目前只放**确认是另一张表**的三处（写了理由与去向）。
// 条目只在"确认是另一张表、且不打算本轮收"时才允许加；过期条目（指不到现存的命中）也会红
// ——白名单不是藏东西的地毯。
var allowedStatusLiterals = []statusAllowedLiteral{
	{
		scope: "后台作业状态", file: "seelebridge/tools/async_exec.go", word: "killed",
		reason: "这是**回执状态**（asyncPayload.Status：observed|killed|already_finished|finished…），" +
			"不是执行域状态机那一格（同一结构里的 State 才是，值是 asyncState*）；" +
			"回执词表的收口要连 job_manage 的四种 op 一起做，登记为下一批",
	},
	{
		scope: "计划批次结果状态", file: "seelebridge/plan/tool_provider.go", word: "failed",
		reason: "`switch nr.Status` 读的是**框架 workplan 的节点状态**（NodeBase.Status：completed|failed|…），" +
			"不是 plan_run 结果里这一批的 status（那个是本格，已引 dto.PlanRunStatus*）",
	},
	{
		scope: "计划批次结果状态", file: "seelebridge/plan/tool_provider.go", word: "completed",
		reason: "同上（框架节点状态词的另一半）",
	},
}

// statusWordViolation 是一次命中。
type statusWordViolation struct {
	scope   string
	file    string
	line    int
	form    string
	word    string
	snippet string
}

func (v statusWordViolation) String() string {
	return fmt.Sprintf("[%s] %s:%d [%s] 状态词字面量 %q：%s", v.scope, v.file, v.line, v.form, v.word, v.snippet)
}

// TestSubagentStatusVocabularyGate 是门禁本体：扫真源码，命中必须整体落在白名单里。
func TestSubagentStatusVocabularyGate(t *testing.T) {
	root := repoRoot()

	violations := []statusWordViolation{}
	for _, scope := range statusVocabularyScopes {
		files := make([]string, 0, len(scope.files))
		for file := range scope.files {
			files = append(files, file)
		}
		sort.Strings(files)

		for _, file := range files {
			source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
			if err != nil {
				// 清单里的文件不见了 = 门禁自己的失败：宁可红，不要静默少扫一个。
				t.Fatalf("「%s」清单里的 %s 读不到（%s）：%v", scope.name, file, scope.files[file], err)
			}
			found, err := scanStatusWordLiterals(file, source, scope.words)
			if err != nil {
				t.Fatalf("%s 解析失败：%v", file, err)
			}
			for index := range found {
				found[index].scope = scope.name
			}
			violations = append(violations, found...)
		}
	}

	remaining := make([]statusWordViolation, 0, len(violations))
	used := make([]bool, len(allowedStatusLiterals))
	for _, violation := range violations {
		matched := false
		for index, allowed := range allowedStatusLiterals {
			if used[index] {
				continue
			}
			if allowed.scope == violation.scope && allowed.file == violation.file && allowed.word == violation.word {
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
		t.Errorf("%v\n  这一格的状态词只有一份定义（在契约里）。引它，或把\"为什么像却不并\"写进 allowedStatusLiterals",
			violation)
	}
	for index, allowed := range allowedStatusLiterals {
		if !used[index] {
			t.Errorf("白名单过期：%s 的 %q 已经不再命中（条目理由：%s）——请删掉这一条",
				allowed.file, allowed.word, allowed.reason)
		}
	}

	if len(remaining) > 0 || t.Failed() {
		t.Log("口径见 docs/arch/workunit-duplication-inventory.md §四 U6 与 docs/2026-10-06-workunit-jobs-port/ 的落地记录")
	}
}

// TestSubagentStatusVocabularyGateCatchesViolations 是门禁的**阴性对照**：故意违规的样例
// 必须被同一套扫描函数判红，引用契约常量的样例必须判绿。没有这一条，门禁就只是"一段没人
// 验过的正则"。
func TestSubagentStatusVocabularyGateCatchesViolations(t *testing.T) {
	words := map[string]bool{"queued": true, "running": true, "done": true, "failed": true, "killed": true}
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
		{"状态常量声明", `package p

const (
	asyncStateDone   = "done"
	asyncStateKilled = "killed"
)
`, "killed"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := scanStatusWordLiterals("sample.go", []byte(testCase.source), words)
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

const asyncStateDone = dto.AsyncStateDone

func done(status string) bool { return status == dto.AsyncStateDone || status == dto.SubAgentDone.String() }
`
	found, err := scanStatusWordLiterals("clean.go", []byte(clean), words)
	if err != nil {
		t.Fatalf("样例必须能解析：%v", err)
	}
	if len(found) != 0 {
		t.Fatalf("引用契约常量不该被判红，实际命中：%v", found)
	}
}

// scanStatusWordLiterals 在一份源码里找"状态词字面量"的五种形态（判据见文件头）。
// words 是这一格的取值面：不在取值面里的字符串字面量不判（各格互不越界）。
func scanStatusWordLiterals(relative string, source []byte, words map[string]bool) ([]statusWordViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relative, source, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: 解析失败：%w", relative, err)
	}

	violations := []statusWordViolation{}
	report := func(literal *ast.BasicLit, form string) {
		word, unquoteErr := strconv.Unquote(literal.Value)
		if unquoteErr != nil || !words[word] {
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
		case *ast.ValueSpec:
			// 常量/变量声明就是"第二份定义"本人：`asyncStateDone = "done"`。
			for index, name := range typed.Names {
				if !isStatusName(name.Name) || index >= len(typed.Values) {
					continue
				}
				if literal, ok := typed.Values[index].(*ast.BasicLit); ok {
					report(literal, "状态常量声明")
				}
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
