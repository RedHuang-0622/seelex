package ssh

// probe.go —— 只连不写的探针：连不上也要说清**哪一段坏了、下一步修什么**。
//
// 探针的产品价值不在"能不能连"，而在把"裸错误串"翻成可行动的结论。上游那条
// 诊断链（Server accepts key 之后仍 Permission denied ⇒ 口令/agent 问题，而不是
// key 没登记）就是本文件存在的理由：两种病因的修法完全不同，混为一谈会把人
// 引到错方向（docs/arch/ssh-tool.md §6.2）。

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh/knownhosts"
)

// 探针分类（机器的判据；Hint 才是给人看的）。
const (
	// ClassOK 表示连接与认证都通过。
	ClassOK = "ok"
	// ClassUnreachable 表示 TCP 建连失败（主机不在/DNS/端口不通/防火墙）。
	ClassUnreachable = "unreachable"
	// ClassNotSSH 表示端口通但对方不像 SSH（版本串/握手异常）。
	ClassNotSSH = "not_ssh"
	// ClassAlgorithmMismatch 表示双方没有共同算法。
	ClassAlgorithmMismatch = "algorithm_mismatch"
	// ClassHostUnknown 表示首次连接（known_hosts 无条目）。
	ClassHostUnknown = "host_unknown"
	// ClassKeyMismatch 表示主机指纹与记录不符。
	ClassKeyMismatch = "key_mismatch"
	// ClassHostRevoked 表示主机条目已吊销。
	ClassHostRevoked = "revoked"
	// ClassAuthRejected 表示认证材料被提交但被拒（口令错/agent 未解锁/key 被否）。
	ClassAuthRejected = "auth_rejected"
	// ClassNoAuthMethod 表示我们的认证方式不在服务端允许列表里。
	ClassNoAuthMethod = "no_auth_method"
	// ClassNoCredential 表示本端没有可用认证材料（既没 agent，也没 key/口令）。
	// 它与 auth_rejected 必须分开：一个是我们没提交东西，一个是我们提交了被否。
	ClassNoCredential = "no_credential"
	// ClassTimeout 表示某一段超时（段名见 Segment）。
	ClassTimeout = "timeout"
	// ClassUnknown 表示没归到上面任何一类。
	ClassUnknown = "unknown"
)

// ProbeResult 是一次探针的结论。
type ProbeResult struct {
	OK      bool    `json:"ok"`
	Class   string  `json:"class"`
	Segment Segment `json:"segment,omitempty"`
	// Detail 是脱敏后的原始错误（不含秘密；用于人工核对，不用于判定）。
	Detail string `json:"detail,omitempty"`
	// Hint 是给人读的下一步动作。
	Hint string `json:"hint,omitempty"`
	// Fingerprint 是本次握手观测到的主机公钥指纹（SHA256）。
	Fingerprint string `json:"fingerprint,omitempty"`
	// KnownHostLine 是可直接追加到 known_hosts 的条目（仅在拿到公钥时有值）。
	KnownHostLine string `json:"known_hosts_line,omitempty"`
	// AllowedMethods / TriedMethods / OfferedMethods 是认证段的**结构事实**：
	// 判定"公钥不被接受"还是"公钥被接受但被拒"靠它们，不靠字符串匹配。
	AllowedMethods []string `json:"allowed_methods,omitempty"`
	TriedMethods   []string `json:"tried_methods,omitempty"`
	OfferedMethods []string `json:"offered_methods,omitempty"`
	AuthUsed       string   `json:"auth_used,omitempty"`
}

// Probe 只连不写：建立连接、报告分类，然后立刻关掉。它不写 known_hosts、
// 不执行任何命令。
func Probe(ctx context.Context, spec Spec) ProbeResult {
	client, trace, err := dial(ctx, spec)
	res := ProbeResult{Class: ClassUnknown}
	if trace != nil {
		res.Segment = trace.Segment
		if trace.Verifier != nil {
			if observed := trace.Verifier.observed(); observed.key != nil {
				res.Fingerprint = observed.fingerprint
				res.KnownHostLine = knownhosts.Line([]string{spec.Addr()}, observed.key)
			}
		}
		if trace.Plan != nil {
			res.AllowedMethods = trace.Plan.serverAllowed()
			res.TriedMethods = trace.Plan.triedMethods()
			res.OfferedMethods = trace.Plan.offeredProtos()
		}
	}
	if err == nil {
		res.OK = true
		res.Class = ClassOK
		res.AuthUsed = client.AuthUsed()
		_ = client.Close()
		return res
	}
	res.Detail = err.Error()
	res.Class, res.Hint = classifyDial(trace, err)
	return res
}

// classifyDial 把连接期失败翻成分类 + 可行动提示。
func classifyDial(trace *dialTrace, err error) (class, hint string) {
	// 入参/凭据类的失败先于分段归因：它们连"哪一段坏了"都谈不上。
	var codeErr *CodeError
	if errors.As(err, &codeErr) {
		if codeErr.Code == CodeNoCredential {
			return ClassNoCredential,
				"本端没有可用的认证材料：agent 未运行、未给私钥路径、也未给口令。" +
					"先确认要用的那一种（agent / key / password）再重试。"
		}
	}
	if class, hint, ok := classifyHostKeyError(err); ok {
		return class, hint
	}
	segment := SegBanner
	if trace != nil {
		segment = trace.Segment
	}
	msg := err.Error()

	switch segment {
	case SegConnect:
		return ClassUnreachable,
			"TCP 建连失败：核对主机名/端口/安全组，以及本机到该地址的网络是否可达。"

	case SegBanner:
		if timedOut(trace, err) {
			return ClassTimeout, "握手在 banner 段超时：端口可能不是 SSH，或对端在限流/防护。"
		}
		if strings.Contains(msg, "no common algorithm") {
			return ClassAlgorithmMismatch,
				"双方没有共同的密钥交换/加密算法：报告双方算法集，不建议放宽本端算法集。"
		}
		return ClassNotSSH,
			"端口可达但握手失败：目标可能不是 SSH 服务（例如 HTTP 端口），或中间有设备在改写。"

	case SegAuth:
		return classifyAuth(trace, err)
	}
	return ClassUnknown, "未能归类的失败；请附 Detail 与 Segment 人工核对。"
}

// classifyAuth 是 L8 那条诊断链的固化形态。
//
// 两种病因必须分开：
//   - publickey **在允许列表里且被尝试过** ⇒ 公钥进了认证流程但被否：
//     私钥有口令而无人输入 / agent 未解锁 / key 被服务端拒；
//   - publickey **不在允许列表里** ⇒ 我们的公钥根本没被接受：
//     私钥缺失、权限、格式或未登记。
func classifyAuth(trace *dialTrace, err error) (class, hint string) {
	if timedOut(trace, err) {
		return ClassTimeout, "认证段超时：检查 agent 是否卡住（例如等待口令/确认弹窗）。"
	}

	plan := (*authPlan)(nil)
	if trace != nil {
		plan = trace.Plan
	}
	if plan != nil {
		allowed := plan.serverAllowed()
		offered := plan.offeredProtos()
		tried := plan.triedMethods()

		if len(allowed) > 0 {
			anyOffered := false
			for _, o := range offered {
				if containsString(allowed, o) {
					anyOffered = true
					break
				}
			}
			if !anyOffered {
				return ClassNoAuthMethod,
					"服务端不接受我们提交的任何认证方式（允许：" + strings.Join(allowed, ", ") +
						"；我们提交：" + strings.Join(offered, ", ") + "）：换认证方式或先登记公钥。"
			}
			if containsString(offered, protoPublicKey) && containsString(allowed, protoPublicKey) &&
				containsString(tried, protoPublicKey) {
				return ClassAuthRejected,
					"公钥认证被拒，但**公钥确实被提交过**（publickey 在服务端允许列表且已尝试）：" +
						"常见原因是私钥有口令而无人输入、或 ssh-agent 未解锁。这不是\"key 没登记\"。"
			}
			if containsString(offered, protoPassword) && containsString(allowed, protoPassword) &&
				containsString(tried, protoPassword) {
				return ClassAuthRejected, "密码认证被拒：核对凭据本身，而不是公钥。"
			}
		}
	}

	if strings.Contains(err.Error(), "unable to authenticate") {
		return ClassAuthRejected,
			"认证失败（服务端未提供允许方法列表，无法进一步区分\"key 被拒\"与\"key 未被接受\"）：" +
				"检查凭据引用与 agent 状态。"
	}
	return ClassUnknown, "认证阶段失败且无法归类；请附 Detail 人工核对。"
}

// timedOut 报告这次失败是否属于"某一段超时"。
func timedOut(trace *dialTrace, err error) bool {
	if trace == nil {
		return false
	}
	var dialErr *DialError
	if !errors.As(err, &dialErr) {
		return false
	}
	return dialErr.TimedOut
}
