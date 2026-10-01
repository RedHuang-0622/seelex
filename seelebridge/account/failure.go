package account

import (
	"context"
	"errors"
	"strings"
)

// FailureKind 是「这次失败是账号本身的问题，还是上游/网络的问题」的语义分类。
//
// 为什么账号层必须有这一层：账号池只负责并发租约与负载选择，**不做重试**
// （Seele `accountpool/README.md`：「模块不负责…重试…这些策略由调用方或上层模块
// 组合」）。于是「第一志愿没额度 → 第二志愿」这条回退只能由上层实现，而它的前提是
// 先**认出**这是账号问题：
//
//   - 网络抖动 / 上游 5xx：原地重试同一账号可能就好了，换账号没有意义；
//   - 额度耗尽（402 / insufficient balance …）/ 鉴权被拒（401 / invalid api key）：
//     同一个账号重试多少次都是同一个结果，**必须换账号**。
//
// 判据是错误文本里的 provider 语义：seelebridge 的错误链路
// （`internal/stream` 与 api 层）保留了上游的状态码与措辞
// （如 `seelebridge: stream with account "goalplan-1": ChatClient stream: HTTP 402 …`），
// 因此字符串匹配在这里是可定位、可测试的（与 application/core 的
// `classifyProviderFailure` 同一口径）。
type FailureKind string

const (
	// FailureNone 表示这不是账号资格问题（网络、上游故障、ctx 取消、业务错误…）：
	// 调用方应当原样上抛，换账号不会改变结果。
	FailureNone FailureKind = ""
	// FailureQuota 表示这个账号当前没有可用额度（计费/配额）。
	FailureQuota FailureKind = "quota_exhausted"
	// FailureAuth 表示这个账号的凭据被拒（换一个账号才有意义）。
	FailureAuth FailureKind = "auth_rejected"
)

// quotaMarkers / authMarkers 是**大小写不敏感**的语义标记。只收「明确指向账号资格」
// 的措辞：上游把 403 也用于内容策略拒绝，因此不把裸 `403` 当作鉴权失败——那会让
// 一次内容拒答把账号池里的账号逐个撞一遍。
var (
	quotaMarkers = []string{
		"http 402", "payment required",
		"insufficient_quota", "insufficient quota",
		"exceeded your current quota", "quota exceeded", "quota exhausted",
		"insufficient balance", "insufficient_balance", "insufficient_user_quota",
		"exceeded your credit", "no credit", "out of credit", "credit balance",
		"billing",
		"余额不足", "额度不足", "额度已用尽", "无额度", "欠费",
	}
	authMarkers = []string{
		"http 401", "invalid api key", "invalid_api_key", "incorrect api key",
		"unauthorized", "authentication failed", "鉴权失败", "密钥无效",
	}
)

// ClassifyFailure 把一次账号调用错误分类为「账号资格问题」或「其它」。
// nil 错误返回 FailureNone。
func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return FailureNone
	}
	message := strings.ToLower(err.Error())
	// ctx 取消/超时是调用方的问题，不是账号资格问题：先短路，避免错误文本里
	// 恰好带上某个标记时把取消也当成额度耗尽。
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return FailureNone
	}
	for _, marker := range quotaMarkers {
		if strings.Contains(message, marker) {
			return FailureQuota
		}
	}
	for _, marker := range authMarkers {
		if strings.Contains(message, marker) {
			return FailureAuth
		}
	}
	return FailureNone
}

// FailureAdvice 给出一条可读的换号理由（日志/错误文本用）。
func FailureAdvice(kind FailureKind) string {
	switch kind {
	case FailureQuota:
		return "已无可用额度"
	case FailureAuth:
		return "凭据被拒"
	default:
		return "不可用"
	}
}
