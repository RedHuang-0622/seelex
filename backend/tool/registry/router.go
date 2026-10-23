package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/tool/io/fs"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
)

// Deps is the runtime callback set injected by the root package (Runtime).
type Deps struct {
	RegisterTool func(name, description string, schema map[string]interface{}, handler func(ctx context.Context, argsJSON string) (string, error))
	ProjectScope *security.ProjectScope
	// SessionKey 从执行 ctx 解析会话键（生产 = telemetry.SessionIDFromContext）。
	// 工具路径根按会话解析：后台/并行会话不得借用视图会话的项目根。
	SessionKey             func(context.Context) string
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
	// AsyncExecEnabled 打开**作业面**（seele.yaml limits.async_exec.enabled，默认
	// false）：bash_bg / read_batch / bg_manage 注册，fork 的 async 模式可用。
	// 关闭时它们都不注册、bash 收到旧入参 background=true 直接报错——能力不可实施
	// 时必须拒绝，不得静默降级成同步执行（security/sandbox.go 同源口径）。
	AsyncExecEnabled bool
	// AsyncBatchID 解析某会话当前的 chat 请求 ID（= 工作表格的批次键）。后台执行
	// 派发时要盖上它，否则投影出来的行会掉进"早期任务"批次里，看不出是谁起的。
	AsyncBatchID func(sessionID string) string
}

// Router registers and executes the project-scoped tool family.
type Router struct {
	deps Deps
	// async 是后台命令登记表（切片关闭时闲置）：句柄表、去重键、输出文件。
	// 装在一次 Runtime 装配的 Router 上（RegisterBuiltins 只调一次）。
	async *asyncRegistry
}

// NewRouter constructs the scoped tool router with injected deps.
func NewRouter(deps Deps) *Router {
	return &Router{deps: deps, async: newAsyncRegistry()}
}

// resolveNodePath 解析工具路径的根（口径见 resolveScopedPath）。
func (r *Router) resolveNodePath(ctx context.Context, path string, forWrite bool) (string, error) {
	return resolveScopedPath(r.deps, ctx, path, forWrite)
}

// resolveScopedPath 是工具路径解析的**唯一实现**（处理器与路径门前置检查共用）：
// worktree 节点（SceneScope.WorkspaceID 指向 worktree 根）→ 节点根；否则按执行
// ctx 的会话键取该会话自己的项目根（会话未绑定时回退进程默认根，保持旧会话语义）。
// worktree 目录由 git 创建（真实目录），resolveInside 内含 withinRoot 校验（越界拒绝）。
func resolveScopedPath(deps Deps, ctx context.Context, path string, forWrite bool) (string, error) {
	if scope, ok := dto.SceneScopeFromContext(ctx); ok && scope.SceneID != "" && scope.WorkspaceID != "" {
		candidate, err := security.ResolveInside(scope.WorkspaceID, path)
		if err != nil {
			return "", err
		}
		return candidate, nil
	}
	sessionKey := security.DefaultScopeKey
	if deps.SessionKey != nil {
		sessionKey = deps.SessionKey(ctx)
	}
	if forWrite {
		return deps.ProjectScope.ResolveWriteFor(sessionKey, path)
	}
	return deps.ProjectScope.ResolveReadFor(sessionKey, path)
}

// sessionKey 返回执行 ctx 的会话键；未注入解析面时返回空键（进程默认根）。
func (r *Router) sessionKey(ctx context.Context) string {
	if r.deps.SessionKey == nil {
		return security.DefaultScopeKey
	}
	return r.deps.SessionKey(ctx)
}

// registerProjectScopedTools overrides the Seele builtin filesystem tools.
// Holder inline providers take precedence over builtin providers with the same
// name, keeping this policy local to Seelex.
func (r *Router) Register() {
	// 名字用本包的常量（与 ProjectScopedToolName 同一份名单）：注册面与"项目作用域已占位"
	// 这条判据必须同源，否则增删一个工具会让两边漂移（见 scoped_names.go）。
	r.deps.RegisterTool(ReadFileToolName, "Read a file inside the bound project.", readFileSchema(), r.scopedReadFile)
	r.deps.RegisterTool(GrepSearchToolName, "Search file contents inside the bound project.", grepSchema(), r.scopedGrep)
	r.deps.RegisterTool(GlobToolName, "Find matching files inside the bound project.", globSchema(), r.scopedGlob)
	r.deps.RegisterTool(WriteFileToolName, "Write a file inside the bound project.", writeFileSchema(), r.scopedWriteFile)
	r.deps.RegisterTool(EditFileToolName, "Edit a file inside the bound project.", editFileSchema(), r.scopedEditFile)
	allowBackground := r.asyncEnabled()
	// bash 工具族（设计文档 §A.4 / 打点 K-3）——两个名字，不是一个名字加开关：
	//   bash      串行、同步、**写类**（保守归类）：保留现状语义，但不再有
	//             background 开关（那一类由 bash_bg 承担，回执形状也不一样）。
	//   bash_bg   后台受管（JobTool.Add 的进程作业面）：派发即返回受理回执。
	//
	// **bash_read 已删除（2026-10-15 用户裁决）**：它是"只读名牌 + 服务端语法判定"的
	// 分裂面，实测 479 次调用里 165 次（34.4%）折在判定上（复合语法 102 / PowerShell
	// 命令面 38 / 白名单外子命令 18 / 其它 7）——模型按"只读 ⇒ bash_read"的常识写命令，
	// 判定按更窄的 POSIX 单命令口径拒绝。判定侧的另一半问题（白名单是 POSIX 词表、而
	// 本机执行壳退化成 PowerShell）意味着这些命令就算放行也会执行失败。命令面的
	// **免打断**由 `bash` 的 allow 模式表承担（见 permission_policy.go 的 bash 模式白名单），
	// 只读文件读取走 read_file / grep_search / glob——工具面因此只剩两个 bash 名字。
	r.deps.RegisterTool(BashToolName, bashDescription(), bashSchema(), r.scopedBash)
	if allowBackground {
		// 作业面只在能力常驻时注册：关闭时它们必然无句柄可取，注册几个只会报错的
		// 工具只是占模型的选项与 token。关就是关（见 Deps.AsyncExecEnabled 注释）。
		r.deps.RegisterTool("bash_bg", bashBgDescription(), bashBgSchema(), r.scopedBashBg)
		r.deps.RegisterTool("read_batch", readBatchDescription(), readBatchSchema(), r.scopedReadBatch)
		r.deps.RegisterTool(bgManageToolName, jobManageDescription(), jobManageSchema(), r.scopedJobManage)
	}
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
	// 远端面：路径是**远端路径**，不做本地根解析（那是本地作用域的判据；远端由对端的
	// 权限面负责，本层不猜）。回执带 execution_surface / env，见远端读的回执注释。
	if transport, ok := RemoteTransport(ctx); ok {
		return readFileRemote(ctx, transport, input)
	}
	path, err := r.resolveNodePath(ctx, input.Path, false)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: read %q: %w", input.Path, err)
	}
	content, _, err := windowLines(string(data), input.StartLine, input.EndLine)
	if err != nil {
		return "", err
	}
	return content, nil
}

// windowLines 是 read_file 的**行窗口判据**（本地面与远端面同一份）。
//
// 行号 1 起、区间左闭右开；end_line 只在"给了、不小于 start、且小于总行数"时收窄
// （与历史行为逐字一致：end_line 超出总行数等于读到底）。
func windowLines(content string, startLine, endLine int) (string, int, error) {
	if startLine <= 0 {
		startLine = 1
	}
	lines := strings.Split(content, "\n")
	start := startLine - 1
	if start >= len(lines) {
		return "", len(lines), fmt.Errorf("read_file: start_line %d exceeds file length %d", startLine, len(lines))
	}
	end := len(lines)
	if endLine > 0 && endLine >= startLine && endLine < end {
		end = endLine
	}
	return strings.Join(lines[start:end], "\n"), len(lines), nil
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
				display, _ := r.deps.ProjectScope.RelativeFor(r.sessionKey(ctx), path)
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
			display, _ := r.deps.ProjectScope.RelativeFor(r.sessionKey(ctx), path)
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
	// 远端面：路径是远端路径（不解析本地根），内容走内容通道（不经 shell 词法）。
	if transport, ok := RemoteTransport(ctx); ok {
		return writeFileRemote(ctx, transport, input)
	}
	path, err := r.resolveNodePath(ctx, input.Path, true)
	if err != nil {
		return "", err
	}
	// 写路径经文件系统 actor（per-path 串行化——并行子代理写同一文件互斥，
	// 见 P0 文件操作 Actor 化设计）。
	if err := r.deps.FileSystem.Write(path, []byte(input.Content)); err != nil {
		return "", fmt.Errorf("write_file: write %q: %w", input.Path, err)
	}
	return receiptJSON("write_file", withExecutionFace(ctx, map[string]any{
		"status": "ok", "path": input.Path, "size": len(input.Content),
	}))
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
	// 远端面：读-替换-写都在对端（替换判据与本地面同源：全部出现处替换 + 计数）。
	if transport, ok := RemoteTransport(ctx); ok {
		return editFileRemote(ctx, transport, input)
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
	return receiptJSON("edit_file", withExecutionFace(ctx, map[string]any{
		"status": "ok", "path": input.Path, "replacements": count,
	}))
}

type scopedBashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	// Stdin 是**命令的 stdin 数据**（与 command 正交）。它存在的理由见
	// TransportRequest.Stdin：数据拼进命令正文就要过 shell 词法与代码页，任何一处
	// 漏转义都是静默改数据；远端面把它当作内容通道原样喂给进程。
	Stdin string `json:"stdin,omitempty"`
	// Background 是**已废弃的旧入参**：它只被兼容解析，用来把老会话/老提示词送来的
	// `background=true` 明确拒掉并指向 bash_bg。保留字段而不是删掉，是为了让这次拒绝
	// 有话说——静默当成同步执行等于把"后台"偷偷变成"前台"。
	Background bool `json:"background,omitempty"`
	// Description 同上：旧 background 形态的行标题，现在只在 bash_bg 里有意义。
	Description string `json:"description,omitempty"`
}
type scopedBashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// BashDiagnosticEvent describes a boundary crossed by scopedBash. It is
// deliberately metadata-only: command text, arguments, working directories,
// output, and account data must never enter diagnostic logs.
//
// 形状的唯一事实源在 `contract/dto`（D-M1-7）：前端诊断面不再为一个只读事件
// 结构直接 import 本包。这里是同身份别名，调用点与签名零改动。
type BashDiagnosticEvent = dto.BashDiagnosticEvent

// BashDiagnosticObserver receives best-effort scoped bash stage events.
// Implementations should return promptly; observer failures are isolated from
// the tool call itself.
type BashDiagnosticObserver = dto.BashDiagnosticObserver

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
	// background 已经搬去 bash_bg。旧入参一律拒绝：静默当成同步执行是"请求的和执行的
	// 不是同一件事"，而静默转后台是"结果突然不在这一轮里"——两条都不能做。
	if input.Background && !r.asyncEnabled() {
		return "", fmt.Errorf("bash: %s", asyncDisabledText)
	}
	if input.Background {
		return "", fmt.Errorf("bash: background=true 已废弃；后台受管命令改用 bash_bg（派发即返回受理回执，取回/终止/销项走 bg_manage）")
	}
	if input.Command == "" {
		return receiptJSON("bash", withExecutionFace(ctx, map[string]any{
			"stdout": "", "stderr": "", "exit_code": 0,
		}))
	}
	if err := r.auditSerialTimeout("bash", input.Timeout); err != nil {
		return "", err
	}
	// 远端面：命令正文与 stdin 数据都走内容通道，workdir 原样交给对端（不本地解析——
	// 命令里的路径改写属权限模式表，见 pathGateTargets 的注释）。
	if transport, ok := RemoteTransport(ctx); ok {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.remote.start", Session: r.sessionKey(ctx)})
		return bashRemote(ctx, transport, input.Command, input.Stdin, input.Workdir, r.scopedToolTimeout(input.Timeout))
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.start"})
	workdir, err := r.resolveNodePath(ctx, input.Workdir, false)
	if err != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.error", Err: err})
		return "", err
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.resolve.done"})
	return r.executeScopedBash(ctx, input.Command, input.Stdin, input.Timeout, workdir)
}

// newExecProcessTree 建"整棵树可终止"的执行域（Windows Job Object / POSIX 进程组）。
//
// 做成变量只为一条用例：**"Job 建不出来"（受限环境 / 组策略）这条退化路径没法在真机上按需
// 复现**（本机建得出来，走不到那一步），而"退化时同步链说什么"正是 U5 要钉的事实。
// 生产路径与既有用例都不碰它；`&security.ProcessTree{}` 就是"Job 没建成"的真实形态。
var newExecProcessTree = security.NewProcessTree

// newProcessTreeCommand 组装一条"整棵树可终止"的命令——**进程树装配只有这一份**：
// 同步链（executeScopedBash 及其重试）与后台链（async_run.startAsync）都转调它。
//
// exec.CommandContext 的默认取消只杀直接子进程（bash/powershell），它 fork 出来的孙
// 进程照活：既继续产出（用户以为停了），又继续持有输出管道，于是 cmd.Wait 要等到孙进程
// 自己退出才返回——"停止工具调用"就变成"点了停止还要再等几十秒"。进程树（Windows
// Job Object / POSIX 进程组）覆盖整棵树，两条链同源。
//
// WaitDelay 是兜底：孙进程握管道而终止实现失效时，收尾仍能在预算内返回，不被按住。
//
// runCtx 由调用方给——**超时/取消策略有意不并**，两者语义不同：
//   - 后台（startAsync）：`context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)`
//     ——受理回执一返回，本次工具调用的 ctx 就失效，沿用它会把刚起的命令连带杀掉；
//   - 同步（executeScopedBash）：`context.WithTimeout(ctx, r.scopedToolTimeout(...))`
//     ——受本次工具调用预算约束、可被"停止"取消。
//
// 装配（树 + 起命令位 + 取消换整组终止 + WaitDelay）没有理由分家，策略有。
func newProcessTreeCommand(runCtx context.Context, shell string, shellArgs []string, workdir string) (*exec.Cmd, *security.ProcessTree) {
	tree := newExecProcessTree()
	cmd := exec.CommandContext(runCtx, shell, shellArgs...)
	// 起命令位与 argv[0] 对齐：exec 把裸名字（POSIX 上的 `bash`）解析成绝对路径写进 Path，
	// 但 Args[0] 仍是调用方给的裸名字。两条链共用这一份装配，就在这里对齐一次——
	// 不留"Windows 恰好一致、Linux 不一致"的平台差（process_tree_assembly_test.go 钉的就是这条）。
	if len(cmd.Args) > 0 {
		cmd.Args[0] = cmd.Path
	}
	winhide.Apply(cmd)
	cmd.Dir = workdir
	security.ConfigureHiddenCommand(cmd)
	security.ConfigureProcessTree(cmd)
	cmd.Cancel = func() error { return tree.Terminate() }
	cmd.WaitDelay = asyncWaitDelay
	return cmd, tree
}

// startWithProcessTree 起命令并把进程挂进树（两条链共用）。挂不上不放弃执行：派发
// 已经发生，退化成"只杀直接子进程"比报错有用，差别由 tree.Degraded() 说得出。
func startWithProcessTree(cmd *exec.Cmd, tree *security.ProcessTree) error {
	if err := cmd.Start(); err != nil {
		tree.Close()
		return err
	}
	if cmd.Process != nil {
		_ = tree.Attach(cmd.Process.Pid)
	}
	return nil
}

// executeScopedBash 是 bash 的同步执行路径：授权与路径解析已经在调用方完成，
// 这里是"已经批准之后的执行"。
func (r *Router) executeScopedBash(ctx context.Context, command, stdinData string, timeoutSec int, workdir string) (string, error) {
	// 执行路径（2026-08-04 回滚）：沙箱接入被怀疑导致工具挂起，恢复 v1
	// 直连 exec（cwd 门禁语义不变）；CommandSandbox 接口保留在 sandbox.go，
	// 待定位挂起根因后再接入（接入时需 fail-fast，不得悄悄降级）。
	shell, shellArgs := scopedBashCommand(command)
	r.observeBash(BashDiagnosticEvent{Stage: "bash.command.prepared", Shell: filepath.Base(shell), Session: r.sessionKey(ctx)})
	timeout := r.scopedToolTimeout(timeoutSec)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, tree := newProcessTreeCommand(runCtx, shell, shellArgs, workdir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// stdin 数据：与命令正文正交（命令走 argv/脚本，数据走管道）。本地与远端同一条
	// 契约；不给数据时不设 Stdin（不是给一个空管道——空管道会让交互式命令立即 EOF）。
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.starting", Shell: filepath.Base(shell), Session: r.sessionKey(ctx)})
	if err := startWithProcessTree(cmd, tree); err != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.process.start.error", Shell: filepath.Base(shell), Session: r.sessionKey(ctx), Err: err})
		return "", fmt.Errorf("bash: %w", err)
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.started", Shell: filepath.Base(shell), Session: r.sessionKey(ctx)})
	// 起命令之后的**唯一一处**退化读数（两条起命令点都读它）：树挂不上时把"终止只及直接
	// 子进程"说出去，否则这件事对调用方不可见（③U5）。
	r.noteProcessTreeDegraded(filepath.Base(shell), tree)
	waitErr := cmd.Wait()
	tree.Close()
	if runCtx.Err() == context.DeadlineExceeded {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.timeout", Shell: filepath.Base(shell), Session: r.sessionKey(ctx), Err: runCtx.Err()})
		return "", fmt.Errorf("bash: timeout after %v", timeout)
	}
	if runCtx.Err() != nil {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.canceled", Shell: filepath.Base(shell), Session: r.sessionKey(ctx), Err: runCtx.Err()})
		return "", fmt.Errorf("bash: %w", runCtx.Err())
	}
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		// 进程自己已退出，只是输出管道被孙进程多握了一会儿：命令成了（与后台执行域同口径）。
		waitErr = nil
	}
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.process.wait.error", Shell: filepath.Base(shell), Session: r.sessionKey(ctx), Err: waitErr})
			return "", fmt.Errorf("bash: %w", waitErr)
		}
	}
	r.observeBash(BashDiagnosticEvent{Stage: "bash.process.exited", Shell: filepath.Base(shell), Session: r.sessionKey(ctx), ExitCode: exitCode})
	result := scopedBashResult{Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String()), ExitCode: exitCode}
	// docker 自动恢复（根治，2026-08-07）：真实环境有 docker CLI 但 Docker
	// Desktop 守护进程未运行时，命令失败并匹配 daemon-down 模式 → 自动启动
	// 守护进程（limits.disable_docker_auto_start 可关）→ 就绪后重跑一次。
	if result.ExitCode != 0 && !r.deps.DisableDockerAutoStart &&
		r.deps.DockerDaemonDown(result.Stdout, result.Stderr) && r.deps.DockerCLIPath() != "" {
		r.observeBash(BashDiagnosticEvent{Stage: "bash.docker.recovery", Shell: filepath.Base(shell), Session: r.sessionKey(ctx)})
		startErr := r.deps.EnsureDocker(ctx)
		if startErr == nil {
			// 重跑（新超时上下文；原 runCtx 可能已耗尽）。
			retryCtx, retryCancel := context.WithTimeout(ctx, timeout)
			retryCmd, retryTree := newProcessTreeCommand(retryCtx, shell, shellArgs, workdir)
			var retryOut, retryErrBuf bytes.Buffer
			retryCmd.Stdout, retryCmd.Stderr = &retryOut, &retryErrBuf
			if stdinData != "" {
				retryCmd.Stdin = strings.NewReader(stdinData)
			}
			runErr := startWithProcessTree(retryCmd, retryTree)
			if runErr == nil {
				r.noteProcessTreeDegraded(filepath.Base(shell), retryTree)
				runErr = retryCmd.Wait()
				retryTree.Close()
				if errors.Is(runErr, exec.ErrWaitDelay) {
					runErr = nil
				}
			}
			retryCancel()
			if runErr == nil {
				r.observeBash(BashDiagnosticEvent{Stage: "bash.docker.retry.ok", Shell: filepath.Base(shell)})
				return bashLocalReceipt(ctx, scopedBashResult{
					Stdout: strings.TrimSpace(retryOut.String()), Stderr: strings.TrimSpace(retryErrBuf.String()), ExitCode: 0,
				})
			}
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
	return bashLocalReceipt(ctx, result)
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

// noteProcessTreeDegraded 是同步链**唯一的退化读数落点**：树挂不上时，"这条命令的终止只及
// 直接子进程"必须说得出口。
//
// 为什么要有它（③U5）：装配早就只有一份（newProcessTreeCommand + startWithProcessTree），
// 但读数只有后台链有——探针把 `Degraded` 折进 `AsyncRunInfo.Degraded`（工作表格那一栏）与
// 观察行"· 进程树挂不上，终止只及直接子进程"（async_probe.go）。同步链过去把树放在局部变量
// 里，从头到尾没人读过 `Degraded()`，于是同一件事对前台调用方**完全不可见**：命令照跑、
// 输出照给，用户不知道"停止"打不到孙进程。
//
// 判据只有一处（`tree.Degraded()`），两条链读的是同一棵树上的同一个事实；后台链不需要这条
// 事件（它的读数比一行日志细），所以这里只服务同步链。不退化时一个字都不说——正常机器上
// Job 建得出来，同步链的诊断阶段序列与从前一致。
func (r *Router) noteProcessTreeDegraded(shell string, tree *security.ProcessTree) {
	if r == nil || tree == nil || !tree.Degraded() {
		return
	}
	r.observeBash(BashDiagnosticEvent{Stage: bashProcessDegradedStage, Shell: shell})
}

// bashProcessDegradedStage 是退化读数的阶段名（一个常量，免得两处起命令点各写一遍字面量）。
const bashProcessDegradedStage = "bash.process.degraded"

// scopedBashCommand 按**装配期解析出的那一份实际壳结论**起命令（见 shell_fact.go）。
//
// Git for Windows supplies Bash on supported Windows development hosts; using
// PowerShell first would reject ordinary model commands such as "pwd && ls -la".
// 探针（固定路径 → PATH → PowerShell/cmd 兜底）只在 ResolveShellFact 里跑一次；这里不再
// 每次调用现算——否则"执行层用哪个壳"与"模型面/权限表以为用的是哪个壳"会各算各的（审计 D4）。
func scopedBashCommand(command string) (string, []string) {
	fact := ResolveShellFact()
	return fact.Path, shellCommandArgs(fact.Kind, command)
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

// ── 串行 bash 的时长预算 ─────────────────────────────────────────────────

// serialBashBudget 是串行 bash 的时长预算：预期跑得更久的命令必须派发到后台作业面
// （bash_bg），而不是把串行入口的 timeout 抬上去。
//
// 为什么这条线是必需的：串行命令占着整个回合——它跑多久，模型就多久做不了别的；
// 而分钟级的构建/测试/安装/下载恰好是后台化最划算的形状（派发即回执、结果分次取回、
// 途中能做别的）。5 分钟是"人已经会去泡杯茶"的量级；这个数字同时出现在工具描述、
// 拒绝消息与系统提示词里，唯一的来源就是这里（见 serialBashBudgetLabel）。
const serialBashBudget = 5 * time.Minute

// serialBashBudgetLabel 把预算写成给模型读的文案（"5 minutes"）。描述、错误消息与
// 系统提示词用的是同一个词，且都从这里派生——数字改了文案跟着改，改不动的只有
// assets/system/instructions.md 里那一处，由 TestSerialBashBudgetAgreesWithPrompt 钉住。
func serialBashBudgetLabel() string {
	return fmt.Sprintf("%d minutes", int(serialBashBudget/time.Minute))
}

// auditSerialTimeout 审查串行入口**声明的**时长：超预算就拒绝，不截断、也不静默跑下去。
//
// 为什么拒绝而不是按预算截断：截断是静默降级——模型以为给了 30 分钟，命令在第 5 分钟
// 被杀，工作白做且没有任何信号说明该换入口；拒绝则把唯一的正确动作（派发到 bash_bg）
// 直接写回给模型。
//
// 只在作业面常驻时审查：作业面关闭（limits.async_exec.enabled=false）时没有 bash_bg
// 可派发，此时拒绝一条长命令等于既不执行也不给替代路径——那时旧口径（显式 timeout
// 说了算）才是诚实的。没声明 timeout 的调用同样不受影响：没有"声明的时长"可审查，
// 长命令该走后台这件事由工具描述与系统提示词负责说明。
func (r *Router) auditSerialTimeout(toolName string, requestedSeconds int) error {
	if requestedSeconds <= 0 || !r.asyncEnabled() {
		return nil
	}
	if time.Duration(requestedSeconds)*time.Second <= serialBashBudget {
		return nil
	}
	return fmt.Errorf("%s: timeout %ds 超过串行预算 %s；预期跑得更久的命令改用 bash_bg 派发"+
		"（后台受管：派发即返回受理回执，取回走 bg_manage(op=fetch, handle)，终止走 op=kill）",
		toolName, requestedSeconds, serialBashBudgetLabel())
}

// bashDescription 说明 bash 的边界：**串行、同步**，写与读都走它（bash_read 已删除）。
//
// 时长边界（"超过预算的命令不属于串行入口"）必须写在描述里：服务端只拦得住显式声明的
// timeout，而没声明的那种正是最容易跑成 30 分钟串行的那一类。
func bashDescription() string {
	return "Run a command with its working directory constrained to the bound project. " +
		"This is not an OS sandbox. It is the one shell entry (read or write): read-only " +
		"inspection commands on the allow-list run without approval, everything else asks. " +
		"It is serial and synchronous — it holds the turn while the " +
		"command runs — and it refuses a `timeout` above its " + serialBashBudgetLabel() +
		" budget: commands expected to take longer belong to bash_bg, whose call returns only " +
		"an acceptance receipt. " +
		"Pass `stdin` for data the command should read on its standard input (byte-transparent); " +
		"never splice data into `command`. " +
		// 实际壳是**装配期解析一次的事实**（见 shell_fact.go）：模型写命令前必须知道
		// 自己写的是哪种方言，否则命令会"过闸即死"（审计 D4）。
		ResolveShellFact().DialectHint()
}

// bashSchema 下发 bash 入参。
func bashSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{"type": "string"},
			"timeout": map[string]interface{}{"type": "integer"},
			"workdir": map[string]interface{}{"type": "string"},
			// stdin 是**数据通道**（与 command 正交）：数据拼进命令正文就要过 shell 词法
			// 与代码页，任何一处漏转义都是静默改数据；这里原样喂给进程的标准输入。
			"stdin": map[string]interface{}{
				"type": "string",
				"description": "Data piped to the command's stdin (byte-transparent). " +
					"Use it instead of splicing data into `command`.",
			},
		},
		"required": []string{"command"},
	}
}
