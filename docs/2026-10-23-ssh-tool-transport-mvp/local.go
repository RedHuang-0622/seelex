package main

// local.go —— 本地执行面（等价于"现行为"）：os 文件系统 + 本机进程。

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalTransport 把操作落到本机某个根目录。
type LocalTransport struct {
	root    string
	handles []string
}

// NewLocalTransport 建本地执行面。
func NewLocalTransport(root string) *LocalTransport {
	return &LocalTransport{root: root}
}

func (t *LocalTransport) Name() string { return "local" }

func (t *LocalTransport) Describe() string { return "local:" + t.root }

// resolve 把相对路径解析到根内，并做 containment 校验（越界即拒）。
func (t *LocalTransport) resolve(path string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return "", toolErr(CodeBadArg, "给出具体文件路径", "path 为空")
	}
	if filepath.IsAbs(cleaned) {
		return "", toolErr(CodeBadArg, "使用相对项目根的路径", "不收绝对路径：%s", cleaned)
	}
	full := filepath.Join(t.root, cleaned)
	rel, err := filepath.Rel(t.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", toolErr(CodeBadArg, "路径必须在项目根内", "越界路径：%s", cleaned)
	}
	return full, nil
}

func (t *LocalTransport) Read(_ context.Context, path string, limit int) (Content, error) {
	full, err := t.resolve(path)
	if err != nil {
		return Content{}, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Content{}, toolErr(CodeLocalIO, "先列目录确认文件是否存在", "本机文件不存在：%s", path)
		}
		return Content{}, toolErr(CodeLocalIO, "检查文件权限", "读取失败：%v", err)
	}
	return capContent(path, raw, limit), nil
}

func (t *LocalTransport) Write(_ context.Context, path string, data []byte) (WriteResult, error) {
	full, err := t.resolve(path)
	if err != nil {
		return WriteResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return WriteResult{}, toolErr(CodeLocalIO, "检查父目录权限", "建目录失败：%v", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return WriteResult{}, toolErr(CodeLocalIO, "检查文件权限", "写入失败：%v", err)
	}
	return WriteResult{Path: path, Bytes: len(data), Transport: t.Name()}, nil
}

func (t *LocalTransport) Edit(ctx context.Context, path, oldText, newText string, replaceAll bool) (EditResult, error) {
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

func (t *LocalTransport) Exec(ctx context.Context, script string) (ExecResult, error) {
	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/c"
	}
	cmd := exec.CommandContext(ctx, shell, flag, script)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// 远端/本机跑完了、退出码非零 ⇒ **不是接线失败**。
			exitCode = exitErr.ExitCode()
		} else {
			return ExecResult{}, toolErr(CodeLocalIO, "检查命令是否存在", "启动失败：%v", err)
		}
	}
	return ExecResult{
		Status:    "completed",
		ExitCode:  exitCode,
		Stdout:    out.String(),
		Stderr:    errOut.String(),
		Transport: t.Name(),
	}, nil
}

func (t *LocalTransport) Manage(_ context.Context, op, handle string) (ManageResult, error) {
	switch op {
	case "list":
		handles := append([]string(nil), t.handles...)
		return ManageResult{Op: op, Handles: handles, Transport: t.Name()}, nil
	case "start":
		t.handles = append(t.handles, handle)
		return ManageResult{Op: op, Handles: []string{handle}, Transport: t.Name()}, nil
	default:
		return ManageResult{}, toolErr(CodeBadArg, "op 只支持 list/start", "未知 op：%s", op)
	}
}

// ── 共享判据（两个执行面**必须**用同一份）──────────────────────────────

// capContent 截断到 limit 字节并标 truncated（limit<=0 表示不截）。
func capContent(path string, raw []byte, limit int) Content {
	out := Content{Path: path, Bytes: len(raw)}
	data := raw
	if limit > 0 && len(raw) > limit {
		data = raw[:limit]
		out.Truncated = true
	}
	out.Text = string(data)
	return out
}

// applyEdit 是**唯一一份**编辑判据：oldText 必须唯一出现（除非 replaceAll）。
//
// 这份函数被本地与远端两个传输共用——两处各写一份是这类工具最常见的漂移源
// （"本地要求唯一、远端允许全替换"），所以它只能存在一处。
func applyEdit(text, oldText, newText string, replaceAll bool) (string, int, error) {
	if oldText == "" {
		return "", 0, toolErr(CodeBadArg, "给出要替换的原文片段", "old_text 为空")
	}
	count := strings.Count(text, oldText)
	if count == 0 {
		return "", 0, toolErr(CodeBadArg, "检查原文是否与文件内容一致", "原文片段未在文件中出现")
	}
	if count > 1 && !replaceAll {
		return "", 0, toolErr(CodeBadArg,
			"扩大 old_text 使其唯一，或显式 replace_all",
			"原文片段出现 %d 次，不唯一", count)
	}
	if replaceAll {
		return strings.ReplaceAll(text, oldText, newText), count, nil
	}
	return strings.Replace(text, oldText, newText, 1), 1, nil
}

// 说明：远端路径与内容一律经 base64 承载（见 ssh.go 的 remotePathExpr），
// 因此这里不需要 shell 引号拼接——少一处拼接就少一处注入面。
