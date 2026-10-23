package ssh

// remote.go —— 远端文件/作业面**共享的字节与路径承载**（五份远端实现只用这一份）。
//
// 三条纪律（与 exec.go 的"命令来源纪律"同源，只是对象换了）：
//
//  1. **远端路径不过 shell 词法**：路径可能是中文、带空格、带引号、带 `$`。把它
//     拼进命令行会让远端 shell 重新解释一遍——轻则找不到文件，重则执行了路径里的
//     `$(...)`。因此路径一律 base64 承载，由远端 `base64 -d` 解码进 shell 变量
//     （字母表只有 A-Za-z0-9+/=，对任何 shell 都安全）。
//  2. **内容只走内容通道**：文件内容、脚本体、stdin 数据都经 stdin / 独立连接送出，
//     **不拼进命令正文**——拼进去就要过引号与代码页，任何一处漏转义都是静默改数据。
//  3. **判据留在 Go 这边**：远端脚本只做机械动作（解码、读、写、报状态）；行窗口、
//     替换次数、截断判定都在本端算，一份判据只有一处实现。

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// 远端辅助脚本的**保留退出码**。0-89 留给被测命令本身，90 起是"辅助脚本自己坏了"，
// 于是"命令跑完退出 1"与"路径解不开"在任何一层都分得开（不靠读 stderr 文本）。
const (
	remoteExitDecodeFailed   = 90
	remoteExitNotRegularFile = 91
	remoteExitFileIOFailed   = 92
)

// 远端面的会话级错误码（与 ssh.go 的入参类码同层，工具层据此给下一步）。
const (
	// CodeRemoteToolMissing 表示远端缺 `base64`（最小可用环境都没有，属环境问题）。
	CodeRemoteToolMissing = "SSH_REMOTE_TOOL_MISSING"
	// CodeRemotePathInvalid 表示路径解码失败/形态不合法（本端给的载荷坏了）。
	CodeRemotePathInvalid = "SSH_REMOTE_PATH_INVALID"
	// CodeRemoteFileIO 表示远端文件操作失败（不存在/权限/写盘），stderr 带原文。
	CodeRemoteFileIO = "SSH_REMOTE_FILE_IO"
	// CodeRemoteEditNoMatch 表示要替换的 old_string 在远端文件里不存在。
	CodeRemoteEditNoMatch = "SSH_EDIT_OLD_STRING_NOT_FOUND"
)

// encodePayload 把任意字符串折成 base64（字节透明：中文、CRLF、二进制都一样）。
func encodePayload(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// decodeVar 生成"把 base64 载荷解码进 shell 变量"的表达式（路径与临时目录都走它）。
func decodeVar(variable, value string) string {
	return variable + "=$(printf %s '" + encodePayload(value) + "' | base64 -d) || exit " +
		strconv.Itoa(remoteExitDecodeFailed)
}

// stdinOrNil 是 runCommand 的入参适配：空数据 = 不给 stdin（不是给一个空管道）。
func stdinOrNil(data string) io.Reader {
	if data == "" {
		return nil
	}
	return strings.NewReader(data)
}

// unsupportedRemoteTool 报告本端脚本要用的工具在远端不存在（stderr 里是 sh 的原文）。
func unsupportedRemoteTool(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "base64: not found") ||
		strings.Contains(lower, "base64: command not found") ||
		strings.Contains(lower, "base64: no such file")
}

// remoteScriptError 把辅助脚本的非零退出翻成带固定码的错误。
//
// 判据用**退出码**而不是 stderr 文本：文本随远端 locale 与 shell 变，退出码不会。
func remoteScriptError(op string, res Result) error {
	switch res.ExitCode {
	case remoteExitDecodeFailed:
		return codeErr(CodeRemotePathInvalid, "%s: 远端路径解码失败（本端载荷损坏或远端 base64 不可用）", op)
	case remoteExitNotRegularFile:
		return codeErr(CodeRemoteFileIO, "%s: 远端目标不是普通文件: %s", op, strings.TrimSpace(res.Stderr))
	case remoteExitFileIOFailed:
		return codeErr(CodeRemoteFileIO, "%s: 远端文件操作失败: %s", op, strings.TrimSpace(res.Stderr))
	}
	if unsupportedRemoteTool(res.Stderr) {
		return codeErr(CodeRemoteToolMissing, "%s: 远端环境缺少 base64（最小可用环境不满足）: %s", op, strings.TrimSpace(res.Stderr))
	}
	return codeErr(CodeRemoteFileIO, "%s: 远端操作失败（退出码 %d）: %s", op, res.ExitCode, strings.TrimSpace(res.Stderr))
}

// decodeRemoteBase64 解一份远端回来的 base64 流。
//
// truncated=true 时**不假装解码完整**：只解满 4 字节对齐的前缀（base64 的解码单元），
// 并把"内容被截断"这件事交给调用方写进回执——半行数据当成整文件比报错危险得多。
func decodeRemoteBase64(encoded string, truncated bool) ([]byte, bool) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', ' ', '\t':
			return -1
		}
		return r
	}, encoded)
	if truncated {
		clean = clean[:len(clean)-(len(clean)%4)]
		decoded, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
			if err != nil {
				return nil, true
			}
		}
		return decoded, true
	}
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
		if err != nil {
			return nil, false
		}
	}
	return decoded, false
}

// runInline 跑一段**本包生成的**脚本（单行 ASCII），可选把 data 作为它的 stdin。
//
// 为什么生成物可以走内联而用户内容不行：生成物只有固定字面量 + base64（字母表安全），
// 没有一处随用户输入变化；用户内容一律走内容通道（见文件头的纪律 2）。
func (b *Backend) runInline(ctx context.Context, script, data string) (Result, error) {
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return Result{Status: StatusFailed, ExitCode: -1}, err
	}
	defer func() { _ = client.Close() }()
	return client.runCommand(ctx, script, stdinOrNil(data), nil, 0, "")
}

// scriptDir 造一个远端作业/临时目录（ASCII、可预测、带时间戳与主机 ID）。
//
// 为什么不落在 home：登录目录可能是只读挂载；`${TMPDIR:-/tmp}` 是 POSIX 的临时目录
// 约定，且这里的目录名是**本端生成的常量形状**（不含用户输入），可以直接拼。
func scriptDir(hostID, kind string, now time.Time) string {
	id := sanitizeHostID(hostID)
	return fmt.Sprintf("${TMPDIR:-/tmp}/seelex-%s-%s-%d", id, kind, now.UnixNano())
}

// sanitizeHostID 把主机 ID 收成"可以出现在路径里"的形态（空 = "ssh"）。
func sanitizeHostID(hostID string) string {
	trimmed := strings.TrimSpace(hostID)
	if trimmed == "" {
		return "ssh"
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "ssh"
	}
	return b.String()
}
