package assembly

import (
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	limit "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock (Core.ViewMu) where workflows must publish one coherent
// snapshot; the authoritative Snapshot and external ports live in the shared
// state kernel (Core), while each group still makes ownership and reset
// boundaries explicit.
type serviceState struct {
	*state.Core
	session.ApplicationRuntimeState
	commands contract.CommandRegistryPort

	lifecycleRuntimeState
	promptRuntimeState

	// sessions 是会话域（阶段 B：会话资源唯一所有者；本状态只保留当前
	// 会话的只读视图指针 V，不再持有任何会话容器）。
	sessions *session.Domain

	// toolRecords owns the per-session accumulators, leaf stages, switch, snapshot
	// port and flush error. Host callbacks make draft and view policy explicit.
	toolRecords *applicationtoolrecord.Application

	// draft 是"新建会话"草稿槽位（Core.ViewMu 保护）：早分配 SID 的草稿
	// （HasSession=false，不建引擎 bundle）持有真实会话 ID 与工作区绑定；
	// 首次提交（materializeDraftSession）时消费并清空。
	draft *session.DraftSlot

	// knobs 是 composer 底部两条**独立**拉条（推理强度 / 循环次数）的状态与判据：
	// 读数、按会话说强度租约与全部守卫都已随 M5-R-limitb 归位 `kernel/limit.Knobs`（plan §2.8）。
	// 宿主能力（视图锁、快照写面、引擎与档位读数）经 `serviceKnobsHost` 注入；
	// 加锁次序仍是 视图锁 → 组件锁（只单向），见该组件与适配的注释。
	knobs *limit.Knobs
}

type lifecycleRuntimeState struct {
	idle     chan struct{}
	draining bool
	closed   bool
}

type promptRuntimeState struct {
	promptStack   *prompt.PromptStack
	effortManager *prompt.EffortManager
}
