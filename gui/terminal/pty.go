package terminal

import (
	"fmt"
	"sync"

	"github.com/aymanbagabas/go-pty"
)

// ptyProcess 把 go-pty 的「Pty + Cmd」适配成本包的 process 接口：跨平台差异
// （Windows ConPTY / unix 原生 pty）全部由 go-pty 承担，本层只做接口收窄。
//
// Close 必须幂等：读泵收工（子进程退出后主动关句柄）与用户关闭（Close →
// shutdown）会各调一次；Windows 上重复 ClosePseudoConsole 会二次释放同一个
// HPCON——句柄值可能已被系统复用，实测会让宿主进程直接死掉（无输出、退出码
// 1），因此这里用 once 兜住。
type ptyProcess struct {
	session pty.Pty
	command *pty.Cmd

	closeOnce sync.Once
	closeErr  error
}

func (p *ptyProcess) Read(buffer []byte) (int, error) { return p.session.Read(buffer) }

func (p *ptyProcess) Write(data []byte) (int, error) { return p.session.Write(data) }

func (p *ptyProcess) Resize(cols, rows int) error { return p.session.Resize(cols, rows) }

func (p *ptyProcess) Kill() error {
	if p.command.Process == nil {
		return nil
	}
	return p.command.Process.Kill()
}

func (p *ptyProcess) Wait() error { return p.command.Wait() }

func (p *ptyProcess) Close() error {
	p.closeOnce.Do(func() { p.closeErr = p.session.Close() })
	return p.closeErr
}

// ptyLauncher 是默认启动器：真 PTY + 真子进程。找不到 shell、PTY 不可用
// （老 Windows 无 ConPTY）或启动失败都在这里报错，不留给前端一个"空面板"。
func ptyLauncher(request launchRequest) (process, error) {
	session, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("terminal: create pseudo console: %w", err)
	}
	command := session.Command(request.Shell, request.Args...)
	command.Env = request.Env
	command.Dir = request.Dir
	if err := command.Start(); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("terminal: start %s: %w", request.Shell, err)
	}
	// 初始尺寸：失败不致命（默认 80x25 仍可用），前端 fit 后会再报一次真实值。
	_ = session.Resize(request.Cols, request.Rows)
	return &ptyProcess{session: session, command: command}, nil
}
