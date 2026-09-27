package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// 串行 bash 的时长预算：预算本身是 serialBashBudget，入口说明在三份工具描述里，
// 规则条文在系统提示词里。这个文件钉住四件事——拒绝、边界、开关不误伤，
// 以及"描述与提示词里的那个数字就是代码执行的那个数字"。

// TestSerialBashBudgetRefusesOverBudgetTimeout：超预算的显式 timeout 必须被拒，且
// 拒绝消息要能照着做（点名 bash_bg 与预算数字）。两个串行入口同一条口径：串行是
// 时长这条线的判据，写类/只读不是。
func TestSerialBashBudgetRefusesOverBudgetTimeout(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-budget")
	for _, call := range []struct {
		tool string
		run  func(context.Context, string) (string, error)
		args string
	}{
		{"bash", router.scopedBash, `{"command":"echo long","timeout":600}`},
		{"bash_read", router.scopedBashRead, `{"command":"ls -la","timeout":600}`},
	} {
		_, err := call.run(ctx, call.args)
		if err == nil {
			t.Fatalf("%s 接受了超预算 timeout：应拒绝并指向 bash_bg", call.tool)
		}
		if !strings.Contains(err.Error(), "bash_bg") || !strings.Contains(err.Error(), serialBashBudgetLabel()) {
			t.Fatalf("%s 的拒绝消息必须点名替代入口与预算：%v", call.tool, err)
		}
	}
}

// TestSerialBashBudgetKeepsItsBoundary：边界两边各一格，外加两种**不该**拒绝的情形
// ——预算内的显式 timeout 照跑、没声明 timeout 的调用没有"声明的时长"可审查、
// 作业面关闭时没有替代入口所以不能拒绝（否则既不执行也不给路）。
func TestSerialBashBudgetKeepsItsBoundary(t *testing.T) {
	on := asyncTestRouter(t, true)
	budgetSeconds := int(serialBashBudget / time.Second)
	if err := on.auditSerialTimeout("bash", budgetSeconds); err != nil {
		t.Fatalf("预算之内（%ds）不得拒绝：%v", budgetSeconds, err)
	}
	if err := on.auditSerialTimeout("bash", budgetSeconds+1); err == nil {
		t.Fatal("超过预算 1 秒就该拒绝")
	}
	if err := on.auditSerialTimeout("bash", 0); err != nil {
		t.Fatalf("未声明 timeout 不受审查：%v", err)
	}
	if err := asyncTestRouter(t, false).auditSerialTimeout("bash", 3600); err != nil {
		t.Fatalf("作业面关闭时不得拒绝长命令（没有 bash_bg 可派发）：%v", err)
	}
}

// TestSerialBashBudgetLeavesNormalCallsAlone：预算之内的命令真的照跑（审查不是
// 空转的常量比大小，它插在执行路径上且只拦那一类）。
func TestSerialBashBudgetLeavesNormalCallsAlone(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-budget-ok")
	payload, err := router.scopedBash(ctx, `{"command":"echo within-budget","timeout":30}`)
	if err != nil {
		t.Fatalf("预算之内的同步命令必须照跑：%v", err)
	}
	if !strings.Contains(payload, "within-budget") {
		t.Fatalf("预算之内的命令没有真正执行：%s", payload)
	}
}

// TestSerialBashBudgetIsWrittenInEveryEntryDescription：模型只有描述可读，所以三个
// 入口都得写明这条线——只写在串行的拒绝消息里，模型要先撞一次墙才知道。
func TestSerialBashBudgetIsWrittenInEveryEntryDescription(t *testing.T) {
	registered := captureRegisteredTools(t, true)
	for _, name := range []string{"bash", "bash_read", "bash_bg"} {
		description := registered[name].description
		if !strings.Contains(description, serialBashBudgetLabel()) {
			t.Fatalf("%s 描述必须写明预算 %s：%q", name, serialBashBudgetLabel(), description)
		}
	}
	if !strings.Contains(registered["bash_bg"].description, "serial bash / bash_read refuse") {
		t.Fatalf("bash_bg 描述必须点明它是超预算命令的入口：%q", registered["bash_bg"].description)
	}
}

// TestSerialBashBudgetLabelMatchesBudget：文案必须由预算派生，不得各写一份数字。
func TestSerialBashBudgetLabelMatchesBudget(t *testing.T) {
	want := fmt.Sprintf("%d minutes", int(serialBashBudget/time.Minute))
	if got := serialBashBudgetLabel(); got != want {
		t.Fatalf("serialBashBudgetLabel() = %q, want %q", got, want)
	}
}

// TestSerialBashBudgetAgreesWithPrompt：系统提示词那份 assets 里的 5 分钟与
// serialBashBudget 之间没有类型层面的约束，只能靠这条断言把两者扣在一起：
// 改预算忘改提示词就是红。
func TestSerialBashBudgetAgreesWithPrompt(t *testing.T) {
	instructions := promptassets.SystemInstructions()
	for _, required := range []string{"Long-Running Commands", serialBashBudgetLabel(), "bash_bg", "job_manage(op=fetch, handle)"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("系统提示词缺少串行预算规则的 %q", required)
		}
	}
}
