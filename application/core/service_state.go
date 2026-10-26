package core

import (
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	limit "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"sync/atomic"
	"time"
)

// serviceState is assembled from cohesive state groups. Components share the
// application lock (Core.ViewMu) where workflows must publish one coherent
// snapshot; the authoritative Snapshot and external ports live in the shared
// state kernel (Core), while each group still makes ownership and reset
// boundaries explicit.
type serviceState struct {
	*state.Core
	commands contract.CommandRegistryPort

	conversationRuntimeState
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
	draft *draftSlot

	// knobs 是 composer 底部两条**独立**拉条（推理强度 / 循环次数）的状态与判据：
	// 读数、按会话说强度租约与全部守卫都已随 M5-R-limitb 归位 `backend/limit.Knobs`（plan §2.8）。
	// 宿主能力（视图锁、快照写面、引擎与档位读数）经 `serviceKnobsHost` 注入；
	// 加锁次序仍是 视图锁 → 组件锁（只单向），见该组件与适配的注释。
	knobs *limit.Knobs

	// chatSeq 是聊天请求 ID 的单调序号（Core.ViewMu 保护）。requestID 必须
	// 跨会话唯一：Windows 上 time.Now().UnixNano() 分辨率约 0.5ms，并行
	// 会话在同一 tick 启动会碰撞，导致 request→session 绑定与
	// ClearReActBudget/FinalizeTask 串写。附加序号消除碰撞。
	chatSeq uint64
	// draftSeq 是草稿会话 ID 的单调序号（Core.ViewMu 保护）：早分配 SID 在
	// Windows 时间戳低分辨率下也保持同 tick 内唯一。
	draftSeq uint64
	// sessionIDSeq 是显式新建会话（fork/切项目）ID 的全局原子序号
	// （F-4：逐会话宿主不再经 engine.StartSession 拿活跃别名 ID）。
	sessionIDSeq atomic.Uint64
	// compacting 是「该会话有一轮上下文压缩正在进行」的集合（Core.ViewMu 保护）。
	// 它必须独立于 ChatState.Running 存在：落在**没有在飞回合**的会话上的显式
	// 压缩（冷加载/刚清空 → compactSessionContextWithoutEpoch）刻意不写 Running
	// （伪造 Running 会把“有人在跑这个会话”漏进快照、任务注册表和停止按钮），
	// 于是 Submit 的 busy 判据看不见这一轮。缺这道门时的现场：一条消息在压缩
	// 读完引擎历史之后、写回之前开出新回合，两边各自替换引擎历史并写上下文状态，
	// 后写的那份把这一轮压缩整个丢掉。
	// compactSig 是「有会话的压缩轮已收口」的广播信号，形状与 restoreSig 相同
	// （等待方先读通道再睡觉，醒来重读集合复判）。见 context_compact_gate.go。
	compacting map[string]struct{}
	compactSig chan struct{}

	// fullAccessDefault 是进程级全权默认（装配期从引擎门捕获一次；G4：
	// 会话未选择时回退该值，不继承其它会话的遗留开关）。
	fullAccessDefault bool
	// permissionTierDefault 是进程级权限档位默认（装配期从引擎门捕获一次；
	// G4：会话未选择时回退该档位）。fullAccessDefault 保留为兼容读面。
	permissionTierDefault string
}

// draftSlot 保留草稿状态。ID 是早分配的真实会话 ID（草稿会话键）；
// Workspace 为"工作区会话"草稿的工作区绑定（任务会话草稿为 nil）。
type draftSlot struct {
	ID        string
	Workspace *model.WorkspaceInfo
	CreatedAt time.Time
	UpdatedAt time.Time
}

type conversationRuntimeState struct {
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
