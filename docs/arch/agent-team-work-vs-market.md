# Agent Team「team work」与市场常见多代理方案：机制差异、收益与数据依据

> 本文回答三个问题：Seelex 的 Agent Team 协作（下文称 **team work**）和市场常见多代理方案
> 到底差在哪、这样做好在哪、凭什么这么说（数据依据）。
>
> 口径声明（先读）：
> - **A 类证据 = 本次在本仓库实测**，每条都给了可复现命令；
> - **B 类证据 = 本仓库静态度量**，本次测得；
> - **C 类证据 = 仓库既有实测报告**，标注其版本/日期/口径（不是本次新测）；
> - **D 类 = 外部公开资料口径**，本次**未能在线复核**（`web_search` 返回 403 配额不足），
>   只作对照、不作结论。凡 D 类，文中一律显式标注"未复核"。

---

## 0. 一句话结论

市场常见方案大多把多代理协作**做成"消息传递 + 摘要回传"**：队友拿到的是一段被谁
打包过的上下文，交回的是一段总结；协作过程本身**没有一个权威、可逐条验收的账**。
Seelex 的 team work 反过来把账本放在第一位：**协作介质就是那份唯一的帧账本**，
每个角色的上下文是账本的**前缀投影**，队友的工作、主会话的工作、评审的裁决都落在
同一条 `seq` 序列上——所以"它做了什么"可以逐帧查，而不是靠它自己说。

---

## 1. 问题定义：多代理协作难在哪

| # | 失效模式 | 典型表现 |
|---|---|---|
| F1 | **摘要回传丢证据** | 子代理说"已完成，改了 3 个文件"，父代理只能信；失败时无法回放它到底看了什么 |
| F2 | **没有权威记录** | 协作过程散在各自的 message history 里；换框架/换进程就没了；多个 agent 各记一份，彼此不一致 |
| F3 | **上下文靠手工打包** | "给子代理喂什么"是开发者/父代理的临时决定；喂多了浪费、喂少了它就是盲的，且无从审计 |
| F4 | **人类只在头尾** | 人只能给整任务、看最终结果；中途换档、撤权、叫停、审阅都没有一等公民入口 |
| F5 | **重启即失忆** | 状态在内存/框架对象里；进程重启后无法续做，也无法解释"它当初为什么那么做" |

Seelex 的设计取舍就是逐条对着 F1–F5 来的。

---

## 2. Seelex team work 的机制（每条带代码锚点）

1. **单一帧账本**：会话里每一次进出都是账本上的一帧，`seq` 单调递增、带 `role_name`。
   队友的行与主会话的行**同账本、同序号空间**——不是两套 history 互相 CV。
   - 存储层：`sessionstore/role_session.go`；契约：`application/contract/dto/rolesession.go`
2. **上下文 = 账本前缀投影（不是"打包一段给它"）**：角色记录自切点 `C` 起算
   （`seq > C` 才属于它），`C` 由 `join_seq_id`（入伙那一回合）决定，compact 之后由
   `applied_seq` 抬高；**main 恒为 `C=0`**（主会话复用主会话自身，不受入伙闸门约束）。
   - 装配与判据：`application/core/agentteam/runtime.go`、`sessionstore/role_session.go`
   - 钉住判据的测试：`application/core/agentteam/prefix_test.go`、`sessionstore/role_session_test.go`
3. **两把尺子，别混用**：
   - `assembleRoleWire`（**喂模型**的那段）= "此刻的共享上下文"；
   - `RoleSnapshot`（**给人看**的那份）= "它入伙以来的账"。
   两者共用同一判据、内容不同。面板上渲染的占位（`—`）就是"它其实没看到这一帧"的
   诚实表达，而不是把主会话的历史伪装成它记得的上下文。
   - 前端：`gui/frontend/dist/agent-team-view.js`（`renderRoleRecordTable`）
4. **目标可嵌套（活动栈）+ 回合制评审**：goal 不是单值，而是**活动栈**（嵌套压栈、
   栈顶为当前目标、栈下是被压栈暂停的目标）；ADVISOR(tl) 按回合制推进，指令与裁决
   也落在同一账本上（权威正文是 `tl_directive` 帧，进行中的只读近端是 `in_flight`）。
   - `application/core/goal_coordinator.go`、`goal_service.go`、`goal_work_summary.go`
   - 测试：`application/core/goal_team_wiring_test.go`、`goal_directive_visible_immediately_test.go`
5. **角色有生命周期，不是"临时 spawn 一个"**：员工母本（全局）+ 本会话在编；
   团队只负责"装配/顺序/呼叫"；发言顺序 `order_roles` 拖拽即提交。
   - `application/core/agentteam/{runtime,scheduler}.go`、`dto/agentteam.go`
6. **人类控制面是一等公民**：会话粒度权限档（manual/edit/auto/full）、工具权责模型
   （主体 × 路由组 × 位 + sudo）、审批面板、员工在编/入库。
   - `docs/arch/agent-permission-subjects.md`、`gui/bridge.go`
7. **渲染层零回写**：Team 面板与 Goal 面板都是**纯渲染件**（Goal 活动栈、角色记录、
   权限档下拉都没有写回后端的入口）。真值只在存储层，前端不持有"我以为选了什么"。

---

## 3. 与市场常见方案逐条对比

> 表中的"主流方案"一列是**公开文档口径的机制描述**，不针对任何产品的具体版本；
> 涉及数字的外部件（见 §5.D）本次未复核。

| 维度 | Seelex team work | 主流多代理方案（示例口径） |
|---|---|---|
| **协作介质** | 唯一帧账本（同 `seq` 空间，逐帧带角色） | 消息传递（AutoGen/CrewAI/LangGraph 类）；或父↔子摘要回传（CLI 类子代理）；或 git 合并（并行分支类） |
| **队友上下文从哪来** | 账本**前缀投影**（`seq > C`），判据可算、可审计 | 父代理/开发者**临时打包**一段提示；或共享全部历史；或只给任务描述 |
| **越权上下文** | 结构上不可能（入伙前的帧不在它的记录里；缺 `compact_ref` 直接报错，不静默兼容） | 取决于调用方是否夹带；无统一闸门 |
| **"它做了什么"怎么验** | 逐帧查（`seq` + 行 + 工具调用 + 证据） | 多数靠子代理的自述摘要；或翻各自的 log |
| **进程重启** | 账本在存储层，重启后按前缀重建 | 框架内存态/对象图，重启通常丢失或需自建持久化 |
| **目标形态** | **活动栈**（可嵌套、可回退），栈上每帧带验收条件 | 单值任务/单次 run；嵌套靠嵌套调用 |
| **质量对抗面** | ADVISOR 回合制评审，裁决写入同一账本 | reviewer 角色（CrewAI/MetaGPT 类）或人工 merge；多数无"裁决入账" |
| **人类介入点** | 会话粒度权限档 + 审批面板 + 员工在编，中途可换 | 多在头尾（派任务/看结果）；中途干预要打断 run |
| **角色生命周期** | 员工母本 + 在编 + 发言顺序（可拖拽、可复现） | 每次 run 现建 agent；或代码里硬编码 crew |
| **失败模式** | 面窄：某角色的会话坏了不影响账本；面板发现 `unassigned_role_rows>0`、切点异常等 | F1–F5 里至少中一条 |

**和"反多代理"论点（Cognition 一类）的关系**：该论点担心并行子代理把上下文切碎、
各写各的。Seelex 的前缀匹配恰好是同一担心的结构性回答——**每个角色的可见上下文是
账本的一段确定前缀**，不是各自summary 拼接；而它的产出回落到同一账本，不需要靠
merge 拼回一致性。

---

## 4. 这样做的好处（按用户价值排序）

1. **可验收（最核心）**："队友做完了"必须落到某条 `seq` 上，且能看到它当时能看到的
   上下文边界（占位 `—`）。验收不依赖它自己的措辞。
2. **可追溯、可诊断**：切点可算（`join_seq_id` / `compact_ref.applied_seq`），
   异常有明确症状表（见 `docs/2026-09-16-team-work-record-dataflow/README.md` §7）。
3. **上下文成本有界**：每个队友只吃前缀那一段（不是整段会议记录），喂多/喂少不再是
   调用方的自由裁量，而是可解释的规则。
4. **质量有对抗面**：评审回合制 + 裁决入账；"谁在什么时候说了什么"不靠回忆。
5. **重启与续做**：账本在存储层，恢复到某帧即可续做；这也是"作品集演示"可复现的基础。
6. **人类可控且权限收敛**：会话粒度档位 + 工具位模型，撤权/提权都是一次操作，
   且不改变账本语义（权限是主体×路由组的判据，不是协作介质的属性）。
7. **文档与测试可当验收标准**：口径由测试钉死（B 类证据），文档里写的行为有代码锚点。

---

## 5. 数据依据

### 5.A 本次实测（本仓库，可复现）

| 项目 | 命令 | 结果 |
|---|---|---|
| 前端用例 | `node --test gui/frontend/dist/**/*.test.mjs` | **316 passed / 0 failed**（2.30s） |
| Go 编译 | `go build ./...` | exit 0（无输出） |
| 新 GUI 暂存构建 | `scripts/seelex-flow.ps1 -Stage Stage` | `dist/stage-gui/seelex-gui.exe` 28.4 MB，SHA256 `FD4A0F5F44BCF3999FBD5DAFCE457CCA2EABA9B67D668BE9011577221E197158` |
| 无头冒烟 | `scripts/seelex-flow.ps1 -Stage Smoke` | `[1/2] version exit=0 output=dev` PASS；`[2/2] boot exit=0 ready=True` PASS（`/help` 渲染成功） |
| 可分发 GUI 包 | `scripts/build-gui.ps1 -Version dev -BuildKind Dev` | `dist/archive/seelex-vdev-windows-amd64-gui.zip` 10.5 MB + `.sha256` |
| 包内 exe 是否含新前端 | `findstr /C:"<marker>" <exe>` | `goal-stack-table`=2、`role-record-table`=2、`perm-menu-item`=6、`perm-picker`=3（与暂存 exe 命中数一致） |

### 5.B 仓库静态度量（本仓库，本次测得）

| 指标 | 数值 |
|---|---|
| Go 测试函数 | **1985** 个（`func Test` 计数） |
| Go 测试文件 | **486** 个 `_test.go` |
| 前端测试文件 / 用例 | 36 / 316 |
| team work 关键模块规模 | `gui/frontend/dist/agent-team-view.js` 927 行、`gui/bridge.go` 1099 行 |
| 机制文档面 | `application/core/README-agentteam.md` 16.3 KB、`application/core/README-goal.md` 21.6 KB、`gui/frontend/README.md` 53.8 KB |
| 该机制的活体探针（需真实模型） | `gui/{team_live_probe_test.go, team_workcontent_live_probe_test.go, goal_team_wiring_live_probe_test.go, role_live_probe_test.go}` |

> 说明：B 类只能说明"这套口径被多少测试和文档钉住"，**不能**说明"它比别的方案快多少"。
> 后者需要 A/C 类口径的端到端基准（见 §6 未验证项）。

### 5.C 仓库既有实测报告（口径：TUI/backend，2026-08-05，`.seelex/perf/REPORT-latest.md`）

| 指标 | 旧构建 | 新代码 |
|---|---|---|
| 串行 warm 吞吐 | 0.288 req/s | **0.401 req/s（+39%）** |
| 单轮对话 P50 | 3.71 s | **3.18 s** |
| 工具调用 P50（get_time 链路） | 4.30 s | 4.67 s（工具本身 P50≈1 ms，差异来自上游 LLM） |
| 连续工具调用成功率 | 7/8 | **8/8** |

同报告的真实计费数据（整任务，非仅产品自身）：**37,689,781 token**，其中
**输入命中缓存 97.6%**、输入未命中 1.8%、输出 0.6%。

> 这张表对我们的论证价值在于：**多代理框架的开销大头是"反复喂上下文"**，
> 而 Seelex 的前缀投影正是把"反复喂"压到"只喂该看的那段"，并让主会话缓存继续命中。
> 注意：该报告是**历史版本、TUI 口径**，不包含 team work 的专用基准。

### 5.D 外部公开资料（**本次未复核**，仅作对照）

以下三条是写作时依据的公开口径，本次 `web_search` 因配额 403 不可用，**未复核**，
请在使用前自行核对原始出处：

1. 厂商级多代理研究系统公开数据（Anthropic 类）：多代理相较单代理在其内部评测上有
   显著提升，同时 token 消耗远高于普通对话（数量级差异）。
2. "不要构建多代理"一方的公开论点（Cognition 类）：并行子代理导致上下文碎片化，
   主张单线程上下文 + 压缩。
3. 框架类方案（AutoGen / CrewAI / LangGraph / MetaGPT 系）：机制为消息传递、
   角色流水线或 SOP 分阶段产出结构化产物。

**为什么不把这三条写进结论**：未复核的数字放进作品集是风险，不是证据。

---

## 6. 局限与未验证项（诚实清单）

1. **没有做"多代理 vs 单代理"的在线 A/B 基准**：本文的对比是机制层 + 静态/结构证据层，
   不是速度/成功率对比实验。
2. **没有 team work 的 token 前后实测**：前缀投影省了多少上下文，目前只有"规则可算"
   与 C 类旁证，缺一次同任务、同模型的对照测量。
3. **外部数字未复核**（§5.D），作品集里引用前必须回原出处。
4. **活体探针需要真实模型**（`gui/*_live_probe_test.go`），本次未运行；它们才是
   "这套协作在真实模型下跑通"的证据面，需要单独一次带凭据的验证。
5. **样本与时间**：C 类报告为 2026-08-05 单沙箱口径，样本 8–25 条/组，存在上游 API 抖动。

---

## 7. 参考（代码与文档锚点）

- 记录与数据流：`docs/2026-09-16-team-work-record-dataflow/README.md`
- 团队运行时与调度：`application/core/README-agentteam.md`、`application/core/agentteam/{runtime,scheduler}.go`
- 目标与评审：`application/core/README-goal.md`、`application/core/goal_coordinator.go`
- 权责模型：`docs/arch/agent-permission-subjects.md`
- GUI 事实源：`gui/README.md`、`gui/frontend/README.md`
- 既有性能报告：`.seelex/perf/REPORT-latest.md`（2026-08-05，TUI 口径）
