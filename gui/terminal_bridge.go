package gui

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/gui/terminal"
)

// terminalEventName 是终端输出/退出事件的桌面事件名。它独立于 seelex:event
// （应用事件流）：终端不是会话状态，不进 Snapshot、不参与 delivery_seq 水位与
// 重推，前端只按 id 增量追加到对应 xterm 实例。
const terminalEventName = "seelex:terminal"

// TerminalSession 是终端会话的桥接投影（前端标签数据源）。
type TerminalSession = terminal.Info

// TerminalOpenOptions 是前端开终端时可选的参数（零值 = 平台默认 shell +
// 当前会话工作区目录）。
type TerminalOpenOptions struct {
	Shell string   `json:"shell,omitempty"`
	Args  []string `json:"args,omitempty"`
	Dir   string   `json:"dir,omitempty"`
	Cols  int      `json:"cols,omitempty"`
	Rows  int      `json:"rows,omitempty"`
}

// TerminalOpen 打开一个新终端（下栏面板的「+」）并返回会话元数据。
//
// 工作目录以**后端当前工作区**为准（前端只在未绑定时回退进程项目根）：
// 终端的 cwd 属于后端事实，不由渲染层指定路径，避免前端传入任意目录。
func (bridge *Bridge) TerminalOpen(options TerminalOpenOptions) (TerminalSession, error) {
	manager := bridge.terminalManager()
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = bridge.terminalDir()
	}
	info, err := manager.Open(terminal.Options{
		Shell: options.Shell,
		Args:  options.Args,
		Dir:   dir,
		Cols:  options.Cols,
		Rows:  options.Rows,
	})
	if err != nil {
		return TerminalSession{}, err
	}
	return info, nil
}

// TerminalWrite 把用户输入写入终端；data 是 base64 编码的原始字节（与输出
// 事件同一套编码，前端 xterm 的 onData 直接编码即可，不做字符串转义假设）。
func (bridge *Bridge) TerminalWrite(id, data string) error {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("terminal: input is not base64: %w", err)
	}
	return bridge.terminalManager().Write(id, decoded)
}

// TerminalResize 按前端 fit 后的真实尺寸调整终端。
func (bridge *Bridge) TerminalResize(id string, cols, rows int) error {
	return bridge.terminalManager().Resize(id, cols, rows)
}

// TerminalClose 结束并回收一个终端。
func (bridge *Bridge) TerminalClose(id string) error {
	return bridge.terminalManager().Close(id)
}

// TerminalList 返回当前终端会话（前端重连/重绘时的权威来源）。
func (bridge *Bridge) TerminalList() []TerminalSession {
	return bridge.terminalManager().List()
}

// terminalManager 惰性创建进程级终端管理器（首次使用时创建；事件经
// seelex:terminal 外投）。
func (bridge *Bridge) terminalManager() *terminal.Manager {
	bridge.termMu.Lock()
	defer bridge.termMu.Unlock()
	if bridge.terminals == nil {
		bridge.terminals = terminal.New(bridge.emitTerminalEvent)
	}
	return bridge.terminals
}

// emitTerminalEvent 把终端事件投递给渲染层（未就绪时丢弃：终端是用户显式
// 打开的面板，不存在"渲染层还没订阅就丢首帧"的状态同步问题，重连后前端用
// TerminalList 对账）。
func (bridge *Bridge) emitTerminalEvent(event terminal.Event) {
	bridge.mu.Lock()
	emit, ctx := bridge.emitFn, bridge.ctx
	bridge.mu.Unlock()
	if emit == nil || ctx == nil {
		return
	}
	emit(ctx, terminalEventName, event)
}

// terminalDir 给出新终端的工作目录：当前会话绑定的工作区根优先，未绑定时回退
// 启动时的项目根（与资源管理器/文件预览同一份 workspace 事实）。
func (bridge *Bridge) terminalDir() string {
	snapshot := bridge.app.Snapshot()
	if snapshot.CurrentWorkspace != nil {
		if root := strings.TrimSpace(snapshot.CurrentWorkspace.RootPath); root != "" {
			return root
		}
	}
	return bridge.info.Project.Root
}

// closeTerminals 结束全部终端（宿主退出路径）。未创建过管理器时零开销。
func (bridge *Bridge) closeTerminals() {
	bridge.termMu.Lock()
	manager := bridge.terminals
	bridge.terminals = nil
	bridge.termMu.Unlock()
	if manager != nil {
		manager.CloseAll()
	}
}
