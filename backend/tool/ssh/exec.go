package ssh

// exec.go —— 命令执行：内容只走文件 / stdin，输出有界，超时三分。
//
// 三条纪律写在代码里而不是注释里（docs/arch/ssh-tool.md §9）：
//   - 一次性命令串只收单行 ASCII（非 ASCII 会被多层代码页静默损坏，且损坏后
//     仍能被解析——这是最贵的一类 bug）；
//   - 输出上限到了就标 truncated，不静默截断也不无限往上下文里灌；
//   - channel 超时返回 channel_timeout + still_running，绝不返回失败。

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	xssh "golang.org/x/crypto/ssh"
)

// Request 是一次命令执行请求：三种命令来源互斥。
type Request struct {
	// Inline 是内联命令，**只允许单行 ASCII**（唯一允许的一次性参数形态）。
	Inline string
	// CommandFile 是本地脚本文件路径（首选）：内容按 UTF-8 原样经 stdin 送入。
	CommandFile string
	// Stdin 是直接经 stdin 送入的脚本文本（任意 UTF-8）。
	Stdin string

	// Shell 是远端解释器，空 = bash。
	Shell string
	// Workdir 是执行前的 cd 目标，空 = 登录目录。
	Workdir string
	// Env 是可选环境变量；服务端未开 AcceptEnv 时会被拒（不致命）。
	Env map[string]string
	// MaxOutputBytes 覆盖 Spec 里的上限。
	MaxOutputBytes int
}

// DetachRequest 是脱钩执行请求：内容同样只走 base64 载荷，避免引号与编码。
type DetachRequest struct {
	Inline      string
	CommandFile string
	Stdin       string
	Shell       string
	Workdir     string
	// LogPath 是远端日志路径；空 = /tmp/seelex-<时间戳>.log。
	LogPath string
}

// Run 执行一次命令并等待结束（或等到 channel 段超时）。
func (c *Client) Run(ctx context.Context, req Request) (Result, error) {
	started := time.Now()
	res := Result{Status: StatusFailed, AuthUsed: c.authUsed, ExitCode: -1}

	resolved, err := c.resolve(req)
	if err != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		return res, err
	}

	sess, err := c.client.NewSession()
	if err != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		res.Class, res.Hint = ClassUnknown, "建立会话通道失败（连接可能已被远端关闭）。"
		return res, fmt.Errorf("ssh: new session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	limit := req.MaxOutputBytes
	if limit <= 0 {
		limit = c.spec.maxOutput()
	}
	stdout := newCapBuffer(limit)
	stderr := newCapBuffer(limit)
	sess.Stdout = stdout
	sess.Stderr = stderr
	if resolved.stdin != nil {
		sess.Stdin = resolved.stdin
	}
	for k, v := range req.Env {
		// AcceptEnv 由远端 sshd 决定；失败不致命（Setenv 只是"请求"）。
		_ = sess.Setenv(k, v)
	}

	if startErr := sess.Start(resolved.command); startErr != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		res.Class, res.Hint = ClassUnknown, "远端拒绝启动命令（可能是 shell 不存在或通道已被关闭）。"
		return res, fmt.Errorf("ssh: start command: %w", startErr)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	timeouts := c.spec.Timeouts.withDefaults()

	select {
	case waitErr := <-done:
		res.DurationMS = time.Since(started).Milliseconds()
		fillOutput(&res, stdout, stderr, resolved.note)
		c.applyWait(&res, waitErr)
		return res, nil
	case <-time.After(timeouts.Channel):
		// 本端不再等待。**这不等于远端失败**：先尝试礼貌终止，但不宣称"那边挂了"。
		_ = sess.Signal(xssh.SIGTERM)
		_ = sess.Close()
		res.DurationMS = time.Since(started).Milliseconds()
		res.Status = StatusChannelTimeout
		res.Segment = SegChannel
		res.StillRunning = true
		res.Note = fmt.Sprintf("本端在 %s 后停止等待；远端命令可能仍在运行。要拿回结果请用 detached 形态（日志路径可取回）。", timeouts.Channel)
		fillOutput(&res, stdout, stderr, resolved.note)
		return res, nil
	case <-ctx.Done():
		_ = sess.Signal(xssh.SIGTERM)
		_ = sess.Close()
		res.DurationMS = time.Since(started).Milliseconds()
		res.Status = StatusChannelTimeout
		res.Segment = SegChannel
		res.StillRunning = true
		res.Note = "调用方取消（ctx）；远端命令可能仍在运行。"
		fillOutput(&res, stdout, stderr, resolved.note)
		return res, nil
	}
}

// RunDetached 让命令在远端脱钩运行并立刻返回日志路径。
//
// 形态固定为 `setsid <shell> <script> </dev/null >log 2>&1 &`：
//   - `</dev/null` 必须有，否则后台进程占住通道（nohup 不脱钩，实测仍会 PipeTimeout）；
//   - `>log` 必须有，否则"没有输出"会被误读成"挂死"。
//
// 脚本内容一律 base64 传输：base64 字母表对 shell 安全，绕开三层引号与代码页。
func (c *Client) RunDetached(ctx context.Context, req DetachRequest) (Result, error) {
	started := time.Now()
	res := Result{Status: StatusFailed, AuthUsed: c.authUsed, ExitCode: -1}

	script, err := c.scriptFor(req)
	if err != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		return res, err
	}

	shell := req.Shell
	if shell == "" {
		shell = "bash"
	}
	logPath := req.LogPath
	if logPath == "" {
		logPath = fmt.Sprintf("/tmp/seelex-%d.log", time.Now().UnixNano())
	}
	scriptPath := strings.TrimSuffix(logPath, ".log") + ".sh"

	payload := base64.StdEncoding.EncodeToString([]byte(script))
	var b strings.Builder
	fmt.Fprintf(&b, "printf %%s '%s' | base64 -d > %s && chmod 600 %s", payload, shellQuote(scriptPath), shellQuote(scriptPath))
	if req.Workdir != "" {
		fmt.Fprintf(&b, " && cd %s", shellQuote(req.Workdir))
	}
	fmt.Fprintf(&b, " && setsid %s %s </dev/null >%s 2>&1 & echo $!",
		shell, shellQuote(scriptPath), shellQuote(logPath))

	sess, err := c.client.NewSession()
	if err != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		return res, fmt.Errorf("ssh: new session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	pidOut := newCapBuffer(1024)
	sess.Stdout = pidOut
	sess.Stderr = newCapBuffer(8192)
	if startErr := sess.Start("/bin/sh -c " + shellQuote(b.String())); startErr != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		res.Class, res.Hint = ClassUnknown, "远端拒绝启动脱钩命令。"
		return res, fmt.Errorf("ssh: start detached command: %w", startErr)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	// 脱钩命令应当立刻返回；给一个短上限，避免"以为脱钩了其实挂着"。
	wait := 15 * time.Second
	if t := c.spec.Timeouts.withDefaults().Channel; t < wait {
		wait = t
	}
	select {
	case <-done:
	case <-time.After(wait):
		_ = sess.Close()
		res.DurationMS = time.Since(started).Milliseconds()
		res.Status = StatusChannelTimeout
		res.Segment = SegChannel
		res.StillRunning = true
		res.Note = "脱钩命令没有在预期时间内返回；远端可能仍在运行，日志路径可用于核对。"
		return res, nil
	case <-ctx.Done():
		_ = sess.Close()
		res.DurationMS = time.Since(started).Milliseconds()
		res.Status = StatusChannelTimeout
		res.StillRunning = true
		res.Note = "调用方取消（ctx）。"
		return res, nil
	}

	pid := strings.TrimSpace(pidOut.String())
	res.DurationMS = time.Since(started).Milliseconds()
	res.Status = StatusDetached
	res.StillRunning = true
	res.LogPath = logPath
	res.Handle = detachedHandle(c.spec.ID, pid)
	res.Stdout = pid
	// 事实提醒：输出滞后不等于挂死（远端程序自身缓冲，例如 python 需 -u）。
	res.Note = "已脱钩；日志可能因远端程序自身缓冲而滞后，输出滞迟不等于挂死。增量读取请按 log_path 取。"
	return res, nil
}

// resolvedCommand 是一次解析后的可执行形态。
type resolvedCommand struct {
	command string
	// stdin 非 nil 表示脚本内容经 stdin 送入（`<shell> -s`），这是保真的主通道。
	stdin io.Reader
	note  string
}

// resolve 把三种命令来源归一成一种可执行形态，并执行"单行 ASCII"校验。
func (c *Client) resolve(req Request) (resolvedCommand, error) {
	sources := 0
	for _, s := range []string{req.Inline, req.CommandFile, req.Stdin} {
		if s != "" {
			sources++
		}
	}
	if sources == 0 {
		return resolvedCommand{}, codeErr(CodeNoCommand, "one of inline, command_file or stdin is required")
	}
	if sources > 1 {
		return resolvedCommand{}, codeErr(CodeCommandConflict,
			"inline / command_file / stdin are mutually exclusive; pass exactly one")
	}

	shell := req.Shell
	if shell == "" {
		shell = "bash"
	}
	cd := ""
	if req.Workdir != "" {
		cd = "cd " + shellQuote(req.Workdir) + " && "
	}

	if req.Inline != "" {
		if err := validateInline(req.Inline); err != nil {
			return resolvedCommand{}, err
		}
		return resolvedCommand{command: cd + req.Inline}, nil
	}
	script, err := c.scriptFor(DetachRequest{CommandFile: req.CommandFile, Stdin: req.Stdin})
	if err != nil {
		return resolvedCommand{}, err
	}
	return resolvedCommand{
		command: cd + shell + " -s",
		stdin:   strings.NewReader(script),
	}, nil
}

// scriptFor 取出脚本文本（文件优先，其次 stdin 字面量），并做 UTF-8 校验。
func (c *Client) scriptFor(req DetachRequest) (string, error) {
	var raw []byte
	switch {
	case req.CommandFile != "":
		data, err := os.ReadFile(req.CommandFile)
		if err != nil {
			return "", fmt.Errorf("ssh: read command file %q: %w", req.CommandFile, err)
		}
		raw = data
	case req.Stdin != "":
		raw = []byte(req.Stdin)
	default:
		// 脱钩路径允许只有 inline：那是已经通过 validateInline 的 ASCII 单行命令。
		if req.Inline == "" {
			return "", codeErr(CodeNoCommand, "one of inline, command_file or stdin is required")
		}
		return req.Inline, nil
	}
	// BOM 会让解释器把首行 shebang 读成乱码，先剥掉再校验。
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) {
		return "", codeErr(CodeInvalidScriptUTF8,
			"script content is not valid UTF-8; write the script as UTF-8 and retry")
	}
	return string(raw), nil
}

// validateInline 是"一次性参数"的白名单：单行 + 纯 ASCII。
//
// 为什么这么严：非 ASCII 经过宿主代码页、shell、远端 locale 的多次转换后可能
// **静默**损坏——脚本仍能解析，只是中文字面量变了。这种失败在断言里才暴露，
// 且现场极难归因。所以宁可在入口拒收，把失败提前到可解释的那一步。
func validateInline(cmd string) error {
	if strings.ContainsAny(cmd, "\r\n") {
		return codeErr(CodeInlineMultiline,
			"inline command must be a single line; put multi-line content in command_file or stdin")
	}
	for _, r := range cmd {
		if r > 0x7F {
			return codeErr(CodeNonASCIIInline,
				"inline command contains non-ASCII (U+%04X); put the script in command_file or stdin "+
					"(multi-layer encoding will silently corrupt non-ASCII arguments)", r)
		}
	}
	return nil
}

// applyWait 把 Wait 的结果翻成 Result（exit code 与三态语义）。
func (c *Client) applyWait(res *Result, waitErr error) {
	if waitErr == nil {
		res.Status = StatusCompleted
		res.ExitCode = 0
		return
	}
	var exitErr *xssh.ExitError
	if errors.As(waitErr, &exitErr) {
		// 远端**跑完了**，只是退出码非零：这是 completed，不是 failed。
		res.Status = StatusCompleted
		res.ExitCode = exitErr.ExitStatus()
		return
	}
	var missing *xssh.ExitMissingError
	if errors.As(waitErr, &missing) {
		res.Status = StatusCompleted
		res.ExitCode = -1
		res.Note = "远端未回传退出状态（会话被关闭）；输出可能不完整。"
		return
	}
	res.Status = StatusFailed
	res.Class = ClassUnknown
	res.Hint = "通道层面失败（连接中断或远端拒绝）；命令是否执行过不确定，请回读核对。"
	res.Stderr += "\n" + waitErr.Error()
}

// fillOutput 落统一输出字段：有界、编码有损可判、换行不做归一。
func fillOutput(res *Result, stdout, stderr *capBuffer, note string) {
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.Truncated = stdout.Truncated() || stderr.Truncated()
	res.LossyUTF8 = !utf8.ValidString(res.Stdout) || !utf8.ValidString(res.Stderr)
	if note != "" {
		res.Note = note
	}
	if res.LossyUTF8 {
		res.Note = strings.TrimSpace(res.Note + " 输出含非法 UTF-8 字节（未静默替换，请核对编码）。")
	}
}

// capBuffer 是有上限的收集器：超过上限仍继续消费（不阻断通道），但只保留前 limit 字节。
//
// 必须带锁：session 的拷贝 goroutine 在写它，而超时分支要在 Close 后读它。
type capBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	total int64
	over  bool
}

func newCapBuffer(limit int) *capBuffer { return &capBuffer{limit: limit} }

func (b *capBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	if b.limit > 0 {
		if room := b.limit - b.buf.Len(); room > 0 {
			if len(p) <= room {
				b.buf.Write(p)
			} else {
				b.buf.Write(p[:room])
			}
		}
		if b.total > int64(b.limit) {
			b.over = true
		}
	} else {
		b.buf.Write(p)
	}
	return len(p), nil
}

func (b *capBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *capBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.over
}

// shellQuote 把任意字符串安全地变成 POSIX shell 的单引号字面量。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// detachedHandle 生成脱钩作业句柄：稳定前缀（host id）+ 远端 pid + 时间戳。
func detachedHandle(hostID, pid string) string {
	if hostID == "" {
		hostID = "ssh"
	}
	if pid == "" {
		pid = "0"
	}
	return fmt.Sprintf("%s:%s:%d", hostID, pid, time.Now().UnixNano())
}
