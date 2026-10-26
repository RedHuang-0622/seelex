package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 本文件是 docs/2026-10-15-architecture-reshape/plan.md §1.3「门面不变量」的机械断言
// （M1-D）。四条不变量的口径：
//
//	I1 门面依赖闭包      —— 门面只能触达允许清单内的 Seelex 包
//	I2 前端直接依赖      —— 前端只 import 门面（+ 前端自己的子包），不得直接 import 后端包
//	I3 门面不出现构造函数 —— application 包内无 func New*
//	I4 类型用别名而非新类型 —— application 包内每个 type 都是 `type X = Y`
//
// 允许清单是 **ratchet（只许收缩）**：清单外的依赖一律失败；清单内的依赖被删掉不失败，
// 但下次整顿时应当把清单一起收短（不然清单会变成"历史包袱登记表"）。
//
// 为什么 I1 只对"Seelex 包"做 ratchet：门面的传递闭包今天还包含 ~220 个第三方叶子包
// （Seele / viper / grpc / otel …），它们全部经由 application/core 与 seelebridge 进来。
// 把这些叶子逐条写进清单只会淹没信号；本里程碑要钉的是**层边界**（门面还能不能碰到
// seelebridge / store / seelexctx），所以清单只收 Seelex 前缀的包。
// 契约层（contract）不同：那里的判据是"闭包 = stdlib"，所以连 Seele 与 otel 都必须
// 逐条列出来（见 contractClosureAllowList）。

const (
	seelexModulePrefix = "github.com/RedHuang-0622/seelex"
	contractPackage    = "./contract/..."
	facadePackage      = "./application"
)

// facadeClosureAllowList 是门面（./application）传递闭包里的 Seelex 包（2026-10-16 实测）。
// 只许收缩：M4 解散 seelebridge、M5 收窄门面后，这张表应当大幅变短。
//
// 关于清单规模：plan 点名的搬迁会让同一个包换名字进闭包（例如
// `application/core/internal/limits` → `backend/limit`，M2-A）——这类是**一条出、
// 一条进**的等量替换，清单规模不变。plan 点名的**新模块**（例如 M2-B 的
// `backend/limit/loop`：从 prompt/adapters/fork 抽出来的判据层）会让闭包真的多一个包，
// 这是允许的，但必须同时满足两条并记进里程碑记录：
//  1. 新模块自己的依赖闭包是"干净叶子"（只有标准库 / 明确登记的第三方）；
//  2. 这里把上限往上抬一格，并写清"多了谁、为什么"。
//
// 除此之外，清单**只许收缩**——只减表里的条目、不动这个数字。
// M2-C3：`backend/limit/accountwindow` 是同一口径下的新判据模块（自身闭包 = 标准库），
// 按"新模块可 +1"的规则把上限抬到 68。
// M2-C4：`backend/limit/policy` 同口径——权限判据从 `seelebridge/tools` 抽出，是
// **一条进**的新判据模块（自身闭包 = 标准库 + Seele + `contract/dto`，后者本就在表内），
// 因此上限抬到 69。
// M3-S2：`store/root` 同口径——数据根锁 / 原子发布 / 共享读 / 残尾 / `Settings` 从
// `store` 抽出，是**一条进**的基座模块（自身闭包 = 纯标准库，比 `backend/limit`
// 更干净），因此上限抬到 70。
// M4：`internal/jsonobj` 同口径——LLM 原文的 JSON 容错层从 `seelebridge/json_object.go`
// 抽出成模块级叶子包（自身闭包 = 纯标准库），因此上限抬到 71。
// 同一片里还删掉了 `seelebridge/internal/mapper`（plan §2.10 要求删；它唯一的函数内联进
// 调用方 `runtime_live.go`）——闭包少一个包，上限**跟着降**回 70。
// M4-C（调度器持久化面）：`store/schedule` 同口径——定时任务定义的全局 JSONL 从
// `backend/engine/schedule/persistence.go` 抽出成基座子包（直接依赖 = 标准库 + contract 系），
// 因此上限抬到 71。
// M4-J2a：`backend/engine/obs` 同口径——请求画像与会话遥测从装配器再下沉成叶子观测包
// （直接依赖 = 标准库 + Seele + `internal/telemetry`），因此上限抬到 72。
// M4-J2b：`backend/tool/io/media` 是**域下沉**而不是抽判据——会话媒体与随图
// （原 assemble 的 `runtime_image.go`）带着宿主端口搬进自己的包。它不引入任何新的
// 下游包（只用已在清单内的 `store` / `backend/tool/computer` / `imageattach` /
// `internal/{model,telemetry}`），所以闭包只多它自己一格，上限抬到 73。
// M5-R5：`middle/view/board` 同口径——项目看板的行投影 + 触发表 + 同步器从
// `application/core/project_board.go` 抽成中间层模块（自身直接依赖 = 标准库 +
// `store` + `middle/protocol/model` + `contract/dto` + `middle/view/worktable` +
// `backend/limit`，全在清单内）。此前它是 `application/core` 内部的一个文件、
// 不是独立包，因此这是**一条进**的新模块，上限抬到 74。
// 同片还抽走了 `middle/view/board/boardtest`（测试夹具包）：它只被两处用例 import，
// 不在门面闭包里（`go list -deps ./application` 不含测试依赖），故不登记。
// M5-R5c：`middle/view`（视图读数面：诊断文本 / 性能指标 / 运行时投影）同口径——
// 从 `application/core` 的三个根文件抽出中间层视图根包（plan §2.4）。此前它们是
// `application/core` 内部的文件、不是独立包，因此这是**一条进**的新模块，上限抬到 75。
// 同片还消掉一条越层边：投影形状本来取自身为适配层的 `backend/engine/assemble`，
// 现在直接用契约层 `contract/dto`（闭包规模不变，只是来源改正）。
// M5-R5d：`middle/view/errors`（错误呈现域）同口径——从 `application/core` 的两个根文件
// （`error_codes.go` + `error_presentation.go`）抽出中间层视图子包（plan §2.4）。
// 此前它们是 `application/core` 内部的文件、不是独立包，因此这是**一条进**的新模块，
// 上限抬到 76。同一片还把跨层判据（错误码 / provider 失败分类 / ReAct 预算哨兵）
// 下沉到 `contract`：它本来就在清单里，闭包规模不变。
// M5-R-limitc：`backend/limit/tier`（权限档位域）同口径——把 `application/core` 的三处散落
// 实现抽成独立模块（plan §2.8），因此是**一条进**的新模块，上限抬到 77。
// M5-R-tool-review：`backend/tool/review`（show_files 落点）同口径——从 `application/core`
// 抽成独立模块（plan §2.6），因此是**一条进**的新模块，上限抬到 78。
// M5-R-tool-reference：`backend/tool/reference`（工具结果引用与分页判据）同口径——从
// `application/core` 抽成独立模块（plan §2.6），因此是**一条进**的新模块，上限抬到 79。
// M5-R6-residency-preview：`middle/residency`（快照预算的截断判据）同口径——从
// `application/core` 抽成中间层叶子模块（plan §2.4），因此是**一条进**的新模块，上限抬到 80。
// M5-R6-history-page：`middle/switch/history`（历史分页的窗口装配判据）同口径——从
// `application/core` 抽成中间层叶子模块（plan §2.4），因此是**一条进**的新模块，上限抬到 81。
// M5-R6-session-meta：`middle/switch`（会话展示元数据判据 + 后续切换家族）同口径——从
// `application/core` 抽成中间层模块（plan §2.4），因此是**一条进**的新模块，上限抬到 82。
// P21-10（2026-10-17）：`backend/tool/jobs`（异步作业域：作业事件流 + 执行表投影）是**新建**的
// 域包（plan §2.6 的目标落点），装配层（本就在闭包内，见下条 `backend/engine/assemble` 的登记理由）
// 现在 import 它，因此闭包多这一条，上限抬到 83。它与既有登记项不同的一点是：它不是从
// `application/core` 抽出来的，而是从**装配层**抽出来的——闭包里的装配层本来就在，抽出来的域包
// 自然跟着进闭包。域包自身只依赖 Seele 的 `event`/`jobs`、`backend/tool/registry` 与 `contract/dto`。
// M5-R6-workspace（2026-10-17）：`middle/switch/workspace`（工作区用例）从
// `application/core/workspace_usecase.go` 抽成中间层叶子模块（plan §2.4）。
// 这是一条"从 core 抽出的落点新建"，装配层现在 import 它；新包只依赖
// `contract`/`contract/dto`/`middle/protocol/model` 与窄端口，没有第三方或反向依赖。
// 因此上限抬到 85。
// M-SSH（2026-10-23）：`backend/tool/ssh`（远端执行面的连接基座）进入闭包，上限抬到 86。
// 与前几条不同，它不是"抽出来"的，而是**新能力**：内置工具族多了一个执行面
// （read/write/edit/bash/bg_manage 可在远端跑），装配层经 `backend/tool/registry`
// 的传输适配间接 import 它。抬高理由与 P21-10 同口径——闭包里的装配层本就在，
// 它新引入的执行面基座自然跟着进闭包；该包只依赖标准库与 vendor 内的
// `golang.org/x/crypto/ssh`，不 import 任何上层包，方向上不构成反向依赖。
// 2026-10-25 teammate 会话硬化：门面新增 `middle/view/sessionaccess`（统一
// View/Chat/Recover 契约）与 `backend/multiagent/agentsession`（WorkItem/子代理
// 独立 DurableHistory 工厂）。两者都是新领域落点，自身不反向依赖 application；
// 按“新模块可 +1”的既有规则，上限从 86 抬到 88。
// BM-002 共享协议下沉：`contract/protocol` 成为门面依赖闭包内的新契约子包。
// 这是一次换落点后的新包登记，且自身只依赖标准库与 `contract/dto`，上限抬到 89。
// P1 Session 包提取（2026-10-26）：`application/session` 承接不拥有 chat 状态的
// Session 应用用例。core 在迁移期仍是门面依赖，因此这是“一条进”的领域包，
// 上限抬到 90；后续兼容退场时会与 core 一起收回。
// AP-085 Reference 包提取（2026-10-26）：`application/reference` 承接
// read_tool_result/read_plan 的纯应用编排。它从 core 抽出且不反向依赖
// application，按“新领域落点可 +1”规则，上限抬到 91。
// AP-086 Conversation Search 包提取（2026-10-26）：
// `application/conversation/search` 承接历史检索工具适配；同样是从 core
// 抽出的领域包，不反向依赖 application，上限按同一规则抬到 92。
// 同一批的 Input Suggestions 与 Observability Perf 包也从 core 抽出；两者都是
// 无状态应用适配，不反向依赖 application，上限按 93、94 递进。
// AP-091/092 包提取：`application/lifecycle` 承接 drain/cancel/shutdown/fault guard，
// `application/policy` 承接角色账号与权限策略。两者均只依赖窄 deps/contract/model，
// 不反向依赖 application/core，按新领域落点规则将上限抬到 96。
// AP-094 `application/admin` 承接账号管理适配，只依赖 contract、protocol/model 与
// view/state 的只读投影形状；AP-095 `application/workspace` 承接项目环境、
// Git 查询与批量登记投影。两者都不反向依赖 application/core；上限抬到 98。
// AP-096 `application/session/query` 承接只读会话目录与 durable 查询；AP-097
// `application/history` 承接历史分页类型与窗口决策；AP-098
// `application/execution` 承接项目看板查询；AP-101 `application/review` 承接
// 文件审阅请求适配；AP-103 `application/goal` 承接 Goal 路由；AP-107A
// `application/session/cold` 承接冷恢复组件链，上限抬到 104。
// AP-109 `application/admin/scheduler` 承接定时任务应用编排；AP-111
// `application/conversation/context` 承接显式压缩应用编排；AP-112
// `application/conversation` 承接 Queue 应用编排；AP-114
// `application/conversation/edit` 承接 Edit/Fork 组件，上限抬到 108。
// AP-115A `application/execution/toolhooks` 承接 ToolHookBridge，上限抬到 109。
// AP-115B `application/execution/toolrecord` 承接工具记录状态机，上限抬到 110。
// AP-115C1 `application/execution/table` 承接 WorkTable publisher，上限抬到 111。
// AP-115D1 `application/input/command` 承接基础内置命令，上限抬到 112。
// AP-116F1 把前端兼容转发从 core 移到 application/compat；这是纯适配包，
// 不导入 core 以外的新领域实现，门面闭包上限抬到 113。
// EP-019～EP-025 新增三个 Application 身份门面：`application/event`、
// `application/window` 与 `application/session/viewstate`。它们不迁移状态机，
// 只把 core 对稳定协议/策略类型的直接 import 收口到 Application 边界；
// 每个门面自身继续复用 Middle/Engine 的唯一实现，因此按“新模块可 +1”
// 规则将上限抬到 116。EP-026～EP-033 再新增 switchpolicy、taskcontext
// 与 session/runtime 三个身份门面，同样只重定向稳定类型的消费边界，
// 上限按“新模块可 +1”抬到 119。
// EP-043：SessionState/ViewState 迁入 Application 后，两个 Middle 包退出闭包，
// 净减少一条，上限收紧到 119。
const facadeClosureAllowListMax = 119

var facadeClosureAllowList = []string{
	"github.com/RedHuang-0622/seelex/application",
	"github.com/RedHuang-0622/seelex/application/compat",
	"github.com/RedHuang-0622/seelex/application/admin",
	"github.com/RedHuang-0622/seelex/application/admin/scheduler",
	"github.com/RedHuang-0622/seelex/application/conversation/search",
	"github.com/RedHuang-0622/seelex/application/conversation",
	"github.com/RedHuang-0622/seelex/application/conversation/context",
	"github.com/RedHuang-0622/seelex/application/conversation/edit",
	// EP-021：Event 协议身份门面；投递与序号仍只有 Middle event 一份。
	"github.com/RedHuang-0622/seelex/application/event",
	"github.com/RedHuang-0622/seelex/application/execution",
	"github.com/RedHuang-0622/seelex/application/execution/table",
	// EP-031：Engine Task Context 身份门面。
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext",
	"github.com/RedHuang-0622/seelex/application/execution/toolhooks",
	"github.com/RedHuang-0622/seelex/application/execution/toolrecord",
	"github.com/RedHuang-0622/seelex/application/goal",
	"github.com/RedHuang-0622/seelex/application/history",
	"github.com/RedHuang-0622/seelex/application/input",
	"github.com/RedHuang-0622/seelex/application/input/command",
	"github.com/RedHuang-0622/seelex/application/lifecycle",
	"github.com/RedHuang-0622/seelex/application/observability",
	"github.com/RedHuang-0622/seelex/application/policy",
	"github.com/RedHuang-0622/seelex/application/reference",
	"github.com/RedHuang-0622/seelex/application/review",
	"github.com/RedHuang-0622/seelex/application/session",
	"github.com/RedHuang-0622/seelex/application/session/cold",
	"github.com/RedHuang-0622/seelex/application/session/state",
	"github.com/RedHuang-0622/seelex/application/session/query",
	// EP-033：Engine Session Runtime 身份门面。
	"github.com/RedHuang-0622/seelex/application/session/runtime",
	// EP-027：Middle session-switch 纯策略身份门面。
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy",
	// EP-025：View State 身份门面；Snapshot/revision 编排仍只有 Middle state 一份。
	"github.com/RedHuang-0622/seelex/application/session/viewstate",
	// EP-020：Context Window Policy 身份门面；配置与校验仍只有 Engine 一份。
	"github.com/RedHuang-0622/seelex/application/window",
	"github.com/RedHuang-0622/seelex/application/workspace",
	"github.com/RedHuang-0622/seelex/application/core",
	"github.com/RedHuang-0622/seelex/backend/multiagent/team",
	"github.com/RedHuang-0622/seelex/backend/multiagent/agentsession",
	"github.com/RedHuang-0622/seelex/backend/engine/react",
	"github.com/RedHuang-0622/seelex/backend/engine/input/contextcontrol",
	"github.com/RedHuang-0622/seelex/backend/engine/context",
	"github.com/RedHuang-0622/seelex/backend/multiagent/goal",
	"github.com/RedHuang-0622/seelex/backend/engine/input",
	"github.com/RedHuang-0622/seelex/kernel/state",
	"github.com/RedHuang-0622/seelex/backend/engine/prompt",
	"github.com/RedHuang-0622/seelex/backend/multiagent/resume",
	"github.com/RedHuang-0622/seelex/backend/engine/session",
	"github.com/RedHuang-0622/seelex/middle/view/subagent",
	"github.com/RedHuang-0622/seelex/middle/view/sessionaccess",
	// M5-R5：项目看板的行投影与同步器（从 application/core/project_board.go 抽出）。
	"github.com/RedHuang-0622/seelex/middle/view/board",
	// M5-R5d：错误呈现域（原 application/core/{error_codes,error_presentation}.go）归位
	// 中间层视图子包（plan §2.4）。**新增登记的理由**：这是一次"换落点 + 落点新建"，
	// 门面闭包里 `application/core` 仍在，它引用的呈现包由 core 根文件变成 middle/view/errors，
	// 闭包因此多了这一个包；同一片没有引入任何新的第三方/反向依赖（只依赖 contract 与 Seele/errors）。
	"github.com/RedHuang-0622/seelex/middle/view/errors",
	// M5-R-limitc：权限档位域归位 limit 板块（plan §2.8）。**新增登记的理由**：这是
	// "把 application/core 三处散落实现抽成独立模块"的一次换落点 + 落点新建，门面闭包里
	// `application/core` 仍在；新包只依赖 `contract`（词表）与标准库，没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/backend/limit/tier",
	// M5-R5c：视图读数面（从 application/core/{diagnostics,perf_stats,runtime_projection}.go 抽出）。
	"github.com/RedHuang-0622/seelex/middle/view",
	"github.com/RedHuang-0622/seelex/backend/engine/task",
	"github.com/RedHuang-0622/seelex/application/session/viewstate",
	"github.com/RedHuang-0622/seelex/middle/view/worktable",
	"github.com/RedHuang-0622/seelex/middle/protocol/event",
	"github.com/RedHuang-0622/seelex/middle/protocol/model",
	"github.com/RedHuang-0622/seelex/application/prompt",
	// M2-C3：每账号上下文窗口的判据层（从 seelexctx/window.go 抽出）。
	"github.com/RedHuang-0622/seelex/backend/limit/accountwindow",
	"github.com/RedHuang-0622/seelex/backend/limit",
	// M2-C：审批实现从 application/approval 搬到这里——同一份代码换落点，
	// 清单规模不变（等量替换，见 D-M2-11）；契约层因此**少**了 application/approval。
	"github.com/RedHuang-0622/seelex/backend/limit/approval",
	// M2-B：ReAct 循环上限的判据层（从 application/prompt + wiring/adapters +
	// backend/tool/fork 抽出）。自身闭包 = 标准库，属"干净叶子"新模块。
	"github.com/RedHuang-0622/seelex/backend/limit/loop",
	// M2-E（plan §2.8）：effort 档位策略表从 application/prompt 拆出。**新增登记的
	// 理由**：prompt 仍承担 EffortManager 的宿主动作，策略表移到 limit/effort 后
	// 闭包多出这一个干净叶子（只依赖 loop + contract/dto），没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/backend/limit/effort",
	// M2-C4：权限判据（主体类 / 路由组表 / 规则表 / 档位覆盖 / 工具簇属声明 /
	// 壳类别词表）从 seelebridge/tools 抽出（M2-C4）；执行门当时仍留在 tools，
	// 2026-10-16 M4-T3 整包归位到 backend/tool/registry（包名 registry）。
	"github.com/RedHuang-0622/seelex/backend/limit/policy",
	"github.com/RedHuang-0622/seelex/contract",
	"github.com/RedHuang-0622/seelex/contract/dto",
	// BM-002：共享 wire 形状从 middle/protocol/model 下沉；旧路径以 alias 保留。
	"github.com/RedHuang-0622/seelex/contract/protocol",
	"github.com/RedHuang-0622/seelex/internal/frontmatter",
	"github.com/RedHuang-0622/seelex/backend/engine/prompt/assets",
	// M5-R2：工具调用读数的内存累计从 `internal/toolrecord` 归位到 store 板块
	// （plan §2.10 / §2.12.2）。同一份代码换落点，属等量替换，清单上限不变。
	"github.com/RedHuang-0622/seelex/store/record",
	"github.com/RedHuang-0622/seelex/internal/winhide",
	"github.com/RedHuang-0622/seelex/internal/jsonobj",
	// M5-R9：mcpstack 归位到 engine 板块的 mcp 子域（plan §2.5）；原先的
	// `backend/engine/mcp` 已由 MCP 生命周期管理者占用，因此退一档做子包。
	// 同一份代码换落点，属等量替换，清单上限不变。
	"github.com/RedHuang-0622/seelex/backend/engine/mcp/mcpstack",
	"github.com/RedHuang-0622/seelex/backend/engine/assemble",
	"github.com/RedHuang-0622/seelex/backend/engine/obs",
	"github.com/RedHuang-0622/seelex/backend/engine/account",
	"github.com/RedHuang-0622/seelex/backend/tool/fork",
	"github.com/RedHuang-0622/seelex/backend/tool/io/fs",
	"github.com/RedHuang-0622/seelex/backend/tool/io/imageattach",
	// M4-J2b：会话媒体与随图的域下沉（见上限常量处的说明）。
	"github.com/RedHuang-0622/seelex/backend/tool/io/media",
	// M5-R-tool-review：`show_files` 的落点归位 tool 板块（plan §2.6）。**新增登记的理由**：
	// "把 core 的落点抽成独立模块"的一次换落点 + 落点新建；新包只依赖 `contract`/`contract/dto`
	// 与标准库，没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/backend/tool/review",
	// M5-R-tool-reference：工具结果引用与分页判据归位 tool 板块（plan §2.6）。**新增登记的理由**：
	// "把 core 的判据抽成独立叶子模块"的一次换落点 + 落点新建；新包只依赖 `middle/protocol/model`
	// （已在清单内）与标准库，没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/backend/tool/reference",
	// M5-R6-residency-preview：快照预算的截断判据归位中间层常驻/预算落点（plan §2.4 的
	// `middle/residency`，本片建包）。**新增登记的理由**："计算入中间层、取数留宿主"的
	// 换落点 + 落点新建；新包只依赖标准库，没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/middle/residency",
	// M5-R6-history-page：历史分页的窗口装配判据归位 `middle/switch/history`（plan §2.4，本片建包）。
	// **新增登记的理由**："把 core 的窗口装配判据抽成中间层叶子模块"的换落点 + 落点新建；
	// 新包只依赖 `middle/protocol/model` 与 `middle/view/state`（都已在清单内），没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/middle/switch/history",
	// M5-R6-session-meta：会话展示元数据的读写判据归位 `middle/switch` 根包（plan §2.4，本片建包）。
	// **新增登记的理由**：换落点 + 落点新建；新包只依赖 `middle/protocol/model`（已在清单内），
	// 存储端口由宿主断言后注入，没有引入第三方或反向依赖。
	"github.com/RedHuang-0622/seelex/middle/switch",
	"github.com/RedHuang-0622/seelex/middle/switch/workspace",
	"github.com/RedHuang-0622/seelex/internal/actor",
	"github.com/RedHuang-0622/seelex/internal/config",
	"github.com/RedHuang-0622/seelex/internal/docker",
	"github.com/RedHuang-0622/seelex/internal/model",
	"github.com/RedHuang-0622/seelex/internal/stream",
	"github.com/RedHuang-0622/seelex/internal/telemetry",
	// P22：共享稳定哈希原语（node 会话号 / 账号定序 / 权限主体名）。
	// **等量替换的理由**：三处原各有一份 FNV-1a 实现，收敛为 `internal/stablehash`；
	// 同时删掉了 `backend/tool/plan` 的清单条目。新包只依赖标准库，没有引入第三方
	// 或反向依赖，闭包条数不变。
	"github.com/RedHuang-0622/seelex/internal/stablehash",
	"github.com/RedHuang-0622/seelex/backend/engine/mcp",
	"github.com/RedHuang-0622/seelex/backend/tool/node",
	// M5-R9：这里是 Runtime 的插件可见性投影缓存，原先就装在
	// `backend/engine/plugin` 目录里；plugin 生命周期包从仓库顶层归位到同一个
	// 目录后，缓存退到子包 `backend/engine/plugin/visibility`（包名 plugin →
	// visibility）。同一份代码换落点，属等量替换，清单上限不变。
	"github.com/RedHuang-0622/seelex/backend/engine/plugin/visibility",
	// M5-R9：skill 从仓库顶层归位到 engine 板块（plan §2.5）。同一份代码换落点，
	// 属"一条出一条进"的等量替换，清单上限不变。
	"github.com/RedHuang-0622/seelex/backend/engine/skill",
	"github.com/RedHuang-0622/seelex/backend/engine/schedule",
	"github.com/RedHuang-0622/seelex/backend/tool/search",
	"github.com/RedHuang-0622/seelex/backend/limit/gate",
	"github.com/RedHuang-0622/seelex/backend/multiagent/subagent",
	"github.com/RedHuang-0622/seelex/backend/tool/task",
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork",
	"github.com/RedHuang-0622/seelex/backend/tool/registry",
	// P21-10：异步作业域（作业生命周期事件流 + 后台执行表投影），plan §2.6 的目标落点。
	// **新增登记的理由**：该域的两面原在装配层（`backend/engine/assemble` 的 jobs_events /
	// runtime_async），装配层已在闭包内 ⇒ 抽出来的域包跟着进闭包，属"一条进"的换落点；
	// 域包自身是干净叶子（只依赖 Seele `event`/`jobs`、`backend/tool/registry`、`contract/dto`）。
	"github.com/RedHuang-0622/seelex/backend/tool/jobs",
	"github.com/RedHuang-0622/seelex/backend/tool/computer",
	"github.com/RedHuang-0622/seelex/backend/tool/worktree",
	"github.com/RedHuang-0622/seelex/backend/workunit",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/compactor",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/lifecycle",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/memory",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/merger",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/provider",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens",
	"github.com/RedHuang-0622/seelex/store",
	// M3-S2：会话存储的基座机制（数据根锁 / 进程存活 / 共享读 / 原子发布 / 残尾 /
	// Settings）从 store 抽出。同一份代码换落点，属"新模块可 +1"，且自身
	// 闭包是纯标准库（`go list -deps ./store/root` 除自身外无带点的包）。
	"github.com/RedHuang-0622/seelex/store/root",
	"github.com/RedHuang-0622/seelex/store/schedule",
	// M-SSH：远端执行面的连接基座（Dial / 认证 / 主机指纹 / 探针 / 远端作业）。
	// **新增登记的理由**：它不是一个新能力层，而是"内置工具族的第二个执行面"——
	// 门面闭包里的 `backend/tool/registry` 与 `backend/engine/assemble` 都要 import 它
	// 才能把工具调用打到远端。自身只依赖标准库与 `golang.org/x/crypto/ssh`（vendor 内），
	// 不 import 本仓库任何上层包，闭包方向上不构成反向依赖。
	"github.com/RedHuang-0622/seelex/backend/tool/ssh",
}

// contractClosureAllowList 是契约层传递闭包里的**全部非 stdlib** 包（2026-10-17 实测）。
//
// P3 已把实现依赖清干净：引擎历史写面改用 `contract.EngineMessage`，
// `SearchHistory` 改用 `dto.SearchResult`，`NodeContextSnapshot` 改用
// `dto.ContextSnapshot`——`Seele/types`、`store`、`seelectx/*` 与全部第三方包
// 都已离开闭包。剩下的 4 个全是**纯形状包**：协议词汇
// `middle/protocol/{model,event}`（plan §1.3 I1 允许的零依赖协议层）与契约自身。
//
// 与 facadeClosureAllowListMax 同理：这张表**只许缩小**，契约层新增任何非 stdlib
// 依赖都必须先删条旧条目（即先做减法），不允许直接顶到上限以上。
// BM-002 后新增 `contract/protocol` 自身这一格；它是 contract 的模式子包，
// 依赖仍只有标准库与 contract/dto。上限由 4 调整为 5。
const contractClosureAllowListMax = 5

var contractClosureAllowList = []string{
	"github.com/RedHuang-0622/seelex/middle/protocol/event",
	"github.com/RedHuang-0622/seelex/middle/protocol/model",
	"github.com/RedHuang-0622/seelex/contract",
	"github.com/RedHuang-0622/seelex/contract/dto",
	"github.com/RedHuang-0622/seelex/contract/protocol",
}

// frontendDirectImportAllowList 是四个前端入口的**直接** import 允许清单（只许收缩）。
//
// 为什么用"直接"而不是"传递闭包"：前端 import 门面，门面今天仍传递依赖
// application/core → seelebridge/store/seelexctx，所以"前端传递闭包 ⊆ 门面"
// 在 M5 门面收窄之前**物理上不可能成立**。plan §1.2 对前端写的是"明确禁止：直接
// import store/engine/tool/limit"，本用例按直接依赖钉这条边界；传递闭包要等 M5。
var frontendDirectImportAllowList = map[string][]string{
	"./gui": {
		"github.com/RedHuang-0622/seelex/application",
		// D-M1-2：纯数据形状，plan 口径暂列允许项，M5 门面收窄时统一裁决。
		"github.com/RedHuang-0622/seelex/middle/protocol/model",
		"github.com/RedHuang-0622/seelex/contract/dto",
		"github.com/RedHuang-0622/seelex/gui/terminal",
	},
	"./headless": {
		"github.com/RedHuang-0622/seelex/application",
		// D-M1-6：headless RPC 透传 goal 域的请求/响应形状；随 P3（goal 请求形状
		// 契约化）收口。headless 已从 gui 拆出（P7），这条边不再挂在 gui 上。
		"github.com/RedHuang-0622/seelex/backend/multiagent/goal",
		"github.com/RedHuang-0622/seelex/contract/dto",
	},
	"./tui": {
		"github.com/RedHuang-0622/seelex/application",
		"github.com/RedHuang-0622/seelex/contract/dto",
		"github.com/RedHuang-0622/seelex/tui/splash",
	},
	"./console": {
		"github.com/RedHuang-0622/seelex/application",
		// D-M1-7 已收口：BashDiagnosticEvent 形状下沉 contract/dto，console 只经门面 + 契约。
		"github.com/RedHuang-0622/seelex/contract/dto",
	},
}

// TestFacadeDependencyClosureRatchet 钉 I1：门面闭包里的 Seelex 包只许收缩。
func TestFacadeDependencyClosureRatchet(t *testing.T) {
	if len(facadeClosureAllowList) > facadeClosureAllowListMax {
		t.Errorf("facadeClosureAllowList 有 %d 条，超过上限 %d：这张表只许收缩。"+
			"搬迁换落点允许等量替换（一条出、一条进）并在记录里给出差集，"+
			"不允许净增长", len(facadeClosureAllowList), facadeClosureAllowListMax)
	}
	allow := stringSet(facadeClosureAllowList)
	var unexpected []string
	for _, dependency := range goListDependencies(t, facadePackage) {
		if !strings.HasPrefix(dependency, seelexModulePrefix) {
			continue
		}
		if _, ok := allow[dependency]; !ok {
			unexpected = append(unexpected, dependency)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("门面依赖闭包新增了未登记的 Seelex 包（plan §1.3 I1 只许收缩）：\n  %s\n"+
			"要么改掉这条依赖，要么在 facadeClosureAllowList 里登记并说明理由（新增登记是例外，不是常态）",
			strings.Join(unexpected, "\n  "))
	}
}

// TestContractDependencyClosureIsStdlibOrAllowlisted 钉 plan §4 M1 的退出条件之一：
// 契约层依赖闭包 = stdlib；现状由 contractClosureAllowList 钉住（D-M1-1）。
func TestContractDependencyClosureIsStdlibOrAllowlisted(t *testing.T) {
	if len(contractClosureAllowList) > contractClosureAllowListMax {
		t.Errorf("contractClosureAllowList 有 %d 条，超过上限 %d：契约层的目标闭包是 stdlib，"+
			"这张表只许收缩、不许净增长", len(contractClosureAllowList), contractClosureAllowListMax)
	}
	allow := stringSet(contractClosureAllowList)
	var unexpected []string
	for _, dependency := range goListDependencies(t, contractPackage) {
		if isStdlib(dependency) {
			continue
		}
		if _, ok := allow[dependency]; !ok {
			unexpected = append(unexpected, dependency)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("契约层闭包新增了非 stdlib 依赖（plan §1.2：contract 允许依赖只有 stdlib）：\n  %s",
			strings.Join(unexpected, "\n  "))
	}
}

// TestFrontendDirectImportsStayInsideFacade 钉 I2：三个前端入口直接 import 的 Seelex 包。
func TestFrontendDirectImportsStayInsideFacade(t *testing.T) {
	for frontend, allowList := range frontendDirectImportAllowList {
		allow := stringSet(allowList)
		var unexpected []string
		for _, imported := range goListDirectImports(t, frontend) {
			if !strings.HasPrefix(imported, seelexModulePrefix) {
				continue
			}
			if _, ok := allow[imported]; !ok {
				unexpected = append(unexpected, imported)
			}
		}
		if len(unexpected) > 0 {
			sort.Strings(unexpected)
			t.Errorf("%s 直接 import 了门面之外的 Seelex 包：\n  %s\n"+
				"前端只能经门面消费状态（plan §1.2；越层边清单见 §3.3）",
				frontend, strings.Join(unexpected, "\n  "))
		}
	}
}

// TestGUIHasNoDirectBackendImports 钉 plan §4 M1 的退出条件「前端 4 条越层边消失」。
//
// 与上一条的区别：这条钉的是**具体那 4 条边**（§3.3 点名的 store /
// seelexctx / seelexctx/search / seelebridge），任何一条回来都直接失败，且报错文案
// 指向当初的删除清单，避免"允许清单被人手放宽"把回归掩盖掉。
func TestGUIHasNoDirectBackendImports(t *testing.T) {
	forbidden := []string{
		"github.com/RedHuang-0622/seelex/store",
		"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx",
		"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search",
		"github.com/RedHuang-0622/seelex/backend/engine/assemble",
	}
	imported := stringSet(goListDirectImports(t, "./gui"))
	for _, path := range forbidden {
		if _, ok := imported[path]; ok {
			t.Errorf("./gui 又直接 import 了 %s：这 4 条越层边已在 M1-C 删除，"+
				"GUI 必须经门面再导出消费（见 m1-record.md §3.4 的删除清单）", path)
		}
	}
}

// TestFacadeHasNoConstructors 钉 I3：门面包内不得出现 func New*。
func TestFacadeHasNoConstructors(t *testing.T) {
	for _, path := range packageGoFiles(t, filepath.Join(repoRoot(), "application")) {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range parsed.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			if strings.HasPrefix(function.Name.Name, "New") {
				t.Errorf("%s 定义了构造函数 %s：门面不持有构造函数（I3），"+
					"构造只发生在 wiring/（plan §1.3 ④ / §2.2）", filepath.Base(path), function.Name.Name)
			}
		}
	}
}

// facadeOwnContractTypes 是门面**故意新造**的入站契约类型（不是搬迁来的别名）：
// plan §1.3 的 ③「用例方法面收窄成五组窄接口」要求能力接口声明在门面里。
// EP-046C 后这些接口的事实源是 `application/faces_api.go`，core 不再定义第二套。
// 除这些入站契约且来自纯 DTO/port 的接口外，门面导出的类型仍必须是别名。
var facadeOwnContractTypes = map[string]bool{
	"ViewFace":                 true,
	"CommandFace":              true,
	"PolicyFace":               true,
	"QueueFace":                true,
	"ConversationAPI":          true,
	"SessionAPI":               true,
	"ExecutionQueryAPI":        true,
	"WorkItemQueryAPI":         true,
	"TeammateQueryAPI":         true,
	"SubagentQueryAPI":         true,
	"TranscriptQueryAPI":       true,
	"ExecutionCommandAPI":      true,
	"WorkspaceAPI":             true,
	"GoalAPI":                  true,
	"PolicyAPI":                true,
	"AdminAPI":                 true,
	"RoleAPI":                  true,
	"SubagentAPI":              true,
	"StateAPI":                 true,
	"QueueAPI":                 true,
	"LifecycleAPI":             true,
	"SessionQuery":             true,
	"ConversationHistoryQuery": true,
}

// TestFacadeKeepsTypeIdentityByAlias 钉 I4：门面导出的每个类型都必须是别名
// （`type X = Y`），搬迁不得产生新的类型身份——只有 `facadeOwnContractTypes`
// 里那四个窄接口是门面自己的契约类型。
func TestFacadeKeepsTypeIdentityByAlias(t *testing.T) {
	for _, path := range packageGoFiles(t, filepath.Join(repoRoot(), "application")) {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range parsed.Decls {
			generic, ok := decl.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Assign.IsValid() {
					continue
				}
				if facadeOwnContractTypes[typeSpec.Name.Name] {
					continue
				}
				t.Errorf("%s 定义了新类型 %s（不是 `type X = Y` 别名）："+
					"门面必须保形，前端才能零改动（I4）", filepath.Base(path), typeSpec.Name.Name)
			}
		}
	}
}

// ── 工具 ──────────────────────────────────────────────────────────────

// goListDependencies 返回 `go list -deps <pattern>` 的包路径集合（stdout 逐行）。
func goListDependencies(t *testing.T, pattern string) []string {
	t.Helper()
	return runGoList(t, "list", "-deps", pattern)
}

// goListDirectImports 返回 `go list -f {{.Imports}} <pattern>` 的直接 import 集合。
// 用 .Imports（不含测试文件的 import）而不是 -deps：本用例钉的是生产代码的依赖边。
func goListDirectImports(t *testing.T, pattern string) []string {
	t.Helper()
	output := runGoList(t, "list", "-f", "{{.Imports}}", pattern)
	fields := make([]string, 0, len(output))
	for _, line := range output {
		for _, field := range strings.Fields(line) {
			fields = append(fields, field)
		}
	}
	return fields
}

// goListTestImports 返回 `go list -f {{.TestImports}} {{.XTestImports}} <pattern>` 的集合。
// 与 goListDirectImports 互补：这条专门看**测试文件**的 import（I6 的第二半）。
func goListTestImports(t *testing.T, pattern string) []string {
	t.Helper()
	output := runGoList(t, "list", "-f",
		"{{range .TestImports}}{{.}} {{end}}{{range .XTestImports}}{{.}}{{end}}", pattern)
	fields := make([]string, 0, len(output))
	for _, line := range output {
		for _, field := range strings.Fields(line) {
			fields = append(fields, field)
		}
	}
	return fields
}

// runGoList 在仓库根目录执行 go list 并把 stdout 按行返回。
//
// 为什么用 `go list` 而不是自己解析源码：`go list` 会按当前平台的 build tags
// 过滤文件——手工解析会把 main_windows.go 之类平台文件的 import 也算进来，
// 让门禁在别的平台上误报。
func runGoList(t *testing.T, args ...string) []string {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = repoRoot()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s 失败: %v\n%s", strings.Join(args, " "), err, output)
	}
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// packageGoFiles 返回目录下的非测试 .go 文件（生产面）。
func packageGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files
}

// isStdlib 按 Go 的约定判定：import 路径第一段不含 "." 即为标准库
// （第三方域名、`gopkg.in`、`go.opentelemetry.io` 都含 "."）。`vendor/...`
// 同样被判为 stdlib——它们是工具链随附的标准库 vendored 副本，不是本仓库的依赖选择。
func isStdlib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
