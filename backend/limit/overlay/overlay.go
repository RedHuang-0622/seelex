// Package overlay 管理「运行时上限覆盖层」。
package overlay

// runtime_limits_overlay.go — 「运行时上限覆盖层」：把用户在**界面上**改过的运行时
// 参数落成数据根里的一份 JSON（runtime-limits.json），启动时作**最高优先覆盖层**叠
// 在 config/seelex.yaml 的 limits 段之上。
//
// 覆盖链：内置默认 ← config/seelex.yaml 的 limits 段 ← 数据根 runtime-limits.json。
//
// 为什么不写回 config/seelex.yaml：那份文件是**用户的配置**（带注释、手改），程序往
// 回写要重排注释、抹平格式差异，而且"界面上拧的一个数"与"用户写进配置的数"本是两件
// 事——覆盖层让它们各自留在自己的地方，谁压谁一眼可解释。
//
// 落点与 workspace_index.json / scheduled-tasks.jsonl 同级
// （数据根 = store 的基目录），不往二进制目录或用户项目目录里凭空造文件。

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/limit"
)

// FileName 是覆盖层的文件名（数据根内）。
const FileName = "runtime-limits.json"

// Overlay 是覆盖层的正文。**只有界面上真的拧过的格子会出现在这里**：
// 零值/缺省 = 这一格没被改过，配置里的值原样生效。
type Overlay struct {
	// SessionReleaseWindow 是界面上改过的「会话驻留窗口」（见 seelexctx）。
	SessionReleaseWindow int `json:"session_release_window,omitempty"`
	// AccountWindows 是界面上按账号设过的上下文窗口（token；键 = 账号名）。
	// 空表 = 没有任何账号被单独设过，全部走 config/seelex.yaml 的
	// limits.context_window。优先级：账号级 > 全局 > 账号档（见 seelebridge 的
	// Runtime.ContextWindowFor）。
	AccountWindows map[string]int `json:"account_windows,omitempty"`
	// ToolRecordEnabled 是界面上改过的「工具记录计数开关」。**指针**是刻意的：
	// nil = 没在界面上改过（用默认值：开），false = 明确关过——用零值表达"关"会让
	// "没改过"与"改过关"分不开，而这两者在启动时的行为不同。
	ToolRecordEnabled *bool `json:"tool_record_enabled,omitempty"`
}

// Path 是覆盖层的落盘路径：数据根，与 workspace_index.json 同级。调用方（组合根）负责把数据根取出来传进来。
func Path(dataRoot string) string {
	return filepath.Join(dataRoot, FileName)
}

// Read 读覆盖层。路径为空 / 文件不存在 / 内容损坏一律回落到
// "没有覆盖"——与 gui-appearance.json 同一条口径：界面上拧过的参数永远不该阻断启动
// （坏文件只记一条日志，用户照样能开，只是那一格回到配置值）。
func Read(path string) Overlay {
	if strings.TrimSpace(path) == "" {
		return Overlay{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Overlay{} // 不存在是常态（没在界面上改过任何东西）
	}
	var parsed Overlay
	if err := json.Unmarshal(raw, &parsed); err != nil {
		log.Printf("runtime-limits: %s 内容损坏（%v），按“没有覆盖”继续", path, err)
		return Overlay{}
	}
	return parsed
}

// Apply 把覆盖层叠到 limits 上（覆盖层优先）。取值不合法的格子
// 只记一条日志并跳过，不让一份坏文件把启动拦下。
func Apply(base limit.Limits, overlay Overlay) limit.Limits {
	if overlay.SessionReleaseWindow <= 0 {
		return base
	}
	if err := limit.ValidateSessionReleaseWindow(overlay.SessionReleaseWindow); err != nil {
		log.Printf("runtime-limits: session_release_window 取值不合法（%v），忽略这一格覆盖", err)
		return base
	}
	base.SessionReleaseWindow = overlay.SessionReleaseWindow
	return base
}

// WriteSessionReleaseWindow 把覆盖层落盘：**读-改-写**（先读回现有正文再改要改的那一
// 格），这样下一次往这份文件里加第二个参数时，别的格子不会被这次写入抹掉。
//
// 写的是临时文件再改名（同目录内的 rename 在 Windows 上也是原子的）：一次失败不会
// 留下半份 JSON —— 半份 JSON 会被 Read 当成"损坏"整份丢掉，等于
// 把用户之前改过的参数一起清掉。
func WriteSessionReleaseWindow(path string, window int) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("runtime-limits: 落盘路径为空")
	}
	current := Read(path)
	current.SessionReleaseWindow = window
	return writeFile(path, current)
}

// WriteAccountWindow 把"某个账号的上下文窗口"写进覆盖层：
// 同样是**读-改-写**（别的账号的格子与同一份文件里的其它参数都不能被这次写入抹掉），
// window <= 0 = 清掉这个账号的覆盖（键从这里消失，回落到 seelex.yaml 的全局值）。
func WriteAccountWindow(path, name string, window int) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("runtime-limits: 落盘路径为空")
	}
	current := Read(path)
	if window > 0 {
		if current.AccountWindows == nil {
			current.AccountWindows = map[string]int{}
		}
		current.AccountWindows[name] = window
	} else if current.AccountWindows != nil {
		delete(current.AccountWindows, name)
		if len(current.AccountWindows) == 0 {
			current.AccountWindows = nil
		}
	}
	return writeFile(path, current)
}

// WriteToolRecordEnabled 把"工具记录计数开关"写进覆盖层：同样是
// **读-改-写**（别的格子不能被这次写入抹掉）。
func WriteToolRecordEnabled(path string, enabled bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("runtime-limits: 落盘路径为空")
	}
	current := Read(path)
	current.ToolRecordEnabled = &enabled
	return writeFile(path, current)
}

// writeFile 把一份覆盖层落到盘上（临时文件 + rename，见下）。
func writeFile(path string, current Overlay) error {
	payload, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// StoreFile 是 contract.ReleaseWindowStore 在本宿主上的落盘实现：把界面
// 上改过的会话驻留窗口写进数据根的 runtime-limits.json，下次启动由
// Apply 叠加回来。
type StoreFile struct{ Path string }

func (store StoreFile) SaveSessionReleaseWindow(window int) error {
	if err := WriteSessionReleaseWindow(store.Path, window); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}

// SaveAccountWindow 实现 contract.AccountWindowStore：把界面上按账号设的窗口写进
// 数据根 runtime-limits.json 的 account_windows 表，下次启动由
// Overlay.AccountWindows 注入 RuntimeConfig 叠回来。
func (store StoreFile) SaveAccountWindow(name string, window int) error {
	if err := WriteAccountWindow(store.Path, name, window); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}

// SaveToolRecordEnabled 实现 contract.ToolRecordStore：把界面上改的工具记录开关写进
// 数据根 runtime-limits.json 的 tool_record_enabled 格，下次启动由
// Overlay.ToolRecordEnabled 读回（读回落点在 main.go 的装配处）。
func (store StoreFile) SaveToolRecordEnabled(enabled bool) error {
	if err := WriteToolRecordEnabled(store.Path, enabled); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}
