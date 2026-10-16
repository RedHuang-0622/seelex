package switching

// queue.go — 「回合运行中排队输入」的**编辑面判据**（调换顺序 / 撤回到输入框）。
//
// 落点沿革：2026-10-17 随 **M5-R6-queue-edit** 接收者转移从 `application/core/service_queue.go`
// 归位（plan §2.4 的 `service_queue.go` → `middle/switch/`）；锁、队列投影与事件发布仍在宿主。
//
// 口径（迁移前注释逐字保留其含义）：队列只承载「回合运行中」接受的输入（回合收尾时整批提升为下一轮）。
// 因此调换/撤回只在目标会话**运行态**下有意义：非运行态一律以 `ErrQueueNotRunning` 明确拒绝，
// 绝不触碰正在运行的回合（不动 Chat.Running/RequestID/StartedAt，不动引擎历史、任务槽或可见会话）。
//
// 下标空间：对外（ChatState.InputQueue / QueuedCount）与对内（会话队列）是同一套下标——投影逐项、
// 按队列顺序、**不按载荷类型过滤**。因此 GUI/TUI 渲染的第 i 行就是撤回/调换操作的第 i 项，无需换算；
// 若在投影处按载荷过滤，用户看到的行与队列项会错位（调换"成功但列表不变"、撤回交还从未显示过的条目）。

import (
	"errors"
	"fmt"
	"strings"
)

// 队列编辑的三个哨兵（调用方用 `errors.Is` 判定；宿主侧保留迁移前的导出名作为同一错误值的别名）。
var (
	// ErrQueueNotRunning 表示目标会话当前没有运行中的回合，因此不存在可调换/撤回的「排队中」输入
	// （队列只在回合运行期间承接输入）。
	ErrQueueNotRunning = errors.New("no queued input: session is not running")
	// ErrQueueSessionNotFound 表示目标会话不在会话域注册表中（不存在）。
	ErrQueueSessionNotFound = errors.New("queue session not found")
	// ErrQueueIndexOutOfRange 表示调换/撤回的队列下标越界。
	ErrQueueIndexOutOfRange = errors.New("queued input index out of range")
)

// QueueGateErrors 是队列编辑门禁要原样返回的**宿主域错误值**（中间层不重定义它们，plan §1.1）。
type QueueGateErrors struct {
	// Closed 是"应用已关闭"；Draining 是"应用正在排空"（与 Submit 同口径）。
	Closed   error
	Draining error
}

// QueueEditGate 是队列编辑的关闭/排空门禁（与 Submit 同口径）：nil = 放行。
//
// 次序是判据的一部分：**关闭优先于排空**。宿主必须带上对应轴的错误值（nil 会被当作
// `ErrQueueGateUnassembled` 响亮失败，而不是静默放行）。
func QueueEditGate(closed, draining bool, errs QueueGateErrors) error {
	switch {
	case closed:
		if errs.Closed == nil {
			return ErrQueueGateUnassembled
		}
		return errs.Closed
	case draining:
		if errs.Draining == nil {
			return ErrQueueGateUnassembled
		}
		return errs.Draining
	}
	return nil
}

// ErrQueueGateUnassembled 是门禁端口的**装配缺陷**信号（该轴拒绝放行但宿主没带错误值）。
// 装配遗漏必须响亮失败：静默放行会把"应用正在排空"变成一次成功的队列编辑（最难查的失效方向）。
var ErrQueueGateUnassembled = errors.New("queue edit gate error is not assembled")

// QueueTargetSession 解析队列编辑的目标会话 ID：空 = 当前视图会话（草稿会话亦由视图 ID 归属）。
func QueueTargetSession(sessionID, viewSessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return viewSessionID
	}
	return sessionID
}

// QueueTargetError 判定目标会话是否可编辑（调用方在视图锁内解析出 `known` / `running`）：
//   - 会话不在会话域注册表且不是当前视图会话 → `ErrQueueSessionNotFound`；
//   - 会话当前没有运行中的回合 → `ErrQueueNotRunning`（视图会话尚未注册时同样按"没有回合在跑"
//     处理，而不是"会话不存在"）。
//
// 两条错误都带会话 ID（文案逐字保留迁移前的 `%w: session %q`）。
func QueueTargetError(sessionID, viewSessionID string, known, running bool) error {
	if !known {
		if sessionID == viewSessionID {
			return fmt.Errorf("%w: session %q", ErrQueueNotRunning, sessionID)
		}
		return fmt.Errorf("%w: session %q", ErrQueueSessionNotFound, sessionID)
	}
	if !running {
		return fmt.Errorf("%w: session %q", ErrQueueNotRunning, sessionID)
	}
	return nil
}

// ReorderIndexError 是调换下标越界的错误（文案逐字保留迁移前的 `%w: from=%d to=%d`）。
func ReorderIndexError(from, to int) error {
	return fmt.Errorf("%w: from=%d to=%d", ErrQueueIndexOutOfRange, from, to)
}

// RecallIndexError 是撤回下标越界的错误（文案逐字保留迁移前的 `%w: index=%d`）。
func RecallIndexError(index int) error {
	return fmt.Errorf("%w: index=%d", ErrQueueIndexOutOfRange, index)
}
