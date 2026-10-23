// Package main 是「内置工具双传输策略」的**孤立可行性 MVP**。
//
// 它不属于产品链路：不注册工具、不改派发、不碰权限。它只回答一个问题——
// 用一条策略接口 + 一个责任链中间件，能否让同一份 handler 骨架同时跑在
// 本机与远端，并按项目状态自动选择。
//
// 设计依据：docs/arch/ssh-tool-transport.md
package main

// transport.go —— 策略接口、结果形状、错误码。

import (
	"context"
	"fmt"
)

// Transport 是"五件事"的执行面抽象。两个实现：LocalTransport / SSHTransport。
//
// 关键设计：**方法数 = 能力数，不等于工具数**。工具的参数 schema 与返回形状
// 与传输无关，切换传输不改变模型看到的东西。
type Transport interface {
	// Name 返回 "local" 或 "ssh"（进轨迹，用于事后归因）。
	Name() string
	// Describe 返回人类可读的执行面描述（本地 = 根目录；远端 = host:port）。
	Describe() string

	Read(ctx context.Context, path string, limit int) (Content, error)
	Write(ctx context.Context, path string, data []byte) (WriteResult, error)
	Edit(ctx context.Context, path, oldText, newText string, replaceAll bool) (EditResult, error)
	Exec(ctx context.Context, script string) (ExecResult, error)
	Manage(ctx context.Context, op string, handle string) (ManageResult, error)
}

// Content 是读文件的结果。
type Content struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Transport string `json:"transport"`
}

// WriteResult 是写文件的结果。
type WriteResult struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Transport string `json:"transport"`
}

// EditResult 是编辑的结果（唯一串替换）。
type EditResult struct {
	Path      string `json:"path"`
	Replaced  int    `json:"replaced"`
	Transport string `json:"transport"`
}

// ExecResult 是命令执行结果。三态与连接基座一致：
// StatusCompleted / StatusDetached / StatusChannelTimeout，超时**不是失败**。
type ExecResult struct {
	Status       string `json:"status"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	StillRunning bool   `json:"still_running"`
	Transport    string `json:"transport"`
}

// ManageResult 是后台作业管理的结果。
type ManageResult struct {
	Op        string   `json:"op"`
	Handles   []string `json:"handles"`
	Transport string   `json:"transport"`
	Note      string   `json:"note,omitempty"`
}

// 工具错误码。SSH 面专用码与本地参数错严格分开：模型据此决定"换招还是改参数"。
const (
	// CodeBadArg 是本地参数错误（路径为空、替换串不存在等）。
	CodeBadArg = "TOOL_BAD_ARGUMENT"
	// CodeLocalIO 是本机 IO 错误。
	CodeLocalIO = "TOOL_LOCAL_IO"

	// CodeSSHConnectFailed 连接段失败（DNS/端口/防火墙）。
	CodeSSHConnectFailed = "SSH_CONNECT_FAILED"
	// CodeSSHHostUntrusted 首次连接（known_hosts 无条目）。
	CodeSSHHostUntrusted = "SSH_HOST_UNTRUSTED"
	// CodeSSHHostKeyMismatch 主机指纹不符。
	CodeSSHHostKeyMismatch = "SSH_HOST_KEY_MISMATCH"
	// CodeSSHAuthFailed 认证被拒。
	CodeSSHAuthFailed = "SSH_AUTH_FAILED"
	// CodeSSHNoCredential 本端没有可用认证材料。
	CodeSSHNoCredential = "SSH_NO_CREDENTIAL"
	// CodeSSHTimeout 某一段超时（banner/auth）。
	CodeSSHTimeout = "SSH_TIMEOUT"
	// CodeSSHChannelTimeout 通道段超时（不是失败，仍远端在跑）。
	CodeSSHChannelTimeout = "SSH_CHANNEL_TIMEOUT"
	// CodeSSHPathNotFound 远端路径不存在。
	CodeSSHPathNotFound = "SSH_PATH_NOT_FOUND"
	// CodeSSHRemoteCmdFailed 远端命令非零退出（业务结论，不是接线错误）。
	CodeSSHRemoteCmdFailed = "SSH_REMOTE_CMD_FAILED"
	// CodeSSHUnsupported 远端缺少所需命令（如 coreutils 的 base64 -d）。
	CodeSSHUnsupported = "SSH_REMOTE_UNSUPPORTED"
)

// ToolError 是带错误码的工具失败。
type ToolError struct {
	Code     string
	Message  string
	NextStep string
}

func (e *ToolError) Error() string {
	if e.NextStep == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Message + "（下一步：" + e.NextStep + "）"
}

func toolErr(code, nextStep, format string, args ...any) *ToolError {
	return &ToolError{Code: code, Message: fmt.Sprintf(format, args...), NextStep: nextStep}
}

// codeForError 把错误归一成工具错误码。SSH 面的错误必须带码，未知即 CodeLocalIO。
func codeForError(err error) string {
	if err == nil {
		return ""
	}
	var te *ToolError
	if asToolError(err, &te) {
		return te.Code
	}
	return CodeLocalIO
}

// asToolError 是 errors.As 的薄封装（避免在此文件引入 errors 只为一次断言）。
func asToolError(err error, target **ToolError) bool {
	for err != nil {
		if te, ok := err.(*ToolError); ok {
			*target = te
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
