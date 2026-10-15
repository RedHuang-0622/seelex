# 项目看板（Project Board）：把「这个项目在忙什么」写成数据根里的派生投影，钩子被动落盘、按需读取

> 状态：**已实现**（M0–M7 已落地，见 [`docs/arch/seelex-project-board.md`](../../arch/seelex-project-board.md)）。本文保留为**当时的设计方案与评审输入**；其"尚未实现"的原始状态声明已过期。
> 后续偏离（别再照本文找落点）：落点先搬进**项目根**（M7），再改成**按会话分目录**——`<项目根>/.seelex/board/<会话 ID>/{worktable,teamwork}.jsonl`，人读件 `BOARD.md` 与 `project-known.md` 留在看板根（M9，见 arch 版 §3 / §9.1 D-7）。本文 §4.3、§5.4、§7.1、§7.2 里的 `<baseDir>/<boardRoot>/project-<hash>/session-<hash>/` 是**当时**的设想。
> 日期：2026-10-14（第二版：按用户口径重建，非打补丁式修订）
> 证据分级：**Confirmed** = 本轮逐点自读代码/磁盘实测（带 `文件:行`，可复现）；
> **Hypothesis** = 推断，附「怎么坐实」。
> 上游：`docs/2026-10-13-seelex-dot-a2a-star-topology/README.md`（`.seelex` 的产品语义头脑风暴）；
> 评审记录：`docs/2026-10-13-seelex-project-board/plan-review.md`。

**一句话**：在数据根里为每个会话维护两块**派生投影**——`worktable`（这个会话在忙哪些任务）与
`teamwork`（里程碑走到哪一步了）——由**既有的工具完成钩子**在「模型列出/更新 todo」时**被动落盘**；
模型**不在每轮上下文里看到它**，只有**新会话按提示词要求时**才去读。
另有一份项目级的 `project-known.md`（项目摘要），它是唯一需要花 token 的部分，
因此做成**注册能力 + 被动初始化/更新 + 责任链**，只有用户明确要求时才做。

---

## 1. 简要描述

### 1.1 这件事在解决什么

用户在项目里干活时，`.seelex` 里应该能看出**这个项目在忙哪些任务、里程碑走到哪了**。
今天看不到：任务与里程碑的权威状态只活在**会话内存**与**会话级落盘**里
（`task.TaskRegistry` actor、`SessionRecord.Tasks`、`moduleTeamwork` head），
磁盘上没有一个「别的会话 / 别的进程 / 人」能直接读到的项目状态。

本方案补的就是这一层：**把已有的活体投影，按会话落成可读文件**。

### 1.2 三层（谁是谁的输入）

```text
┌─ 事实层（唯一权威，已有，本方案一行不改）──────────────────────────────┐
│ task.TaskRegistry（kind=todo/task 的行，actor）                        │
│ SessionRecord.Tasks（会话落盘）                                        │
│ moduleTeamwork head = TeamworkPlan（里程碑/工作项）                     │
│ teamwork/events.jsonl（追加审计）                                      │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │ ① 钩子（确定性代码，0 token）
                           │    tool_complete ∈ {todo_*, task_add, task_complete}
                           ▼
┌─ 投影层（新增；派生、可重建、**不是事实源**）───────────────────────────┐
│ .seelex/session-<id>/worktable      ← 任务行投影                        │
│ .seelex/session-<id>/teamwork       ← 里程碑 + 工作项投影               │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │ ② **按需**读（不是每轮注入）
                           │    触发 = 提示词要求（"承接会话 X"/"项目什么样"）
                           ▼
┌─ 读取层（新增；被动、有界、0 常驻字节）────────────────────────────────┐
│ 新会话靠 `## Available Skills` 里的被动技能条目知道"去哪读、怎么读"      │
│ 读面：项目→会话枚举 + 专用读工具（见 §3.7）                             │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │ ③ 唯一有 token 成本的一步
                           ▼
┌─ 摘要层（.seelex/project-known.md）───────────────────────────────────┐
│ 已注册能力 + 被动初始化/更新（只有提示词要求才做）+ 责任链             │
│ 内容 = ProjectRecord 的**人读渲染**（不是新的输入源）                   │
└────────────────────────────────────────────────────────────────────────┘
```

### 1.3 成本模型（本方案的立论基础）

| 产物 | 谁写 | token 成本 | 何时进上下文 |
|---|---|---|---|
| `worktable` / `teamwork` | 钩子（确定性代码） | **0**（不产生 LLM 生成） | **只在被读时**，由提示词触发 |
| `project-known.md` | 已注册能力（要 LLM 归纳） | **有** | 只在被读时 |
| 被动技能目录条目 | 插件装配（`RenderSkillCatalog`） | 一次性、稳定、**可缓存**（几十 token 量级：仅 name + description） | 常驻前缀，但**插件不变则字节不变** |

**两条推论，直接决定设计**：

1. **最便宜的东西不该每轮付费**：`worktable`/`teamwork` 是免费的确定性投影，
   所以**不进任何常驻上下文段**（既不进 system/project 前缀，也不进请求尾部）。
   每轮请求字节不变 ⇒ 前缀缓存命中不受影响。
2. **最贵的东西必须有明确入口**：`project-known.md` 要花 token，所以它不能"自动刷新"，
   必须是一个**要人喊才做**的注册能力。

### 1.4 本版相对上一版的实质变化（三处，不是措辞）

| 变化 | 上一版 | 本版 |
|---|---|---|
| **产物的地位** | 尾部注入段 + `I7`「故意不继承」⇒ 事实上成了"上下文供给源" | 纯派生投影；**不进上下文**；`I7` 改为**继承** |
| **读取方式** | 每轮请求尾部自动注入 | **按需读**：提示词要求时才读；新会话靠被动技能目录知道去哪读 |
| **成本归属** | 摊在"每轮注入" | 0 成本（投影）/ 有成本（摘要），两者拆开，各给机制 |

---

## 2. 需求拆解

### 2.1 用户原话 → 可验收项

| # | 用户原话 | 拆成的可验收项 |
|---|---|---|
| **R1** | 「项目下的 `.seelex` 有描述这个项目主要忙些什么任务」 | 项目级（跨会话）能读到**任务清单及其状态**，而不是只看"当前会话的 todo" |
| **R2** | 「这些任务的里程碑是什么，忙到什么阶段了」 | 里程碑与工作项及其状态（pending/active/done 等）可被**项目级**读出 |
| **R3** | 「这个是 teamwork 和列出 todo 的**被动技能**，放一个**钩子**，当列出 todo 的时候触发」 | ① 写侧：模型调用 todo 族工具 → **被动落下投影文件**（模型无需额外调工具）；② 读侧：**新会话按提示词要求**去读（例："我要承接会话 X 的 worktable" / "我们项目现在是什么样"） |
| **R4** | 「和 todo 一样，支持按照**会话**做出区分」 | 每个会话各一份/各一组行，互不覆盖；同时项目级能聚合 |
| **R5** | 「这个钩子作为可以在设置中调整的部分」 | 一个配置开关 + 触发面可选（对齐 `limits.async_exec` 的「整块收回」口径），**触发面默认 `todo_list`** |
| **R6** | （本轮新增）项目摘要 MD 需要**注册能力**做初始化/更新，**被动**（只有提示词要求才做），走**责任链** | 存在一个注册能力可初始化/更新 `project-known.md`；**无显式调用不产文件**；组织方式是责任链 |
| **R7** | （本轮新增）`.seelex` **可提交/不可提交由用户决定** | 实现侧不把「是否进版本库」当设计前提；默认本机派生缓存（不提交），要不要加 `.gitignore` 否定规则由用户定 |

### 2.2 需求的技术边界（把 R 翻译成"不变量"）

| 需求 | 技术边界 |
|---|---|
| R1/R2 | 必须有**项目级读面**；数据来自**既有投影**，不新增判定逻辑 |
| R3① | 触发点是**工具完成钩子**（唯一能看到"模型列了 todo"的地方）；写入是**代码**，不消耗模型 token |
| R3② | 读取是**按需**的：常驻上下文**零新增**；"知道去哪读"靠**被动技能目录**（已有通道） |
| R4 | 存储按**会话**分片；聚合读**只读**、有界 |
| R5 | 开关在**装配处**；关就是关（不静默降级） |
| R6 | 摘要能力**被动**：不自动刷新；且必须**复用**既有项目知识链（否则是第二份实现） |
| R7 | 产物默认**不进版本库**；提交与否是用户决定，实现不依赖 |

### 2.3 非目标（本方案明确不做）

- 不新增事实源：**任何 gate / 裁决 / 领域状态机都不得读投影**（对齐 `sessionstore/board.go:1-20` 的口径）。
- **不做每轮上下文注入**（不新增常驻前缀段、不新增请求尾部件）。
- 不重写 todo/teamwork 的状态机：投影只做**字段映射**。
- 不在项目工作树里造第二套文件写实现（除非 §4.10 Q1 拍板为"导出"形态，且导出走显式路径）。
- 不把 `scheduled-tasks.jsonl`（任务定义，全局）与 `worktable`（任务状态投影，按会话）混为一谈。

---

## 3. 代码现状

> 本章只写**本轮作者亲自读过并实测**的事实。每行末尾给证据；来自上一版 plan 未经本文复核的部分一律标注来源。

### 3.1 数据根：`-store` 派生，且它与「项目根」不是一回事

| 事实 | 证据 |
|---|---|
| `-store` 默认 `.seelex/sessions` | `main.go:58` |
| `baseDir = filepath.Dir(*storePath)`；`session-storage.json` 落在 baseDir | `main.go:1589`（`initStore`） |
| 数据根锁用同一个 `dataRoot := filepath.Dir(*storePath)` | `main.go:1575` |
| baseDir 同级已有三份先例：`session-storage.json`、`workspace_index.json`、`scheduled-tasks.jsonl` | `main.go:1589`、`:973`（`filepath.Join(filepath.Dir(*storePath), "scheduled-tasks.jsonl")`） |
| json 后端根 = `<defaultPath>/sessions-json` | `sessionstore/sessionstore.go:81`（`Config.Normalize`） |
| 项目目录 = `<root>/project-<hash(projectID)>`；会话目录 = 其下 `session-<hash(sessionID)>`；`hash` 不可逆 | `sessionstore/sessionstore.go:1740-1745`（`projectDir` / `sessionDir`） |
| 项目级文件目前只有一个：`session-meta.json`（置顶/别名/排序位） | `sessionstore/json_layout.go:248-256` |
| 实测本仓库开发态：`.seelex/` = `baselines/ perf/ sessions/ sessions-json/ stage-limits/ workspace_index.json`；`sessions-json/` 只有 `lock.owner`；`sessions/` 只有 `.lock` | 磁盘实测 |

**⚠️ `.seelex` 有三重身份，必须在设计阶段就分清**（这是上一版最容易误读的一点）：

| 身份 | 是什么 |
|---|---|
| (a) 运行时**数据根** | `dirname(-store)`；**CWD 相对**；**跨项目共用**（项目按 `project-<hash>` 分片） |
| (b) 用户口中的"**项目的** `.seelex`" | 在项目根里、可提交/可不提交 |
| (c) 本仓库里它**恰好就是**仓库根的 `.seelex/` | 因为默认从仓库根启动 ⇒ (a) 与 (b) 在开发态**重合** |

**处理**：落点一律写成「**按数据根派生**」，不硬编码 `.seelex/`；
(a) 与 (b) 的解耦见 §4.3 与 §4.10 Q1。

### 3.2 todo 已经是「工作表格里的一行」，不是独立清单

| 事实 | 证据 |
|---|---|
| 四个 handler：`todo_init/todo_add/todo_done/todo_status` 注册委托 `task.Tools.RegisterTodoTools()` | `seelebridge/runtime_tools.go:200-203`、`:251`（[上一版已核]） |
| todo 融合为 `kind=todo` 的 task，权威源是 `task.TaskRegistry` actor | `seelebridge/task/README.md`（[上一版已核]） |
| **工作表格行的 `Kind` 是字符串**（取值 `plan/todo/task/subagent/tasklist`），来自 `record.Kind` | `application/core/work_table.go:30` 起（`taskRecordToWorkItem`） |

**推论**：任何"再写一份 todo 清单当事实源"的做法都与 `AGENTS.md §8` 冲突。新文件只能是**投影**。

### 3.3 工作表格：**已有投影**，且**已有被动同步点**

| 事实 | 证据 |
|---|---|
| `buildWorkTable(plan, tasks, subagentTree, asyncRuns) []WorkItem` —— 四路输入归一为扁平行，有界（`Limits().WorkTableRows`） | `application/core/work_table.go:30` |
| 请求尾部打点块 `workTableTraceBlockFor` 已接线为 `context_runtime.Deps.WorkTableTraceBlock` | `application/core/service_assembler.go:192-193` |
| 工具完成后**已经**在调用被动重投影 `service.refreshWorkTableFromSources()` | `application/core/tool_hooks.go` 完成段末（`handleToolCompleteObserved` → … → `refreshWorkTableFromSources()`） |
| 钩子不是"可增挂的钩子表"，而是 `Hooks()` 返回的**固定闭包**（`OnLLMComplete`/`OnToolStart`/`OnToolComplete`/`OnIterationComplete`） | `application/core/tool_hooks.go:310` 起；bridge 只有 `Bind` / `SetDiagnosticObserver` |

**⚠️ 结论**：本方案**不得**新造一个"工具完成回调"；
写侧必须**挂在既有 `refreshWorkTableFromSources` 之后**，让"内存投影"与"文件投影"一次做完，
避免两条时间线漂移。

### 3.4 teamwork：会话粒度已齐，**项目粒度不存在**

| 事实 | 证据 |
|---|---|
| 计划落点：`moduleTeamwork` head（`metadata/teamwork.json`，整份替换、原子发布 + 校验和自愈） | `sessionstore/module_heads.go:57-61`（`moduleTeamwork storageModule = "teamwork"`）、`:250`（`mutexFor` case） |
| `TeamworkPlan{TeamID, Version, Members, Milestones, State}`；**没有 `stages`**（阶段口径已整条退场） | `sessionstore/teamwork.go:98-105` |
| `TeamworkMilestone{ID, Name, DependsOn, Required, Status, Content, Items}`；`DependsOn` = 里程碑之间的**屏障** | `sessionstore/teamwork.go:132-150` |
| `TeamworkWorkItem{ID, Milestone, Role, Name, Description, Goal, DependsOn, Status, SessionID, Worktree, Handle, Note, StartedAt, FinishedAt}`；`DependsOn` = 里程碑内 **DAG** | `sessionstore/teamwork_items.go:46-90` |
| **"忙到什么阶段"的权威读数是运行态** `TeamworkState{Jobs, Milestones map[string]string, Unmerged, State, ClosedAt, ClosedReason}` | `sessionstore/teamwork.go:151-172` |
| ⚠️ 收口事实在 `TeamworkState.State`（字符串，空 = 未收口），**不是** `TeamworkPlan.State` —— 后者是 `TeamworkState` **结构体本身** | `sessionstore/teamwork.go:98-105` 与 `:151-172` 对照 |
| 里程碑状态常量：`pending`（空）/`active`/`done`；工作项：`pending/review/done/failed` | `sessionstore/teamwork.go` 常量区；`sessionstore/teamwork_items.go:40-43` |
| 审计行 `TeamworkEvent{At, Kind, TeamID, Role, Handle, Node, Milestone, WorkItem, RoleSessionID, Detail}` | `sessionstore/teamwork.go:173-194` |
| 看板存档（另一件事）：`metadata/board_goal.json` / `metadata/board_team.json`，`BoardLifecycle{Kind, State, Seq, …}`；**下游存档、活体优先、不进领域判定** | `sessionstore/board.go:1-70` |

**⚠️ 关键结论**：teamwork 的**计划与运行态都是会话级**的（`metadata/teamwork.json` 在会话目录下）。
"项目级里程碑视图"不存在，需要**跨会话聚合读**（§4.6）。

### 3.5 「被动技能目录」是模型"零调用可见"的既有通道

| 事实 | 证据 |
|---|---|
| `RenderSkillCatalog(skills, sigil)` 只渲染 **name + description**，按 name 排序、同输入恒同输出；**Prompt 正文永不进入本段** | `application/core/prompt_layer/skill_catalog.go` |
| 装配位置：system = base（identity→plugin→effort→instructions）+ **可用技能目录**；两者都只随**插件切换**变化 | `application/core/prompt_layer/coordinator.go:126-160`（`SystemPromptForActiveTaskLockedFor` / `skillCatalogPart`） |
| 该段的注释明确写着"前缀缓存纪律"：动态段（激活技能正文、plan 执行指令）已被移出 system，走 transcript append / 请求尾部 | 同上注释 |

**⚠️ 结论**：新会话"知道 `.seelex` 里有投影、该去读它"，**靠的就是这条目录条目**——
它是**几十 token 量级、字节稳定、可缓存**的，不是每轮新增的动态段。
这正是用户说的"被动技能"；它与"每轮注入内容"是**两件事**。

### 3.6 项目知识：`ProjectRecord` 已存在，但有两处断点（**开工前必须先修**）

| 事实 | 证据 |
|---|---|
| `ProjectRecord{Version, Modules[], SourceHashes[], BuiltAt}`；`ModuleSemantics{Name, Summary, Path, Docs}` | `sessionstore/project_record.go` 结构体首段 |
| 唯一写者 = `project_refresh` 工具；来源 = `docs/modules` + `gui/module_dotting.json` + `seelex.project.md`（可插拔） | `main.go:784-801`；`sessionstore/project_record.go`（`NewProjectKnowledgeBuilder` / `ProjectKnowledgeSources`） |
| 注入链：`Runtime.ProjectKnowledge` → `projectBlock()` → `AssemblerOptions.ProjectBlock` | `main.go:680-681` |
| **断点①：三个默认来源实测都不存在**（`docs/modules`、`gui/module_dotting.json`、`seelex.project.md`）；**真实元数据在 `docs/gui/module_dotting.json`**（实测存在） | 实测 glob |
| **断点②：写键 ≠ 读键** —— `RefreshProjectKnowledge` 用 `builder.Root()`（**绝对路径**）读写，而正常读面 `main.go:681` 用 `store.Workspace()`（`ws-<nano>`） | `sessionstore/project_record.go`（`RefreshProjectKnowledge` 里的 `store.LoadProjectRecord(builder.Root())` / `store.SaveProjectRecord(builder.Root(), record)`）vs `main.go:681` |
| 断点②的后果：实测全仓 `**/project-record.json` **零命中** | 实测 glob |
| 工作区 ID 是**机器本地**的：`ws-<unixNano>` | `workspace/workspace.go:165` |

### 3.7 读取面的「工程边界」——这条直接决定落点

模型侧的读工具都声明为 **inside the bound project**：

| 工具 | 合约 |
|---|---|
| `read_file` / `grep_search` / `glob` | "Read/Search/Find … **inside the bound project**" |
| `show_files` | 只收**工作区相对路径**；绝对路径/越界会被跳过并回报原因 |

**⚠️ 后果**：若投影落在**数据根**而数据根**不在绑定工程内**，模型**根本读不到**它 ——
"按需读"这条设计会直接失效。
本仓库开发态之所以"看起来没问题"，是因为数据根恰好 = `<仓库根>/.seelex`（§3.1(c)），属**巧合**。

**处理**：见 §4.3 与 §4.10 Q1 —— 要么落点在工程内，要么补一个**专用读工具**。

### 3.8 配置先例与提交边界

| 事实 | 证据 |
|---|---|
| `limits.async_exec.enabled`：常驻开 = 入口在工具面；`false` = **整块收回**（不静默降级） | `config/seelex.yaml:223-236` |
| `limits.async_exec.trigger_conversation`：终态触发一个回合，**只在会话空闲时** | `config/seelex.yaml:227-232` |
| `limits.runtime.allow_multi_process`：改变**写者假设**的开关必须写明代价（进程内锁不跨进程） | `config/seelex.yaml:236-248` |
| `.gitignore` **整体忽略** `.seelex/`（第 14 行是 `.seele/`） | `.gitignore:15` |
| 数据根里有 `lock.owner`（PID/程序名/主机名） | 实测 `.seelex/sessions-json/lock.owner` |

**⚠️ 后果**：可提交面**不可能是"整体放开"**——数据根里有会话正文、大输出、媒体、`lock.owner`。
"可提交"只能是**显式挑选的少数派生件**。

### 3.9 缺口清单

| # | 缺口 | 后果 | 对应需求 |
|---|---|---|---|
| **G1** | 无**项目级（跨会话）的「计划/待办」投影与读面。（项目级的**记录/枚举**面已有三份：`ProjectRecord` 模块语义 / `SessionMeta` 展示元数据 / `ListWorkspace`+`SessionsOf` 会话枚举，但没有一份回答"这个项目有哪些未完成任务、里程碑到哪了"） | R1/R2 无法满足 | R1/R2 |
| **G2** | 无「列 todo → 落投影文件」的写侧链路：`refreshWorkTableFromSources` 只重投影到内存与事件 | R3① 无法满足 | R3① |
| **G3** | 无「按需读取面」：既没有"去哪读"的被动技能条目，也没有能读到数据根投影的入口 | R3② 无法满足 | R3② |
| **G4** | 无能力开关 | R5 无法满足 | R5 |
| **G5** | `ProjectRecord` 的**两处断点**（来源全落空 + 写键≠读键） | "项目摘要 MD"永不出现 | R6 |
| **G6** | `.gitignore` 整体忽略 `.seelex/`，且数据根含不可提交内容 | R7 需要显式挑选可提交件 | R7 |
| **G7** | 读取面受"绑定工程"约束（§3.7） | 落点与读面必须成对设计 | R3② |
| **G8** | 投影的生命周期未绑定：删会话、子代理/角色会话的小文件堆积 | 孤儿文件与噪声 | R4 |

### 3.10 本章证据索引

| 结论 | 证据 |
|---|---|
| 数据根系于 `-store` | `main.go:58`、`:1575`、`:1589`、`:973` |
| json 根 / 目录派生 / `hash` 不可逆 | `sessionstore/sessionstore.go:81`、`:1740-1745` |
| 项目级展示元数据落点 | `sessionstore/json_layout.go:248-256` |
| 工作表格投影与按会话读面 | `sessionstore/session_granular.go:676`（`SessionsOf`）、`sessionstore/sessionstore.go:945`（`ListWorkspace`）、`:356`（`Workspace`） |
| 工作表格行与 kind 取值 | `application/core/work_table.go:30` 起 |
| 被动同步点已经在钩子完成段 | `application/core/tool_hooks.go`（`Hooks()` `:310` 起；完成段调 `refreshWorkTableFromSources()`） |
| 尾部注入位存在（本方案**不用**） | `application/core/service_assembler.go:192-193` |
| 被动技能目录 | `application/core/prompt_layer/skill_catalog.go`、`coordinator.go:126-160` |
| teamwork 类型与模块 | `sessionstore/teamwork.go:98-194`、`sessionstore/teamwork_items.go:40-90`、`sessionstore/module_heads.go:57-61`/`:250` |
| 看板存档口径 | `sessionstore/board.go:1-70` |
| 项目知识两处断点 | `sessionstore/project_record.go`、`main.go:680-681`、`main.go:784-801` + 实测 glob |
| 工作区 ID 机器本地 | `workspace/workspace.go:165` |
| 配置先例 | `config/seelex.yaml:223-248` |
| 提交边界 | `.gitignore:15`；实测 `lock.owner` |
| 单一实现 / 文档放置 / ProjectScope / 红灯先行 / 删除清单 | `AGENTS.md §8`、`§2`、`§6`、`§8.7`、`§8.6` |

**未做（后续补证）**：
1. §3.7 的"绑定工程"究竟是**每会话的 workspace** 还是**进程 CWD** —— **[Hypothesis]**，只按工具自身合约文本判断；坐实方法：绑一个工程外目录跑一次读。
2. `-store` 指向其它盘符/UNC 时 `projectDir` 的派生行为（未实测）。
3. `seelebridge/runtime_teamwork.go` 的 12 个工具 handler 与 `seelebridge/teamwork/*` 的逐行核对（本方案只读其投影，不改其域）。

---

## 4. 目标模型

### 4.1 结构（§1.2 那张图的展开：四层职责）

| 层 | 新增/已有 | 职责 | 明确不做 |
|---|---|---|---|
| 事实层 | 已有 | todo/task 的状态与依赖、里程碑与工作项 | —— |
| 投影层 | **新增** | 把事实层的**既有投影输出**映射成行，按会话追加落盘 | 不判定、不重算状态、不做 gate |
| 读取层 | **新增** | 让模型/人**按需**读到投影；知道"去哪读" | **不进常驻上下文**、不自动刷新 |
| 摘要层 | **新增（复用既有 `ProjectRecord`）** | 项目摘要 MD 的初始化/更新 | 不新增项目知识来源 |

### 4.2 铁律（写成可测的不变式）

| # | 不变式 | 判据 |
|---|---|---|
| **I1 派生而非事实** | 删掉投影文件，系统功能不变，且能被重建 | 「重建成功」用例：删文件 → 触发同步 → 折叠读结果与活体投影逐行相等 |
| **I2 活体优先** | 本会话读**活体投影**；文件只在活体给不出时兜底（跨会话/重启） | 与 `board.go:1-20` 同口径；**反例用例**：篡改文件不得影响活体与领域判定 |
| **I3 不新增第二份判据** | 状态、分类、里程碑语义只由既有处产生 | `buildWorkTable` / `TeamworkPlan` 是唯一判据处；新代码只做字段映射（`AGENTS.md §8.3`） |
| **I4 单写者 + 原子追加** | 一个会话文件只有一个写者；行级追加不重写整份；崩溃残尾视为未提交 | 复用 `appendEventLogLine` + `truncateCrashTail`（`sessionstore/jsonl_io.go`） |
| **I5 有界** | 读取行数、跨会话会话数、单行字节都有上限；截断显式标注 | 对齐尾部块既有口径 |
| **I6 空则退场** | 没有任务/里程碑 → 不建文件、不渲染、不留空壳 | 对齐看板退场语义（`sessionstore/board.go`） |
| **I7 不进模型上下文** | 投影**不得**进入 system/project 前缀，也**不得**进入请求尾部；只有"被读"时才进（由提示词触发） | 回归用例：能力开/关，装配前缀字节**完全一致**；请求尾部**不含**项目看板段 |

### 4.3 落点与命名

**物理落点按数据根派生**（`baseDir = filepath.Dir(*storePath)`），逻辑名按用户口径：

| 用户口径 | 逻辑键 | 物理路径 | 说明 |
|---|---|---|---|
| `.seelex/session-xxx/worktable` | 会话 id | `<baseDir>/<boardRoot>/session-<sessionKey>/worktable.jsonl` | 任务行投影（追加式） |
| `.seelex/session-xxx/teamwork` | 会话 id | 同目录 `teamwork.jsonl` | 里程碑 + 工作项 |
| `.seelex/project-known.md` | 项目 | `<baseDir>/<boardRoot>/project-known.md` | **渲染产物**，非输入（§4.7） |

两个待定结构（§4.10 Q1/Q2）：

- `<boardRoot>`：数据根内的独立子目录（**不与** `project-<hash>/session-<hash>` 混层，避免两套读法）；
- `<sessionKey>`：**建议用既有 `hash(sessionID)`**，字面会话 id 写在行内的 `scope.session`。
  理由：既有布局刻意 hash（不泄漏 id、避开文件名字符集与长度问题），且 `hash` 不可逆 ⇒
  若改成字面目录名，必须另配一份 `id ↔ dir` 清单才能枚举，等于多一套读法（§4.10 Q2）。

**为什么落点留在数据根**（三案对比）：

| 方案 | 优点 | 代价 | 结论 |
|---|---|---|---|
| **A. 数据根内** | 单进程写者假设、原子写、模块锁、残尾自愈、锁与目录**全是现成的**；不碰用户工作树 | 默认不进版本库；**且可能被 §3.7 的工程边界挡住** | **建议打底**（配 B 的导出） |
| B. 项目根 `.seelex/` | 可与代码同仓提交、可 review；读工具天然可达 | ① 需 `.gitignore` 否定规则；② 与"数据根单写者/原子写"两套纪律并存 ⇒ 第二份文件写实现（`AGENTS.md §8`）；③ 项目根是用户工作树，写盘须走 `ProjectScope`（`AGENTS.md §6`） | 仅作**导出形态** |
| **C. A 打底 + 显式导出** | 落盘纪律白拿；"可提交"由用户开关决定（R7）；导出件可做**去本地化**清洗（去掉 `ws-<nano>`、绝对路径、时间戳） | 多一步导出动作 | **推荐** |

> **可提交件的三条硬纪律**（无论选 A 还是 C）：
> ① 不含 `lock.owner` / 会话正文 / 媒体 / 大输出；
> ② 不含 `ws-<nano>`、绝对路径、本机用户名（`workspace/workspace.go:165` 是机器本地 ID）；
> ③ 不含抖动时间戳（`ProjectRecord.BuiltAt` 会让每次刷新都产生 diff）。

### 4.4 数据形状

**行信封（两块 JSONL 共用，避免第二套读法）**

```jsonc
{
  "schema": 1,
  "kind": "work",                       // work | milestone | milestone_item
  "key": "work:todo:3",                 // 折叠键：同一 key 只认 Seq 最大的一行
  "seq": 12,                            // 本会话本文件内单调递增（水位 = 最大 seq）
  "at": 1760000000,                     // unix 秒
  "scope": { "project": "ws-…", "session": "seelex-1791905205083700300-3" },
  "payload": { /* 见下：与既有 DTO 同形，不复制状态机 */ }
}
```

- `kind=work` → 与 `WorkItem` 同义（`phase/task/status/assignee/dependencies/attachments` + `kind` + `batch`）。
  `payload.kind` 取**实际字符串**（`plan/todo/task/subagent/tasklist`，见 §3.2），**不新增枚举常量**。
- `kind=milestone` → 与 `TeamworkMilestone` 同义（`id/name/depends_on/required/status/content`）+ `plan_version`。
- `kind=milestone_item` → 与 `TeamworkWorkItem` 同义（`id/milestone/role/depends_on/status/session_id`）。
- 里程碑状态取数：**运行态 `TeamworkState.Milestones[id]` 优先**，计划里的 `TeamworkMilestone.Status` 兜底；
  整队是否收口读 **`TeamworkState.State`**（空 = 未收口；`closed`）——**不是** `TeamworkPlan.State`（§3.4 的 ⚠️）。
- **不复用** `BoardLifecycle` / `board_*`：那是「会话级**存档**」（整份替换 + 快照指纹 + 不进上下文），
  本方案是「**行级投影**」（append-only + 跨会话聚合）。同名会诱导误并，必须分名分责。

**读侧折叠（唯一读法）**

```go
// FoldProjectBoardRows 把追加流折成「每个 key 的最新一行」。
//   - 未知 schema / 未知 kind → 跳过并**记一行日志**（读侧不猜、不半读、不报错）；
//   - 残尾行（未换行收尾）在读取阶段就已被丢弃；
//   - 输出按 (kind, key) 稳定排序，保证渲染／导出字节稳定。
func FoldProjectBoardRows(rows []projectBoardRow) []projectBoardRow
```

**水位**：文件内最大 `seq`。写侧用它做幂等（同一批重复提交是空操作）；
读侧用它判断"活体投影的 revision 是否已落盘"（未落盘则读活体，I2）。

### 4.5 触发模型（写侧）

```yaml
limits:
  project_board:
    enabled: false          # 整块收回（口径同 async_exec.enabled：关就是关）
    hook: "todo_list"       # off | todo_list | task_complete | tool_complete | all
    flush_idle_ms: 2000     # 去抖：同一批次内多次触发只落一次
    max_rows: 512           # 单次追加行上限
```

`hook` 的语义（**触发面是装配表，不是散落的 `if name == …`**，对齐 `AGENTS.md §8.5`）：

| 取值 | 触发条件 | 适用 | 代价 |
|---|---|---|---|
| `off` | 不触发（比 `enabled:false` 更细的一档） | 只保留读面 | 0 |
| **`todo_list`（默认）** | 工具名 ∈ `{todo_init, todo_add, todo_done, todo_status, task_add, task_complete}` 且成功 | 贴合用户口径 | **覆盖不到"从不调 todo"的会话**（headless/子代理/角色） |
| `task_complete` | 只在会话终态工具后触发 | 很省 | 项目状态滞后到回合末 |
| `tool_complete` | 任何工具完成后触发（去抖 + 指纹去重） | 最及时 | 写盘次数最高 |
| `all` | 上面并集 | 调试用 | 最高 |

**落点拆分**（回答"挂在 session store 还是权限中间件"）：

| 问题 | 落点 | 为什么不是别处 |
|---|---|---|
| 能不能有这块能力 | 配置 `limits.project_board.enabled` + 装配处（`main.go`） | 与 `async_exec` 同款；关就是关 |
| 什么时候落盘 | **既有** `application/core/tool_hooks.go` 完成段的 `refreshWorkTableFromSources()` **之后** | 这是**唯一**能看到"模型列了 todo"的地方；且已有被动同步点（§3.3），不新造回调 |
| 写到哪、谁能写 | store 的项目作用域写面 `Router.withRepositoryAt(projectID, …)` + 会话分片文件 | `AGENTS.md §6` 要求读写携带正确 project scope |
| （可选）谁不许触发 | 权限规则里给只读主体（`emp_ro`）加 deny | 与工具白名单同表 |
| **模型怎么看见** | **不在这里** —— 见 §4.6（按需读） | 权限层没有内容端口；上下文层**不注入**（I7） |

> **不把被动能力挂在权限中间件上**：权限中间件是「判定器」，没有注入端口。
> 正确的拆分是「开关在装配处、触发在工具钩子、落盘在 store、读取在读取层」。

### 4.6 读取模型（按需，**不注入**）

**这是本版与上一版最大的差别。** 读面由三件事组成：

1. **"去哪读"**：一条**被动技能条目**（`plugins/<plugin>/<skill>/SKILL.md`，只把
   name + description 放进 `## Available Skills` 目录，见 §3.5）。它告诉模型：
   "当用户要求承接某会话 / 询问项目现状时，读 `.seelex/session-<id>/{worktable,teamwork}`
   与 `.seelex/project-known.md`"。
   —— 成本：几十 token、字节稳定、随插件切换才变（**可缓存**）。
2. **"怎么读"**：读面二选一（见 §3.7 与 §4.10 Q1）：
   - 若投影落在**绑定工程内** ⇒ 直接 `read_file`；
   - 若落在**工程外**（数据根情形）⇒ 必须有**专用读工具**（`project_board_read`）：
     只读、有界（`inject_rows` 等价的行上限）、按会话/项目聚合，复用
     `Router.ListWorkspace(projectID)`（`sessionstore/sessionstore.go:945`）与
     `SessionGranularStore.SessionsOf(projectID)`（`sessionstore/session_granular.go:676`）枚举会话，
     **不自造枚举**（`AGENTS.md §8`）。
3. **"什么时候读"**：由**提示词**决定。例：
   - "我要承接会话 X 的 worktable" → 读那一个会话的投影；
   - "我们项目现在是什么样" → 读聚合投影 + `project-known.md`。

**明确不做**：不把投影放进 system/project 前缀，**也不放进请求尾部**。
理由见 §1.3。

### 4.7 摘要模型（`project-known.md`：注册能力 + 被动 + 责任链）

`worktable`/`teamwork` 是免费的确定性投影；`project-known.md` 要 LLM 归纳，**是唯一要花 token 的部分**，
所以它的形态必须是：

| 要素 | 口径 |
|---|---|
| **注册能力** | 一个已注册的工具（复用/扩展既有 `project_refresh`，见 `main.go:784-801`），**不是**新写第二把工具 |
| **被动初始化/更新** | **无显式调用 → 不产文件**（不自动刷新、不在装配期构建）；只有用户在提示词里要求时才做 |
| **责任链** | 命中即处理、未命中传递；**复用**既有三分支判定（`RefreshProjectKnowledge` 的「来源 hash 未变→复用 / 已变→重建 / 重建失败→保留上一版本」）与既有来源合并链（`ProjectKnowledgeBuilder.scanModules()` 的「元数据（权威）→ 文档目录（补缺）→ 手写说明」） |
| **产物定位** | **`ProjectRecord` 的人读渲染（读侧视图），不是输入源** —— 否则就是第二份项目知识实现（`AGENTS.md §8.1/§8.3`） |
| **确定性** | 同一 `ProjectRecord` 两次渲染字节相同；**剔除 `BuiltAt`、绝对路径、`ws-<nano>`**（§4.3 三条硬纪律） |
| **前置** | 必须先修 G5 两处断点（§3.6），且**红灯先行**（`AGENTS.md §8.7`） |

> ⚠️ 命名撞车提醒：现有 `seelex.project.md`（来源名，实测不存在）、`project-record.json`（产物）、
> 本方案的 `project-known.md`（**渲染**）。文档与 README 里必须写明各自角色，避免后人误当同一个东西。

### 4.8 与「看板存档」七条不变式的关系

`sessionstore/board.go:1-20` 与 `docs/arch/session-board-metadata-lifecycle.md` 给**会话看板存档**
钉了 I1–I7。本方案的产物与那份存档**不是一回事**（那份是"渲染/快照恢复用、不进上下文"，
本方案是"给模型按需读的项目状态"），逐条交代：

| 存档的不变式 | 本方案 | 说明 |
|---|---|---|
| I1 领域代码不得读它做判定 | **继承** | 纯派生投影，任何 gate/裁决不得读它 |
| I2 活体优先 | **继承** | 本会话读活体，投影只补其他会话 |
| I3 枚举—锁一一对应 | **继承** | 若新增 `storageModule` 枚举，必须同批加 `mutexFor` case（`TestModuleLocksAreDistinct` 会炸） |
| I4 无变化不写盘 | **继承** | 去抖 + 指纹 + 水位 |
| I5 close 不复活 | **不适用** | 本项目看板没有"关闭"生命周期；"没有行就不渲染"的退场口径仍继承 |
| I6 schema 不猜 | **继承** | 未知 `schema` 跳过并记日志 |
| **I7 不进模型上下文** | **继承**（本版修正） | 上一版把这条当"故意不继承"；本版回到继承：投影**不常驻**上下文，只在被读时进入 |

### 4.9 中断与残留语义

本方案新增**进程外的持久文件**，必须先把"写到一半就死了"写清。仓库已有成熟范式
（`seelebridge/worktree` 的「失败保留现场 + 兜底清理器 `Prune`」）：

| 场景 | 处置 | 依据 |
|---|---|---|
| 追加中进程被杀 → 文件尾部半行 | **残尾视为未提交**：下次追加前截断到最后一个 `\n` 再续写 | `sessionstore/jsonl_io.go`（`truncateCrashTail`） |
| 一批行只写了一半 | **不需要补偿**：折叠读天然容忍"少一部分最新行"（读到上一版投影，不是坏数据） | §4.4 折叠读 |
| 触发它的会话/子代理被 kill | 不产生需人裁决的"现场"（本方案只有派生行）；并发由会话分片 + 独立锁隔离 | 对照 `worktree/README.md`：那里保留现场是因为现场里是**人的产出** |
| 残留回收 | 若将来确需清理（例如能力关停后遗留），照 `Prune` 两条判据：**不在册 + 干净** 才回收 | `worktree/README.md` |
| **会话被删除** | 投影同批回收（或标记为可回收孤儿） —— 本版新增（上一版遗漏） | §3.9 G8 |

> 一句话：**派生文件的中断语义比 git 现场简单得多**——残尾自愈 + 折叠读容忍缺行，就是全部；
> 不要为此再引入一套"保留现场"机制（那是第二份实现）。

### 4.10 待拍板

| # | 问题 | 选项 | 建议 |
|---|---|---|---|
| **Q1** | 物理落点 + 读取面 | A=数据根（读不到，§3.7）/ B=项目根 `.seelex/`（读得到，需 `ProjectScope` + 两套写纪律）/ C=A 打底 + 显式导出 + 专用读工具 | **C**。**但先答 §3.7**：落点在工程外就必须有 `project_board_read` |
| **Q2** | 会话目录命名 | 既有 `hash` 分片 / 字面 session id / `hash` 目录 + 清单映射 | **`hash` 目录 + 行内字面 id**；若必须字面目录，配套 `id↔dir` 清单 |
| **Q3** | `enabled` 默认值 | `false` / `true` | **`false`**：写盘免费，但 `todo_list` 覆盖不到的会话会产生噪声文件 |
| **Q4** | "责任链"的确切所指 | (a) `ProjectRecord` 的「复用/重建/回退」三分支 / (b) `.seelex` 查找链（工程根→数据根→exe 目录）/ (c) 来源合并链（元数据→文档→手写） | **(a)+(c) 复用既有**；(b) 仅在 Q1 选 C 时需要，且应复用 `main.go:93` 的既有配置责任链口径 |
| **Q5** | 行粒度 | `upsert` 事件行（推荐）/ 每次全表快照行 | **upsert**：选择器简单、体积小 |
| **Q6** | 跨会话写权 | 其他会话只读 / 允许写同一项目目录 | **只读**：分片写者唯一 |

---

## 5. teamwork 需要的里程碑

> 顺序事实：`depends_on` 是唯一依据（与 `TeamworkMilestone.DependsOn` 同口径：**里程碑之间是屏障**）。
> M0 是屏障；M1 与 M5a 可并行；M6 收口在最后。

```text
M0 契约冻结
 ├──> M1 存储面 ──> M2 投影 + 同步器 ──> M3 钩子 + 开关
 │                     └─────────────> M4 按需读取面
 └──> M5 项目摘要（M5a 修 G5 可与 M1 并行；M5b/M5c 依赖 M1）
                                     M3 + M4 + M5 ──> M6 收口
```

| # | 里程碑 | 交付物 | 验收判据（可测） | depends_on |
|---|---|---|---|---|
| **M0** | **契约冻结** | 修正后的口径落成 `docs/arch/seelex-project-board.md`；`sessionstore/README.md` 与 `application/core/README-work-table.md` 增节 | 评审通过；文档标注「规划」；本文 §7 的伪代码全部换成**真名**（`truncateCrashTail`、`record.Kind`、既有钩子完成段，见 §7.0） | — |
| **M1** | **存储面** | `sessionstore/project_board.go`（行信封、`FoldProjectBoardRows`、路径派生、追加、独立锁、有界）；Router 读/写两方法；删除绑定 | 追加幂等；残尾自愈；未知 schema 跳过**且记日志**；`-store` 换根路径随之变；路径与行内无绝对路径/用户名；空则**不建文件** | M0 |
| **M2** | **投影 + 同步器** | `application/core/project_board.go` 纯函数映射 + syncer（去抖/指纹/水位），挂**既有**同步点之后 | **一致性用例**：折叠读 == 活体 `buildWorkTable` 投影，逐行相等；删文件后重建结果相同（I1）；内容没变不写盘 | M1 |
| **M3** | **钩子 + 开关** | `limits.project_board.*` 解析 + 装配门 + 触发装配表 | `enabled:false` ⇒ 零文件、零读取面；`hook:todo_list` 时只有 todo/task 族触发；写盘失败不半写、不阻塞回合；syncer 可终止 | M2 |
| **M4** | **按需读取面**（**不含常驻注入**） | 被动技能条目（只放 name+description 进目录）+ 读面（工程内直读 **或** `project_board_read` 专用工具，按 Q1 定） | **回归**：能力开/关装配前缀字节**完全一致**；请求尾部**不含**项目看板段；未要求读时上下文无该内容 | M1 |
| **M5** | **项目摘要 `.seelex/project-known.md`** | a) 修 G5（来源可配置 + 统一 projectID 键，红灯先行）<br>b) `ProjectRecord` → MD 渲染（确定性）<br>c) 注册能力（扩展既有 `project_refresh`）+ 责任链 + 被动 | 写键=读键；两次渲染字节相同且不含时间戳/绝对路径/`ws-<nano>`；无显式调用不产文件；重建失败保留上一版本 | a: M0；b/c: M1 |
| **M6** | **收口** | 模块 README 增节 + 函数索引；端到端验收；token 增量实测留痕；可提交边界说明 | 端到端用例绿；文档与实现同步；可提交件不含 `lock.owner`/会话正文/媒体；交付含**删除清单**（`AGENTS.md §8.6`） | M3、M4、M5 |

### 5.1 用例清单（按里程碑编号）

> 层级：**unit** = 纯函数/单包；**integration** = 跨包；**e2e** = 真跑一轮；**live** = 需要真 provider/真进程（可选）。

| ID | 用例 | 层级 | 覆盖 |
|---|---|---|---|
| T1.1 | `TestProjectBoardAppendIdempotent`（同批两次 = 一次；水位不回退） | unit | I4 |
| T1.2 | `TestFoldProjectBoardRowsLatestWins`（同 key 取最大 Seq；输出稳定排序） | unit | §4.4 |
| T1.3 | `TestFoldProjectBoardRowsUnknownSkipped`（跳过 + 记日志） | unit | I6 |
| T1.4 | `TestProjectBoardCrashTailSelfHeals`（半行截断后仍可折叠读） | unit | I4 |
| T1.5 | `TestProjectBoardPathFollowsStoreRoot`（换 `-store` 根，路径随之变） | unit | §4.3 |
| T1.6 | `TestProjectBoardPathNoAbsoluteNoUser`（无绝对路径/用户名） | unit | §4.3 硬纪律② |
| T1.7 | `TestProjectBoardLocksDistinct`（与 message/teamwork 模块锁互异） | unit | I3 |
| T1.8 | `TestProjectBoardRowsBounded`（越界显式报错，不静默截断） | unit | I5 |
| T1.9 | `TestProjectBoardEmptyNoFile`（空则退场） | unit | I6 |
| T1.10 | `TestProjectBoardDeleteBindsSession`（删会话 ⇒ 投影回收） | integration | G8 |
| **T2.1** | `TestBuildProjectBoardRowsMatchesLiveProjection`（**核心一致性**：折叠读 == 活体投影） | integration | I1/I3 |
| T2.2 | `TestBuildProjectBoardRowsNoJudgement`（只搬运，无二次判定） | unit | I3 |
| T2.3 | `TestBuildProjectBoardRowsKindMapping`（每个 kind 显式"投影/不投影"） | unit | §3.2 |
| T2.4 | `TestProjectBoardSyncerDebounce`（同批只落一次） | unit | I4 |
| T2.5 | `TestProjectBoardSyncerFingerprint`（内容没变不写盘） | unit | I4 |
| T2.6 | `TestProjectBoardSyncerWatermark`（水位推进；重复提交空操作） | unit | §4.4 |
| T2.7 | `TestProjectBoardTeamworkMilestonePriority`（运行态优先、声明态兜底；收口读 `TeamworkState.State`） | unit | §3.4 |
| T2.8 | `TestProjectBoardRebuildAfterDelete`（删文件后重建结果相同） | integration | I1 |
| **T2.9** | `TestProjectBoardIsNotAnInput`（**反例**：篡改文件 ⇒ 活体与领域判定不变） | integration | I1/I2 |
| T3.1 | `TestProjectBoardDisabledNoArtifacts`（"关就是关"） | integration | R5 |
| T3.2 | `TestProjectBoardTriggerTodoListOnly` | unit | R5 |
| T3.3 | `TestProjectBoardTriggerTaskComplete` / `…ToolComplete` | unit | R5 |
| T3.4 | `TestProjectBoardHookFailureNoHalfWrite` | integration | I4 |
| T3.5 | `TestProjectBoardSyncerShutdown`（goroutine/信号口可终止） | unit | `AGENTS.md §6` |
| T3.6 | `TestProjectBoardHooksOnExistingSyncPoint`（挂既有同步点，不新造回调） | integration | §3.3 |
| **T4.1** | `TestProjectBoardPrefixByteIdentical`（**回归**：开关前后前缀字节完全一致） | integration | **I7** |
| **T4.2** | `TestProjectBoardNoTailInjection`（请求尾部不含项目看板段） | integration | **I7** |
| T4.3 | `TestProjectBoardSkillCatalogStable`（只含 name+description、按名排序、随插件切换才变） | unit | §3.5 |
| T4.4 | `TestProjectBoardReadOnDemand`（要求读才读；未要求时上下文无该内容） | e2e | R3② |
| T4.5 | `TestProjectBoardReadBoundary`（落点在工程外 ⇒ 明确失败并指向专用读工具） | integration | §3.7 |
| T4.6 | `TestProjectBoardTokenDelta`（实测单轮增量 ≈ 0，只余技能目录固定增量） | live | §1.3 |
| **T5.1** | `TestProjectRefreshWriteReadSameKey`（**红灯先行**：先复现写键≠读键、修后绿） | integration | G5② |
| T5.2 | `TestProjectRefreshSourcesConfigurable`（默认对齐真实路径；无来源 ⇒ 明确错误） | unit | G5① |
| T5.3 | `TestProjectKnownRenderDeterministic`（两次渲染字节相同；无时间戳/绝对路径/`ws-…`） | unit | §4.7 |
| T5.4 | `TestProjectKnownPassiveOnly`（无显式调用不产文件） | integration | R6 |
| T5.5 | `TestProjectKnownReusesExistingChain`（命中即处理；**复用**既有 builder/判定；交付列删除清单） | unit | `AGENTS.md §8` |
| T5.6 | `TestProjectKnownRebuildFailureKeepsLast`（可回退；首建失败显式报错） | unit | §4.7 |
| T6.1 | `TestProjectBoardGitignoreBoundary`（可提交件不含 `lock.owner`/正文/媒体） | unit | R7 |
| T6.2 | `TestProjectBoardEndToEnd`（todo → 落盘 → 新会话按提示词读 → 与权威源一致） | e2e | 全局 |
| T6.3 | `TestProjectBoardMultiProcessPolicy`（多进程下不交错，或明确记录"要求单进程"） | live | §3.8 |
| T6.4 | `TestProjectBoardDocsConsistent`（README/`docs/arch` 与实现同步） | unit | `AGENTS.md §3` |

**红灯先行顺序**（`AGENTS.md §8.7`）：**T5.1**（复现 G5 断点）与 **T2.9**（防第二事实源）先红。

---

## 6. 每个里程碑的改动范围

### M0 契约冻结

| 包 | 文件 | 改动 |
|---|---|---|
| `docs/arch` | **新** `seelex-project-board.md` | 落成稳定口径（本文 §4 的定型版） |
| `docs` | 本目录 `plan.md` / `plan-review.md` | 标注状态与关系 |
| `sessionstore` | `README.md` | 增节：投影层的生态位与不变量 |
| `application/core` | `README-work-table.md` | 增节：写侧同步点与读侧分离 |

**不做**：任何 Go 代码。

### M1 存储面

| 包 | 文件 | 改动 | 为什么是这个包 |
|---|---|---|---|
| `sessionstore` | **新** `project_board.go` | 行信封类型、`FoldProjectBoardRows`、路径派生（复用 `projectDir`/`sessionDir` 同款 `hash`）、追加（复用 `appendEventLogLine` + `truncateCrashTail`）、独立锁、有界行数 | 存储语义归 `sessionstore`（`AGENTS.md §1`） |
| `sessionstore` | `sessionstore.go` | `Router.AppendProjectBoardRows(projectID, sessionID, …)`、`Router.ReadProjectBoardRows(projectID, opts)`（走 `withRepositoryAt`） | 项目作用域写面的唯一入口在 Router |
| `sessionstore` | `module_heads.go` | **仅加锁**（本方案走独立数据文件，不新增 `storageModule` 枚举）；若最终决定走模块 head 路线，则必须同批加 `mutexFor` case | 枚举未映射会 panic |
| `sessionstore` | `lifecycle.go` | 删除会话时同批回收投影目录（G8） | 生命周期归存储层 |

**复用清单（禁止新写）**：JSONL 追加、残尾截断、原子写、目录派生、锁枚举。
**不自造**：第二份 JSONL 追加/原子替换实现。

### M2 投影 + 同步器

| 包 | 文件 | 改动 |
|---|---|---|
| `application/core` | **新** `project_board.go` | 纯函数 `buildProjectBoardRows(workItems, teamPlan, teamState)`；`projectBoardSyncer`（去抖/幂等/水位/指纹） |
| `application/core` | `tool_hooks.go` | 在既有 `refreshWorkTableFromSources()` **之后**调用 syncer（**不新增回调入口**） |
| `application/core` | `work_table.go` | 暴露"本会话活体投影"给 syncer 的取数函数（**不重算**，直接读既有产物） |
| `seelebridge` | **新** `runtime_project_board.go` | 窄可选端口 `ProjectBoardPort`（类型断言发现可选能力的既有范式） |
| `seelebridge/teamwork` | **不改** | 里程碑状态仍由域维护，本方案**只读**其投影 |

### M3 钩子 + 开关

| 包 | 文件 | 改动 |
|---|---|---|
| `config` | `seelex.yaml` | `limits.project_board.*` + 注释（照抄 `async_exec` 的说明风格） |
| `main.go` | 装配处 | 读配置 → 构造 store 端口 + syncer → **按开关决定挂不挂**（未启用则一律不挂） |
| `application/core` | `project_board.go` | 触发装配表 `triggerFor(hookMode)`（**装配表，不是散落分支**） |

### M4 按需读取面

| 包 | 文件 | 改动 |
|---|---|---|
| `plugins/<plugin>/<skill>/SKILL.md` | **新** 被动技能 | 只宣告 name + description：何时读、去哪读、读哪几个文件 |
| `sessionstore` | `session_granular.go` / `sessionstore.go` | 聚合读：复用 `ListWorkspace` + `SessionsOf`，**只读、有界** |
| （按 Q1） | 读工具 | 若落点在工程外：**新** `project_board_read`（只读、有界、聚合）；若在工程内：不新增工具 |
| `application/core` | `prompt_layer` | **不改** —— 明确**不**把投影放进 system 前缀、**不**放进请求尾部 |

### M5 项目摘要

| 包 | 文件 | 改动 |
|---|---|---|
| `sessionstore` | `project_record.go` | ① 默认来源对齐真实路径（或可配置）；② **统一 projectID 键**（写用工作区 ID = 读用工作区 ID） |
| `main.go` | `registerProjectRefreshTool` | 扩展渲染动作（`project-known.md`），**复用**既有 `RefreshProjectKnowledge` |
| `sessionstore` | **新** `project_known.go`（或并入 `project_record.go`） | `ProjectRecord` → MD 的确定性渲染（剔时间戳/绝对路径/`ws-…`） |
| `seelebridge` | 权限/工具面 | 摘要能力保持**被动**：不在装配期构建，不加自动触发 |

### M6 收口

| 包 | 文件 | 改动 |
|---|---|---|
| `sessionstore` / `application/core` | `README*.md` + 函数索引 | 增节与索引（`AGENTS.md §3`） |
| `docs/arch` | `seelex-project-board.md` | 定稿；标注已实现范围 |
| 交付物 | —— | 删除清单、端到端证据、token 增量实测读数、可提交边界说明 |

---

## 7. 关键改动的伪代码

### 7.0 先把上一版的假 API 换掉（开工前必查）

| 上一版写的 | 实际不存在 | 本版用真名 |
|---|---|---|
| `toolHooks.SetAfterToolComplete(...)` | 全仓只在该 plan 命中 | 在 `ToolHookBridge.Hooks()` 完成段**既有** `refreshWorkTableFromSources()` 之后接线 |
| `repairJSONLTail(path)` | 全仓只在该 plan 命中 | `truncateCrashTail(file *os.File)`（`sessionstore/jsonl_io.go`） |
| `WorkKindTodo` / `WorkKindTask` | 全仓只在该 plan 命中 | `record.Kind` 字符串：`plan/todo/task/subagent/tasklist` |
| `teamPlan.State == "closed"` | `TeamworkPlan.State` 是 `TeamworkState` **结构体** | 读 `TeamworkState.State`（空 = 未收口；`closed`） |

### 7.1 行信封与折叠读（`sessionstore/project_board.go`）

```go
package sessionstore

const (
	projectBoardSchema  = 1
	projectBoardMaxRows = 512 // 单次追加行上限（防一口气写爆）
)

type projectBoardScope struct {
	Project string `json:"project"` // 工作区 ID（不是绝对路径）
	Session string `json:"session"` // 会话 ID（字面值；目录名走 hash）
}

type projectBoardRow struct {
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"` // work | milestone | milestone_item
	Key     string            `json:"key"`  // 折叠键：同一 key 只认 Seq 最大的一行
	Seq     uint64            `json:"seq"`  // 本文件内单调递增（水位 = 最大 seq）
	At      int64             `json:"at"`
	Scope   projectBoardScope `json:"scope"`
	Payload json.RawMessage   `json:"payload"`
}

// FoldProjectBoardRows 是**唯一**读法：每个 key 取 Seq 最大的那一行。
//   - 未知 schema / 未知 kind → 跳过并**记一行日志**（读侧不猜、不半读、不报错）；
//   - 残尾行在读取阶段已被丢弃；
//   - 输出按 (kind, key) 稳定排序（渲染/导出字节稳定的前提）。
func FoldProjectBoardRows(rows []projectBoardRow, logf func(string, ...any)) []projectBoardRow {
	latest := make(map[string]projectBoardRow, len(rows))
	for _, row := range rows {
		if row.Schema != projectBoardSchema {
			logf("project board: skip unknown schema %d (key=%s)", row.Schema, row.Key)
			continue
		}
		if !knownProjectBoardKind(row.Kind) {
			logf("project board: skip unknown kind %q (key=%s)", row.Kind, row.Key)
			continue
		}
		if prev, ok := latest[row.Key]; !ok || row.Seq > prev.Seq {
			latest[row.Key] = row
		}
	}
	out := make([]projectBoardRow, 0, len(latest))
	for _, row := range latest {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}
```

### 7.2 追加（同文件；复用既有追加与残尾自愈）

```go
// AppendProjectBoardRows 追加一批行（一条事实一行；残尾自愈；不读整份文件）。
//   - 幂等：水位（文件内最大 Seq）≥ 本批最小 Seq ⇒ 重复提交，空操作；
//   - 退场：文件不存在且本批为空 ⇒ **不建文件**（I6）；
//   - 单写者：本会话文件只有本会话写（Q6 定：跨会话只读）。
func (repository *jsonRepository) AppendProjectBoardRows(
	projectID, sessionID, file string, rows []projectBoardRow,
) (watermark uint64, err error) {
	if err := validateProjectID(projectID); err != nil { // sessionstore.go:248
		return 0, err
	}
	if len(rows) == 0 || len(rows) > projectBoardMaxRows {
		return 0, fmt.Errorf("project board: rows out of bounds: %d", len(rows))
	}

	lock := repository.projectBoardLock(projectID, sessionID) // 独立锁，不与 message/teamwork 共用
	lock.Lock()
	defer lock.Unlock()

	path := repository.projectBoardPath(projectID, sessionID, file)
	if watermark, ok, err := repository.projectBoardWatermark(path, rows); err != nil {
		return 0, err
	} else if ok {
		return watermark, nil // 幂等
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	// 复用既有追加语义：先截残尾，再 O_APPEND 一行一条。
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := truncateCrashTail(file); err != nil { // jsonl_io.go
		return 0, err
	}
	for _, row := range rows {
		line, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return 0, marshalErr
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			return 0, err
		}
	}
	return rows[len(rows)-1].Seq, nil
}

// projectBoardPath 按数据根派生；目录名沿用 hash（与 projectDir/sessionDir 同款）。
func (repository *jsonRepository) projectBoardPath(projectID, sessionID, file string) string {
	return filepath.Join(
		repository.root, projectBoardDir, "project-"+hash(projectID), "session-"+hash(sessionID), file,
	)
}
```

### 7.3 纯函数投影（`application/core/project_board.go`）

```go
// buildProjectBoardRows 把**既有投影的输出**映射成项目看板行。
// 语义只映射、不判定：状态取值/依赖关系都来自入参（I3）。
//   - workItems 来自既有 buildWorkTable（work_table.go:30）
//   - teamPlan  来自 moduleTeamwork head（sessionstore/teamwork.go:98）
//   - teamState 来自运行态投影（"忙到什么阶段"的权威读数）
func buildProjectBoardRows(
	scope sessionstore.ProjectBoardScope, seq uint64, now int64,
	workItems []WorkItem, teamPlan *TeamworkPlan, teamState *TeamworkState,
) []sessionstore.ProjectBoardRow {
	rows := make([]sessionstore.ProjectBoardRow, 0, len(workItems))
	for _, item := range workItems {
		switch item.Kind { // 实际字符串取值，不新增枚举
		case "todo", "task":
			rows = append(rows, row("work", "work:"+item.ID, seq, now, scope, map[string]any{
				"task": item.Task, "status": item.Status, "kind": item.Kind,
				"assignee": item.Assignee, "dependencies": item.Dependencies,
				"batch": item.BatchID,
			}))
		default:
			// plan/subagent/tasklist/async 行各有别的读面，本投影**不搬运**（显式声明）
		}
	}
	if teamPlan == nil {
		return rows
	}
	for _, ms := range teamPlan.Milestones {
		rows = append(rows, row("milestone", "milestone:"+ms.ID, seq, now, scope, map[string]any{
			"id": ms.ID, "name": ms.Name,
			"depends_on": ms.DependsOn, // 屏障：里程碑之间的顺序唯一事实
			"required":   ms.Required,
			"status":     milestoneStatusFor(teamState, ms), // 运行态优先，声明态兜底
			"content":    ms.Content,
			"plan_version": teamPlan.Version,
			"team_state":   teamStateOf(teamState), // 空 = 未收口；closed = 已收口
		}))
		for _, item := range ms.Items {
			rows = append(rows, row("milestone_item", "milestone:"+ms.ID+"/item:"+item.ID,
				seq, now, scope, map[string]any{
					"id": item.ID, "milestone": item.Milestone, "role": item.Role,
					"depends_on": item.DependsOn, // 里程碑内 DAG
					"status":     item.StatusOrPending(),
					"session_id": item.SessionID,
				}))
		}
	}
	return rows
}

// milestoneStatusFor：运行态优先、声明态兜底（teamwork.go:151-172）。
func milestoneStatusFor(state *TeamworkState, ms TeamworkMilestone) string {
	if state != nil {
		if value := strings.TrimSpace(state.Milestones[ms.ID]); value != "" {
			return value
		}
	}
	return ms.Status // 空 = pending
}

// teamStateOf：收口事实在 TeamworkState.State（**不是** TeamworkPlan.State）。
func teamStateOf(state *TeamworkState) string {
	if state == nil {
		return ""
	}
	return state.State
}
```

### 7.4 同步器（去抖 / 指纹 / 水位）

```go
// OnToolComplete 由既有钩子完成段调用（tool_hooks.go）。它只做两件事：
// 判触发面 → 标脏 → 唤醒唯一的 flush goroutine（信号口容量 1，latest-wins）。
// **不是**新造的工具完成回调（§7.0）。
func (syncer *projectBoardSyncer) OnToolComplete(name string, err error, sessionID string) {
	if err != nil || !syncer.triggers.matches(name) {
		return
	}
	syncer.mu.Lock()
	syncer.dirty[sessionID] = struct{}{}
	syncer.mu.Unlock()
	syncer.kick() // 唤醒唯一 flush goroutine
}

// flush 是**唯一**写盘路径（单写者）：取活体投影 → 纯函数映射 → 指纹去重 → 追加。
func (syncer *projectBoardSyncer) flush(sessionID string) error {
	workItems := syncer.deps.WorkTableRows(sessionID) // 既有 buildWorkTable 输出，不重算
	teamPlan := syncer.deps.TeamPlanFor(sessionID)     // nil = 本会话无团队计划
	teamState := syncer.deps.TeamStateFor(sessionID)   // 运行态投影（可 nil）

	seq := syncer.nextSeq(sessionID)
	rows := buildProjectBoardRows(syncer.scopeFor(sessionID), seq, time.Now().Unix(),
		workItems, teamPlan, teamState)
	if len(rows) == 0 {
		return nil // I6：空则退场，不建文件、不留空壳
	}
	fingerprint := hashRows(rows)
	if syncer.lastFingerprint[sessionID] == fingerprint {
		return nil // 内容没变不写盘（对齐 board.go 的指纹节流口径）
	}
	watermark, err := syncer.deps.Store.AppendProjectBoardRows(
		syncer.projectIDFor(sessionID), sessionID, sessionstore.ProjectBoardWorkFile, rows,
	)
	if err != nil {
		return err // 失败不半写：追加是逐行的，最坏留下一批完整行，折叠读仍自洽
	}
	syncer.lastFingerprint[sessionID] = fingerprint
	syncer.lastWatermark[sessionID] = watermark
	return nil
}
```

### 7.5 触发装配表（不是散落分支）

```go
// projectBoardTrigger 是**装配表**（AGENTS.md §8.5）。
type projectBoardTrigger struct {
	tools map[string]struct{}
	all   bool
	empty bool
}

func triggerFor(hookMode string) projectBoardTrigger {
	switch hookMode {
	case "todo_list": // 默认：贴合用户口径（代价：覆盖不到从不调 todo 的会话）
		return projectBoardTrigger{tools: setOf(
			"todo_init", "todo_add", "todo_done", "todo_status", "task_add", "task_complete",
		)}
	case "task_complete":
		return projectBoardTrigger{tools: setOf("task_complete")}
	case "tool_complete", "all":
		return projectBoardTrigger{all: true}
	default: // "off"
		return projectBoardTrigger{empty: true}
	}
}
```

### 7.6 读取面（**按需**，不注入）

```go
// ReadProjectBoardRows 读一个项目的项目级行：本会话给不给由调用方定，
// 其他会话逐个折叠读；总数与会话数都**有上限**（I5）。只读，不合并写（Q6）。
func (router *Router) ReadProjectBoardRows(projectID string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool) {
	var all []ProjectBoardRow
	metas := router.ListWorkspace(projectID) // 既有项目→会话枚举（sessionstore.go:945），不读正文
	for index, meta := range metas {
		if opts.ExcludeSession != "" && meta.SessionID == opts.ExcludeSession {
			continue
		}
		if opts.MaxSessions > 0 && index >= opts.MaxSessions {
			break
		}
		rows := FoldProjectBoardRows(readProjectBoardFile(router, projectID, meta.SessionID), logf)
		if len(all)+len(rows) >= opts.MaxRows {
			return append(all, rows[:opts.MaxRows-len(all)]...), true // Truncated=true，显式标注
		}
		all = append(all, rows...)
	}
	return all, false
}

// projectBoardReadTool（仅当 Q1 判定落点在工程外时新增）：
//   入参：{ project?: string, session?: string, kind?: "work"|"milestone"|"milestone_item", limit?: int }
//   返回：有界的行文本（纯函数渲染，字节稳定）；**只读**，不改任何状态。
//   为什么必须有它：read_file 的边界是"绑定工程"（§3.7），数据根里的投影它读不到。
```

**读侧"去哪读"的被动技能条目**（只把 name + description 放进 `## Available Skills`）：

```markdown
---
name: project-board
description: 当用户要求“承接某个会话的工作表格/看板”、询问“这个项目在忙什么/里程碑到哪了”，
  或要求初始化/更新项目摘要时使用。读 .seelex/session-<id>/{worktable,teamwork} 与
  .seelex/project-known.md；不要在没有用户要求时主动读取或刷新。
---
（正文只在被激活后注入，不进目录段 —— 见 RenderSkillCatalog 的"发现不泄全文"口径）
```

### 7.7 摘要渲染与责任链（**复用**既有链）

```go
// 责任链 = 既有判定，不新写一条链（§4.7 / AGENTS.md §8.1）。
// 命中即处理、未命中传递：
//
//   [1] 无来源          → 明确报错（不产空记录）        ── 首建失败显式提示
//   [2] 来源 hash 未变  → 复用已有 ProjectRecord         ── 不重建（O(0) 写）
//   [3] 来源已变        → 重建 ProjectRecord → 落盘      ── 增量
//   [4] 重建失败        → 保留上一版本（可回退）          ── 有 existing 才回退
//
//   这四步就是 RefreshProjectKnowledge 的既有语义（sessionstore/project_record.go），
//   本方案**只加一步渲染**：
func ProjectKnownMarkdown(record ProjectRecord) []byte {
	var b strings.Builder
	b.WriteString("# 项目摘要（Project Known）\n\n")
	// ⚠️ 可提交件纪律：不含 BuiltAt、不含绝对路径、不含 ws-<nano>（§4.3）
	for _, module := range record.Modules {
		b.WriteString("## " + module.Name + "\n\n")
		b.WriteString(strings.TrimSpace(module.Summary) + "\n\n")
		// Docs 只写仓库相对路径；Path 若为绝对路径则丢弃
	}
	return []byte(b.String()) // 纯函数：同一 record 两次渲染字节相同
}

// 被动：**只在**用户要求时调用；装配期/每轮/自动刷新一律不调用（R6）。
func refreshProjectKnown(ctx context.Context, store *sessionstore.Router, root string, force bool) error {
	builder := sessionstore.NewProjectKnowledgeBuilder(sessionstore.ProjectKnowledgeSources{
		Root: root,
		// 默认来源必须对齐真实路径：docs/gui/module_dotting.json（修 G5①）
	})
	result, err := sessionstore.RefreshProjectKnowledge(ctx, store, builder, force) // 复用既有链
	if err != nil {
		return err
	}
	record, err := store.LoadProjectRecord(store.Workspace()) // 统一键：工作区 ID（修 G5②）
	if err != nil {
		return err
	}
	return store.WriteProjectKnownFile(store.Workspace(), ProjectKnownMarkdown(record))
}
```

### 7.8 装配（`main.go`：能力开关决定"挂不挂"，没有半个能力）

```go
func installProjectBoard(runtime *seelebridge.Runtime, hooks *application.ToolHookBridge,
	store *sessionstore.Router, limits ProjectBoardLimits) error {
	if !limits.Enabled {
		return nil // 不挂同步器、不注册读工具、不产文件（关就是关）
	}
	syncer := core.NewProjectBoardSyncer(core.ProjectBoardDeps{
		Store:         store,
		WorkTableRows: core.WorkTableRowsFor, // 既有产物，不重算
		TeamPlanFor:   core.TeamPlanFor,
		TeamStateFor:  core.TeamStateFor,
		Triggers:      core.TriggerFor(limits.Hook),
		FlushIdle:     time.Duration(limits.FlushIdleMS) * time.Millisecond,
	})
	hooks.SetProjectBoardSink(syncer.OnToolComplete) // 接进**既有**完成段，不新造回调
	if limits.ReadTool {
		runtime.RegisterTool("project_board_read", "...", projectBoardReadSchema, app.ProjectBoardReadHandler)
	}
	return nil
}
```

### 7.9 目标态数据流（端到端）

```text
模型调 todo_add / todo_done / task_complete
        │
        ▼
task.TaskRegistry（actor，唯一权威源）
        │  既有钩子完成段 → refreshWorkTableFromSources()
        ▼
projectBoardSyncer（去抖 + 指纹 + 水位）            ← 配置：limits.project_board.hook
        │  纯函数映射（buildWorkTable + TeamworkPlan + TeamworkState）
        ▼
sessionstore.AppendProjectBoardRows（O_APPEND 一行一条；残尾自愈）
        │
        ▼
<baseDir>/<boardRoot>/project-<hash>/session-<hash>/{worktable,teamwork}.jsonl
        │
        └─ ① 不进任何常驻上下文段（I7）
          ② 新会话按提示词要求 → 读面（read_file 或 project_board_read）
          ③ 项目摘要：用户要求 → 注册能力 → 责任链 → project-known.md
```

---

## 附：术语与分名分责（防误并）

| 名字 | 是什么 | 谁写 | 进上下文？ |
|---|---|---|---|
| `board_goal` / `board_team`（`metadata/*.json`） | 会话级**看板存档**（快照恢复用） | goal 域 / teamwork 域 | ❌（`board.go` I7） |
| `ProjectRecord`（`project-record.json`） | 项目级**模块语义记录** | `project_refresh` 工具 | ✅（前缀 `project` 块） |
| `project-known.md` | `ProjectRecord` 的**人读渲染** | 同上（本方案新增渲染动作） | 按需 |
| `worktable` / `teamwork`（本方案） | 会话级**任务/里程碑投影** | 钩子（代码） | 按需 |
| `scheduled-tasks.jsonl` | 全局**任务定义** | 调度器 | ❌ |
| `AGENTS.md` | 人类协作规范 | 人 | ❌（运行期无消费者） |
