package residency

// archive.go 是 **C2 ArchiveSession** 用例判据的落点：门控次序 → 存储读 →
// 驱逐前置 flush + 释放引擎 bundle + 从驻留序摘除 → 写归档状态 → 释放该会话
// 名下的后台命令 → 按项目范围刷新目录。
//
// 落点沿革：2026-10-16 随 **M5-R6-archive** 接收者转移从
// `application/core/archive_session.go` 归位（plan §2.4 的 `archive_session.go` → `middle/residency/`）；
// 门面只剩"把宿主读数与落点装成端口"的装配缝 + 一行转发。
//
// 依赖纪律：本包只吃 `middle/protocol/model` 与标准库——会话单元驻留标记、引擎实例、
// 存储落点、过渡锁都由端口带进来，本包不 import `application/*`、`session`、`store`、
// `backend/*`（plan §1.1）。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ErrArchiveNotFound 表示目标会话不存在或 record 缺失/版本不兼容。
//
// 宿主以 `application/core.ErrSessionNotFoundArchive` 这个别名对外保留迁移前的名字
// （同一个错误值，`errors.Is` 两个名字都成立）。
var ErrArchiveNotFound = errors.New("session not found")

// ErrArchiveGateUnassembled 是门控端口的**装配缺陷**信号：某一轴明确拒绝放行，
// 但宿主没有带该轴对应的错误值（nil）。
//
// 为什么不自认"放行"：装配遗漏的方向必须是"响亮地失败"而不是"静默不拒绝"——
// 后者会把"应用正在排空"变成一次成功的归档，是最难查的失效方向（同类判据见
// `catalog.go` 的"必须非 nil"）。
var ErrArchiveGateUnassembled = errors.New("archive gate error is not assembled")

// ArchiveGate 是归档门控的读数与宿主错误值（宿主在视图锁内取数后装配成值）。
//
// 三条拒绝轴各自带着宿主的错误值原样返回：调用方 `errors.Is` 拿到的仍是宿主自己的
// 错误（"application is shut down" / `ErrApplicationDraining` / `ErrChatRunning`
// 都是宿主域的错误，中间层不重新定义它们，见 plan §1.1）。
type ArchiveGate struct {
	// Closed 是应用已关闭；Draining 是应用正在排空（不再接受新动作）。
	Closed   bool
	Draining bool
	// Busy 是目标会话正忙（待审批 / 运行中 / 排队输入 / 在飞请求）；Restoring 是目标
	// 会话正在后台冷加载。两者共用同一条拒绝语义（迁移前就是 `busy || restoring`）。
	Busy      bool
	Restoring bool

	// ErrClosed / ErrDraining / ErrBusy 是对应轴上原样返回的宿主错误值（必须非 nil）。
	ErrClosed   error
	ErrDraining error
	ErrBusy     error
}

// Reject 按迁移前的**原次序**判决门控（关闭 → 排空 → 忙 / 恢复中），返回 nil 即放行。
// 次序是判据的一部分：同时"已关闭"与"忙"时调用方拿到的必须是关闭那条错误。
func (gate ArchiveGate) Reject() error {
	switch {
	case gate.Closed:
		return gateError(gate.ErrClosed)
	case gate.Draining:
		return gateError(gate.ErrDraining)
	case gate.Busy || gate.Restoring:
		return gateError(gate.ErrBusy)
	}
	return nil
}

// gateError 把"该轴拒绝"翻译成一个错误：宿主没带错误值即装配缺陷。
func gateError(err error) error {
	if err == nil {
		return ErrArchiveGateUnassembled
	}
	return err
}

// ArchiveStore 是归档用例的存储面（宿主按会话定位落点后转发；全部必须非 nil）。
type ArchiveStore struct {
	// Load 读目标会话的落盘 record；ok=false 表示 record 缺失（落点仍是宿主的事）。
	Load func(sessionID string) (record model.SessionRecord, ok bool, err error)
	// Flush 把目标会话的内存态落盘（驱逐前置 flush：归档 record 因此携带最新五片）。
	Flush func(sessionID string) error
	// MarkArchived 把 record 的可见状态置为 archived。
	MarkArchived func(sessionID string) error
	// RefreshProject 按目标会话所属项目请求一次目录刷新（归档状态属于该项目格子）。
	RefreshProject func(sessionID string)
}

// ArchiveEngine 是归档用例的引擎面（全部必须非 nil）。
//
// `Unload` / `ForgetResident` 只在 `Loaded` 为真时被调用：legacy 单会话引擎不上报会话路由
// （`Loaded` 恒 false）时它们不会被碰，与迁移前"断言不上就不动引擎"一致。
type ArchiveEngine struct {
	// Resident 报告会话单元**当前**是否持有引擎实例（引擎驻留标记）。
	Resident func(sessionID string) bool
	// Loaded 报告引擎侧是否真有该会话的实例（未装配驻留能力时恒 false）。
	Loaded func(sessionID string) bool
	// Unload 释放引擎侧的会话 bundle。
	Unload func(sessionID string) error
	// ForgetResident 把会话移出驻留使用序并清驻留标记（宿主持锁，本包不知道锁）。
	ForgetResident func(sessionID string)
}

// ArchiveDeps 是归档用例的宿主能力面（值语义：每次调用装配一次；无状态）。
type ArchiveDeps struct {
	// Gate 取门控读数（宿主在视图锁内读四个轴）；中间层在**通过会话 ID 校验之后**才调用
	// 它——空 ID 不该白读一眼宿主状态。必须非 nil。
	Gate func() ArchiveGate
	// Store / Engine 是存储面与引擎面；必须非 nil。
	Store  ArchiveStore
	Engine ArchiveEngine
	// ReleaseAsync 释放该会话名下还在跑的后台命令（会话没了就没有取回/终止入口了）。
	// 必须非 nil（宿主侧本身就是空操作安全的）。
	ReleaseAsync func(sessionID string)
}

// Archive 归档指定会话：门控通过后把 record.Status 写为 archived，驻留会话先 flush 并释放引擎 bundle。
//
// 归档只改 record 状态通道，history/transcript/tool-results/context 四片原样保留；该行于是从常规
// 目录（分格枚举）过滤掉，仍可按 ID 冷读/重开，其它项目的目录不受影响；驻留会话的
// flush → unload 是 **INV-G8 的驱逐前置语义**（先落盘再放引擎，归档 record 因而携带最新五片）。
//
// 次序、文案与幂等口径逐字保留迁移前的实现：
//
//  1. 空会话 ID（trim 后）→ `session ID is required`；
//  2. 门控：关闭 → 排空 → 忙 / 恢复中（错误值由宿主给出，见 ArchiveGate）；
//  3. 读 record：读失败 → `archive load %q: %w`；缺失或 ID 不符 → `%w: %q`（ErrArchiveNotFound）；
//  4. `record.Status == archived` 时**幂等返回 nil**：不碰引擎、不写盘、不刷新目录；
//  5. 驻留且引擎真有实例时：flush（`archive flush %q: %w`）→ unload（`archive unload %q: %w`）
//     → 从驻留序摘除（顺序不可换：先落盘再放引擎，最后才摘标记）；
//  6. 写归档状态（`archive %q: %w`）→ 释放后台命令 → 按项目刷新目录。
func Archive(deps ArchiveDeps, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if err := deps.Gate().Reject(); err != nil {
		return err
	}

	record, ok, err := deps.Store.Load(sessionID)
	if err != nil {
		return fmt.Errorf("archive load %q: %w", sessionID, err)
	}
	if !ok || record.ID != sessionID {
		return fmt.Errorf("%w: %q", ErrArchiveNotFound, sessionID)
	}
	if record.Status == model.SessionStatusArchived {
		return nil
	}

	// 驱逐前置 flush：引擎仍驻留时先持久化完整会话快照，再释放 bundle；Unit/View/Runtime
	// 槽保留（Resident=false，重开走 cold_load）。
	if deps.Engine.Resident(sessionID) && deps.Engine.Loaded(sessionID) {
		if err := deps.Store.Flush(sessionID); err != nil {
			return fmt.Errorf("archive flush %q: %w", sessionID, err)
		}
		if err := deps.Engine.Unload(sessionID); err != nil {
			return fmt.Errorf("archive unload %q: %w", sessionID, err)
		}
		deps.Engine.ForgetResident(sessionID)
	}

	if err := deps.Store.MarkArchived(sessionID); err != nil {
		return fmt.Errorf("archive %q: %w", sessionID, err)
	}
	// 同一时点终止该会话名下还在跑的后台命令：会话没了就没有任何取回/终止入口了。
	deps.ReleaseAsync(sessionID)
	deps.Store.RefreshProject(sessionID)
	return nil
}
