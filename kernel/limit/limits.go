// Package limit 是 Seelex 的**判据与档位底层包**：运行时上限（`config/seelex.yaml`
// 的 `limits` 段）的类型、默认值、校验、加载，以及进程级生效快照（active.go）。
//
// 依赖方向红线（docs/2026-10-15-architecture-reshape/plan.md §1.2）：本包只允许依赖
// 标准库、`gopkg.in/yaml.v3` 与纯契约 `contract/dto`，不得 import `seelexctx`、
// `application/*`、`seelebridge/*`；它是全仓库最底层的判据落点，上层不各自重算。
package limit

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SessionStorageLimits 是 config/seelex.yaml `limits.session_storage` 块（与
// limits 段同文件；缺失 = 存储默认值）。键名与 my_design §11 的叶子一一对应
// （`session_storage.<域>.<叶子>` → `session_storage.<域>_<叶子>`）。
type SessionStorageLimits struct {
	RetryCacheMaxItems             int   `yaml:"retry_cache_max_items"`             // 尝试缓存条目上限
	RetryCacheMaxChars             int   `yaml:"retry_cache_max_chars"`             // 尝试缓存字符上限
	RetryCacheWireRecentErrors     int   `yaml:"retry_cache_wire_recent_errors"`    // 同一操作最近 K 条
	RetentionCompactFrameThreshold int   `yaml:"retention_compact_frame_threshold"` // 压缩帧数阈值
	RetentionRawBytesAlert         int64 `yaml:"retention_raw_bytes_alert"`         // 原始 message 告警字节
	QueuePersistPending            *bool `yaml:"queue_persist_pending"`             // 待发送队列落盘恢复
	LockStaleAfterSeconds          int   `yaml:"lock_stale_after_seconds"`          // 数据根锁陈旧判定
	LockAutoRecover                *bool `yaml:"lock_auto_recover"`                 // 陈旧锁自动接管
	BlobSoftLimitChars             int   `yaml:"big_tool_result_soft_limit_chars"`
	BlobHardLimitBytes             int   `yaml:"big_tool_result_hard_limit_bytes"`
	BlobSessionQuotaBytes          int   `yaml:"big_tool_result_session_quota_bytes"`
	// 媒体分区（my_design §10）：字节/像素硬限，无字符软限、永不截断。
	MediaMaxItemBytes       int     `yaml:"media_max_item_bytes"`
	MediaSessionQuotaBytes  int     `yaml:"media_session_quota_bytes"`
	MediaMaxItemsPerSession int     `yaml:"media_max_items_per_session"`
	MediaMaxLongSide        int     `yaml:"media_max_long_side"`
	WireBudgetTokens        int     `yaml:"wire_budget_tokens"` // §5.2 wire 总预算
	WireSoftRatio           float64 `yaml:"wire_soft_ratio"`    // 软阈值比例（触发压缩）
	WireTargetRatio         float64 `yaml:"wire_target_ratio"`  // 目标比例（裁剪到哪）
}

// ── 运行时上限（limits）────────────────────────────────────────
// config/seelex.yaml 的 limits 段（权限在同一目录的 config/seele.yaml，两者不是
// 同一个文件）：集中治理需要跨模块一致、影响资源消耗或用户可见行为的运行时上限。
// 局部 UI/格式常量仍由所属模块维护，不宣称消除所有常量。
// 缺失字段 → 零值 → 消费方套用 DefaultLimits 的默认值；无需改代码即可调参。

// Limits 是运行时行为上限集合（零值 = 未配置，走默认）。
type Limits struct {
	// SessionStorage 是 v8 会话存储的 §11 参数块（my_design §11）。默认值不在
	// 这里重复声明：零值 = 未配置，由 store 的覆盖链补默认，避免同一
	// 数字两处维护。分片行数沿用既有顶层键 limits.message_shard_size。
	SessionStorage SessionStorageLimits `yaml:"session_storage"`
	// 时延类（秒；0 = 无限制/未配置）
	ToolCallTimeoutSec     int `yaml:"tool_call_timeout"`     // 工具调用超时（0 = 无限制）
	ApprovalTimeoutSec     int `yaml:"approval_timeout"`      // 审批等待
	PlanDecisionTimeoutSec int `yaml:"plan_decision_timeout"` // preflight 决策回合
	HeartbeatIntervalSec   int `yaml:"heartbeat_interval"`    // workplan 心跳间隔
	ReplanWindowSec        int `yaml:"replan_window"`         // replan 频率窗口
	SearchTimeoutSec       int `yaml:"search_timeout"`        // 所有搜索源的 HTTP 超时
	TavilyTimeoutSec       int `yaml:"tavily_timeout"`        // 兼容别名（已弃用，优先 search_timeout）
	// 预算/上限类
	MaxConcurrentReplans   int `yaml:"max_concurrent_replans"`       // replan 并发上限
	MaxReplansPerWindow    int `yaml:"max_replans_per_window"`       // 窗口内 replan 次数
	MaxReplanProviderReqs  int `yaml:"max_replan_provider_requests"` // 窗口内 provider 请求预算
	MaxReplansPerPlanChain int `yaml:"max_replans_per_plan_chain"`   // 单计划链 replan 次数
	// SessionReleaseWindow 是**会话释放窗口**（yaml: session_release_window，默认 1）：
	// 受保护集（当前视图 / 待审批 / 运行·排队·在飞 / 正在恢复）之外的会话，最多
	// 保留几个「已加载」的会话。语义与释放动作见 application/assembly/session_release.go：
	//   1 = 只保留上一个（切走一个、再切走一个才释放最旧的）——出厂口径；
	//   0/缺失 = 未配置 → 默认 1（本轮窗口不会因为没人配就退化成"全都留着"）；
	//   N = 再保留 N 个（越大越顺滑，代价是常驻内存越多）。
	// 释放 = 引擎实例 + 桥侧会话本体（框架 Session / 会话上下文存储）+ 可见正文
	// 一起放掉；会话单元、目录行、磁盘事实保留，重开走冷加载。
	SessionReleaseWindow int `yaml:"session_release_window"`
	// ResidentSessionLimit / LoadedContentLimit 是**分层覆盖（老口径，可选）**：
	// 显式 >0 时按"该层驻留会话总数上限"（含当前视图会话）计算——只想单独约束
	// 某一层的现场用它；0/缺失 = 交给 SessionReleaseWindow 统一管辖。
	//
	// 两者管的是同一份内存的两层：resident 层是引擎 bundle（provider 历史 +
	// 工具运行时，重），content 层是已加载会话的可见正文窗口（history_window 条，
	// 轻）。分层口径下的取值理由是"引擎远重于一个正文窗口"，所以老默认按 2×
	// 留余量（6 / 12）——那是**老口径**的默认；新口径下两层共用一个窗口，默认 1。
	ResidentSessionLimit int `yaml:"resident_limit"`
	LoadedContentLimit   int `yaml:"loaded_content_limit"`
	HistoryWindow        int `yaml:"history_window"`          // 会话可见历史条数
	PlanNodeEvents       int `yaml:"plan_node_events"`        // 节点详情时间线上限
	PlanNodeMaxLoops     int `yaml:"plan_node_max_loops"`     // 子代理节点循环上限（默认 384）
	EvidenceChars        int `yaml:"evidence_chars"`          // 证据/输出截断
	ReplanEvidenceBytes  int `yaml:"replan_evidence_bytes"`   // replan 证据字节上限
	InputLoopLimit       int `yaml:"input_loop_limit"`        // 输入循环上限
	ReferencePageSize    int `yaml:"reference_page_size"`     // 引用工具默认分页
	MaxReferencePageSize int `yaml:"max_reference_page_size"` // 引用工具分页上限
	GrepMaxResults       int `yaml:"grep_max_results"`        // grep 默认结果数
	SessionNameRunes     int `yaml:"session_name_runes"`      // 会话名截断
	PreflightRetry       int `yaml:"preflight_retry"`         // preflight 重试次数
	OutputReserveTokens  int `yaml:"output_reserve_tokens"`   // provider 输出预留 token；**运行期压缩预算不用它**——那里取账号档 max_tokens（Runtime.MaxOutputTokens()），本键只在 DefaultContextBudget（兜底）与 ValidateRetainWindow（启动期校验）消费
	// ContextWindow 覆盖账号档的上下文窗口（token；0 = 未配置 → 用账号档
	// config/accounts.yaml 的 defaults.context_window / 账号条目覆盖值）。
	//
	// 为什么放在这里：窗口是**预算的基数**（预算 = 窗口 − 账号档 max_tokens −
	// 窗口÷安全除数），而账号档是本地凭据文件——不进版本库、也不参与
	// scripts/sync-bootseed-defaults.ps1；配在本段才随出厂档分发、可评审。消费点是
	// seelebridge Runtime.ContextWindow()（判据与保留窗口都读它）。
	// 语义边界（两条都别读错）：
	//   - 覆盖是**全局**的：所有账号（含子代理角色）一刀切，账号档里更小的窗口会被
	//     盖掉；只有当"账号档填小了、模型其实更大"时才该用它。
	//   - 它只改**判据用的**窗口，provider 侧仍有自己的硬限制：填得比模型真实窗口大，
	//     请求会被 provider 拒（超窗口）。
	ContextWindow int `yaml:"context_window"`
	// ── 上下文压缩预算比例（0 = 用默认比例；见 task_context.newContextBudget）──
	// 预算 = 账号档 context_window − 账号档 max_tokens − window/context_safety_reserve_divisor；
	// （兜底路径 DefaultContextBudget 才用 output_reserve_tokens 当输出预留的下限。）
	// 下面各个百分比都以此为基数。默认 8/95/98/80/50（2026-09-26 起压缩阈值上调：
	// 旧 75/90 的保留窗口落点贴着软线，长会话会一轮一压；target 80 同时是保留区
	// 硬上限，soft − target 是每次压缩留给下一轮的余量）。
	// 取值超界（不在 [0,100]）在 LoadLimits 显式报错，不再静默回退默认值。
	ContextSafetyReserveDivisor int `yaml:"context_safety_reserve_divisor"` // 安全预留除数（默认 8 → 窗口/8）
	ContextSoftPercent          int `yaml:"context_soft_percent"`           // 软压缩线（占预算 %，默认 95）。**2026-09-30 起已不参与判据**（取消软线
	// 提前量：压缩改写请求前缀、provider 前缀缓存整段作废，折回来的余量不值得
	// 每轮付这份代价）——装配层自动压缩的唯一阈值改由 context_hard_percent 给出，
	// 报告面的 soft 与 hard 同源。键与下面的 soft<hard 校验保留只为兼容既有配置，
	// 下一版一并摘除。
	ContextHardPercent       int `yaml:"context_hard_percent"`        // 硬阈值线（占预算 %，代码默认 98；出厂配置 config/seelex.yaml 2026-10-15 由 98 调到 90）。软线必须 < 它，否则 LoadLimits 报错
	ContextTargetPercent     int `yaml:"context_target_percent"`      // 压缩后目标（占预算 %，默认 80）；同时是压缩后保留区/请求落点的硬上限（RetainDecision.TargetTokens），必须低于 soft 才有余量
	ContextSingleItemPercent int `yaml:"context_single_item_percent"` // 单条输入外置阈值（占预算 %，默认 50）
	// ── 保留区下限与帧摘要传递上限（《压缩四区模型》边界判定 / 《待落地》1、2）──
	// ContextRetainFloorPercent 是**保护区下限**（占预算 %）：
	//   floor = max(最近 1 个完整协议单元, 该比例 × 预算)
	// 保留区 = clamp(占比 × 全量, floor, retain_tokens)。0 = 未配置（只保留
	// 「至少 1 个完整协议单元」兜底，与引入该旋钮之前逐位一致）。
	// 取值超界（不在 [0,100]）在 LoadLimits 显式报错，不再静默回退默认值。
	ContextRetainFloorPercent int `yaml:"context_retain_floor_percent"`
	// ContextFrameCarryTokens 是**帧摘要传递上限**（token）：本地压缩把上一帧
	// Chapter 2 正文并入新帧时的并入量上限；超出部分退化为锚点（segment_id +
	// request 首尾 + 一句话），细节靠 search_history / read_compressed_turn 回读。
	// 该份正文是唯一进上下文、进缓存前缀的帧内容，不设上限会随帧数膨胀。
	ContextFrameCarryTokens int `yaml:"context_frame_carry_tokens"`
	ToolTokenOverhead       int `yaml:"tool_token_overhead"`   // 工具 token 估算开销
	ContextMaxUnits         int `yaml:"context_max_units"`     // 上下文压缩扫描单元上限
	MessageShardSize        int `yaml:"message_shard_size"`    // 会话存储分片条数
	SummaryChars            int `yaml:"summary_chars"`         // 摘要截断字符数
	TodoMaxItems            int `yaml:"todo_max_items"`        // todolist 清单项上限
	WorkTableRows           int `yaml:"work_table_rows"`       // 工作表格（work table）最大行数
	WalkTimeoutSec          int `yaml:"walk_timeout"`          // glob/grep 目录遍历超时（秒）
	MaxToolResultChars      int `yaml:"max_tool_result_chars"` // 工具结果最大**字符数**（0 → 默认；超大结果归档为 result_ref）。与上面 context_* 那组"占预算 %"的 token 判据不是一套数：它只管"单条结果太长"，与上下文窗口/压缩阈值互不影响。存储侧另有 session_storage.big_tool_result_*（当前无生产调用方，见 store/storage_settings.go）
	// SnapshotToolOutputChars 是**可见会话快照**的单条工具输出上限（0 →
	// 默认 8000）。超过该值的输出只把前 N 字符的预览放进快照会话
	// （message.content / tool.result），完整内容归档为 result_ref，前端
	// 点击"加载完整输出"时经 ToolResultContent 读回。provider 侧历史
	// 仍保留完整结果（受 max_tool_result_chars 约束），此值只约束
	// GUI/渲染进程拿到的载荷，是 WebView2 渲染内存的治本截断线。
	SnapshotToolOutputChars int `yaml:"snapshot_tool_output_chars"`
	// docker 守护进程自动恢复（2026-08-07）：bash 命令因 Docker Desktop
	// 未运行失败时，自动启动守护进程并重跑一次命令（真实环境有 docker CLI
	// 但 daemon 未启动是常见状态，沙箱应帮模型把环境"修好"而不是报错）。
	DisableDockerAutoStart bool `yaml:"disable_docker_auto_start"` // true = 关闭自动拉起（默认开启）
	DockerStartTimeoutSec  int  `yaml:"docker_start_timeout"`      // 启动等待上限（秒；0 → 60）
	// fork 子代理长任务宽松预算（2026-08-08）：fork_subagents 是同步编排
	// 工具，总时长 = 全部子代理工作量之和，不能被通用工具超时（30 分钟）
	// 掐死；fork 用独立大预算。子代理节点循环数复用 effort 调节值
	// （SubagentPolicy.MaxNodeLoops），不做独立常量。
	ForkTimeoutSec int `yaml:"fork_timeout"` // fork 工具总超时（秒；0 → 7200 = 2 小时）
	// AsyncExec 是**作业面**（子进程调用系契约：bash_bg / read_batch / bg_manage /
	// fork_subagents 的 async 模式）的开关块。默认 false = 关闭：作业工具都不注册、
	// bash 收到旧入参 background=true 直接报错、fork 的 async 模式显式拒绝。
	// 关就是关（不静默降级成同步执行），可一键回滚。
	// 规格：docs/2026-09-24-async-tool-deferred-ack/README.md §8。
	AsyncExec AsyncExecLimits `yaml:"async_exec"`
	// ProjectBoard 是**项目看板**（会话级任务/里程碑的派生投影 + 按需读取面）的开关块。
	// **零值 false = 关**（关就是关：不注入订阅口、不在工具完成段触发、一个文件都不写、
	// 读取面（技能条目 + project_board_read）也不注册）；**出厂档 config/seelex.yaml
	// 显式打开**（2026-10-15 起，与"文档说实现了、盘上就该真有东西"同口径）。
	//
	// 开着的代价全在工具完成段这条热路径上（去抖后仍要取活体投影，内容没变不写盘），
	// 写盘本身有界（单文件上限 + 越界先压实）。回滚臂就是一行 `enabled: false`。
	// 规格：docs/arch/seelex-project-board.md §5.3 / §5.4 / §9.3。
	ProjectBoard ProjectBoardLimits `yaml:"project_board"`
	// ContextCompactionSummary 是**压缩处 LLM 章节化摘要**（前缀重放厚摘要）的
	// 开关块。**缺省即开**（2026-10-04 起）：配置里整块缺失、或块在但没写
	// `enabled` 键，都按打开处理（与出厂档 config/seelex.yaml 一致）；只有**显式**
	// `enabled: false` 才关闭（压缩恒走本地确定性压缩 summary_source=local、一次模型
	// 调用都不发），这就是回滚臂。
	//
	// 为什么把缺省放在"开"一侧：关掉时帧会退化成"元数据投影 + 一句本地兜底措辞"，
	// 而那次退化的现场极难归因（2026-10-04：用户读到"压缩摘要开关未开启"，配置里却是
	// `enabled: true`）。缺省与出厂档一致，"关"就只剩显式一个来源。
	//
	// 打开的代价必须写清楚：这是**新增的、无人值守的付费调用**——每次压缩一次，
	// 溢出区超过片预算时分片重放会按片多次。收益是帧 Chapter 2 从"元数据投影"
	// 变成真摘要（Errors and Fixes / Pending / Next Step 三节不再恒 (none)），
	// 从而给 search_history 的词法初筛提供有区分度的关键词。
	ContextCompactionSummary CompactionSummaryLimits `yaml:"context_compaction_summary"`
	// Team 是 teamwork 的产品级约束块（role 数、active job 数等），见 TeamLimits。
	Team TeamLimits `yaml:"team"`
	// Concurrency 是可选的路由级窄化限制。默认关闭：团队作业仍由
	// Team.MaxJobs 统一约束；开启后才为 teammate / subagent 路由再叠一层限制。
	Concurrency ConcurrencyLimits `yaml:"concurrency"`
	// Plugins 是**按会话插件装配**的约束块（能力轴：一个 teammate 身上挂几个插件），
	// 见 PluginLimits。
	Plugins PluginLimits `yaml:"plugins"`
	// Continuation 是**goal 未收口 + 被硬中断时的自动续跑**开关块（见
	// ContinuationLimits）：零值（含整块缺失）= 关，关就是关（一次都不扫、
	// 一个回合都不起）。设计见 docs/arch/goal-auto-continue.md。
	Continuation ContinuationLimits `yaml:"continuation"`
	// Runtime 是进程级启动行为块（见 RuntimeLimits）：当前只有一个开关——
	// 是否同意多进程共用同一数据根。零值 = 关（单实例），与既有
	// 「单数据根 = 单进程写者」（store/data_root_lock.go）一致；
	// 打开需在配置里显式写 runtime.allow_multi_process: true。
	Runtime RuntimeLimits `yaml:"runtime"`
}

// AsyncExecLimits 是后台命令轮询切片的开关块。零值（含整个块缺失）= 关闭，
// 因此不需要在 DefaultLimits / WithDefaults 里重复声明默认。
type AsyncExecLimits struct {
	Enabled bool `yaml:"enabled"`
	// TriggerConversation 让后台作业落到**终态**时为它所属会话起一个回合：作业面
	// 从设计起就是轮询型（模型答完就停，结果由模型自己的下一次工具调用取回），
	// 「跨回合无人取回」是它的固有缺口（docs/2026-09-24-async-tool-deferred-ack/
	// README.md §10.5），本开关补的就是这一步——终态 → 起回合 → 模型自己
	// `bg_manage(op=fetch)` 取回。
	//
	// 语义边界（写死，别在实现里漂）：
	//   - **done 与 failed 都触发**；killed 不触发（那不是"作业有了结果"，是被终止）；
	//   - **只在会话空闲时触发**：忙会话不被打断、也不往它的队列里塞东西——铁律见
	//     docs/arch/teamwork-leader-worker-architecture.md §6.1，忙会话走"下一次回合
	//     边界的打点块完成行"（work_table_async.go）；
	//   - 每个句柄只触发一次（句柄单调、永不复用）。
	//
	// 零值 = 关（与 async_exec 同一套"关就是关"的纪律）。
	TriggerConversation bool `yaml:"trigger_conversation"`
}

// ProjectBoardLimits 是**项目看板**的开关块（limits.project_board）。
//
// 三格里没有 `max_rows`，这是**有意的偏离**（契约 §5.4 的示例块里有它）：单次追加的
// 行数上限是存储面的硬不变量，唯一判据是 `dto.ProjectBoardMaxRows`
// （越界显式报错，不静默截断）。在这里再放一个可调上限，就会有两个判据对同一次追加
// 各说各话（配置说 1000、存储说 512，用户看到的只有"没生效"）。
type ProjectBoardLimits struct {
	// Enabled 是整块能力的开关（缺省 false）。true 才注入订阅口；false ⇒ 完成段
	// 一次判断都不做、零文件、读取面也不注册（无静默降级）。
	Enabled bool `yaml:"enabled"`
	// Hook 是触发面：off | todo_list（缺省）| task_complete | tool_complete | all。
	// 取值校验在装配期完成（唯一判据是 application/assembly 的 ProjectBoardHookModes，
	// 这里不抄一份词表）：拼错的名字**显式启动失败**，不静默降级成"从不触发"。
	Hook string `yaml:"hook"`
	// FlushIdleMS 是落盘去抖窗（毫秒）：窗内多次触发只落一次盘。
	// 0 = 未配置 → 同步器的缺省（2s，唯一一处定义在 application/assembly）。
	FlushIdleMS int `yaml:"flush_idle_ms"`
}

// CompactionSummaryLimits 是压缩处 LLM 章节化摘要的开关块。
//
// 两层口径**刻意分开**，不要把其中一层的话搬到另一层：
//   - **配置文件层**：缺省 = 开（整块缺失 / 没写 enabled 键都算没表态）。判定在
//     LoadLimits（YAML 的键在不在，只有它看得见）与 DefaultLimits（连文件都没有）；
//     bool 字段的零值分不清"没写"与"写了 false"，所以这份区分必须留在配置入口。
//   - **字段层**：显式 false 仍然 = 关；直接构造 Limits{} 的宿主/测试保持"零值 = 关"
//     的旧行为（WithDefaults 不补这一位——补了就把回滚臂吞掉了）。
//
// 两个 token 字段的零值各自回退到消费方的既有兜底常量（InputTokens → 不分片；
// Chapter2Tokens → seelexctx.PrefixReplayMaxTokens），不在这里重复声明，避免
// 同一数字两处维护。
type CompactionSummaryLimits struct {
	Enabled bool `yaml:"enabled"`
	// InputTokens 是分片重放的**片预算**（模型输入侧）：溢出区自身超过它时按
	// 协议单元切片逐片重放、摘要前向传递（见 ChunkReplayMessages /
	// SummarizeChunkPlan）。0 = 未配置 → 沿用既有推导（账号上下文窗口 × 3/4，
	// 见 seelebridge 的 replayInputTokens）；写负值在 LoadLimits 报错。
	// 注意"不分片"不是 0 的语义——那会静默改掉既有推导。
	InputTokens int `yaml:"input_tokens"`
	// Chapter2Tokens 是厚摘要的输出预算。0 → PrefixReplayMaxTokens（2048）。
	// QuickChat 通道未透传预算时仅作记录。
	Chapter2Tokens int `yaml:"chapter2_tokens"`
}

// TeamLimits 是 teamwork 产品级约束块：role 数与 active worker job 数
// 是两个独立维度，不能互相推导。MaxTeammates 是旧键名，继续表示 role 数；
// MaxRoles 是同一语义的新键；MaxJobs 只限制团队当前 active KindWorker 作业数。
type TeamLimits struct {
	// MaxTeammates 是兼容旧配置的 role 数键，不表示 job 并发数。
	MaxTeammates int `yaml:"max_teammates"`
	// MaxRoles 是 role 数上限的新键，与 MaxTeammates 同义。两者同时出现且
	// 不同时，启动期显式报错，不替用户猜哪一个是意图。
	MaxRoles int `yaml:"max_roles"`
	// MaxJobs 是同一团队 active worker job 的硬上限（默认 6）。
	MaxJobs int `yaml:"max_jobs"`
	// WorkItemToolFailure 是连续显式工具失败后的交权策略，默认关闭。
	WorkItemToolFailure WorkItemToolFailureLimits `yaml:"workitem_tool_failure"`
}

// WorkItemToolFailureLimits 是 WorkItem worker 连续显式工具失败返回策略。
type WorkItemToolFailureLimits struct {
	Enabled   bool `yaml:"enabled"`
	Threshold int  `yaml:"threshold"`
}

// ConcurrencyLimits 是可选路由级并发限制块。
type ConcurrencyLimits struct {
	Teammate RouteConcurrencyLimit `yaml:"teammate"`
	Subagent RouteConcurrencyLimit `yaml:"subagent"`
}

// RouteConcurrencyLimit 是单一路由的可选窄化限制。Enabled=false 时 Max
// 不参与判据，允许保持 0；Enabled=true 时 Max 必须为正。
type RouteConcurrencyLimit struct {
	Enabled bool `yaml:"enabled"`
	Max     int  `yaml:"max"`
}

// RuntimeLimits 是**进程级启动行为**块（limits.runtime）。与 TeamLimits 不同，
// 它不放"能开几个人"这类业务上限，只回答"这一次启动允不允许"。
//
// 当前只有一个开关，且**默认关**（零值 false），理由是现状即单实例：数据根
// 独占锁（store/data_root_lock.go 的 lock.owner）把"单数据根 = 单进程
// 写者"钉成不变量，第二个进程启动即被拒绝。这个开关只用来在明确知情时放行。
type RuntimeLimits struct {
	// AllowMultiProcess 决定启动期是否同意第二个进程共用同一数据根。
	//   - false（默认，含整块缺失）：单实例。另一个进程在写同一数据根时，
	//     启动期闸门（bootstrap.guardMultiProcess）给出可读拒绝；装配深处的数据根锁
	//     同样会把冲突报成 ErrDataRootLocked。
	//   - true：放行。第二个进程不再被数据根锁拒绝，代价是失去跨进程写者
	//     串行化——一致性由使用者负责（配置注释里写清了代价）。
	AllowMultiProcess bool `yaml:"allow_multi_process"`
}

// DefaultTeamMaxTeammates 是 role 数上限的出厂默认值（兼容旧名）。
const DefaultTeamMaxTeammates = 6

// DefaultTeamMaxJobs 是团队 active worker job 数的出厂默认值。
const DefaultTeamMaxJobs = 6

// DefaultWorkItemToolFailureThreshold 是连续失败返回策略的默认阈值。
const DefaultWorkItemToolFailureThreshold = 3

// PluginLimits 是**按会话插件装配**的产品级约束块（limits.plugins）。
//
// 与 TeamLimits 同族：都是"Seelex 自己掌控、框架不可替代"的那几个数字。这里的
// 上限定的是**一个人身上挂几个能力包**——每多挂一个，代价不是零：技能目录每轮
// 常驻 system prompt（见 backend/engine/assemble/runtime_role_plugins.go 的黄牌读数），工具面
// 还要在权限面之外再做一层收窄求交。
type PluginLimits struct {
	// PerTeammate 是每个 teammate（每个角色会话）的插件数上限（默认 3）。
	// 超限的 team_plan / 角色登记**显式拒绝**（不静默截断——截断会把"我声明了 5 个"
	// 悄悄变成"装了 3 个"）。
	//
	// 处置口径：0 = 未配置 → 默认 3；负值在 LoadLimits 显式报错（与
	// team.max_teammates 同一口径：负数既不是"无限制"也不是"禁用"，两种解读都会
	// 让配置看不出来）。
	PerTeammate int `yaml:"per_teammate"`
}

// DefaultPluginsPerTeammate 是每会话插件数上限的出厂默认值（决策：3 个；与
// dto.MaxPluginsPerRole 同值，两处都在"没有配置"的那一侧兜底）。
const DefaultPluginsPerTeammate = 3

// DefaultLimits 返回全部默认值（与重构前的硬编码常量一一对应，行为不变）。
func DefaultLimits() Limits {
	return Limits{
		ToolCallTimeoutSec:     int((30 * time.Minute) / time.Second), // 旧默认 120s 已提高
		ApprovalTimeoutSec:     600,                                   // 等待用户审批
		PlanDecisionTimeoutSec: 10,
		HeartbeatIntervalSec:   15,
		ReplanWindowSec:        60,
		SearchTimeoutSec:       15,
		MaxConcurrentReplans:   2,
		MaxReplansPerWindow:    6,
		MaxReplanProviderReqs:  6,
		MaxReplansPerPlanChain: 2,
		// 会话释放面（2026-10-14）：统一窗口默认 1 = 受保护集之外只保留上一个。
		// 分层覆盖键不在这里补默认——0 的语义是"交给统一窗口管辖"。
		SessionReleaseWindow: 1,
		HistoryWindow:        200,
		PlanNodeEvents:       30,
		PlanNodeMaxLoops:     384,
		EvidenceChars:        800,
		ReplanEvidenceBytes:  12 * 1024,
		InputLoopLimit:       9999,
		ReferencePageSize:    4000,
		MaxReferencePageSize: 12000,
		GrepMaxResults:       20,
		SessionNameRunes:     16,
		PreflightRetry:       2,
		OutputReserveTokens:  512,
		// 压缩预算比例：窗口/8、95%、98%、80%、50%。soft/target 的差即每次
		// 压缩留给下一轮的余量；target 同时是保留区硬上限（见 RetainDecision）。
		ContextSafetyReserveDivisor: 8,
		ContextSoftPercent:          95,
		ContextHardPercent:          98,
		ContextTargetPercent:        80,
		ContextSingleItemPercent:    50,
		// 帧摘要传递上限：并入量 ≤ 1024 token（默认值即「定值」；0 = 未配置 →
		// 本默认）。并入超限的部分退化为锚点，细节靠检索回读。
		ContextFrameCarryTokens: 1024,
		ToolTokenOverhead:       64,
		ContextMaxUnits:         4,
		MessageShardSize:        100,
		SummaryChars:            800,
		TodoMaxItems:            20,
		WorkTableRows:           200,
		WalkTimeoutSec:          30,
		// fork 汇总窗口按子代理数 ×n 放大：4×2000 字结论 ≈ 24KB，默认
		// 60000 字节（约 2 万汉字）给足余量——窗口是容灾上限不是截断线。
		MaxToolResultChars:      60000,
		SnapshotToolOutputChars: 8000,
		DockerStartTimeoutSec:   60,
		ForkTimeoutSec:          7200,
		// teamwork：role 数与 active worker job 数各默认 6，语义互相独立。
		Team: TeamLimits{
			MaxTeammates: DefaultTeamMaxTeammates,
			MaxRoles:     DefaultTeamMaxTeammates,
			MaxJobs:      DefaultTeamMaxJobs,
			WorkItemToolFailure: WorkItemToolFailureLimits{
				Enabled:   false,
				Threshold: DefaultWorkItemToolFailureThreshold,
			},
		},
		// 按会话插件装配：每会话上限默认 3（能力包的常驻代价见 PluginLimits）。
		Plugins: PluginLimits{PerTeammate: DefaultPluginsPerTeammate},
		// 压缩处厚摘要（前缀重放 LLM 章节化摘要）：**缺省即开**（2026-10-04 起，
		// 与出厂档 config/seelex.yaml 一致）。这里声明的是缺省值，不是强制值：
		// 显式 `enabled: false` 仍然关（LoadLimits 里显式值优先），直接构造
		// Limits{} 的宿主/测试也仍然是关（字段零值不变）。
		ContextCompactionSummary: CompactionSummaryLimits{Enabled: true},
	}
}

// ReleaseWindow 返回生效的**会话释放窗口**（受保护集之外最多保留几个已加载
// 会话）：未配置或非法（<=0）→ 默认 1（只保留上一个）。这是唯一读法——消费方
// 不得直接读 SessionReleaseWindow 字段，否则零值会退化成"窗口 0 = 什么都留不住"。
func (l Limits) ReleaseWindow() int {
	if l.SessionReleaseWindow > 0 {
		return l.SessionReleaseWindow
	}
	return DefaultLimits().SessionReleaseWindow
}

// 会话释放窗口的可调区间（前端「设置 → 会话驻留」的取值面，也是启动期覆盖层的
// 校验面）。
//
// 上界 16 是**取舍**而不是算术限制：窗口里的每一个会话都意味着一份常驻的引擎实例
// + 桥侧本体 + 可见正文，摆一个"永不释放"的格子等于把内存治理整块关掉。下界 1
// （不是 0）：窗口 0 的语义是"谁都不留"，那会让每次切会话都冷加载，而且与配置层的
// 零值口径（0 = 未配置 → 默认 1）撞车。
const (
	SessionReleaseWindowMin = 1
	SessionReleaseWindowMax = 16
)

// ValidateSessionReleaseWindow 是窗口取值的**唯一**判据：设置面的写路径与启动期的
// 落盘覆盖层共用它，避免出现"能存进去、读回来却不认"的两份口径。
func ValidateSessionReleaseWindow(window int) error {
	if window < SessionReleaseWindowMin || window > SessionReleaseWindowMax {
		return fmt.Errorf("会话驻留窗口需在 %d..%d 之间（收到 %d）",
			SessionReleaseWindowMin, SessionReleaseWindowMax, window)
	}
	return nil
}

// DefaultToolResultLimit 返回工具结果字符预算的 seelex 生效默认值。
// 所有消费方（processor / controller / application.core）以此为兜底，
// 与 seelex.yaml limits 段 max_tool_result_chars 的覆盖合并后保持一致；
// 未配置 → 本默认（60000）。框架 ctx_manager 默认（约 4000，见 seele.go
// re-export）不再作为兜底，仅保留 re-export 语义。
func DefaultToolResultLimit() int { return DefaultLimits().MaxToolResultChars }

// WithDefaults 把零值字段替换为默认值，返回完整配置。
// ToolCallTimeoutSec 特殊：0 是显式"无限制"语义（limits 段存在时），不补默认。
func (l Limits) WithDefaults() Limits {
	def := DefaultLimits()
	if l.ApprovalTimeoutSec == 0 {
		l.ApprovalTimeoutSec = def.ApprovalTimeoutSec
	}
	if l.PlanDecisionTimeoutSec == 0 {
		l.PlanDecisionTimeoutSec = def.PlanDecisionTimeoutSec
	}
	if l.HeartbeatIntervalSec == 0 {
		l.HeartbeatIntervalSec = def.HeartbeatIntervalSec
	}
	if l.ReplanWindowSec == 0 {
		l.ReplanWindowSec = def.ReplanWindowSec
	}
	if l.SearchTimeoutSec == 0 && l.TavilyTimeoutSec != 0 {
		l.SearchTimeoutSec = l.TavilyTimeoutSec
	}
	if l.SearchTimeoutSec == 0 {
		l.SearchTimeoutSec = def.SearchTimeoutSec
	}
	if l.MaxConcurrentReplans == 0 {
		l.MaxConcurrentReplans = def.MaxConcurrentReplans
	}
	if l.MaxReplansPerWindow == 0 {
		l.MaxReplansPerWindow = def.MaxReplansPerWindow
	}
	if l.MaxReplanProviderReqs == 0 {
		l.MaxReplanProviderReqs = def.MaxReplanProviderReqs
	}
	if l.MaxReplansPerPlanChain == 0 {
		l.MaxReplansPerPlanChain = def.MaxReplansPerPlanChain
	}
	if l.SessionReleaseWindow == 0 {
		l.SessionReleaseWindow = def.SessionReleaseWindow
	}
	if l.HistoryWindow == 0 {
		l.HistoryWindow = def.HistoryWindow
	}
	if l.PlanNodeEvents == 0 {
		l.PlanNodeEvents = def.PlanNodeEvents
	}
	if l.PlanNodeMaxLoops == 0 {
		l.PlanNodeMaxLoops = def.PlanNodeMaxLoops
	}
	if l.EvidenceChars == 0 {
		l.EvidenceChars = def.EvidenceChars
	}
	if l.ReplanEvidenceBytes == 0 {
		l.ReplanEvidenceBytes = def.ReplanEvidenceBytes
	}
	if l.InputLoopLimit == 0 {
		l.InputLoopLimit = def.InputLoopLimit
	}
	if l.ReferencePageSize == 0 {
		l.ReferencePageSize = def.ReferencePageSize
	}
	if l.MaxReferencePageSize == 0 {
		l.MaxReferencePageSize = def.MaxReferencePageSize
	}
	if l.GrepMaxResults == 0 {
		l.GrepMaxResults = def.GrepMaxResults
	}
	if l.SessionNameRunes == 0 {
		l.SessionNameRunes = def.SessionNameRunes
	}
	if l.PreflightRetry == 0 {
		l.PreflightRetry = def.PreflightRetry
	}
	if l.OutputReserveTokens == 0 {
		l.OutputReserveTokens = def.OutputReserveTokens
	}
	if l.ContextSafetyReserveDivisor == 0 {
		l.ContextSafetyReserveDivisor = def.ContextSafetyReserveDivisor
	}
	if l.ContextSoftPercent == 0 {
		l.ContextSoftPercent = def.ContextSoftPercent
	}
	if l.ContextHardPercent == 0 {
		l.ContextHardPercent = def.ContextHardPercent
	}
	if l.ContextTargetPercent == 0 {
		l.ContextTargetPercent = def.ContextTargetPercent
	}
	if l.ContextSingleItemPercent == 0 {
		l.ContextSingleItemPercent = def.ContextSingleItemPercent
	}
	if l.ContextFrameCarryTokens == 0 {
		l.ContextFrameCarryTokens = def.ContextFrameCarryTokens
	}
	if l.ToolTokenOverhead == 0 {
		l.ToolTokenOverhead = def.ToolTokenOverhead
	}
	if l.ContextMaxUnits == 0 {
		l.ContextMaxUnits = def.ContextMaxUnits
	}
	if l.MessageShardSize == 0 {
		l.MessageShardSize = def.MessageShardSize
	}
	if l.SummaryChars == 0 {
		l.SummaryChars = def.SummaryChars
	}
	if l.TodoMaxItems == 0 {
		l.TodoMaxItems = def.TodoMaxItems
	}
	if l.WorkTableRows == 0 {
		l.WorkTableRows = def.WorkTableRows
	}
	if l.WalkTimeoutSec == 0 {
		l.WalkTimeoutSec = def.WalkTimeoutSec
	}
	if l.MaxToolResultChars == 0 {
		l.MaxToolResultChars = def.MaxToolResultChars
	}
	if l.SnapshotToolOutputChars == 0 {
		l.SnapshotToolOutputChars = def.SnapshotToolOutputChars
	}
	if l.DockerStartTimeoutSec == 0 {
		l.DockerStartTimeoutSec = def.DockerStartTimeoutSec
	}
	if l.ForkTimeoutSec == 0 {
		l.ForkTimeoutSec = def.ForkTimeoutSec
	}
	switch {
	case l.Team.MaxTeammates == 0 && l.Team.MaxRoles == 0:
		l.Team.MaxTeammates = def.Team.MaxTeammates
		l.Team.MaxRoles = def.Team.MaxRoles
	case l.Team.MaxTeammates == 0:
		l.Team.MaxTeammates = l.Team.MaxRoles
	case l.Team.MaxRoles == 0:
		l.Team.MaxRoles = l.Team.MaxTeammates
	}
	if l.Team.MaxJobs == 0 {
		l.Team.MaxJobs = def.Team.MaxJobs
	}
	if l.Team.WorkItemToolFailure.Threshold == 0 {
		l.Team.WorkItemToolFailure.Threshold = def.Team.WorkItemToolFailure.Threshold
	}
	if l.Plugins.PerTeammate == 0 {
		l.Plugins.PerTeammate = def.Plugins.PerTeammate
	}
	return l
}

// Durations 返回常用的时间转换（秒字段 → time.Duration）。
func (l Limits) Durations() (toolCall, approval, planDecision, heartbeat, replanWindow, searchTimeout time.Duration) {
	return time.Duration(l.ToolCallTimeoutSec) * time.Second,
		time.Duration(l.ApprovalTimeoutSec) * time.Second,
		time.Duration(l.PlanDecisionTimeoutSec) * time.Second,
		time.Duration(l.HeartbeatIntervalSec) * time.Second,
		time.Duration(l.ReplanWindowSec) * time.Second,
		time.Duration(l.SearchTimeoutSec) * time.Second
}

// LoadLimits 读取 config/seelex.yaml（运行参数文件；权限在 config/seele.yaml）的
// limits 配置段：
//   - 文件不存在或 limits 段缺失 → DefaultLimits()（完整默认值）；
//   - limits 段存在 → 解析字段（未写字段 0 → 调用方 WithDefaults 补默认；
//     tool_call_timeout: 0 是显式"无限制"，保留）；
//   - 解析失败或负值显式报错。
func LoadLimits(path string) (Limits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultLimits(), nil
		}
		return Limits{}, fmt.Errorf("limits: read config: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Limits{}, fmt.Errorf("limits: parse config: %w", err)
	}
	root := documentContent(&document)
	limitsNode, ok := root["limits"]
	if !ok {
		return DefaultLimits(), nil
	}
	// limits 段的键存在性：压缩处厚摘要开关的缺省语义要读它（见
	// compactionSummaryDefaultsOpen）。取值一律走下面的 Decode，这份映射只回答
	// "写没写这个键"。
	limitsContent := documentContent(limitsNode)
	var check Limits
	if err := limitsNode.Decode(&check); err != nil {
		return Limits{}, fmt.Errorf("limits: parse config: %w", err)
	}
	// 续跑块里的显式数字先校验（拼错/负数显式失败，不静默降级成"从不续跑"）。
	if err := check.Continuation.Validate(); err != nil {
		return Limits{}, fmt.Errorf("limits: %w", err)
	}
	teamContent := documentContent(limitsContent["team"])
	_, legacyRolesWritten := teamContent["max_teammates"]
	_, rolesWritten := teamContent["max_roles"]
	if legacyRolesWritten && rolesWritten && check.Team.MaxTeammates != check.Team.MaxRoles {
		return Limits{}, fmt.Errorf(
			"limits: team.max_roles (%d) must equal legacy team.max_teammates (%d)",
			check.Team.MaxRoles, check.Team.MaxTeammates)
	}
	if rolesWritten && !legacyRolesWritten {
		check.Team.MaxTeammates = check.Team.MaxRoles
	}
	if legacyRolesWritten && !rolesWritten {
		check.Team.MaxRoles = check.Team.MaxTeammates
	}
	for name, route := range map[string]RouteConcurrencyLimit{
		"concurrency.teammate": check.Concurrency.Teammate,
		"concurrency.subagent": check.Concurrency.Subagent,
	} {
		if route.Max < 0 {
			return Limits{}, fmt.Errorf("limits: %s.max must not be negative", name)
		}
		if route.Enabled && route.Max <= 0 {
			return Limits{}, fmt.Errorf("limits: %s.max must be positive when enabled", name)
		}
	}
	if check.ToolCallTimeoutSec < 0 || check.ApprovalTimeoutSec < 0 || check.PlanDecisionTimeoutSec < 0 ||
		check.HeartbeatIntervalSec < 0 || check.ReplanWindowSec < 0 || check.SearchTimeoutSec < 0 || check.TavilyTimeoutSec < 0 ||
		check.MaxConcurrentReplans < 0 || check.MaxReplansPerWindow < 0 || check.MaxReplanProviderReqs < 0 || check.MaxReplansPerPlanChain < 0 || check.SessionReleaseWindow < 0 || check.ResidentSessionLimit < 0 ||
		check.LoadedContentLimit < 0 ||
		check.HistoryWindow < 0 || check.PlanNodeEvents < 0 || check.PlanNodeMaxLoops < 0 ||
		check.EvidenceChars < 0 || check.ReplanEvidenceBytes < 0 || check.InputLoopLimit < 0 ||
		check.ReferencePageSize < 0 || check.MaxReferencePageSize < 0 || check.GrepMaxResults < 0 ||
		check.SessionNameRunes < 0 || check.PreflightRetry < 0 || check.OutputReserveTokens < 0 ||
		check.ContextWindow < 0 ||
		check.ToolTokenOverhead < 0 || check.ContextMaxUnits < 0 || check.MessageShardSize < 0 || check.SummaryChars < 0 || check.TodoMaxItems < 0 || check.WorkTableRows < 0 || check.WalkTimeoutSec < 0 ||
		check.MaxToolResultChars < 0 || check.SnapshotToolOutputChars < 0 || check.DockerStartTimeoutSec < 0 ||
		check.ForkTimeoutSec < 0 || check.ContextRetainFloorPercent < 0 || check.ContextFrameCarryTokens < 0 ||
		check.ContextCompactionSummary.InputTokens < 0 || check.ContextCompactionSummary.Chapter2Tokens < 0 ||
		check.Plugins.PerTeammate < 0 ||
		check.ProjectBoard.FlushIdleMS < 0 ||
		check.Team.MaxTeammates < 0 || check.Team.MaxRoles < 0 ||
		check.Team.MaxJobs < 0 || check.Team.WorkItemToolFailure.Threshold < 0 {
		return Limits{}, fmt.Errorf("limits: values must not be negative")
	}
	// 比例类旋钮的超界不再静默回退默认值（那会把「用户写错了」吞成「看起来生效」）：
	// 预算比例必须落在 (0,100]，0 = 未配置（由 WithDefaults 补默认）。
	for _, ratio := range []struct {
		name  string
		value int
	}{
		{"context_soft_percent", check.ContextSoftPercent},
		{"context_hard_percent", check.ContextHardPercent},
		{"context_target_percent", check.ContextTargetPercent},
		{"context_single_item_percent", check.ContextSingleItemPercent},
		{"context_retain_floor_percent", check.ContextRetainFloorPercent},
	} {
		if ratio.value < 0 || ratio.value > 100 {
			return Limits{}, fmt.Errorf("limits: %s must be within [0,100], got %d", ratio.name, ratio.value)
		}
	}
	// 相对关系同样是配置的一部分（与保留区下限 > retain_tokens 的那条启动期报错同一条
	// 纪律：静默接受非法组合，问题只会以性能症状出现）。软线是"到达就压缩"的主判据，
	// 硬线是"装配后仍越线就立刻自主压缩"的抢跑路径：软线 ≥ 硬线时抢跑每轮都成立，
	// 表现为"一轮对话压一次"，而配置里看不出任何异常。
	//
	// 判定用 WithDefaults 之后的**生效值**，不是原始解析结果：只写了 soft: 100 而没写
	// hard 时，生效的是 100/98（非法）；只看原始值会让这种写法绕过校验。
	effective := check.WithDefaults()
	if effective.ContextSoftPercent >= effective.ContextHardPercent {
		return Limits{}, fmt.Errorf(
			"limits: context_soft_percent (%d) must be < context_hard_percent (%d)"+
				"（软线到达即压缩；软线 ≥ 硬线会让自主压缩每轮抢跑）",
			effective.ContextSoftPercent, effective.ContextHardPercent)
	}
	// 压缩处厚摘要开关的缺省语义（2026-10-04 起 = 开，与出厂档一致）：整块缺失、或
	// 块在但没写 enabled 键，都按打开处理；写了 enabled 就是显式值，显式值优先
	// （`enabled: false` 是回滚臂，不许被默认值吞掉）。判定只能在这里做——bool 字段
	// 的零值分不清"没写"与"写了 false"，而这一段的 YAML 键就在手边。
	if compactionSummaryDefaultsOpen(limitsContent) {
		check.ContextCompactionSummary.Enabled = true
	}
	return check, nil
}

// compactionSummaryDefaultsOpen 报告配置里的压缩处厚摘要开关是否落在"缺省"一侧
// （缺省 = 开）：整块缺失、或块在但没写 enabled 键（含空块）= 缺省；写了 enabled
// 键 = 显式值（true/false 都算），显式值不做缺省补正。
//
// limitsContent 是 limits 段按 **键存在性** 组织的映射（见 documentContent）：
// 只读"写没写"，不猜"写了什么"——取值一律由 Decode 负责，这里不复制一份取值逻辑。
func compactionSummaryDefaultsOpen(limitsContent map[string]*yaml.Node) bool {
	block, written := limitsContent["context_compaction_summary"]
	if !written {
		return true
	}
	_, hasEnabled := documentContent(block)["enabled"]
	return !hasEnabled
}

// documentContent 返回 YAML 节点按**键存在性**组织的映射：文档节点取根映射，
// 映射节点（如 limits 段的值节点）直接读它自己的键；null/空文档与非映射节点 → nil。
//
// 两种入参都要支持，是因为"写没写这个键"既可能问文档（整个 limits 段），也可能问
// 某一段（context_compaction_summary.enabled），而调用方手里只有那一个节点——为
// 第二种再写一份取键逻辑，就是同一件事两处维护。
func documentContent(document *yaml.Node) map[string]*yaml.Node {
	if document == nil {
		return nil
	}
	if document.Kind == yaml.MappingNode {
		return mappingContent(document)
	}
	if len(document.Content) == 0 {
		return nil
	}
	return mappingContent(document.Content[0])
}

// mappingContent 把映射节点的键值对收进 map；非映射（含 null / 标量）→ nil。
// 只读键与节点，不取值——取值一律由 yaml 的 Decode 负责。
func mappingContent(node *yaml.Node) map[string]*yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	content := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		content[node.Content[index].Value] = node.Content[index+1]
	}
	return content
}
