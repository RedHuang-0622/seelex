package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/fs"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// Deps is the runtime callback set injected by the root package (Runtime).
type Deps struct {
	RegisterTool           func(name, description string, schema map[string]interface{}, handler func(ctx context.Context, argsJSON string) (string, error))
	ProjectScope           *security.ProjectScope
	FileSystem             fs.FileSystem
	GrepMaxResults         int
	WalkTimeoutSec         int
	ToolCallTimeout        time.Duration
	ToolCallTimeoutSec     int
	DisableDockerAutoStart bool
	ObserveBash            func(BashDiagnosticEvent)
	EnsureDocker           func(ctx context.Context) error
	DockerDaemonDown       func(stdout, stderr string) bool
	DockerCLIPath          func() string
	DockerHint             func(err error) string
}

// Router registers and executes the project-scoped tool family.
type Router struct {
	deps Deps
}

// NewRouter constructs the scoped tool router with injected deps.
func NewRouter(deps Deps) *Router {
	return &Router{deps: deps}
}

// resolveNodePath 解析工具路径的根：worktree 节点（NodeScope.WorkspaceID
// 指向 worktree 根）→ 节点根；否则 ProjectScope 单根。worktree 目录由
// git 创建（真实目录），resolveInside 内含 withinRoot 校验（越界拒绝）。
func (r *Router) resolveNodePath(ctx context.Context, path string, forWrite bool) (string, error) {
	if scope, ok := model.NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.WorkspaceID != "" {
		candidate, err := security.ResolveInside(scope.WorkspaceID, path)
		if err != nil {
			return "", err
		}
		return candidate, nil
	}
	if forWrite {
		return r.deps.ProjectScope.ResolveWrite(path)
	}
	return r.deps.ProjectScope.ResolveRead(path)
}

// registerProjectScopedTools overrides the Seele builtin filesystem tools.
// Holder inline providers take precedence over builtin providers with the same
// name, keeping this policy local to Seelex.
func (r *Router) Register() {
	r.deps.RegisterTool("read_file", "Read a file inside the bound project.", readFileSchema(), r.scopedReadFile)
	r.deps.RegisterTool("grep_search", "Search file contents inside the bound project.", grepSchema(), r.scopedGrep)
	r.deps.RegisterTool("glob", "Find matching files inside the bound project.", globSchema(), r.scopedGlob)
	r.deps.RegisterTool("write_file", "Write a file inside the bound project.", writeFileSchema(), r.scopedWriteFile)
	r.deps.RegisterTool("edit_file", "Edit a file inside the bound project.", editFileSchema(), r.scopedEditFile)
	r.deps.RegisterTool("bash", "Run a command with its working directory constrained to the bound project. This is not an OS sandbox.", bashSchema(), r.scopedBash)
}

type scopedReadFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

func (r *Router) scopedReadFile(ctx context.Context, argsJSON string) (string, error) {
	var input scopedReadFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_file: invalid args: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	path, err := r.resolveNodePath(ctx, input.Path, false)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: read %q: %w", input.Path, err)
	}
	if input.StartLine <= 0 {
		input.StartLine = 1
	}
	lines := strings.Split(string(data), "\n")
	start := input.StartLine - 1
	if start >= len(lines) {
		return "", fmt.Errorf("read_file: start_line %d exceeds file length %d", input.StartLine, len(lines))
	}
	end := len(lines)
	if input.EndLine > 0 && input.EndLine >= input.StartLine && input.EndLine < end {
		end = input.EndLine
	}
	return strings.Join(lines[start:end], "\n"), nil
}

type scopedGrepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}
type scopedGrepResult struct {
	Path    string `json:"path"`
	LineNum int    `json:"line_num"`
	Content string `json:"content"`
}

func (r *Router) scopedGrep(ctx context.Context, argsJSON string) (string, error) {
	var input scopedGrepInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("grep_search: invalid args: %w", err)
	}
	if input.Pattern == "" {
		return "[]", nil
	}
	root, err := r.resolveNodePath(ctx, input.Path, false)
	if err != nil {
		return "", err
	}
	if input.MaxResults <= 0 {
		input.MaxResults = r.deps.GrepMaxResults // limits.grep_max_results（默认 20）
	}
	walkCtx, cancelWalk := walkContext(ctx, r.walkTimeout())
	defer cancelWalk()
	results := make([]scopedGrepResult, 0)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			if (strings.HasPrefix(info.Name(), ".") || heavyDirNames[info.Name()]) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if input.Glob != "" {
			matched, matchErr := filepath.Match(input.Glob, info.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}
		select {
		case <-walkCtx.Done():
			return filepath.SkipAll // 超时 = 另一种截断，返回已收集结果
		default:
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, input.Pattern) {
				display, _ := r.deps.ProjectScope.Relative(path)
				results = append(results, scopedGrepResult{Path: display, LineNum: index + 1, Content: strings.TrimSpace(line)})
				if len(results) >= input.MaxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && len(results) == 0 {
		return "", fmt.Errorf("grep_search: walk: %w", err)
	}
	// 遍历超时：与 MaxResults 同语义的另一种截断——返回已收集的部分
	// 结果（契约保持数组；模型可从数量感知不完整）。
	output, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("grep_search: marshal: %w", err)
	}
	return string(output), nil
}

type scopedGlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (r *Router) scopedGlob(ctx context.Context, argsJSON string) (string, error) {
	var input scopedGlobInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("glob: invalid args: %w", err)
	}
	if input.Pattern == "" {
		return "[]", nil
	}
	root, err := r.resolveNodePath(ctx, input.Path, false)
	if err != nil {
		return "", err
	}
	walkCtx, cancelWalk := walkContext(ctx, r.walkTimeout())
	defer cancelWalk()
	results := make([]string, 0)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			// 跳过隐藏目录与重目录（构建产物/依赖/版本控制），避免
			// **/* 全树遍历卡顿（对齐 ripgrep 默认忽略语义）。
			if (strings.HasPrefix(info.Name(), ".") || heavyDirNames[info.Name()]) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-walkCtx.Done():
			return filepath.SkipAll // 超时 = 另一种截断，返回已收集结果
		default:
		}
		if matchGlobPattern(input.Pattern, path) || matchGlobPattern(input.Pattern, info.Name()) {
			display, _ := r.deps.ProjectScope.Relative(path)
			results = append(results, display)
		}
		return nil
	})
	if err != nil && len(results) == 0 {
		return "", fmt.Errorf("glob: walk: %w", err)
	}
	output, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("glob: marshal: %w", err)
	}
	return string(output), nil
}

// walkTimeout 返回 glob/grep 目录遍历超时（limits.walk_timeout，默认 30s；
// 0 = 不限制）。遍历是 IO 密集操作，慢盘/大仓库下必须有界。
func (r *Router) walkTimeout() time.Duration {
	if r.deps.WalkTimeoutSec <= 0 {
		return 0
	}
	return time.Duration(r.deps.WalkTimeoutSec) * time.Second
}

// walkContext 构造遍历上下文：超时 >0 时包 WithTimeout；0 = 不限制
// （直接返回原 ctx——WithTimeout(ctx, 0) 会立即过期，语义错误）。
func walkContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// heavyDirNames 是遍历时跳过的重目录（构建产物/依赖/版本控制；对齐
// ripgrep 默认忽略，避免全树遍历在慢盘上卡顿）。
var heavyDirNames = map[string]bool{
	"node_modules": true, "build": true, "out": true, "vendor": true,
	"__pycache__": true, ".venv": true, "venv": true, "target": true,
	"obj": true, "bin": true, "coverage": true, "dist": true,
}

// matchGlobPattern 是 glob 模式匹配（正斜杠语义，路径统一 ToSlash 后
// 匹配；支持 ** 递归通配——filepath.Match 不支持 ** 且 Windows 上模式
// 正斜杠与路径反斜杠不匹配，导致 **/* 恒空）。
func matchGlobPattern(pattern, path string) bool {
	return globMatch(filepath.ToSlash(pattern), filepath.ToSlash(path))
}

// globMatch 递归匹配按 "/" 切分的 glob 模式与路径：
//   - ** 匹配任意段（0+）；**/ 匹配 0+ 段前缀；
//   - * 匹配单段内任意字符；? 匹配单字符；
//   - 其余字符字面匹配。
func globMatch(pattern, path string) bool {
	if pattern == "" {
		return path == ""
	}
	if path == "" {
		return pattern == "**"
	}
	if pattern == "**" {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		// **/ 匹配 0 段（跳过）或 1 段（消费路径首段后继续）。
		return globMatch(pattern[3:], path) || globMatch(pattern, afterFirstSegment(path))
	}
	if !globSegmentMatch(beforeFirstSegment(pattern), beforeFirstSegment(path)) {
		return false
	}
	return globMatch(afterFirstSegment(pattern), afterFirstSegment(path))
}

// globSegmentMatch 匹配单段（无分隔符；* 任意、? 单字符、字面）。
func globSegmentMatch(pattern, segment string) bool {
	if pattern == "" {
		return segment == ""
	}
	if segment == "" {
		return allStars(pattern)
	}
	switch pattern[0] {
	case '*':
		// 贪心：* 匹配 0+ 字符，剩余模式继续。
		return globSegmentMatch(pattern[1:], segment) || globSegmentMatch(pattern, segment[1:])
	case '?':
		return globSegmentMatch(pattern[1:], segment[1:])
	default:
		if pattern[0] != segment[0] {
			return false
		}
		return globSegmentMatch(pattern[1:], segment[1:])
	}
}

func beforeFirstSegment(value string) string {
	if index := strings.IndexByte(value, '/'); index >= 0 {
		return value[:index]
	}
	return value
}

func afterFirstSegment(value string) string {
	if index := strings.IndexByte(value, '/'); index >= 0 {
		return value[index+1:]
	}
	return ""
}

func allStars(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '*' {
			return false
		}
	}
	return len(value) > 0
}

type scopedWriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *Router) scopedWriteFile(ctx context.Context, argsJSON string) (string, error) {
	var input scopedWriteFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("write_file: invalid args: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	path, err := r.resolveNodePath(ctx, input.Path, true)
	if err != nil {
		return "", err
	}
	// 写路径经文件系统 actor（per-path 串行化——并行子代理写同一文件互斥，
	// 见 docs/plan/file-operations-actorization.md P0）。
	if err := r.deps.FileSystem.Write(path, []byte(input.Content)); err != nil {
		return "", fmt.Errorf("write_file: write %q: %w", input.Path, err)
	}
	return fmt.Sprintf(`{"status":"ok","path":%q,"size":%d}`, input.Path, len(input.Content)), nil
}

type scopedEditFileInput struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (r *Router) scopedEditFile(ctx context.Context, argsJSON string) (string, error) {
	var input scopedEditFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("edit_file: invalid args: %w", err)
	}
	if input.Path == "" || input.OldString == "" {
		return "", fmt.Errorf("edit_file: path and old_string are required")
	}
	path, err := r.resolveNodePath(ctx, input.Path, true)
	if err != nil {
		return "", err
	}
	// 读-改-写经文件系统 actor 原子化（锁内 read-modify-write，同路径串行）。
	count, err := r.deps.FileSystem.Edit(path, input.OldString, input.NewString)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("edit_file: old_string not found in %q", input.Path)
		}
		return "", fmt.Errorf("edit_file: %q: %w", input.Path, err)
	}
	return fmt.Sprintf(`{"status":"ok","path":%q,"replacements":%d}`, input.Path, count), nil
}

type scopedBashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Workdir string `json:"workdir,omitempty"`
}
type scopedBashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// BashDiagnosticEvent describes a boundary crossed by scopedBash. It is
// deliberately metadata-only: command text, arguments, working directories,
// output, and account data must never enter diagnostic logs.
type BashDiagnosticEvent struct {
	Stage    string
	Shell    string
	ExitCode int
	Err      error
}

// BashDiagnosticObserver receives best-effort scoped bash stage events.
// Implementations should return promptly; observer failures are isolated from
// the tool call itself.
type BashDiagnosticObserver func(BashDiagnosticEvent)

func (r *Router) scopedBash(ctx context.Context, argsJSON string) (output string, returnedErr error) {
	defer func() {
		if returnedErr != nil {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.handler.return.error", Err: returnedErr})
			return
		}
		r.observeBash(BashDiagnosticEvent{Stage: "bash.handler.return"})
	}()
	var input scopedBashInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("bash: invalid args: %w", err)
	}
	if input.Command == "" {
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.start"})
	workdir, err := r.resolveNodePath(ctx, input.Workdir, false)
	if err != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.error", Err: err})
		return "", err
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.done"})
	// 执行路径（2026-08-04 回滚）：沙箱接入被怀疑导致工具挂起，恢复 v1
	// 直连 exec（cwd 门禁语义不变）；CommandSandbox 接口保留在 sandbox.go，
	// 待定位挂起根因后再接入（接入时需 fail-fast，不得悄悄降级）。
	shell, shellArgs := scopedBashCommand(input.Command)
	r.observeBash(BashDiagnosticEvent{Stage: "bash.command.prepared", Shell: filepath.Base(shell)})
	timeout := r.scopedToolTimeout(input.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, shell, shellArgs...)
	cmd.Dir = workdir
	security.ConfigureHiddenCommand(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.starting", Shell: filepath.Base(shell)})
	if err = cmd.Start(); err != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.process.start.error", Shell: filepath.Base(shell), Err: err})
		return "", fmt.Errorf("bash: %w", err)
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.started", Shell: filepath.Base(shell)})
	err = cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.timeout", Shell: filepath.Base(shell), Err: runCtx.Err()})
		return "", fmt.Errorf("bash: timeout after %v", timeout)
	}
	if runCtx.Err() != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.canceled", Shell: filepath.Base(shell), Err: runCtx.Err()})
		return "", fmt.Errorf("bash: %w", runCtx.Err())
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.process.wait.error", Shell: filepath.Base(shell), Err: err})
			return "", fmt.Errorf("bash: %w", err)
		}
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.exited", Shell: filepath.Base(shell), ExitCode: exitCode})
	result := scopedBashResult{Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String()), ExitCode: exitCode}
	// docker 自动恢复（根治，2026-08-07）：真实环境有 docker CLI 但 Docker
	// Desktop 守护进程未运行时，命令失败并匹配 daemon-down 模式 → 自动启动
	// 守护进程（limits.disable_docker_auto_start 可关）→ 就绪后重跑一次。
	if result.ExitCode != 0 && !r.deps.DisableDockerAutoStart &&
		r.deps.DockerDaemonDown(result.Stdout, result.Stderr) && r.deps.DockerCLIPath() != "" {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.docker.recovery", Shell: filepath.Base(shell)})
		startErr := r.deps.EnsureDocker(ctx)
		if startErr == nil {
			// 重跑（新超时上下文；原 runCtx 可能已耗尽）。
			retryCtx, retryCancel := context.WithTimeout(ctx, timeout)
			retryCmd := exec.CommandContext(retryCtx, shell, shellArgs...)
			retryCmd.Dir = workdir
			security.ConfigureHiddenCommand(retryCmd)
			var retryOut, retryErrBuf bytes.Buffer
			retryCmd.Stdout, retryCmd.Stderr = &retryOut, &retryErrBuf
			if runErr := retryCmd.Run(); runErr == nil {
				retryCancel()
				r.observeBash(BashDiagnosticEvent{Stage: "bash.docker.retry.ok", Shell: filepath.Base(shell)})
				encoded, _ := json.Marshal(scopedBashResult{
					Stdout: strings.TrimSpace(retryOut.String()), Stderr: strings.TrimSpace(retryErrBuf.String()), ExitCode: 0,
				})
				return string(encoded), nil
			}
			retryCancel()
			r.observeBash(BashDiagnosticEvent{Stage: "bash.docker.retry.failed", Shell: filepath.Base(shell)})
			result.Stderr = strings.TrimSpace(retryErrBuf.String()) + r.deps.DockerHint(nil)
			if retryOut.Len() > 0 {
				result.Stdout = strings.TrimSpace(retryOut.String())
			}
			result.ExitCode = 1
		} else {
			result.Stderr = strings.TrimSpace(result.Stderr) + r.deps.DockerHint(startErr)
			result.ExitCode = 1
		}
	}
	encoded, _ := json.Marshal(result)
	return string(encoded), nil
}

func (r *Router) observeBash(event BashDiagnosticEvent) {
	if r == nil || r.deps.ObserveBash == nil {
		return
	}
	// Diagnostics cannot alter a production tool call, even when a consumer
	// accidentally panics.
	defer func() { _ = recover() }()
	r.deps.ObserveBash(event)
}

// scopedBashCommand chooses a shell that honors the public bash tool's syntax.
// Git for Windows supplies Bash on supported Windows development hosts; using
// PowerShell first would reject ordinary model commands such as "pwd && ls -la".
// Fixed install paths are probed first, then PATH (custom installs such as
// scoop/chocolatey/portable), then PowerShell/cmd as last resort.
func scopedBashCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		for _, bash := range []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files\Git\usr\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
		} {
			if security.FileExists(bash) {
				return bash, []string{"-c", command}
			}
		}
		if bash, err := exec.LookPath("bash"); err == nil {
			return bash, []string{"-c", command}
		}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "bash", []string{"-c", command}
	}
	if powershell := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`; security.FileExists(powershell) {
		return powershell, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	}
	if commandPrompt := `C:\Windows\System32\cmd.exe`; security.FileExists(commandPrompt) {
		return commandPrompt, []string{"/d", "/s", "/c", command}
	}
	return "sh", []string{"-c", command}
}

// scopedToolTimeout 解析 bash 类工具的默认超时：优先显式 tool_call_timeout
// 配置（limits.tool_call_timeout；0 = 无限制）；未配置时兜底 30 分钟
// （旧 30s 兜底会掐断子代理的长命令）。
func (r *Router) scopedToolTimeout(requestedSeconds int) time.Duration {
	timeout := r.deps.ToolCallTimeout
	if timeout <= 0 {
		timeout = time.Duration(r.deps.ToolCallTimeoutSec) * time.Second
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	if requestedSeconds > 0 {
		timeout = time.Duration(requestedSeconds) * time.Second
	}
	return timeout
}

func readFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "start_line": map[string]interface{}{"type": "integer"}, "end_line": map[string]interface{}{"type": "integer"}}, "required": []string{"path"}}
}
func grepSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": map[string]interface{}{"type": "string"}, "path": map[string]interface{}{"type": "string"}, "glob": map[string]interface{}{"type": "string"}, "max_results": map[string]interface{}{"type": "integer"}}, "required": []string{"pattern"}}
}
func globSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": map[string]interface{}{"type": "string"}, "path": map[string]interface{}{"type": "string"}}, "required": []string{"pattern"}}
}
func writeFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}}, "required": []string{"path", "content"}}
}
func editFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "old_string": map[string]interface{}{"type": "string"}, "new_string": map[string]interface{}{"type": "string"}}, "required": []string{"path", "old_string", "new_string"}}
}
func bashSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}, "timeout": map[string]interface{}{"type": "integer"}, "workdir": map[string]interface{}{"type": "string"}}, "required": []string{"command"}}
}
