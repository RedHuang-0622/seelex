package viewerrors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	seeleerrors "github.com/RedHuang-0622/Seele/errors"

	"github.com/RedHuang-0622/seelex/contract"
)

func TestPresentedClassificationIdentifiesSourceWithoutProviderDetails(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		module string
		method string
		forbid []string
	}{
		{
			name:   "context exhaustion",
			err:    errors.New(`engine loop 15: ChatClient stream: HTTP 400: invalid params, context window exceeds limit (2013), request_id=req-secret`),
			module: "上下文恢复", method: "recoverProviderFailure",
			forbid: []string{"HTTP", "2013", "req-secret", "400"},
		},
		{
			name:   "empty provider content",
			err:    errors.New(`engine loop 0: ChatClient stream: HTTP 400: invalid params, chat content is empty (2013), request_id=req-secret`),
			module: "会话安全", method: "prepareProviderHistory",
			forbid: []string{"HTTP", "2013", "req-secret", "400"},
		},
		{
			name:   "provider timeout",
			err:    errors.New(`engine loop 16: ChatClient stream: HTTP 504: timeout_error, request_id=req-secret`),
			module: "模型传输", method: "ChatStream",
			forbid: []string{"HTTP", "504", "req-secret", "timeout_error"},
		},
		{
			name:   "provider server error",
			err:    errors.New(`ChatClient stream: HTTP 500: server_error, request_id=req-secret`),
			module: "模型传输", method: "ChatStream",
			forbid: []string{"HTTP", "500", "req-secret", "server_error"},
		},
		{
			name:   "result ref unavailable",
			err:    errors.New(`read_tool_result: result_ref is not available in the current session`),
			module: "工具执行", method: "read_tool_result",
			forbid: []string{"result_ref is not available"},
		},
		{
			name:   "result ref required",
			err:    errors.New(`read_tool_result: result_ref is required and offset must be non-negative`),
			module: "工具执行", method: "read_tool_result",
			forbid: []string{"result_ref is required"},
		},
		{
			name:   "unknown failure",
			err:    errors.New(`unrecognized internal failure request_id=req-secret`),
			module: "代理运行时", method: "runChat",
			forbid: []string{"req-secret", "unrecognized internal"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Classify(test.err).String()
			for _, expected := range []string{"模块：" + test.module, "方法：" + test.method} {
				if !strings.Contains(got, expected) {
					t.Fatalf("presentation %q does not contain %q", got, expected)
				}
			}
			for _, forbidden := range test.forbid {
				if strings.Contains(got, forbidden) {
					t.Fatalf("presentation leaks %q: %q", forbidden, got)
				}
			}
		})
	}
}

// TestClassifyStructuredErrorsByCode 验证 slice 8 的结构化错误分类
// （seeleerrors.From 按 Code/Function 结构性读取，替代字符串匹配）：
// 结构化错误与字符串匹配产生相同分类；预算错误保持 errors.Is 语义。

func TestClassifyStructuredErrorsByCode(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		module string
		method string
	}{
		{
			name:   "persistence code",
			err:    Wrap(errors.New("storage unavailable"), contract.ErrorCodePersistenceFailed),
			module: "会话持久化", method: "persistCurrentSession",
		},
		{
			name:   "react budget code",
			err:    Wrap(fmt.Errorf("%w: no progress", contract.ErrReActBudgetExceeded), contract.ErrorCodeReActBudget),
			module: "执行预算", method: "finalizeReActBudget",
		},
		{
			name:   "context exceeded code",
			err:    Wrap(fmt.Errorf("estimated=1 budget=1"), contract.ErrorCodeContextExceeded),
			module: "上下文恢复", method: "recoverProviderFailure",
		},
		{
			name:   "function field inference",
			err:    seeleerrors.Wrap(errors.New("boom"), seeleerrors.Context{Function: "persistCurrentSession"}),
			module: "会话持久化", method: "persistCurrentSession",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Classify(test.err).String()
			for _, expected := range []string{"模块：" + test.module, "方法：" + test.method} {
				if !strings.Contains(got, expected) {
					t.Fatalf("presentation %q does not contain %q", got, expected)
				}
			}
		})
	}

	// 结构化包装不破坏预算错误的 errors.Is 语义。
	budgetErr := Wrap(fmt.Errorf("%w: no progress", contract.ErrReActBudgetExceeded), contract.ErrorCodeReActBudget)
	if !errors.Is(budgetErr, contract.ErrReActBudgetExceeded) {
		t.Fatal("errors.Is(contract.ErrReActBudgetExceeded) broken through structured envelope")
	}
	// 取消语义优先于结构化分类（与旧分类顺序一致）。
	canceled := Wrap(fmt.Errorf("wrapped: %w", context.Canceled), "some.other.code")
	if got := Classify(canceled).String(); !strings.Contains(got, "任务已停止") {
		t.Fatalf("canceled error should present as 任务已停止, got %q", got)
	}
}
