//go:build windows

package ssh

// auth_agent_windows.go —— Windows 上的 ssh-agent 接入。
//
// Windows 的 OpenSSH agent **不是 unix socket**，而是命名管道
// `\\.\pipe\openssh-ssh-agent`，所以 net.Dial("unix", …) 在这里必然失败。
// 用 x/sys/windows 的 CreateFile 拿一个读写双向句柄再包成 *os.File 即可，
// 因此不需要额外依赖（docs/arch/ssh-tool.md §12 D-3 的默认解）。

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// defaultAgentSocket 返回 Windows OpenSSH agent 的默认管道名。
func defaultAgentSocket() string { return `\\.\pipe\openssh-ssh-agent` }

// dialDefaultAgent 打开平台默认的 agent 通道。
func dialDefaultAgent() (io.ReadWriteCloser, error) { return dialAgent(defaultAgentSocket()) }

// dialAgent 打开指定名字的 agent 通道（Windows：命名管道）。
func dialAgent(name string) (io.ReadWriteCloser, error) {
	if name == "" {
		name = defaultAgentSocket()
	}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("ssh: agent pipe name %q: %w", name, err)
	}
	handle, err := windows.CreateFile(
		ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,   // 不共享：agent 管道按连接独占
		nil, // 默认安全属性
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("ssh: open agent pipe %q (is the ssh-agent service running?): %w", name, err)
	}
	return os.NewFile(uintptr(handle), name), nil
}
