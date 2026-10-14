# 项目看板（Project Board）：会话级任务/里程碑的派生投影与按需读取面

> 状态：**已冻结契约（M0 产物）**。实现进度见文末「实现状态」表；未落地部分一律标 **规划**。
> 日期：2026-10-15（M0 契约冻结）
> 上游设计：`docs/2026-10-13-seelex-project-board/plan.md`、`docs/2026-10-13-seelex-project-board/plan-review.md`
> 证据分级：**Confirmed** = 本轮逐点自读代码（带 `文件:行`）；**Hypothesis** = 推断。

**一句话**：把**已经存在**的工作表格投影（`buildWorkTable`）与 teamwork 计划/运行态投影
（`TeamworkPlan` + `TeamworkState`）按**会话**落成**项目目录**下的**追加式 JSONL 派生文件**
（`<项目根>/.seelex/board/`，另配人读件 `BOARD.md`），
由**既有**工具完成钩子被动落盘；模型**不**因此多出任何常驻上下文，只在**提示词要求时**
按需读取。项目摘要 `project-known.md` 是 `ProjectRecord` 的**人读渲染**，走既有责任链、被动生成。

---

## 1. 生态位与边界

| 层 | 新增/已有 | 职责 | 明确不做 |
|---|---|---|---|
| 事实层 | 已有 | `task.TaskRegistry`（`kind=todo/task` 的行）、`SessionRecord.Tasks`、`moduleTeamwork` head、`teamwork/events.jsonl` | —— |
| 投影层 | **新增** | 把事实层的**既有投影输出**映射成行，按会话追加落盘 | 不判定、不重算状态、不做 gate |
| 读取层 | **新增** | 让模型/人**按需**读到投影 | **不进常驻上下文**、不自动刷新 |
| 摘要层 | **新增（复用既有 `ProjectRecord` 链）** | `project-known.md` 的初始化/更新 | 不新增项目知识来源 |

**非目标**：不新增事实源；不做每轮上下文注入；不重写 todo/teamwork 状态机；
不新增第二套文件写实现（落点即使搬进项目目录，走的仍是 `jsonl_io.go` 那套
追加 + 残尾自愈 + 分片锁，**不是**另起一套写盘实现）。

### 1.1 分名分责（防误并）

| 名字 | 是什么 | 谁写 | 进上下文？ |
|---|---|---|---|
| `board_goal` / `board_team`（`metadata/*.json`） | 会话级**看板存档**（快照恢复用） | goal / teamwork 域 | ❌（`sessionstore/board.go:1-20` I7） |
| `ProjectRecord`（`project-record.json`） | 项目级**模块语义记录** | `project_refresh` 工具 | ✅（前缀 `project` 块） |
| `project-known.md`（本文） | `ProjectRecord` 的**人读渲染** | `project_refresh` 的渲染动作 | 按需 |
| `worktable.jsonl` / `teamwork.jsonl`（本文） | 会话级**任务/里程碑投影** | 工具完成钩子（代码） | 按需 |
| `scheduled-tasks.jsonl` | 全局**任务定义** | 调度器 | ❌ |

---

## 2. 不变式（写成可测的判据）

| # | 不变式 | 判据 |
|---|---|---|
| **I1 派生而非事实** | 删掉投影文件，系统功能不变，且能被重建 | 删文件 → 触发同步 → 折叠读结果与活体投影逐行相等 |
| **I2 活体优先** | 本会话读**活体投影**；文件只在活体给不出时兜底 | **反例用例**：篡改文件不得影响活体与领域判定 |
| **I3 不新增第二份判据** | 状态、分类、里程碑语义只由既有处产生 | `buildWorkTable` / `TeamworkPlan` / `TeamworkState` 是唯一判据处；新代码只做字段映射（`AGENTS.md §8.3`） |
| **I4 单写者 + 原子追加** | 一个**投影文件**只有一个写者（文件是项目级的，同项目多会话共用）；行级追加不重写整份；崩溃残尾视为未提交 | 复用 `sessionstore/jsonl_io.go` 的 `truncateCrashTail`；写者是进程内的同步器，锁按**文件路径**分片（数据根的 `lock.owner` 不覆盖项目目录，见 §9.3 R-3） |
| **I5 有界** | 读取行数、跨会话会话数、单行字节都有上限；截断显式标注 | 对齐尾部块既有口径 |
| **I6 空则退场** | 没有任务/里程碑 → 不建文件、不渲染、不留空壳 | 对齐 `sessionstore/board.go` 的退场语义 |
| **I7 不进模型上下文** | 投影**不得**进入 system/project 前缀，也**不得**进入请求尾部 | 回归用例：能力开/关，装配前缀字节**完全一致**；请求尾部**不含**项目看板段 |

---

## 3. 落点与命名（**本节的偏离即决策**）

物理落点是**项目目录**（该会话所属工作区的根），落在一层隐藏目录里：

```text
<项目根>/.seelex/board/worktable.jsonl      # 工作：todo / task 行（一个项目一份，多会话共用）
<项目根>/.seelex/board/teamwork.jsonl       # 编排：milestone / milestone_item 行
<项目根>/.seelex/board/BOARD.md             # 人读件（由折叠后的行确定性渲染）
<项目根>/.seelex/board/project-known.md     # 项目摘要的人读渲染（§6.1，同目录同生命周期）
```

- `<项目根>` = 该会话所属工作区的 `RootPath`
  （`WorkspacePort.SessionWorkspace(sessionID).RootPath`，`workspace/workspace.go:273`）。
  全仓只有**一处**解析：`application/core` 的 `Service.ProjectRootFor(sessionID)`
  （`application/core/project_board.go`）；**未绑定工作区的会话返回 `""` ⇒ 不落盘**
  （没有落点就没有投影，`Router.validateProjectRoot` 显式拒绝空根）。
- **文件名固定、目录层级固定**：不再有 `project-<hash(projectID)>/session-<hash(sessionID)>/`
  两层派生目录。会话维度写在**行内** `scope.session`（折叠读按 `(Session, Key)` 分桶），
  不写进路径 ⇒ 一个项目一份平铺文件，多会话共用。
- `BOARD.md` 不落 `at`、不渲染 `scope.project`：时刻不是看板要看的东西（每次追加都变 ⇒
  无意义 diff），`ws-<nano>` 是机器本地 ID（硬纪律 2）。

> **偏离说明（两次，都留痕）**
>
> - 相对 `plan.md §4.3`（写 `<baseDir>/<boardRoot>/…`）：2026-10-14 的实现把根钉在
>   `<jsonRoot>` 之下——"单写者 + 原子追加 + 残尾自愈 + 独立锁"这套纪律全部长在
>   `jsonRepository` 上，落在它的 `root` 下才是零新增写盘实现（`AGENTS.md §8`）。
> - 相对本契约 2026-10-14 版（写 `<jsonRoot>/project-board/project-<hash>/session-<hash>/`）：
>   2026-10-15 改钉**项目根**。那一版不可提交也不可归属——数据根在工程之外、目录名是 hash、
>   行内还带着 `ws-<nano>`。现在**落点即项目归属**：外部 agent（codex / claude code）直接读盘上的
>   `<项目根>/.seelex/board/BOARD.md` 就能接手。写盘纪律仍是同一份实现
>   （`jsonl_io.go` 的追加 + 残尾自愈 + **按文件路径分片**的投影锁），换的只是路径来源。

**落点的三条硬纪律**（投影落在项目目录里，它们从"将来导出时再说"变成**现行要求**）：

1. 不含 `lock.owner` / 会话正文 / 媒体 / 大输出；
2. 不含 `ws-<nano>`（`workspace/workspace.go:165` 是机器本地 ID）、绝对路径、本机用户名
   ——因此 `ProjectBoardScope.Project` 槽位**保留但生产路径不写**它；
3. 不含抖动时间戳（`ProjectRecord.BuiltAt` 会让每次刷新都产生 diff）；`BOARD.md` 不渲染 `at`。

`.seelex/` 在仓库级 `.gitignore:14` 的"运行时数据"段里 ⇒ 落点自带"不入库"，
投影与 `project-known.md` 都不进版本库；这也正是 Vue/Node 生态里 `.seelex` 这类目录的通行读法
（机器可重建的派生数据 vs 人要提交的源码分开放）。

---

## 4. 数据形状（冻结）

### 4.1 行信封

```go
type ProjectBoardScope struct {
	Project string `json:"project,omitempty"` // 保留槽位；**生产路径不写**（ws-… 是机器本地 ID）
	Session string `json:"session"`           // 字面会话 ID（落点是项目级平铺文件，会话维度只在行里）
}

type ProjectBoardRow struct {
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"`    // work | milestone | milestone_item
	Key     string            `json:"key"`     // 折叠键
	Seq     uint64            `json:"seq"`     // 文件内单调递增；水位 = 最大 Seq
	At      int64             `json:"at"`      // unix 秒
	Scope   ProjectBoardScope `json:"scope"`
	Payload map[string]any     `json:"payload"`
}
```

**为什么 `Payload` 用 `map[string]any` 而不是 `json.RawMessage`**：投影侧是纯字段映射，
不产生第二份序列化；`encoding/json` 对 map 的 key 做**字典序**输出，因此同一 map 两次
编码字节相同（渲染/导出的字节稳定性前提）。

### 4.2 折叠读（唯一读法）

```go
// FoldProjectBoardRows 把追加流折成「每个 Key 的最新一行」。
//   - 未知 schema / 未知 kind ⇒ 跳过并**记一行日志**（读侧不猜、不半读、不报错）；
//   - 残尾行在读取阶段已被丢弃；
//   - 删除语义：最新一行的 payload 带 "removed": true 时，该 Key **从结果中消失**
//     （tombstone；否则「条目被删掉」在 upsert 语义下永远消失不掉）；
//   - 输出按 (Kind, Key) 稳定排序。
func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow
```

**水位** = 文件内最大 `Seq`。写侧用它做幂等（同一批重复提交是空操作）；
读侧用它判断"活体投影的 revision 是否已落盘"（未落盘则读活体，I2）。

### 4.3 payload 形状（与既有 DTO 同形，不复制状态机）

- `kind=work` → `task / kind / status / assignee / dependencies / batch`（与 `WorkItem` 同义）。
  `payload.kind` 取**实际字符串**（`plan/todo/task/subagent/tasklist`，见
  `application/core/work_table.go:30`），**不新增枚举常量**。
- `kind=milestone` → `id / name / depends_on / required / status / content / plan_version / team_state`。
- `kind=milestone_item` → `id / milestone / role / depends_on / status / session_id`。
- 里程碑状态取数：**运行态 `TeamworkState.Milestones[id]` 优先**，计划里的
  `TeamworkMilestone.Status` 兜底；整队是否收口读 **`TeamworkState.State`**
  （空 = 未收口；`closed`）——`TeamworkPlan.State` 是 `TeamworkState` **结构体本身**
  （`sessionstore/teamwork.go:98-105` 与 `:151-172` 对照）。

---

## 5. API 契约（冻结）

### 5.1 `sessionstore`（存储面）

```go
const (
	ProjectBoardSchema       = 1
	ProjectBoardMaxRows      = 512  // 单次追加行数上限（越界显式报错）
	ProjectBoardWorkFile     = "worktable.jsonl"
	ProjectBoardTeamworkFile = "teamwork.jsonl"
	ProjectBoardMarkdownFile = "BOARD.md"         // 人读件（确定性渲染：不落 at、不落 ws-<nano>）
	ProjectBoardKnownFile    = "project-known.md" // 项目摘要的人读渲染（§6.1，同目录同生命周期）
)

// 落点目录：`<项目根>/.seelex/board`（唯一一处派生；文件名固定，会话维度只在行内）。
func ProjectBoardDir(projectRoot string) string

type ProjectBoardReadOptions struct {
	File           string // 必填：ProjectBoardWorkFile | ProjectBoardTeamworkFile
	ExcludeSession string // 跳过该会话（调用方自读活体时用）
	MaxSessions    int    // 跨会话枚举上限；<=0 用默认
	MaxRows        int    // 折叠后总行数上限；<=0 用默认
}

// 写：追加一批行。幂等（水位 ≥ 本批最小 Seq ⇒ 空操作并返回当前水位）；
// 空批不建文件（I6）；残尾自愈；单写者（**按文件路径分片**的投影锁 + 独立锁）。
// 返回新水位（文件内最大 Seq）。projectRoot = 会话所属工作区的根（落点
// `<项目根>/.seelex/board/`）；空 root 显式报错（没有落点 ⇒ 不落盘）。
func (router *Router) AppendProjectBoardRows(projectRoot, file string, rows []ProjectBoardRow) (uint64, error)

// 读：单会话单文件（会话维度在行内 scope.session，不在路径里）。返回原始行（已丢弃
// 残尾），折叠由调用方做（FoldProjectBoardRows 按 (Session, Key) 分桶）。
func (router *Router) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]ProjectBoardRow, error)

// 水位：文件内最大 Seq（0 = 尚无行）。写侧据此取批内起始 Seq（重启后仍单调）。
func (router *Router) ProjectBoardWatermark(projectRoot, file string) (uint64, error)

// 聚合读：一个项目内跨会话折叠读，只读、有界；bool = 是否截断（显式标注）。
func (router *Router) ReadProjectBoard(projectRoot string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error)

// 会话删除时回收：**按项目根**（不再按 projectID 派生——存储面不认识工作区，
// 解析器由装配侧经 Router.SetProjectBoardRootFor(func(sessionID) string) 注入；
// 回收因此挂在 Router.DeleteWorkspace 上，不在 jsonRepository.Delete 里）。
func (router *Router) ReclaimProjectBoard(_ /*projectID*/, sessionID string) error

// 人读件读回（BOARD.md，确定性渲染的字节）。
func (router *Router) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error)

// 项目摘要渲染件写/读（project-known.md，与看板同目录）。
func (router *Router) WriteProjectKnown(projectRoot string, data []byte) error
func (router *Router) ReadProjectKnown(projectRoot string) ([]byte, error)

// 落点解析器注入（装配侧：会话 → 项目根）；回收也因此不依赖存储面的工作区认知。
func (router *Router) SetProjectBoardRootFor(resolve func(sessionID string) string)
```

**锁**：新增**独立**的按路径互斥锁（不与 message / teamwork 模块锁共用），
不新增 `storageModule` 枚举（`sessionstore/module_heads.go` 的枚举—锁一一对应不变量因此不动）。

### 5.2 `application/core`（投影 + 同步器）

```go
// 写侧端口：工具完成后把「本会话可能变了」告诉同步器。只标脏 + 唤醒，不做 IO。
type ProjectBoardSink interface {
	OnToolComplete(name string, err error, sessionID string)
}

// 取数依赖：全部来自既有产物，不重算（I3）。
type ProjectBoardDeps struct {
	Store         ProjectBoardStore
	WorkTableRows func(sessionID string) []WorkItem                    // 既有 buildWorkTable 输出
	TeamPlanFor   func(sessionID string) *sessionstore.TeamworkPlan    // nil = 本会话无团队计划
	TeamStateFor  func(sessionID string) *sessionstore.TeamworkState   // 运行态投影（可 nil）
	ProjectRootFor func(sessionID string) string                       // 会话 → **项目根**（"" = 无落点，不落盘）
	Triggers      ProjectBoardTrigger
	FlushIdle     time.Duration
	Now           func() time.Time
}

type ProjectBoardStore interface {
	AppendProjectBoardRows(projectRoot, file string, rows []sessionstore.ProjectBoardRow) (uint64, error)
	ProjectBoardWatermark(projectRoot, file string) (uint64, error)
}

func NewProjectBoardSyncer(deps ProjectBoardDeps) *ProjectBoardSyncer
func (s *ProjectBoardSyncer) OnToolComplete(name string, err error, sessionID string)
func (s *ProjectBoardSyncer) Close() // 终止唯一 flush goroutine（可重复调用）

// 纯函数映射：语义只映射、不判定（I3）。seq 是本批起始 Seq，行内依次 +1。
func BuildProjectBoardRows(scope sessionstore.ProjectBoardScope, seq uint64, at int64,
	workItems []WorkItem, teamPlan *sessionstore.TeamworkPlan,
	teamState *sessionstore.TeamworkState) []sessionstore.ProjectBoardRow

// 触发装配表（不是散落的 if name == …）
type ProjectBoardTrigger struct{ /* 非导出 */ }
func TriggerFor(hookMode string) ProjectBoardTrigger
func (t ProjectBoardTrigger) Matches(name string) bool
```

**同步器语义**：`OnToolComplete` 只做「判触发面 → 标脏 → 唤醒唯一 flush goroutine
（信号口容量 1，latest-wins）」；`flush` 是**唯一**写盘路径，且必须在**独立 goroutine**
上跑（写盘失败不阻塞回合）。`flush`：

1. 取 `WorkTableRows` / `TeamPlanFor` / `TeamStateFor`；
2. `BuildProjectBoardRows` 映射成两块行集（work 行 → `worktable.jsonl`；milestone/milestone_item
   行 → `teamwork.jsonl`）；
3. 每块：`watermark = ProjectBoardWatermark(...)`；`seq = watermark+1`；行集为空 ⇒ 跳过（I6）；
4. **指纹去重**：内容（去掉 seq/at 后）与上次相同 ⇒ 不写盘；
5. 与上次 flush 的 key 集合做差 ⇒ 为消失的 key 补一行 `payload{"removed":true}`（tombstone）；
6. `AppendProjectBoardRows`；失败只记日志，不 panic、不阻塞回合。

### 5.3 落点拆分（回答"挂在哪"）

| 问题 | 落点 | 为什么不是别处 |
|---|---|---|
| 能不能有这块能力 | 配置 `limits.project_board.enabled` + 装配处（`main.go`） | 与 `limits.async_exec.enabled` 同款；关就是关 |
| 什么时候落盘 | **既有** `application/core/tool_hooks.go` 完成段 `refreshWorkTableFromSources()` **之后** | 这是唯一能看到"模型列了 todo"的地方，且已有被动同步点；**不新造回调** |
| 写到哪、谁能写 | `Router.AppendProjectBoardRows(projectRoot, file, rows)`（项目根下的平铺文件 + **按路径分片**的投影锁） | `AGENTS.md §6` 要求读写携带正确 project scope |
| 模型怎么看见 | **不在这里**（读侧按需，见 §6） | 上下文层**不注入**（I7） |

> `ToolHookBridge.Hooks()` 返回的是**固定闭包**（`tool_hooks.go:310` 起），
> bridge 只有 `Bind` / `SetDiagnosticObserver`。因此同步器从**装配根**经
> `core.Service` 的可选字段注入，完成段在既有 `refreshWorkTableFromSources()` 之后调用它。

### 5.4 触发装配表

| `hook` 取值 | 触发条件 | 代价 |
|---|---|---|
| `off` | 不触发（比 `enabled:false` 更细的一档） | 0 |
| **`todo_list`（默认）** | 工具名 ∈ `{todo_init, todo_add, todo_done, todo_status, task_add, task_complete}` 且成功 | **覆盖不到"从不调 todo"的会话**（headless/子代理/角色） |
| `task_complete` | 只在会话终态工具后触发 | 项目状态滞后到回合末 |
| `tool_complete` / `all` | 任何工具完成后触发（去抖 + 指纹去重） | 写盘次数最高 |

配置形状（`config/seelex.yaml` 的 `limits` 段，照抄 `async_exec` 的说明风格）：

```yaml
limits:
  project_board:
    enabled: false        # 整块收回（关就是关，无静默降级）
    hook: "todo_list"     # off | todo_list | task_complete | tool_complete | all
    flush_idle_ms: 2000   # 去抖：同一批次内多次触发只落一次
    # max_rows: 512 —— **不提供该键**（见 §9.1 D-2：单次追加行上限是存储面的硬不变量
    #                  ProjectBoardMaxRows，配置再放一个就是对同一次追加的第二份判据）
```

---

## 6. 读取模型（按需，**不注入**）

读面由三件事组成：

1. **"去哪读"**：一条**被动技能条目**（`plugins/<plugin>/<skill>/SKILL.md`）。只有
   name + description 进入 `## Available Skills` 目录（`RenderSkillCatalog`，
   `application/core/prompt_layer/skill_catalog.go`）——几十 token、字节稳定、
   随插件切换才变（可缓存）。**正文只在被激活后注入**。
2. **"怎么读"**：专用只读工具 `project_board_read` **仍然是必要的那一把**，但理由换了
   （2026-10-15 落点搬进项目目录后，"数据根在绑定工程之外"这半条不再成立）：
   - `workspace.ReadFile` / `ListTree` 把 `.seelex` **整个列为忽略目录**，这是有意的——
     默认数据根恰好也叫 `.seelex/`、可能就在工程根下，整体放开会把 `lock.owner`、会话正文、
     媒体一起放给模型。既不放宽忽略名单，就得有一条**专用**读法；
   - 这条读法要**有界 + 可归属**：两份 jsonl 是追加日志（I5 单文件 512 行），直接 `read_file`
     会把整份拖进上下文；读面按会话过滤、按行数截断并**显式标注** truncated。
   - 外部 agent（codex / claude code）**不走这条路**：它们直接读盘上的
     `<项目根>/.seelex/board/BOARD.md`（人读件）与两份 jsonl——这正是"搬到项目目录"的用意。
3. **"什么时候读"**：由**提示词**决定（"我要承接会话 X 的 worktable" / "我们项目现在是什么样"）。

**明确不做**：不把投影放进 system/project 前缀，**也不放进请求尾部**。

### 6.1 摘要模型（`project-known.md`）

| 要素 | 口径 |
|---|---|
| 注册能力 | 扩展既有 `project_refresh` 工具（`main.go:784-801`），**不是**新写第二把工具 |
| 被动 | **无显式调用 → 不产文件**；不在装配期构建 |
| 责任链 | **复用** `RefreshProjectKnowledge` 的「复用 / 重建 / 回退」三分支 与
`ProjectKnowledgeBuilder.scanModules()` 的「元数据（权威）→ 文档目录（补缺）→ 手写说明」 |
| 产物定位 | `ProjectRecord` 的**人读渲染**，不是输入源 |
| 落点 | `<项目根>/.seelex/board/project-known.md`（与看板同目录、同一条生命周期；写根同样由 `Service.ProjectRootFor(sessionID)` 解析，未绑定会话 **不落盘**） |
| 确定性 | 同一 `ProjectRecord` 两次渲染字节相同；**剔除 `BuiltAt`、绝对路径、`ws-<nano>`** |

---

## 7. 里程碑与验收（照抄 plan §5，含一处合并）

```text
M0 契约冻结
 ├──> M1 存储面 ──> M2 投影 + 同步器 ──> M3 钩子 + 开关 + 装配 ──> M4 按需读取面
 │                                     └──> M5 项目摘要渲染（与 M2 并行）
 └──>                                                  M4 + M5 ──> M6 收口
```

| # | 里程碑 | 交付物 | 验收判据（可测） | depends_on |
|---|---|---|---|---|
| **M0** | 契约冻结 | **本文**；`sessionstore/README.md` / `application/core/README-work-table.md` 增节（M6 补齐） | 真名 API（拒绝 plan 的三处假 API）；评审通过 | — |
| **M1** | 存储面 | `sessionstore/project_board.go`；Router 五个方法；删除会话时回收；**G5 两处断点修复（红灯先行）** | 追加幂等；残尾自愈；未知 schema 跳过**且记日志**；路径随**项目根**变（M7 起）；不含绝对路径/用户名；空则不建文件 | M0 |
| **M2** | 投影 + 同步器 | `application/core/project_board.go` | **一致性**：折叠读 == 活体投影，逐行相等；删文件后重建相同（I1）；反例：篡改文件不影响活体（I2） | M1 |
| **M3** | 钩子 + 开关 + 装配 | `config/seelex.yaml` + `main.go` 装配门 + 触发装配表 | `enabled:false` ⇒ 零文件、零读取面；`hook:todo_list` 只被 todo/task 族触发；写盘失败不阻塞回合；syncer 可终止 | M2 |
| **M4** | 按需读取面 | 被动技能条目 + `project_board_read` | **回归**：能力开/关装配前缀字节**完全一致**；请求尾部不含项目看板段 | M3 |
| **M5** | 项目摘要渲染 | `ProjectKnownMarkdown`（纯函数）+ 路由写面 + `project_refresh` 渲染动作 | 两次渲染字节相同且不含时间戳/绝对路径/`ws-…`；无显式调用不产文件；重建失败保留上一版本 | M1 |
| **M6** | 收口 | 模块 README 增节 + 函数索引；端到端用例；token 增量实测留痕；**删除清单** | 端到端用例绿；文档与实现同步；可提交件不含 `lock.owner`/会话正文/媒体 | M4、M5 |

### 7.1 G5 两处断点（M1 内修，红灯先行）

| 断点 | 现状（**Confirmed**） | 修法 |
|---|---|---|
| ① 默认来源全落空 | `NewProjectKnowledgeBuilder` 默认 `<root>/docs/modules`、`<root>/gui/module_dotting.json`、`<root>/seelex.project.md`；实测三者均不存在，真实元数据在 **`docs/gui/module_dotting.json`** | 默认 `MetadataPath` 对齐真实路径；来源保持可配置 |
| ② **写键 ≠ 读键** | `RefreshProjectKnowledge` 用 `builder.Root()`（**绝对路径**）读写；正常读面 `main.go:680-681` 用 `store.Workspace()`（`ws-<nano>`）⇒ 写进去的永远读不到 | 渲染件落到**项目根**：`main.go` 的 `project_refresh` 渲染动作取 `app.ProjectRootFor(sessionID)`（与看板落点同源；未绑定才退回本次扫描根），再 `store.WriteProjectKnown(projectRoot, …)`；**先写会红的用例**（`AGENTS.md §8.7`） |

---

## 8. 复用清单（禁止第二份实现）

| 需求 | **复用** | 不新写 |
|---|---|---|
| JSONL 追加 + 残尾自愈 | `sessionstore/jsonl_io.go` 的 `truncateCrashTail` | 第二份尾修复 |
| 落点目录派生 | `sessionstore.ProjectBoardDir(projectRoot)`（`<项目根>/.seelex/board`） | 第二套目录命名 / 自造 hash 目录 |
| 项目作用域写面 | `Router.withRepositoryAt` | 直连文件系统 |
| 会话枚举 | `Router.ListWorkspace` | 自造枚举 |
| 工作表格投影 | `application/core/work_table.go` 的 `buildWorkTable` | 重算状态 |
| 里程碑/工作项语义 | `sessionstore/teamwork.go` 的 `TeamworkPlan` / `TeamworkState` | 第二套状态机 |
| 项目知识链 | `ProjectKnowledgeBuilder` / `RefreshProjectKnowledge` | 第二份项目知识实现 |
| 工具完成同步点 | `application/core/tool_hooks.go` 完成段 | 第二个工具完成回调 |
| 被动技能目录 | `RenderSkillCatalog` | 第二条"去哪读"通道 |

## 9. 实现状态

> 2026-10-14 收口：M1–M6 全部落地。本节是**实现状态与证据**的单一处（README 增节见
> `sessionstore/README.md`、`application/core/README-work-table.md`）。
> 2026-10-15 落点搬迁（M7）：投影从数据根 hash 目录搬到**项目目录**
> `<项目根>/.seelex/board/`（flat、去 hash、一个项目一份文件、多会话共用），并新增人读件
> `BOARD.md`；`project-known.md` 同目录。

| 里程碑 | 状态 | 落点 |
|---|---|---|
| M0 契约冻结 | **已落地** | 本文 |
| M1 存储面 | **已落地** | `sessionstore/project_board.go` + `project_board_test.go`（T1.1–T1.11 + 聚合读）；G5 双修见 `project_record.go` + `project_refresh_key_test.go` |
| M2 投影 + 同步器 | **已落地** | `application/core/project_board.go`（映射 + 同步器 + 触发装配表 + 取数口）+ `project_board_test.go`（T2.1–T2.9 + 触发表 + I1/I2/I6 + Close 幂等）；`work_table.go` 抽出 `taskRecordsForSession` |
| M3 钩子 + 开关 + 装配 | **已落地** | 核心侧：`Service.SetProjectBoardSink`（`service_components.go` 的 `projectBoardPort`）+ `tool_hooks.go` 完成段（既有 `refreshWorkTableFromSources()` 之后）；装配侧：`seelexctx.ProjectBoardLimits`、`config/seelex.yaml`、`main.projectBoardSinkFor`、`seelebridge/runtime_project_board.go`（`TeamworkPlanFor` 窄端口） |
| M4 按需读取面 | **已落地** | `plugins/default/project-board/SKILL.md`（被动技能条目）+ `seelebridge/runtime_project_board_read.go` + `main.registerProjectBoardReadTool`（只在 enabled 时注册）+ `project_board_read` 入 ro 簇 |
| M5 项目摘要渲染 | **已落地** | `sessionstore/project_known.go` + `project_known_test.go`（T5.3–T5.6）+ `main.registerProjectRefreshTool` 的渲染动作 |
| M6 收口 | **已落地** | 本节 + 模块 README 增节与索引 + 端到端用例 + 交付说明（下节） |
| M7 落点搬迁 | **已落地** | 存储面：`sessionstore/project_board.go`（`ProjectBoardDir` / `projectBoardPath` / `validateProjectRoot` / 按路径分片锁 / `reclaimProjectBoard`）+ `project_board_markdown.go`（`BOARD.md` 确定性渲染）+ `project_known.go`（改吃 projectRoot）+ `sessionstore.go`（`Router.SetProjectBoardRootFor`；会话删除回收移入 `Router.DeleteWorkspace`）。核心面：`application/core/project_board.go`（`ProjectRootFor` 取代 `ProjectIDFor`；取 "" ⇒ 不落盘）。装配面：`main.go`（注入 `app.ProjectRootFor` + 读工具 schema 加 `project_root`）。读面：`seelebridge/runtime_project_board_read.go`。用例：`sessionstore/project_board_landing_test.go`（判据）+ core/main/seelebridge 三处签名同步 |

**验证读数（2026-10-14，本地 Windows，`CGO_ENABLED=0`）**

| 判据 | 读数 |
|---|---|
| 构建 / 静态检查 | `go build ./...` exit 0；`go vet ./...` 清洁；`gofmt -l` 对本次新增/修改文件无输出 |
| 存储面 | `go test ./sessionstore/ -count=1` ⇒ `ok`（含 T1.1–T1.11、聚合读、G5 双修、T5.3–T5.6） |
| 投影 + 同步器 + 完成段 | `go test ./application/core/ -run ProjectBoard` ⇒ 全部 PASS（14 条 M2/T3.x + 词表不变量） |
| 读取面 | `go test ./seelebridge/ -run ProjectBoardRead` ⇒ 4/4 PASS |
| 装配门 + I7 | `go test . -run ProjectBoard` ⇒ 6/6 PASS（关则零文件 / 未知名显式失败 / 真实存储面落盘 / 装配前缀字节一致 / 投影不进请求 / 端到端读回） |
| 工具分封 | `TestEveryRegisteredToolDeclaresGroups` ⇒ 71 个工具全部分封（含 `project_board_read`） |

**落点搬迁（M7）的验证读数（2026-10-15，本地 Windows）**

| 判据 | 读数 |
|---|---|
| 落点（红灯 → 绿灯） | `go test .../sessionstore -run ProjectBoard` ⇒ `ok`。红灯先行用例 `TestProjectBoardMustNotLandUnderDataRoot` 搬迁前 FAIL（「投影仍落在数据根的 hash 目录（…\sessions-json\project-board）」）；正面用例 `TestProjectBoardLandsInProjectBoardDir` 钉 `BOARD.md` + `worktable.jsonl` 同时出现在 `<项目根>/.seelex/board/` |
| 装配门 | `go test . -run ProjectBoard` ⇒ 6/6 PASS；`TestProjectBoardEnabledWritesArtifacts` 断言落点是 `<项目根>/.seelex/board/<固定名>`、**数据根零文件**、`BOARD.md` 在册 |
| I7（投影不进上下文） | 同批 PASS：`TestProjectBoardPrefixBytesIdentical`、`TestProjectBoardNeverReachesRequest`（写侧的会话按工作区绑定解析落点后重跑） |
| 端到端读面 | 同批 PASS：`TestProjectBoardEndToEndThroughToolFace`（读面按会话过滤读**同项目另一会话**的行） |
| 投影 + 同步器 | `go test ./application/core -run ProjectBoard` ⇒ `ok`（折叠读改成按 `(Session, Key)` 分桶后重跑） |
| 实测盘面 | 真实存储面写一次后 `<项目根>/.seelex/board/`：`BOARD.md` 371 B、`worktable.jsonl` 198 B、`teamwork.jsonl` 184 B；数据根下 `project-board/` **不存在** |

### 9.1 实现期的偏离（都是决策，不是漂移）

| # | 契约写法 | 实现 | 为什么 |
|---|---|---|---|
| D-1 | §4.3 落点写 `<baseDir>/<boardRoot>` | 钉在 `<jsonRepository.root>/project-board/...`（**已被 D-5 取代**） | "单写者 + 原子追加 + 残尾自愈 + 独立锁"这套纪律全部长在 `jsonRepository` 上，落在它的 `root` 下才是零新增写盘实现 |
| D-2 | §5.4 配置块含 `max_rows: 512` | **不提供**该键 | 单次追加的行数上限是存储面的硬不变量（`sessionstore.ProjectBoardMaxRows`）。配置再放一个可调上限，就是对同一次追加的第二份判据（配置说 1000、存储说 512，用户只看到"没生效"）。出厂档注释里写明了这条 |
| D-3 | §5.4 未指名 hook 取值拼错的后果 | 装配期**显式启动失败** | `TriggerFor` 对未知档名按默认档兜底（"配置写错了退化成默认"）是安全的读法，但"开了却没生效"必须能被看见：`main.projectBoardHookMode` 用 `core.ProjectBoardHookModes()`（词表唯一来源）校验，拼错即报错 |
| D-4 | §6 只说"技能条目 + 只读工具" | 技能条目**静态**存在（随插件），工具**按开关注册** | 技能条目只有 name+description 进目录，是"去哪读"的目录行（静态 ⇒ 前缀字节与开关无关，I7 的字节回归因此成立）；真正的读取面是工具，关着时它根本不存在（M3 的"零读取面"） |
| D-5 | §3 落点写 `<jsonRoot>/project-board/project-<hash(projectID)>/session-<hash(sessionID)>/` | 改钉**项目根**：`<项目根>/.seelex/board/`（flat、去 hash、一个项目一份文件、多会话共用）+ 人读件 `BOARD.md` | 旧落点**不可提交也不可归属**：数据根在工程之外、目录名是 hash、行内还带 `ws-<nano>`。落点即项目归属之后，外部 agent（codex / claude code）直接读盘上的 `BOARD.md` 就能接手，且 `.seelex/` 已被 `.gitignore:14` 忽略 ⇒ 派生数据自动不入库。写盘纪律仍是同一份实现（追加 + 残尾自愈 + 按路径分片的投影锁），只换了路径来源；代价是**数据根的 `lock.owner` 不再覆盖**这份投影（见 R-3） |

### 9.2 交付说明（可提交边界 + 删除清单）

**可提交件**：源码、用例、`config/seelex.yaml` 的开关块、本文、模块 README 增节、
`plugins/default/project-board/SKILL.md`。**不入库**：投影文件、`BOARD.md` 与 `project-known.md`
——它们落在 `<项目根>/.seelex/` 下（`.gitignore:14` 的"运行时数据"段已忽略 `.seelex/`），
且行内**不写** `ws-<nano>`（`scope.project` 生产路径不写）⇒ 落点自带"不含本机 ID"。

**删除清单（`AGENTS.md §8.6`）**：本次交付**未删除任何代码/文件**。为避免第二份实现而
**有意不写**的清单：

| 不写的东西 | 复用了什么 |
|---|---|
| 第二套 JSONL 追加 / 残尾修复 | `sessionstore/jsonl_io.go` 的 `truncateCrashTail`、`appendEventLogLine` |
| 第二套目录命名 / 锁枚举 | `ProjectBoardDir(projectRoot)`（`<项目根>/.seelex/board`，**固定名、不派生 hash 目录**）；投影自带按路径分片的独立锁（**不**新增 `storageModule` 枚举） |
| 第二份工作表格判据 | `buildWorkTable`；按会话取 task 快照的判据抽成 `taskRecordsForSession`（打点块与看板共用一份） |
| 第二套里程碑状态机 | `TeamworkPlan` / `TeamworkState`（运行态优先、声明态兜底） |
| 第二个工具完成回调 | 既有 `tool_hooks.go` 完成段（`refreshWorkTableFromSources()` 之后） |
| 第二条"去哪读"通道 | `RenderSkillCatalog`（技能目录）+ 一把专用只读工具 |
| 第二份项目知识实现 | `RefreshProjectKnowledge` / `ProjectKnowledgeBuilder` |

### 9.3 遗留风险与待办（不进本轮交付，明写以免误读）

| # | 风险 | 现状与缓解 |
|---|---|---|
| R-1 | **单文件 512 行上限**：M2 的指纹去重是"内容一变就整份重盖 seq"，长期会话的 `worktable.jsonl` 会触顶，之后每次 flush 都因越界被存储面**显式拒绝**（不静默写坏，但也确实停止更新）。M7 之后更紧：文件是**项目级**的，同项目多会话共用一份 ⇒ 行数按项目累加而不是按会话。 | 越界只记日志、不影响回合；`limits.project_board.enabled` 出厂关闭。待办：立"投影压实 / 轮转"条目，或把 512 明确为**读侧窗口**（保留最近 N 条活体行）。 |
| R-2 | **活体 task 分区口径**：`WorkTableRowsFor` 对"视图/活跃会话"读实时注册表（`""` 分区）。全链路 harness 里同一次 todo 落在会话分区，视图分区为空 ⇒ 该现场下投影为空（本期端到端用例因此只钉读侧）。 | 生产路径下视图会话的行确实在 `""` 分区（既有打点块同一条判据）。待办：补一条"视图会话行落在 `""` 分区"的现场用例，把这条假设钉住。 |
| R-3 | **多进程共用项目根**：投影是进程内单写者（按路径分片的投影锁不跨进程），且 M7 之后它落在**项目目录**里——**数据根的 `lock.owner` 不再覆盖**这份投影。 | 与既有口径一致：`limits.runtime.allow_multi_process` 默认关；开了之后一致性由使用者负责（契约 §2 I4 只承诺单写者）。同一项目根被两个数据根（两个进程）同时投影时，行内 `scope.session` 仍能区分归属，但**不承诺**交错追加的先后 |
| R-4 | **技能条目静态存在**：能力关着时，技能目录里仍有 `project-board` 一行（正文不会被注入）。 | 有意（D-4）：前缀字节与开关无关是 I7 的可测形式；代价是"关着时也有一条几十 token 的目录行"。 |

