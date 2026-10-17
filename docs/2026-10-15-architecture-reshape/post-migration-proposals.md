# 迁移后提案汇总（Post-migration proposals）

日期：2026-10-16（2026-10-16 补入用户逐条裁决）
状态：**提案清单，不是已实现**。本文件是"架构重排（[plan.md](plan.md)）期间发现、但按纪律
不该混进搬迁一起做"的事项的**唯一汇总处**——每条写清：涉及哪些包、现在什么流程、建议改成
什么流程、两者差在哪、建议是什么，以及**用户裁决**。

维护约定：

- 搬迁期间新发现的同类事项**只往本文件加行**，不夹带进搬迁切片（plan §5.8「一次一主题」）。
- 状态只有四种：`待裁决` / `已裁决`（用户已定，写结论） / `建议做`（我的建议 + 判据） / `建议不做`（附理由）。
- 每条都要能指向证据（`文件:行`、命令输出、或 `m*-record.md` 里的 D-* 编号）。

---

## 1. 汇总表

| ID | 提案 | 涉及包 | 现在 | 建议 | 差异与风险 | 我的建议 | 状态 |
|---|---|---|---|---|---|---|---|
| P1 | 路径门（ProjectScope）前移到权限判定之前 | `backend/limit/gate`（ProjectScope）、`backend/tool/registry`（PermissionGate 中间件）、`wiring/bootstrap`（装配顺序） | 事件 → **权限判定（ask 就地弹审批）** → 诊断 → handler 里才查路径越界 | 事件 → 主体解析 → **路径越界检查** → 权限判定 → 审批 | 越界路径今天会**先弹一次没用的审批**；换序后直接拒、不弹窗。错误文案与"谁先拒"改变（安全相关路径） | 做，**放在搬迁收口之后**，只移路径门、不拆审批 | **已修复（见 [p-items-record.md](p-items-record.md) §P1，`aabb311d`）** |
| P2 | loop 预警：预算用尽后 **+1 次总结调用** + 设置面板「无限循环」勾选 | `backend/limit/loop`（**看这里**：档位轮数/预算/上限判据）、ReAct 主循环（现 `application/core/chat.go` → 终局 `backend/engine/react`）、设置面板（GUI/TUI 的"循环次数"档位处）、`wiring`（注入） | 只有事后收尾：预算耗尽后 `finalizeReActBudget` 保留一次纯文本交付回合；面板只有"循环次数"数值 | ①预警/用尽属于**主代理（mainagent）**这一层的事，**不涉及 subagent/teammate**；②预算用尽后做 **+1 次调用**，内容就是"总结预算内已经完成的工作"；③设置面板支持**「无限循环」勾选**（勾上即不设上限）；④`workunit` 在预算预警里的角色**待商榷**，先不接 | 这是**新增行为**（多一次调用、多一个面板开关、多一条设置），与搬迁的"同输入同结果"互斥；`workunit` 是否承载总结容器**未定** | 做，但**放在 M5 之后**；判据：同一输入下"预算内工作"的字面命中 + 面板勾选后无上限生效 | **已裁决（语义已更正，按上表「建议」列执行；时机 M5 之后）** |
| P3 | 契约层提纯：`contract` 闭包 = stdlib | `contract`、`contract/dto`、`middle/protocol/{model,event}`、`seelexctx/{search,snapshot}`、Seele `types` | `contract` 仍依赖 Seele `types`、`middle/protocol/*` 形状、`seelexctx/{search,snapshot}`；ratchet 钉住 | 把端口签名要用的形状下沉 `contract`/`contract/dto`，Seele 类型换 engine-owned 消息类型（先拆实际消费字段） | 取决于"状态 owner / 产品 A-B 模型 / scope"三个产品向决定；Seele 类型替换要证明无损 | 做（按建议），但**先拿到那三个决定** | **已裁决（按建议做；前置三项决定）** |
| P4 | `store/` 按 **领域驱动设计（DDD）** 重做 | `store`（现单包 134 生产文件）、`store/{root,workspace,schedule,record}` | 机制层已独立（`store/root` 闭包纯标准库）；域仍同包 | 按**限界上下文**重划包与引用：会话/看板/团队/角色/节点/媒体/legacy 各自成上下文，包引用按依赖方向重排 | **不是搬文件**：`Router`(131)/`jsonRepository`(82)/`storeEngine`(182) 三个类型 395 个方法散在 33/57 文件，Go 要求方法同包 → 必须先**按 DDD 拆类型与上下文**（跨约 110 个外部调用点） | 做，**先做领域建模与类型分解**，再落包 | **已裁决（按 DDD 重做；先建模）** |
| P5 | `backend/engine/assemble` 的 `Runtime` 按域下沉收口 | `backend/engine/assemble`（现 48 生产文件 / 12.5k 行）、各 `backend/*` | 旧根包整体落位（352 方法的跨文件单类型） | 每域：目标包定义**只读 Host 端口** → 域状态与方法随类型走 → assemble 只留持有与挂载；删 `ports.go` | 域差异只能出现在装配表与策略调用点；顺序/关机语义逐字保持 | 做（按计划） | **已裁决（继续按计划）** |
| P6 | `application/core` 拆分 + `Service` 门面收窄五组窄接口（+I5） | `application/core`（69 生产文件、`Service` 419 方法）、`middle/*`、`backend/*`、`kernel/state`、门面 `application` | 单包承载全部用例面；门面是大平面 | **按 DDD**：先 `kernel/state` 共享内核 → 域状态下沉 → 跨域读写走消费方接口；门面收窄成 A/B/C/D/E 五组 | 顺序错了会成环；68% 方法是未导出的，不能沿公共 API 切 | 做（按计划） | **已裁决（按 DDD 拆；按计划推进）** |
| P7 | 前端收口三件 | `gui/**`（`headless*.go`、注释/README 旧路径）、`application/console` → `console/`、`tui` | 仍在原地；注释与 README 有旧路径（D-M3-13） | 按 plan §2.1：`headless/` 独立、`console/` 顶层化、旧路径注释一次对齐 | 前端行为零改动 | 做（按建议），搬迁收尾后一批 | **已裁决（按建议做）** |
| P8 | 契约 vs 兼容面甄别（别名/转发面清理） | `backend/workunit`、`backend/limit/policy`、`store`、`middle/protocol/*`、`internal/*` | 搬迁期留了一层 `type X = Y` 别名与转发（`GroupBit*`、`NodeScope`、`Settings`、`StoreFile` 等） | **先判性质**：①**契约抽象**（如 `workunit` = 后台工作 agent 的抽象，含 subagent/teammate，并为未来后台 agent 方案（含规划中的远程 agent 调用）提供契约；`policy` = 多条件 select 出 bool 决定权限的统一抽象与契约）→ 保留并归位；②**兼容面**（只为不动调用点而存在的转发/别名）→ 删；③**冗余或僵尸面** → **先暴露清单与详解，交用户决定**，不自行删 | 判错性质会把契约当垃圾删掉，或把垃圾当契约供起来 | 按上述三类处置；**每一条都要先给"为什么是这一类"的判据与证据** | **已裁决（按三类甄别；冗余/僵尸先暴露给用户决定）** |
| P9 | 错误文案统一 | `backend/engine/assemble/events_unified.go`（原 `seelebridge/events_unified.go`；提案原文写的 `backend/engine/obs` 是**错位置**，已按 `rg` 更正） | 文案里仍写 `"unified events: sessionstore router is not attached"`（D-M3-12），两处各写一份 | 改成当前包名措辞，并归到**一处**（两个入口共用同一句） | 用户可见面；改则同步用例 | 做（按建议），单独一个 commit | **已修复（见 [p-items-record.md](p-items-record.md) §P9）** |
| P10 | **判据漂移（较高优先级 bug）**：`isSessionNotFound` 多份实现 | `store/durable_history.go`、`seelexctx/search/search.go`、`application/core/session_runtime/archive.go`、`wiring/adapters/session_workspace_ports.go`（终局 `backend/engine/context/seelectx/search`） | 实测漂移面是**7 处、4 个包**（比提案写的"两份"更宽）：store / search 各一个同名函数，另有两处**内联**副本（archive.go 1 处、adapters 4 处），且语义已分叉（fs-only vs fs+`sql.ErrNoRows`） | 归到**一处**，落点是「未找到」错误的编码者 `store`；语义取 `fs.ErrNotExist`（SQL 后端已 `ErrBackendRetired`，无路径可产生 `sql.ErrNoRows`） | AGENTS §8.3「同一判据两处即缺陷」；不合并会让"会话不存在"在不同路径下两种答案 | 先写会红的用例复现差异，再合并；**并加机械门禁防回潮** | **已修复（见 [p-items-record.md](p-items-record.md) §P10）** |
| P11 | 覆盖缺口：节点预算上限守卫 | `backend/tool/plan`、`backend/engine/assemble` | `docs/arch/ARCHITECTURE_REVIEW.md` 声称为 `plan_input_fuzz_test.go`；复核更正：该文件**存在**（`backend/engine/assemble/plan_input_fuzz_test.go`），但只 fuzz `NormalizePlanLoadArguments`，**不触及** `ValidatePolicyLoad` 的 MaxNodeLoops/MaxNodeOutputTokens 分支 → 覆盖缺口为真、理由不同 | 补用例（原描述的二选一里选"补"） | 不补=文档失真 | 补边界用例，并把文档指向真实用例名 | **已修复（见 [p-items-record.md](p-items-record.md) §P11）** |
| P12 | 文档漂移清理 | `docs/arch/**`、模块 README、`docs/gui/**` | `docs/arch` 约 23 行指向从未存在的旧文件名；README 漂移门禁现存 12 条（与本方案无关） | 逐条改指当前实现或标"已删除" | 不打红 CI（有容忍表），但损害"文档即事实" | 按建议做，可并入 P7 收口片 | **已裁决（按建议判别）** |
| P13 | 「文件与函数索引」生成器全仓化 | `scripts/gen_core_readme_index.py`、全仓 Go 包 README | 生成器只覆盖 `application/core` 分卷与少数登记包 | 提取逻辑扩到全仓，一次补齐 | 逐包手补会漂移 | 按建议做；先确认"中文 doc 注释缺失"包的降级行为 | **已裁决（按建议判别）** |
| P14 | 多入口 / 多变体构建 | `wiring/{config,bootstrap,registry}`、新增 `cmd/<变体>/main.go` | `main.go` 已 36 行 | 有第二入口需求时加 `cmd/<变体>/main.go`，不复制 main | 现在没有真实第二入口 | 按建议：先不做，等需求 | **已裁决（按建议判别）** |
| P15 | `store/root` test-only 导出收敛 | `store/root`、`store`（集成用例） | 为让集成用例伪造/replay 锁文件，多导出 6 项（D-M3-8） | 用例改行为级断言后可收回导出 | 取消导出会逼用例复制锁文件格式（第二份实现） | 按建议：先不做 | **已裁决（按建议判别）** |
| P16 | `store` 的 `RouterStorage` 端口反转 | `store`、`seelexctx/lifecycle`（向上依赖已在 M3 删除） | M3 用"断言整条上移消费侧"（端口化实测成环，D-M3-11） | 若将来要下沉契约层，先解环 | 无第二消费方 | 建议不做（除非出现第二消费方） | 建议不做 |
| P17 | 工具记录看板的"整机汇总"读面 | `store/record`、`middle/view/record`、钩子 | 用户裁决：读数**不进 Snapshot**、钩子**直连 JSONL** | 若真需"这台机器哪个工具最该优化"：加显式汇总（读多份 JSONL），不进快照 | 多一层聚合与缓存口径 | 按需实现 | 已裁决（V10） |
| P18 | 进草稿的**字段清理口径三处不一致** | `application/core/session_draft.go`（`BeginNewSession` → `middle/switch/draft.go`）、`session_history.go`（`resetViewToDraftAfterRestoreFailure`）、`session_lifecycle.go`（`switchViewToFreshDraftLocked`） | 三条"把视图变成草稿"的路径各清自己的字段集合：`ReadFiles` 只有两条清、`HistoryOffset/TotalMessages/HasMoreHistory` 只有 `BeginNewSession` 清、`SetSessionTitleLocked` 只有两条调 | 把"进草稿要清哪些格"收成**一份判据**（`DraftShellReset`），三条路径都调它 | 三处**可能**是故意的（冷恢复失败路径要保留窗口读数）；合并＝行为变更，必须先逐格确认 | 先做逐格差异确认（我已在 `m5-record.md` §30 列出三处差集），确认后再合并 | **等你决定** |
| P19 | 历史名残留：`seelebridge:` 错误文案 + `seelebridge.` 包别名 | `backend/engine/assemble`（文案）、`wiring/registry/registry.go`（`seelebridge "…/assemble"` 别名）、`gui/bridge_test.go`（同款别名） | `seelebridge/` 目录已删，但错误文案仍写 `seelebridge: scheduler unavailable`，装配与 gui 测试仍用 `seelebridge` 当包别名 | 文案改成当前包名措辞；别名改成 `assemble`（纯改名，零行为） | 文案是**用户可见**面（改＝行为变更，要同步用例）；别名在 `gui/**`（I5 冻结面）→ 动它就要用户放行前端改动 | 拆两步：先改别名（需你放行 `gui/bridge_test.go` 一行）与文案（单独 commit），时机放在 M5 之后 | **等你决定** |
| P20 | `application/core` 全量测试**高负载偶发挂死** | `application/core`（`SnapshotView` / Coordinator 的 `RWMutex`）、用例侧 | 干净树 `c091dbba` 上 12 次全量跑挂 2 次（`panic: test timed out`，多个 goroutine 卡 `RWMutex.Lock/RLock`）；单跑涉事用例 `TestSnapshotIncludesPersistedSessions` 4.3s 通过 | 专项定位（`-race` + 逐用例二分 + 并发度控制），判定是"跨用例锁交互"还是"真死锁" | 不是本批任一搬迁片引入（**不含本批**的干净树上同样复现）；但它会让 M5 出口验收的 `go test ./...` 不稳定 | 单独立项定位，**在 M5 出口验收前判定**；若确认既有则记入独立缺陷清单 | **建议先定位** |
| P21 | `assemble.Runtime` 公开面仍未收窄（R11 只做到按域分文件） | `backend/engine/assemble`（现 `runtime_*.go` 共 13 个转发/装配文件）、`wiring/adapters`、`gui` | `ports.go` 已归零、各域宿主面有家，但 `Runtime` 仍是 140 个公开方法的大平面（plan §2.5 的终局是 4 个文件） | 收窄 `Runtime` 公开面：域方法随类型走，`Runtime` 只留 deps/bundle/live/拓扑+关机，窄面由 `wiring/adapters` 手写转发持有 | 要同时动 `wiring/adapters` 与 `gui`（前端冻结面）→ 不是"零改动"片 | 排在 M5 之后做（R11b），先出"谁在调 `Runtime` 哪个方法"的普查表 | **等你决定** |
| P22 | **R10b 已实施**（首次尝试中止后重做成功，见 m5-record.md §31/§33）：`application/core/aliases.go` 不是纯别名文件 | `application/core/aliases.go`（234 行）、`application/goal`? 无；实际涉及 `application/core` **167 个文件**与 12 处外部 `core.X` 引用 | 契约 §1.3 写的 R10b 是"删 204 行别名 → core 内改限定名"。实测该文件是**混合体**：91 个类型别名块 + **词表 const 块**（会话状态/事件名/角色名/协议版本）+ **3 个未导出 input 别名** + **8 个构造函数**（`NewEventHub` / `NewApprovalBroker` / `NewPromptStack` / `NewEffortManager` / `NewCommandRegistry` / `NewDefaultWindowPolicy` / `DefaultWindowConfig` / `LoadWindowConfig`）+ **5 个纯转发 helper**（`cloneSnapshot` / `cloneRuntimeState` / `maxLoopsFor` / `reasoningEffortFor` / `reactBudgetFor`） | 拆两步：①**先拆文件**（别名块删；词表 → `vocabulary.go`；构造器与转发 helper → `assembly_helpers.go`，并把 `New*` 一族按 plan §1.3 ④ 迁 `wiring/`）；②再做 core 内的限定名替换（`model.X` / `contract.Y` / `event.Z` / `input_router.W`） | 工作量实测：**91 个别名 / core 内 1736 处引用 / 247 个文件**（含注释与字符串）；纯代码态替换 1536 处 / 167 文件 / 需补 import 245 条；误伤类别有两类（**结构体字段名/字段键**与**方法定义名**），都能被编译器抓住，但要逐个修；外部还有 12 处 `core.X` 要重指向（含 `application/application.go` 的 `Command`/`CommandResult`、`test/`、`wiring/registry`） | 已按"契约前提不实"**中止并回滚**（工作树保持绿）。可复用产物：普查脚本 `tmp/reshape/r10b_census.py`、替换器 `tmp/reshape/r10b_rewrite.py`（含 `--apply` 与 dry-run）、字段修补器 `tmp/reshape/r10b_fix_fields.py`；替换器的 import 分组有 bug（把 `"strings"` 分到外部组），且未定位一处"变量名与包名同名时丢限定符"的现象（`event.Arguments = …` → `Arguments = …`），**修好这两个问题再重做** | **已实施（`909c9f97`）：按"先拆文件、再改限定名"落地；工具缺陷（字段修补器吃掉选择器与多值 case）修好后重跑，1536 处限定名替换 + 167 文件 |

---

## 2. 逐条讲解（流程对比）

### P1 路径门前移（M2-C5）

```text
现在：工具调用 → 事件 → 权限门（簇属守卫 → 主体解析 → 判定：allow/deny 出结果，ask 就地弹审批）
                → 诊断 → handler（Router 里才查 ProjectScope 路径越界）→ 执行
建议：工具调用 → 事件 → 主体解析 → 路径门 → 权限判定 → 审批 →（第五段）循环预警
```

**差异**：唯一真实换序是"路径门从 handler 内提到判定之前"——越界路径不再"先问人再失败"，用户少一次无意义打扰；
错误从"执行失败（路径越界）"变成"作用域被拒"，日志里失败原因文字随之改变。
**实施红线**：主体解析必须仍在路径门之前（否则员工"该提权"的调用会被路径门先拦）；**不拆审批**
（拆了会改审批超时/取消的返回边界）。

### P2 loop 预警（语义已按用户更正）

**谁的事**：这是**主代理（mainagent）**这一层的事——**不涉及 subagent 与 teammate**。

**看哪里**：`backend/limit/loop`（档位轮数、ReAct 预算、有效上限与天花板判据都在这里；这是"loop 的 limit"唯一判据处）。

```text
现在：主代理回合内按 ReAct 预算跑；预算耗尽 → finalizeReActBudget 保留一次纯文本交付回合收尾
     面板：只有"循环次数"一个数值档（application/model 的 LoopLimit，门面 SetLoopLimit）
建议：预算用尽 → 做 +1 次调用，内容 =「总结预算内已经完成的工作」
     面板：在"循环次数"档位处加「无限循环」勾选（勾上 = 不设上限）
     workunit 是否承载总结容器 → 待商榷，本轮不接
```

**差异**：①多一次 LLM 调用（+1 次总结），它花在"汇报预算内成果"而不是继续干活；
②面板多一个开关（勾选后不再因预算被截断）；③这是**新增行为**，必须在搬迁收口后单独一片做、单独验收；
④`workunit`（后台工作 agent 的抽象）与"预算预警"的关系先不绑定——它的语义见 P8。

**验收判据（建议）**：同一份合成回合 + 固定预算 → 断言"预算内已完成的工具/产出"逐条出现在 +1 次总结的输入提示里；
勾上「无限循环」后不再触发该 +1 路径；不勾时行为与今天逐字一致（除新增的那次总结调用）。

### P4 `store/` 按 DDD 重做

**现状**：`store/` 单包 134 个生产文件；机制层 `store/root` 已独立（闭包纯标准库）。
**用户裁决**：按**领域驱动设计**重做 store 的内容与包的引用（不是按文件 mv）。
**必做前置**：先做**限界上下文划分与类型分解**——`Router`(131)/`jsonRepository`(82)/`storeEngine`(182) 是跨文件单类型，
395 个方法必须随类型走；外部约 110 个调用点要按"消费方接口"改造。包引用按依赖方向重排（上层只持端口）。

### P6 `application/core` 按 DDD 拆分（同 P4 口径）

**现状**：`application/core` 69 个生产文件、`Service` 419 个方法（68% 未导出）。
**用户裁决**：超大包按 DDD 拆分，按计划推进。
**必做前置**：先 `kernel/state` 共享内核（谁都能读、只有域能写），再域状态下沉，跨域读写走消费方接口；
最后门面收窄成五组窄接口 + I5 断言。

### P8 契约抽象 vs 兼容面（甄别口径）

用户给出的两个"契约抽象"的定义，必须先按它们判性质：

| 名字 | 用户定义的语义 | 因此 |
|---|---|---|
| `workunit` | **后台工作 agent 的抽象**：包含 subagent 与 teammate，并为**后来的后台工作 agent 方案（含规划中的远程 agent 调用）提供契约** | 这是**契约抽象**：它的接口/哨兵/状态词表要保留并归位；不是兼容面 |
| `policy` | **多条件 select 出 bool 值来确定权限**的操作 = 权限设置的**统一抽象与契约** | 同上：保留并归位；删除只允许发生在"**兼容面**"上 |

**处置规则**（本文件的纪律）：对任何别名/转发/表：

1. 先判它是**契约抽象**还是**兼容面**——判据写清（它是否被多方按语义依赖？是否是未来方案的接口？）；
2. 兼容面 → 删（每删一条记"原位置 → 新唯一位置"）；
3. **冗余或僵尸面 → 不自行删**：先列清单 + 逐条详解（是什么、谁在用、为什么疑似冗余/僵尸、删了会怎样），
   **交用户决定**。

### P10 判据漂移（较高优先级 bug）

```text
现在：store/durable_history.go 的 isSessionNotFound(...)   ─┐ 两者判据不同
     seelexctx/search/search.go 的 isSessionNotFound(...)  ─┘（后者还多认 sql.ErrNoRows，SQL 后端已退役）
建议：先写一条会红的用例复现"同一输入两处不同答案"，再合并成唯一判据（保留哪一份语义要写依据），再跑绿
```

**为什么优先**：这是"同一判据两处实现"的**现网不一致**（AGENTS §8.3），不合并会让"会话不存在"在不同路径下有两种答案。

---

## 3. 用户裁决（2026-10-16，逐条）

| 提案 | 用户裁决 |
|---|---|
| P1 | 根据建议改动流程（= 迁移收口后做，只移路径门、不拆审批） |
| P2 | **语义更正**：loop 预警是 **mainagent** 的事；看 `limit` 下 loop 的 limit 部分；**预算用尽后做 +1 次调用，总结预算内的工作内容**；**设置面板支持「无限循环」勾选**；`workunit` 在预算预警里的使用**有待商榷** |
| P3 | 按照建议来（前置三项产品向决定） |
| P4 | 确实需要按照**领域驱动设计**的规范重做 store 的内容与包的引用 |
| P5 | 继续按计划进行 |
| P6 | 同理，超大包按 DDD 拆分，按计划进行 |
| P7 | 按照建议做 |
| P8 | **语义更正**：`workunit` = 后台工作 agent 的抽象（含 subagent/teammate）+ 为未来后台 agent 方案（含规划中的远程 agent 调用）提供契约；`policy` = 多条件 select 出 bool 决定权限，是权限设置的统一抽象与契约。**对内容的改动必须先判定是契约抽象还是兼容面**；其它冗余/僵尸面**先暴露出来交用户决定**（先详解再决定） |
| P9 | 按照建议做 |
| P10 | 需要修改合并的**较高优先级 bug** |
| P11–P15 | 根据建议判别 |

---

## 4. 与其他文档的关系

- 搬迁方案与约束：[plan.md](plan.md)。
- 各里程碑**已做**内容、删除清单与偏差：[m1-record.md](m1-record.md)、[m2-record.md](m2-record.md)、
  [m3-record.md](m3-record.md)、[m4-record.md](m4-record.md)、[m5-record.md](m5-record.md)。
- 本文件里**已执行**的提案条目（做过的删除/新增与证据）：[p-items-record.md](p-items-record.md)。
- 剩余搬迁切片与切分技术：[m5-slices.md](m5-slices.md)、[m4-decision-brief.md](m4-decision-brief.md)。
- 本文件只放**提案**（未做）+ 用户裁决，与"已做/在做"的记录分开，避免把规划写成既成事实。

---

## 5. P8 实测判别：契约抽象 / 兼容面 / 冗余·僵尸面（2026-10-16）

（§1 汇总表里 P8 行的状态以本节为准：**判别已完成**。）

**本节数字的复核口径**（root 2026-10-16 独立重跑）：`rg -l '^\s*seelebridge\s' --glob '*.go'` = **34** ✓、
`rg -c 'seelebridge' --glob '*.go'` 命中文件数 = **207** ✓。两条经复核**已更正**：

1. `store/legacy_storage.go` 行数以 `wc -l` / Python 口径为 **301**（本环境下 PowerShell 的
   `Measure-Object -Line` 会少算，**不要**用它数行）；
2. §5.2 原列 `prompt.ReActBudget` 为"限定名 = 0，删"——**错**：该限定名被
   `application/core/task_context/coordinator.go`（多处签名与字段）等 6 个文件消费，属 §5.1 的
   "领域词汇别名"，已改判为**保留**。

> **两处不重复**：逐条详解（含 19 条"生产者零引用"僵尸面、仅被测试引用的出口、
> "同一形状被 2–3 个名字各自出口"的三条链、以及每条的删除后果）在
> [p8-surface-audit.md](p8-surface-audit.md)——那是**规范详解处**。
> 本节只保留本文件独有的两块：`seelebridge` **命名残留**的计数（§5.3 Z3）与建议执行顺序（§5.4），
> 其余条目以该文为准。

**盘点口径**：全仓 `type X = Y`（含 `type (...)` 块内）共 **266 处**（脚本 `tmp/reshape/alias_census.py`，按词边界统计）。逐条判定改用**限定名**精确计数（`rg -n '\bpkg\.Name\b'`），避免同名符号互相污染；"包内用量"另按裸名在定义包内单独统计。用户口径：① 契约抽象 → 保留并归位；② 兼容面 → 删；③ 冗余/僵尸面 → **先暴露清单与详解，由用户决定**（§5.3 即此清单，本轮未动）。

### 5.1 契约抽象（保留；只欠文档里指向已删模块的陈述）

| 面 | 位置 | 判据（证据） |
|---|---|---|
| `workunit` 生命周期契约 | `backend/workunit/{contract.go,ports.go}` | 消费方 9 处：`backend/multiagent/{subagent,teamwork}`、`backend/tool/{worktree,node}`、`internal/telemetry`、`backend/engine/assemble/workunit_*`；`ports.go` 的三根宿主端口让生命周期实现里搜不到 `teamwork.`/`worktree.`/`store.` 限定符（`e2e/workunit_ports_test.go` 机械门禁）——正是用户定义的"后台工作 agent（subagent/teammate，及未来远程 agent）契约"。**欠**：`contract.go` 顶部仍写"实现包（`seelebridge` 的 lifecycleHost）"，落点早已是 `backend/engine/assemble/workunit_*`（文档漂移，并入 P12） |
| `policy` 权限判定契约 | `backend/limit/policy/{policy.go,tiers.go,tool_meta.go,shellkind.go}` | 档位目录与路由组表的事实源在 `contract/dto`（`dto.PermissionTiers()`/`NormalizePermissionTier`），本包只做两件事：按档位剪掉 `ask` 规则（`ApplyTier`，从不新增 allow、从不触碰 deny）、按名字把组翻成框架 `ToolMeta`（`DeclaredToolMeta` → `ToolKindForGroup`）；`tiers.go` 明写"档位是声明式覆盖，不是新判定机制"。符合用户定义的"多条件 select 出 bool 的权限统一抽象与契约"。保留 |
| `dto` 词汇别名（9 条） | `middle/protocol/model/state.go` | 定义只有一份（`contract/dto`），本包以别名给中层词汇：`model.X` 被 35+ 文件消费，`application/application.go` 再把它抬成前端门面词汇。删别名只是把限定符从 `model.X` 换成 `dto.X`，无漂移风险可消除 |
| 领域包词汇别名（约 15 条） | `backend/tool/{task,plan,worktree,node}`、`backend/multiagent/{subagent,goal}`、`backend/engine/schedule`、`internal/model/*` | 同构：领域包的公共词汇指向契约里的唯一形状。保留 |
| 平台/桥接别名 | `backend/tool/computer/darwin_core.go`（`cgFloat = float64`）、`gui/terminal_bridge.go`（`TerminalSession = terminal.Info`） | 与迁移无关的命名选择。保留 |

### 5.2 兼容面（删；都是"只为不动调用点"而存在）

| 位置 | 条数 | 现状证据 | 删除动作 |
|---|---|---|---|
| `application/core/aliases.go` | 230 行 | `application/core` 包内与 `application/console` 当本地词汇用；`application/application.go` 已自己重导出同一批名字（前端门面不会断） | R10：整文件删；包内改引 `model.X`/`contract.X`/`dto.X`，**以编译器报错为全量清单** |
| `store/root_aliases.go` | 7 条 | 外部只有 3 处：`wiring/bootstrap/runtime.go` 用 `store.Settings`(2) 与 `store.DataRootLockedByWith`(1)；`store.NewSettings`、`store.ErrDataRootLocked`、`store.ErrDataRootStaleLock`、`store.DataRootLockedBy` 的**限定名引用 = 0**，只剩 `store/` 自己的测试在用 | 删文件；3 处外部改 `storeroot.X`；`store/` 测试改限定名 |
| `backend/engine/assemble/{ports.go,runtime.go,runtime_role_plugins.go}` | 15 条（11+3+1） | 旧根包名遗留 | R11 随 `ports.go` 一并删 |
| `application/core/service_scheduler.go:333,336` | 2 条 | `core.ScheduledTaskSpec`/`ScheduledTaskStatus` 限定名 **= 0**（包内也无人用）；同文件第 10 行的 import 别名仍叫 `seelebridge`，实际指向 `backend/engine/assemble` | 删这 2 条 + import 别名改 `assemble` |
| `session/ports.go:26,29`（`SessionKind`/`TranscriptEvent`） | 2 条 | `session.SessionKind`/`session.TranscriptEvent` 限定名 = 0；包内仅定义行 | 删；改用 `store.Kind`/`store.TranscriptEvent` |

**改判（root 复核，见本节开头的"复核口径"）**：`application/prompt/effort.go:29` 的
`ReActBudget = loop.Budget` **不是**兼容面——`rg -n 'prompt\.ReActBudget'` 命中 6 个文件
（`application/core/aliases.go:224,234`、`application/core/task_context/coordinator.go` 的 4 处签名/字段等），
是生产在用的**领域词汇别名**，按 §5.1 处置（保留）。要收口的是 `application/core/aliases.go` 里那一跳（R10）。

### 5.3 冗余/僵尸面（**等你决定；本轮未动**）

（Z1/Z2 与 [p8-surface-audit.md](p8-surface-audit.md) §0/§3 的 `store/legacy_storage.go` 是同一件事，两处结论一致：都等你裁决。）

**Z1 `store/legacy_storage.go`（301 行，`wc -l`）整文件存疑。**

- 它定义 `SessionStore` + `NestedSessionStore` 两个类型与两个构造（legacy shard 文件存储、按工作区路由的嵌套存储）。
- 全仓限定名引用：`store.SessionStore` = 0、`store.NestedSessionStore` = 0、`store.NewNestedSessionStore` = 0；`store.NewSessionStore` = **1**，唯一消费者是 `test/smoke_test.go:35`，而那里拿到 `st` 之后只做了一句 `_ = st.FrameworkStore()`，整段流程随后走 `runtime.NewMainSession`。
- 包内：`store/` 里只有 `legacy_storage.go` 自己引用这两个类型名。
- 文件头三处陈述都已失真：`// Package storage ...`（包名是 `store`）、"置于 `internal/`"（实际在 `store/`）、"根包经 `storage_aliases.go` 重导出"（`store/storage_aliases.go` **不存在**）。
- 为什么可疑：现代会话存储是 `store/sessionstore.go` 那条线；这一份"看起来还能用"的第二套存储留在包里，读者会以为存在两条写路径。
- **删了会怎样**：`test/smoke_test.go` 去掉那 4 行（本来也没用它的产出）→ 全仓会话存储只剩一条路径；`store/README-storage.md` 的索引段同步删。**不删的代价**：僵尸面长期挂着，且注释继续骗人。
- **我的建议**：删。若你要留作"legacy 兼容读"，那它至少要有一条真实读路径 + 一条钉住格式的用例，否则就是幽灵。
- **决策前要问自己的一句**（[p8-surface-audit.md](p8-surface-audit.md) §7 也提了）：老数据根里可能还有 legacy shard 文件——**还要不要回读**？要回读就保留"读"这条路（并补一条格式用例）；确定不再回读，删掉才没有数据风险。

**Z2 `store/legacy_storage.go` 的两个包内别名 `Message`/`SessionMeta`。**

- `type Message = types.Message`（Seele 框架类型）、`type SessionMeta = storage.SessionMeta`（Seele 框架存储类型）。
- 包内以**裸名**使用它们：`Message` 出现在 16 个文件、`SessionMeta` 出现在 4 个文件（`json_layout.go`/`sessionstore.go`/`message_rows.go`/`legacy_storage.go`）。
- 为什么可疑：它不是"第二份实现"（定义只有一份），但把"这个类型来自框架"这一事实藏起来了——`store` 的公开面看起来像自有类型，实际是 Seele 类型别名，将来换引擎类型时没有编译期信号。
- **删了会怎样**：16 个文件里 `Message` → `types.Message`、4 个文件里 `SessionMeta` → `storage.SessionMeta`（纯限定符改写 + README 索引同步）。**不删的代价**：类型来源在调用点不可见。
- **我的建议**：删（与 Z1 同批；若 Z1 保留，至少把注释写成"这是框架类型的本地别名"）。

**Z3 `seelebridge` 命名残留**（模块已删，名字还在；脚本 `tmp/reshape/seelebridge_residue.py`，实测）：

| 载体 | 数量 | 说明 |
|---|---:|---|
| Go 文件提到 `seelebridge` | 207 | 其中 123 个同时含"迁移前/原/曾经"这类**历史陈述**（合法，保留） |
| 作为 **import 别名**（`seelebridge "…/backend/engine/assemble"`） | 34 | 例：`gui/bridge_test.go`、`wiring/registry/registry.go`、`application/console/backend_console.go`、`backend/workunit/*`、`contract/ports.go`、`internal/telemetry/summary.go` |
| 作为**限定符**（`seelebridge.X`） | 56 | 全是上面那些别名的使用点 |
| 作为**字符串字面量**（用户可见文案） | 24 | 例：`store/legacy_storage.go` 的 7 处 `"seelebridge: …"` |
| 出现在 `//` 注释 | 169 | 需逐条判断：历史陈述保留、指向当前实现的改成新落点 |
| Markdown 文档提到 | 414 | 336 含历史陈述（保留）；93 处限定符指向已删包名 |

- **删了会怎样**：34 处 import 别名 → `assemble`；24 处文案 → 当前包名口径（**这部分已并入 P9**，且不碰正在搬迁的 `application/core/**`、`middle/**`、`backend/engine/assemble/**`、`backend/tool/registry/**`）；169 处注释与 414 篇文档只改**指向当前实现**的陈述（并入 P12），历史记录不许改写成"从没存在过"。
- **我的建议**：别名改名 + 文案改写**做**（机械、零行为变化）；注释/文档按"历史陈述保留、现状陈述改新落点"逐条处理。

### 5.4 建议的执行顺序

1. **5.2 的兼容面删除**（`store/root_aliases.go`、`service_scheduler.go` 两条 + import 别名、`session/ports.go` 两条）：与 M5 并行、零行为变化。`application/core/aliases.go` 与 `backend/engine/assemble/*` 两组分别在 R10/R11 收口。（原列的 `prompt.ReActBudget` 已按上条改判为保留。）
2. **Z3 的别名与文案**：随 P9 一起（已扩范围），只动不在搬迁路径上的文件。
3. **Z1/Z2**：等你一句"删/留"；删的话与 Z3 同批做（同文件冲突面大，不拆两片）。
