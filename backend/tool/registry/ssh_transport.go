package registry

// ssh_transport.go —— Transport 的 ssh 一支（接口适配层）。
//
// 为什么适配放在这里而不是 backend/tool/ssh：那个包的依赖方向是**单向**的
// （它不认识工具名、权限主体与会话，见该包 README 的"依赖方向"）。所以：
// 底座在 ssh 包（*ssh.Backend，一次一连接），形状在这里被贴上工具层需要的接口
// 与固定错误码。这也是"错误码只有一处映射"的落点——分类 → 码的那张表在
// transport_selector.go 的 transportCodeForProbeClass，本文件只负责把底座错误
// **归一**成那张表的输入（一个探针分类）。

import (
	"context"
	"errors"

	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// sshTransport 是 registry.Transport 的 ssh 实现。
type sshTransport struct {
	backend *seelessh.Backend
	// endpoint 只用于错误文案与留痕（不含凭据）。
	endpoint string
}

// NewSSHTransport 把 ssh 包的执行面包成工具层的 Transport（backend 为 nil 时返回 nil）。
func NewSSHTransport(backend *seelessh.Backend, endpoint string) Transport {
	if backend == nil {
		return nil
	}
	return &sshTransport{backend: backend, endpoint: endpoint}
}

// Kind 返回 ssh。
func (t *sshTransport) Kind() TransportKind { return TransportSSH }

// Exec 执行一次命令。
//
// 连接期失败 → 带固定码的 TransportError（Err 里裹着探针分类，供上层填留痕与
// 文案）；执行期失败（远端命令没跑起来）按 Result.Status 如实回报，不当连接失败。
func (t *sshTransport) Exec(ctx context.Context, req TransportRequest) (TransportOutput, error) {
	result, err := t.backend.Run(ctx, req.Command, req.Workdir)
	if err != nil {
		return TransportOutput{
			Stdout: result.Stdout, Stderr: result.Stderr,
			ExitCode: result.ExitCode, Status: string(result.Status),
		}, t.connectError(err)
	}
	return TransportOutput{
		Stdout: result.Stdout, Stderr: result.Stderr,
		ExitCode: result.ExitCode, Status: string(result.Status),
		Truncated: result.Truncated, StillRunning: result.StillRunning,
		AuthUsed: result.AuthUsed, DurationMS: result.DurationMS,
	}, nil
}

// connectError 把底座错误归一成固定码 + 探针分类。
func (t *sshTransport) connectError(err error) error {
	class := ssClassifyFailure(err)
	code := transportCodeForProbeClass(class)
	return &TransportError{
		Code: code,
		Msg:  "SSH 端点 " + trimmedOr(t.endpoint, "(未指定)") + " 执行失败（分类 " + trimmedOr(class, "unknown") + "）",
		Err:  &ProbeClassError{Class: class, Err: err},
	}
}

// ssClassifyFailure 把 backend/tool/ssh 的三层错误（CodeError / DialError /
// 指纹错误值）翻成探针分类。判据只在这里，选择器与执行面共用同一套分类词表。
func ssClassifyFailure(err error) string {
	if err == nil {
		return seelessh.ClassOK
	}
	// 首次连接 / 指纹不符：known_hosts 是唯一判据（不做文本匹配）。
	var keyErr *seelessh.CodeError
	if errors.As(err, &keyErr) && keyErr != nil {
		switch keyErr.Code {
		case seelessh.CodeNoCredential:
			return seelessh.ClassNoCredential
		case seelessh.CodeKnownHostsMissing:
			return seelessh.ClassHostUnknown
		}
	}
	if errors.Is(err, seelessh.ErrKnownHostsMissing) {
		return seelessh.ClassHostUnknown
	}
	var dialErr *seelessh.DialError
	if errors.As(err, &dialErr) && dialErr != nil {
		if dialErr.TimedOut || errors.Is(dialErr.Err, context.DeadlineExceeded) {
			return seelessh.ClassTimeout
		}
		switch dialErr.Segment {
		case seelessh.SegConnect:
			return seelessh.ClassUnreachable
		case seelessh.SegBanner:
			return seelessh.ClassNotSSH
		case seelessh.SegAuth:
			return seelessh.ClassAuthRejected
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return seelessh.ClassTimeout
	}
	return seelessh.ClassUnknown
}
