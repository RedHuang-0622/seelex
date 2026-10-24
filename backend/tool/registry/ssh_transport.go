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
	"strings"

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

// Exec 执行一次命令（命令正文与 stdin 数据都走内容通道）。
//
// 走 ssh 包的 RunJob（起受管作业 + 等到结束或预算用尽）而不是 Run：Run 在预算用尽时
// 只能说"可能还在跑"，而调用方需要的是**可再指认的凭据**（handle + log_path），否则
// "仍在运行"这句话没有下一步。
func (t *sshTransport) Exec(ctx context.Context, req TransportRequest) (TransportOutput, error) {
	result, err := t.backend.RunJob(ctx, req.Command, req.Workdir, req.Timeout)
	if err != nil {
		return TransportOutput{
			Stdout: result.Stdout, Stderr: result.Stderr,
			ExitCode: result.ExitCode, Status: string(result.Status),
			StillRunning: result.StillRunning, AuthUsed: result.AuthUsed,
			DurationMS: result.DurationMS, Handle: result.Handle, LogPath: result.LogPath,
		}, t.faceError(err)
	}
	return TransportOutput{
		Stdout: result.Stdout, Stderr: result.Stderr,
		ExitCode: result.ExitCode, Status: string(result.Status),
		Truncated: result.Truncated, StillRunning: result.StillRunning,
		AuthUsed: result.AuthUsed, DurationMS: result.DurationMS,
		Handle: result.Handle, LogPath: result.LogPath, Note: result.Note,
	}, nil
}

// Start 派发一条远端后台作业并立即返回 `ssh:` 句柄。
func (t *sshTransport) Start(ctx context.Context, req TransportRequest) (TransportManageOutput, error) {
	started, err := t.backend.StartJob(ctx, req.Command, req.Workdir)
	if err != nil {
		return TransportManageOutput{}, t.faceError(err)
	}
	return TransportManageOutput{
		Handle: started.Handle, State: seelessh.JobRunning, StillRunning: true,
		LogPath: started.OutPath,
		Note:    "远端后台作业已受理；用 bg_manage(op=observe|fetch|kill) 管理该 ssh: 句柄。",
	}, nil
}

// Read 读一个远端文件并按行窗口切好（窗口判据与本地面同一份：windowLines）。
func (t *sshTransport) Read(ctx context.Context, req TransportReadRequest) (TransportReadOutput, error) {
	content, err := t.backend.ReadFile(ctx, req.Path)
	if err != nil {
		return TransportReadOutput{}, t.faceError(err)
	}
	if content.Truncated {
		// 半份内容上切行会给出"看起来正常"的残缺结果：整次读拒掉，让调用方知道
		// 要么换手段（远端面暂不支持服务端切行）。
		return TransportReadOutput{
				Bytes: content.Bytes, Truncated: true,
				Note: "远端文件超过单次读取上限；内容不完整，已拒绝按行切分。",
			}, transportErr(CodeSSHUnreachable,
				"read_file: 远端文件超过读取上限（%d 字节已读），拒绝在残缺内容上切行", content.Bytes)
	}
	text, total, err := windowLines(string(content.Data), req.StartLine, req.EndLine)
	if err != nil {
		return TransportReadOutput{}, err
	}
	return TransportReadOutput{Content: text, TotalLines: total, Bytes: content.Bytes}, nil
}

// Write 覆盖写一个远端文件（内容走内容通道）。
func (t *sshTransport) Write(ctx context.Context, req TransportWriteRequest) (TransportWriteOutput, error) {
	written, err := t.backend.WriteFile(ctx, req.Path, req.Content)
	if err != nil {
		return TransportWriteOutput{}, t.faceError(err)
	}
	return TransportWriteOutput{Bytes: written}, nil
}

// Edit 在远端文件里做"全部出现处替换"。
func (t *sshTransport) Edit(ctx context.Context, req TransportEditRequest) (TransportEditOutput, error) {
	count, err := t.backend.EditFile(ctx, req.Path, req.OldString, req.NewString)
	if err != nil {
		return TransportEditOutput{}, t.faceError(err)
	}
	return TransportEditOutput{Replacements: count}, nil
}

// Manage 管理远端作业（observe / fetch / kill / done）。
//
// 四个 op 的远端语义：
//   - observe：读一次状态（running / exited(exit) / gone），**不碰输出**；
//   - fetch：读状态 + 取回当前 out/err（重读当前日志，不是增量游标）；
//   - kill：发 SIGTERM 给整组并**再读一次状态**（"发了信号"≠"死了"，不虚构）；
//   - done：远端没有账本可销（句柄是自描述的目录），只如实回执"本端不再引用"。
func (t *sshTransport) Manage(ctx context.Context, req TransportManageRequest) (TransportManageOutput, error) {
	op := strings.ToLower(strings.TrimSpace(req.Op))
	if op == "" {
		return TransportManageOutput{}, transportErr(CodeSSHUnreachable, "bg_manage: op 不能为空")
	}
	dir, err := seelessh.ParseJobHandle(req.Handle)
	if err != nil {
		return TransportManageOutput{}, t.faceError(err)
	}
	out := TransportManageOutput{LogPath: dir + "/out"}
	switch op {
	case "observe":
		state, statusErr := t.backend.JobStatus(ctx, req.Handle)
		if statusErr != nil {
			return TransportManageOutput{}, t.faceError(statusErr)
		}
		return fillRemoteState(out, state), nil
	case "fetch":
		state, statusErr := t.backend.JobStatus(ctx, req.Handle)
		if statusErr != nil {
			return TransportManageOutput{}, t.faceError(statusErr)
		}
		out = fillRemoteState(out, state)
		stdout, stderr, truncated, outErr := t.backend.JobOutput(ctx, req.Handle, req.MaxBytes)
		if outErr != nil {
			return TransportManageOutput{}, t.faceError(outErr)
		}
		out.Stdout, out.Stderr, out.Truncated = stdout, stderr, truncated
		out.Note = "远端面取回的是**当前**落盘日志（不是增量）；仍在跑时内容还会增长。"
		return out, nil
	case "kill":
		signal, killErr := t.backend.JobKill(ctx, req.Handle)
		if killErr != nil {
			return TransportManageOutput{}, t.faceError(killErr)
		}
		state, statusErr := t.backend.JobStatus(ctx, req.Handle)
		if statusErr != nil {
			return TransportManageOutput{}, t.faceError(statusErr)
		}
		out = fillRemoteState(out, state)
		out.Note = "已向远端作业发终止信号（" + trimmedOr(signal, "无回话") + "）；" +
			"最终状态以本次读数为准，信号不保证立刻生效。"
		return out, nil
	case "done":
		state, statusErr := t.backend.JobStatus(ctx, req.Handle)
		if statusErr != nil {
			return TransportManageOutput{}, t.faceError(statusErr)
		}
		out = fillRemoteState(out, state)
		out.Note = "远端没有作业账本可销（句柄自描述作业目录）；done 只表示本端不再引用它。" +
			"要停进程请用 kill。"
		return out, nil
	}
	return TransportManageOutput{}, transportErr(CodeSSHUnreachable,
		"bg_manage: 远端面不支持 op=%q（可用 observe | fetch | kill | done）", req.Op)
}

// fillRemoteState 把远端状态读数填进回执（状态词与"还在跑"的判定只此一处）。
func fillRemoteState(out TransportManageOutput, state seelessh.JobState) TransportManageOutput {
	out.State = state.State
	out.ExitCode = state.ExitCode
	out.StillRunning = state.State == seelessh.JobRunning
	return out
}

// faceError 把 ssh 底座错误归一成工具层错误。
//
// 两条路必须分开：**操作类**失败（路径解码、文件 IO、替换不命中）带 ssh 包的会话级码，
// 原样上抛（它的码就是给机器判定的）；**传输类**失败（连不上 / 认证 / 指纹 / 超时）
// 归一成固定的 SSH_* 码。把前者报成后者会让人去查网络，而病灶在文件上。
func (t *sshTransport) faceError(err error) error {
	if err == nil {
		return nil
	}
	var codeErr *seelessh.CodeError
	if errors.As(err, &codeErr) && codeErr != nil {
		return err
	}
	return t.connectError(err)
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
