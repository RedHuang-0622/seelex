//go:build !windows

package security

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
)

// 进程树终止（POSIX）：Setpgid 让执行体自成进程组，终止 = 对负 PID 发 SIGKILL，
// 组内所有成员一起走。
//
// 与 Windows 变体同签名，调用方（seelebridge/tools 的后台执行域）不需要分平台。

const processTreeKillExitCode = 137

var (
	errNoProcessTree = errors.New("security: 进程树未绑定任何进程")
)

// ConfigureProcessTree 让子进程自成进程组（Setpgid）——负 PID 终止的前提。
//
// 只补自己那一位：SysProcAttr 可能有别的写者，整体赋值会抹掉别人的设置。
func ConfigureProcessTree(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// ProcessTree 是一次执行体的可终止域（POSIX 上就是那个进程组）。
type ProcessTree struct {
	mu  sync.Mutex
	pid int
}

// NewProcessTree 无需系统资源，只是记下稍后绑定的进程组号。
func NewProcessTree() *ProcessTree {
	return &ProcessTree{}
}

// Degraded 在 POSIX 上恒为 false：Setpgid + kill(-pgid) 覆盖整棵树，没有"挂不上"这一形态
// （Windows 的 Job 才可能建不出来或分配失败——见同目录 windows 变体与 U5 残②）。
func (t *ProcessTree) Degraded() bool { return false }

// Attach 绑定进程组号（= 首进程 PID，因为 Setpgid）。
func (t *ProcessTree) Attach(pid int) error {
	if t == nil || pid <= 0 {
		return errNoProcessTree
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pid = pid
	return nil
}

// Terminate 对整个进程组发 SIGKILL。ESRCH（组已经没了）算良性。
// 返回其它错误 = 没能确认终止，调用方不得谎报已杀。
func (t *ProcessTree) Terminate() error {
	if t == nil {
		return errNoProcessTree
	}
	t.mu.Lock()
	pid := t.pid
	t.mu.Unlock()
	if pid <= 0 {
		return errNoProcessTree
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

// Close 没有系统资源要释放，保持与 Windows 变体同生命周期形状。
func (t *ProcessTree) Close() {}
