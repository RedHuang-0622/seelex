package main

// main.go —— MVP 的驱动：跑九组场景，全 PASS 才 exit 0。
//
// 它回答的问题（对应 docs/arch/ssh-tool-transport.md §7 的判据）：
//   S1 同一份 handler 在本地/远端两面结果一致（A1/A2）
//   S2 远端写读往返，中文与换行不掉字节
//   S3 edit 的唯一串判据两面共用（A2）
//   S4 远端退出码非零是 completed，不是失败
//   S5 远端路径不存在 → SSH_PATH_NOT_FOUND（A6）
//   S6 选择矩阵六格（A3）
//   S7 channel 超时不是失败（A7）
//   S8 责任链顺序与短路
//   S9 显式指名不降级（A4）

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

const fixturePassword = "mvp-pw"

type demo struct {
	local *LocalTransport
	ssh   *SSHTransport
	srv   *fixtureServer
	// short 是一条"通道超时压到 300ms"的远端面，用于演示超时不是失败。
	short *SSHTransport
	spec  ssh.Spec
}

func main() {
	ctx := context.Background()
	d, err := setup(ctx)
	if err != nil {
		fmt.Printf("SETUP FAILED: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if d.ssh != nil {
			_ = d.ssh.client.Close()
		}
		if d.short != nil {
			_ = d.short.client.Close()
		}
		d.srv.Close()
	}()

	scenarios := []struct {
		name string
		run  func(context.Context, *demo) error
	}{
		{"S1 同一 handler 两面结果一致", scenarioSameHandlerBothFaces},
		{"S2 远端写读往返（中文/换行不掉字节）", scenarioWriteReadRoundTrip},
		{"S3 edit 判据两面共用", scenarioEditSharesRule},
		{"S4 远端非零退出码是 completed", scenarioExitCodeIsNotFailure},
		{"S5 远端路径不存在 → SSH_PATH_NOT_FOUND", scenarioRemotePathNotFound},
		{"S6 选择矩阵六格 + 显式优先", scenarioSelectionMatrix},
		{"S7 channel 超时不是失败", scenarioChannelTimeout},
		{"S8 责任链顺序与短路", scenarioChainOrderAndShortCircuit},
		{"S9 显式指名 ssh 不降级", scenarioExplicitNeverDegrades},
	}

	failed := 0
	for _, scenario := range scenarios {
		if err := scenario.run(ctx, d); err != nil {
			failed++
			fmt.Printf("[FAIL] %s\n        %v\n", scenario.name, err)
			continue
		}
		fmt.Printf("[PASS] %s\n", scenario.name)
	}
	fmt.Printf("\n%d/%d 场景通过\n", len(scenarios)-failed, len(scenarios))
	if failed > 0 {
		os.Exit(1)
	}
}

// setup 起本地根 + 进程内 sshd，并用**探针**确认连通后才拿到远端执行面。
func setup(ctx context.Context) (*demo, error) {
	root := filepath.Join(os.TempDir(), fmt.Sprintf("seelex-mvp-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	const seed = "alpha beta gamma\n第二行中文\n"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(seed), 0o644); err != nil {
		return nil, err
	}

	srv, err := StartFixture(fixturePassword, map[string]string{"notes.txt": seed})
	if err != nil {
		return nil, err
	}

	// 首次信任：把夹具指纹写进临时 known_hosts（等价于"人确认过指纹"）。
	khDir := filepath.Join(root, ".kh")
	if err := os.MkdirAll(khDir, 0o755); err != nil {
		return nil, err
	}
	khPath := filepath.Join(khDir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		return nil, err
	}

	host, portStr, err := net.SplitHostPort(srv.Addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	spec := ssh.Spec{
		ID:       "fixture",
		Hostname: host,
		Port:     port,
		User:     "tester",
		Auth:     ssh.AuthSpec{Mode: ssh.AuthPassword, Password: fixturePassword},
		KnownHosts: ssh.KnownHostsSpec{
			Files: []string{khPath},
			Mode:  ssh.KnownHostsStrict,
		},
		Timeouts: ssh.Timeouts{
			Connect: 3 * time.Second,
			Banner:  3 * time.Second,
			Auth:    3 * time.Second,
			Channel: 10 * time.Second,
		},
	}

	local := NewLocalTransport(root)
	RegisterTransport(local)

	sshTransport, decision, err := DialSSH(ctx, spec)
	if err != nil {
		srv.Close()
		return nil, fmt.Errorf("dial ssh: %w (decision=%+v)", err, decision)
	}
	RegisterTransport(sshTransport)

	shortSpec := spec
	shortSpec.Timeouts.Channel = 300 * time.Millisecond
	shortClient, err := ssh.Dial(ctx, shortSpec)
	if err != nil {
		srv.Close()
		return nil, fmt.Errorf("dial short-channel ssh: %w", err)
	}

	return &demo{
		local: local,
		ssh:   sshTransport,
		srv:   srv,
		short: NewSSHTransport(shortClient, shortSpec),
		spec:  spec,
	}, nil
}

// call 走完整责任链（审计 → 权限 → 选择 → 终端）。
func (d *demo) call(ctx context.Context, state ProjectState, explicit string, tool string, args map[string]string) (Result, []string, int, error) {
	trace := make([]string, 0, 4)
	terminalCalls := 0
	terminal := TerminalHandler()
	counting := func(inner context.Context, c Call) (Result, error) {
		terminalCalls++
		return terminal(inner, c)
	}
	handler := Compose(
		AuditMiddleware(&trace),
		PermissionMiddleware(func(string) bool { return true }),
		SelectorMiddleware(state, explicit),
	)(Meta{Name: tool, Eligible: true}, counting)
	res, err := handler(ctx, Call{Name: tool, Args: args})
	return res, trace, terminalCalls, err
}

// readingState 是"已绑定 + 有端点 + 已启用 + 探针通过"的状态。
func readingState() ProjectState {
	return ProjectState{Bound: true, HasEndpoint: true, SSHEnabled: true, ProbeClass: "ok"}
}

// ── 场景 ────────────────────────────────────────────────────────────

func scenarioSameHandlerBothFaces(ctx context.Context, d *demo) error {
	localRes, _, _, err := d.call(ctx, readingState(), "local", "read_file", map[string]string{"path": "notes.txt"})
	if err != nil {
		return fmt.Errorf("local read: %w", err)
	}
	sshRes, _, _, err := d.call(ctx, readingState(), "ssh", "read_file", map[string]string{"path": "notes.txt"})
	if err != nil {
		return fmt.Errorf("ssh read: %w", err)
	}
	localContent, ok := localRes.Value.(Content)
	if !ok {
		return fmt.Errorf("local value type = %T", localRes.Value)
	}
	sshContent, ok := sshRes.Value.(Content)
	if !ok {
		return fmt.Errorf("ssh value type = %T", sshRes.Value)
	}
	if localContent.Text != sshContent.Text {
		return fmt.Errorf("两面内容不一致：local=%q ssh=%q", localContent.Text, sshContent.Text)
	}
	if localRes.Impl != "local" || sshRes.Impl != "ssh" {
		return fmt.Errorf("impl 标注错：local=%q ssh=%q", localRes.Impl, sshRes.Impl)
	}
	return nil
}

func scenarioWriteReadRoundTrip(ctx context.Context, d *demo) error {
	const content = "第一行：中文\n第二行\ttab\n"
	if _, _, _, err := d.call(ctx, readingState(), "ssh", "write_file",
		map[string]string{"path": "out/新文件.txt", "content": content}); err != nil {
		return fmt.Errorf("ssh write: %w", err)
	}
	res, _, _, err := d.call(ctx, readingState(), "ssh", "read_file", map[string]string{"path": "out/新文件.txt"})
	if err != nil {
		return fmt.Errorf("ssh read back: %w", err)
	}
	content2 := res.Value.(Content)
	if content2.Text != content {
		return fmt.Errorf("往返丢字节：got %q want %q", content2.Text, content)
	}
	if stored, _ := d.srv.File("out/新文件.txt"); stored != content {
		return fmt.Errorf("远端落盘内容不符：%q", stored)
	}
	return nil
}

func scenarioEditSharesRule(ctx context.Context, d *demo) error {
	if _, _, _, err := d.call(ctx, readingState(), "ssh", "write_file",
		map[string]string{"path": "edit.txt", "content": "alpha beta gamma"}); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	res, _, _, err := d.call(ctx, readingState(), "ssh", "edit_file", map[string]string{
		"path": "edit.txt", "old_text": "beta", "new_text": "BETA",
	})
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}
	edited := res.Value.(EditResult)
	if edited.Replaced != 1 {
		return fmt.Errorf("replaced = %d, want 1", edited.Replaced)
	}
	// 同一个片段再改一次：远端应当与原于本地的同一份判据给出"未出现"。
	_, _, _, err = d.call(ctx, readingState(), "ssh", "edit_file", map[string]string{
		"path": "edit.txt", "old_text": "beta", "new_text": "BETA2",
	})
	if err == nil {
		return fmt.Errorf("第二次替换本应因原文不存在而失败")
	}
	if code := codeForError(err); code != CodeBadArg {
		return fmt.Errorf("错误码 = %s, want %s（说明两面没用同一份判据）", code, CodeBadArg)
	}
	return nil
}

func scenarioExitCodeIsNotFailure(ctx context.Context, d *demo) error {
	for _, face := range []string{"local", "ssh"} {
		res, _, _, err := d.call(ctx, readingState(), face, "bash", map[string]string{"script": "exit 3"})
		if err != nil {
			return fmt.Errorf("%s bash: %w", face, err)
		}
		execRes := res.Value.(ExecResult)
		if execRes.Status != "completed" {
			return fmt.Errorf("%s status = %q, want completed", face, execRes.Status)
		}
		if execRes.ExitCode != 3 {
			return fmt.Errorf("%s exit code = %d, want 3", face, execRes.ExitCode)
		}
	}
	return nil
}

func scenarioRemotePathNotFound(ctx context.Context, d *demo) error {
	_, _, _, err := d.call(ctx, readingState(), "ssh", "read_file", map[string]string{"path": "no/such.txt"})
	if err == nil {
		return fmt.Errorf("读不存在的远端文件本应报错")
	}
	if code := codeForError(err); code != CodeSSHPathNotFound {
		return fmt.Errorf("错误码 = %s, want %s", code, CodeSSHPathNotFound)
	}
	return nil
}

func scenarioSelectionMatrix(ctx context.Context, d *demo) error {
	cases := []struct {
		name      string
		state     ProjectState
		explicit  string
		want      string
		degraded  bool
		wantError bool
	}{
		{"未绑定项目", ProjectState{}, "", "local", false, false},
		{"无端点", ProjectState{Bound: true}, "", "local", false, false},
		{"有端点未启用", ProjectState{Bound: true, HasEndpoint: true}, "", "local", false, false},
		{"启用+探针通过", ProjectState{Bound: true, HasEndpoint: true, SSHEnabled: true, ProbeClass: "ok"}, "", "ssh", false, false},
		{"启用+未探过", ProjectState{Bound: true, HasEndpoint: true, SSHEnabled: true}, "", "local", true, false},
		{"启用+探针失败", ProjectState{Bound: true, HasEndpoint: true, SSHEnabled: true, ProbeClass: "auth_rejected"}, "", "local", true, false},
		{"显式 local", readingState(), "local", "local", false, false},
		{"显式 ssh", readingState(), "ssh", "ssh", false, false},
		{"显式 ssh 但未启用", ProjectState{Bound: true, HasEndpoint: true}, "ssh", "", false, true},
		{"显式 ssh 但无端点", ProjectState{Bound: true}, "ssh", "", false, true},
	}
	for _, tc := range cases {
		decision, err := Resolve(tc.state, tc.explicit)
		if tc.wantError {
			if err == nil {
				return fmt.Errorf("%s：应当报错却返回 %+v", tc.name, decision)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("%s：不应报错：%w", tc.name, err)
		}
		if decision.Transport != tc.want {
			return fmt.Errorf("%s：transport = %q, want %q", tc.name, decision.Transport, tc.want)
		}
		if decision.Degraded != tc.degraded {
			return fmt.Errorf("%s：degraded = %v, want %v", tc.name, decision.Degraded, tc.degraded)
		}
		// A5：降级必须留痕
		if decision.Degraded && decision.Detail == "" {
			return fmt.Errorf("%s：降级却没有 detail，事后无法归因", tc.name)
		}
	}
	return nil
}

func scenarioChannelTimeout(ctx context.Context, d *demo) error {
	execRes, err := d.short.Exec(ctx, "sleep 5")
	if err != nil {
		return fmt.Errorf("channel 超时不该返回 error（它不是失败）：%w", err)
	}
	if execRes.Status != string(ssh.StatusChannelTimeout) {
		return fmt.Errorf("status = %q, want %q", execRes.Status, ssh.StatusChannelTimeout)
	}
	if !execRes.StillRunning {
		return fmt.Errorf("still_running 应为 true（本端不等待，远端可能仍在跑）")
	}
	return nil
}

func scenarioChainOrderAndShortCircuit(ctx context.Context, d *demo) error {
	// 放行：审计 enter → 选择 → 终端 → 审计 exit
	trace := make([]string, 0, 4)
	terminalCalls := 0
	terminal := TerminalHandler()
	counting := func(inner context.Context, c Call) (Result, error) {
		terminalCalls++
		return terminal(inner, c)
	}
	allowed := Compose(
		AuditMiddleware(&trace),
		PermissionMiddleware(func(string) bool { return true }),
		SelectorMiddleware(readingState(), "local"),
	)(Meta{Name: "read_file", Eligible: true}, counting)
	if _, err := allowed(ctx, Call{Name: "read_file", Args: map[string]string{"path": "notes.txt"}}); err != nil {
		return fmt.Errorf("放行链：%w", err)
	}
	if len(trace) != 2 || trace[0] != "enter:read_file" || trace[1] != "exit:read_file" {
		return fmt.Errorf("放行链顺序不对：%v", trace)
	}
	if terminalCalls != 1 {
		return fmt.Errorf("终端调用次数 = %d, want 1", terminalCalls)
	}

	// 拒绝：进入审计后立刻短路，**不进**选择器与终端
	trace = trace[:0]
	terminalCalls = 0
	denied := Compose(
		AuditMiddleware(&trace),
		PermissionMiddleware(func(string) bool { return false }),
		SelectorMiddleware(readingState(), "local"),
	)(Meta{Name: "read_file", Eligible: true}, counting)
	_, err := denied(ctx, Call{Name: "read_file", Args: map[string]string{"path": "notes.txt"}})
	if err == nil {
		return fmt.Errorf("拒绝链本应报错")
	}
	if terminalCalls != 0 {
		return fmt.Errorf("短路失败：终端被调用了 %d 次", terminalCalls)
	}
	if len(trace) != 2 || trace[1] != "exit:read_file:err" {
		return fmt.Errorf("拒绝链审计轨迹不对：%v", trace)
	}
	return nil
}

func scenarioExplicitNeverDegrades(ctx context.Context, d *demo) error {
	// 显式要 ssh，但项目未启用 SSH：必须报错，而不是偷偷回落本机。
	_, _, _, err := d.call(ctx, ProjectState{Bound: true, HasEndpoint: true}, "ssh", "read_file",
		map[string]string{"path": "notes.txt"})
	if err == nil {
		return fmt.Errorf("显式 ssh + 未启用：本应报错，却成功返回（说明发生了静默降级）")
	}
	return nil
}
