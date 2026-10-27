package switching

// unload.go — `UnloadSession` 用例的**判据与次序**（卸载会话：先持久化（非活跃时），再释放内存态，
// 最后收口视图单例；运行中/恢复中的会话一律拒绝）。
//
// 落点沿革：2026-10-17 随 **M5-R6-unload** 接收者转移从 `application/assembly/session_lifecycle.go`
// 归位（plan §2.4 的 `session_lifecycle.go` → `middle/switch/`）；本片只搬 `UnloadSession`，
// 同文件的 `hotAttachSession`（热挂载，端口面更大）留待下一片（已登记）。
//
// 口径（迁移前注释逐字保留其含义）：registry 保留 COLD 元数据，重开走 cold_load；
// 释放面只有一条路（`releaseFull`：引擎实例 + 桥侧本体 + 可见正文 + 会话单元/标题/任务投影一起离场），
// LRU 释放走同一条但用 `releaseIdle`（多一次 flush、保留会话单元）。

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnloadGateUnassembled 是门控端口的**装配缺陷**信号（该轴拒绝放行但宿主没带错误值）。
// 装配遗漏必须响亮失败：静默放行会把"会话在跑"变成一次成功的卸载。
var ErrUnloadGateUnassembled = errors.New("unload gate error is not assembled")

// UnloadGate 是卸载门控的读数与宿主错误值（宿主在视图锁内取数后装配成值）。
type UnloadGate struct {
	// Restoring 是目标会话正在后台冷加载；Running 是它有回合在跑。两者共用同一条拒绝语义
	// （迁移前就是两条独立判定、同一个 `ErrChatRunning`）。
	Restoring bool
	Running   bool
	// ErrBusy 是对应拒绝轴上原样返回的宿主错误值（必须非 nil）。
	ErrBusy error
}

// Reject 判定是否拒绝卸载：恢复中或运行中一律拒绝（返回宿主的忙碌错误）。
func (gate UnloadGate) Reject() error {
	if !gate.Restoring && !gate.Running {
		return nil
	}
	if gate.ErrBusy == nil {
		return ErrUnloadGateUnassembled
	}
	return gate.ErrBusy
}

// UnloadDeps 是卸载用例的宿主面（值语义：每次调用装配一次；无状态）。
type UnloadDeps struct {
	// Gate 取门控读数（宿主在视图锁内读两轴）。必须非 nil。
	Gate func() UnloadGate
	// IsViewSession 报告目标会话是否就是当前视图会话（活跃会话不预持久化：它本来就在写路径上）。
	IsViewSession func(sessionID string) bool
	// Persist 把目标会话的内存态落盘（仅非活跃会话调用）。
	Persist func(sessionID string) error
	// Release 释放内存态（releaseFull 口径：引擎 + 桥本体 + 正文 + 会话单元/标题/任务投影）。
	Release func(sessionID string) error
	// RenewView 在活跃会话被卸载后收口视图单例（宿主锁内切到就地新建的草稿单元）。
	// 非活跃会话不会被调用。
	RenewView func()
	// PublishSnapshotChanged 发布一次快照变更事件。
	PublishSnapshotChanged func(sessionID string)
	// RefreshCatalog 请求一次会话目录刷新。
	RefreshCatalog func()
}

// Unload 卸载指定会话：运行中/恢复中的会话拒绝；非活跃会话先持久化再释放；活跃会话释放后收口视图单例。
//
// 次序（迁移前逐字保留）：
//
//  1. 空会话 ID（trim 后）→ `session ID is required`；
//  2. 门控：恢复中 / 运行中 → 宿主的忙碌错误（`ErrChatRunning`）；
//  3. 非活跃会话先 `Persist`（失败 → `unload persist %q: %w`，不释放）；
//  4. `Release`（失败 → `release session %q: %w`）；
//  5. 活跃会话：`RenewView`（视图单例始终有对应单元）；
//  6. 发布快照变更 + 请求目录刷新。
func Unload(deps UnloadDeps, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if err := deps.Gate().Reject(); err != nil {
		return err
	}
	active := deps.IsViewSession(sessionID)
	if !active {
		if err := deps.Persist(sessionID); err != nil {
			return fmt.Errorf("unload persist %q: %w", sessionID, err)
		}
	}
	if err := deps.Release(sessionID); err != nil {
		return fmt.Errorf("release session %q: %w", sessionID, err)
	}
	if active {
		deps.RenewView()
	}
	deps.PublishSnapshotChanged(sessionID)
	deps.RefreshCatalog()
	return nil
}
