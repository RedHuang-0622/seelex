// balance.go — 账号余额查询（设置 → 账户 → 查看余额）。
//
// 为什么放在账号域：余额是**账号凭据**的属性，只有这里持有 AccountSpec（含
// api_key）与 provider 身份。装配层与界面层都只该看到"某账号的余额读数"，
// 不该自己去拼 URL、也不该碰密钥。
//
// 与流式超时纪律的关系（见 README「流式超时纪律」）：那一条说的是 **LLM 流**不许
// 设整请求 wall-clock 上限（健康但缓慢的长流会被按总时长掐断）。余额是一问一答的
// 短查询，恰恰需要**有界**：这里的 client 带 20s 总超时，宁可在界面上报"查不到"，
// 也不让一次探测把界面挂在无限等待里。
//
// 支持的 provider 是**显式白名单**：目前只有 DeepSeek 有公开的余额接口
// （`GET {base}/user/balance`）。其它 OpenAI 兼容供应商没有等价端点，此时返回
// ErrBalanceUnsupported（可展示的原话），而不是拿一个猜的 URL 去打人家的服务。
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/internal/model"
)

// ErrBalanceUnsupported 表示该账号的供应商没有可查询的余额接口（可展示原话）。
var ErrBalanceUnsupported = errors.New("seelex: 该供应商没有可查询的余额接口（目前支持 DeepSeek 的 /user/balance）")

// balanceTimeout 是一次余额探测的**整请求**上限：一问一答的短查询，有界才对。
const balanceTimeout = 20 * time.Second

// balanceBodyLimit 是非 200 响应体在错误文案里最多回显多少字节（够看清原因，
// 又不至于把上游整页 HTML 灌进界面）。
const balanceBodyLimit = 200

// BalanceInfo 是一种币种的余额读数。
//
// 金额保持 provider 给的**字符串原样**：货币与精度由服务端定（"110.00"），
// 解析成 float 再格式化回去只会把末尾的 0 丢掉，让读数与账单对不上。
type BalanceInfo struct {
	Currency string
	// Total 是总余额；Granted 是赠送余额；ToppedUp 是充值余额。
	Total    string
	Granted  string
	ToppedUp string
}

// Balance 是一个账号此刻的余额读数（多币种按 provider 给的顺序）。
type Balance struct {
	// Available 是 provider 自报的"当前是否可用"（额度耗尽时为 false，
	// 这正是 402 之前唯一能提前看到的信号）。
	Available bool
	Infos     []BalanceInfo
}

// isEmpty 报告这份读数是不是"什么都没读到"（没币种条目 = 上游给了空表）。
func (balance Balance) isEmpty() bool { return len(balance.Infos) == 0 }

// BalanceEndpoint 返回该账号的余额查询端点；供应商没有该能力时返回
// ErrBalanceUnsupported。
func BalanceEndpoint(spec model.AccountSpec) (string, error) {
	if !isDeepSeekAccount(spec) {
		return "", ErrBalanceUnsupported
	}
	base := strings.TrimRight(strings.TrimSpace(spec.BaseURL), "/")
	if base == "" {
		// 账号档只写了 model: deepseek-*（没有 base_url）时按官方端点兜底。
		base = "https://api.deepseek.com"
	}
	// DeepSeek 的余额端点挂在域名根上，不在 /v1 之下：base_url 写成
	// https://api.deepseek.com/v1（对话同样能用）时要去掉那段版本前缀。
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		base = base[:len(base)-len("/v1")]
	}
	return base + "/user/balance", nil
}

// isDeepSeekAccount 判据与账号档的口径一致（命中任一即算）：base_url 指向
// deepseek 域名、model 名以 deepseek 开头、或 provider 显式写了 deepseek。
func isDeepSeekAccount(spec model.AccountSpec) bool {
	if strings.EqualFold(strings.TrimSpace(spec.Provider), "deepseek") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(spec.Model)), "deepseek") {
		return true
	}
	baseURL := strings.TrimSpace(spec.BaseURL)
	if baseURL == "" {
		return false
	}
	if parsed, err := url.Parse(baseURL); err == nil {
		return strings.Contains(strings.ToLower(parsed.Hostname()), "deepseek")
	}
	return strings.Contains(strings.ToLower(baseURL), "deepseek")
}

// QueryBalance 查询一个账号的余额。
//
// 错误口径：不支持 → ErrBalanceUnsupported；没凭据/网络/非 200 → 带上下文的错误
// （状态码 + 截断的响应体），**绝不**把 Authorization 或 api_key 写进错误文案。
func QueryBalance(ctx context.Context, spec model.AccountSpec) (Balance, error) {
	endpoint, err := BalanceEndpoint(spec)
	if err != nil {
		return Balance{}, err
	}
	apiKey := strings.TrimSpace(spec.APIKey)
	if apiKey == "" {
		return Balance{}, fmt.Errorf("seelex: account %q has no api_key", spec.Name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Balance{}, fmt.Errorf("seelex: build balance request for %q: %w", spec.Name, err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := (&http.Client{Timeout: balanceTimeout}).Do(request)
	if err != nil {
		return Balance{}, fmt.Errorf("seelex: query balance for %q: %w", spec.Name, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Balance{}, fmt.Errorf("seelex: read balance response for %q: %w", spec.Name, err)
	}
	if response.StatusCode != http.StatusOK {
		return Balance{}, fmt.Errorf("seelex: query balance for %q: %s: %s",
			spec.Name, response.Status, excerpt(body))
	}

	var payload deepSeekBalancePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Balance{}, fmt.Errorf("seelex: parse balance response for %q: %w", spec.Name, err)
	}
	balance := Balance{Available: payload.IsAvailable}
	for _, info := range payload.BalanceInfos {
		balance.Infos = append(balance.Infos, BalanceInfo{
			Currency: info.Currency,
			Total:    info.TotalBalance,
			Granted:  info.GrantedBalance,
			ToppedUp: info.ToppedUpBalance,
		})
	}
	if balance.isEmpty() {
		return Balance{}, fmt.Errorf("seelex: balance response for %q carries no balance_infos", spec.Name)
	}
	return balance, nil
}

// deepSeekBalancePayload 是 DeepSeek `GET /user/balance` 的响应形状。
type deepSeekBalancePayload struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

// excerpt 截断响应体用于错误文案（去换行，限制长度）。
func excerpt(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > balanceBodyLimit {
		return text[:balanceBodyLimit] + "…"
	}
	if text == "" {
		return "(空响应)"
	}
	return text
}
