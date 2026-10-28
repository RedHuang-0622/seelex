# store/teamwork

## 生态位

Teamwork 计划的限界上下文实现：稳定 wire 形状、DAG 校验/折叠判据（M8 `af2bbaab`），
以及计划/审计/绑定的落盘 owner（M8 本切片）。主要调用方是根 `store.Router` 的迁移期
兼容转发、`backend/multiagent/teamwork` 的团队工具，以及看板归档的派生读取。

## 职责与非职责

- 职责：计划 head 整份替换、`events.jsonl` 与 `worktrees.jsonl` 只追加、崩溃残尾读取、
  作业输出目录；plan/work-item DAG 校验、状态词表、binding 最后一行折叠。
- 非职责：worker 执行、job 调度、整队收口用例、看板投影或跨域事务；team registry
  （`session/team/roles.json` 的角色身份与策略）归 `store/role`，不在本包。

## 目录结构

- `model.go`：`Plan` / `Member` / `Milestone` / `WorkItem` 等别名、状态常量、
  `Event` / `Binding` wire 形状与 `Bindings` / `PlanIsEmpty` / `WorkItemTerminal` 判据。
- `validation.go`：`ValidatePlan` 与其里程碑/工作项 DAG、跨里程碑依赖、角色引用校验。
- `store.go`：物理路径常量、`Store` 读写删除面、两条追加流水与崩溃残尾读取。
- `store_test.go`：路径身份、整份替换、只追加、残尾、冷读、并发追加与作业目录。
- `teamwork_test.go`：binding 折叠与 DAG 判据的纯函数回归。

## 核心实现

`ModuleID = "teamwork"` 是计划 head 的模块身份（`metadata/teamwork.json`）。落盘名字
`teamwork` / `events.jsonl` / `worktrees.jsonl` / `jobs` 与 M8 之前逐字相同，改一个字节
就是数据迁移。`WritePlan` 先 `ValidatePlan(plan, maxTeammates)` 再
`layout.Commit(scope, ModuleID, "", plan, now)`——整份替换、原子发布；`ReadPlan` 走
`layout.ReadWithHeal`，缺失返回空计划（还没声明过团队，不是错误）。`AppendEvent` /
`AppendBinding` 共用 `appendLine`：`EnsureGuide` → 模块锁 → 建目录 →
`O_APPEND` 单次写"一行 JSON + `\n`"。`ReadEvents` / `ReadBindings` 共用 `readRows`：
缺失为空切片，跳过最后一行解析失败（崩溃残尾），中间行损坏显式报错。
`Bindings` 按 `work_item` 取最后一行，`released=true` 表示解除。

## 数据流与生命周期

写入：调用方给出完整计划 → 结构性校验（角色/里程碑/工作项/DAG）→ 模块锁内整份发布
head。审计与绑定是**只追加**的事实流：一次调用追加一行，永不重写；读取按文件顺序，
即发生顺序。折叠在读取之后进行（binding 取最后一行），因此历史行不会因为后续事实而
被删除。作业目录只提供路径与建目录，文件命名与保留策略归调用方。

## 依赖方向

允许依赖 `contract/dto`、`store/root`、`store/internal/layout` 与标准库。禁止 import
根 `store`、application、middle、backend、wiring，也禁止 import 其他
`store/<domain>` 实现或看板投影实现。

## 并发、存储与错误语义

计划发布与两条追加流水共用 `layout.Lock(scope, ModuleID)`——与根覆盖率时代
`mu(key, moduleTeamwork)` 是同一把锁，跨适配层不会分裂成两把；不同会话互不阻塞。
追加写入是单次 `Write`，并发追加只会整行交错，不会半行污染；崩溃最多留下无换行的半行，
读侧跳过它，中间损坏不静默吞掉。空 `event.kind` 与空 `binding.work_item` 在落盘前
显式报错，不留空文件。计划校验失败时磁盘一个字节都不动。

## 扩展方式

新增审计事件类型只需在 `model.go` 的 `Kind` 词表与调用方同步；新增团队字段必须保持既有
JSON tag 不变并先在 `contract/dto` 定义形状。新增持久化文件应作为本包的只追加流水或
新的模块 head 实现，禁止在根包再写一份追加/折叠/路径判据。

## Review 指南

重点检查：`metadata/teamwork.json`、`teamwork/events.jsonl`、`teamwork/worktrees.jsonl`
与 `teamwork/jobs` 的路径是否逐字未变；计划是否仍整份替换而非合并；两条流水是否仍
只追加（没有 `O_TRUNC`、没有压实重写）；崩溃残尾规则是否只放过最后一行；binding 是否
仍取最后一行；DAG 校验是否仍在落盘前执行；是否有人把 registry 或 worker 状态塞回本包。

## 测试与验证

```text
go test ./store/teamwork -count=1
go test ./store/... ./backend/multiagent/teamwork/... ./test/ -count=1
```

根包 `teamwork_test.go` 仍验证 Router 单层转发的行为 parity；
`e2e/store_ddd_teamwork_io_ownership_gate_test.go` 验证本包是 I/O owner，根包只剩转发。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### model.go

- `func PlanIsEmpty(plan Plan) bool` — PlanIsEmpty reports whether no team has been declared.
- `func WorkItemStatusOrPending(item WorkItem) string` — WorkItemStatusOrPending maps the empty status to pending.
- `func WorkItemTerminal(item WorkItem) bool` — WorkItemTerminal reports whether an item has a terminal status.
- `func Bindings(rows []Binding) map[string]Binding` — Bindings folds append-only binding rows by Work Item; the last row wins.

### store.go

- `func New(layout *storelayout.Layout) *Store` — New 构造 teamwork 持久化面。
- `func (s *Store) Dir(scope dto.SessionKey) string` — Dir 返回 teamwork 数据目录（不创建）。
- `func (s *Store) EventPath(scope dto.SessionKey) string` — EventPath 返回审计流水路径。
- `func (s *Store) BindingPath(scope dto.SessionKey) string` — BindingPath 返回绑定账本路径。
- `func (s *Store) JobsDir(scope dto.SessionKey) string` — JobsDir 返回作业输出目录（不创建）。
- `func (s *Store) WritePlan(scope dto.SessionKey, plan Plan, maxTeammates int) error` — WritePlan 整份替换计划 head。maxTeammates <= 0 = 只做结构性校验。
- `func (s *Store) ReadPlan(scope dto.SessionKey) (Plan, error)` — ReadPlan 读回计划。**缺失 = 空计划**（还没声明过团队），不是错误。
- `func (s *Store) AppendEvent(scope dto.SessionKey, event Event) error` — AppendEvent 追加一条审计事件（只追加、不重写）。
- `func (s *Store) ReadEvents(scope dto.SessionKey) ([]Event, error)` — ReadEvents 读回全部审计事件（文件顺序 = 发生顺序）。
- `func (s *Store) AppendBinding(scope dto.SessionKey, binding Binding) error` — AppendBinding 追加一行 Work Item 绑定事实（只追加、不重写）。
- `func (s *Store) ReadBindings(scope dto.SessionKey) ([]Binding, error)` — ReadBindings 读回全部绑定事实（文件顺序 = 发生顺序）。
- `func (s *Store) JobOutputDir(scope dto.SessionKey) (string, error)` — JobOutputDir 返回（并确保存在）teammate 作业输出目录。
- `func (s *Store) appendLine(scope dto.SessionKey, path string, line []byte, label string) error` — appendLine 是两条追加流水的**唯一**物理实现：确保 guide → 模块锁 → 建目录 →
- `func layoutScope(key dto.SessionKey) storelayout.Scope`
- `func readRows[T any](path, label string) ([]T, error)` — readRows 是两条追加流水的**唯一**读取实现：缺失 = 空切片；跳过崩溃残尾。
- `func decodePayload[T any](head storelayout.Head) (T, error)` — decodePayload 解模块 head 的 payload（与根 storeEngine 的通用解码同一文案）。

### store_test.go

- `func teamworkStoreFixture(t *testing.T) (*Store, *storelayout.Layout, dto.SessionKey)`
- `func coldStore(t *testing.T, layout *storelayout.Layout, scope dto.SessionKey) *Store` — coldStore 用**同一个数据根**新建 layout + Store：冷读证明读面不依赖内存态。
- `func validPlan(teamID string) Plan`
- `func TestStorePhysicalPathsAreUnchanged(t *testing.T)` — TestStorePhysicalPathsAreUnchanged 钉住"零数据迁移"：计划 head 仍是
- `func TestWritePlanReplacesWholePayload(t *testing.T)` — TestWritePlanReplacesWholePayload：计划是 head 整份替换——第二次写入必须让第一份
- `func TestReadPlanMissingIsEmpty(t *testing.T)` — TestReadPlanMissingIsEmpty：还没声明过团队是正常状态，不是错误。
- `func TestWritePlanValidatesBeforeWriting(t *testing.T)` — TestWritePlanValidatesBeforeWriting：校验失败时不得留下半个 head。
- `func TestReadPlanColdStart(t *testing.T)` — TestReadPlanColdStart：换一个 Store 实例（新 layout）仍能读回——读面不依赖内存态。
- `func TestEventsAreAppendOnlyAndOrdered(t *testing.T)` — TestEventsAreAppendOnlyAndOrdered：审计只追加，文件前缀必须原样保留，读回顺序=发生顺序。
- `func TestAppendEventRequiresKind(t *testing.T)` — TestAppendEventRequiresKind：没有 kind 的审计行会让读面无法分类，必须显式拒绝。
- `func TestReadEventsMissingIsEmpty(t *testing.T)` — TestReadEventsMissingIsEmpty 与 TestReadBindingsMissingIsEmpty：缺失=空集，不是错误。
- `func TestReadBindingsMissingIsEmpty(t *testing.T)`
- `func TestCrashTailIsSkipped(t *testing.T)` — TestCrashTailIsSkipped：崩溃只会留下半行尾声；读侧跳过它，但中间行损坏仍报错。
- `func TestBindingsFoldLastRowAfterColdRead(t *testing.T)` — TestBindingsFoldLastRowAfterColdRead：绑定只追加，折叠取最后一行；Released 撤下。
- `func TestConcurrentAppendsKeepWholeLines(t *testing.T)` — TestConcurrentAppendsKeepWholeLines：两条流水共用模块锁，并发追加不得产生撕裂行。
- `func TestJobOutputDirIsSessionScoped(t *testing.T)` — TestJobOutputDirIsSessionScoped：作业输出目录在会话内，可重复取。

### teamwork_test.go

- `func TestBindingsFoldLastRow(t *testing.T)`
- `func TestValidatePlanRejectsGraphAndCrossMilestoneDependency(t *testing.T)`

### validation.go

- `func ValidatePlan(plan Plan, maxTeammates int) error` — ValidatePlan validates one plan before persistence. maxTeammates <= 0 means
- `func validateWorkItems(plan Plan, roles map[string]struct{}) error`
- `func validateMilestoneGraphAcyclic(milestones []Milestone) error`
- `func validateItemGraphAcyclic(milestone Milestone) error`
