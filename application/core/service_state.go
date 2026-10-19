package core

import (
	"github.com/RedHuang-0622/seelex/application/prompt"
	input_router "github.com/RedHuang-0622/seelex/backend/engine/input"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/residency"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
	"sync"
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
	commands *input_router.CommandRegistry

	conversationRuntimeState
	lifecycleRuntimeState
	workTableRuntimeState
	promptRuntimeState

	// sessions 是会话域（阶段 B：会话资源唯一所有者；本状态只保留当前
	// 会话的只读视图指针 V，不再持有任何会话容器）。
	sessions *session.Domain

	// toolRecords 是**每会话**一份的工具记录内存累计（键 = 会话 ID；落盘按批次走
	// Deps.ToolRecordSnapshotStore）。放这里而不是 components：它是随工具调用持续变化的
	// 会话状态，不是装配期交接一次的外部端口。
	toolRecords *toolRecordAccumulators

	// toolStages 是**叶子暂存**的登记表（执行单元 → 它自己那份本地累加，见
	// tool_record_stage.go）。与 toolRecords 同一生态位（随工具调用变化、不落盘），
	// 但作用域是"执行单元"而不是"会话"：读数在这里先攒、收口时才交给会话累计。
	toolStages *toolRecordStages

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
	// reviewSeq 是「请 GUI 打开待审阅文件」召唤 ID 的单调序号（与 chatSeq 同一
	// 理由：时间戳在同一 tick 内会撞号，撞号会让前端把"新的一次召唤"读成
	// "同一份快照的补放"而不重新开 chip）。无锁（atomic），因为它在 ViewMu 之外
	// 也要可用。
	reviewSeq atomic.Uint64
	// draftSeq 是草稿会话 ID 的单调序号（Core.ViewMu 保护）：早分配 SID 在
	// Windows 时间戳低分辨率下也保持同 tick 内唯一。
	draftSeq uint64
	// sessionIDSeq 是显式新建会话（fork/切项目）ID 的全局原子序号
	// （F-4：逐会话宿主不再经 engine.StartSession 拿活跃别名 ID）。
	sessionIDSeq atomic.Uint64
	// degradeOnce 保证并发故障只触发一次降级退出。
	degradeOnce sync.Once
	// residentOrder 是驻留引擎（session bundle）的 LRU 使用序（Core.ViewMu
	// 保护；索引 0 = 最近使用）。G6 驱逐按最旧优先；running/queued/
	// awaiting_approval 与当前视图会话不可驱逐（INV-G8）。
	residentOrder residency.Order

	// loadedContentOrder 是「已加载会话可见正文」的 LRU 使用序（Core.ViewMu
	// 保护；索引 0 = 最近使用）。内容上限 = limits.loaded_content_limit
	// （默认 12，见 seelexctx.LoadedContentLimit）；超限时按最旧优先卸载正文
	// （application/core/content_lru.go）：视图会话 / running / queued /
	// awaiting_approval（含待批/排队输入）/ restoring / 本已无正文的会话都
	// 不做候选。与 residentOrder 是两层独立治理：引擎驻留与否、正文留内存
	// 与否互不替代。
	loadedContentOrder residency.Order

	// viewEpoch 是视图切换的单调序号（Core.ViewMu 保护）：它是一支**时钟**——
	// 每次"激活某个会话"都从这里取一个序号当该会话的视图意图值（见
	// sessionViewIntent），不直接参与发布判定。
	viewEpoch uint64
	// sessionViewIntent 是**按会话**的视图意图（Core.ViewMu 保护）：sessionID →
	// 最近一次"激活该会话"取到的序号。后台冷加载完成时校验自己仍是**该会话**的
	// 最新意图，否则只完成装载、不抢视图。
	//
	// 为什么按会话而不是全局：全局序号会被**别的会话**的切换推走——"切走又切回
	// 同一个会话"时，在途装载因序号过期而永不发布，视图停在空壳上（用户看到空
	// 会话）。会话释放面（window=1）让"切回即冷加载"成为常见路径，这条路必须成立：
	// 切回同一目标只重指空壳、不动意图，在途装载完成时仍能发布。
	sessionViewIntent map[string]uint64
	// restoring 是“后台冷加载中”的会话集合（Core.ViewMu 保护）：这些会话
	// 的目录行与当前快照展示 SessionStatusRestoring，装载完成/失败即移除。
	restoring map[string]struct{}
	// restoreSig 是“有会话的 restoring 被清除”的广播信号（Core.ViewMu 保护）：
	// clearRestoringLocked 关闭旧通道并重建新通道；等待方（awaitRestore）先读
	// 通道再去睡觉，醒来后重读 restoring 复判（多会话并发装载时一个信号可能
	// 对应别的会话，复判后继续等）。用途：把显式后台提交挂到装载完成点
	// （SubmitToSession 非阻塞契约与“restoring 期间不得开新回合”的调和）。
	restoreSig chan struct{}

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

// workTableRuntimeState 持有工作表格增量发布器（CSP 汇聚；见
// worktable_publisher.go）。
type workTableRuntimeState struct {
	workTablePublisher *worktable.WorkTablePublisher
	// lastPublishedSubagentTreeSig 是上一次随 worktable.changed 发出的子代理
	// 树内容签名（只由工作表格发布器 goroutine 读写，见
	// Service.workTableEventPayload）：相同签名不重复下发树，避免表格增量被
	// 运行期树投影撑大。
	lastPublishedSubagentTreeSig string
}

type lifecycleRuntimeState struct {
	idle     chan struct{}
	draining bool
	closed   bool
	// CSP 生命周期消费者（子代理树信号 / plan 节点事件 / task 变更）停止
	// 控制：取代同步回调嵌套，数据经 channel 流转。
	lifecycleStop chan struct{}
	lifecycleOnce sync.Once
}

type promptRuntimeState struct {
	promptStack   *prompt.PromptStack
	effortManager *prompt.EffortManager
}
