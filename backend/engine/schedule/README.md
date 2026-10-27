# schedule 域（backend/engine/schedule）

## 生态位

承载 seelex 的定时/周期任务 actor：标准库 time.Ticker 驱动单循环 goroutine，
任务两类（command 白名单命令 / prompt 复用 agent 会话），排期两种：

物理位置：2026-10-16 按
[plan.md](../../../docs/2026-10-15-architecture-reshape/plan.md) §2.12.1 从
`seelebridge/scheduler/`（包名 `scheduler`）整包归位到这里，包名改为 `schedule`；
同一片的持久化面已先拆出去（`FileStore` → `store/schedule`、端口 → `contract`、
记录 → `contract/dto`），本包只认端口。
周期重复（minute/hour/day/week/month 或固定 Interval）与一次性定时（RunAt）。

宿主转发面：`Runtime` 的 9 个公开方法（`RestoreScheduledTasks` / `ScheduleTask` / `UpdateScheduledTask` /
`CancelScheduledTask` / …）在 M5-R11-schedule 片从装配层 `ports.go`（该文件已删除）原样搬到该包的
`runtime_schedule.go`，只做 nil 守卫 + 逐行委托，**判据与状态全在本包**。

任务**定义**落在一个**全局** JSONL 里（默认 `<store>/scheduled-tasks.jsonl`，
见 `store/schedule/`）：一行一条变更、append-only、不按项目分区也不按会话分片。
触发产生的**会话记录**不在这里——那走会话自己的存储与读写纪律。

主要调用方：
`ports.go`（调度器端口）、`main.go`（白名单、执行器与 `ScheduledTasksPath` 装配、
冷启动 `RestoreScheduledTasks`）。

## 职责与非职责

- 职责：任务登记/校验/编辑/排期/取消/快照、任务定义的全局 JSONL 持久化与冷启动
  重建、白名单命令执行（argv 直传、环境清洗、超时）、prompt 任务委托执行器。
- 非职责：会话执行本身（归 session/agent）、命令的 shell 语义、**触发产生的
  会话正文**的落盘（归 store；本域只给执行器"新建会话 + 装配工作区"
  这条指令，不碰任何存储实现）、按项目/会话分区的任务副本（任务定义只有全局
  一份）。

## 与其它域的关系

```mermaid
flowchart LR
    RUNTIME["runtime/ports.go"] --> ST["scheduler.State<br/>单循环 goroutine（time.Ticker）"]
    MAIN["main.go：白名单 / 执行器 / JSONL 路径装配"] --> ST
    ST --> STORE["contract.ScheduledTaskPersistence<br/>store/schedule.FileStore：register / cancel 变更行"]
    ST --> T1["command 任务<br/>白名单 argv 直传 + 环境清洗 + 超时"]
    T1 --> SEC["security.ScrubEnvironment"]
    ST --> T2["prompt 任务"]
    T2 --> EXEC["PromptExecutor 闭包"]
    EXEC --> APP["application：绑定会话投递，或新建会话 + 工作区装配"]
    APP --> SESS["会话记录按会话存储纪律落盘（项目作用域由绑定决定）"]
    ST --> SNAP["状态快照 + schedule.registered / cancelled / fired 事件"]
```

## 排期状态

```mermaid
stateDiagram-v2
    [*] --> Registered: 登记（sch.registered 事件）
    Registered --> Waiting: 计算下次触发时间
    Waiting --> Fired: 到达触发点（sch.fired 事件）
    Fired --> Waiting: 周期任务（hour / day / week / month / Interval）
    Fired --> Done: 一次性任务（RunAt）
    Registered --> Cancelled: 取消（sch.cancelled 事件）
    Waiting --> Cancelled: 取消
    Done --> [*]
    Cancelled --> [*]
```

`month` 是日历月：月末日期自动钳制（如 1-31 加 1 月 → 2-28/29）。

### 周期锚点（开始时间 / 星期几）

周期有两个口径，由 `ScheduledTaskSpec.StartClock` / `StartWeekday` 决定：

| 口径 | 触发时刻 | 例（2026-10-07 星期三 10:00 创建） |
|---|---|---|
| **锚点**（给了 `HH:MM`） | 严格晚于 now 的第一个锚点墙钟 | 每天 09:00 → 10-08 09:00；每周一 09:00 → 10-12 09:00 |
| **滚动**（没给锚点） | `now + 周期`，即"每个周期按当前时间" | 每 1 天 → 明天 10:00 |

锚点只对有"几点"可言的单位（day / week / month）开放；minute / hour 是子日周期，
给了就报错（不静默忽略——静默吞掉锚点会让「每天 09:00」悄悄退化成「创建时刻起每 24 小时」）。
星期用 ISO（1 = 周一 … 7 = 周日），只归周周期、且必须与 `HH:MM` 同时给。
`9:00` 这类短写接受并归一成 `09:00`。锚点的墙钟含义由
`nextScheduledAt` → `advanceToAnchor` / `anchorOnDay` / `isoWeekday` 实现。

### 触发落点（会话与工作区）

prompt 任务的落点只有一条判据（`PromptExecutor` 契约，实现在装配根）：

| `ScheduledTaskSpec` | 落点 |
|---|---|
| `SessionID` 为空（**默认**） | **新建一个会话**发起；`WorkspaceID` 非空时先把新会话装配到该工作区（项目绑定 + 按会话工具根） |
| `SessionID` 非空 | 投递到那个既有会话（显式绑定） |

每次触发把实际落点写进状态的 `LastSessionID`，面板与冒烟据此指认"跑到哪个会话去了"。
新建会话在**后台**跑，不切用户正在看的视图指针；它自己的会话记录照常按会话存储纪律落盘。

### 触发装配（权限 / 插件 / 执行参数）

任务定义里还带几格**装配**，触发时落到那次会话/那一轮（实现见
`application/assembly/service_scheduler.go` 的 `AssembleScheduledRun`）：

| 字段 | 口径 |
|---|---|
| `PermissionTier` | 空 = **默认 full access**；非空必须是 `dto.PermissionTierIDs` 之一（未识别显式拒绝）。落到这次会话的档位槽 + 执行门 + 审批自动放行，并按会话级设置落盘 |
| `Plugins` | 空 = 不覆盖（继承宿主当前激活插件）；非空经 `dto.NormalizePlugins` 归一（去空白、重复显式拒绝、超上限显式拒绝），触发时带进这一轮的执行 ctx |
| `ReasoningEffort` | 思考强度（provider wire 词表，经 `dto.NormalizeWireReasoningEffort` 归一；未识别显式拒绝，含账号配置侧的 `"session"`）。**空 = 未声明**：默认值由装配层按默认档位的读数落定（`prompt.DefaultEffortLevel`，见下）。触发时按会话记一笔，在该会话的**回合起点**下发一次（账号池是进程级共享的，只能这样按会话生效） |
| `LoopLimit` | 循环次数（>= 1；负值显式拒绝）。**0 = 未声明**：默认值同上由装配层落。触发时钉在**那个会话自己的引擎**上（`contract.SessionLoopLimitPort`），不改进程默认值、不动别的会话 |

默认值为什么不在这一层落：默认参数 = **默认档位（`prompt.DefaultEffortLevel` = high）的两条
读数**（wire `high` + `384` 轮），而档位表属于 `application/prompt` 这一域。调度器只判"什么
算一个合法取值"，装配层在**创建/编辑那一刻**把默认填进定义（面板上读到的值与触发时用的值同源），
冷启动读回的老定义（没有这两格）由装配层在触发时按同一份判据兜底。

为什么权限的默认是 full 而不是主会话的 manual：定时任务在后台跑，**没人在审批弹窗上
点"同意"**——默认手动等于每次触发都卡死在一次永远没人回答的审批上。这条默认只落在
这次任务新建的会话上，不改进程默认档位、不影响其它会话。

插件装配走的是 teammate 回合的**同一条原语**（`seeltools.WithRolePlugins` → 工具面
每轮从 ctx 现算，`PluginFace` 收口）：不切宿主全局激活插件、不在任何句柄上缓存"当前
装配"（缓存在并发回合里会被后写的那份覆盖）。边界与 `runtime_role_plugins.go` 一致：
装配只回答"能用哪些能力包"，**不放宽权限面**；主代理的**插件正文层**仍来自宿主当前
激活插件，ctx 装配管的是这一轮的工具面与技能目录。

装配名不存在时应用层在**创建/编辑**就拒绝（未知插件名显式拒绝，不静默忽略）——
留到触发时才发现，就只剩一次"这一轮什么工具都没有"的失灵。

## 核心实现

- `State`：自带锁的 actor；ticker 循环 `tick` 找出到期任务，独立 goroutine
  执行（running 标志防重叠），状态快照只读外发。
- `Schedule`：校验周期下限/周期单位（`period_unit`）+ 数值/锚点（`start_clock` /
  `start_weekday`，搭配非法即拒）/命令白名单/prompt 执行器装配后创建任务；周期表达支持
  minute/hour/day/week/month，month 为日历月（`addCalendarMonths` 月末钳制），无单位时回退
  `Interval` 秒级固定周期；`RunAt` 非零时创建一次性定时任务（要求晚于当前时间，创建即启用，
  执行后自动停用并清除下次排期，记录保留供面板查看）。
- `Update`：**整体替换**既有任务的定义（PUT 语义：面板上是什么，任务就是什么），
  ID 不变、运行账目保留（`RunCount` / 上次结果 / 上次落点），下次运行按新定义重算。
  校验与创建**共用** `normalizeSpec` 这一份判据——不存在"创建时拦得住、编辑时漏得过"。
- `normalizeSpec` / `applyDefinition`：前者是"什么算一个合法任务定义"的唯一实现
  （trim + 锚点归一 + 周期搭配 + 最小周期 + 白名单/执行器 + 一次性语义 + 装配归一：
  档位空落 full、插件经 `dto.NormalizePlugins`、思考强度经 `dto.NormalizeWireReasoningEffort`、
  负的循环次数显式拒），后者把归一
  结果写进任务本体与状态快照（创建、编辑、冷启动重建三处共用，避免"编辑后某几格还是旧值"）。
- `contract.ScheduledTaskPersistence` / `store/schedule.FileStore`（2026-10-16 起；
  plan §2.12.1 的持久化面拆分）：任务定义的**全局 JSONL**。一行一条变更
  （`dto.ScheduledTaskRecord`：`deleted` 区分登记与取消墓碑），以 `O_APPEND` 追加
  一整行；`Load` 按 ID 后写覆盖先写重放，丢弃墓碑、读不动的行与未收尾的残尾。
  只存**定义与启用状态**，不存上次结果/日志尾部（那是运行期展示面，命令输出
  可能含敏感内容，不该因为"重启后想看见上次结果"而落盘）。
- `Restore`：冷启动重建。周期任务重算下次运行时间（**不追补**停机期间错过的
  触发点）；停用的恢复为停用；一次性任务时刻已过则不恢复；命令不在白名单、
  周期非法、执行器未装配等逐条跳过（返回 恢复数/跳过数），一条坏记录不挡住其余。

## 数据流

装配根启动时 Restore（全局 JSONL → 内存排期）→ Schedule/`Update`/`CancelTask`
（内存变更 + 追加一行到全局 JSONL，写失败即认定该次变更不成立）→ start
（惰启动 ticker）→ tick → executeTask（周期任务下次运行按 `nextScheduledAt`
计算；一次性任务执行后停用）→ 状态回写 + observe（通知 application 投影）。

## 依赖方向

允许依赖：`contract` / `contract/dto`、`backend/limit/gate`（沙箱命令原语）、
`internal/winhide`（隐藏子进程控制台）。禁止依赖：`seelebridge` 根包及其它域、
`store`（持久化只经 `contract.ScheduledTaskPersistence` 端口，实现在 `store/schedule`）、
`application/*`（状态回写走 `SetObserver` 回调，不反向 import）。

## 并发、存储、安全

单循环 goroutine + 每任务独立执行 goroutine；环境变量经
`security.ScrubEnvironment` 清洗；白名单命令 argv 固定直传不经 shell。

JSONL 是 append-only 单写者通道（`store/schedule.FileStore` 的 `mu` 串行化）：崩溃只会留下未收尾的
残尾，读侧按"未提交"跳过；登记/取消在写失败时**不改内存**（登记失败 = 没登记，
取消失败 = 没取消），避免"面板与磁盘劈叉"。

## 扩展方式

新增任务类型：扩展 `Schedule` 校验与 `executeTask` 分支。
换持久化介质：实现 `contract.ScheduledTaskPersistence` 并改装配根注入的实现（调度器只认端口）。
新增落点语义：改 `PromptExecutor` 契约并在装配根实现，不在调度器里按任务名分支。
可编辑字段的扩展：加进 `normalizeSpec` 与 `applyDefinition`（判据与状态投影各一处），
不要在 `Update` 里另开一段赋值。

## Review 指南

- 同一任务是否可能重叠执行；停机是否等待运行中任务（`schedulerShutdownWait`）。
- 任务定义是否仍然是**全局一份**：任何"按项目/会话分文件或分片"的改动都越界。
- 恢复路径与运行期是否共用同一套判据（周期校验、锚点、一次性过期），有没有
  出现第二份"什么算合法"的实现。
- 编辑是否只走 `normalizeSpec` + `applyDefinition`：绕开它们半路赋值，就会出现
  "状态快照和定义不一致"（面板显示旧值、排期按新值跑）。
- 触发落点是否只有一条判据（sessionID 空 = 新建会话）；新建会话有没有顺手改掉
  用户当前视图或全局工程根。
- 装配有没有越界：权限档位只落**那次会话**（不改进程默认），插件只进**这一轮 ctx**
  （不切宿主全局激活插件、不缓存），思考强度只按那次会话下发一次、循环次数只钉在那个
  会话的引擎上（都不改进程默认值）；未知插件名 / 未识别强度 / 负的循环次数是否在
  创建/编辑就被拒。

## 测试与验证

```text
go test ./backend/engine/schedule/... -count=1
```

- `schedule_test.go`：排期/锚点/白名单/重叠执行/执行器落点参数/编辑（整体替换、
  与创建共用校验、ID 不变、账目保留）/装配（默认 full、插件归一、非法显式拒绝）/
  执行参数（空 = 未声明由装配层落默认、声明值归一、非法取值显式拒绝）。
- `global_jsonl_test.go`：经 `State` 的全局单文件集成面——冷启动重建、取消墓碑、逐条跳过判据、
  编辑追加同 ID 定义行后的恢复、装配随定义恢复、残尾丢弃。

## 文件与函数索引

- `schedule.go`
  - `type State struct`：定时任务的 actor 资源（自带锁，读写即消息进出；存储经 `contract.ScheduledTaskPersistence` 注入）。
  - `func NewState() *State`：构造调度器状态（base ctx 用于停机时取消运行中任务）。
  - `func NewStateWithStore(store contract.ScheduledTaskPersistence) *State`：构造带全局 JSONL 任务定义的调度器状态。
  - `func (s *State) Start()`：惰启动 ticker 循环（首次创建任务时调用；重复调用幂等）。
  - `func (s *State) Schedule(ctx context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error)`：校验入参并创建任务（创建后立即排期；observer 通知投影）。
  - `func (s *State) Update(ctx context.Context, id string, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error)`：用一份新定义覆盖既有任务（ID 不变、运行账目保留、下次运行重算）。
  - `func (s *State) normalizeSpec(spec ScheduledTaskSpec) (normalizedSpec, error)`：校验并归一创建/编辑入参（含装配：档位空落 full、插件经 dto.NormalizePlugins），是"什么算一个合法任务定义"的唯一一份实现。
  - `func (t *task) applyDefinition(definition normalizedSpec, nextRun time.Time)`：把归一后的定义写进任务本体与状态快照（创建、编辑、冷启动重建共用）。
  - `func scheduledPermissionTier(raw string) (string, error)`：归一任务声明的权限档位（空 = 默认 full access，未识别显式拒绝）。
  - `func (s *State) CancelTask(id string) error`：取消并移除任务（运行中的执行不受影响，写墓碑行）。
  - `func (s *State) Restore() (int, int, error)`：从全局 JSONL 读回任务定义（冷启动重建），返回（恢复数, 跳过数）。
  - `func (s *State) Snapshot() []ScheduledTaskStatus`：返回任务只读快照（按 ID 排序）。
  - `func (s *State) SetPromptExecutor(executor PromptExecutor)`：注入提示词任务执行器（nil = 禁用 prompt 任务）。
  - `func (s *State) SetObserver(observer func())`：注入状态变化通知（main 接 application 快照投影发布）。
  - `func (s *State) Stop()`：优雅停机：停 ticker、取消 base ctx（终止运行中任务），等待运行 goroutine 退出。
  - `func NewFileStore(path string) *FileStore`：构造全局 JSONL 任务定义存储。
  - `func (store *FileStore) Path() string`：返回落盘路径（空 = 未配置，调度器此时不持久化）。
  - `func (store *FileStore) Append(record ScheduledTaskRecord) error`：追加一行变更（O_APPEND 写一整行，失败即本次变更不成立）。
  - `func (store *FileStore) Load() ([]ScheduledTaskRecord, error)`：读回当前任务集（丢弃墓碑、读不动的行与未收尾残尾）。
