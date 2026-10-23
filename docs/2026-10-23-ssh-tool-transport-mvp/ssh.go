package main

// ssh.go —— 远端执行面：把"五件事"落到 backend/tool/ssh 的连接基座上。
//
// 复用口径：连接、四段超时、指纹校验、三态收口**全部来自基座**，本文件只负责
// 把工具的意图翻译成远端命令，以及把基座的失败翻译成工具错误码。任何"再实现一遍
// 超时/再判一遍指纹"的写法都算违规。

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// SSHTransport 是远端执行面。
type SSHTransport struct {
	client *ssh.Client
	spec   ssh.Spec
}

// NewSSHTransport 用一条已建立的连接建远端执行面。
func NewSSHTransport(client *ssh.Client, spec ssh.Spec) *SSHTransport {
	return &SSHTransport{client: client, spec: spec}
}

func (t *SSHTransport) Name() string { return "ssh" }

func (t *SSHTransport) Describe() string { return "ssh:" + t.spec.Addr() }

// remotePathExpr 把远端路径编成"命令串里只出现 ASCII"的表达式。
//
// 为什么必须这么做：路径本身可能含中文/空格/引号，而基座拒绝非 ASCII 内联命令
// （它会静默损坏，见 docs/arch/ssh-tool.md §9）。base64 是唯一既保真又对 shell
// 安全的承载方式，因此路径与内容**都**走 base64，命令串永远纯 ASCII。
func remotePathExpr(path string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(path))
	return `"$(printf %s '` + encoded + `' | base64 -d)"`
}

func (t *SSHTransport) Read(ctx context.Context, path string, limit int) (Content, error) {
	res, err := t.client.Run(ctx, ssh.Request{
		Inline:         "cat -- " + remotePathExpr(path),
		MaxOutputBytes: limit,
	})
	if err != nil {
		return Content{}, mapSSHError(err)
	}
	if res.Status == ssh.StatusChannelTimeout {
		return Content{}, toolErr(CodeSSHChannelTimeout,
			"换 detached 形态或缩小读取范围", "读取 %s 时通道超时（远端可能仍在读）", path)
	}
	if res.ExitCode != 0 {
		if isRemoteNotFound(res.Stderr) {
			return Content{}, toolErr(CodeSSHPathNotFound, "先列远端目录确认路径",
				"远端文件不存在：%s", path)
		}
		return Content{}, toolErr(CodeSSHRemoteCmdFailed, "读 stderr 判断远端原因",
			"远端 cat 失败（exit %d）：%s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	out := capContent(path, []byte(res.Stdout), 0)
	out.Truncated = res.Truncated
	out.Transport = t.Name()
	return out, nil
}

// Write 走「base64 载荷内联 + base64 -d 落盘」：内容里出现引号/换行/中文都不会
// 破坏命令解析，因为 base64 字母表对 shell 安全。
//
// MVP 已知简化：载荷内联在命令串里（受 base 的 Inline 单行 ASCII 约束）。
// 生产形态应把载荷走 stdin，需要基座 Request 增加"命令 + stdin 数据"的同时表达，
// 见 docs/arch/ssh-tool-transport.md §4.5 的缺口记录。
func (t *SSHTransport) Write(ctx context.Context, path string, data []byte) (WriteResult, error) {
	payload := base64.StdEncoding.EncodeToString(data)
	res, err := t.client.Run(ctx, ssh.Request{
		Inline: "printf %s '" + payload + "' | base64 -d > " + remotePathExpr(path),
	})
	if err != nil {
		return WriteResult{}, mapSSHError(err)
	}
	if res.Status == ssh.StatusChannelTimeout {
		return WriteResult{}, toolErr(CodeSSHChannelTimeout,
			"确认远端是否已落盘再重试", "写入 %s 时通道超时", path)
	}
	if res.ExitCode != 0 {
		return WriteResult{}, toolErr(CodeSSHRemoteCmdFailed, "确认远端 base64 可用与目录可写",
			"远端写入失败（exit %d）：%s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return WriteResult{Path: path, Bytes: len(data), Transport: t.Name()}, nil
}

// Edit 是 read → 唯一串替换 → write。**判据函数与本地共用**（applyEdit）。
func (t *SSHTransport) Edit(ctx context.Context, path, oldText, newText string, replaceAll bool) (EditResult, error) {
	content, err := t.Read(ctx, path, 0)
	if err != nil {
		return EditResult{}, err
	}
	updated, replaced, err := applyEdit(content.Text, oldText, newText, replaceAll)
	if err != nil {
		return EditResult{}, err
	}
	if _, err := t.Write(ctx, path, []byte(updated)); err != nil {
		return EditResult{}, err
	}
	return EditResult{Path: path, Replaced: replaced, Transport: t.Name()}, nil
}

func (t *SSHTransport) Exec(ctx context.Context, script string) (ExecResult, error) {
	res, err := t.client.Run(ctx, ssh.Request{Inline: script})
	if err != nil {
		return ExecResult{}, mapSSHError(err)
	}
	out := ExecResult{
		Status:       string(res.Status),
		ExitCode:     res.ExitCode,
		Stdout:       res.Stdout,
		Stderr:       res.Stderr,
		StillRunning: res.StillRunning,
		Transport:    t.Name(),
	}
	// 关键：channel_timeout **不是失败**，不返回 error（与基座同判据）。
	return out, nil
}

// Manage 演示远端作业句柄的命名空间：必须带 host 前缀，否则两台机器的同 pid 会撞。
func (t *SSHTransport) Manage(_ context.Context, op, handle string) (ManageResult, error) {
	prefix := t.spec.Addr() + ":"
	switch op {
	case "list":
		return ManageResult{Op: op, Handles: []string{prefix + "4242"}, Transport: t.Name()}, nil
	case "start":
		return ManageResult{Op: op, Handles: []string{prefix + handle}, Transport: t.Name()}, nil
	default:
		return ManageResult{}, toolErr(CodeBadArg, "op 只支持 list/start", "未知 op：%s", op)
	}
}

// ── 连接与错误码映射 ──────────────────────────────────────────────────

// DialSSH 先探针后连接：**连通性测试与连接用同一份判据**，不另写"测端口通不通"。
//
// 先探的好处：探针会把失败归到具体分段（connect/banner/auth）并给出可行动提示，
// 直接用 Dial 的裸错误则只能得到一个字符串。
func DialSSH(ctx context.Context, spec ssh.Spec) (*SSHTransport, Decision, error) {
	probe := ssh.Probe(ctx, spec)
	if !probe.OK {
		return nil, Decision{
				Transport: "local",
				Reason:    "probe_failed",
				Degraded:  true,
				Detail:    probe.Class,
			},
			&ToolError{
				Code:     codeFromProbeClass(probe.Class),
				Message:  "远端不可达或不可认证（class=" + probe.Class + ", segment=" + string(probe.Segment) + "）",
				NextStep: probe.Hint,
			}
	}
	client, err := ssh.Dial(ctx, spec)
	if err != nil {
		return nil, Decision{Transport: "local", Reason: "dial_failed", Degraded: true},
			&ToolError{Code: CodeSSHConnectFailed, Message: err.Error(),
				NextStep: "核对连接配置后重试探测"}
	}
	return NewSSHTransport(client, spec), Decision{Transport: "ssh", Reason: "probe_ok"}, nil
}

// codeFromProbeClass 是**唯一一份** Class → 工具错误码的映射表。
func codeFromProbeClass(class string) string {
	switch class {
	case ssh.ClassUnreachable:
		return CodeSSHConnectFailed
	case ssh.ClassHostUnknown:
		return CodeSSHHostUntrusted
	case ssh.ClassKeyMismatch:
		return CodeSSHHostKeyMismatch
	case ssh.ClassHostRevoked:
		return CodeSSHHostKeyMismatch
	case ssh.ClassAuthRejected, ssh.ClassNoAuthMethod:
		return CodeSSHAuthFailed
	case ssh.ClassNoCredential:
		return CodeSSHNoCredential
	case ssh.ClassTimeout:
		return CodeSSHTimeout
	case ssh.ClassNotSSH, ssh.ClassAlgorithmMismatch:
		return CodeSSHConnectFailed
	default:
		return CodeSSHConnectFailed
	}
}

// mapSSHError 把基座的分段失败翻成工具错误码。
func mapSSHError(err error) error {
	var dialErr *ssh.DialError
	if asDialError(err, &dialErr) {
		switch dialErr.Segment {
		case ssh.SegConnect:
			return toolErr(CodeSSHConnectFailed, "核对主机/端口/网络", "建连失败：%v", dialErr.Err)
		case ssh.SegBanner:
			if dialErr.TimedOut {
				return toolErr(CodeSSHTimeout, "端口可能不是 SSH 或对端在限流", "握手超时")
			}
			return toolErr(CodeSSHConnectFailed, "目标可能不是 SSH 服务", "握手失败：%v", dialErr.Err)
		case ssh.SegAuth:
			if dialErr.TimedOut {
				return toolErr(CodeSSHTimeout, "检查 agent 是否卡在口令/确认弹窗", "认证超时")
			}
			return toolErr(CodeSSHAuthFailed, "换凭据，不要改代码", "认证失败：%v", dialErr.Err)
		}
	}
	return toolErr(CodeSSHConnectFailed, "查看连接配置", "SSH 失败：%v", err)
}

// asDialError 是 errors.As 的薄封装。
func asDialError(err error, target **ssh.DialError) bool {
	for err != nil {
		if de, ok := err.(*ssh.DialError); ok {
			*target = de
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// isRemoteNotFound 识别远端"文件不存在"（用于把远端语义翻成本端错误码）。
func isRemoteNotFound(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "no such file") || strings.Contains(lower, "cannot open") ||
		strings.Contains(lower, "not found")
}
