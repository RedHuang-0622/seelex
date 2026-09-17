package terminal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── 假进程与假 launcher（注册表/读泵/回收语义的离线验证面）──────────────

type fakeResize struct {
	Cols int
	Rows int
}

type fakeProcess struct {
	out chan []byte
	// waitDone 在「子进程结束」（Kill/Close/finish 任一）时关闭一次：假进程用它
	// 复刻真实 Wait 的阻塞语义（否则读泵会在 Open 后立刻收敛成 0 退出码）。
	waitDone chan struct{}
	release  sync.Once

	mu      sync.Mutex
	written bytes.Buffer
	resizes []fakeResize
	killed  bool
	closed  bool
	waitErr error
}

func newFakeProcess(chunks ...string) *fakeProcess {
	process := &fakeProcess{out: make(chan []byte, 64), waitDone: make(chan struct{})}
	for _, chunk := range chunks {
		process.out <- []byte(chunk)
	}
	return process
}

func (f *fakeProcess) Read(buffer []byte) (int, error) {
	chunk, ok := <-f.out
	if !ok {
		return 0, io.EOF
	}
	if len(chunk) > len(buffer) {
		return 0, fmt.Errorf("fake process chunk too large: %d > %d", len(chunk), len(buffer))
	}
	return copy(buffer, chunk), nil
}

func (f *fakeProcess) Write(data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	return f.written.Write(data)
}

func (f *fakeProcess) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizes = append(f.resizes, fakeResize{Cols: cols, Rows: rows})
	return nil
}

func (f *fakeProcess) Kill() error {
	f.mu.Lock()
	f.killed = true
	f.mu.Unlock()
	f.release.Do(func() { close(f.waitDone) })
	return nil
}

func (f *fakeProcess) Wait() error {
	<-f.waitDone
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeProcess) Close() error {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		close(f.out)
	}
	f.mu.Unlock()
	f.release.Do(func() { close(f.waitDone) })
	return nil
}

// finish 模拟「shell 自己退出」：写入退出状态并结束读端。
func (f *fakeProcess) finish(waitErr error) {
	f.mu.Lock()
	f.waitErr = waitErr
	f.mu.Unlock()
	_ = f.Close()
}

func (f *fakeProcess) writtenText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.written.String()
}

// eventRecorder 收集终端事件（读泵 goroutine 与测试并发，必须加锁）。
type eventRecorder struct {
	mu     sync.Mutex
	events []Event
	signal chan struct{}
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{signal: make(chan struct{}, 256)}
}

func (r *eventRecorder) handle(event Event) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	select {
	case r.signal <- struct{}{}:
	default:
		// 通知位已满说明测试还没消费：直接丢通知（waitFor 会重新查快照），
		// 绝不能在这里阻塞读泵。
	}
}

func (r *eventRecorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// waitFor 等待满足条件的事件出现（超时即失败）。
func (r *eventRecorder) waitFor(t *testing.T, what string, match func([]Event) bool) []Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if events := r.snapshot(); match(events) {
			return events
		}
		select {
		case <-r.signal:
		case <-deadline:
			t.Fatalf("timed out waiting for %s; events=%+v", what, r.snapshot())
			return nil
		}
	}
}

// fixedShellLauncher 返回一个使用指定假进程的 launcher（不启动真实 shell）。
func fixedShellLauncher(handle *fakeProcess) launcher {
	return func(launchRequest) (process, error) { return handle, nil }
}

func newTestManager(recorder *eventRecorder, process *fakeProcess) *Manager {
	manager := New(recorder.handle)
	manager.launch = fixedShellLauncher(process)
	manager.resolveShell = func(explicit string, args []string) (string, []string, error) {
		shell := explicit
		if shell == "" {
			shell = "fake-shell"
		}
		return shell, args, nil
	}
	return manager
}

// ── 注册表与读泵 ────────────────────────────────────────────────────────

func TestManagerEmitsBase64OutputThenExit(t *testing.T) {
	recorder := newEventRecorder()
	process := newFakeProcess("hello ", "终端\r\n")
	manager := newTestManager(recorder, process)

	info, err := manager.Open(Options{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if info.ID != "term-1" || info.Cols != 100 || info.Rows != 30 || !info.Running {
		t.Fatalf("unexpected info: %+v", info)
	}

	events := recorder.waitFor(t, "both output chunks", func(events []Event) bool {
		outputs := 0
		for _, event := range events {
			if event.Kind == KindOutput {
				outputs++
			}
		}
		return outputs >= 2
	})
	decoded := ""
	for _, event := range events {
		if event.Kind != KindOutput {
			continue
		}
		bytes, err := base64.StdEncoding.DecodeString(event.Data)
		if err != nil {
			t.Fatalf("output is not base64: %v", err)
		}
		decoded += string(bytes)
	}
	if decoded != "hello 终端\r\n" {
		t.Fatalf("decoded output = %q", decoded)
	}

	process.finish(errors.New("shell exited"))
	events = recorder.waitFor(t, "exit event", func(events []Event) bool {
		for _, event := range events {
			if event.Kind == KindExit {
				return true
			}
		}
		return false
	})
	var exit *Event
	for index := range events {
		if events[index].Kind == KindExit {
			exit = &events[index]
		}
	}
	if exit == nil || exit.ExitCode == nil || *exit.ExitCode != -1 {
		t.Fatalf("unexpected exit event: %+v", exit)
	}
	if info, ok := manager.Get("term-1"); !ok || info.Running {
		t.Fatalf("session must stop running after exit: %+v ok=%v", info, ok)
	}
	if err := manager.Close("term-1"); err != nil {
		t.Fatalf("Close after exit must be idempotent: %v", err)
	}
	if _, ok := manager.Get("term-1"); ok {
		t.Fatal("closed session must be removed from the registry")
	}
}

func TestManagerKeepsCreationOrderAndRejectsUnknownSession(t *testing.T) {
	recorder := newEventRecorder()
	manager := New(recorder.handle)
	manager.resolveShell = func(explicit string, args []string) (string, []string, error) {
		return "fake-shell", args, nil
	}
	manager.launch = func(launchRequest) (process, error) { return newFakeProcess(), nil }

	first, err := manager.Open(Options{})
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	second, err := manager.Open(Options{})
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	listed := manager.List()
	if len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("List must follow creation order: %+v", listed)
	}

	if err := manager.Write("term-404", []byte("x")); err == nil {
		t.Fatal("Write on unknown session must fail")
	}
	if err := manager.Resize("term-404", 10, 10); err == nil {
		t.Fatal("Resize on unknown session must fail")
	}
	if err := manager.Close("term-404"); err == nil {
		t.Fatal("Close on unknown session must fail")
	}

	if err := manager.Resize(second.ID, 0, 0); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if info, _ := manager.Get(second.ID); info.Cols != DefaultCols || info.Rows != DefaultRows {
		t.Fatalf("zero size must fall back to defaults: %+v", info)
	}
	if err := manager.Close(first.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if listed := manager.List(); len(listed) != 1 || listed[0].ID != second.ID {
		t.Fatalf("closed session must leave the registry: %+v", listed)
	}
	manager.CloseAll()
	if listed := manager.List(); len(listed) != 0 {
		t.Fatalf("CloseAll must empty the registry: %+v", listed)
	}
}

func TestManagerWritesInputAndKillsOnClose(t *testing.T) {
	recorder := newEventRecorder()
	process := newFakeProcess()
	manager := newTestManager(recorder, process)

	info, err := manager.Open(Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := manager.Write(info.ID, []byte("echo hi\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := process.writtenText(); got != "echo hi\r\n" {
		t.Fatalf("written = %q", got)
	}
	if err := manager.Close(info.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	process.mu.Lock()
	killed, closed := process.killed, process.closed
	process.mu.Unlock()
	if !killed || !closed {
		t.Fatalf("Close must kill and release the terminal: killed=%v closed=%v", killed, closed)
	}
}

func TestManagerRejectsOpenWithoutHandlerAndOverLimit(t *testing.T) {
	manager := New(nil)
	manager.launch = func(launchRequest) (process, error) { return newFakeProcess(), nil }
	manager.resolveShell = func(explicit string, args []string) (string, []string, error) {
		return "fake-shell", nil, nil
	}
	if _, err := manager.Open(Options{}); err == nil {
		t.Fatal("Open without an event handler must fail")
	}

	recorder := newEventRecorder()
	manager = newTestManager(recorder, newFakeProcess())
	opened := make([]string, 0, MaxSessions)
	for index := 0; index < MaxSessions; index++ {
		info, err := manager.Open(Options{})
		if err != nil {
			t.Fatalf("Open #%d: %v", index, err)
		}
		opened = append(opened, info.ID)
	}
	if _, err := manager.Open(Options{}); err == nil {
		t.Fatalf("Open beyond %d sessions must fail", MaxSessions)
	}
	for _, id := range opened {
		if err := manager.Close(id); err != nil {
			t.Fatalf("Close %s: %v", id, err)
		}
	}
}

func TestManagerShellFailureIsReported(t *testing.T) {
	recorder := newEventRecorder()
	manager := New(recorder.handle)
	manager.resolveShell = func(explicit string, args []string) (string, []string, error) {
		return "", nil, errors.New("terminal: shell \"nope\" not found")
	}
	if _, err := manager.Open(Options{Shell: "nope"}); err == nil {
		t.Fatal("Open with a missing shell must fail instead of opening an empty terminal")
	}

	manager.resolveShell = func(explicit string, args []string) (string, []string, error) {
		return "fake-shell", nil, nil
	}
	manager.launch = func(launchRequest) (process, error) { return nil, errors.New("boom") }
	if _, err := manager.Open(Options{}); err == nil {
		t.Fatal("launcher failure must surface")
	}
	if listed := manager.List(); len(listed) != 0 {
		t.Fatalf("failed open must not register a session: %+v", listed)
	}
}

// ── 纯函数 ──────────────────────────────────────────────────────────────

func TestNormalizeSize(t *testing.T) {
	cases := []struct {
		inCols, inRows     int
		wantCols, wantRows int
	}{
		{0, 0, DefaultCols, DefaultRows},
		{-5, -5, DefaultCols, DefaultRows},
		{1, 1, MinCols, MinRows},
		{120, 40, 120, 40},
		{MaxCols + 100, MaxRows + 100, MaxCols, MaxRows},
	}
	for _, item := range cases {
		cols, rows := normalizeSize(item.inCols, item.inRows)
		if cols != item.wantCols || rows != item.wantRows {
			t.Errorf("normalizeSize(%d,%d) = (%d,%d), want (%d,%d)",
				item.inCols, item.inRows, cols, rows, item.wantCols, item.wantRows)
		}
	}
}

func TestMergeEnvOverridesInPlace(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/root"}
	merged := mergeEnv(base, []string{"HOME=/home/seelex", "TERM=xterm-256color"})
	if len(merged) != 3 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0] != "PATH=/bin" || merged[1] != "HOME=/home/seelex" || merged[2] != "TERM=xterm-256color" {
		t.Fatalf("override must be in place and append new keys: %+v", merged)
	}
	if got := mergeEnv(base, nil); len(got) != 2 {
		t.Fatalf("empty extra must keep base untouched: %+v", got)
	}
}

func TestUsableDirRequiresRealDirectory(t *testing.T) {
	if got := usableDir(""); got != "" {
		t.Fatalf("empty dir = %q", got)
	}
	if got := usableDir("   "); got != "" {
		t.Fatalf("blank dir = %q", got)
	}
	missing := "definitely-not-a-directory-seelex-test"
	if got := usableDir(missing); got != "" {
		t.Fatalf("missing dir must fall back to cwd: %q", got)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := usableDir(cwd); got != cwd {
		t.Fatalf("usableDir(cwd) = %q, want %q", got, cwd)
	}
}

func TestTitleForStripsExtension(t *testing.T) {
	if got := titleFor("/usr/bin/bash", 1); got != "bash" {
		t.Fatalf("titleFor(bash) = %q", got)
	}
	if got := titleFor("C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", 2); got != "powershell" {
		t.Fatalf("titleFor(powershell) = %q", got)
	}
	if got := titleFor("", 3); got != "term 3" {
		t.Fatalf("titleFor(empty) = %q", got)
	}
}

func TestShellForRejectsUnknownExplicitShell(t *testing.T) {
	if _, _, err := ShellFor("seelex-definitely-missing-shell", nil); err == nil {
		t.Fatal("unknown explicit shell must be rejected")
	}
}

func TestShellForHonoursEnvOverride(t *testing.T) {
	shell := hostShellName()
	if shell == "" {
		t.Skip("no shell available on this host")
	}
	t.Setenv(ShellEnvName, shell)
	path, _, err := ShellFor("", nil)
	if err != nil {
		t.Fatalf("ShellFor with env override: %v", err)
	}
	if !strings.Contains(strings.ToLower(path), strings.ToLower(strings.TrimSuffix(shell, ".exe"))) {
		t.Fatalf("resolved %q does not look like %q", path, shell)
	}
}

func TestShellForDefaultResolutionOnHost(t *testing.T) {
	path, args, err := ShellFor("", nil)
	if err != nil {
		t.Skipf("no shell available: %v", err)
	}
	if strings.TrimSpace(path) == "" {
		t.Fatal("resolved shell must not be empty")
	}
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(path), "powershell.exe") && len(args) != 1 {
		t.Fatalf("powershell must run with -NoLogo, got %+v", args)
	}
}

// ── 真机集成（真 PTY + 真 shell）────────────────────────────────────────

// hostShellName 返回本机可用的 shell 名（找不到时跳过集成用例）。
func hostShellName() string {
	candidates := []string{"pwsh.exe", "powershell.exe", "cmd.exe"}
	if runtime.GOOS != "windows" {
		candidates = []string{"/bin/bash", "/bin/sh"}
	}
	for _, candidate := range candidates {
		if _, _, err := ShellFor(candidate, nil); err == nil {
			return candidate
		}
	}
	return ""
}

// TestManagerRealShellRoundTrip 是唯一一条真机用例：真 PTY + 真 shell，验证
// 「先出提示符/横幅（未输入前就有输出）→ 输入行回显 → exit 退出码 0」。
func TestManagerRealShellRoundTrip(t *testing.T) {
	if hostShellName() == "" {
		t.Skip("no shell available on this host")
	}
	recorder := newEventRecorder()
	manager := New(recorder.handle)
	manager.resolveShell = ShellFor

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	info, err := manager.Open(Options{Dir: dir, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer manager.CloseAll()
	if info.Dir != dir {
		t.Fatalf("terminal must start in the requested dir: %+v", info)
	}

	// 1. 未输入任何东西前就该有输出（提示符或横幅）：证明 PTY 真的活着。
	recorder.waitFor(t, "shell output before input", func(events []Event) bool {
		return len(outputText(events)) > 0
	})

	// 2. 输入一行并看到回显（终端回显即往返成功）。注意只发 `\r`：xterm 的
	//    Enter 也是 `\r`，而 PowerShell 的 PSReadLine 把 `\n`（Ctrl+J）绑成
	//    「续行」——多发一个 `\n` 会把 shell 推进 `>>` 续行态（实测）。
	marker := fmt.Sprintf("seelex-terminal-probe-%d", time.Now().UnixNano())
	if err := manager.Write(info.ID, []byte("echo "+marker+"\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	recorder.waitFor(t, "echoed marker", func(events []Event) bool {
		return strings.Contains(outputText(events), marker)
	})

	// 3. `exit` 退出后必须收到 exit 事件且退出码为 0。
	if err := manager.Write(info.ID, []byte("exit\r")); err != nil {
		t.Fatalf("Write exit: %v", err)
	}
	events := recorder.waitFor(t, "exit event", func(events []Event) bool {
		for _, event := range events {
			if event.Kind == KindExit && event.ExitCode != nil && *event.ExitCode == 0 {
				return true
			}
		}
		return false
	})
	if len(events) == 0 {
		t.Fatal("no events recorded")
	}
}

func outputText(events []Event) string {
	var builder strings.Builder
	for _, event := range events {
		if event.Kind != KindOutput {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(event.Data)
		if err != nil {
			continue
		}
		builder.Write(decoded)
	}
	return builder.String()
}
