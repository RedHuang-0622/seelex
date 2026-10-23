package registry

// remote_face.go —— 五个内置工具的**远端执行面**（一份 handler，两条传输）。
//
// 这一层只做三件事，别的不做：
//
//  1. **选面**：`RemoteTransport(ctx)` 给出这次调用的远端面（ok=false = 走本地既有
//     路径，逐字不改）；
//  2. **转形状**：把工具的入参翻成传输请求，把传输输出翻成工具回执；
//  3. **自报执行面**：每份回执都带 `execution_surface`（local|ssh）与 `env`（环境名）。
//
// 刻意不做的事（做不到就说做不到，不在这里补）：
//
//   - **不解析远端路径**：远端面上的 path/workdir 是对端路径，本层不做 ProjectScope
//     解析、也不猜"这个路径对应哪台机器上的哪里"。命令正文里的路径改写属权限模式表
//     （见 pathGateTargets 的注释），传输层与工具层都不碰；
//   - **不判业务成功**：远端退出码非零仍是"跑完了"（ssh 包的三态口径），回执照抄；
//   - **不伪造身份**：远端面拿不到 handle / log_path 时，回执里就没有这两格，也不说
//     "稍后取回"。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RemoteFaceToolNames 是**真的有远端面**的工具名单（唯一一份）。
//
// 它是传输选择器的消费者闸门：只有会读执行面的工具才进这份名单，选择器才对它们做
// "这次走哪儿"的判断。名字写错或漏写都会让"选了 ssh 但 handler 跑本地"重新变成可能，
// 所以名单与 handler 的 `RemoteTransport(ctx)` 分支必须同一个提交里改。
func RemoteFaceToolNames() []string {
	return []string{
		ReadFileToolName, WriteFileToolName, EditFileToolName, BashToolName, bgManageToolName,
	}
}

// executionFaceFields 是回执里"这是哪台机器"的两格（判据只此一处）。
func executionFaceFields(ctx context.Context) map[string]any {
	surface, env := ExecutionFace(ctx)
	return map[string]any{
		"execution_surface": surface,
		"env":               env,
	}
}

// receiptJSON 把回执渲染成字符串（本地与远端共用同一套渲染）。
func receiptJSON(tool string, fields map[string]any) (string, error) {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("%s: 渲染回执失败: %w", tool, err)
	}
	return string(encoded), nil
}

// withExecutionFace 给一份"已经有形状"的回执补上执行面两格（本地回执的加法式改动）。
//
// 为什么用加法而不是换形状：本地回执是既有契约（会话历史、GUI 渲染、用例都依赖它的
// 键），加两个键不动老键；而远端回执本来就是新的，直接带上。
func withExecutionFace(ctx context.Context, fields map[string]any) map[string]any {
	for key, value := range executionFaceFields(ctx) {
		fields[key] = value
	}
	return fields
}

// ── read_file 远端面 ────────────────────────────────────────────────

// readFileRemote 是 read_file 的远端回执。
//
// 形状说明（**本地与远端有意不同**，理由必须一起读）：本地 read_file 的输出就是文件
// 正文本身（历史契约，模型按正文用），远端面的输出是一份 JSON 信封——因为远端读必须
// 自报"你读的是哪台机器上的文件"，而正文里加一行标记会污染内容与行号。于是：
// 本地 = 正文（逐字不改），远端 = {content, path, ..., execution_surface, env}。
func readFileRemote(ctx context.Context, transport Transport, input scopedReadFileInput) (string, error) {
	out, err := transport.Read(ctx, TransportReadRequest{
		Path: input.Path, StartLine: input.StartLine, EndLine: input.EndLine,
	})
	if err != nil {
		return "", err
	}
	fields := withExecutionFace(ctx, map[string]any{
		"status":      "ok",
		"path":        input.Path,
		"start_line":  input.StartLine,
		"end_line":    input.EndLine,
		"total_lines": out.TotalLines,
		"bytes":       out.Bytes,
		"content":     out.Content,
	})
	if out.Truncated {
		fields["truncated"] = true
		fields["note"] = out.Note
	}
	return receiptJSON("read_file", fields)
}

// ── write_file / edit_file 远端面 ───────────────────────────────────

func writeFileRemote(ctx context.Context, transport Transport, input scopedWriteFileInput) (string, error) {
	out, err := transport.Write(ctx, TransportWriteRequest{Path: input.Path, Content: input.Content})
	if err != nil {
		return "", err
	}
	return receiptJSON("write_file", withExecutionFace(ctx, map[string]any{
		"status": "ok", "path": input.Path, "size": out.Bytes,
	}))
}

func editFileRemote(ctx context.Context, transport Transport, input scopedEditFileInput) (string, error) {
	out, err := transport.Edit(ctx, TransportEditRequest{
		Path: input.Path, OldString: input.OldString, NewString: input.NewString,
	})
	if err != nil {
		return "", err
	}
	return receiptJSON("edit_file", withExecutionFace(ctx, map[string]any{
		"status": "ok", "path": input.Path, "replacements": out.Replacements,
	}))
}

// ── bash 远端面 ────────────────────────────────────────────────────

// bashRemote 是 bash 的远端回执。
//
// 四格必须分开、不许互相顶替（本工作项的硬缺口 ④）：
//   - status           远端失败分类（completed / channel_timeout / ...）；
//   - truncated        输出在**传输上限**处被截断（文件内容不全，不是命令失败）；
//   - still_running    本端预算用尽但**远端仍在跑**（不是失败、也不是成功）；
//   - handle/log_path  仍在跑时的取回凭据（没有它们就不说"稍后取回"）。
func bashRemote(ctx context.Context, transport Transport, command, stdinData, workdir string, budget time.Duration) (string, error) {
	out, err := transport.Exec(ctx, TransportRequest{
		Command: command,
		Stdin:   stdinData,
		// workdir 原样交给远端：这是"命令在哪跑"，不是"本机哪个根下的目录"。
		Workdir: workdir,
		Timeout: budget,
	})
	if err != nil {
		return "", err
	}
	fields := withExecutionFace(ctx, map[string]any{
		"stdout":        strings.TrimSpace(out.Stdout),
		"stderr":        strings.TrimSpace(out.Stderr),
		"exit_code":     out.ExitCode,
		"status":        out.Status,
		"truncated":     out.Truncated,
		"still_running": out.StillRunning,
		"duration_ms":   out.DurationMS,
	})
	if out.Handle != "" {
		fields["handle"] = out.Handle
	}
	if out.LogPath != "" {
		fields["log_path"] = out.LogPath
	}
	if out.Note != "" {
		fields["note"] = out.Note
	}
	if out.StillRunning && out.Handle == "" {
		fields["note"] = "远端仍在运行，但本端没有拿到句柄；要取回请重跑并加长 timeout。"
	}
	return receiptJSON("bash", fields)
}

// bashLocalReceipt 是 bash 的**本地**回执：历史三键（stdout / stderr / exit_code）不动，
// 补上执行面与环境名两格。
//
// 为什么本地也要标：模型在一次会话里可能先后落在两侧（项目刚配好连接、探针刚通），
// 回执不说"这是本机还是远端"，它下一次就会按上一轮的经验读路径。
func bashLocalReceipt(ctx context.Context, result scopedBashResult) (string, error) {
	return receiptJSON("bash", withExecutionFace(ctx, map[string]any{
		"stdout": result.Stdout, "stderr": result.Stderr, "exit_code": result.ExitCode,
	}))
}
