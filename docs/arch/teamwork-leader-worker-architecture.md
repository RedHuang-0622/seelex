# Teamwork 目标架构：leader + 异步 worker（子进程工具调用范式）

> **用途**：把 Agent Team（teamwork）从「席位同步轮转」重构为「**leader 异步调度 worker**」的**详细设计 + 里程碑**，
> 作为下一会话（含 Seele 框架侧）的实现输入。
>
> **口径**：本文＝**目标设计（target design）**。凡「现状」＝本轮静读、带 `文件`/`文件:符号` 锚点；凡「设计/草案」＝提案。
> **本文不改代码。** Seele 本地检出 `G:\program\go\seele`（`github.com/RedHuang-0622/Seele`，go.mod `v0.3.2`，当前无 replace）。

---

## 0. 结论与已拍板决策

**一句话**：把「团队 = 一排轮流发言的座位」换成「**团队 = 一个 leader + 一组可被派发/观察/终止的 worker 作业**」；
把 Seelex 里已验证的**异步作业面**（子进程调用契约）**通用部分**（契约 + Manager + 管理工具 `jobs_manage`）**下沉为 Seele 原生 `jobs` 根能力**（派发侧 `bash_bg` 等仍留 Seelex）；
teamwork 的硬编排写进**独立存储 `moduleTeamwork`**，由 leader 掌控顺序、阻塞与里程碑。

**已拍板（累计确认）**：

| # | 决策 | 落点 |
|---|---|---|
| D1 | **`jobs` 契约 + Manager + `jobs_manage` 通用管理工具进 Seele**；**`Executor` 与派发侧产品工具（`bash_bg`/`read_batch`/`fork_subagents`）留 Seelex** | §2、§3 |
| D2 | **teammate = 长驻会话（一人一会话一 worktree）** | §4.2、§4.4 |
| D3 | **硬编排用新存储 `moduleTeamwork`** | §4.6 |
| D4 | **废弃旧顺序字段；顺序由 mainagent 掌控；goal 座位循环降级为 `jobs` 契约下的一种 Executor** | §4.6、§7、§9 |
| D5 | **限制 teammate 人数** | §4.3 |
| D6 | **teammate 的子代理：不设开关，硬移除 `fork_subagents`** | §4.3 |
| D7 | **teammate 生命周期结束（mainagent 判定）→ 释放 worktree + 删会话记录内容，保留 teammate 在线** | §4.4、§4.7 |
| D8 | **一个 teammate = 一个角色，禁止重复角色** | §4.2 |
| D9 | **版本/分支管理一律走 git，Seelex 不内置管理系统** | §4.7 |
| D10 | **job 作用域取 O3 形态，但用「会话 + 主体」两个并列字段表达，不拼字符串**；归属打点沿用既有 `BatchID` 盖印章范式 | §3.1、§10.1 |

---

## 1. 现状 vs 目标（范式差距）

| 维度 | 现状（席位制） | 目标（leader + worker 作业面） |
|---|---|---|
| 驱动 | goal 治理循环 `govern.TurnGovernor` **同步**推座位（`goal_coordinator.go` 的 `AdvanceAfterChat`） | **leader 派发 worker 作业**（`jobs.Manager.Dispatch` → handle），worker 并发 |
| 并发 | 座位串行（一轮一座） | **非串行**：N 个 worker 同一管理器下并行 |
| 隔离 | 角色会话 `role_session_id`，无独立工作区 | 每个 teammate **绑定 worktree**（`seelebridge/worktree`，git）+ `emp_<role>` 主体 |
| 结果回传 | 座位 `Act` 同步返回一句话 | **作业句柄** `observe/fetch/kill/done`，消费式增量 |
| 通知 | 无 | **变更信号口** + **回合边界有界摘要**（不唤醒忙会话） |
| 顺序/阻塞 | `lifecycle.order_policy/order_roles` | leader 掌控的**硬编排计划** + 显式 join（`moduleTeamwork`） |
| 框架 | `tools` 全同步，无作业概念 | Seele `jobs` 根能力 |

**Seele 现状（本轮静读）**：`tools.ToolHandler.Execute(ctx,argsJSON)(string,error)` 全同步；`workplan` 有并行 DAG（scheduler/forkexec）；
`session` 一次会话串行（回合闸门）、多会话可并发、`Reset(ctx)` 可清空；`event` 有 Sink/Recorder/Heartbeat；`tools/permission` 有 Subject×Group×Bits+sudo。
**唯独缺**「把一次长任务表达为可派发/可观察/可终止的作业」那层契约。

**Seelex 现状（要下沉的范式原型）**：`seelebridge/tools/job_contract.go` 头注即「**作业契约**」——
派发（`bash_bg`/`read_batch`/`subagent`）→ handle → `job_manage(observe/fetch/kill/done)`；
三类作业（`JobKindProcess`/`JobKindInline`/`JobKindSubagent`）**共用同一张登记表与同一套状态机**，
管理面只有一份实现（`jobManager`）；隔离靠 `JobSpec.SessionID`（`Router.CloseSessionAsync` 会话销毁即杀），
归属靠 `JobSpec.BatchID`（chat 请求批次，task/todo/plan/subagent 条目自动盖章）——**两个并列字段，不拼字符串**（见 §10.1）。

---

## 2. 分工：Seele `jobs` vs Seelex `Executor`（D1，已细化）

```text
┌───────────────────────── Seele（无产品语义）─────────────────────────┐
│  jobs ← 新根能力：契约(Scope/Spec/Record/State/Handle/Executor/Sink)   │
│         + Manager（作业表 / 状态机 / 四动作）+ jobs_manage 管理工具      │
│         + 权限 / 事件 / 作用域回收接线                                 │
│  tools · tools/permission · session · workplan · event · agent（既有） │
└──────────────────────────────────────────────────────────────────────┘
   ▲ 注册 Executor + 调用 Manager                边界门禁：jobs 不 import seelex
┌───────────────────────── Seelex（产品层）────────────────────────────┐
│  派发侧工具（**留在 Seelex**）：bash_bg / read_batch / fork_subagents  │
│  Executor 实现：process(shell) / inline(读扇出) / worker(teammate 会话)│
│                  / seat(goal 座位循环，D4)                             │
│  leader(主代理) + team plan(moduleTeamwork) + worktree(git) + 里程碑  │
└──────────────────────────────────────────────────────────────────────┘
```

**判据（D1 细化）**：「换个产品也成立、且与具体命令语义无关」的进 Seele——**契约 + Manager + `jobs_manage`（管理侧）**；
「与具体产品命令/提示词语义绑定」的留 Seelex——**派发侧工具（`bash_bg`/`read_batch`/`fork_subagents`）与全部 Executor**。
即 **管理侧通用、派发侧产品化**。`jobs` **禁止** import 任何 seelex 包（边界门禁）。

---

## 3. Seele `jobs` 详细设计（契约 + 管理器 + 通用管理工具）

> **边界**：Seele 只提供**契约**与**契约默认需要的方法**（Manager 四动作 + `jobs_manage`）；
> `bash_bg` / `read_batch` / `fork_subagents` 的**实现仍在 Seelex**，它们只是 `jobs.Manager` 的调用方。
> 这样 Seele 侧零产品语义，Seelex 侧保留命令/提示词的全部控制权。

### 3.1 契约

```go
package jobs // G:\program\go\seele\jobs

type Kind string  // 开放字符串；框架不解释具体值，由调用方注册 Executor
const (
    KindProcess Kind = "process" // 外部进程（进程树可终止）
    KindInline  Kind = "inline"  // 进程内扇出（取消靠 ctx）
    // seelex 侧再注册：KindWorker("worker")、KindSeat("seat")（§7）
)

type State string
const (StateRunning State="running"; StateDone State="done"; StateFailed State="failed"; StateKilled State="killed")

type Handle string

// Scope 是作业的**作用域**（隔离 + 回收）：两个**并列字段，不拼成一个串**——
// 对齐既有 todo/task/subagent 的绑定范式（JobSpec.SessionID + 盖章 BatchID），
// 避免自造分隔符带来的转义/碰撞规则（理由与取舍见 §10.1）。
type Scope struct {
    Session string // 会话键：与既有 JobSpec.SessionID 同源（Router.sessionKey）
    Subject string // 主体：teammate 分组维度（emp_<role>）；空 = 主代理
}

type Spec struct {
    Kind        Kind
    Scope       Scope           // 作用域：{Session, Subject}——隔离 / 回收 / 分组（§10.1）
    Node        string          // 归属打点：编排节点 / 团队阶段（盖印章，**不参与鉴权**）
    Batch       string          // 归属打点：派发它的那次 chat 请求（同既有 BatchID）
    Description string          // 作业行标题（投影 / 打点）
    Payload     json.RawMessage // 由 Executor 解释
    Index       int             // 同批内排序键（派发序）
    Dedup       string          // 去重键（仅 running 期间生效）
}

type Record struct { // 只读投影；句柄活在内存，永不落盘（沿用不变量 I-21）
    Handle Handle; Seq int; Kind Kind; State State; ExitCode int
    Scope Scope; Node string; Batch string // 归属（与 async_probe.JobInfo 同字段集）
    Description string; OutputRef string; Bytes int64; Lines int
    Summary string   // 终态有界摘要（≤512B）；按轮重播，必须严格有界
    Cursor int64; Truncated bool; Degraded bool
    StartedAt, EndedAt time.Time
}

type Manager interface {
    Dispatch(ctx, Spec) (Handle, error)                 // 立即返回受理回执（不含输出）
    Observe(handle) (Record, bool)                      // 只读，不推进游标
    Fetch(ctx, handle, FetchBudget) (chunk string, rec Record, err error) // 消费式增量
    Kill(ctx, handle) error
    Done(ctx, handle) error
    // Snapshot：Subject 空 = 该会话全部在册作业；非空 = 该会话内该主体（两档语义，§10.1）
    Snapshot(scope Scope) []Record
    // Reclaim：Subject 空 = 会话级回收（逐位等于既有 CloseSessionAsync）；非空 = 只回收该主体名下
    Reclaim(ctx, scope Scope) error
    Events() <-chan struct{}                            // 变更信号口（容量 1，latest-wins）
}

type Executor interface { Kind() Kind; Start(ctx, Spec, Sink) error }
type Sink interface { Note(text string); Complete(state State, summary string); SignalBytes() }

type Option func(*options)
func WithExecutor(Executor) Option                  // 注册执行体
func WithLimits(Limits) Option                      // 在途上限/记录槽/硬上限/输出上限/节流
func WithSubjectResolver(func(ctx) string) Option   // 从执行 ctx 解析主体（emp_<role>）
func WithReclaimer(func(ctx, Scope) error) Option   // 作用域回收钩子（见 3.2 / §10.1）
```

### 3.2 状态机与收尾（不变式，逐条可测）

| 不变式 | 内容 |
|---|---|
| I-1 收尾恰好一次 | 正常退出 / 硬超时 / 被杀 / 执行体 panic **四条路都必须落到 `finish` 恰好一次** |
| I-2 退出码语义 | 硬超时合成 `exit=124`、被杀合成 `exit=137`，注记写进输出文件（否则模型只看到"突然结束"） |
| I-3 句柄唯一 | 句柄 `a<seq>`（seq 单调，禁用 `len(runs)` 推——驱逐后会重号）；去重键=作用域+载荷，仅 running 期间生效 |
| I-4 句柄不落盘 | 记录/句柄只在内存；写进持久化会留下"永远 running 的假行"（I-21） |
| I-5 回收 | 程序收尾时关进程树句柄（Job Object `KILL_ON_JOB_CLOSE`）是唯一确定回收点；**作用域销毁即杀**（`Reclaim`） |
| I-6 输出有界 | 输出文件字节上限（超限截断仍向子进程报"已消费"，避免把基础设施限制伪装成命令失败）；增量=文件偏移游标 |
| I-7 跨作用域拒绝 | 取回/终止一律拒绝跨 `Scope` 访问（照抄既有 `run.sessionID != sessionID` 的判据，扩成 Scope 不等即拒） |

> 这些是 seelex 异步面现有不变式的**框架化**（`seelebridge/tools/job_contract.go` 头注 + `async_exec.go` + `tools/README.md`），落地时以既有测试当回归。

### 3.3 事件与信号

- **事件流由 Seelex 构建，框架只给信号口与读面**：`jobs` 不 import `event`、不建 `event.Recorder`、不发 `event.Sink`（没有 `WithEventSink`）。理由：`event.Sink` 的实现必须在 `jobs.Manager` 的**构造期**定下，而构造期拿不到"这条作业属于哪个会话的哪条事件流"——框架的 `Recorder` 是单例、**序号全局**，会话事件库却是**按会话**追加/排序的。这样发出来的事件 **append 不到会话事件流的尾部**，只能由产品事后**回填**会话归属，且全局序号在按会话排序下不成立。Seelex 侧（`seelebridge/jobs_events.go`）改为订阅 `Events()`，用 `Snapshot` 取到 `Record.Scope.Session` 之后自行投影并追加（补 `agent.runtime` 定位、序号取时间基 `uint64(at.UnixNano())`，与 Seelex 既有事件同策略）。
- `Events()` 是**变更信号口**：派发/终态/新字节三类触发；容量 1、latest-wins、**不推进游标、不进上下文**。
- **无 push 唤醒**：框架**绝不**把结果投递进忙会话（详见 §6.1）。

### 3.4 权限与归属接线

- 派发工具的 `ToolMeta{Kind: ToolKindWrite, Groups, Bits: BitWrite|BitExecute, Resource}` → 由 `tools/permission` 在**分发前**鉴权（派发侧工具在 Seelex 声明，见 §5）。
- `Scope.Subject` 由 Manager 解析后交给 Executor；Executor 把主体放进执行 ctx（参考 `seelebridge/runtime_role_turn.go` 的 `tools.WithEmployeeSubject`），执行体内所有工具按主体权责收窄。
- **归属与隔离分两处**：`Scope` 决定「能不能取 / 要不要回收」（参加鉴权），`Node`/`Batch` 只做打点归属（**不参加鉴权**）——照抄既有 `SessionID` 与 `BatchID` 的分工。
- `kill` 亦鉴权：无权杀这一条则拒绝。

### 3.5 包布局（建议）

```
jobs/
  job.go        # Kind/State/Handle/Scope/Spec/Record/FetchBudget
  manager.go    # Manager 实现（表 + 状态机 + 四动作 + Snapshot/Reclaim）
  executor.go   # Executor/Sink 契约
  options.go    # Option/Limits
  events.go     # 事件与信号口
  builtin/      # 管理侧通用工具：jobs_manage（observe/fetch/kill/done）
                # 注意：派发侧（bash_bg/read_batch/fork_subagents）不在 Seele，见 §2/§4
  README.md
```

---

## 4. Seelex 侧详细设计

### 4.1 生态位变化

| 角色 | 现在 | 目标 |
|---|---|---|
| **主代理** | EXEC 座位（唯一 writer，亲自干活） | **leader**：编排（派发/观察/收口）+ 关键路径工作；不用座位表达 |
| **techlead/TL** | ADVISOR 座位（同步评审） | **review worker**：由 leader 派发；独立上下文 + 受限执行位 |
| **员工（pm/exec/test…）** | `roleTurnSeat`（同步一轮一句） | **worker**：长驻会话 + worktree + `emp_<role>` 主体，作业化并发 |

### 4.2 teammate 模型（一人一会话一 worktree / 一角色一 teammate，D2+D8）

- **绑定关系（唯一事实）**：`teammate ≙ { role_name, role_session_id, worktree, permission_groups }`。
  `role_session_id` 由 `agentteam.RoleSessionID(mainSessionID, teamID, roleName)` 派生
  （`(主会话, team_id, role_name)` 决定，重复装配幂等）。
  **主会话身份是必需分量**（2026-10-01，用例 2）：两个会话召唤同一支团队时，各自拿到
  工厂派发的自己的成员实例；不含主会话身份时角色引擎槽、项目根绑定、权责反查都会把
  两边当成同一个人。
- **一角色一 teammate、禁止重复**：`agentteam.Normalize` 已在装配侧拒重复（`agentteam: duplicate role %q`，`spec.go`）；
  新计划层**必须再校验一次**（计划可被 leader 改写，不能只靠装配侧）。
- **禁内置角色当 teammate**：`user`/`main` 复用主会话（`needsRoleSession`），不得作为 worker 派发。
- **worktree 归属**：优先在计划里给 teammate 指定 `worktree`（如 `seelex/<role>`）；缺失回退共享主工作区（沿用 `worktree_manager.Begin` 的降级语义）。

### 4.3 teammate 资源约束（D5+D6）

| 约束 | 设计 | 落点 |
|---|---|---|
| **人数上限** | `limits.team.max_teammates`（配置；默认取与 `ResidentSessionLimit` 同量级，建议 **6**）。超限的 `team_dispatch`/`team_plan` **显式拒绝**（不静默排队） | `seelexctx/limits.go`（新增 `TeamLimits` 块）+ 计划校验 |
| **子代理策略** | **不设开关：teammate 工具面直接不注册 `fork_subagents`（硬移除）**。理由：① 一角色一 teammate 已定，嵌套子代理会**绕过人数上限**；② teammate 绑 worktree，子代理会派生更多 worktree——**生命周期说不清就会留下删不干净的 worktree 与孤儿作业**；③ 需要并行广度时，正确做法是 **leader 多派几个 teammate**，而不是让 teammate 再分叉 | teammate 工具面装配（`seelebridge/tools` 装配点） |

> 与在途上限的关系：`jobs` 的在途上限（现 32）是框架级兜底；**团队人数上限是产品级约束，必须先于框架兜底生效**（否则"限制"形同虚设）。
> worktree 数量因此被 teammate 数量封顶（无子代理 ⇒ 无孙 worktree），再叠加 `team_retire` 即释放（§4.4），不会累积残留。

### 4.4 teammate 会话生命周期（D7：回收作业 + 释放 worktree + 删内容、保在线）

leader 判定某 teammate 的一轮任务结束 → `team_retire(role)`，**顺序固定**（先停作业、再释放工作区、最后清记忆，避免"清完记忆还在写"）：

| 步骤 | 动作 | 机制 |
|---|---|---|
| 1 | 回收该 teammate 名下未完成的作业 | `jobs.Manager.Reclaim(ctx, Scope{Session:"<主会话>", Subject:"emp_<role>"})`（只动这一个 teammate，不牵连同会话其他人；§10.1） |
| 2 | **释放 worktree**（节约存储，D7） | `seelebridge/worktree` 的 `Release`（`git worktree remove` + 删本地分支）；**释放前若工作区脏 → 先提交或按 `ErrUncommittedChanges` 语义显式报错，不静默丢弃** |
| 3 | 清空该 teammate 的**会话记录内容**（工作历史 + durable 快照、message 行/上下文栈） | Seele `session.Reset(ctx)` + sessionstore 清该 `role_session_id` 的消息通道 |
| 4 | **保留 teammate 在线** | 计划里的成员条目（`role_name`/`role_session_id`/`permission_groups`）**保留**、`worktree` 字段置空待重派；前端「员工在线」态不变 |

- **语义边界**：删的是**会话内容**（对话记忆）与**工作区检出**，不是**注册/在编**。下一次派发时 teammate 以**干净上下文 + 全新 worktree**开跑。
- **谁决定**：leader（mainagent）——通过 `team_retire` 或计划里的生命周期策略；框架只执行，不自行判定 teammate 死活。
- **为什么是"释放"而非"保留现场"**：teammate 不挂子代理（D6）后 worktree 数量被 teammate 数量封顶，`team_retire` 即释放，**不会出现删不干净的 worktree**。

### 4.5 leader 工具面与提示词

| 工具 | 作用 | 关键参数 |
|---|---|---|
| `team_plan` | 定义/更新硬编排计划（写 `moduleTeamwork`） | `team_id, stages[], members[], milestones[]` |
| `team_dispatch` | 派发一个 teammate 的作业（→ `jobs.Dispatch`，带 `Scope{Session, Subject:"emp_<role>"}` + `Node`） | `role, stage, goal` |
| `jobs_manage` | 观察/取回/终止/销项（Seele 通用工具，D1） | `op, handle` |
| `team_join` | **有界**汇合等待（真实依赖点才用） | `handles[], budget` |
| `team_milestone` | 声明里程碑 + **leader 撰写内容** | `id, content` |
| `team_retire` | 结束某 teammate 一轮任务（回收作业 + 释放 worktree + 清内容 + 保在线） | `role` |

提示词：把上述工具 + 计划语义写进 leader 的 skill（`plugins/default/*`），使「通过提示词原生驱动 goal 的 teamwork 所需一切」。

### 4.6 team plan 硬编排 + `moduleTeamwork`（D3+D4）

```jsonc
// session/<sid>/teamwork/plan.json
{
  "team_id": "v-model", "version": 1,
  "stages": [ {"id":"req","roles":["pm"],"depends_on":[]},
              {"id":"impl","roles":["exec"],"depends_on":["req"]},
              {"id":"test","roles":["test_case"],"depends_on":["impl"]},
              {"id":"review","roles":["tl"],"depends_on":["test"]} ],
  "members": [ {"role":"pm","role_session_id":"v-model-pm","worktree":"","permission_groups":{"ro":4}}, ... ],
  "milestones": [ {"id":"m-impl","after":["impl"],"required":["exec"],"status":"pending","content":""} ],
  "state": { "stage":"impl", "jobs":{"exec":"a12"}, "milestones":{} }
}
```

- `stages.depends_on` 是**顺序/依赖的唯一事实**（取代 `order_policy/order_roles`，D4）。
- 存储：sessionstore 新增 `moduleTeamwork`（枚举 + **独立锁 + head**；`mutexFor` 无 case 会 panic，必须补）。
  目录 `session/<sid>/teamwork/{plan.json, events.jsonl}`；与现有 `session/<sid>/team/roles.json`（在编成员）区分。
- **不落盘的内容**：作业句柄（内存，I-4）、worktree 路径/差异/补丁（git 的事，§4.7）。
- 旧字段退场：`lifecycle.order_policy/order_roles` 标注历史/只读，读面切换到计划。

### 4.7 版本 / 分支管理：git 统一，Seelex 不内置（D9）

- **唯一入口是 git**：worktree 创建/分支/rebase/merge/提交 全走 `seelebridge/worktree` 的 `GitRunner`（git 子进程），沿用 `NodeWorktree{Path,Branch:seelex/<id>,BaseCommit,MainBranch}` 语义。
- **Seelex 不建自有版本系统**：不存快照仓库、不做 diff/patch 数据库、不维护第二份"分支真相"；`moduleTeamwork` 只存**编排与绑定**（谁、什么顺序），不存内容。
- **合并门**：沿用 `worktree_manager` 的 `approve.ApprovalGate`（合并审批）——审批是产品语义，允许留在 Seelex；**版本事实**仍由 git 提供。
- worktree 失败现场保留（`ErrUncommittedChanges`）供人工恢复，不由框架静默删除。
- **释放时机（D7）**：`team_retire` 释放 checkout 与其本地分支（节约存储）；`moduleTeamwork` 只留 `worktree` 指派名（重派时重建），不留路径真相。
- **无孙 worktree**：teammate 不挂子代理（D6）⇒ worktree 只由 teammate 派生，数量封顶、可回收。

---

## 5. 权限接线

- **派发即鉴权**：`team_dispatch` / `bash_bg` 等派发侧工具带 `ToolMeta.Bits`，中间件分发前判定（`docs/arch/agent-permission-subjects.md`）。
- **执行体以主体运行**：worker 回合起手放 `emp_<role>` 主体；该回合工具面按角色权责收窄。
- **评审者"牙齿"**：review worker 给**只读 + 受限执行位**（能跑 test/lint/编译，不能改代码）——把裁决从"观点"变"证据"（对齐 MAST `task verification`）。
- **teammate 工具面裁剪**：worker 首轮即排除 `fork_subagents`（D6，硬移除）与不该有的写位。

---

## 6. 信号 / 通知 + leader 生态位

### 6.1 铁律：不得唤醒忙会话

异步面选型已证明：push 结果进会话必回闯 `ChatStream` 持有的会话锁，且迟到 `role=tool` 在 wire 层非法、破坏前缀缓存（`seelebridge/tools/README.md` 引 `docs/2026-09-24-async-tool-deferred-ack/README.md` §0）。故「通知」＝：
- **变更信号口**（`jobs.Events()`）→ 服务投影/UI，不推进游标、不进上下文；
- **回合边界有界摘要**（完成行 ≤512B）→ 在 leader 下一回合随请求尾部打点块回填（对齐 K-5 完成回填）。

### 6.2 顺序 / 阻塞由 leader 掌握

顺序写进 `stages.depends_on`（硬编排）；阻塞是**显式 join**（`team_join`，有界）；里程碑由 leader 声明并**撰写内容**。

### 6.3 leader 生态位（问询结论）

**「leader 阻塞与否」与「worker 是否推进」正交；只影响延迟/锁持有/token，不影响正确性。**

- **A 阻塞**（`fetch(wait)` 钉住）：单次最多钉 ~60s（`asyncMaxWaitMS`），用户输入与审批排队——仅"下一步强依赖"时用。
- **B 忙自己的事**（默认）：派发后继续关键路径/再派发，回合边界看摘要收敛。
- **C 关系不大**：完成经信号口+边界摘要自然浮现——仅当里程碑全由依赖边驱动时成立。

**默认组合：B 为主 + A 点状（汇合点）+ C 兜底**；**把顺序/阻塞建模进 team plan，而不是建模进 leader 的调用姿势**。

---

## 7. goal 的位置（D4：座位循环 = `KindSeat` Executor）

- **goal 域不变**：`goal.Controller` / 终态 gate / 逃生仍是「意图 + 完成判定 + 逃生」的宿主。
- **goal 座位循环降级**：`govern` 座位轮转（`newGovernor`/`seatPlan.seats`/`roleTurnSeat`）改为 Seelex 注册的一个 `jobs.Executor`（`Kind = "seat"`）。
  它不再是**与 jobs 并列的第二套驱动**，而是 jobs 契约下的一个实现：派发 → 跑有限轮座位 → 输出治理结论；不再直接长持主会话锁。
- **ADVISOR**：作为 review worker 的默认实现（`seat` 或 `worker` 执行体）。
- 效果：**驱动唯一化**（一切长任务都是作业），goal 从"驱动者"退回"目标状态 + 收口判定"。

---

## 8. 里程碑（M0–M4，详表）

> 每期**独立可验证**；纪律：**先建新面、后撤旧面**；每步跑 `go build ./...`、`go vet ./...`、`go test ./... -count=1`（含 `-tags "gui,desktop,production"`）。

### M0 — Seele `jobs` 根能力（框架：契约 + Manager + `jobs_manage`）
- **目标**：作业契约/管理器/管理工具进 Seele；行为与 seelex 现有异步面**逐字节对齐**。**`bash_bg` 等派发侧实现不动**（D1）。
- **交付物**：`G:\program\go\seele\jobs\{job,manager,executor,options,events}.go` + `jobs/builtin/`（`jobs_manage`）+ `jobs/README.md` + `docs/arch/1x-jobs-contracts.md`；
  Seelex：`go.mod` 加 replace；`seelebridge/tools/*` 的**记录/状态机**迁到 `jobs.Manager`（`Scope{Session,Subject}` 与既有 `SessionID` 逐位对齐；**派发工具签名与语义不变**）；`process`/`inline` Executor 落地。
- **触碰面**：`G:\program\go\seele\jobs\**`、`seelebridge/tools/{job_*,async_*}.go`、`seelexctx/limits.go`（`AsyncExec` 保留）。
- **验收**：seelex 既有 `async_*_test.go` + `job_contract_test.go` 全绿（`bash_bg`/`read_batch`/`job_manage` 行为不变，含 `TestCloseSessionAsyncKillsOnlyOwnSession`）；Seele `go test ./... -count=1 -timeout 300s` + `go vet ./...` + `go build ./...` 绿；`jobs` 无 seelex import。
- **退出判据**：作业面无回归；`jobs` 契约冻结（后续变更走 Seele 评审）。

### M1 — worker Executor + worktree（teammate 上线）
- **目标**：`KindWorker` = 在角色会话跑限量回合；teammate 绑 worktree + `emp_<role>`；人数上限与子代理策略生效。
- **交付物**：`seelebridge` worker Executor（`runtime_role_turn.go` 升格）；worktree 绑定/释放（复用 `seelebridge/worktree`，git）；
  `limits.team.max_teammates`（默认 6）；teammate 工具面**移除 `fork_subagents`（无开关）**。
- **验收**：能派发/观察/取回/终止一个 teammate 作业；`Reclaim(Scope{Session,Subject})` 只杀该 teammate 而不动同会话其他作业；worktree 创建与释放（git）；越权工具被拒；**超员被拒**；**重复角色被拒**；teammate 工具面**不存在** `fork_subagents`（连开关都没有）。
- **退出判据**：单个 teammate 端到端跑通（干净上下文 + worktree 落痕）。

### M2 — team plan 存储 + leader 工具面 + 提示词 + goal 座位降级
- **目标**：`moduleTeamwork` 落盘；leader 六件套工具；提示词驱动；goal 座位循环改 `KindSeat` Executor。
- **交付物**：sessionstore 新模块（枚举+锁+head+读写面）；plan schema 与校验（含一角色一 teammate、人数上限）；
  `team_plan/team_dispatch/team_join/team_milestone/team_retire` 工具；leader skill；
  `application/core/govern/*` 改为 `KindSeat` Executor 实现。
- **验收**：plan 落盘/重载一致；leader 用提示词跑通一条 V 模型流水线（req→impl→test→review，含一个里程碑）；`Spec.Node` 归属正确落到里程碑聚合；里程碑事件落 `event.Sink`；顺序链式派发且里程碑注入验证通过。
- **退出判据**：**提示词即可驱动完整 teamwork**（无需手改代码）。

### M3 — teammate 生命周期（回收作业→释放 worktree→清内容→保在线）+ 信号/里程碑注入
- **目标**：`team_retire` 按固定顺序执行；完成摘要与里程碑帧在回合边界注入。
- **交付物**：`jobs.Reclaim` + `worktree.Release` 接线 + `session.Reset`/sessionstore 清 `role_session_id` 消息通道；roster 保在线；边界摘要/里程碑帧注入。
- **验收**：retire 后**会话内容为空、worktree 已释放（`git worktree list` 无残留）**且 teammate **仍在册在线**；下一任务上下文干净；脏工作区按语义报错而非静默丢弃；忙会话**不被唤醒**（无 push）。
- **退出判据**：长驻 teammate 跨任务复用不腐蚀上下文、不残留 worktree。

### M4 — 清场（旧顺序字段 + 死代码）
- **目标**：`order_policy/order_roles` 退场；死代码删除；文档同步。
- **交付物**：字段迁移/只读化；§9 清单逐条处理；`role_turn.go`/`contract.RoleTurnPort` 归并或退场；文档/README 刷新。
- **验收**：无引用、无回归、`go vet` 静默、`e2e` 文档门禁绿。
- **退出判据**：仓库只剩一套顺序事实（team plan）与一套驱动（jobs）。

---

## 9. 死代码处理（M4，逐条确认，不批量删）

| 候选 | 现状锚点 | 处理 |
|---|---|---|
| goal 治理座位循环 | `application/core/govern/`、`goal_coordinator.go` 的 `newGovernor`/`seatPlan.seats`/`roleTurnSeat`/`newRoleTurnSeat` | 降级为 `KindSeat` Executor（D4），原座位派生逻辑随之收敛或退场 |
| ADVISOR 座位 | `application/core/goal/advisor.go`、`NewAdvisorSeat` | 转 review worker |
| team 环与逃生 | `application/core/agentteam/{scheduler,runtime}.go`——**已接线**（生产消费 `Order()` / `SetPrefix()` / `SetOrder()` / `Snapshot()`；原写"未接线"是旧结论，2026-10-01 按事实更正） | `TurnScheduler` 的无消费者接口**已退场（2026-10-01，#3）**：channel 投递（`Requests`/`Request`/`Next`）、顺序编辑三件（`Move`/`Remove`/`Restore`）、只读 getter `Prefix`，见 [`../devlog/2026-10-01-turn-rotation-retired.md`](../devlog/2026-10-01-turn-rotation-retired.md)。环与逃生仍 live（`NoteTurn` 记账 + `TeamView.schedule`）；`Advance`/`Runtime.Next` 同样没有生产消费者但**未删**（逃生 ③`no_executor`/④`empty_ring` 的唯一计算点，删=删行为）。退场条件 = team plan 成为唯一顺序事实（届时再定推进路径与"逃生并入 `jobs` 管理面"） |
| 座位派生读面 | `application/core/agentteam_runtime.go`（`teamRoleSeatsFor`） | 若 team plan 成唯一顺序事实则退场 |
| 旧顺序字段 | `lifecycle.order_policy/order_roles` | 转历史/只读，读面切到计划 |
| `RoleTurnRunner` 适配 | `application/core/role_turn.go`、`contract.RoleTurnPort` | 若 worker Executor 走 `jobs` 契约则退场/改名 |

---

## 10. 已决决策 + 剩余待议

**本轮新增已决（承上）**：
- **D6 收紧**：`teammate_subagents` **不保留任何开关** —— teammate 工具面硬移除 `fork_subagents`（理由见 §4.3）。
- **D7 补充**：`team_retire` 四步固定（回收作业 → **释放 worktree** → 清会话内容 → 保在线，§4.4）。
- **D1 细化**：`bash_bg` **不搬 Seele**，仍由 Seelex 实现；Seele 只提供契约与默认需要的管理方法（`jobs_manage`）。
- **D10（O3 落定）**：job 作用域 = `Scope{Session, Subject}` **两个并列字段**，不拼字符串；见 §10.1。

**本轮拍定（原「剩余待议」两条，现为 D11/D12）**：
- **D11 `max_teammates` 默认值 = 6**（与 `ResidentSessionLimit` 同量级）。落点：
  `seelexctx.TeamLimits.MaxTeammates`（配置 `limits.team.max_teammates`）；零值 → 常量
  `DefaultTeamMaxTeammates = 6`，**负值在 `LoadLimits` 启动期报错**（不让负数有语义）。
  超限的 `team_dispatch` / `team_plan` **显式拒绝**（不静默排队）。M1 实测后只调数字，不改契约。
- **D12 计划带 `events.jsonl` 追加审计面 = 要**。落点：`sessionstore` 的 `moduleTeamwork`——
  计划落模块 head（等价 `plan.json`：原子发布 + 校验和自愈），审计落**同模块的数据文件**
  `teamwork/events.jsonl`（只追加、不重写；崩溃残尾跳过，不因此读不出前面的行）。
  审计面记 `plan / dispatch / join / milestone / retire` 五类事实。

### 10.1 job 作用域键：**已定 O3 语义，用两个并列字段表达（不拼字符串）**

**结论**：作用域 = `Scope{Session, Subject}`，**两个并列字段**；`Snapshot`/`Reclaim` 按字段匹配，**不引入任何分隔符**。

**为什么不拼串**：既有 todo / task / subagent 的绑定范式已经把这件事做对了——
`JobSpec.SessionID`（隔离 + 回收，`Router.CloseSessionAsync` 按它杀）+ `JobSpec.BatchID`（归属盖印章，
由 `Router.currentBatch` 注入，task/todo/plan/subagent 条目自动盖章）**是两个并列字段**，
跨会话访问靠 `run.sessionID != sessionID` 直接拒绝（`seelebridge/tools/job_contract.go`）。
自造 `a/b` 或 `a::b` 只会新增**转义规则与碰撞面**（会话 id、角色名里都可能出现该字符），而收益为零。

**字段分工（照抄既有分工）**：

| 字段 | 作用 | 参加鉴权/隔离？ | 既有对应 |
|---|---|---|---|
| `Scope.Session` | 隔离 + 回收粒度（会话销毁即杀） | ✅ | `JobSpec.SessionID` / `killSession` |
| `Scope.Subject` | teammate 分组维度（`emp_<role>`）+ 权限主体 | ✅（组内可见） | `emp_<role>` 主体（`WithEmployeeSubject`） |
| `Spec.Node` | 编排节点 / 团队阶段归属（打点、里程碑聚合） | ❌ | `BatchID` 的盖印章做法 |
| `Spec.Batch` | 派发它的那次 chat 请求（工作表格批次） | ❌ | `BatchID` / `AsyncBatchID` |

**两档语义（空 Subject = 整会话）**：

| 调用 | 语义 | 既有对应 |
|---|---|---|
| `Snapshot(Scope{Session})` | 该会话全部在册作业 | `observeSession(sessionID)` |
| `Snapshot(Scope{Session, Subject:"emp_exec"})` | 该会话内该 teammate 的作业（前端按员工分组 + 里程碑聚合） | 新增 |
| `Reclaim(Scope{Session})` | 会话级回收（会话删除/归档） | **逐位等于** `CloseSessionAsync(sessionID)` |
| `Reclaim(Scope{Session, Subject})` | 只回收该 teammate 名下作业（`team_retire` 步 1），不牵连同会话其他人 | 新增 |

**被否掉的备选（留档）**：O2 agent id（与既有会话级回收语义不一致，需 agent→session 映射）；
O4 plan/stage id（脱离会话生命周期；`bash_bg` 这种非 teamwork 场景没有 plan，还得养第二套键）。

---

## 11. 落地状态（2026-10-01）

> 口径同开头：**已实现** = 有代码 + 有用例；**已建面** = 契约 / 存储 / 端口就位，装配接线待做；
> **待做** = 未动。纪律不变：**先建新面、后撤旧面**。

| 里程碑 | 状态 | 落点 |
|---|---|---|
| M0 Seele `jobs` 根能力 | **已实现** | `Seele/jobs/{job,manager,executor,options,output}.go` + `jobs/builtin`（`jobs_manage`）+ `jobs/README.md` + `Seele/docs/arch/16-jobs-contracts.md`；不变式 I-1..I-7 逐条有用例（含去重、跨作用域拒绝、两档 Snapshot/Reclaim、硬上限 124 / 被杀 137 / panic 收尾、输出封顶不改判终态） |
| M0 Seelex `go.mod` replace | **已实现（临时）** | `replace github.com/RedHuang-0622/Seele => G:/Program/go/seele`，`go work vendor` 已重生成 vendor；Seele 打 tag 发布 `jobs` 后即删除 |
| M0 旧异步面迁到 `jobs.Manager` | **受阻（证据见 §12）** | `seelebridge/tools/{async_exec,async_run,async_probe,job_contract}.go` 仍是旧实现。**派发工具签名与语义未动**（`bash_bg`/`read_batch`/`job_manage` 行为零变化）；本轮只做了阻塞分析与文档，**未改任何生产代码**。§12 给出逐条实证：在「既有 `async_*_test.go` 一字不改」+「Seele `jobs` 契约冻结」两条硬约束下，忠实的门面化无法落地（能绕的几条，绕法就是把状态机在 Seelex 侧原样留一份，迁移变成名义上的） |
| M1 worker Executor | **已实现** | `seelebridge/teamwork/executor.go`：`KindWorker` / `KindSeat`；载荷 `WorkerRequest` 既是 `jobs.Spec.Payload` 又是执行体入参；生产实现 = `Runtime.RunWorker`（角色会话里跑一轮有界回合，起手绑工作区/带 `emp_<role>` 权责） |
| M1 worktree 绑定 / 释放 | **已实现** | `Runtime.ReleaseWorkspace` 接 `seelebridge/worktree`（脏工作区按 `ErrUncommittedChanges` 语义报错、不静默丢弃；`CleanupWorktree` 走 git；无现场幂等）；`Runtime.bindWorkerProjectRoot` 优先绑 worktree、缺失回退主工作区 |
| M1 人数上限 | **已实现** | `seelexctx.TeamLimits`（默认 6）+ `config/seelex.yaml` 的 `limits.team.max_teammates` + 两道拒绝（计划校验 + 派发闸门）；组合根经 `Runtime.SetTeamworkBackend` 注入 |
| M1 teammate 工具面移除 `fork_subagents` | **已实现** | `seelebridge/runtime_role_turn.go` 的 `teammateAgent`/`teammateToolFace`（硬移除：可见面剔除 + 派发口拒绝），`newRoleEngine` 装配点用 `teammateToolFace(r.agt)`；`runtime_role_face_test.go` 钉住（含"装配点确实用它"） |
| M2 `moduleTeamwork` 存储 | **已实现** | `sessionstore/module_heads.go`（枚举 + **独立锁** + `mutexFor` case）+ `sessionstore/teamwork.go` |
| M2 plan schema + 校验 | **已实现** | `sessionstore.ValidateTeamworkPlan`：一角色一 teammate、禁内置角色、人数上限、阶段 id 唯一、`depends_on` 无环（Kahn）、里程碑引用存在 |
| M2 `events.jsonl` 审计面（D12） | **已实现** | `AppendTeamworkEvent` / `ReadTeamworkEvents`（只追加、残尾容错）；`Coordinator` 写 `plan / dispatch / join / milestone / retire` 五类事实 |
| M2 leader 六件套工具 | **已实现** | `seelebridge/runtime_teamwork.go` 的 `team_plan/team_dispatch/team_join/team_milestone/team_retire` 经 `r.RegisterTool` 注册（`RegisterBuiltins` + `SetTeamworkBackend`），`jobs_manage` 由 `jobs/builtin` 提供；路由组表已分封（`team_*` → ctl、`jobs_manage` → rw）。**leader 提示词** = `plugins/default/teamwork/SKILL.md`（`$teamwork`）。组合根接线见 `main.go` 的 `Runtime.SetTeamworkBackend`；`sessionstore.Router.TeamworkFor` 提供持久面 |
| M2 goal 座位降级 `KindSeat` | **已实现** | **派发侧**端口 = `application/core/goal_coordinator.go` 的 `SeatJobs`（`DispatchSeat` / `JoinSeat`；终态读数 `dto.SeatJobOutcome`），经 `goalCoordinatorDeps.SeatJobs` 注入、由 `service_assembler.go` 的 `assembler.deps.Runtime.(SeatJobs)`（+ 装配探针 `SeatJobsAssembled`）探测；**执行侧**端口 = `seelebridge/teamwork.SeatRoundRunner`（`Runtime.SetSeatRoundRunner`，组合根 `main.go` 在 `initApplication` 之后传入 application 侧实现者 `Service.RunSeatRound`）。座位循环正文**唯一**（`goalCoordinator.runSeatRound`，作业执行体与同步降级路径共用）：`advanceAfterChat` 逃生记账之后——装配了作业面 → `DispatchSeat` → 有界（默认 5 分钟）`JoinSeat`，终态非 done ⇒ `Summary` 走与 `gov.Next` 同一条登记路径进 `RoundError`；未装配（端口 nil / 作业面未装配）⇒ 现状同步循环，行为一字不变。注册：`Runtime.SetTeamworkBackend` 的 `jobs.New` 补 `teamwork.SeatExecutor(r)`（作业 `Scope{Session}`、`Description` 为治理行标题）；会话归属与本轮正文走**载荷**（`SeatRequest.SessionID/Detail`），不依赖作业 ctx（由 `jobs.Manager` 从 `Background` 派生） |
| M3 `team_retire` 四步 | **已实现** | `Coordinator.Retire`：`Reclaim(Scope{Session,Subject})` → 释放 worktree → 清会话内容 → 保在线；端口缺失时**显式报错**；生产实现 = `Runtime.ReleaseWorkspace` + `Runtime.ResetSession`（角色会话为进程内执行面，清内存历史即"内容已清"） |
| M4 清场 | **待做（§9 已逐条核实）；已先清掉"内置形态目录"这一块** | §9 清单的**引用事实与退场条件**见 [`../devlog/2026-10-01-m4-deadcode-inventory.md`](../devlog/2026-10-01-m4-deadcode-inventory.md)：六条候选里只有 `TurnScheduler` 的 `Next/Request/Remove/Restore` 是 `test-only`（删它同时是改规格，要连带改 `scheduler_wiring_test.go` 与 README），其余五条都 `blocked`——`newGovernor`/`seatPlan`/`NewAdvisorSeat` 仍是座位循环正文（`runSeatRound`）的唯一座位派生来源、`teamRoleSeatsFor`/`RoleTurnPort` 仍有生产消费者、`order_policy/order_roles` 被席位环与前端 `team.set_order`（员工栏「摘除」时提交整张顺序表）共同消费。**已做的三步标注/清理**：① 2026-10-01 `order_policy` 在 `dto`（GoDoc）、`application/core/agentteam/README.md` 与 GUI 里标注为历史字段（字段本体、落盘取值与 `order_roles` 一字未动，退场条件仍 blocked），见 [`../devlog/2026-10-01-teamwork-legacy-order-fields.md`](../devlog/2026-10-01-teamwork-legacy-order-fields.md)；② 同日**删除内置形态目录**（`agentteam/presets.go` 与全部配套入口）——它不属于 §9 六条，但同属"旧面"，且 `goal` 上线自动装配团队会**整份替换掉会话已有的团队**（事故，不是自动化），见 [`../devlog/2026-10-01-no-builtin-team-shapes.md`](../devlog/2026-10-01-no-builtin-team-shapes.md)；③ 同日 **GUI 侧撤掉全部人工编排**（团队形态 chip / 顺序策略 / 拖拽调序 / 位置列一并退场，"次序 = 登记先后"落进面板与文档），见 [`../devlog/2026-10-01-team-panel-no-shapes.md`](../devlog/2026-10-01-team-panel-no-shapes.md)；④ 同日 **#1/#6 的员工执行面整条退场**（M2 的 `KindSeat` 已接管座位循环，员工干活改由 leader 派 worker 作业，退场条件「先建新面、后撤旧面」成立）：`RoleSeat` 只留 `RoleName`/`RoleKind`、`roleTurnSeat`/`newRoleTurnSeat`/`roleTurnNote`/`withRoleTurnInput`/`RoleTurnRunner`/`goalCoordinatorDeps.RoleTurnFor`/`application/core/role_turn.go` 与跨层的 `contract.RoleTurnPort`+`deps.RoleTurn`+`dto.RoleTurnRequest/RoleTurnOutcome`+`Runtime.RunRoleTurn` 一并删除（`runRoleRound` 保留——worker 作业与 ADVISOR 评审仍共用它），见 [`../devlog/2026-10-01-seat-employee-face-retired.md`](../devlog/2026-10-01-seat-employee-face-retired.md)；⑤ 同日 **#3 的 `TurnScheduler` 无消费者接口退场**：channel 投递（`Requests`/`Request`/`Next`）、顺序编辑三件（`Move`/`Remove`/`Restore`）、只读 getter `Prefix`，连同只服务它们的 `requests` 通道、`RuntimeOptions.Buffer`、`TurnRequest.RoundID` 与 `orderLocked`/`indexOfRole`；`scheduler_wiring_test.go` 的声明按事实改写并新增"退场必须被记下来"，见 [`../devlog/2026-10-01-turn-rotation-retired.md`](../devlog/2026-10-01-turn-rotation-retired.md)；⑥ 2026-10-03 **goal 席位轮转整条退场**（阶段三 W3）：goal 的驱动从"座位环"换成**提示词驱动的 leader 派活**，`application/core/govern`（整包）/ `goal/adapter.go` / 座位作业面 `jobs.KindSeat`（`SeatExecutor`/`SetSeatRoundRunner`/`RunSeat`…）/ headless `goal_gov_*` / 只读视图的 `Round`·`RoundLimit`·`CurrentSeat`·`Broken`·`BreakReason`·`RoundError` 一并删除；保留 `Controller` + 终态 gate + 逃生，见 [`../devlog/2026-10-03-seat-rotation-retired.md`](../devlog/2026-10-03-seat-rotation-retired.md) |

**结论**：作业面（Seele `jobs`）与 teamwork 的**编排面 / 存储面 / 生命周期 / 工具面接线**已落地并有回归；
`fork_subagents` 硬移除与 leader 提示词亦已就位；**goal 座位循环也已降级为 `jobs.KindSeat` 执行体**（M2 的最后一块：
座位循环正文唯一、作业路径与同步降级路径共用，装配/未装配两侧行为都有用例钉住）。
剩下两件：**旧异步面迁移到 `jobs.Manager`**（M0）——本轮做了阻塞分析，结论是**在这两条硬约束下不落地**（§12）；
与 **M4 清场**（已推进：员工执行面整条退场 + `TurnScheduler` 无消费者接口退场，见上表 ④⑤；余项仍按 §9 逐条核实）——都属于**替换旧面**的那一侧，
按本文纪律放在新面已就位之后。

## 12. M0「旧异步面迁到 `jobs.Manager`」阻塞分析（2026-10-01）

**结论**：本步**无法在「既有 `async_*_test.go` 一字不改」+「Seele `jobs` 契约冻结」两条硬约束下忠实落地**。
Seelex 侧本轮**未改任何生产代码**（对外行为逐字节不变，见 CHANGELOG）。下面每条都配可复现证据：一个只 `import jobs` 的探针程序（跑完即删，不入库）实测输出。

### 12.1 冲突点（逐条 + 实证）

| # | 冲突 | 实证（探针实测 / 源码锚点） |
|---|---|---|
| B1 | `jobs.Manager.Dispatch` 要求 `Description` 非空；既有用例直接调 `registry.begin(sessionID, command, "", "")`（**空描述**） | `Dispatch(Description:"") -> err=jobs: spec.description is required (ErrEmptyDesc=true)`（`manager.go` 的 `ErrEmptyDesc`） |
| B2 | `Dispatch` **立即起执行体**（`go m.execute`）；既有用例把 `begin` 当「**只登记、不执行**」，随后自己 `finish/attach/setCancel` | 探针：`Dispatch` 返回时执行体已在跑（`executor started? len(started)=1`，`manager.go` 的 `go m.execute`） |
| B3 | `Dispatch` 立即创建并**持有** `<handle>.log` 写句柄；Seelex 的 `close/removeDir` 语义要求「登记期间输出目录可被 `os.RemoveAll` 删掉」 | 探针：`RemoveAll while job registered -> unlinkat ... being used by another process`；独立复现：Go `os.OpenFile` 在 Windows 不带 `FILE_SHARE_DELETE`，`RemoveAll` 必失败（`output.go` 的 `newOutputWriter`） |
| B4 | `Fetch` 是「读增量 + **推进游标** + 终态即**自动 retire**」的**一体**动作；Seelex 工具面是**两段式**（`advanceTail` 读 → `markCursor` 提交），`retire` 只由 fetch-交付后 / `op=done` 显式触发 | 探针：`fetch -> state=done` 后 `observe -> present=false`（已被 manager 销项），第二次 `fetch -> ErrRetired`。工具面的 `snapshot→advanceTail→markCursor→snapshot→render→retire` 会在第二次 `snapshot` 处报「已不在登记表里」（`job_contract.go` 的 `jobManager.Fetch`） |
| B5 | `Manager` **没有**外部「合成终态」入口（终态只由 `Executor` 拿到的 `Sink` 决定）；Seelex 的 `registry.finish(handle, exit)` 是执行体**之外**的公共方法（用例直接调） | `Manager` 接口面只有 `Dispatch/Observe/Fetch/Kill/Done/Snapshot/Reclaim/Events/ScopeOf/Close`，无 `Complete/Declare`（`manager.go`） |
| B6 | `Manager` **没有** `RetiredState(handle) (State, bool)`：销项后只剩 `ErrRetired`，读不到终态**字面量**；Seelex 的 `retiredState` 要返回字面量给重复 `done/fetch` 的幂等回执 | 探针：`second fetch -> err=jobs: job already retired: a1`；`renderRetired/renderObserved`（`async_exec.go`）需要 `state` |
| B7 | `Manager` **不删**每个作业的输出文件（`retireLocked` 只关句柄，`Close`/`prune` 也不删）；Seelex 要求驱逐/销项**连带删文件** | 读 `manager.go` 的 `retireLocked/Close/prune`（无 `os.Remove`）；用例 `TestAsyncRegistryEvictionDropsRecordAndLog` 断言文件消失 |
| B8 | 执行体要知道**自己的句柄**才能回填 Seelex 侧表（进程树、取消口、命令原文、Index、Notified）；`Executor.Start(ctx, spec, sink)` 的 `spec` **不带 handle** | `job.go` 的 `Spec`（无 `Handle`）；`Handle` 由 `Dispatch` 才产生 |

### 12.2 为什么「打补丁」不成立

上述每一条都能用 Seelex 侧旁路硬绕：空描述填空串（B1）、执行体写成「无 router 时阻塞等 body」（B2/B5）、用 `Snapshot` 差分反推被驱逐句柄再删文件（B7）、给 `Payload` 塞自造关联 id 找回句柄（B8）。但绕出来的结果**恰好是把状态机在 Seelex 侧原样留了一份**：

- **游标必须留**（B4：两段式「渲染-提交」语义 manager 没有）⇒ manager 的 `Cursor/Truncated` 成为死字段；
- **墓碑字面量必须留**（B6）⇒ 与 manager 的 `retired` 表双份；
- **记录/销项策略必须留**（B7：差分反推）⇒ 与 manager 的 `prune` 双份；
- **终态合成入口必须留**（B5/B8）⇒ Seelex 仍拥有「什么算终态」。

也就是说，得到的不是「薄门面」，而是「**两份状态机 + 一堆对齐 hack**」，还新引入 goroutine 泄漏（无 router 的阻塞执行体）与漂移面。这与 M0 的目标（把记录/状态机**搬进** `jobs.Manager`）相悖，故**不落地**，等 Seele 契约补齐（§12.3）后再做一次干净的门面化。

### 12.3 解除阻塞所需的最小 Seele 契约增补（建议，留待 Seele 侧评审）

1. `Manager.Declare(ctx, spec) (Handle, error)`（或 `Spec.Deferred`）：**只登记、不起执行体** —— 对齐 `beginJob` 的既有语义。
2. **外部终态入口**：导出按句柄的 `Sink`（`Manager.SinkOf(handle) (Sink, bool)`），或 `Manager.Complete(ctx, handle, state, exit, summary)` —— 对齐 `registry.finish`。
3. `Manager.Peek(handle, budget)`：**只读增量**（不推进游标、不销项），与 `Fetch`（推进 + 销项）并列 —— 对齐两段式工具面。
4. `Manager.RetiredState(handle) (State, bool)`：销项墓碑的**字面量**读面（幂等回执）。
5. **输出文件归属与删除**：`Spec.OutputPath` + manager 不接管文件句柄（或 manager 在 `retire` 时删文件）—— 同时解掉 B3 的 Windows `RemoveAll` 约束与 B7。
6. `Limits.HardCap = 0`（不交 manager 合成终态）**已在 M0 具备**；Seelex 执行体自带 30 分钟硬上限与 `exit=124/137` 注记，保持不动。

> 这 6 条一旦就位，Seelex 侧只需把 `asyncRegistry` 的 `runs` 侧表缩到「进程树 / 取消口 / 命令原文 / Index / Notified」，其余读面直连 manager，即可实现**逐字节不变**的门面化。


### 12.4 后续裁决（2026-10-01）：此路径**不再作为候选**

用户裁决：把异步面 / 作业事件流**迁到 Seele** 这条路**已经走过并认定失败**，原因是 `jobs` 侧的
sink 形状做不到「`job_manage` 之后保持稳定前缀、再把数据追加到尾部」：

- `event.Sink`（`WithEventSink`）必须在 `jobs.New` **构造期**定死，那时还不知道"这条作业属于哪个
  会话的哪条事件流"；框架 `event.Recorder` 是**单例 + 序号全局**，而 Seelex 的会话事件库是
  **按会话追加/排序**的——这样发出的事件**append 不到会话事件流的尾部**，只能事后回填归属，且全局
  序号在按会话排序下不成立（§3 的那条结构性理由）。
- 结论：该 hook 已从 `jobs` 撤出（`Sink` 契约回到 `Note` / `SignalBytes` / `Exit` / `Complete`），
  事件流落在 Seelex 侧（`seelebridge/jobs_events.go` 订阅 `Events()` + `Snapshot` 自投影追加），
  与 `docs/arch/context-prefix-chain.md` 的"已定稿轮次 append-only、旧轮字节不变"同一口径。

因此：**本步不再开工，也不必等 §12.3 的契约增补**；§12.3 降级为**留档**（记录"当年为什么绕不过去"），
不构成待办。


## 附：锚点索引

**Seelex（本轮静读）**
- 异步作业面（范本）：`seelebridge/tools/{job_contract,job_tools,job_run,job_subagent,async_exec,async_run,async_probe}.go`、`seelebridge/tools/README.md`、`seelebridge/runtime_async.go`、`seelebridge/runtime_tools.go`
  - 契约与类别：`job_contract.go:JobSpec/JobHandle/JobTool/jobManager`（三类共表共状态机）
  - 隔离：`async_run.go:CloseSessionAsync`；`job_contract.go` 的 `run.sessionID != sessionID` 判据
  - 归属：`async_probe.go:JobInfo{SessionID,BatchID}`、`router.go:AsyncBatchID`、`runtime_tools.go:AsyncBatchID`、`task/task.go:defaultBatchID`
- 子代理：`seelebridge/fork/{tool,types}.go`；worktree（git）：`seelebridge/worktree/worktree_manager.go`（`GitRunner`/`NodeWorktree`/`approve.ApprovalGate`）
- 进程树：`seelebridge/security/process_tree_{windows,other}.go`
- 团队（角色唯一/会话派生）：`application/core/agentteam/{spec,registry}.go`（`Normalize` duplicate 校验、`RoleSessionID`、`needsRoleSession`）
- 治理（降级目标）：`application/core/govern/`、`application/core/goal_coordinator.go`
- 配置：`seelexctx/limits.go`（`Limits`/`AsyncExec` → 新增 `TeamLimits`）
- 存储：`sessionstore/module_heads.go`（`storageModule`/`mutexFor`/`moduleForStackKind`）
- 权限：`docs/arch/agent-permission-subjects.md`、`seelebridge/runtime_role_turn.go`

**Seele（`G:\program\go\seele`）**
- `README.md`、`ARCHITECTURE.md`、`docs/arch/{09,10,12,14}-*.md`
- `tools/tools.go`（`ToolMeta`/`Bits`）、`tools/permission/types.go`、`tools/gateway/`
- `session/README.md`（回合闸门 + `Reset`）、`workplan/README.md`、`event/README.md`

**对照**
- [`agent-team-phase2-and-goal-vs-vmodel.md`](agent-team-phase2-and-goal-vs-vmodel.md)、[`agent-team-seat-vs-claim.md`](agent-team-seat-vs-claim.md)、[`a2a-agent-team-factory.md`](a2a-agent-team-factory.md)
- [`../research/2026-09-30-goal-a2a-flow-and-techleader.md`](../research/2026-09-30-goal-a2a-flow-and-techleader.md)、[`../research/2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md`](../research/2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md)
- `docs/2026-09-24-async-tool-deferred-ack/README.md`
