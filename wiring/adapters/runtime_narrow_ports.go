package adapters

// runtime_narrow_ports.go — 窄可选端口的**生产转发面**。
//
// 为什么需要这个文件（2026-10-02 现场）：
//
// application/core 对"不是每个后端都有的能力"一律用**类型断言**探测
// （见 contract.TeamworkBoardProjection / context_runtime.CompactionIndexPort 的
// 注释："有就有、没有就是没装配"）。这些断言的接收者是 `Deps.Runtime`，而组合根
// （wiring/bootstrap）注入的**不是** *assemble.Runtime，而是本包的 RuntimePort：
//
//	Engine: eng, Runtime: adapters.RuntimePort{Runtime: runtime},
//
// RuntimePort 是**显式手写转发**的包装（无内嵌、无方法提升）。于是"bridge 上有这个
// 方法"与"core 断言得到这个方法"之间隔着一份人肉清单——漏一行，断言就是静默
// false，功能**整块消失**，而且没有任何编译或测试信号。
//
// 这不是假设。下面两行就漏了，后果在 GUI 上直接可见：
//
//	① TeamworkBoardSnapshot 漏 → contract.TeamworkBoardProjection 断言恒 false
//	   → runtime.teamwork_board 恒 nil → 「团队看板」面板与 TUI Alt+T 那一节
//	   永远整块退场（用户报"团队看板没有接线到我们"）。
//	② SessionContextStoreFor 漏 → service_assembler 的 goalStoreFor 恒 nil
//	   → goal 域的 Controller 没有 Store：第五栈不落盘、看板存档面从不绑定
//	   → metadata/board_goal.json 从不产生、重启后 goal 看板无从恢复
//	   （用户报"goal 看板没有重启恢复"）。
//
// 两条都在真实 store 上取证过：同一次会话的 metadata/ 里有 teamwork.json 与
// board_team.json（团队侧经 wiring/bootstrap 的 SetTeamworkBackend 直接走 Runtime，不经过
// 本包装，所以那条链路是通的），却**没有** board_goal.json。
//
// 纪律（把清单变成编译期事实）：application/core 里新增一个窄可选端口断言时，
// 必须在本文件补一行转发 + 一行 `var _` 断言。TestRuntimePortForwardsEveryNarrowPort
// 是一次性闸门：application/core 里出现新的 `Deps.Runtime.(` 断言而本文件没同步时
// 它会红，并指名道姓要求补转发。

import (
	"context"
	"errors"
	"time"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/backend/engine/context"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// TeamworkBoardSnapshot 实现 contract.TeamworkBoardProjection：把某会话的团队看板
// 只读投影（计划 + 作业行 + 审计流水）交给核心。nil = 未装配 teamwork / 解析不出
// 作用域 / 该会话尚无计划——前端据此整块退场（不留空壳），本层不改变这个语义。
func (port RuntimePort) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.TeamworkBoardSnapshot(sessionID)
}

// SessionContextStoreFor 实现 application/core 的会话上下文存储取用面：goal 第五栈
// （以及绑定它的两块看板存档面）经它按会话注入。未实例化/未绑定 → nil，goal 域退回
// 内存态（不报错、不阻断）——那是能力缺失的降级，不是错误。
func (port RuntimePort) SessionContextStoreFor(sessionID string) *store.SessionContextStore {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.SessionContextStoreFor(sessionID)
}

// SetAccountWindow 实现 contract.AccountWindowPort：界面「设置 → 账号」设的窗口经这里
// 落到运行时上，下一次装配/判据立刻按新值算（判定的三跳在 seelebridge 的
// ContextWindowFor 一处）。漏了它 = 界面说改了窗口、预算照旧算旧窗口。
func (port RuntimePort) SetAccountWindow(name string, window int) {
	if port.Runtime == nil {
		return
	}
	port.Runtime.SetAccountWindow(name, window)
}

// SetRoleAccount 把角色默认账号推到运行时账号管理器。
func (port RuntimePort) SetRoleAccount(role, account string) error {
	if port.Runtime == nil {
		return errors.New("runtime is not assembled")
	}
	return port.Runtime.SetRoleAccount(role, account)
}

// AccountWindowFor 返回该账号**生效**的窗口（token；账号级 > 全局 > 账号档）。
func (port RuntimePort) AccountWindowFor(name string) int {
	if port.Runtime == nil {
		return 0
	}
	return port.Runtime.ContextWindowFor(name)
}

// AccountWindowPort 的转发面在这里成文（编译期钉住：漏一个方法这行就红）。
var _ contract.AccountWindowPort = RuntimePort{}

// AccountBalance 实现 contract.AccountBalancePort：界面「设置 → 账户 → 查看余额」的
// 转发面，把账号域的读数（Balance）翻成界面契约（dto.AccountBalance）。
//
// 漏了它**不会报错**：core 侧的断言恒 false ⇒ 面板上每一行都报"宿主未提供余额查询
// 能力"——与本文件头注释里那两处漏接同一类（bridge 上有方法、core 也在探测，中间
// 少一行转发，功能整块消失）。
//
// 映射只在这一层做：账号域不认识 dto，界面也不认识账号域；两边形状变了只改这里，
// 不牵动探测逻辑。
func (port RuntimePort) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	if port.Runtime == nil {
		return dto.AccountBalance{}, errors.New("account balance: runtime is unavailable")
	}
	balance, err := port.Runtime.AccountBalance(ctx, name)
	if err != nil {
		return dto.AccountBalance{}, err
	}
	return accountBalanceDTO(name, balance, time.Now()), nil
}

// accountBalanceDTO 是纯映射（时间由调用方给）：域读数 → 界面契约。
//
// 单独抽出来是为了让"形状没搬错"成为一条可直接断言的判据（金额字符串原样、
// 币种顺序不变、available 如实），而不必为了它构造整个运行时。
func accountBalanceDTO(name string, balance account.Balance, fetchedAt time.Time) dto.AccountBalance {
	result := dto.AccountBalance{
		Name:      name,
		Available: balance.Available,
		Infos:     make([]dto.AccountBalanceInfo, 0, len(balance.Infos)),
		FetchedAt: fetchedAt,
	}
	for _, info := range balance.Infos {
		result.Infos = append(result.Infos, dto.AccountBalanceInfo{
			Currency: info.Currency,
			Total:    info.Total,
			Granted:  info.Granted,
			ToppedUp: info.ToppedUp,
		})
	}
	return result
}

// contract.AccountBalancePort（application/core/service_account.go 的 AccountBalance）：
// 逐账号查余额的转发面。
var _ contract.AccountBalancePort = RuntimePort{}
var _ contract.AccountRoleSelectionPort = RuntimePort{}

// TeammateSessionLive 实现 contract.TeammateSessionProjection：**当前 teammate 会话**
// 的实时只读投影（这件事的会话此刻在说什么）。漏了它，"查看这件事的会话"就只能从会话库
// 读——而 teammate 的一轮活是进程内执行面、正文不落盘，读出来的是主会话的历史（用户报
// 的就是"全是历史会话，不是当前的 teammate 的会话"）。
func (port RuntimePort) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView {
	if port.Runtime == nil {
		return dto.TeammateSessionLiveView{SessionID: sessionID}
	}
	return port.Runtime.TeammateSessionLive(sessionID)
}

// TeammateSessionLivePage 是同一份读数的**分页读法**（"只有尾巴"改成"有界窗口 +
// 分页"的翻页入口）。与 TeammateSessionLive 共用一个事实源，转发不另立形状；窗口/裁剪
// 判据都在 seelebridge 侧，这里只搬。
func (port RuntimePort) TeammateSessionLivePage(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	if port.Runtime == nil {
		return dto.TeammateSessionLiveView{SessionID: sessionID, Offset: offset, Limit: limit}
	}
	return port.Runtime.TeammateSessionLivePage(sessionID, offset, limit)
}

// TeamworkJobCompletions / TeamworkJobEvents 实现 contract.TeamworkJobCompletion：teammate
// 作业（jobs.Manager 里的 worker 作业）的终态只读投影与变化信号口——「做完自动返回」那条
// 链的输入。漏了它**不会报错**：core 侧的类型断言失败 ⇒ 信号口读成 nil ⇒ 空闲会话的触发
// 回合静默消失（leader 上下文里永远没有"谁跑完了"的回声，这正是 2026-10-04 的现场）。
// 两张作业表的取回工具不是同一个（jobs_manage vs bg_manage），因此这一路投影只搬 teammate
// 那一张表的读数，不合表。
func (port RuntimePort) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.TeamworkJobCompletions()
}

func (port RuntimePort) TeamworkJobEvents() <-chan struct{} {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.TeamworkJobEvents()
}

// WithPluginAssembly 实现 application/core 的「按轮插件装配」探测点（定时任务：
// 任务声明的插件集合带进这一轮执行 ctx）。漏了它**不会报错**：core 侧的断言失败
// ⇒ 定时任务的插件装配整块消失（任务照跑，但用的是宿主全局激活的那个插件），
// 而且面板上还显示着"装配了 cad"——读数与行为相反，比不装配更难查。
func (port RuntimePort) WithPluginAssembly(ctx context.Context, plugins []string) context.Context {
	if port.Runtime == nil {
		return ctx
	}
	return port.Runtime.WithPluginAssembly(ctx, plugins)
}

// SetSessionReasoningEffort 实现 contract.ReasoningEffortPort：把会话档位对应的 wire
// 思考强度下发给**配置为跟随会话**的账号（写死强度的账号不动），返回被改动的账号数。
//
// 漏了它**不会报错**：core 侧的断言恒 false ⇒ 用户切档只换了提示词与 loop 次数，
// 模型那边照旧按配置初值/服务端默认想——界面上档位换了、行为没换（正是本包装注释里
// 那类"读数与行为相反"的缺陷）。返回值是纯读数，调用方不必依赖它做控制流。
func (port RuntimePort) SetSessionReasoningEffort(effort string) int {
	if port.Runtime == nil {
		return 0
	}
	return port.Runtime.SetSessionReasoningEffort(effort)
}

// TeamworkPlanFor 实现 application/core 的 `TeamworkPlanSource` 窄可选端口
// （project_board.go 的类型断言）：按会话取 teamwork 计划，未装配 / 该会话没有
// 计划 ⇒ false。
//
// 漏了它**不会报错**：core 侧断言恒 false ⇒ `ProjectBoardTeamPlanFor` 永远返回
// nil ⇒ 项目看板的"这件事的团队计划/运行态"两格整块为空（`ProjectBoardTeamStateFor`
// 也一起空）。这与 2026-10-02 那次 TeamworkBoardSnapshot 漏接是同一类：bridge 上
// 有这个方法、core 也在探测，中间少了一行转发就功能整块消失。
func (port RuntimePort) TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool) {
	if port.Runtime == nil {
		return store.TeamworkPlan{}, false
	}
	return port.Runtime.TeamworkPlanFor(sessionID)
}

// 编译期断言：application/core / view_state 用类型断言探测的每一个窄可选端口，
// 生产包装都必须满足。少一个方法，这里就编译不过——不留"运行时静默 false"。
var (
	_ contract.TeamworkBoardProjection    = RuntimePort{}
	_ contract.TeammateSessionProjection  = RuntimePort{}
	_ contract.TeamworkJobCompletion      = RuntimePort{}
	_ context_runtime.CompactionIndexPort = RuntimePort{}
	// contract.ReasoningEffortPort（application/core/session_scope.go 的
	// syncSessionReasoningEffort）：effort 档位 → wire 思考强度的下发口。
	_ contract.ReasoningEffortPort = RuntimePort{}
	// compactionReadbackProbe（context_runtime/compaction_index.go，包内命名接口）：
	// 压缩**之前**先试一次模型读后感的读数闸。断言失败 = 读数闸缺省，于是"重放
	// 运行时失败"的压缩仍会改写 agent 的上下文（现场：压缩之后看不见上文）。
	_ interface {
		ReadbackCompactionSummary(context.Context, string, context_runtime.CompactionIndexRequest) (context_runtime.CompactionIndexReceipt, error)
	} = RuntimePort{}
	_ interface {
		SessionContextStoreFor(string) *store.SessionContextStore
	} = RuntimePort{}
	_ interface{ ForkInFlight(string) bool }  = RuntimePort{}
	_ interface{ PerSessionExecution() bool } = RuntimePort{}
	_ interface {
		TaskSnapshotFor(string) []dto.TaskRecord
	} = RuntimePort{}
	_ interface {
		WithPluginAssembly(context.Context, []string) context.Context
	} = RuntimePort{}
	// board.TeamworkPlanSource（middle/view/board/syncer.go，包内命名接口）：
	// 项目看板按会话取 teamwork 计划 + 运行态。断言失败 = 那两格恒空。
	_ interface {
		TeamworkPlanFor(string) (store.TeamworkPlan, bool)
	} = RuntimePort{}
)
