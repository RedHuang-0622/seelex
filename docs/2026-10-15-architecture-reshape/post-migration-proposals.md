# 迁移后提案汇总（Post-migration proposals）

日期：2026-10-16
状态：**提案清单，不是已实现**。本文件是"架构重排（[plan.md](plan.md)）期间发现、但按纪律
不该混进搬迁一起做"的事项的**唯一汇总处**——每条都写清：涉及哪些包、现在什么流程、建议改成
什么流程、两者差在哪、我的建议是什么。

维护约定：

- 搬迁期间新发现的同类事项**只往本文件加行**，不夹带进搬迁切片（plan §5.8「一次一主题」）。
- 行里的状态只有四种：`待裁决`（需要用户点头）、`已裁决`（用户已定，写结论）、
  `建议做`（我的建议：做，且已给判据）、`建议不做`（我的建议：不做，附理由）。
- 每条都要能指向证据（`文件:行`、命令输出、记录里的 D-* 编号）。

---

## 1. 汇总表

| ID | 提案 | 涉及包 | 现在 | 建议 | 差异与风险 | 我的建议 | 状态 |
|---|---|---|---|---|---|---|---|
| P1 | 路径门（ProjectScope）前移到权限判定之前 | `backend/limit/gate`（ProjectScope）、`backend/tool/registry`（PermissionGate 中间件）、`wiring/bootstrap`（中间件装配顺序） | 事件 → **权限判定（含审批，ask 就地弹窗）** → 诊断 → handler 里才查路径越界 | 事件 → 主体解析 → **路径越界检查** → 权限判定 → 审批 | 越界路径今天会**先弹一次没用的审批**，点完才失败；换序后直接拒、不弹窗。错误文案与"谁先拒"改变（安全相关路径，非纯搬迁） | **做，但放在搬迁收口之后**，且只移路径门、不拆审批（拆审批会改超时/取消边界） | 待裁决（用户 2026-10-16 已认可顺序：迁移后做） |
| P2 | loop 预警（`Verdict`: Ok/Warn/Exhausted + 总结走 `workunit.RecoveryNote`） | `backend/limit/loop`（判据）、ReAct 循环（现 `application/core/chat.go`，终局 `backend/engine/react`）、`backend/workunit`、`wiring/bootstrap` | 只有**事后收尾**：预算耗尽后 `finalizeReActBudget` 保留一次纯文本交付 | 事前预警：Warn 时不再派发新工具、让引擎产出"当前工作总结"；Exhausted 沿用既有收尾 | 这是**新增行为**（运行时会长出新的提示与拒绝点），不是搬迁 | **做，但在 M5 之后**（先让搬迁纪律干净收口），判据用"同一输入跑两遍：预警路径与既有收尾路径结论一致" | 待裁决（用户已定：M5 完成后讨论） |
| P3 | 契约层提纯：`contract` 闭包 = stdlib | `contract`（含 `ports.go`）、`contract/dto`、`middle/protocol/{model,event}`、`seelexctx/{search,snapshot}`（终局 `backend/engine/context/seelectx/*`）、Seele `types` | `contract` 仍依赖 Seele `types`、`middle/protocol/*` 的形状、`seelexctx/{search,snapshot}` —— 门禁用 ratchet 钉住（`contractClosureAllowListMax`） | 把端口签名要用的形状下沉进 `contract`/`contract/dto`，Seele 类型换成 engine-owned 消息类型（先拆实际消费字段） | 取决于"状态 owner / 产品 A-B 模型 / scope"三个**产品向**决定（pre-design 明确列为前置）；Seele 类型替换要证明无损（`types.Message` → `EngineMessage` 是否丢字段） | **做，但必须先拿到那三个决定**；在此之前 ratchet 只许收缩、别动 | 待裁决（阻塞在 D01–D12） |
| P4 | `store/` 深拆 8 路分包（root/session/board/team/role/node/media/legacy） | `store`、`store/root`、`store/{workspace,schedule,record}` … | `store/` 是**单包**（134 生产文件）；机制层已下沉 `store/root`（闭包纯标准库） | 按域各自成包 | **不是搬文件**：`Router`(131)/`jsonRepository`(82)/`storeEngine`(182) 三个类型共 395 个方法散在 33/57 个文件里，Go 要求方法同包 —— 必须先**拆类型**（跨 110 个外部调用点） | **做，但先做类型分解**（切法 B）；分解前别动，否则只能复制实现 | 待裁决（plan §3.6 V4/V5 已给结论） |
| P5 | `backend/engine/assemble` 的 `Runtime` 按域下沉收口 | `backend/engine/assemble`（现 48 生产文件 / 12.5k 行）、各 `backend/*` 板块 | 旧 `seelebridge` 根包整体落位（352 方法的跨文件单类型）；已下沉 obs/media/board 等域 | 每个域：目标包定义**只读 Host 端口** → 域状态与方法随类型走 → assemble 只留"持有 + 挂载"；删 `ports.go` 兼容面 | 域差异只能出现在装配表与策略调用点；顺序/关机语义必须逐字保持 | **做（已在做，R11 收口）**；目标 assemble ≈ deps/bundle/live/拓扑+关机 4–6 文件 | 搬迁中 |
| P6 | `application/core` 按域切分 + `Service` 门面收窄五组窄接口（+I5 断言） | `application/core`（69 生产文件、`Service` 419 方法）、`middle/{view,switch,residency}`、`backend/*`、`kernel/state`、门面 `application` | 单包承载全部用例面；门面是"类型别名 + 用例方法"的大平面 | 先 `kernel/state`（共享内核），再域状态下沉；跨域读写走消费方接口；门面收窄成 A 类型/事件身份、B 读投影、C 命令面、D 档位与策略、E 队列与无状态函数五组 | 切分顺序错了会成环；`Service` 68% 方法是未导出的，不能沿公共 API 切 | **做（已在做，R5–R10）** | 搬迁中 |
| P7 | 前端收口三件 | `gui/**`（`bridge.go`、`headless*.go`）、`application/console` → `console/`、`tui` | `gui/headless*.go` 仍在 `gui/`；`application/console` 仍是 `application` 子包；`gui/**` 注释与 README 里仍有旧路径（D-M3-13） | 按 plan §2.1：`headless/` 独立成包（D4 可选）、`console/` 顶层化；注释/文档旧路径一次性对齐 | 前端行为零改动；只碰 import 与文案 | **建议做**（机械、低风险），但放在搬迁收尾后一次性做，避免与门面收窄互相踩 | 建议做 |
| P8 | 兼容别名与兼容面清理 | `backend/workunit`、`backend/limit/policy`（`GroupBit*`）、`store`（`Settings`/`StoreFile` 等）、`middle/protocol/model`（`NodeScope` 等）、`internal/model` | 搬迁期为"不动调用点"留了一层 `type X = Y` 别名与转发（如 `GroupBit*`、`NodeScope`、`Settings`） | 消费方全部改到新家后逐个删除；删一处跑一次编译 | 删早了没有编译错误面（别名与真名同形），错误形态是"删了没人提供该类型" | **建议做**，按 `m4-decision-brief`/`m5-slices` 的 K 片清单逐条删；每删一条记"原位置 → 新唯一位置" | 建议做 |
| P9 | 错误文案统一 | `backend/engine/obs`（原 `seelebridge/events_unified.go`） | 文案里仍写 `"unified events: sessionstore router is not attached"`（D-M3-12） | 改成当前包名措辞 | 错误文案是用户可见面；改了必须同步用例 | **建议做**，单独一个 commit（文案 + 用例同改） | 建议做 |
| P10 | 判据漂移：`isSessionNotFound` 两份实现 | `store/durable_history.go`、`seelexctx/search/search.go`（终局 `backend/engine/context/seelectx/search`） | 两处各自判断"会话不存在"，后者还多认 `sql.ErrNoRows`（SQL 后端已退役） | 合成一处（或证明两者语义差异是刻意的并写注释） | 这是 AGENTS §8.3 意义上的"同一判据两处"缺陷，且两处已不一致 | **建议做**：先写会红的用例复现差异，再合并 | 建议做 |
| P11 | 覆盖缺口：节点预算上限守卫 | `backend/tool/plan`（终局）、`backend/limit/loop` | `docs/arch/ARCHITECTURE_REVIEW.md` 提到的 `plan_input_fuzz_test.go` 在现行树里**不存在**，该分支当前无覆盖 | 补一条钉该上限的用例，或把该行标注为"无覆盖" | 不补的后果是"文档宣称有覆盖"这一处失真会一直在 | **建议做**（补用例或改文档，二选一，别两头不动） | 建议做 |
| P12 | 文档漂移清理 | `docs/arch/**`、各模块 README、`docs/gui/**` | `docs/arch` 里约 23 行指向**从未在当前树存在过**的旧文件名；README 漂移门禁现存 12 条（与本方案无关） | 逐条改指当前实现或标注"已删除" | 不改会影响"文档即事实"的可信度，但不会打红 CI（门禁有容忍表） | **建议做**，可并入 P7 的收口片 | 建议做 |
| P13 | 「文件与函数索引」生成器全仓化 | `scripts/gen_core_readme_index.py`、全仓 Go 包 README | 生成器只覆盖 `application/core` 分卷与少数登记包；其它包手写或缺失（readme-spec §3 要求） | 把提取逻辑扩到全仓（按包遍历），一次补齐所有模块 README 的索引 | 索引缺失是**历史债**，逐包手补会漂移 | **建议做**（一次生成、分段提交），但要先确认生成器对"中文 doc 注释缺失"包的降级行为 | 建议做 |
| P14 | 多入口 / 多变体构建 | `wiring/config`、`wiring/bootstrap`、`wiring/registry`、新增 `cmd/<变体>/main.go` | `main.go` 已瘦到 36 行（flag → bootstrap → 前端 → 关机） | 需要第二套启动形态时，加 `cmd/<变体>/main.go`：`config.BindFlags` + 换装配 `Options` + 选前端，不复制 main | 现在没有第二入口的**具体需求**，提前造目录就违反"不预建万能包" | **建议先不做**：等出现第二个真实入口需求再落（现在的形态已让它变成"加一个文件"的事） | 建议不做（待需求） |
| P15 | `store/root` 的 test-only 导出收敛 | `store/root`、`store`（集成用例） | 为让 `store/` 侧集成用例伪造/replay 锁文件，`store/root` 多导出了 `OwnerRecord`/`NewOwnerRecord`/`ReadOwnerRecord`/`WriteOwnerRecord`/`DataRootLockFile`/`ProcessAlive`（D-M3-8） | 把这些用例改成"行为级"断言（用真实子进程持锁），即可收回导出 | 取消导出会逼用例复制锁文件格式（那才是第二份实现） | **建议先不做**：当前形态已满足"一份实现"，收回导出的收益小于引入重复格式的风险 | 建议不做 |
| P16 | `store` 的 `RouterStorage` 端口反转 | `store`、`seelexctx/lifecycle`（原向上依赖，已在 M3 删除） | M3 采用"断言整条上移消费侧"，未走"契约层端口"路线（实测 `contract → seelexctx → store` 成环，D-M3-11） | 若将来要把该适配器真正下沉契约层，先解环 | 现在没有第二消费方，端口化是纯形式收益 | **建议不做**（除非出现第二消费方） | 建议不做 |
| P17 | 工具记录看板的"整机汇总"读面 | `store/record`、`middle/view/record`、钩子 | 用户裁决 V10：读数**不进 `Snapshot`**，钩子**直接与 JSONL 交互** | 若仍要"这台机器哪个工具最该优化"的全局视图：加一层显式汇总（读多份 JSONL），不进快照 | 不加就只有会话级读数；加了多一层聚合与缓存口径 | **建议按需**：现在不做；真要整机视图时按"显式汇总、不进快照"实现 | 已裁决（V10） |

---

## 2. 逐条讲解（流程对比）

### P1 路径门前移（M2-C5）

**现在的流程**（`backend/tool/registry/registry_state.go` 的权限中间件 + `backend/limit/gate/project_scope.go`）：

```text
工具调用 → 事件中间件 → 权限门：簇属守卫 → 主体/档位解析 → Seele 判定
                                                            ├─ allow/deny → 出结果
                                                            └─ ask → 就地弹审批（approvalTimeout）
           → 诊断中间件 → handler（Router 里才查 ProjectScope：路径在不在项目内）
           → 真正执行
```

**建议的流程**（plan §1.4 定序）：主体/档位解析 → **路径门** → 权限判定 → 审批 →（第五段）循环预警。

**差异**：唯一真实的换序是"路径门从 handler 内提到判定之前"。后果：越界路径不再"先问人再失败"，
用户少一次无意义打扰；错误从"执行失败（路径越界）"变成"作用域被拒"；日志里的失败原因文字改变。
**实施风险**：主体解析必须仍在路径门之前（否则员工"该提权的调用"会被路径门先拦）；**不建议**把审批
从判定内部拆出来独立成段（那会改审批超时/取消的返回边界）。

### P2 loop 预警（M2-D）

**现在**：预算耗尽后由 `finalizeReActBudget` 保留一次纯文本交付；`application/prompt/effort.go` 的
档位表提供 `ReActBudget{MaxToolRounds, MaxToolCalls, MaxNoProgressRounds}`。
**建议**：在 `backend/limit/loop` 里补 `Verdict(budget, used) → Ok|Warn|Exhausted`；Warn 时不再派发
新工具、让引擎产出"当前工作总结"；总结正文只走 `workunit.RecoveryNote`（唯一容器）。
**差异**：这是**新增行为**（多一个运行期拒绝点、多一段提示文本），与搬迁的"同输入同结果"纪律互斥，
所以必须在里程碑之外单独做、单独验收。

### P3 契约层提纯

**现在**：`contract/ports.go` 为了端口签名仍 import Seele `types`、`middle/protocol/*` 的形状、
`seelexctx/{search,snapshot}`；门禁用 `contractClosureAllowListMax` 这类 ratchet 钉住"只许收缩"。
**建议**：把端口签名要用的形状下沉 `contract`/`contract/dto`，Seele 类型换成 engine-owned 消息类型。
**差异**：`types.Message` → `EngineMessage` 必须证明**无损**（字段是否丢）——这正是 pre-design 把它列为
"需要状态 owner / 产品 A-B 模型 / scope 三个决定"的原因。

### P4 `store/` 深拆

**现在**：`store/` 单包（134 生产文件），机制层 `store/root` 已独立（闭包纯标准库）。
**建议**：按域成包（session/board/team/role/node/media/legacy）。
**差异**：`Router`(131 方法)/`jsonRepository`(82)/`storeEngine`(182) 是**跨文件单类型**，Go 要求方法同包，
所以"搬文件"这一步不成立；必须先拆类型（影响约 110 个外部调用点）。

### P5 / P6 两处"上帝类型"收口

- `backend/engine/assemble` 的 `Runtime`（352 方法）：每域"只读 Host 端口 → 状态与方法随类型走 → 装配侧只留持有与挂载"；
  收口后 assemble 只剩 deps/bundle/live/拓扑+关机。
- `application/core` 的 `Service`（419 方法，68% 未导出）：必须**先建 `kernel/state` 共享内核**，
  再域状态下沉，跨域读写走消费方接口；门面最后收窄成五组窄接口 + I5 断言。

### P7–P13 小修类

共同点：都是**机械、可机械验证**的收尾（前端包位、别名删除、文案、漂移、覆盖缺口、生成器）——
留到搬迁收口后按"一片一 commit"做，避免与门面收窄互相踩。

---

## 3. 已裁决（不再列为提案）

| 项 | 裁决 | 出处 |
|---|---|---|
| V10：工具记录看板读数是否进 `Snapshot` | **不进**；钩子直接与 JSONL 交互 | 用户 2026-10-16 |
| M2-D（loop 预警） | **延后到 M5 完成之后**再讨论 | 用户 2026-10-16 |
| V3：`workunit` 归属 | **独立 `backend/workunit`**（比放 multiagent 更少跨板块回指） | root 裁决，已落地 `68813389` |
| `internal/*` 终局 | **模块根 `internal/*`**（不再二次下沉） | root 裁决 |
| M2-C5 | **迁移收口后再做**，只移路径门、不拆审批 | 用户 2026-10-16「按你的建议来」 |

---

## 4. 与其他文档的关系

- 搬迁方案与约束：[plan.md](plan.md)。
- 各里程碑的**已做**内容、删除清单与偏差：[m1-record.md](m1-record.md)、[m2-record.md](m2-record.md)、
  [m3-record.md](m3-record.md)、[m4-record.md](m4-record.md)、[m5-record.md](m5-record.md)。
- 剩余搬迁切片表与切分技术：[m5-slices.md](m5-slices.md)、[m4-decision-brief.md](m4-decision-brief.md)。
- 本文件只放**提案**（未做），与上面"已做/在做"的记录分开，避免把规划写成既成事实。
