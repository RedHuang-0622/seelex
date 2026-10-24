package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/model"
)

// TestBalanceEndpointDerivation 钉住端点推导：DeepSeek 的余额端点挂在域名根上，
// /v1 前缀要去掉（base_url 写成 /v1 时对话仍可用，余额接口却不在那儿）。
func TestBalanceEndpointDerivation(t *testing.T) {
	cases := []struct {
		name        string
		spec        model.AccountSpec
		want        string
		unsupported bool
	}{
		{
			name: "标准 base_url",
			spec: model.AccountSpec{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash"},
			want: "https://api.deepseek.com/user/balance",
		},
		{
			name: "带 /v1 与尾斜杠",
			spec: model.AccountSpec{BaseURL: "https://api.deepseek.com/v1/", Model: "deepseek-flash"},
			want: "https://api.deepseek.com/user/balance",
		},
		{
			name: "只写了模型名",
			spec: model.AccountSpec{Model: "deepseek-flash"},
			want: "https://api.deepseek.com/user/balance",
		},
		{
			name: "provider 显式 deepseek",
			spec: model.AccountSpec{Provider: "deepseek", BaseURL: "https://api.deepseek.com"},
			want: "https://api.deepseek.com/user/balance",
		},
		{
			name:        "其它供应商",
			spec:        model.AccountSpec{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
			unsupported: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := BalanceEndpoint(testCase.spec)
			if testCase.unsupported {
				if !errors.Is(err, ErrBalanceUnsupported) {
					t.Fatalf("期望 ErrBalanceUnsupported，实际 err=%v endpoint=%q", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("BalanceEndpoint: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("endpoint = %q，期望 %q", got, testCase.want)
			}
		})
	}
}

// TestQueryBalanceParsesDeepSeekShape 用 httptest 钉住真实响应形状的解析：金额保持
// 字符串原样（"110.00" 不能变成 "110"），is_available 要如实带出来。
func TestQueryBalanceParsesDeepSeekShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/user/balance" {
			t.Errorf("请求路径 = %q，期望 /user/balance", request.URL.Path)
		}
		if auth := request.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("Authorization = %q，期望 Bearer sk-test", auth)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"is_available":true,"balance_infos":[` +
			`{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"},` +
			`{"currency":"USD","total_balance":"0.00","granted_balance":"0.00","topped_up_balance":"0.00"}]}`))
	}))
	defer server.Close()

	balance, err := QueryBalance(context.Background(), model.AccountSpec{
		Name: "agent-1", BaseURL: server.URL, Model: "deepseek-flash", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("QueryBalance: %v", err)
	}
	if !balance.Available {
		t.Fatalf("available 应为 true")
	}
	if len(balance.Infos) != 2 {
		t.Fatalf("币种条目数 = %d，期望 2", len(balance.Infos))
	}
	first := balance.Infos[0]
	if first.Currency != "CNY" || first.Total != "110.00" || first.Granted != "10.00" || first.ToppedUp != "100.00" {
		t.Fatalf("读数与原样字符串不符: %#v", first)
	}
}

// TestQueryBalanceReportsStatus 非 200 要把状态码与响应体摘要带出来（"402 Insufficient
// Balance" 正是本仓库真实撞过的那种），且不能泄漏密钥。
func TestQueryBalanceReportsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte("invalid api key sk-test\n"))
	}))
	defer server.Close()

	_, err := QueryBalance(context.Background(), model.AccountSpec{
		Name: "agent-1", BaseURL: server.URL, Model: "deepseek-flash", APIKey: "sk-test",
	})
	if err == nil {
		t.Fatalf("401 应报错")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("错误文案应含状态码: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("错误文案应含响应体摘要: %v", err)
	}
	if strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("错误文案不该带 Authorization: %v", err)
	}
}

// TestQueryBalanceRejectsUnsupported 不支持的供应商要报可展示的原话，且**不发请求**。
func TestQueryBalanceRejectsUnsupported(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called = true
		writer.Write([]byte("{}"))
	}))
	defer server.Close()

	_, err := QueryBalance(context.Background(), model.AccountSpec{
		Name: "agent-1", Provider: "openai", BaseURL: server.URL, Model: "gpt-4o", APIKey: "sk-test",
	})
	if !errors.Is(err, ErrBalanceUnsupported) {
		t.Fatalf("期望 ErrBalanceUnsupported，实际 %v", err)
	}
	if called {
		t.Fatalf("不支持的供应商不该发请求")
	}
}

// TestQueryBalanceEmptyInfos：上游 200 但没有任何币种条目——这是"读不到"，
// 不能当成"余额为 0"报给界面。
func TestQueryBalanceEmptyInfos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"is_available":true,"balance_infos":[]}`))
	}))
	defer server.Close()

	_, err := QueryBalance(context.Background(), model.AccountSpec{
		Name: "agent-1", BaseURL: server.URL, Model: "deepseek-flash", APIKey: "sk-test",
	})
	if err == nil || !strings.Contains(err.Error(), "balance_infos") {
		t.Fatalf("空条目应报错，实际 %v", err)
	}
}

// TestQueryBalanceRequiresAPIKey：没有凭据就不该发请求。
func TestQueryBalanceRequiresAPIKey(t *testing.T) {
	_, err := QueryBalance(context.Background(), model.AccountSpec{
		Name: "agent-1", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash",
	})
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("缺凭据应报错，实际 %v", err)
	}
}
