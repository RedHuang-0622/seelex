// Command sshprobe 是内置 SSH 基座（backend/tool/ssh）的独立驱动：一次连接、
// 一次探针、一次命令，或一次显式信任。
//
// 它是**工具族的样板与排障入口**，不是给模型直接调用的工具：模型侧走
// backend/tool/registry 里的 ssh_* 工具族，参数来自项目内 host 注册表。
//
// 用法（凭据只从文件读，命令行上永远没有秘密）：
//
//	sshprobe -host host.internal -user deploy -key ~/.ssh/id_ed25519 -probe
//	sshprobe -host host.internal -user deploy -auth agent -cmd 'git rev-parse HEAD'
//	sshprobe -host host.internal -user deploy -key ~/.ssh/id_ed25519 \
//	    -cmd-file ./scripts/check.sh -json
//	sshprobe -host host.internal -user deploy -key ~/.ssh/id_ed25519 \
//	    -trust 'SHA256:...'      # 首次连接：指纹必须是人工核对过的
//
// 退出码：0 成功；2 用法错误；3 连接/探针分类失败；4 命令来源非法（含非 ASCII
// 内联命令）；5 channel 超时（本端不再等待，远端可能仍在跑）。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// 退出码：调用方（脚本/CI）据此分流，不要用 1 一把梭。
const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitProbe    = 3
	exitBadInput = 4
	exitTimeout  = 5
)

// config 是命令行参数的直接映射（不含任何秘密字面量——秘密只以文件路径出现）。
type config struct {
	id       string
	host     string
	port     int
	user     string
	authMode string
	keyPath  string
	keyPass  string // 文件路径
	password string // 文件路径
	agent    string

	knownHosts stringList
	maxOutput  int

	connectTimeout time.Duration
	bannerTimeout  time.Duration
	authTimeout    time.Duration
	channelTimeout time.Duration

	probe   bool
	trust   string
	cmd     string
	cmdFile string
	stdin   string
	detach  bool
	logPath string
	workdir string
	jsonOut bool
}

// stringList 是可重复 flag（known_hosts 可以有多个来源）。
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 是 main 的驱动函数：解析 → 建 Spec → 分派 → 渲染。它不 os.Exit，
// 因此可以被测试直接调用（返回值即退出码）。
func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintf(stderr, "sshprobe: %v\n", err)
		return exitUsage
	}

	spec, err := buildSpec(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "sshprobe: %v\n", err)
		return exitBadInput
	}

	// 探针/信任用短预算：它们的失败要快速回到人手里。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch {
	case cfg.trust != "":
		return doTrust(ctx, spec, cfg, stdout, stderr)
	case cfg.probe:
		return doProbe(ctx, spec, cfg, stdout, stderr)
	default:
		return doExec(ctx, spec, cfg, stdout, stderr)
	}
}

// parseFlags 解析参数并做**互斥校验**（在连任何地方之前就把用法错误挡住）。
func parseFlags(args []string, stderr io.Writer) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("sshprobe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "sshprobe —— 内置 SSH 基座的独立驱动\n\n")
		fmt.Fprintf(stderr, "凭据只从文件读（-password-file / -key-pass-file），命令行上不放秘密。\n\n")
		fs.PrintDefaults()
	}

	fs.StringVar(&cfg.id, "id", "", "稳定标识（仅用于日志与句柄前缀，不参与连接）")
	fs.StringVar(&cfg.host, "host", "", "主机名或 IP（必填）")
	fs.IntVar(&cfg.port, "port", 22, "SSH 端口")
	fs.StringVar(&cfg.user, "user", "", "登录用户（必填）")
	fs.StringVar(&cfg.authMode, "auth", "auto", "认证方式：auto|agent|key|password")
	fs.StringVar(&cfg.keyPath, "key", "", "私钥文件路径")
	fs.StringVar(&cfg.keyPass, "key-pass-file", "", "私钥口令文件（不要用命令行传口令）")
	fs.StringVar(&cfg.password, "password-file", "", "密码文件（不要用命令行传密码）")
	fs.StringVar(&cfg.agent, "agent-socket", "", "agent 套接字（默认 unix: $SSH_AUTH_SOCK / windows: 命名管道）")
	fs.Var(&cfg.knownHosts, "known-hosts", "known_hosts 文件（可重复；默认 ~/.ssh/known_hosts）")
	fs.IntVar(&cfg.maxOutput, "max-output", ssh.DefaultMaxOutputBytes, "stdout/stderr 各自的字节上限")

	fs.DurationVar(&cfg.connectTimeout, "connect-timeout", 10*time.Second, "TCP 建连超时")
	fs.DurationVar(&cfg.bannerTimeout, "banner-timeout", 10*time.Second, "握手/banner 超时")
	fs.DurationVar(&cfg.authTimeout, "auth-timeout", 20*time.Second, "认证超时")
	fs.DurationVar(&cfg.channelTimeout, "channel-timeout", 120*time.Second, "命令通道超时")

	fs.BoolVar(&cfg.probe, "probe", false, "只探针（不执行命令，不写 known_hosts）")
	fs.StringVar(&cfg.trust, "trust", "", "首次信任：给出人工核对过的 SHA256 指纹后写入 known_hosts")
	fs.StringVar(&cfg.cmd, "cmd", "", "内联命令（只允许单行 ASCII）")
	fs.StringVar(&cfg.cmdFile, "cmd-file", "", "本地脚本文件（首选：多行/含中文都走这里）")
	fs.StringVar(&cfg.stdin, "stdin", "", "直接经 stdin 送入的脚本文本")
	fs.BoolVar(&cfg.detach, "detach", false, "脱钩执行（setsid，返回日志路径）")
	fs.StringVar(&cfg.logPath, "log-path", "", "脱钩执行的远端日志路径")
	fs.StringVar(&cfg.workdir, "workdir", "", "执行前的远端工作目录")
	fs.BoolVar(&cfg.jsonOut, "json", false, "输出 JSON（默认人读文本）")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.host == "" || cfg.user == "" {
		return config{}, errors.New("-host and -user are required")
	}

	sources := 0
	for _, s := range []string{cfg.cmd, cfg.cmdFile, cfg.stdin} {
		if s != "" {
			sources++
		}
	}
	if cfg.trust != "" {
		if sources > 0 || cfg.detach {
			return config{}, errors.New("-trust cannot be combined with a command")
		}
		return cfg, nil
	}
	if cfg.probe {
		if sources > 0 || cfg.detach {
			return config{}, errors.New("-probe cannot be combined with a command")
		}
		return cfg, nil
	}
	if sources != 1 {
		return config{}, errors.New("exactly one of -cmd / -cmd-file / -stdin is required (or use -probe)")
	}
	if cfg.detach && cfg.stdin != "" {
		// 脱钩形态的脚本要落到远端文件，stdin 在这里没有通道可走。
		return config{}, errors.New("-detach does not accept -stdin; use -cmd-file")
	}
	return cfg, nil
}

// buildSpec 把 config 变成 ssh.Spec：凭据在这一步从文件读入内存，读完即用。
func buildSpec(cfg config) (ssh.Spec, error) {
	spec := ssh.Spec{
		ID:       cfg.id,
		Hostname: cfg.host,
		Port:     cfg.port,
		User:     cfg.user,
		Timeouts: ssh.Timeouts{
			Connect: cfg.connectTimeout,
			Banner:  cfg.bannerTimeout,
			Auth:    cfg.authTimeout,
			Channel: cfg.channelTimeout,
		},
		Workdir:        cfg.workdir,
		MaxOutputBytes: cfg.maxOutput,
		KnownHosts:     ssh.KnownHostsSpec{Files: cfg.knownHosts, Mode: ssh.KnownHostsStrict},
		Auth: ssh.AuthSpec{
			Mode:        ssh.AuthMode(cfg.authMode),
			KeyPath:     cfg.keyPath,
			AgentSocket: cfg.agent,
		},
	}
	if cfg.keyPass != "" {
		secret, err := readSecretFile(cfg.keyPass)
		if err != nil {
			return ssh.Spec{}, err
		}
		spec.Auth.Passphrase = secret
	}
	if cfg.password != "" {
		secret, err := readSecretFile(cfg.password)
		if err != nil {
			return ssh.Spec{}, err
		}
		spec.Auth.Password = secret
	}
	return spec, nil
}

// readSecretFile 读一个只含凭据的文件：去掉行尾换行（编辑器普遍会补一个）。
//
// 秘密不进 argv、不进环境、不进日志：唯一的入口就是这里，且调用方用完即丢。
func readSecretFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", path, err)
	}
	return strings.TrimRight(string(raw), "\r\n"), nil
}

// doProbe 执行探针并渲染结果。
func doProbe(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int {
	res := ssh.Probe(ctx, spec)
	if cfg.jsonOut {
		writeJSON(stdout, res)
	} else {
		fmt.Fprintf(stdout, "host      : %s (%s)\n", displayID(spec), spec.Addr())
		fmt.Fprintf(stdout, "result    : %s\n", pickOK(res.OK))
		if res.Class != "" {
			fmt.Fprintf(stdout, "class     : %s\n", res.Class)
		}
		if res.Segment != "" {
			fmt.Fprintf(stdout, "segment   : %s\n", res.Segment)
		}
		if res.AuthUsed != "" {
			fmt.Fprintf(stdout, "auth_used : %s\n", res.AuthUsed)
		}
		if res.Fingerprint != "" {
			fmt.Fprintf(stdout, "fingerprint: %s\n", res.Fingerprint)
		}
		if res.Hint != "" {
			fmt.Fprintf(stdout, "hint      : %s\n", res.Hint)
		}
		if len(res.AllowedMethods) > 0 {
			fmt.Fprintf(stdout, "server allows: %s\n", strings.Join(res.AllowedMethods, ", "))
		}
		if !res.OK && res.KnownHostLine != "" {
			fmt.Fprintf(stdout, "\n核对指纹无误后执行信任：\n  sshprobe ... -trust '%s'\n", res.Fingerprint)
			fmt.Fprintf(stdout, "将追加的条目：%s\n", res.KnownHostLine)
		}
	}
	if res.OK {
		return exitOK
	}
	fmt.Fprintf(stderr, "sshprobe: probe failed (%s)\n", res.Class)
	return exitProbe
}

// doTrust 执行首次信任（指纹必须与本次握手逐字相等，否则不写盘）。
func doTrust(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int {
	path, line, class, err := ssh.TrustFromProbe(ctx, spec, cfg.trust)
	if err != nil {
		fmt.Fprintf(stderr, "sshprobe: trust failed (%s): %v\n", class, err)
		return exitProbe
	}
	if cfg.jsonOut {
		writeJSON(stdout, map[string]string{"known_hosts": path, "line": line, "class": class})
	} else {
		fmt.Fprintf(stdout, "已写入 %s\n%s\n", path, line)
	}
	return exitOK
}

// doExec 执行一次命令（同步或脱钩）。
func doExec(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int {
	client, err := ssh.Dial(ctx, spec)
	if err != nil {
		// 连接失败时补一次探针：把"哪一段坏了"翻成分类，而不是把裸错误丢给人。
		probe := ssh.Probe(ctx, spec)
		fmt.Fprintf(stderr, "sshprobe: dial failed (%s/%s): %s\n", probe.Class, probe.Segment, probe.Hint)
		return exitProbe
	}
	defer func() { _ = client.Close() }()

	var res ssh.Result
	if cfg.detach {
		res, err = client.RunDetached(ctx, ssh.DetachRequest{
			Inline:      cfg.cmd,
			CommandFile: cfg.cmdFile,
			Shell:       "bash",
			Workdir:     cfg.workdir,
			LogPath:     cfg.logPath,
		})
	} else {
		res, err = client.Run(ctx, ssh.Request{
			Inline:         cfg.cmd,
			CommandFile:    cfg.cmdFile,
			Stdin:          cfg.stdin,
			Shell:          "bash",
			Workdir:        cfg.workdir,
			MaxOutputBytes: cfg.maxOutput,
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "sshprobe: %v\n", err)
		if isBadInput(err) {
			return exitBadInput
		}
		return exitFailure
	}

	if cfg.jsonOut {
		writeJSON(stdout, res)
	} else {
		renderResult(stdout, spec, res)
	}
	return exitFor(res)
}

// exitFor 把 Result 翻成退出码：远端退出码原样透传（这才是脚本要的），
// channel 超时单列一格，避免被读成"命令失败"。
func exitFor(res ssh.Result) int {
	switch res.Status {
	case ssh.StatusDetached:
		return exitOK
	case ssh.StatusChannelTimeout:
		return exitTimeout
	case ssh.StatusFailed:
		return exitFailure
	case ssh.StatusCompleted:
		if res.ExitCode <= 0 {
			return exitOK
		}
		if res.ExitCode > 125 {
			return exitFailure
		}
		return res.ExitCode
	}
	return exitFailure
}

// renderResult 是人读渲染：先事实（状态/退出码/认证方式），再输出。
func renderResult(w io.Writer, spec ssh.Spec, res ssh.Result) {
	fmt.Fprintf(w, "host      : %s (%s)\n", displayID(spec), spec.Addr())
	fmt.Fprintf(w, "status    : %s (exit=%d, %dms)\n", res.Status, res.ExitCode, res.DurationMS)
	fmt.Fprintf(w, "auth_used : %s\n", res.AuthUsed)
	if res.StillRunning {
		fmt.Fprintf(w, "still_running: true\n")
	}
	if res.LogPath != "" {
		fmt.Fprintf(w, "log_path  : %s\n", res.LogPath)
	}
	if res.Handle != "" {
		fmt.Fprintf(w, "handle    : %s\n", res.Handle)
	}
	if res.Truncated {
		fmt.Fprintf(w, "truncated : 输出超过上限，仅保留前段\n")
	}
	if res.LossyUTF8 {
		fmt.Fprintf(w, "encoding  : 输出含非法 UTF-8 字节\n")
	}
	if res.Note != "" {
		fmt.Fprintf(w, "note      : %s\n", res.Note)
	}
	if res.Stdout != "" {
		fmt.Fprintf(w, "\n--- stdout ---\n%s\n", res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprintf(w, "\n--- stderr ---\n%s\n", res.Stderr)
	}
}

// isBadInput 判断错误是否属于"调用方给了不该给的入参"（而非环境问题）。
func isBadInput(err error) bool {
	var codeErr *ssh.CodeError
	if errors.As(err, &codeErr) {
		switch codeErr.Code {
		case ssh.CodeNonASCIIInline, ssh.CodeInlineMultiline, ssh.CodeCommandConflict, ssh.CodeNoCommand,
			ssh.CodeInvalidScriptUTF8:
			return true
		}
	}
	return false
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func pickOK(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}

func displayID(spec ssh.Spec) string {
	if spec.ID == "" {
		return "(no id)"
	}
	return spec.ID
}
