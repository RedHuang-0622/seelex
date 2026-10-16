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
| P1 | 路径门（ProjectScope）前移到权限判定之前 | `backend/limit/gate`（ProjectScope）、`backend/tool/registry`（PermissionGate 中间件）、`wiring/bootstrap`（装配顺序） | 事件 → **权限判定（ask 就地弹审批）** → 诊断 → handler 里才查路径越界 | 事件 → 主体解析 → **路径越界检查** → 权限判定 → 审批 | 越界路径今天会**先弹一次没用的审批**；换序后直接拒、不弹窗。错误文案与"谁先拒"改变（安全相关路径） | 做，**放在搬迁收口之后**，只移路径门、不拆审批 | **已裁决（做；迁移后）** |
| P2 | loop 预警：预算用尽后 **+1 次总结调用** + 设置面板「无限循环」勾选 | `backend/limit/loop`（**看这里**：档位轮数/预算/上限判据）、ReAct 主循环（现 `application/core/chat.go` → 终局 `backend/engine/react`）、设置面板（GUI/TUI 的"循环次数"档位处）、`wiring`（注入） | 只有事后收尾：预算耗尽后 `finalizeReActBudget` 保留一次纯文本交付回合；面板只有"循环次数"数值 | ①预警/用尽属于**主代理（mainagent）**这一层的事，**不涉及 subagent/teammate**；②预算用尽后做 **+1 次调用**，内容就是"总结预算内已经完成的工作"；③设置面板支持**「无限循环」勾选**（勾上即不设上限）；④`workunit` 在预算预警里的角色**待商榷**，先不接 | 这是**新增行为**（多一次调用、多一个面板开关、多一条设置），与搬迁的"同输入同结果"互斥；`workunit` 是否承载总结容器**未定** | 做，但**放在 M5 之后**；判据：同一输入下"预算内工作"的字面命中 + 面板勾选后无上限生效 | **已裁决（语义已更正，按上表「建议」列执行；时机 M5 之后）** |
| P3 | 契约层提纯：`contract` 闭包 = stdlib | `contract`、`contract/dto`、`middle/protocol/{model,event}`、`seelexctx/{search,snapshot}`、Seele `types` | `contract` 仍依赖 Seele `types`、`middle/protocol/*` 形状、`seelexctx/{search,snapshot}`；ratchet 钉住 | 把端口签名要用的形状下沉 `contract`/`contract/dto`，Seele 类型换 engine-owned 消息类型（先拆实际消费字段） | 取决于"状态 owner / 产品 A-B 模型 / scope"三个产品向决定；Seele 类型替换要证明无损 | 做（按建议），但**先拿到那三个决定** | **已裁决（按建议做；前置三项决定）** |
| P4 | `store/` 按 **领域驱动设计（DDD）** 重做 | `store`（现单包 134 生产文件）、`store/{root,workspace,schedule,record}` | 机制层已独立（`store/root` 闭包纯标准库）；域仍同包 | 按**限界上下文**重划包与引用：会话/看板/团队/角色/节点/媒体/legacy 各自成上下文，包引用按依赖方向重排 | **不是搬文件**：`Router`(131)/`jsonRepository`(82)/`storeEngine`(182) 三个类型 395 个方法散在 33/57 文件，Go 要求方法同包 → 必须先**按 DDD 拆类型与上下文**（跨约 110 个外部调用点） | 做，**先做领域建模与类型分解**，再落包 | **已裁决（按 DDD 重做；先建模）** |
| P5 | `backend/engine/assemble` 的 `Runtime` 按域下沉收口 | `backend/engine/assemble`（现 48 生产文件 / 12.5k 行）、各 `backend/*` | 旧根包整体落位（352 方法的跨文件单类型） | 每域：目标包定义**只读 Host 端口** → 域状态与方法随类型走 → assemble 只留持有与挂载；删 `ports.go` | 域差异只能出现在装配表与策略调用点；顺序/关机语义逐字保持 | 做（按计划） | **已裁决（继续按计划）** |
| P6 | `application/core` 拆分 + `Service` 门面收窄五组窄接口（+I5） | `application/core`（69 生产文件、`Service` 419 方法）、`middle/*`、`backend/*`、`kernel/state`、门面 `application` | 单包承载全部用例面；门面是大平面 | **按 DDD**：先 `kernel/state` 共享内核 → 域状态下沉 → 跨域读写走消费方接口；门面收窄成 A/B/C/D/E 五组 | 顺序错了会成环；68% 方法是未导出的，不能沿公共 API 切 | 做（按计划） | **已裁决（按 DDD 拆；按计划推进）** |
| P7 | 前端收口三件 | `gui/**`（`headless*.go`、注释/README 旧路径）、`application/console` → `console/`、`tui` | 仍在原地；注释与 README 有旧路径（D-M3-13） | 按 plan §2.1：`headless/` 独立、`console/` 顶层化、旧路径注释一次对齐 | 前端行为零改动 | 做（按建议），搬迁收尾后一批 | **已裁决（按建议做）** |
| P8 | 契约 vs 兼容面甄别（别名/转发面清理） | `backend/workunit`、`backend/limit/policy`、`store`、`middle/protocol/*`、`internal/*` | 搬迁期留了一层 `type X = Y` 别名与转发（`GroupBit*`、`NodeScope`、`Settings`、`StoreFile` 等） | **先判性质**：①**契约抽象**（如 `workunit` = 后台工作 agent 的抽象，含 subagent/teammate，并为未来后台 agent 方案（含规划中的远程 agent 调用）提供契约；`policy` = 多条件 select 出 bool 决定权限的统一抽象与契约）→ 保留并归位；②**兼容面**（只为不动调用点而存在的转发/别名）→ 删；③**冗余或僵尸面** → **先暴露清单与详解，交用户决定**，不自行删 | 判错性质会把契约当垃圾删掉，或把垃圾当契约供起来 | 按上述三类处置；**每一条都要先给"为什么是这一类"的判据与证据** | **已裁决（按三类甄别；冗余/僵尸先暴露给用户决定）** |
| P9 | 错误文案统一 | `backend/engine/obs`（原 `seelebridge/events_unified.go`） | 文案里仍写 `"unified events: sessionstore router is not attached"`（D-M3-12） | 改成当前包名措辞 | 用户可见面；改则同步用例 | 做（按建议），单独一个 commit | **已裁决（按建议做）** |
| P10 | **判据漂移（较高优先级 bug）**：`isSessionNotFound` 两份实现 | `store/durable_history.go`、`seelexctx/search/search.go`（终局 `backend/engine/context/seelectx/search`） | 两处各自判断"会话不存在"，后者还多认 `sql.ErrNoRows`（SQL 后端已退役）→ 两处**已不一致** | 合并成一处唯一判据（保留哪一份的语义要有依据） | AGENTS §8.3「同一判据两处即缺陷」；不合并会让"会话不存在"在不同路径下两种答案 | **先写会红的用例复现差异，再合并** | **已裁决（较高优先级修复）** |
| P11 | 覆盖缺口：节点预算上限守卫 | `backend/tool/plan`、`backend/engine/assemble` | `docs/arch/ARCHITECTURE_REVIEW.md` 声称为 `plan_input_fuzz_test.go`；复核更正：该文件**存在**（`backend/engine/assemble/plan_input_fuzz_test.go`），但只 fuzz `NormalizePlanLoadArguments`，**不触及** `ValidatePolicyLoad` 的 MaxNodeLoops/MaxNodeOutputTokens 分支 → 覆盖缺口为真、理由不同 | 补用例（原描述的二选一里选"补"） | 不补=文档失真 | 补边界用例，并把文档指向真实用例名 | **已修复（见 [p-items-record.md](p-items-record.md) §P11）** |
| P12 | 文档漂移清理 | `docs/arch/**`、模块 README、`docs/gui/**` | `docs/arch` 约 23 行指向从未存在的旧文件名；README 漂移门禁现存 12 条（与本方案无关） | 逐条改指当前实现或标"已删除" | 不打红 CI（有容忍表），但损害"文档即事实" | 按建议做，可并入 P7 收口片 | **已裁决（按建议判别）** |
| P13 | 「文件与函数索引」生成器全仓化 | `scripts/gen_core_readme_index.py`、全仓 Go 包 README | 生成器只覆盖 `application/core` 分卷与少数登记包 | 提取逻辑扩到全仓，一次补齐 | 逐包手补会漂移 | 按建议做；先确认"中文 doc 注释缺失"包的降级行为 | **已裁决（按建议判别）** |
| P14 | 多入口 / 多变体构建 | `wiring/{config,bootstrap,registry}`、新增 `cmd/<变体>/main.go` | `main.go` 已 36 行 | 有第二入口需求时加 `cmd/<变体>/main.go`，不复制 main | 现在没有真实第二入口 | 按建议：先不做，等需求 | **已裁决（按建议判别）** |
| P15 | `store/root` test-only 导出收敛 | `store/root`、`store`（集成用例） | 为让集成用例伪造/replay 锁文件，多导出 6 项（D-M3-8） | 用例改行为级断言后可收回导出 | 取消导出会逼用例复制锁文件格式（第二份实现） | 按建议：先不做 | **已裁决（按建议判别）** |
| P16 | `store` 的 `RouterStorage` 端口反转 | `store`、`seelexctx/lifecycle`（向上依赖已在 M3 删除） | M3 用"断言整条上移消费侧"（端口化实测成环，D-M3-11） | 若将来要下沉契约层，先解环 | 无第二消费方 | 建议不做（除非出现第二消费方） | 建议不做 |
| P17 | 工具记录看板的"整机汇总"读面 | `store/record`、`middle/view/record`、钩子 | 用户裁决：读数**不进 Snapshot**、钩子**直连 JSONL** | 若真需"这台机器哪个工具最该优化"：加显式汇总（读多份 JSONL），不进快照 | 多一层聚合与缓存口径 | 按需实现 | 已裁决（V10） |

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
