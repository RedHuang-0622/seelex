// Package terminal 为桌面 GUI 提供下栏终端（VS Code 式面板）的后端会话管理：
// 每个终端 = 一个独立 PTY + 一个子进程，互不共享状态，可多开。
//
// 边界（刻意不做）：
//   - 不解析、不渲染终端协议（ANSI/VT 由前端 xterm.js 负责），本包只搬运字节；
//   - 不进 Snapshot、不进 Agent 工具面：终端是**用户**的本地 shell，模型不可
//     调用（避免把"任意命令执行"塞进 Agent 权限模型）；headless 控制面同样不
//     暴露（见 gui/README.md 的边界说明）；
//   - 不做持久化：进程随宿主退出而结束（终端不是会话数据）。
//
// 生命周期：Open → （Write/Resize 任意次）→ 子进程退出或 Close → 回收。
// 所有输出经 EventHandler 回调外投（Bridge 转成 seelex:terminal 事件）。
//
// 可测性：进程启动被收敛到一个可注入的 launcher（默认走 PTY，见 pty.go），
// 因此注册表/读泵/回收语义可以用假进程离线验证，PTY 本身只由一条集成测试覆盖。
package terminal

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// 默认与边界尺寸：前端 fit 后按真实像素回传，这里只做防御性钳制（PTY 对
// 0/负数尺寸的行为在各平台不一致，钳到可用区间比让内核报错更好）。
const (
	DefaultCols = 80
	DefaultRows = 24
	MinCols     = 2
	MinRows     = 2
	MaxCols     = 1000
	MaxRows     = 1000
	// MaxSessions 是同时存活的终端上限：多开是给人用的，超过这个数一定是
	// 前端泄漏或误用，直接拒绝而不是把进程表撑爆。
	MaxSessions = 16
)

// EventKind 是终端事件类别（前端按 kind 分派，不做字符串前缀猜测）。
type EventKind string

const (
	// KindOutput 是终端输出（Data 为 base64 原始字节）。
	KindOutput EventKind = "output"
	// KindExit 是子进程退出（ExitCode 有效），也是终端唯一的终态事件。
	KindExit EventKind = "exit"
)

// Event 是终端会话向上层外投的一条事件。
type Event struct {
	ID   string    `json:"id"`
	Kind EventKind `json:"kind"`
	// Data 是 base64 编码的原始字节：PTY 读边界可能切断多字节字符，直接以
	// 字符串过 JSON 会把残片替换成 U+FFFD（不可逆），因此字节原样搬运，
	// 由前端解码后交给 xterm（write(Uint8Array) 自带 UTF-8 解码）。
	Data string `json:"data,omitempty"`
	// ExitCode 仅在 KindExit 有效（用户主动关闭时可能是 -1/1，前端只作提示）。
	ExitCode *int `json:"exit_code,omitempty"`
}

// Options 描述一次开终端请求（零值即可用：平台默认 shell + 默认目录）。
type Options struct {
	// Shell 是显式指定的 shell 可执行文件；空 = 按平台默认解析（见 ShellFor）。
	Shell string
	// Args 是显式 shell 的附加参数（Shell 为空时忽略）。
	Args []string
	// Dir 是工作目录（通常为当前会话绑定的工作区根）；不存在时回退进程 cwd。
	Dir string
	// Cols/Rows 是初始终端尺寸（0 用默认值）。
	Cols int
	Rows int
	// Env 是附加环境变量（"K=V" 形式，覆盖同名继承项）。
	Env []string
}

// Info 是终端的可展示元数据（桥接层直接下发，前端据此画标签）。
type Info struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Shell     string    `json:"shell"`
	Args      []string  `json:"args,omitempty"`
	Dir       string    `json:"dir"`
	Cols      int       `json:"cols"`
	Rows      int       `json:"rows"`
	Running   bool      `json:"running"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// EventHandler 接收终端事件（Bridge 注入；必须并发安全，会从读泵 goroutine
// 调用）。
type EventHandler func(Event)

// process 是一台已启动的 shell 的句柄（PTY 或测试假进程）。
type process interface {
	// Read 读终端输出；子进程结束/终端关闭后返回错误（读泵据此收敛）。
	Read([]byte) (int, error)
	// Write 写用户输入。
	Write([]byte) (int, error)
	// Resize 设置终端尺寸（列 × 行）。
	Resize(cols, rows int) error
	// Kill 请求结束子进程（幂等；进程可能已退出）。
	Kill() error
	// Wait 等待子进程结束（幂等；返回的 error 用于折算退出码，见 exitCode）。
	Wait() error
	// Close 释放终端句柄（幂等）：读泵据此从阻塞读里收敛，不再依赖子进程
	// 自己退出后控制台自动断开。
	Close() error
}

// launchRequest 是交给 launcher 的启动参数。
type launchRequest struct {
	Shell string
	Args  []string
	Dir   string
	Env   []string
	Cols  int
	Rows  int
}

// launcher 启动一台 shell 并返回进程句柄（默认 ptyLauncher；测试注入假实现）。
type launcher func(request launchRequest) (process, error)

// Manager 是终端会话注册表（进程级，由 GUI 宿主持有）。
type Manager struct {
	mu       sync.Mutex
	seq      int
	sessions map[string]*session
	handler  EventHandler
	// launch 可注入（测试用假进程）；默认 ptyLauncher。
	launch launcher
	// resolveShell 可注入（测试免依赖真实 shell）；默认 ShellFor。
	resolveShell func(explicit string, args []string) (string, []string, error)
}

// New 创建终端管理器；handler 为 nil 时 Open 会失败（不静默丢输出）。
func New(handler EventHandler) *Manager {
	return &Manager{
		sessions:     map[string]*session{},
		handler:      handler,
		launch:       ptyLauncher,
		resolveShell: ShellFor,
	}
}

// Open 启动一个新终端并返回其元数据（内含分配的终端 ID）。
func (m *Manager) Open(options Options) (Info, error) {
	if m == nil {
		return Info{}, errors.New("terminal: nil manager")
	}
	m.mu.Lock()
	handler := m.handler
	capacity := len(m.sessions) >= MaxSessions
	m.mu.Unlock()
	if handler == nil {
		return Info{}, errors.New("terminal: no event handler bound")
	}
	if capacity {
		return Info{}, fmt.Errorf("terminal: too many sessions (max %d)", MaxSessions)
	}

	shell, args, err := m.resolveShell(strings.TrimSpace(options.Shell), options.Args)
	if err != nil {
		return Info{}, err
	}
	cols, rows := normalizeSize(options.Cols, options.Rows)
	live := &session{
		title: titleFor(shell, 0),
		done:  make(chan struct{}),
	}
	handle, err := m.launch(launchRequest{
		Shell: shell,
		Args:  args,
		Dir:   usableDir(options.Dir),
		Env:   mergeEnv(os.Environ(), options.Env),
		Cols:  cols,
		Rows:  rows,
	})
	if err != nil {
		return Info{}, err
	}

	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("term-%d", m.seq)
	live.id = id
	live.title = titleFor(shell, m.seq)
	live.process = handle
	live.info = Info{
		ID:        id,
		Title:     live.title,
		Shell:     shell,
		Args:      append([]string(nil), args...),
		Dir:       usableDir(options.Dir),
		Cols:      cols,
		Rows:      rows,
		Running:   true,
		StartedAt: time.Now(),
	}
	m.sessions[id] = live
	m.mu.Unlock()

	go live.pump(m)
	return live.snapshot(), nil
}

// Write 把用户输入（原始字节）写入终端。
func (m *Manager) Write(id string, data []byte) error {
	live, err := m.lookup(id)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := live.process.Write(data); err != nil {
		return fmt.Errorf("terminal: write %s: %w", id, err)
	}
	return nil
}

// Resize 调整终端尺寸（前端 fit 后回传）。
func (m *Manager) Resize(id string, cols, rows int) error {
	live, err := m.lookup(id)
	if err != nil {
		return err
	}
	cols, rows = normalizeSize(cols, rows)
	if err := live.process.Resize(cols, rows); err != nil {
		return fmt.Errorf("terminal: resize %s: %w", id, err)
	}
	live.mu.Lock()
	live.info.Cols, live.info.Rows = cols, rows
	live.mu.Unlock()
	return nil
}

// Close 结束终端：先杀子进程再关 PTY，读泵收敛后回收会话。已退出的会话重复
// Close 是幂等的。
func (m *Manager) Close(id string) error {
	live, err := m.lookup(id)
	if err != nil {
		return err
	}
	live.shutdown()
	<-live.done

	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return nil
}

// CloseAll 关闭全部终端（宿主退出路径；不等待读泵，避免退出被慢进程拖住）。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	live := make([]*session, 0, len(m.sessions))
	for _, item := range m.sessions {
		live = append(live, item)
	}
	m.sessions = map[string]*session{}
	m.mu.Unlock()
	for _, item := range live {
		item.shutdown()
	}
}

// List 返回当前存活（含已退出但尚未回收）终端元数据，按创建顺序。
func (m *Manager) List() []Info {
	m.mu.Lock()
	live := make([]*session, 0, len(m.sessions))
	for _, item := range m.sessions {
		live = append(live, item)
	}
	m.mu.Unlock()
	infos := make([]Info, 0, len(live))
	for _, item := range live {
		infos = append(infos, item.snapshot())
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return sequenceOf(infos[i].ID) < sequenceOf(infos[j].ID)
	})
	return infos
}

// Get 返回单个终端元数据。
func (m *Manager) Get(id string) (Info, bool) {
	live, err := m.lookup(id)
	if err != nil {
		return Info{}, false
	}
	return live.snapshot(), true
}

func (m *Manager) lookup(id string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("terminal: unknown session %q", id)
	}
	return live, nil
}

func (m *Manager) emit(event Event) {
	m.mu.Lock()
	handler := m.handler
	m.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}

// session 是一个终端（进程句柄 + 读泵）的运行时状态。
type session struct {
	id      string
	title   string
	process process
	done    chan struct{}

	mu     sync.Mutex
	info   Info
	closed bool
}

func (s *session) snapshot() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := s.info
	copied.Args = append([]string(nil), s.info.Args...)
	return copied
}

// pump 是终端的唯一收敛点，也是 session.done 的唯一关闭者。
//
// 两条独立的收工信号必须都处理，且不能只靠其中一条：
//   - 读端：unix 上子进程退出会让 master 读返回 EIO，读循环自然结束；而
//     Windows ConPTY 下子进程退出**不会**关闭输出管道（伪控制台仍持有写端），
//     读端会一直阻塞，必须由这里主动 Close 逼停。
//   - 子进程：退出码只能在 Wait 之后取得，终端终态以它为准。
//
// 因此顺序是：等子进程 → 关句柄（逼停读端）→ 等输出泵排空 → 外投 exit。
// 读端错误本身不外投：终端的终态由 exit 事件表达，而读端错误的分类在各平台
// 不一致（EIO / file already closed），把平台的怪癖暴露给前端只是噪声。
func (s *session) pump(manager *Manager) {
	defer close(s.done)

	outputs := make(chan struct{})
	go func() {
		defer close(outputs)
		buffer := make([]byte, 32*1024)
		for {
			read, err := s.process.Read(buffer)
			if read > 0 {
				manager.emit(Event{
					ID:   s.id,
					Kind: KindOutput,
					Data: base64.StdEncoding.EncodeToString(buffer[:read]),
				})
			}
			if err != nil {
				return
			}
		}
	}()

	code := s.reap()
	_ = s.process.Close()
	<-outputs
	manager.emit(Event{ID: s.id, Kind: KindExit, ExitCode: &code})
}

// reap 等待子进程结束并把终态写回元数据（Wait 只允许调用一次，调用点唯一）。
func (s *session) reap() int {
	code := exitCodeFromWait(s.process.Wait())
	s.mu.Lock()
	s.info.Running = false
	s.info.ExitCode = &code
	s.mu.Unlock()
	return code
}

// shutdown 杀子进程并释放终端句柄（幂等）；读泵随句柄关闭收敛。
func (s *session) shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	running := s.info.Running
	s.mu.Unlock()

	if running {
		_ = s.process.Kill()
	}
	_ = s.process.Close()
}

// exitCodeFromWait 把 Wait 的返回值折算成退出码：*exec.ExitError 取真实码，
// 正常结束为 0，其余等待失败（进程已被强杀/句柄失效）记 -1。
func exitCodeFromWait(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}

// normalizeSize 把请求尺寸钳到可用区间（0 值走默认）。
func normalizeSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = DefaultCols
	}
	if rows <= 0 {
		rows = DefaultRows
	}
	return clamp(cols, MinCols, MaxCols), clamp(rows, MinRows, MaxRows)
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// usableDir 只在目录真实存在时返回它（工作区未绑定时回退进程 cwd，而不是把
// 无效路径交给 CreateProcess）。
func usableDir(dir string) string {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return ""
	}
	info, err := os.Stat(trimmed)
	if err != nil || !info.IsDir() {
		return ""
	}
	return trimmed
}

// mergeEnv 合并继承环境与附加项（后者覆盖同名键，Windows 下键大小写不敏感）。
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := append([]string(nil), base...)
	index := map[string]int{}
	for position, entry := range merged {
		if name, _, ok := strings.Cut(entry, "="); ok {
			index[strings.ToUpper(name)] = position
		}
	}
	for _, entry := range extra {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if position, found := index[strings.ToUpper(name)]; found {
			merged[position] = entry
			continue
		}
		merged = append(merged, entry)
	}
	return merged
}

// titleFor 给出标签默认标题：shell 基名去扩展名（"C:\...\powershell.exe" →
// "powershell"）；同名多开时前端另有编号，这里只保证标题不空。
func titleFor(shell string, seq int) string {
	name := shellBase(shell)
	if name == "" || name == "." || name == "/" {
		name = fmt.Sprintf("term %d", seq)
	}
	return name
}

// shellBase 取 shell 路径的基名并去掉扩展名。刻意不走 filepath：filepath 按宿主机
// 判定分隔符（Linux 上 "\" 不是分隔符），而同一份断言要在三平台都成立，故先把两种
// 分隔符归一化，再按 POSIX 规则切分。
func shellBase(shell string) string {
	name := path.Base(strings.ReplaceAll(shell, `\`, "/"))
	return strings.TrimSuffix(name, path.Ext(name))
}

// sequenceOf 从 "term-<n>" 取创建序号（解析失败排最前，不 panic）。
func sequenceOf(id string) int {
	value := 0
	_, _ = fmt.Sscanf(id, "term-%d", &value)
	return value
}
