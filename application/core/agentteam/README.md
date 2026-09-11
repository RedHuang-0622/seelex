# core/agentteam

## 生态位

A2A 角色团队的**通用装配能力面**：把「`TeamSpec`/`RoleSpec` → 角色会话 + 工作顺序 +
成员表」抽成可复用装配，供 `application/core` 的 Service 门面与 headless `team.*`
接口消费。长期边界见 [`docs/arch/a2a-agent-team-factory.md`](../../../docs/arch/a2a-agent-team-factory.md)；
本次落地的工厂/preset/注册表口径见
[`docs/2026-09-10-a2a-agentteam-recovery/agentteam-management.md`](../../../docs/2026-09-10-a2a-agentteam-recovery/agentteam-management.md)。

主要调用方：`application/core/agentteam_service.go`（窄转发 + 端口适配）、
`gui/headless_team.go`（`team.*` RPC）与 `application/core/goal_service.go`
（goal 创建时自动装配 `goal-a2a`，见下）。goal 的 TL 只是本包的内置 preset，
不是特例。

装配入口有两条：

- **显式**：`team.materialize` / GUI 角色管理页按 preset 装配任意团队；
- **隐式**：`GoalBeginFor` 在 goal 落栈成功后调
  `MaterializeAgentTeamPreset(sessionID, "goal-a2a", 0)`——因为 `goal-a2a` 的
  TL 声明了 `JoinPolicy=on_goal_create`，"goal 上线"就该把 TL 团队拉起来。
  幂等由工厂保证（同 `(team_id, role_name)` 派生同一 `role_session_id`）；
  宿主未装配团队存储时只记日志、不阻塞 goal（`ensureGoalAgentTeam`）。

## 职责与非职责

做什么：

- `Normalize` 规整 `TeamSpec`（默认值、角色去重、`order_roles` 推导与校验）；
- `Factory.Materialize` 由 `TeamSpec` 装配一支团队：幂等建角色会话 → 写注册表 →
  写 `lifecycle` 顺序策略 → 返回成员表；
- `Registry` 角色配置 CRUD 与工作顺序设置；
- `assembleView` 把注册表 + 顺序投影成前端消费的成员表（含定时分区与设计偏差提示）。

刻意不做：

- **不接纳 subagent**：subagent 是 tool calling 能力（劳务派遣），不建角色会话、
  不进 `order_roles`、不占 floor（见设计稿 AT1）；
- **不写 message**：正文只由 sequencer append；
- **不做第二份顺序事实**：顺序只落 `lifecycle.order_policy`/`order_roles`；
- 不决定 provider role：`role_name` 只是 metadata，provider 侧仍只有
  `system/user/assistant/tool`。

## 文件结构

| 文件 | 职责 |
|---|---|
| `spec.go` | `TeamSpec` 规整与校验、角色会话号派生（`RoleSessionID`） |
| `presets.go` | 内置实例：`goal-a2a`（TL 循环）、`review-team`、`research-team`（定时分区） |
| `factory.go` | `Port` 契约、`Factory.Materialize`、成员表投影 `assembleView` |
| `registry.go` | `Registry`：角色配置 CRUD、`SetOrder`、`View` 只读投影 |
| `agentteam_test.go` | 规整/工厂幂等/第二团队（AT8）/定时分区/注册表用例 |

## 核心实现

- `Port` 是唯一外部依赖面：`EnsureRoleSession`、`ReadLifecycleOrder`、
  `SetLifecycleOrder`、`Read/WriteTeamRegistry`。实现方是 `application/core` 的
  `agentTeamAdapter`（把 `internal/adapters.SessionPort` 的 DTO 形态转成工厂输入）。
- `Factory.Materialize` 的幂等键是 `(team_id, role_name)` → `RoleSessionID`；重复装配
  不产生第二个角色会话，返回结果里 `TeamRoleSession.Created=false`。
- `resolveOrderRoles` 是顺序唯一入口：定时角色（`RoleKindTimer`）不得进顺序；
  显式顺序必须是「`user` + `main` + 已注册角色」的子集；未给定时按
  `user → main → 其余角色（OrderPriority 升序）` 推导。
- `assembleView` 只报事实不修补：已注册但不在顺序、顺序里未注册的角色写成
  `TeamView.DesignNotice`，供前端与冒烟断言。

## 数据流或生命周期

```text
TeamSpec（preset 或前端提交）
  → Normalize（默认值/去重/顺序校验）
  → Factory.Materialize
      → Port.EnsureRoleSession（每个非 user/main 角色一个角色会话）
      → Port.WriteTeamRegistry（session/team/roles.json，整份替换型）
      → Port.SetLifecycleOrder（lifecycle head：order_policy/order_roles）
  → dto.TeamMaterializeResult（Spec/View/Sessions/Registry）
```

读取路径：`Registry.View` 读注册表 + `lifecycle` 顺序 → `dto.TeamView`
（成员 + 工作顺序 + 定时分区 + 设计偏差提示）。运行态 `online`/`floor` 高亮由
presence 与 `message head.floor` 提供，不在本包落盘。

## 依赖方向

- 允许：本包 → `application/contract/dto`（纯 DTO）。
- 禁止：本包 → `sessionstore`、`internal/adapters`、`gui`；也禁止反向依赖
  （存储与界面不得 import 本包）。存储形态只在适配器里出现。

## 并发、存储、安全或错误语义

- 装配是**幂等**的：重复 `Materialize` 复用同一角色会话；注册表与顺序整份替换。
- 错误一律显式：未知 preset（`ErrUnknownPreset`）、未注册角色进顺序、定时角色进顺序、
  重复 `role_name`、内置角色被改写/删除都返回错误，不静默降级。
- 不做读时补写：`View` 遇到「注册表有角色但 lifecycle 无顺序」时按注册顺序推导只读
  视图，不落盘，避免读操作产生第二份事实。

## 扩展方式

- 新增团队形态：在 `presets.go` 加一条 preset（角色集 + `order_policy`），或由前端直接
  提交 `TeamSpec`；不改工厂、sequencer、message/draft/compact 形态。
- 新增调度策略：只在 `dto` 增加策略名并校验，替换点是 sequencer 的 role 顺序函数。
- 新增角色字段：加在 `dto.RoleSpec` + `sessionstore.TeamRoleSpec` + 适配器映射三处，
  保持旧注册表可读（缺字段 = 未配置）。

## Review 指南

- 顺序是不是只落 `lifecycle`？有没有在别处复制一份 `order_roles`？
- 定时角色有没有漏进 `order_roles`？subagent 有没有被当成团队成员？
- 角色会话号是否稳定（`(team_id, role_name)`）？重复装配会不会建出第二棵子树？
- `View` 是否偷偷写盘（读路径必须零写入）？
- `role_name` 是否只做了 metadata：没有被当成 provider role 使用？

## 测试与验证

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./application/core/agentteam ./sessionstore ./gui ./internal/adapters
go test ./application/core/agentteam ./sessionstore ./gui -count=1
go test -race ./application/core/agentteam -count=1
```

关键测试：`agentteam_test.go`（规整/幂等/AT8 第二团队/定时分区/注册表 CRUD）、
`sessionstore/team_registry_test.go`（注册表落盘与角色会话幂等）、
`gui/headless_team_test.go`（`team.*` 契约）、
`gui/team_live_probe_test.go`（真实 API + pprof 冒烟，env 门控）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### spec.go

- `func Normalize(spec dto.TeamSpec) (dto.TeamSpec, error)` — Normalize 把 TeamSpec 规整成可装配形态：补默认值、去重、推导 order_roles
- `func resolveRoleKind(roleName string, kind dto.RoleKind) dto.RoleKind` — resolveRoleKind 让内置角色名（user/main）永远取内置 kind
- `func resolveOrderRoles(spec dto.TeamSpec, registered map[string]struct{}) ([]string, error)` — resolveOrderRoles 决定工作顺序：显式给定时必须是 [user, main + 已注册角色] 的
- `func RoleSessionID(teamID, roleName string) string` — RoleSessionID 派生角色会话号：同一个 (team_id, role_name) 永远得到同一个值
- `func needsRoleSession(kind dto.RoleKind) bool` — needsRoleSession 判定该角色是否需要独立角色会话子树
- `func registeredRoles(spec dto.TeamSpec) []dto.RoleSpec` — registeredRoles 返回需要角色会话的已注册角色

### presets.go

- `func goalA2APreset() dto.TeamSpec` — goalA2APreset 是第一个实例：goal 的 user→main↔tl 固定循环
- `func reviewTeamPreset() dto.TeamSpec` — reviewTeamPreset 是第二个实例（AT8 证据）
- `func researchTeamPreset() dto.TeamSpec` — researchTeamPreset 演示「定时 agent 不入 order_roles」的第三形态
- `func Preset(teamKind string) (dto.TeamSpec, error)` — Preset 返回内置团队实例（goal-a2a / review-team / research-team）
- `func Presets() []dto.TeamSpec` — Presets 返回全部内置 preset（供前端角色管理页列出可选团队形态）

### factory.go

- `func NewFactory(port Port) (*Factory, error)` — NewFactory 构造工厂；port 为 nil 时显式报错（不允许静默空转）
- `func (factory *Factory) Materialize(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error)` — Materialize 装配 TeamSpec
- `func registryFromSpec(spec dto.TeamSpec) dto.TeamRegistry` — registryFromSpec 把 TeamSpec 投影成注册表（角色配置的持久事实）
- `func assembleView(sessionID string, registry dto.TeamRegistry, policy string, orderRoles []string) (dto.TeamView, error)` — assembleView 把注册表 + 生命周期顺序投影成前端消费的成员表
- `func buildMember(teamID, name string, orderIndex int, inOrder bool, byName map[string]dto.RoleSpec) dto.TeamMember`
- `func viewNotices(registry dto.TeamRegistry, orderRoles []string) []string` — viewNotices 只报事实，不自动修补：注册了但不在顺序里的角色、顺序里未注册的角色

### registry.go

- `func NewRegistry(port Port) (*Registry, error)` — NewRegistry 构造注册表读写面；port 为 nil 时显式报错
- `func (registry *Registry) View(mainSessionID string) (dto.TeamView, error)` — View 返回成员表（供右侧栏「状态 → Agent Team」子页与角色管理设置读取）
- `func (registry *Registry) PutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error)` — PutRole 新增或覆盖一个角色配置（按 role_name 幂等）
- `func (registry *Registry) DeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error)` — DeleteRole 删除一个角色配置；角色仍留在工作顺序时同步摘除
- `func (registry *Registry) SetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error)` — SetOrder 写工作顺序策略
- `func firstNonEmpty(values ...string) string`

### agentteam_test.go

- `func newFakePort() *fakePort`
- `func (port *fakePort) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (port *fakePort) ReadLifecycleOrder(string) (string, []string, error)`
- `func (port *fakePort) SetLifecycleOrder(_ string, policy string, roles []string) error`
- `func (port *fakePort) ReadTeamRegistry(string) (dto.TeamRegistry, error)`
- `func (port *fakePort) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error`
- `func TestNormalizeDerivesOrderAndExcludesScheduledRoles(t *testing.T)` — 未给 order_roles 时按 user→main→其余角色推导；定时角色单独分区、不入工作顺序
- `func TestNormalizeRejectsBrokenOrder(t *testing.T)` — 定时角色不得进顺序、顺序角色必须已注册、user/main 必须在列
- `func TestGoalPresetMaterializeIsIdempotent(t *testing.T)` — goal preset 装配建 TL 角色会话、写顺序策略；重复装配不产生第二个会话
- `func TestSecondTeamThroughSameFactory(t *testing.T)` — 同一个工厂实例化 goal 之外的第二个团队
- `func TestResearchPresetKeepsScheduledRoleOutOfOrder(t *testing.T)` — 定时 agent 只出现在定时分区
- `func TestRegistryCRUDAndOrder(t *testing.T)` — 角色配置 CRUD 只改注册表；顺序设置只改 lifecycle 字段
