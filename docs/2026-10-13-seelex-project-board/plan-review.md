# 项目看板 plan 的纠偏、遗留问题、里程碑与测试用例

日期：2026-10-14 ｜ 状态：**评审记录（未改代码，未改 plan.md 正文）**
输入：`docs/2026-10-13-seelex-project-board/plan.md`（73 KiB，设计稿）、
`docs/2026-10-13-seelex-dot-a2a-star-topology/README.md`（上游头脑风暴）、
以及本轮作者对 `main.go` / `sessionstore/` / `application/core/` / `config/` 的逐点自读。

证据分级：**Confirmed** = 本轮实测（带 `文件:行`，可复现）；**Hypothesis** = 推断，附"怎么坐实"。
本文只做四件事：**① 纠偏 ② 遗留问题 ③ 里程碑拆分 ④ 测试用例**。不构成待办，拍板前不动代码。

---

## 一、纠偏：用户口径 vs plan.md 现状

用户本轮给出的修正，主要打在 plan.md 的**三个假设**上：它假设 `.seelex` 的产物是"第二套可读文件"、
假设产物要**每轮注入上下文**、假设产物**默认不进版本库**。逐条对照如下。

| # | 纠偏点 | plan.md 现状（哪一节） | 应改口径 |
|---|---|---|---|
| **C1** | `.seelex` 是**投影**，不是第二事实源 | §0、§3.1 已写"派生投影"，但 §3.9 又给它开了 **I7「故意不继承」**的特例（"本项目看板的存在理由就是进上下文"），事实上把它抬成了"上下文供给源" | **全部产物 = 纯派生投影，可删可重建**；唯一事实源仍是 `task.TaskRegistry` / `SessionRecord.Tasks` / `moduleTeamwork` head / `teamwork/events.jsonl`。§3.9 的 I7 特例**取消** |
| **C2** | **钩子触发，但不直接插入上下文**；不进前缀、不破坏缓存命中 | §0「由既有的请求尾部注入位把摘要送回模型上下文」、§1 **R3**「模型**不需要**额外调工具就能看见结果」、§3.5 四条链路「模型怎么看见 = 请求尾部注入位」、**§3.6 整节**、§4.1（`service_assembler.go` 注入）、§5.4、§6 **M4**、§9 末段、§10 待拍板 3、§7 R2/R3 | **删掉"每轮注入"整条设计**：写侧只落盘，**不新增任何常驻上下文段**（既不进 system/project 前缀，也不进请求尾部）。读侧**只在新会话/新回合由提示词要求时按需读取**（例：用户说"我要承接会话 X 的 worktable"、"我们项目现在是什么样"）。⇒ 每轮请求字节不变，前缀缓存命中不受影响 |
| **C3** | token 成本只在"项目摘要 MD"上；它必须走**注册能力 + 被动初始化/更新 + 责任链** | plan 把 token 成本摊在"每轮注入"（§7 R2），并对项目知识另有 G5 断点 | **两块投影（worktable/teamwork）是确定性代码 → 0 token**（不产生 LLM 生成）。**唯一有 token 成本的是 `.seelex/project-known.md`**：它需要 LLM 归纳 ⇒ 必须是**一个已注册的能力（工具）**，**被动**初始化/更新（只有用户在提示词里要求了才做），用**责任链**组织。⚠️ 仓里已有一条同义链（见 D-11），**必须复用/扩展，不许另起一份**（`AGENTS.md §8.1/§8.3`） |
| **C4** | `.seelex` 可提交/不可提交，**由用户自己决定** | §3.3 把 A 案（数据根内、不提交）与 B 案（项目根、可提交）对立起来，并把"是否可提交"当成需要拍板的**设计选择**（§10 待拍板 1、R7） | 落点仍建议按数据根派生，但**"是否进版本库"不写死**为设计前提：默认 = 本机派生缓存（不提交）；**要不要加 `.gitignore` 否定规则、要不要提交，由用户决定**，实现侧不依赖该决定（见 D-3/D-4） |
| **C5** | 触发面**默认值 = `todo_list`** | §3.5 已是默认 `todo_list` | **保留**（唯一要改的是它的结果去向：不是"送进上下文"，而是"写进投影文件"） |
| **C6** | **按会话 id** 区分 worktable 与 teamwork；`.seelex/session-xxx/{worktable,teamwork}` + `.seelex/project-known.md` | §3.3 的映射是 `<baseDir>/sessions-json/project-<hash>/session-<hash>/board/{worktable,teamwork}.jsonl`；项目摘要是 `project-record.json`（不是 md，也没有 `project-known.md` 这个名字） | 采纳用户口径作为**逻辑命名**（会话 id 作键、`worktable`/`teamwork` 两个产物、项目一份 `project-known.md`）；**物理落点**要解决与既有 hash 分片布局的张力（见 D-1/D-5） |
| **C7** | "被动技能"是 plan 自己第 ① 条通道 | §2.5 已识别两条既有通道（被动技能目录 + 事件钩子），但把 ① 当作"注入哲学"来用 | ✅ 这条**本来就对**：仓里的"被动技能目录"是 `## Available Skills` 段 —— `RenderSkillCatalog`（`application/core/prompt_layer/skill_catalog.go`）只渲染 **name + description**，按 name 排序、**字节稳定、随插件切换才变化**（前缀缓存友好）。**新会话"知道去哪读"靠的就是这条目录条目**，而不是把投影内容塞进前缀 |

### 1.1 纠偏后的三层（改 plan §3.1 的那张图）

```text
  ┌─ 事实层（唯一权威，已有，本方案一行不改）────────────────────────────┐
  │ task.TaskRegistry（kind=todo/task 的行，actor）                        │
  │ SessionRecord.Tasks（会话落盘）                                        │
  │ moduleTeamwork head = TeamworkPlan（里程碑/工作项）                     │
  │ teamwork/events.jsonl（追加审计）                                      │
  └───────────────────────────┬───────────────────────────────────────────┘
                              │ ① 钩子（代码，确定性，0 token）
                              │    tool_complete ∈ {todo_*, task_add, task_complete}
                              ▼
  ┌─ 投影层（新增，派生、可重建、不是事实源）──────────────────────────────┐
  │ .seelex/session-<id>/worktable      ← 任务行投影                       │
  │ .seelex/session-<id>/teamwork       ← 里程碑 + 工作项投影              │
  └───────────────────────────┬───────────────────────────────────────────┘
                              │ ② **按需**读（不是每轮注入！）
                              │    触发 = 提示词要求（"承接会话 X"/"项目什么样"）
                              ▼
  ┌─ 读取层（新增，被动、有界、0 常驻字节）────────────────────────────────┐
  │ 新会话靠 `## Available Skills` 里的被动技能条目知道"去哪读、怎么读"     │
  │ 读面：ListWorkspace 聚合 + 专用读工具（见 D-6：read_file 有工程边界）  │
  └───────────────────────────┬───────────────────────────────────────────┘
                              │ ③ 唯一有 token 成本的一步
                              ▼
  ┌─ 摘要层（`.seelex/project-known.md`）─────────────────────────────────┐
  │ 已注册能力 + 被动初始化/更新（只有提示词要求才做）+ 责任链            │
  │ 内容 = ProjectRecord 的人读渲染（不是新的输入源！见 D-11）             │
  └───────────────────────────────────────────────────────────────────────┘
```

**一句话**：`worktable`/`teamwork` 是**免费的确定性投影**；`project-known.md` 是**要花钱的归纳产物**，
所以它必须"要人喊才做"。plan 把这两件事的成本模型搞反了——它让最便宜的东西每轮付费（注入），
又让最贵的东西没有明确的能力入口。

### 1.2 plan.md 需要就地改的节（清单）

| plan 节 | 处理 |
|---|---|
| §0 一句话 | 删"由既有的请求尾部注入位把摘要送回模型上下文" |
| §1 R3 | 改写验收项：**触发=落下投影；读取=提示词要求时按需**（不是"模型零调用可见"） |
| §3.2 铁律 | I1/I2/I3 保留；I7 相关表述并入 I1（不再是特例） |
| §3.5 触发模型 | 保留装配表与 `todo_list` 默认；**删掉"模型怎么看见 = 请求尾部注入位"那一行**，改为"写侧落盘" |
| **§3.6 注入模型（整节）** | **删除**，替换为「按需读取面」 |
| §3.9 与七条不变式的关系 | **I7 由"故意不继承"改为"继承"**；I5 仍不适用 |
| §4.1 / §4.2 | 删 `service_assembler.go` 注入项与 `prompt_layer 不改`的表述；补**读工具**与**摘要能力**两条 |
| §5.4 注入渲染 | **删除** |
| §6 里程碑 | 用本文 §三 的 M0–M6 替换 |
| §9 数据流 | 末端由"→ 尾部段 → 下一轮可见"改为"→ 按需读取面" |
| §10 待拍板 | 3（注入面）作废；其余按本文 §七 重排 |
| §5.1/§5.2/§5.4 伪代码 | 三处 API 不存在（见 D-12），契约冻结阶段必须落成真名 |

---

## 二、落点与命名（用户口径 → 物理路径）

| 用户口径 | 逻辑键 | 建议物理落点（按数据根派生） | 依赖的既有事实 |
|---|---|---|---|
| `.seelex/session-xxx/worktable` | 会话 id | `<baseDir>/<boardRoot>/session-<sessionKey>/worktable.jsonl` | `baseDir = filepath.Dir(*storePath)`（`main.go:1589`）；`sessionDir` 派生（`sessionstore/sessionstore.go:1740-1745`） |
| `.seelex/session-xxx/teamwork` | 会话 id | 同目录 `teamwork.jsonl` | `moduleTeamwork` head + `TeamworkState`（`sessionstore/teamwork.go`） |
| `.seelex/project-known.md` | 项目 | `<baseDir>/<boardRoot>/project-known.md`（**渲染产物**） | `ProjectRecord`（`sessionstore/project_record.go`）+ `project_refresh`（`main.go:784-801`） |

`<boardRoot>` 与 `<sessionKey>` 的取值是**本方案唯一需要拍板的结构问题**（见 §七 Q1/Q2）：

- 若 `<boardRoot>` = 数据根（A 案）：`session-<sessionKey>` 必须与既有 `project-<hash>/session-<hash>` 两级**并存或合并**，
  且 `<boardRoot>` 若用 `ws-<nano>` 做分区，则**不可提交**（机器本地 ID，见 D-2）。
- 若 `<boardRoot>` = 项目根下的 `.seelex/`（B 案）：可提交，但要过 `ProjectScope`（`AGENTS.md §6`）
  并接受"两套文件写纪律并存"的代价（`AGENTS.md §8`）。

---

## 三、还有什么问题（D 段）

> 除用户已点名的三条之外，本轮自读又找到 16 条。每条给：现象 / 证据 / 风险 / 建议。

### D-1 `.seelex` 有三重身份，必须先钉死（**Confirmed**）

| 身份 | 事实 |
|---|---|
| (a) 运行时**数据根** | `-store` 默认 `.seelex/sessions`（`main.go:58`）；`baseDir = filepath.Dir(*storePath)`（`main.go:1589`）；CWD 相对；**跨项目共用**（项目按 `project-<hash>` 分片，`sessionstore/sessionstore.go:81`、`:1740`）；同级已有 `session-storage.json`、`workspace_index.json`、`scheduled-tasks.jsonl`（`main.go:973`） |
| (b) 用户口中的"**项目的** `.seelex`" | 在项目根里、可提交/可不提交 |
| (c) 本仓库里它**恰好就是**仓库根的 `.seelex/` | 实测内容：`baselines/ perf/ sessions/ sessions-json/ stage-limits/ workspace_index.json`（因为默认从仓库根启动 ⇒ (a) 与 (b) 重合） |

**风险**：(c) 的重合掩盖了 (a)≠(b)。一旦按 (b) 落盘（写项目根），
就同时踩 `AGENTS.md §6`（路径必须过 `ProjectScope`）与数据根的"单写者 + 原子写"两套纪律。
**建议**：把"物理落点"与"是否可提交"**解耦**——物理落点留在数据根（既有纪律白拿），
需要提交时另做一步**导出**（把挑选出的派生件、去掉机器本地信息后写到项目根 `.seelex/`）。

### D-2 `ws-<nano>` 是机器本地 ID，不可进可提交件（**Confirmed**）

`workspace/workspace.go:165`：`id := fmt.Sprintf("ws-%d", time.Now().UnixNano())`。
**风险**：提交后跨机器/跨克隆不可复现；重开工程会换 ID。
**建议**：可提交件里**只出现仓库相对路径与展示名**，工作区 ID 解码到"非提交"的旁文件。

### D-3 `.gitignore` 与可提交边界（**Confirmed**）

`.gitignore:15` 是 `.seelex/`（整体忽略，第 14 行是 `.seele/`）。
数据根里有 `lock.owner`（PID/程序名/主机名，实测 `.seelex/sessions-json/lock.owner`）、
会话正文/大输出/媒体、`workspace_index.json` ——**这些绝不能提交**。
**建议**：可提交面只放**显式挑选的少数派生件**（`project-known.md` + 可选聚合表），
不整体放开；把"能不能提交"做成用户开关，默认关。

### D-4 G5 两处断点（**Confirmed**，会直接卡死 `project-known.md`）

| 断点 | 事实 |
|---|---|
| ① 来源全落空 | 默认来源 `<root>/docs/modules`、`<root>/gui/module_dotting.json`、`<root>/seelex.project.md`（`sessionstore/project_record.go` 的 `NewProjectKnowledgeBuilder`）——三者**实测均不存在**；真实元数据在 `docs/gui/module_dotting.json`（**实测存在**） |
| ② **写键 ≠ 读键** | `RefreshProjectKnowledge` 用 `builder.Root()`（**绝对路径**）当 projectID 写（`store.SaveProjectRecord(builder.Root(), record)`）与读（`store.LoadProjectRecord(builder.Root())`）；而正常项目级读面 `main.go:681` 用 `store.LoadProjectRecord(store.Workspace())` = `ws-<nano>` ⇒ **写进去的永远读不到**。实测：全仓 `**/project-record.json` **零命中** |

**建议**：`project-known.md` 开工前**必须先修 G5**（来源可配置 + 统一 projectID 口径 = 工作区 ID），
并且**先写会红的用例**（`AGENTS.md §8.7` 红灯先行）。

### D-5 会话 id 当目录名 vs 现有 hash 分片（**Confirmed**）

现有布局刻意 hash：`projectDir = <root>/project-<hash(projectID)>`、`sessionDir = …/session-<hash(sessionID)>`
（`sessionstore/sessionstore.go:1740-1745`），`hash` 不可逆。
**风险**：改成字面 id 目录需要字符白名单/转义、长度上限（Windows MAX_PATH）、大小写不敏感文件系统安全，
且会与既有 `ListWorkspace`/`sessionMeta` 的"靠 head 元数据回读 id"路径形成两套读法。
**建议**：**目录仍走 hash，行内 `scope.session` 写字面 id**；"按会话区分"由**逻辑键**满足，
不由目录名满足。若用户坚持字面目录名，则必须配套一份 `id ↔ dir` 清单（否则枚举不出来）。

### D-6 ⚠️ 读取面与"绑定工程"边界冲突（**Confirmed**，本轮新发现，影响最大）

模型侧的读工具（`read_file` / `show_files` / `grep_search`）都声明为"**inside the bound project**"，
`show_files` 明确只收工程相对路径。
**风险**：若投影落在**数据根**（D-1 的 A 案）而数据根不在绑定工程内，
**模型根本读不到它** —— "按需读"这条设计直接失效。
**建议**：二选一——(i) 投影落点必须在**绑定工程根之内**；或 (ii) 补一个**专用读工具**
（`project_board_read`，按会话/项目聚合、有界、只读），别指望 `read_file`。
本仓库开发态之所以"看起来没问题"，是因为数据根恰好 = `<仓库根>/.seelex`（D-1(c)），属**巧合**。

### D-7 钩子扩展点是结构体字面量，不是可增挂的钩子表（**Confirmed**）

`ToolHookBridge.Hooks()` 返回 `&session.LoopHooks{ OnLLMComplete, OnToolStart, OnToolComplete, OnIterationComplete … }`
固定闭包（`application/core/tool_hooks.go:310` 起）；bridge 只有 `Bind` / `SetDiagnosticObserver`。
**plan §5.4 的 `toolHooks.SetAfterToolComplete(...)` 是不存在的 API（实测：该标识符全仓只在 plan.md:669 出现）。**
而 `OnToolComplete` 路径里**已经有** `service.refreshWorkTableFromSources()`（`tool_hooks.go` 完成段末）。
**建议**：把文件投影**挂在既有被动同步点之后**（内存投影与文件投影一次做完），
不要新造第二个工具完成回调（那是第二份实现）。

### D-8 `WorkItem.Kind` 是字符串，不是枚举常量（**Confirmed**）

`taskRecordToWorkItem` 里 `Kind: record.Kind`，取值 `plan / todo / task / subagent / tasklist`
（`application/core/work_table.go:30` 起）。
**plan §5.2 的 `WorkKindTodo` / `WorkKindTask` 不存在（实测：全仓只在 plan.md:521 出现）。**
**风险**：按不存在的枚举写映射 = 读侧"猜 schema"。
**建议**：映射表按**实际取值**写，并为每个 kind 显式声明"投影 / 不投影"。

### D-9 触发面覆盖不到"不用 todo 的会话"

`todo_list` 只在模型调用 `todo_*` / `task_add` / `task_complete` 时触发。
headless、子代理、角色（teammate）会话若从不调 todo，就**永不产出** worktable。
**建议**：配置保留 `task_complete` / `tool_complete` 档（plan §3.5 已有），
并在文档里写明"`todo_list` = 覆盖不到纯代码会话"。

### D-10 生命周期未绑定（plan 未覆盖）

会话被 `Router.Delete` 删除后，投影目录/文件成孤儿；子代理/角色会话会产生大量小文件。
**建议**：删除会话时同批回收投影；单项目投影文件数设上限；补一个显式清理器
（对齐 `seelebridge/worktree` 的 `Prune`「不在册 + 干净才回收」）。

### D-11 ⚠️ `project-known.md` 的"责任链"与既有 `ProjectRecord` 链撞车（**Confirmed**）

仓里已有一条**同义链**：`ProjectKnowledgeBuilder.scanModules()` 按
「模块元数据（权威）→ 模块文档目录（补缺）→ 手写说明」合并（`sessionstore/project_record.go`），
`RefreshProjectKnowledge` 再做「复用 / 重建 / 回退」三分支判定。
**风险**：把 `project-known.md` 做成"新的初始化/更新机制" = **第二份项目知识实现**（`AGENTS.md §8.1/§8.3`）。
**建议**：`project-known.md` 的定位是 **`ProjectRecord` 的人读渲染（读侧视图）**，
不是新的输入源；"注册能力"应注册**既有 `project_refresh` 的扩展档**（例如加一个渲染动作），
"责任链"直接用既有的三分支判定，不新写一条链。

### D-12 plan 内部三处 API/常量不存在（**Confirmed**）

| plan 里写的 | 实测 | 正确件 |
|---|---|---|
| `toolHooks.SetAfterToolComplete`（plan.md:669） | 全仓仅此一处 | 扩展 `Hooks()` 完成段 |
| `repairJSONLTail(path)`（plan.md:483） | 全仓仅此一处 | `truncateCrashTail(file)`（`sessionstore/jsonl_io.go`） |
| `WorkKindTodo` / `WorkKindTask`（plan.md:521） | 全仓仅此一处 | `record.Kind` 字符串取值 |

另外：§3.2 I3 说"未知 schema 跳过**并记一行日志**"，§5.1 的 `FoldProjectBoardRows` 只 `continue` 不记日志——
契约冻结阶段必须二选一。

### D-13 命名撞车三连（**Confirmed**）

`seelex.project.md`（既有来源名，且实测不存在）、`project-record.json`（既有产物）、
`project-known.md`（本方案新名）—— 同一件事三个名字。
**建议**：**一个来源名 + 一个产物名**；若保留 `project-known.md`，必须在文档与 README 里标明
"= `ProjectRecord` 的人读渲染，**不是输入**"。

### D-14 确定性：可提交件不能带时间戳（**Confirmed**）

`ProjectRecord.BuiltAt time.Time`（`sessionstore/project_record.go` 结构体首段）会随每次刷新变化。
**风险**：直接渲染进 `.md` ⇒ 每次刷新都产生 diff，可提交件变噪音。
**建议**：可提交件剔除 `BuiltAt` 与绝对路径；时间戳移入"非提交"的旁文件。

### D-15 多进程下投影可能交错（**Confirmed 依据 + Hypothesis 后果**）

`limits.runtime.allow_multi_process: true` 时进程内锁不跨进程（`config/seelex.yaml:236-248` 明写代价）。
`truncateCrashTail` 只解决"末尾半行"，**不解决"两进程交错追加"**。
**建议**：显式声明"投影要求单进程数据根"，或对追加引入跨进程文件锁（成本要写清）。

### D-16 token 论断要留痕，不要宣称"零 token"

用户口径是"不注入 ⇒ 不额外消耗 token"。严格地说：**投影内容**确实不进上下文（C2）；
但**被动技能的目录条目**（name + description）会进 system 前缀——这是一次性、稳定、可缓存的增量
（`application/core/prompt_layer/skill_catalog.go` 的 `RenderSkillCatalog`：只渲染 name/description、
按 name 排序、插件不变则字节不变）。
**建议**：验收如实记录"单轮增量 ≈ 0，只余技能目录的固定增量（几十 token 量级、可缓存）"，
并**实测一次**写进文档，别用"零 token"这种会被打假的措辞。

### D-17 投影不得携带敏感内容

任务标题/里程碑内容可能含 secrets 或本机信息。
**建议**：一条可测规则 —— 投影行只放 ID、项目相对路径、状态、标题（有界截断）；
单测断言"不出现绝对路径 / 用户名 / `config/accounts.yaml` 类字面量"。

---

## 四、里程碑拆分（M0–M6）

> 顺序事实：`depends_on` 是唯一依据。M0 是屏障；M1 与 M5a 可并行；M6 收口在最后。

```text
M0 契约冻结
 ├──> M1 存储面 ──> M2 投影+同步器 ──> M3 钩子+开关
 │                    └────────────> M4 按需读取面
 └──> M5 项目摘要（M5a 修 G5 可并行；M5b/M5c 依赖 M1）
                                        M3 + M4 + M5 ──> M6 收口
```

| 里程碑 | 交付物 | 验收判据（可测） | depends_on |
|---|---|---|---|
| **M0 契约冻结** | 修正后的口径写进 `docs/arch/seelex-project-board.md`（新）；`sessionstore/README.md` 与 `application/core/README-work-table.md` 增节；plan.md 按 §1.2 清单就地修订 | 评审通过；文档标注「规划」；plan.md 里三处不存在的 API/常量（D-12）已换成真名 | — |
| **M1 存储面** | `sessionstore/project_board.go`（行信封、`FoldProjectBoardRows`、路径派生、追加、独立锁、有界）；Router 读写两方法；删除绑定 | 追加幂等；残尾自愈；未知 schema 跳过且**有日志**；`-store` 换根路径随之变；路径不含绝对路径/用户名；空则**不建文件** | M0 |
| **M2 投影 + 同步器** | `application/core/project_board.go` 纯函数映射 + syncer（去抖/指纹/水位），挂**既有** `refreshWorkTableFromSources` 之后 | **一致性用例**：折叠读 == 活体 `buildWorkTable` 投影，逐行相等；删文件后重建结果相同（I1）；内容没变不写盘 | M1 |
| **M3 钩子 + 开关** | `limits.project_board.*`（默认 `enabled:false`、`hook:todo_list`）+ 装配门 | `enabled:false` ⇒ 零文件零注入；`hook:todo_list` 时只有 todo/task 族触发；钩子写盘失败不半写、不阻塞回合 | M2 |
| **M4 按需读取面（不含常驻注入）** | 被动技能条目（`plugins/<plugin>/<skill>/SKILL.md`，只在目录里宣告 name+description）+ 读面（`ListWorkspace` 聚合 + 专用读工具，见 D-6） | **回归**：开关前后装配前缀字节**完全一致**；请求尾部**不含**项目看板段；未要求读时上下文无该内容 | M1 |
| **M5 项目摘要 `.seelex/project-known.md`** | a) 修 G5（来源可配置 + 统一 projectID 键，红灯先行）<br>b) `ProjectRecord` → MD 渲染（确定性、去时间戳）<br>c) 注册能力（扩展既有 `project_refresh`）+ 责任链（复用既有三分支判定）+ 被动（仅提示词要求才做） | 写键=读键；两次渲染字节相同且不含时间戳/绝对路径/`ws-<nano>`；无显式调用不产文件；重建失败保留上一版本 | M0（a 可与 M1 并行）；b/c 依赖 M1 |
| **M6 收口** | 模块 README 增节 + 函数索引；端到端验收；token 增量实测留痕（D-16）；`.gitignore` 建议与可提交边界说明 | 端到端用例绿；文档与实现同步；可提交件不含 `lock.owner`/会话正文/媒体；交付含**删除清单**（`AGENTS.md §8.6`） | M3、M4、M5 |

---

## 五、测试用例

层级：**unit** = 纯函数/单包；**integration** = 跨包（store+core）；**e2e** = 真跑一轮；
**live** = 需要真 provider/真进程，标记为可选。用例名按仓内 `TestXxx` 约定。

### M1 存储面（`sessionstore`）

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T1.1 | `TestProjectBoardAppendIdempotent` | unit | 同一批行提交两次 ⇒ 只落一次；水位（文件内最大 Seq）不回退 |
| T1.2 | `TestFoldProjectBoardRowsLatestWins` | unit | 同 key 多行 ⇒ 只取 Seq 最大者；输出按 (kind,key) 稳定排序 |
| T1.3 | `TestFoldProjectBoardRowsUnknownSkipped` | unit | 未知 schema/kind 跳过、不报错、**且记一行日志**（与 I6 口径一致） |
| T1.4 | `TestProjectBoardCrashTailSelfHeals` | unit | 写入无换行的半行 → 下次追加前截断 → 折叠读拿不到半行 |
| T1.5 | `TestProjectBoardPathFollowsStoreRoot` | unit | 换 `-store` 根 ⇒ 路径随之变；不硬编码 `.seelex` |
| T1.6 | `TestProjectBoardPathNoAbsoluteNoUser` | unit | 路径与行内容不含绝对路径/用户名（D-17） |
| T1.7 | `TestProjectBoardLocksDistinct` | unit | 新锁与 message/teamwork 模块锁互异（对齐 `TestModuleLocksAreDistinct`） |
| T1.8 | `TestProjectBoardRowsBounded` | unit | 行数/单行字节越界 ⇒ **显式报错**（不静默截断） |
| T1.9 | `TestProjectBoardEmptyNoFile` | unit | 空行集 ⇒ 不建文件、不留空壳（I6） |
| T1.10 | `TestProjectBoardDeleteBindsSession` | integration | 删会话 ⇒ 投影同批回收（或标记为可回收孤儿） |

### M2 投影 + 同步器（`application/core`）

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T2.1 | `TestBuildProjectBoardRowsMatchesLiveProjection` | integration | **核心一致性**：折叠读结果与活体 `buildWorkTable` 投影**逐行相等**（同一输入跑两遍） |
| T2.2 | `TestBuildProjectBoardRowsNoJudgement` | unit | 状态/依赖只搬运：输入 `status=x` ⇒ 输出 `status=x`，无状态机、无二次判定（I3） |
| T2.3 | `TestBuildProjectBoardRowsKindMapping` | unit | `plan/todo/task/subagent/tasklist` 每个 kind 显式"投影/不投影"（D-8） |
| T2.4 | `TestProjectBoardSyncerDebounce` | unit | 同批多次触发 ⇒ 只落一次（`flush_idle_ms`） |
| T2.5 | `TestProjectBoardSyncerFingerprint` | unit | 内容指纹未变 ⇒ 不写盘 |
| T2.6 | `TestProjectBoardSyncerWatermark` | unit | 水位推进正确；重复提交是空操作 |
| T2.7 | `TestProjectBoardTeamworkMilestonePriority` | unit | 里程碑状态**运行态 `TeamworkState` 优先、声明态兜底**；`plan_state` 空/`closed` 正确 |
| T2.8 | `TestProjectBoardRebuildAfterDelete` | integration | 删文件 → 触发同步 → 重建结果与删前相同（I1） |
| T2.9 | `TestProjectBoardIsNotAnInput`（反例） | integration | **篡改投影文件 ⇒ 活体投影与领域判定不变**（I2 活体优先，防第二事实源） |

### M3 钩子 + 开关

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T3.1 | `TestProjectBoardDisabledNoArtifacts` | integration | `enabled:false` ⇒ 不挂钩子、不建文件、无任何读面（"关就是关"） |
| T3.2 | `TestProjectBoardTriggerTodoListOnly` | unit | `hook:todo_list` ⇒ 只有 `todo_*`/`task_add`/`task_complete` 触发；其余工具不触发 |
| T3.3 | `TestProjectBoardTriggerTaskComplete` | unit | `task_complete` 档只在该工具后触发 |
| T3.4 | `TestProjectBoardHookFailureNoHalfWrite` | integration | 写盘失败 ⇒ 不半写、不 panic、不阻塞回合 |
| T3.5 | `TestProjectBoardSyncerShutdown` | unit | syncer 的 goroutine/信号口可终止、不泄漏（`AGENTS.md §6`） |
| T3.6 | `TestProjectBoardHooksOnExistingSyncPoint` | integration | 投影挂在既有 `refreshWorkTableFromSources` 之后，**不新造第二个完成回调**（D-7） |

### M4 按需读取面

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T4.1 | `TestProjectBoardPrefixByteIdentical` | integration | **回归**：能力开/关，装配前缀字节**完全一致**（前缀缓存不变量） |
| T4.2 | `TestProjectBoardNoTailInjection` | integration | 请求尾部块**不含**项目看板段（点名纠偏 C2） |
| T4.3 | `TestProjectBoardSkillCatalogStable` | unit | 被动技能条目只含 name+description、按 name 排序、同输入字节相同、随插件切换才变（`RenderSkillCatalog` 同族） |
| T4.4 | `TestProjectBoardReadOnDemand` | e2e | 模拟"用户要求承接会话 X" ⇒ 读面返回该会话投影；**未要求时上下文不含该内容** |
| T4.5 | `TestProjectBoardReadWithinBoundProject` | integration | 读面受"绑定工程"边界约束；若落点在工程外 ⇒ 明确失败并指向专用读工具（D-6） |
| T4.6 | `TestProjectBoardTokenDelta`（live，可选） | live | 实测开关前后单轮 token 增量 ≈ 0（只余技能目录固定增量），读数写进文档（D-16） |

### M5 项目摘要（`.seelex/project-known.md`）

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T5.1 | `TestProjectRefreshWriteReadSameKey`（**红灯先行**） | integration | 先复现"写键≠读键"（D-4②）**会红**，修后绿：写用工作区 ID，读用同一键 |
| T5.2 | `TestProjectRefreshSourcesConfigurable` | unit | 来源可配置；默认对齐真实路径（`docs/gui/module_dotting.json`）；无来源 ⇒ 明确错误（不是静默空记录） |
| T5.3 | `TestProjectKnownRenderDeterministic` | unit | 同一 `ProjectRecord` 两次渲染 ⇒ 字节相同；**不含 `BuiltAt`/绝对路径/`ws-<nano>`**（D-2/D-14） |
| T5.4 | `TestProjectKnownPassiveOnly` | integration | **无显式调用**（提示词未要求）⇒ 不初始化、不更新、不产文件 |
| T5.5 | `TestProjectKnownReusesExistingChain` | unit | 责任链命中即处理、未命中传递；**且复用 `ProjectKnowledgeBuilder`/`RefreshProjectKnowledge`**；交付列删除清单（D-11） |
| T5.6 | `TestProjectKnownRebuildFailureKeepsLast` | unit | 重建失败 ⇒ 保留上一版本（可回退）；首建失败 ⇒ 显式报错 |

### M6 收口

| ID | 名称 | 层级 | 断言 |
|---|---|---|---|
| T6.1 | `TestProjectBoardGitignoreBoundary` | unit | 可提交件不含 `lock.owner`/会话正文/媒体（D-3） |
| T6.2 | `TestProjectBoardEndToEnd` | e2e | 真跑一轮：todo → 投影落盘 → 新会话按提示词读取 → 内容与权威源一致 |
| T6.3 | `TestProjectBoardMultiProcessPolicy` | live，可选 | `allow_multi_process:true` 下追加不交错，或明确记录"投影要求单进程数据根"（D-15） |
| T6.4 | `TestProjectBoardDocsConsistent` | unit | README/`docs/arch` 与实现同步；plan.md 已无 D-12 的三处假 API |

> 建议的**红灯先行**顺序（`AGENTS.md §8.7`）：先写 T5.1（复现 G5 断点）与 T2.9（防第二事实源）——
> 这两条是本方案最容易漂移的两个点，让它们先红。

---

## 六、待拍板

| # | 问题 | 选项 | 我的建议 |
|---|---|---|---|
| **Q1** | 物理落点 | A=数据根（既有纪律白拿，不可提交）/ B=项目根 `.seelex/`（可提交，需 `ProjectScope` + 两套写纪律）/ C=A 打底 + 显式导出到项目根 | **C**：落盘留数据根，"可提交"走导出。**但先回答 D-6**（读工具边界）——若落点在工程外，就必须有专用读工具 |
| **Q2** | 会话目录命名 | 既有 hash 分片 / 字面 session id / hash 目录 + 清单映射 | **hash 目录 + 行内字面 id**（D-5）；若必须字面目录，配套 `id↔dir` 清单 |
| **Q3** | `enabled` 默认值 | `false`（plan 现口径）/ `true` | 倾向 **`false`**：写盘虽免费，但会**覆盖不到**的会话产生噪声文件；先关后开 |
| **Q4** | "责任链"的确切所指 | (a) `ProjectRecord` 的「复用/重建/回退」三分支 / (b) `.seelex` 查找链（工程根 → 数据根 → exe 目录）/ (c) 来源合并链（元数据 → 文档 → 手写） | **(a)+(c) 复用既有**；(b) 只有在 Q1 选 C 时才需要，且应复用 `main.go:93` 的既有配置责任链口径 |

---

## 附：本轮证据索引（仅列本轮作者**亲自读过**的）

| 结论 | 证据 |
|---|---|
| 数据根系于 `-store`，`baseDir = dirname(-store)`；同级产物先例 | `main.go:58`、`:973`、`:1575`、`:1589` |
| json 后端根 = `<defaultPath>/sessions-json` | `sessionstore/sessionstore.go:81`（`Config.Normalize`） |
| 项目/会话目录 = `project-<hash>`/`session-<hash>`；`hash` 不可逆 | `sessionstore/sessionstore.go:1740-1745` |
| 项目级读面 `LoadProjectRecord(store.Workspace())` | `main.go:680-681` |
| `project_refresh` 用 `builder.Root()` 读/写（写键≠读键） | `sessionstore/project_record.go`（`RefreshProjectKnowledge`、`NewProjectKnowledgeBuilder`） |
| 默认来源 `docs/modules`、`gui/module_dotting.json`、`seelex.project.md` **均不存在**；真实元数据在 `docs/gui/module_dotting.json` | `sessionstore/project_record.go` + 实测 glob |
| 工作区 ID = `ws-<unixNano>` | `workspace/workspace.go:165` |
| `Router.SaveProjectRecord/LoadProjectRecord/ListWorkspace/Workspace/validateProjectID` | `sessionstore/sessionstore.go:248`、`:356`、`:892`、`:929`、`:945` |
| 项目级会话枚举 `SessionsOf(projectID)` | `sessionstore/session_granular.go:676` |
| 项目级展示元数据 = `project-*/session-meta.json` | `sessionstore/json_layout.go:248-256` |
| 残尾自愈件存在 | `sessionstore/jsonl_io.go`（`truncateCrashTail`） |
| 钩子 = `ToolHookBridge.Hooks()` 固定闭包；完成段已调 `refreshWorkTableFromSources` | `application/core/tool_hooks.go:310` 起 |
| 尾部注入位 `WorkTableTraceBlock` | `application/core/service_assembler.go:192` |
| 工作表格投影与 kind 取值 | `application/core/work_table.go:30` 起（`taskRecordToWorkItem`） |
| 被动技能目录只渲染 name+description、字节稳定 | `application/core/prompt_layer/skill_catalog.go`（`RenderSkillCatalog`）、`coordinator.go:126-160` |
| 配置先例（整块收回 / 空闲才触发 / 多进程代价） | `config/seelex.yaml:223-248` |
| `.seelex/` 被整体忽略 | `.gitignore:15` |
| plan 三处假 API/常量（全仓仅 plan.md 命中） | `plan.md:483`、`:521`、`:669` |
| 单一实现 / 文档放置 / ProjectScope / 红灯先行 / 删除清单 | `AGENTS.md §8`、`§2`、`§6`、`§8.7`、`§8.6` |

**未做（后续补证）**：
1. `read_file` 的"绑定工程"边界究竟是**每个会话的 workspace**还是**进程 CWD**——本轮只按工具自身的合约文本判断（D-6，**[Hypothesis]**，坐实方法：绑一个工程外目录跑一次读）。
2. 真 provider 下的 token 增量实测（T4.6，未跑）。
3. `-store` 指向其它盘符/UNC 时 `projectDir` 的派生行为（未实测）。
