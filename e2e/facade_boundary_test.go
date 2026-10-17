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
const facadeClosureAllowListMax = 83

var facadeClosureAllowList = []string{
	"github.com/RedHuang-0622/seelex/application",
	"github.com/RedHuang-0622/seelex/application/core",
	"github.com/RedHuang-0622/seelex/backend/multiagent/team",
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
	"github.com/RedHuang-0622/seelex/middle/view/state",
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
	// M2-C4：权限判据（主体类 / 路由组表 / 规则表 / 档位覆盖 / 工具簇属声明 /
	// 壳类别词表）从 seelebridge/tools 抽出（M2-C4）；执行门当时仍留在 tools，
	// 2026-10-16 M4-T3 整包归位到 backend/tool/registry（包名 registry）。
	"github.com/RedHuang-0622/seelex/backend/limit/policy",
	"github.com/RedHuang-0622/seelex/contract",
	"github.com/RedHuang-0622/seelex/contract/dto",
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
	"github.com/RedHuang-0622/seelex/internal/actor",
	"github.com/RedHuang-0622/seelex/internal/config",
	"github.com/RedHuang-0622/seelex/internal/docker",
	"github.com/RedHuang-0622/seelex/internal/model",
	"github.com/RedHuang-0622/seelex/internal/stream",
	"github.com/RedHuang-0622/seelex/internal/telemetry",
	"github.com/RedHuang-0622/seelex/backend/engine/mcp",
	"github.com/RedHuang-0622/seelex/backend/tool/node",
	"github.com/RedHuang-0622/seelex/backend/tool/plan",
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
	"github.com/RedHuang-0622/seelex/session",
	"github.com/RedHuang-0622/seelex/store",
	// M3-S2：会话存储的基座机制（数据根锁 / 进程存活 / 共享读 / 原子发布 / 残尾 /
	// Settings）从 store 抽出。同一份代码换落点，属"新模块可 +1"，且自身
	// 闭包是纯标准库（`go list -deps ./store/root` 除自身外无带点的包）。
	"github.com/RedHuang-0622/seelex/store/root",
	"github.com/RedHuang-0622/seelex/store/schedule",
}

// contractClosureAllowList 是契约层传递闭包里的**全部非 stdlib** 包（2026-10-16 实测）。
// plan 的目标是这张表最终清空（闭包 = stdlib）；D-M1-1 记录了为什么本轮达不到
// （状态 owner、产品 A/B 模型与 scope 未裁决，见 plan §5.12）。
//
// 与 facadeClosureAllowListMax 同理：这张表**只许缩小**，契约层新增任何非 stdlib
// 依赖都必须先删条旧条目（即先做减法），不允许直接顶到上限以上。
// M2-C：`application/approval` 的形状搬进 contract 后，契约层闭包少了一个包
// （29 → 28）——上限跟着降，把这次收缩钉住。
//
// M3-S2 的例外（记在 m3-record.md）：`store/root` 不是契约层新添的依赖，而是
// `contract/ports.go → seelexctx/search → store → store/root` 这条**既有**
// 污染链上多出的一个名字（D-M1-1 的闭包仍未收口，待裁决）。它是纯 stdlib 叶子，
// 因此按"新模块可 +1"登记，上限 28 → 29；契约层闭包本身没有新增任何真实依赖。
const contractClosureAllowListMax = 29

var contractClosureAllowList = []string{
	"github.com/RedHuang-0622/Seele/errors",
	"github.com/RedHuang-0622/Seele/event",
	"github.com/RedHuang-0622/Seele/seelectx",
	"github.com/RedHuang-0622/Seele/seelectx/cache",
	"github.com/RedHuang-0622/Seele/seelectx/ctx_manager",
	"github.com/RedHuang-0622/Seele/seelectx/storage",
	"github.com/RedHuang-0622/Seele/telemetry",
	"github.com/RedHuang-0622/Seele/types",
	"github.com/RedHuang-0622/Seele/workplan/core/types",
	"github.com/RedHuang-0622/seelex/middle/protocol/event",
	"github.com/RedHuang-0622/seelex/middle/protocol/model",
	"github.com/RedHuang-0622/seelex/contract",
	"github.com/RedHuang-0622/seelex/contract/dto",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/lifecycle",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/memory",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot",
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens",
	"github.com/RedHuang-0622/seelex/store",
	// M3-S2：见 contractClosureAllowListMax 的说明。
	"github.com/RedHuang-0622/seelex/store/root",
	"github.com/cespare/xxhash/v2",
	"go.opentelemetry.io/otel/attribute",
	"go.opentelemetry.io/otel/attribute/internal",
	"go.opentelemetry.io/otel/attribute/internal/xxhash",
	"go.opentelemetry.io/otel/codes",
	"go.opentelemetry.io/otel/semconv/v1.41.0",
	"go.opentelemetry.io/otel/trace",
	"go.opentelemetry.io/otel/trace/embedded",
	"go.opentelemetry.io/otel/trace/internal/telemetry",
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

// facadeOwnContractTypes 是门面**故意新造**的契约类型（不是搬迁来的别名）：
// plan §1.3 的 ③「用例方法面收窄成五组窄接口」要求四组行为面接口声明在门面里
// （`application/faces.go`，M5-R10a 契约 §1.1）。除这四个之外，I4 仍然一句不让：
// 门面导出的类型必须是 `type X = Y` 别名，搬迁不得产生新的类型身份。
var facadeOwnContractTypes = map[string]bool{
	"ViewFace":    true,
	"CommandFace": true,
	"PolicyFace":  true,
	"QueueFace":   true,
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
