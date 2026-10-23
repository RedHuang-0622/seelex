package ssh

// job.go —— 远端**受管作业面**：起一条作业、看状态、取输出、终止。
//
// 为什么要"作业"这一层而不是直接用 Run：一次同步调用一旦在本端预算处收手，远端命令
// 可能仍在跑（exec.go 的 channel_timeout 语义）。若不给调用方一个**可再指认的凭据**，
// 那句"仍在运行"就只能让人干等——既不能取回输出，也不能终止。作业面把这件事落成
// 远端的一组文件（作业目录），句柄因此**自描述**：不需要本端保存任何登记表
// （工具调用之间的进程状态不可依赖：每次调用都是新连接，见 transport.go）。
//
// 作业目录的形态（全部落在 `${TMPDIR:-/tmp}/seelex-<host>-job-<nanos>/`）：
//
//	cmd.sh   用户命令正文（经内容通道写入，不经 shell 词法）
//	run.sh   监管脚本（本端生成）：写 pid → cd → 跑 cmd.sh → 落退出码
//	pid      监管进程的 pid（= 新会话/进程组的组长，终止它等于终止整棵树）
//	out/err  标准输出/错误（**落盘**：本端随时可以回头取，不依赖通道还开着）
//	exit     退出码（有它 = 作业结束）
//
// 为什么用 setsid：监管进程必须是新会话的组长。否则通道一关，sshd 给整个会话发
// SIGHUP，监管进程连同"落退出码"那一步一起消失，于是"跑完了"永远显示成"还在跑"。

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// 远端作业状态词表（回执与控制流共用一处）。
const (
	JobRunning = "running"
	JobExited  = "exited"
	JobGone    = "gone"
)

// JobPollInterval 是等待作业结束时的状态轮询间隔（一条连接上开新会话，成本很低）。
const JobPollInterval = 120 * time.Millisecond

// jobHandlePrefix 是远端作业句柄的命名空间前缀。
//
// 它与 `bt-` / `bs-` / `bteam-` **不同族**（那几个是 `dto.JobNamespace` 的字面量，
// 由 contract/dto 判定）；`ssh:` 段的语法由本文件独占，于是"把本机作业句柄交给远端面"
// 与反向都在**语法上**不可能命中，只会得到一句可照做的错误。
const jobHandlePrefix = "ssh:"

// JobStarted 是一次远端作业的受理回执。
type JobStarted struct {
	// Dir 是**远端展开后**的作业目录（不是本端那份带 ${TMPDIR} 的字面量）。
	Dir    string
	Handle string
	PID    string
	// OutPath / ErrPath 是日志落点（回执要能让人直接去看）。
	OutPath string
	ErrPath string
}

// JobState 是一次远端作业状态读数。
type JobState struct {
	State    string
	ExitCode int
	PID      string
}

// startMarker 是起作业时从远端回读目录的标记行（本端据此拿到展开后的路径）。
const startMarker = "SEELEX_JOB_DIR="

// ── Backend 面：一次调用 = 一条连接（连接内多条会话）────────────────────

// StartJob 起一条远端受管作业并立刻返回受理回执（不等待命令结束）。
func (b *Backend) StartJob(ctx context.Context, command, workdir string) (JobStarted, error) {
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return JobStarted{}, err
	}
	defer func() { _ = client.Close() }()
	return client.startJob(ctx, command, workdir)
}

// JobStatus 读一次远端作业状态（running / exited / gone）。
func (b *Backend) JobStatus(ctx context.Context, handle string) (JobState, error) {
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return JobState{}, err
	}
	defer func() { _ = client.Close() }()
	return client.jobStatus(ctx, handle)
}

// JobOutput 取回作业当前的 stdout / stderr（各自有上限；超限标 truncated）。
func (b *Backend) JobOutput(ctx context.Context, handle string, maxBytes int) (string, string, bool, error) {
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = client.Close() }()
	return client.jobOutput(ctx, handle, maxBytes)
}

// JobKill 终止远端作业，返回"发了什么"的回执（**不**宣称进程已死，见 jobKill）。
func (b *Backend) JobKill(ctx context.Context, handle string) (string, error) {
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()
	return client.jobKill(ctx, handle)
}

// RunJob 起一条作业并等到结束或预算用尽：两种结局在 Result 里如实区分。
//
// 这是远端"跑一条命令"的**唯一入口**（bash 的 ssh 一支走它）。为什么不直接用 Run：
// 用 Run 时预算一到就只剩"可能还在跑"这一句；而这里无论哪种结局，调用方都拿到句柄与
// 日志落点（还在跑 ⇒ still_running + handle + log_path；跑完了 ⇒ 输出 + 退出码）。
func (b *Backend) RunJob(ctx context.Context, command, workdir string, budget time.Duration) (Result, error) {
	startedAt := time.Now()
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return Result{Status: StatusFailed, ExitCode: -1}, err
	}
	defer func() { _ = client.Close() }()

	job, err := client.startJob(ctx, command, workdir)
	if err != nil {
		return Result{Status: StatusFailed, ExitCode: -1}, err
	}
	res := Result{Status: StatusFailed, ExitCode: -1, AuthUsed: client.authUsed,
		Handle: job.Handle, LogPath: job.OutPath}
	if budget <= 0 {
		budget = client.spec.Timeouts.withDefaults().Channel
	}
	deadline := startedAt.Add(budget)

	for {
		state, statusErr := client.jobStatus(ctx, job.Handle)
		if statusErr != nil {
			// 状态读不到：连接坏了就如实说，不猜作业的结局。
			res.DurationMS = time.Since(startedAt).Milliseconds()
			res.Class, res.Hint = ClassUnknown, "远端作业状态读不到（连接中断？）；命令是否结束不确定，请按 log_path 回读。"
			return res, statusErr
		}
		res.Handle, res.LogPath = job.Handle, job.OutPath
		switch state.State {
		case JobExited:
			stdout, stderr, truncated, outErr := client.jobOutput(ctx, job.Handle, 0)
			res.DurationMS = time.Since(startedAt).Milliseconds()
			res.Status = StatusCompleted
			res.ExitCode = state.ExitCode
			res.Stdout, res.Stderr, res.Truncated = stdout, stderr, truncated
			res.Note = "输出取自远端作业目录（已落盘的 out/err）。"
			if outErr != nil {
				res.Note = "作业已结束，但输出读取失败：" + outErr.Error()
				res.Class, res.Hint = ClassUnknown, "作业跑完了，输出取不回来；请按 log_path 直接核对该文件的权限。"
			}
			return res, nil
		case JobGone:
			res.DurationMS = time.Since(startedAt).Milliseconds()
			res.Class, res.Hint = ClassUnknown, "远端作业目录不见了（被清理或换了机器）；命令是否执行过不确定。"
			return res, codeErr(CodeRemoteFileIO, "bash: 远端作业目录 %s 不存在", job.Dir)
		}
		if ctx != nil && ctx.Err() != nil {
			res.DurationMS = time.Since(startedAt).Milliseconds()
			res.Status = StatusChannelTimeout
			res.Segment = SegChannel
			res.StillRunning = true
			res.Note = "调用方取消（ctx）；远端作业仍在运行，可按 handle / log_path 取回或终止。"
			return res, nil
		}
		if time.Now().After(deadline) {
			// 预算用尽：先尽力把已有输出带回去，再如实说"还在跑"。
			stdout, stderr, truncated, _ := client.jobOutput(ctx, job.Handle, 0)
			res.DurationMS = time.Since(startedAt).Milliseconds()
			res.Status = StatusChannelTimeout
			res.Segment = SegChannel
			res.StillRunning = true
			res.Stdout, res.Stderr, res.Truncated = stdout, stderr, truncated
			res.Note = "本端等到预算用尽；远端作业**仍在运行**（输出是到目前为止的落盘内容）。" +
				"要取回后续输出或终止它，用 bg_manage 带着这个 handle。"
			return res, nil
		}
		sleepCtx(ctx, JobPollInterval)
	}
}

// sleepCtx 是可取消的等待（轮询间隔）。
func sleepCtx(ctx context.Context, d time.Duration) {
	if ctx == nil {
		time.Sleep(d)
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// ── Client 面：同一条连接上开多条会话（起/查/取/杀各一条）──────────────

// startJob 起一条受管作业（命令正文经内容通道写入作业目录）。
func (c *Client) startJob(ctx context.Context, command, workdir string) (JobStarted, error) {
	if strings.TrimSpace(command) == "" {
		return JobStarted{}, codeErr(CodeNoCommand, "命令正文为空：没有任何东西可执行")
	}
	dirExpr := scriptDir(c.spec.ID, "job", time.Now())
	supervisor := supervisorScript(workdir)
	script := strings.Join([]string{
		`__d="` + dirExpr + `"`,
		`mkdir -p "$__d" || exit 91`,
		`base64 -d > "$__d/cmd.sh" || exit 92`,
		`printf %s '` + encodePayload(supervisor) + `' | base64 -d > "$__d/run.sh" || exit 92`,
		`chmod 700 "$__d/run.sh" "$__d/cmd.sh" || exit 91`,
		`setsid bash "$__d/run.sh" "$__d" </dev/null >/dev/null 2>&1 &`,
		`echo "` + startMarker + `$__d"`,
	}, "; ")
	// 命令正文走内容通道：它可能多行、含中文、含引号——过 shell 词法就是静默改脚本。
	res, err := c.runCommand(ctx, script, stdinOrNil(encodePayload(command)), nil, 0, "")
	if err != nil {
		return JobStarted{}, err
	}
	if res.ExitCode != 0 {
		return JobStarted{}, remoteScriptError("bash(远端作业受理)", res)
	}
	dir := parseStartDir(res.Stdout)
	if dir == "" {
		return JobStarted{}, codeErr(CodeRemoteFileIO,
			"bash: 远端作业目录没有回读出来（stdout=%q）：无法给出可再指认的句柄", strings.TrimSpace(res.Stdout))
	}
	started := JobStarted{
		Dir: dir, Handle: mintJobHandle(dir),
		OutPath: dir + "/out", ErrPath: dir + "/err",
	}
	// pid 由监管脚本写入，可能比本次调用晚一瞬；读不到不算失败（状态读数会补上）。
	if state, stateErr := c.jobStatus(ctx, started.Handle); stateErr == nil {
		started.PID = state.PID
	}
	return started, nil
}

// jobStatus 读一次状态读数。
func (c *Client) jobStatus(ctx context.Context, handle string) (JobState, error) {
	dir, err := ParseJobHandle(handle)
	if err != nil {
		return JobState{}, err
	}
	script := strings.Join([]string{
		decodeVar("__d", dir),
		`[ -d "$__d" ] || { echo "` + JobGone + `"; exit 0; }`,
		`echo "pid=$(cat "$__d/pid" 2>/dev/null)"`,
		`if [ -f "$__d/exit" ]; then printf '` + JobExited + ` '; cat "$__d/exit"; else echo "` + JobRunning + `"; fi`,
	}, "; ")
	res, err := c.runCommand(ctx, script, nil, nil, 0, "")
	if err != nil {
		return JobState{}, err
	}
	if res.ExitCode != 0 {
		return JobState{}, remoteScriptError("bg_manage(observe)", res)
	}
	return parseJobState(res.Stdout)
}

// jobOutput 取回 out / err 的当前内容（未产出 = 空，不是错误）。
func (c *Client) jobOutput(ctx context.Context, handle string, maxBytes int) (string, string, bool, error) {
	dir, err := ParseJobHandle(handle)
	if err != nil {
		return "", "", false, err
	}
	limit := maxBytes
	if limit <= 0 {
		limit = c.spec.maxOutput()
	}
	stdout, outTrunc, err := c.readJobLog(ctx, dir, "out", limit)
	if err != nil {
		return "", "", false, err
	}
	stderr, errTrunc, err := c.readJobLog(ctx, dir, "err", limit)
	if err != nil {
		return "", "", false, err
	}
	return stdout, stderr, outTrunc || errTrunc, nil
}

// readJobLog 读作业目录里的一个日志文件。
//
// 刻意**不做增量游标**：游标要么存在本端（跨调用的进程状态，不可依赖），要么写进
// 句柄（句柄会因此变成"每次都变"的东西，模型很难用对）。远端面因此是"重读当前日志"，
// 由调用方按 truncated / 内容长度自己决定要不要继续追——回执里说清，不假装是增量。
func (c *Client) readJobLog(ctx context.Context, dir, name string, limit int) (string, bool, error) {
	script := strings.Join([]string{
		decodeVar("__d", dir),
		`[ -f "$__d/` + name + `" ] || exit 0`,
		`base64 < "$__d/` + name + `" || exit 92`,
	}, "; ")
	res, err := c.runCommand(ctx, script, nil, nil, 0, "")
	if err != nil {
		return "", false, err
	}
	if res.ExitCode != 0 {
		return "", false, remoteScriptError("bg_manage(fetch)", res)
	}
	data, truncated := decodeRemoteBase64(res.Stdout, res.Truncated)
	text := string(data)
	if limit > 0 && len(text) > limit {
		text, truncated = text[:limit], true
	}
	return text, truncated, nil
}

// jobKill 终止远端作业（整棵进程树：监管进程是 setsid 的组长，负 pid 即整组）。
//
// 回报口径：命令**已发出**不代表进程已死（SIGTERM 可被忽略）。所以这里只回执"发了
// 什么"，最终状态交给调用方再读一次 Status——不虚构"已终止"。
func (c *Client) jobKill(ctx context.Context, handle string) (string, error) {
	dir, err := ParseJobHandle(handle)
	if err != nil {
		return "", err
	}
	script := strings.Join([]string{
		decodeVar("__d", dir),
		`[ -d "$__d" ] || { echo "` + JobGone + `"; exit 0; }`,
		`[ -f "$__d/exit" ] && { echo "already exited"; exit 0; }`,
		`__p=$(cat "$__d/pid" 2>/dev/null)`,
		`[ -n "$__p" ] || { echo "no pid"; exit 0; }`,
		`kill -TERM -"$__p" 2>/dev/null || kill -TERM "$__p" 2>/dev/null || { echo "kill failed"; exit 0; }`,
		`echo "signaled $__p"`,
	}, "; ")
	res, err := c.runCommand(ctx, script, nil, nil, 0, "")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", remoteScriptError("bg_manage(kill)", res)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// supervisorScript 生成监管脚本（base64 承载，因此可以是多行）。
func supervisorScript(workdir string) string {
	return strings.Join([]string{
		`d="$1"`,
		`echo $$ > "$d/pid"`,
		`w=$(printf %s '` + encodePayload(workdir) + `' | base64 -d)`,
		`if [ -n "$w" ]; then cd "$w" || { echo "cd failed: $w" > "$d/err"; echo 126 > "$d/exit"; exit 0; }; fi`,
		`bash "$d/cmd.sh" </dev/null >"$d/out" 2>"$d/err"`,
		`echo $? > "$d/exit"`,
	}, "\n")
}

// parseStartDir 从起作业的回执里取出展开后的目录。
func parseStartDir(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, startMarker) {
			return strings.TrimSpace(strings.TrimPrefix(line, startMarker))
		}
	}
	return ""
}

// mintJobHandle 把远端目录铸成自描述句柄（`ssh:<base64url(dir)>`）。
func mintJobHandle(dir string) string {
	return jobHandlePrefix + base64.RawURLEncoding.EncodeToString([]byte(dir))
}

// IsRemoteJobHandle 报告句柄是不是远端作业句柄（`ssh:` 族）。
//
// 判据只有前缀一处：工具层据此把管理动作分派到远端面，**不**去解码内容（内容坏了由
// ParseJobHandle 报错，两件事分开）。
func IsRemoteJobHandle(handle string) bool {
	return strings.HasPrefix(strings.TrimSpace(handle), jobHandlePrefix)
}

// ParseJobHandle 解出句柄里的远端目录；不是本族的句柄给一句可照做的错误。
func ParseJobHandle(handle string) (string, error) {
	trimmed := strings.TrimSpace(handle)
	if !strings.HasPrefix(trimmed, jobHandlePrefix) {
		return "", codeErr(CodeRemotePathInvalid,
			"句柄 %q 不是远端作业句柄（远端句柄形如 %s<base64>）：本机作业句柄（bt- / bs- / bteam-）由本机作业面管理，不归远端面",
			handle, jobHandlePrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(trimmed, jobHandlePrefix))
	if err != nil || len(raw) == 0 {
		return "", codeErr(CodeRemotePathInvalid, "句柄 %q 的远端目录段无法解码", handle)
	}
	return string(raw), nil
}

// parseJobState 解析状态读数（`pid=<n>` + `running|exited <code>|gone`）。
func parseJobState(stdout string) (JobState, error) {
	state := JobState{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "pid="):
			state.PID = strings.TrimPrefix(line, "pid=")
		case line == JobRunning:
			state.State = JobRunning
		case line == JobGone:
			state.State = JobGone
		case strings.HasPrefix(line, JobExited):
			state.State = JobExited
			if parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, JobExited))); err == nil {
				state.ExitCode = parsed
			}
		}
	}
	if state.State == "" {
		return JobState{}, codeErr(CodeRemoteFileIO, "bg_manage: 远端状态读数无法解析: %q", stdout)
	}
	return state, nil
}
