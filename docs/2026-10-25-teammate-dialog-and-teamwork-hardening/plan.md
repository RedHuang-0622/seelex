# Teammate 会话能力与 Teamwork 收口硬化方案

日期：2026-10-25

状态：**方案，待确认后实施**。前置审查见同目录 `front-review.md`。

## 1. 设计目标

本轮一次解决八个相互关联的问题：

1. 为 mainagent、subagent、teammate 的会话提供统一 ConversationSession 桥接能力：查看历史、发起 chat、持久快照与恢复。
2. 统一能力接口不等于统一 Session 管理；目录、切换、审批、任务、团队编排仍留在各自领域。
3. mainagent 以主 Session 为对话窗口，subagent 以子代理 Session 为窗口，teammate 以 WorkItem Session 为窗口。
4. teammate 详情整合当前 WorkItem、system prompt、插件装配与能力、WorkItem 进度与完整执行记录、limit 与权限向量。
5. 保留 `max_teammates = 6` 的“最多创建 6 个 role”语义；新增独立 `max_jobs = 6`，表示团队 active worker job 上限。
6. 已收口里程碑不可再排活或重新派发；里程碑页面保留现有查看能力，并整合 WorkItem 四字段条目视图。
7. teammate worktree 创建失败先返回显式错误；用户批准后才能回退主工作区。
8. WorkItem 工具调用失败策略与 teammate/subagent 并发策略均可选、可调；默认不提前返回、不额外限制。
9. 修复 `team_accept` / `team_close` 生命周期和派发持久化事务窗口。

## 2. 非目标

- 不把 teammate 改造成跨机器的远端 Dot。
- 不新增任意 agent-to-agent 群聊。
- 不把三类 Session 的目录、切换、审批、任务等管理机制耦合成一个管理器。
- 不为 teammate 提供绕过 WorkItem 的独立角色聊天窗口。
- 不把 WorkItem 条目视图扩展成完整敏捷研发系统，不引入 sprint、story point、燃尽图或预计工时。
- 不把 teammate 历史写入主会话上下文；使用独立的 WorkItem-session 键和命名空间。
- 不在本方案中实现跨进程团队计划 CAS。
- 不删除历史计划或已有 worktree 数据。

## 3. 设计原则

| 原则 | 落法 |
|---|---|
| 判据只有一份 | 作业上限、workspace 策略、里程碑关闭、会话分页、worktree 释放分别只有一个函数 |
| 会话能力统一、管理机制不合并 | `ConversationSession` 只含 View/Chat/Recover；其余管理能力留在原域 |
| 恢复策略统一 | live → cold → interrupted → missing 使用同一状态机与 durable transcript 语义 |
| 会话窗口固定 | main=Session，subagent=Subagent Session，teammate=WorkItem Session |
| 先持久化意图，再启动作业 | `jobs.Declare` → 持久化 → `jobs.Start` |
| 显式失败优先 | worktree 创建失败、closed milestone、超额 job 都返回可执行错误 |
| 不伪造事实 | 验收后 worktree 释放失败要承认残留；Session 不在本进程时明确说明 |
| 前端只渲染已有事实 | WorkItem 条目视图只展示依赖、内容、负责 teammate、状态 |
| 允许并行但不共享写文件 | 按领域拆车道，`items.go` 与 `runtime_teamwork.go` 禁止并行修改 |

## 4. 统一会话能力

### 4.1 当前差异

mainagent：

- 有持久化历史、驻留/冷读、内容 LRU、草稿周期快照；
- 历史查看走 `SnapshotOf` / transcript；
- chat 走 `SubmitToSession`；
- 恢复按 durable store 重建。

teammate：

- 直接从 role engine 读 `History()`；
- chat 没有统一入口，只在 legacy `team_dispatch(role,goal)` 的 worker 回合里发生；
- 角色引擎刻意不接 `DurableHistory`，进程重启后无法恢复对话；
- 历史裁剪和 WorkItem 进度是两条读面。

subagent：

- 框架 Session + DurableHistory，与主会话同构；
- 树投影使用非阻塞紧凑上下文；
- 详情直接读节点会话；
- 实时流用独立窗口；
- 已有 `ResumeSubagent` / `Reclaim` / `Recover` 恢复链，但查看与 chat 没有暴露为统一能力。

### 4.2 统一 ConversationSession

新增 `middle/view/sessionaccess`，定义三项能力：

```go
type ConversationSession interface {
    View(ctx context.Context, request ViewRequest) (dto.SessionRecordPage, error)
    Chat(ctx context.Context, request ChatRequest) (dto.ChatReceipt, error)
    Recover(ctx context.Context) (dto.SessionRecovery, error)
}
```

三类 adapter：

```text
ConversationSession
  ├─ MainConversationAdapter
  │    View    → SnapshotOf / transcript
  │    Chat    → SubmitToSession
  │    Recover → 现有 durable cold resume
  ├─ SubagentConversationAdapter
  │    View    → LiveDispatcher 或 completed snapshot
  │    Chat    → node session turn
  │    Recover → 现有 Subagent resume
  └─ TeammateConversationAdapter
       View    → WorkItem durable transcript + live overlay
       Chat    → WorkItem session turn
       Recover → WorkItem session cold resume
```

统一的是：

- SessionRef：`kind + sessionID + owner IDs`；
- teammate 的 SessionRef 必须带 `WorkItemID`；没有 WorkItem 就是“当前没有可对话窗口”；
- 历史分页、记录种类、裁剪和状态词；
- chat 受理与有界结果；
- recovery 状态机；
- 错误语义。

不统一的是：

- main 的会话目录、切换、审批、任务；
- subagent 的 fork 树、merge-back、worktree；
- teammate 的 team plan、角色在编和 WorkItem 验收；
- 各类 Session 的内部存储/缓存实现。

### 4.3 统一恢复策略

所有 adapter 使用同一状态机：

```text
available(live) ──▶ snapshot from execution face
      │
      ├─ process lost ──▶ cold durable source
      │
      ├─ unfinished ────▶ interrupted + recovery note
      │
      └─ no durable ────▶ missing（不得冒充空会话）
```

要求：

- main 保持现有 durable 恢复；
- subagent 保持现有 DurableHistory + resume；
- teammate 新增独立 WorkItem-session DurableHistory，挂在与主会话不同的 Session ID/命名空间；
- 三类 chat 在 cold 状态下都先恢复再接受输入；
- 恢复说明继续复用 `workunit.RecoveryNote`，不新增第二份文案。

### 4.4 方案对比

| 维度 | 方案 A：三类 Session 统一成一个管理器 | 方案 B：统一 ConversationSession 能力端口 |
|---|---|---|
| 管理耦合 | 高：目录、切换、审批、任务都被迫同形 | 低：只统一三个能力 |
| 恢复一致性 | 表面同形，实际容易产生条件分支 | 高：状态机与 durable source 契约统一 |
| 可测试性 | 低，巨型接口难 mock | 高，三类 adapter 各有契约测试 |
| 性能 | 高风险，容易把主会话全量快照套给子代理 | 可保持各来源的非阻塞策略 |
| 改动面 | 极大 | 中，新增 adapter 与 teammate durable 恢复 |
| 可回滚性 | 低 | 高，按 adapter 切换 |

推荐 B。

## 5. Teammate 对话

### 5.1 能力定义

新增工具：

```text
team_message
```

入参：

```json
{
  "role": "exec",
  "item": "wi-impl",
  "message": "请确认你刚提交的实现为什么把超时设为 30 秒。",
  "wait_ms": 30000
}
```

- `role` 必填。
- `item` 必填：teammate 的对话窗口就是 WorkItem Session；没有 item 时明确返回“当前没有可对话的 WorkItem”。
- `message` 必填，作为一轮新的 user 输入。
- `wait_ms` 可选，有界；默认 30 秒，硬上限沿用 `limits.approval_timeout` 或工具超时。
- 完成则返回 teammate 回复；超时则返回 job handle、目标 session 和继续读取方式。

### 5.2 不改变 Work Item 状态

对话不是派活：

- 不创建 Work Item；
- 不改变里程碑；
- 不调用 `SettleWorkItem`；
- 不产生 worktree merge；
- 不自动验收。

如果对话目标是一个尚未完成的 Work Item，只追加该 Session 的一轮对话；已有 Work Item 状态仍由原派发/验收链管理。

### 5.3 对话执行路径

```text
team_message
  → 解析 role/item，item 必填
  → ConversationSessionRef{kind: teammate, sessionID, role, item}
  → TeammateConversationAdapter.Recover（若 cold）
  → TeammateConversationAdapter.Chat
  → 过 max_jobs 闸门
  → Declare dialogue job
  → 持久化 job/session 对话意图与同一 ChatReceipt
  → Start
  → 等待有界
  → Peek 输出
  → 返回 reply 或 handle
```

`team_message` 只是工具壳；真正 chat 能力来自统一 `ConversationSession.Chat`。对话作业与普通 worker 共用 `KindWorker` 和 `RunWorker`，但带有：

```go
DialogueOnly bool
```

`WorkerWorkItemID == ""` 时不进入 Work Item settle；`DialogueOnly` 明确禁止合并/验收。

### 5.4 方案对比

| 维度 | 方案 A：同步直接调用 role round | 方案 B：走异步 job + 有界等待 |
|---|---|---|
| 取消/关闭 | 需要另造取消通道 | 复用 jobs kill/reclaim |
| 并发控制 | 自己维护 session gate | 复用 jobs 上限与 dedup |
| 可观测性 | 只有 handler 返回 | 工作表格、jobs_manage、审计都能看到 |
| 与现有架构一致性 | 低 | 高 |
| 首版实现成本 | 低 | 中 |

推荐 B。对话历史与恢复由统一的 `TeammateConversationAdapter` 提供，不再由 `team_message` 单独维护第二份会话状态。

## 6. Teammate 详情整合与会话子页

### 6.1 位置

保留现有 teammate 详情页的基本信息、负责的 Work Item、装配、动态四页签，并增加「会话」页签：

```text
基本信息 | 负责的 Work Item | 会话 | 装配与能力 | Limit / 权限 | 动态
```

「会话」页签先列出这位 teammate 的 WorkItem，再打开选中 WorkItem 的会话；Work Item 详情页的「执行会话」页签复用同一个 `WorkItemExecutionView` 组件。没有 WorkItem 时不提供“长期角色私聊”入口。

### 6.2 基本信息与当前 WorkItem

必须展示：

- 角色名、RoleSessionID、ToolsPolicy；
- 当前 Work Item：`current_work_item`；
- 当前 Work Item 名称、里程碑、状态；
- 当前 sessionID；
- 若没有当前 WorkItem，明确写“当前无 WorkItem”，不拿历史项冒充当前项。

### 6.3 System Prompt

展示两组，不混淆：

| 字段 | 含义 |
|---|---|
| `base_system_prompt` | 已登记角色提示词；未登记时是最小角色框架 |
| `effective_system_prompt` | 上一次/当前 worker 回合真正设置到引擎的 prompt，包含作业纪律、恢复说明、技能目录等装配结果 |

读取来源：

- base：`roleTurnSystemPrompt(role)`；
- effective：当前 role-session handle 的最近一次实际 prompt；
- 冷恢复：从 WorkItem-session durable execution record 恢复。

正文按需加载，不塞进团队快照；默认有界，可复制全文。

### 6.4 插件装配与能力

保留现有 `PluginAssemblyView` 作为装配摘要，并新增安全、按需的插件能力明细：

```go
type PluginCapabilityView struct {
    Name        string
    Description string
    Include     []string
    Exclude     []string
    Tools       []string
    Skills      []PluginSkillView
    MCPServers  []PluginMCPView
    Source      string
    Fault       string
}
```

显示：

- 声明了哪些 plugin；
- mode 是 `replace` 还是 `inherit-host`；
- 每个 plugin 实际通过 include/exclude 后可见的工具名；
- 每个 plugin 的技能名与简介；
- plugin 提供的 MCP server 名称与 transport；
- 失灵、缺失、黄牌原因；
- 插件能力与权限向量的交集读数。

安全边界：

- 不显示 MCP command/env/URL；
- 不显示 plugin 文件的绝对路径；
- 不显示 plugin Prompt 正文（base/effective teammate prompt 另有独立入口）；
- 权限不能因为“查看能力”而放宽。

### 6.5 WorkItem 进度与完整执行记录

新增唯一组件：

```go
type WorkItemExecutionView struct {
    WorkItem WorkItemView
    Progress ProgressView
    Session  SessionRecordPage
}
```

用途：

- Work Item 详情页的「执行会话」；
- teammate 详情页的「当前 WorkItem / 历史 WorkItem -> 进度」；
- 两者调用**同一个 Application 方法、同一个 adapter、同一个渲染组件**。

执行记录不只包含 LLM user/assistant：

- system prompt；
- user；
- assistant 正文与 reasoning；
- tool call（名称、参数摘要）；
- tool result（成功/失败、结果摘要）；
- 阶段打点 / recovery note；
- 当前 in-flight 状态；
- 分页和截断信息。

进度来源：

- `workunit.ProgressOf` 的 stage/summary/in-flight；
- `TeamworkWorkItemView` 的 status/gate/live/interrupted/worktree；
- WorkItem-session transcript 的完整记录。

三者由后端一次组装，不允许前端再分别调用 `TeammateSessionLive`、team board 和 workunit 三处拼接。

### 6.6 Limit 与权限向量

新增 teammate inspection 字段：

```go
type TeammateLimitsView struct {
    MaxJobs             int
    MaxTurnsConfigured  int
    MaxTurnsEffective   int
    MaxPluginsPerMember int
}

type PermissionVectorView struct {
    ToolsPolicy string
    Groups      map[string]uint8
    Grants      []PermissionGrantView
}
```

显示：

- `tools_policy`；
- `permission_groups` 的原始组 → bits；
- 解码后的 r/w/x 能力名；
- 显式格子 vs 按档位继承；
- 每名 teammate 的 configured/effective `max_turns`；
- 当前 `max_jobs` 全局上限；
- 每成员插件数上限。

权限向量与插件能力分开：

- plugin 只能收窄工具面；
- 权限决定“哪些能力能实际使用”；
- UI 同时显示声明面和最终交集，避免把“插件有工具”误读为“该 teammate 有权限”。

### 6.7 会话历史数据流

```text
TeammateDetailPage
  → application.Service.ConversationSession(ref, View)
  → TeammateConversationAdapter
  → WorkItem-session durable transcript + live overlay
  → dto.SessionRecordPage
```

聊天：

```text
TeammateDetailPage / team_message
  → ConversationSession(ref, Chat)
  → TeammateConversationAdapter
```

首次打开页签才读取，不在 teammate 列表渲染时拉全部正文。

## 7. 作业上限语义

### 7.1 目标

必须拆成两个不同约束：

```text
max_teammates = 一个团队最多创建多少个 role（保留旧名）
max_jobs      = 同一团队当前 active KindWorker job 的最大数量
```

`max_teammates` 不是并发数，也不是 Session 数。一个 role 可以承载多个 WorkItem Job。

`max_jobs` 不是：

- 在编角色数；
- 并发 Session 数；
- worktree 数；
- 同时完成的 assistant 回合数。

此外提供两个**可选的路由级并发限制**：

```text
limits.concurrency.teammate.enabled = false
limits.concurrency.teammate.max     = <启用后必填>

limits.concurrency.subagent.enabled = false
limits.concurrency.subagent.max     = <启用后必填>
```

默认关闭 = 不增加第二层限制；`max_jobs` 仍然是团队作业硬上限。

### 7.2 配置迁移

新配置可以写成：

```yaml
limits:
  team:
    max_roles: 6
    max_jobs: 6
  concurrency:
    teammate:
      enabled: false
      max: 0
    subagent:
      enabled: false
      max: 0
```

旧配置继续表示“最多创建多少个 role”：

```yaml
limits:
  team:
    max_teammates: 6
```

兼容规则：

- 旧 `max_teammates` 映射到 `max_roles`，语义不改成 job 数；
- 新旧 role 上限同时出现且不同 → 启动报错；
- `max_jobs` 是独立配置，不从 `max_teammates` 推导；
- 路由级并发配置默认关闭；开启后 `max <= 0` 启动报错。

### 7.3 判据

```text
创建 role：
  如果 role 数 >= max_roles：拒绝 team_plan

派发 WorkItem：
  如果是同一 Work Item 的已运行 job：
    dedup 返回原 handle，不消耗新名额

  否则如果 activeJobs >= max_jobs：拒绝派发

  如果 teammate 路由限制开启：
    该 role 的 active jobs >= max：拒绝

subagent 批次：
  如果 subagent 路由限制开启：
    当前 subagent jobs >= max：拒绝或按现有批次 max_concurrency 收窄
```

`Declare` 的 job 已登记为 running state，因此也占名额；这让“持久化中的派发”与真正启动的作业共享同一条上限。

### 7.4 DTO/前端

- `TeamworkBackend.MaxTeammates` 继续表示 `MaxRoles`。
- 新增 `TeamworkBackend.MaxJobs`。
- board 同时保留 `max_members`，新增 `max_jobs`。
- 计数文案分两处：“在编 role N/上限”和“运行作业 N/上限”。
- `JobRecordCeiling` 接受 `maxJobs`。

### 7.5 WorkItem 工具失败返回策略

该策略仅作用于 teammate 的 WorkItem 执行，不作用于 mainagent 普通 chat。

配置：

```yaml
limits:
  team:
    workitem_tool_failure:
      enabled: false
      threshold: 3
```

语义：

- 默认关闭，行为和现在一致；
- 开启后，同一 WorkItem 的**连续显式工具失败**达到 threshold；
- 停止这一轮 worker 作业；
- 不自动删除、不自动 `git reset`、不自动回滚 worktree；
- 把控制权和结构化错误回执交还 mainagent；
- 回执提示 mainagent 使用 `team_message(item=...)` 询问 teammate 的主要失败原因；
- WorkItem 状态、worktree 和 Session 保留，由 leader 通过 `team_accept` / `team_fail` / 重派处置。

统计边界：

- 工具执行失败、超时、权限拒绝、非零退出计失败；
- 正常返回但业务内容报错不计；
- 成功调用清零连续计数；
- 关闭策略时零计数、零额外分支、零行为变化。

**“错误回滚”在本方案中定义为“把执行权回滚到 leader”**，不是自动回滚代码。自动回滚代码会丢用户产出，必须另立策略。

## 8. 收口里程碑

### 8.1 规则

里程碑状态为 `done` 后：

- 不允许 `team_work` 新增 Work Item；
- 不允许 `team_item` 修改其下工作项；
- 不允许 `team_dispatch` 派发其下任何 Work Item；
- 允许只读看板、详情和审计；
- 如确需追加工作，必须通过新的里程碑或显式 replan 生成新版本。

### 8.2 单一判据

新增：

```go
func MilestoneClosed(plan store.TeamworkPlan, milestoneID string) bool
```

只在 teamwork 域定义一次。`PlanMilestone`、`AdjustItem`、`DispatchItem` 都调用它。

### 8.3 错误

```text
teamwork: 里程碑 "m-build" 已收口；已收口里程碑不可重新排活或派发。
如需追加工作，请创建新的里程碑或显式 replan。
```

不自动生成新版本，不静默重开。

## 9. 里程碑 WorkItem 条目查看规范

### 9.1 数据来源

这不是严格敏捷研发看板，也不引入新的研发管理数据。它只是当前里程碑下 WorkItem 的条目查看规范，只使用当前计划已有字段。

### 9.2 四条核心内容

每个 WorkItem 条目只突出四项：

| 字段 | 来源 |
|---|---|
| 前置依赖 | `depends_on`，以及每个前置的当前状态 |
| 工作内容 | 已有的 `name + description + goal` |
| 负责 teammate | `role` |
| 工作状态 | `status`，并显示 `gate/live/interrupted` 作为状态解释 |

不增加 story point、sprint、燃尽、预计工时、虚拟优先级或时间线。

### 9.3 布局

在里程碑详情页保留现有五个页签：

```text
基本信息 | Work Item | 成员 | 依赖 | 动态
```

在此基础上增加「条目视图」整合块，推荐作为「Work Item」页签的第一段，而不是替换整个里程碑页：

- 顶部保留现有 Work Item 表；
- 下方增加按状态分组的条目卡/紧凑列表；
- 每张卡固定展示四个核心字段；
- 卡片点击进入现有 `item:<id>` 详情页。

也可以新增独立「条目视图」页签，但不得删除或清洗现有五项查看内容。推荐先增强现有 WorkItem 页签，减少导航分叉。

### 9.4 状态分组

若需要分组，只按真实状态分组：

```text
pending | running | review | done | failed
```

`pending` 内可标注“依赖未完成”或“可以派发”，但它们仍是 pending 的说明，不新增 Backlog/Ready 产品状态。

### 9.5 禁止项

- 不显示虚构燃尽；
- 不显示预计/实际工时；
- 不显示 story point；
- 不显示 sprint；
- 不显示模型生成的 KPI；
- 不按数组序伪造优先级。

## 10. Worktree 创建失败与用户批准

### 10.1 严格创建

当前 `WorktreeManager.BeginNamed` 以 `nil` 表示失败，调用方静默回退主工作区。改为：

```go
func (w *WorktreeManager) BeginNamedStrict(nodeID string) (*NodeWorktree, error)
```

错误分类：

```text
worktree_not_git_repository
worktree_invalid_head
worktree_path_conflict
worktree_branch_error
worktree_create_failed
worktree_unavailable
```

现有 `BeginNamed` 保留为兼容 wrapper，但 teammate 路径必须走 strict。

### 10.2 派发流程

```text
team_dispatch(item, workspace=worktree)
  → worktree 失败
  → 返回 worktree_create_failed + approval_required=true
  → 不 Declare、不写 plan、不启动 job
```

mainagent 随后调用现有 `ask_approve`：

```text
问题：worktree 创建失败，是否允许 wi-impl 直接在 main workspace 修改？
选项：允许本次使用主工作区 / 取消
预览：失败原因、目标 item、角色、将失去的隔离保证
```

批准后重试：

```json
{
  "item": "wi-impl",
  "workspace": "main"
}
```

### 10.3 持久化

Work Item / binding 新增现有事实需要的 `workspace_mode`：

```text
worktree | main
```

只有在显式批准后的请求中才写入 `main`。看板显示“主工作区（已批准）”，避免用户误以为有 worktree 隔离。

### 10.4 方案对比

| 维度 | 方案 A：失败自动落主工作区 | 方案 B：失败 → 审批 → 显式重试 |
|---|---|---|
| 数据安全 | 低，用户不知道隔离失效 | 高，用户明确承担风险 |
| 可观测性 | 低 | 高 |
| 自动化程度 | 高 | 中 |
| 语义正确性 | 错，把降级当成功 | 正确，失败和批准分开 |
| 实现成本 | 低 | 中 |

推荐 B。

## 11. `team_accept` / `team_close` 生命周期

### 11.1 目标语义

`team_accept`：

- Work Item → done；
- 销作业行；
- 释放该 Work Item 的 worktree；
- **不清 Session**；
- 允许后续对话历史继续存在；
- 释放失败必须如实报“已验收但 worktree 仍残留”。

`team_close`：

- 回收所有 teammate 作业；
- 释放所有剩余 worktree；
- 清空角色级和 Work Item 级 Session；
- 关闭团队。

### 11.2 实现拆分

将当前 `releaseBinding` 拆成两个单责动作：

```go
releaseWorktree(binding)
resetBindingSession(binding)
```

- `team_accept` 只调 `releaseWorktree`；
- `team_close` 调两段。

现有 `releaseAllItems` 继续只负责整队回收，但内部复用两个单责动作。

### 11.3 幂等顺序

`AcceptItem`：

1. 校验状态；
2. 计划写 done；
3. 销作业行；
4. 释放 worktree；
5. 追加 release binding；
6. 审计。

若第 4/5 步失败：

- Work Item 仍是 done；
- 返回 `accepted_with_cleanup_pending` 语义错误；
- 再次 `team_accept` 只重试清理，不重复改状态；
- `team_close` 可继续兜底清理。

### 11.4 方案对比

| 维度 | 方案 A：先释放再写 done | 方案 B：先写 done，再幂等清理 |
|---|---|---|
| 现场安全 | 写盘失败会丢 worktree 但 item 未完成 | worktree 残留可重试，不丢现场 |
| 可回滚性 | 低 | 高 |
| 语义 | 中间态不清晰 | accepted + cleanup pending 可解释 |
| 推荐 | 否 | 是 |

## 12. 两阶段派发事务

### 12.1 当前缺陷

当前顺序：

```text
jobs.Dispatch（执行体已启动）
  → WritePlan
  → AppendBinding
  → Audit
```

任一步失败都会出现 job 实际运行但账本不完整。

### 12.2 目标顺序

```text
校验闸门
  → 创建/reuse worktree
  → jobs.Declare（只登记，不执行）
  → AppendBinding
  → AppendAudit(prepared)
  → WritePlan(running + handle + session + workspace)
  → jobs.Start(handle)
  → 返回 handle
```

`Start` 是提交屏障：

- Start 前任何失败：job 没跑，可撤销声明、释放新 worktree；
- Start 后：计划、绑定和审计已经全部可见；
- 进程在 Start 前崩溃：作业没有实际运行，重启恢复为 interrupted/retryable；
- 进程在 Start 后崩溃：所有持久事实已落，现场可恢复。

### 12.3 失败补偿

| 失败点 | 补偿 |
|---|---|
| worktree 创建失败 | 返回 typed error，不动计划 |
| Declare 失败 | 释放新建 worktree |
| binding/audit/plan 写入失败 | `jobs.Done` 声明行 + 释放新建 worktree |
| plan 写成功但 Start 失败 | 保留现场，计划标 failed/interrupted，不返回成功 |
| Start 成功 | 正常 |

### 12.4 方案对比

| 维度 | 方案 A：`Dispatch` + 事后补偿 | 方案 B：`Declare → persist → Start` |
|---|---|---|
| “job 已跑但未落账” | 仍然存在 | 结构上消除 |
| 需要事务存储 | 不需要 | 不需要，Start 作提交屏障 |
| 失败可解释 | 中 | 高 |
| 改动范围 | 小 | 中 |
| 推荐 | 否 | 是 |

## 13. 核心接口

### 13.1 统一 ConversationSession

```go
package sessionaccess

type ConversationKind string

const (
    ConversationMain     ConversationKind = "main"
    ConversationSubagent ConversationKind = "subagent"
    ConversationTeammate ConversationKind = "teammate"
)

type SessionRef struct {
    Kind          ConversationKind
    SessionID     string
    MainSessionID string
    TeamID        string
    Role          string
    WorkItemID    string
}

type ViewRequest struct {
    Offset   int
    Limit    int
    MaxRunes int
    Kinds    []string // message/tool_call/tool_result/stage/recovery
}

type ChatRequest struct {
    Message string
    Wait    time.Duration
}

type ConversationSession interface {
    View(ctx context.Context, request ViewRequest) (dto.SessionRecordPage, error)
    Chat(ctx context.Context, request ChatRequest) (dto.ChatReceipt, error)
    Recover(ctx context.Context) (dto.SessionRecovery, error)
}
```

应用层按 `SessionRef.Kind` 选择 adapter。adapter 只实现 View/Chat/Recover，不接管 main 的目录/切换/审批/任务，也不接管 subagent fork 或 teammate team plan。

### 13.2 Adapter 恢复契约

```go
type RecoveryState string

const (
    RecoveryLive        RecoveryState = "live"
    RecoveryCold        RecoveryState = "cold"
    RecoveryInterrupted RecoveryState = "interrupted"
    RecoveryMissing     RecoveryState = "missing"
)

type SessionRecovery struct {
    State        RecoveryState
    SessionID    string
    Durable      bool
    ResumeNote   string
    RecoveredAt  time.Time
}
```

三类 adapter 必须返回同一组状态词。将现有 subagent `SessionFactory` 中可复用的构造与 DurableHistory 装配提取到 `backend/multiagent/agentsession`；teammate 复用同一构造原语，但使用独立的 WorkItem-session registry 和命名空间。main 继续使用现有 durable session source。

### 13.3 Teammate 对话工具

```go
type TeammateMessageRequest struct {
    Role    string
    Item    string
    Message string
    Wait    time.Duration
}

type TeammateChatPort interface {
    ChatTeammate(ctx context.Context, request TeammateMessageRequest) (dto.ChatReceipt, error)
}
```

接口定义在 teamwork 工具面消费方；生产实现只做 `SessionRef` 解析并委托给 `TeammateConversationAdapter.Chat`，不另造对话运行时。

### 13.4 WorkItem 执行读面

```go
type WorkItemExecutionReader interface {
    WorkItemExecution(
        ctx context.Context,
        mainSessionID string,
        itemID string,
        page ViewRequest,
    ) (dto.WorkItemExecutionView, error)
}
```

WorkItem 详情与 teammate 的进度子页都调用这一接口；返回 `WorkItem + Progress + SessionRecordPage`。

### 13.5 Teammate inspection

```go
type TeammateInspectionReader interface {
    TeammateInspection(
        ctx context.Context,
        mainSessionID, role string,
    ) (dto.TeammateInspectionView, error)
}
```

`TeammateInspectionView` 聚合当前 WorkItem、base/effective prompt、插件装配与能力、limit/权限向量。它只读，不改变 team plan。

### 13.6 Workspace 决策

```go
type WorkspaceMode string

const (
    WorkspaceWorktree WorkspaceMode = "worktree"
    WorkspaceMain     WorkspaceMode = "main"
)

type DispatchOptions struct {
    Workspace WorkspaceMode
}
```

`workspace=main` 只能由批准后的重试显式传入。

### 13.7 Worktree 严格端口

```go
type WorkspaceBinder interface {
    BindWorkspace(ctx context.Context, request WorkspaceBinding) (WorkspaceBinding, error)
}
```

失败必须是非 nil error；无 git、路径冲突和 git 命令失败都不得伪装成空 Path 的成功。

## 14. 任务打点表

### 14.1 任务清单

| ID | 任务 | 领域/模块 | 主要文件 | 前置 | 可并行组 |
|---|---|---|---|---|---|
| T00 | 写红测：ConversationSession、完整 SessionRecord、WorkItemExecution、closed milestone、max_jobs、accept/close、worktree typed error、dispatch start-order | 全链路 | `*_test.go` | 无 | 门禁 |
| S01 | 新增 ConversationSession View/Chat/Recover 契约与公共分页 | middle/view | `middle/view/sessionaccess/` | T00 | B |
| S02 | main ConversationSession adapter | application/core | `session_scope.go`、`session_cold_read.go` | S01 | B |
| S03 | 通用 AgentSessionFactory + 每 WorkItem 独立 durable transcript | subagent/engine | `backend/multiagent/agentsession/`、subagent factory adapter、`runtime_role_turn.go` | S01 | B |
| S04 | teammate ConversationSession adapter | teamwork/engine | `teammate_live.go`、inspection adapter | S01、S03 | B |
| S05 | subagent ConversationSession adapter | subagent view | `middle/view/subagent/coordinator.go`、`live_dispatch.go` | S01 | B |
| U01 | WorkItemExecutionView：进度 + 完整 SessionRecordPage 唯一链路 | teamwork/application | workitem execution reader、DTO、tests | S01、S04 | B |
| U02 | TeammateInspectionView：当前 WorkItem / prompt / limit / 权限向量 | teamwork | teammate inspector、DTO | S04 | B |
| U03 | PluginCapabilityView：plugin 工具/技能/MCP 安全明细 | plugin/teamwork | plugin view adapter、DTO | U02 | B |
| C01 | `team_message` 请求/DTO/工具 schema | teamwork | `tool_shapes.go`、`tool_requests.go`、`teamwork.go` | T00 | C |
| C02 | `team_message` → ConversationSession.Chat 与有界等待 | assemble | `runtime_teamwork.go`、`runtime_role_turn.go` | C01、S04、X01 | C |
| C03 | teammate 详情整合与对话历史子页 | GUI | `team-board-view.js`、`app.js`、tests | S04、U01、U02、U03、C01 | D |
| L01 | 拆分 `max_roles`（兼容 `max_teammates`）与 `max_jobs`，增加可选 teammate/subagent 并发配置 | limit/config | `limits.go`、`config/seelex.yaml`、README | T00 | E |
| L02 | role 数、团队 job 数、路由级并发三层判据 | teamwork | `coordinator.go`、`worker_round.go`、board DTO | L01 | E |
| F01 | WorkItem 工具失败计数与可选返回策略 | teamwork/engine | `tool_failure.go`、role loop hooks、tests | T00、X01 | E |
| M01 | closed milestone 唯一判据 | teamwork | `items.go` / new helper | T00 | A |
| B01 | 里程碑 WorkItem 四字段条目视图（保留现有页签） | frontend | `team-board-view.js`、tests | M01 | D |
| W01 | strict worktree 创建与 typed errors | worktree | `worktree_manager.go`、`runtime_teamwork_items.go` | T00 | A |
| W02 | worktree 失败审批重试与 workspace mode | teamwork/GUI | `tool_requests.go`、`items.go`、board DTO | W01、X01 | A |
| A01 | accept 只释放 item worktree；close 清 Session | teamwork | `items.go`、release helpers、tests | T00 | A |
| X01 | Declare→persist→Start 派发事务 | teamwork | `items.go`、`coordinator.go`、tests | T00、W01 | A |
| Q01 | prompts/README/config 同步 | docs | teamwork/goal SKILL、README、config README | C01/L01/M01/W02/A01 | F |
| E01 | headless/E2E/性能/真实装配验收 | test | `test/`、GUI tests、bench | 全部 | F |

### 14.2 关键依赖图

```text
T00
 ├─ S01 ─┬─ S02
 │       ├─ S03 ── S04 ─┬─ U01 ─┐
 │       │                 └─ U02 ── U03
 │       └─ S05              │
 │                           └─ C02 ── C03 (also needs S04/U01/U02/U03)
 ├─ L01 ── L02
 │       └─ F01
 ├─ M01 ── B01
 └─ W01 ── X01 ─┬─ C02
                └─ W02
            A01
```

## 15. 并行车道

### 车道 A：Teamwork 核心状态机（串行）

任务：

```text
M01 → W01 → X01 → W02 → A01
```

原因：

- 共享 `items.go`、`coordinator.go`、tool request/schema；
- 四者共同决定 worktree、job、accept 和 milestone 生命周期；
- 并行改同一文件会产生语义冲突，不能只看 merge 是否冲突。

### 车道 B：统一会话能力与 teammate inspection

任务：

```text
S01 → S02
  └→ S03 → S04 → U01 / U02 → U03
S01 → S05
```

与车道 A 可并行，但不能同时改 `runtime_teamwork.go`。S03 增加独立 WorkItem-session durable factory；S04/U01/U02 只做能力适配与只读投影，不碰派发状态机。

### 车道 C：Teammate 对话

任务：

```text
C01 → C02
```

可在 S04、U01 和 X01 稳定后接入；C01 的 schema/请求可以先并行完成。C02 只调用统一 Chat，不维护第二份对话状态。

### 车道 D：前端

任务：

```text
C03 + B01
```

- `B01` 只消费现有 plan，不必等后端。
- `C03` 使用 `SessionRecordPage` / `WorkItemExecutionView` / `TeammateInspectionView`，依赖 U01–U03。
- 两个任务同改 `team-board-view.js`，必须由同一前端车道串行，或先分文件再合。
- `B01` 必须增强现有 WorkItem 页签/里程碑详情，不得删除现有五个页签。

### 车道 E：配置、并发上限与失败返回

任务：

```text
L01 → L02
  └── F01
```

可独立开发；L02 在 `coordinator.go` 合入前需车道 A 让出该文件。F01 必须默认关闭，并通过零行为用例证明关闭态与现实现一致。

### 车道 F：文档与 E2E

任务：

```text
Q01 / E01
```

Q01 随各任务增量更新；E01 最后执行。

## 16. 里程碑

### M0：红灯与契约冻结

工作：

- 写失败用例；
- 冻结 `ConversationSession`、`SessionRecordPage`、`WorkItemExecutionView`、`TeammateInspectionView`、`DispatchOptions`、`max_jobs` 与 structured worktree error；
- 明确 `team_message` 的 wait 语义。

退出：

- 所有目标行为至少有一个当前失败用例；
- 接口边界评审完成；
- 不修改生产行为。

回退：删除新增测试/契约草案。

### M1：会话能力与恢复统一

工作：S01–S05、U01–U03。

退出：

- main/teammate/subagent 使用同一 View/Chat/Recover 能力接口；
- 三者恢复状态机和 durable transcript 语义一致；
- teammate 的每个 WorkItem Session 新增独立 durable transcript，重启后可恢复；
- subagent 树投影仍不直接读运行中会话；
- teammate 页面读取不会长时间持 role round gate；
- 旧 DTO 外观保持不变或提供兼容映射。

验证：

```text
go test ./middle/view/... ./application/core/... ./backend/multiagent/teamwork/... -count=1
```

回退：保留旧 adapter，按 DTO 注入关闭新 source。

### M2：Teamwork 核心状态机

工作：M01、W01、X01、W02、A01。

退出：

- closed milestone 在写面和派发面都被拒；
- 没有“job 已跑但 plan/binding/audit 缺失”的顺序；
- worktree 失败无自动降级；
- 批准后才允许 `workspace=main`；
- accept 只释放 item worktree，不清 Session；
- close 清 Session 并回收所有残留。

回退：

- X01 保留旧 `Dispatch` 函数作为测试对照，但不双写生产；
- W02 关闭批准重试入口；
- A01 可按 commit 单独回退。

### M3：Teammate 详情、会话与 WorkItem 条目前端

工作：C01–C03、B01。

退出：

- mainagent 可以对 role/item 发起有界对话；
- 超时返回 handle，不丢回复；
- teammate 详情有会话页签、当前 WorkItem、prompt、插件能力、limit/权限向量的整合视图；
- WorkItem 进度与完整执行记录复用同一个接口与组件；
- 里程碑详情保留现有查看能力，并整合 WorkItem 四字段条目视图；
- 没有新增虚构的 sprint、story point、燃尽或工时数据。

验证：

```text
go test ./backend/multiagent/teamwork/... ./backend/engine/assemble/... -count=1
node --test gui/frontend/dist/team-board-view.test.mjs
```

回退：关闭 `team_message` 工具注册；前端隐藏新页签，不删数据。

### M4：上限、配置与模型行为

工作：L01、L02、F01、Q01。

退出：

- `max_teammates` / `max_roles` 只限制 role 数；
- `max_jobs` 只限制团队 active worker job 数；
- teammate/subagent 路由级并发默认不额外限制，启用后按配置生效；
- WorkItem 工具失败返回默认关闭；开启后达到阈值把控制权交还 leader，不自动删产出；
- prompt 不再把三类上限混成一个概念。

验证：

```text
go test ./backend/limit/... ./backend/multiagent/teamwork/... -count=1
```

回退：配置兼容层可恢复旧键映射；不改变已落盘计划。

### M5：全链路验收

工作：E01。

退出：

- headless 真装配完成：
  - 创建团队；
  - 派发两个 Work Item；
  - 与 teammate 对话；
  - 验收一个 Work Item 并确认只释放该 worktree、不清 Session；
  - close 后 Session 清空；
  - worktree 失败 → approve → main workspace 重试；
  - closed milestone 重派被拒；
  - WorkItem 四字段条目视图与状态一致，现有里程碑页签仍在。
- main/teammate/subagent 的会话查看/chat/recover 在长流期间不阻塞界面；
- 全量构建、测试、diff check 通过。

## 17. 测试策略

### 17.1 红灯用例

1. `team_accept` 只释放目标 Work Item worktree，不重置 Session。
2. `team_close` 清空角色与 Work Item Session。
3. worktree 创建失败时不调用 Declare/Start，不写 running。
4. 批准后 `workspace=main` 成功并显示主工作区模式。
5. `Declare` 成功后任一持久化失败，不启动 executor。
6. executor 启动前，plan/binding/audit 已存在。
7. done milestone 的 `team_work` / `team_item` / `team_dispatch` 均被拒。
8. 同一角色多 Work Item 各占 job 名额，不再按 role 排除。
9. 重复派发同一在跑 Work Item 不额外占名额。
10. main/subagent/teammate 都能通过同一 View/Chat/Recover 接口查看、对话和恢复。
11. teammate 进程重启后可从独立 durable transcript 恢复会话历史。
12. `team_message` 能进入目标 session 并把回复取回。
13. WorkItem 详情与 teammate 当前 WorkItem 进度返回同一个 `WorkItemExecutionView`。
14. 完整执行记录包含 tool call/result、stage/recovery，而不是只有 user/assistant。
15. teammate inspection 返回 system prompt、插件能力、limit 和权限向量。
16. `max_teammates` 超限只影响创建 role；`max_jobs` 超限只影响 active worker job。
17. teammate/subagent 路由并发默认关闭时只受 `max_jobs`/框架兜底约束。
18. WorkItem 工具失败返回关闭时行为完全不变。
19. 开启失败返回后，连续失败达到阈值会停止 worker、返回 leader 与结构化原因，且 worktree/产出仍保留。

### 17.2 会话能力跨实现一致性

用同一组输入分别喂：

- main adapter；
- teammate adapter；
- subagent adapter；

断言：

- View/Chat/Recover 三种能力同形；
- live/cold/interrupted/missing 状态一致；
- offset/limit 归一一致；
- total/has_more 一致；
- 单条 UTF-8 裁剪一致；
- unavailable 与 empty 不混淆；
- 完整记录种类一致；
- 恢复说明只来自同一 builder。

### 17.3 前端

- WorkItem 条目固定展示前置依赖、工作内容、负责 teammate、工作状态；
- 现有里程碑五项查看能力仍在；
- pending 的“依赖未完成/可派发”只是状态说明，不新增产品状态；
- 卡片可下钻；
- done milestone 的条目视图只读；
- teammate 当前 WorkItem、prompt、插件能力、limit/权限、会话分页、空态与不可用态；
- 恶意文本转义。

### 17.4 验证命令

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./backend/limit/... ./backend/multiagent/teamwork/... ./backend/multiagent/subagent/... ./middle/view/... ./application/core/... ./gui/... -count=1
go test ./test/ -count=1
node --test gui/frontend/dist/*.test.mjs
git diff --check
```

Windows `CGO_ENABLED=0` 时不得声称 race 已执行。CI 补 Linux `-race`。

## 18. 文档同步

同一批 change 内更新：

- `backend/multiagent/teamwork/README.md`
- `backend/engine/assemble/README.md`
- `backend/multiagent/agentsession/README.md`
- `backend/multiagent/subagent/README.md`
- `middle/view/sessionaccess/README.md`
- `middle/view/subagent/README.md`
- `backend/tool/worktree/README.md`
- `backend/limit/README.md`
- `config/README.md`
- `plugins/default/teamwork/SKILL.md`
- `plugins/default/goal/SKILL.md`
- `gui/README.md`
- `gui/frontend/README.md`
- `test/README-teamwork.md`

定期自动索引刷新：

```text
python scripts/gen_core_readme_index.py --only backend/multiagent/teamwork
python scripts/gen_core_readme_index.py --only middle/view/sessionaccess
```

若生成器暂不支持新包，手工章节仍必须符合 `docs/arch/readme-spec.md`。

## 19. 回滚方案

- `max_roles` / `max_jobs`：旧 `max_teammates` 继续映射到 role 数；回滚可恢复为单配置但不改变已落盘计划。
- sessionaccess：保留旧 adapter；通过装配切回旧读取/chat 实现，不同时维护两份业务判定。
- WorkItem-session durable transcript：新增独立 namespace；回滚时旧会话仍可从 workunit 记录读取诊断，不删除新 transcript 数据。
- `team_message`：工具注册可关闭，不影响 schema/测试数据。
- WorkItem 条目视图：纯前端派生，可撤掉新增整合块，原有五个页签不受影响。
- worktree approval：`workspace=main` 需显式参数；关闭入口后只保留 strict 失败。
- accept/close：按 commit 独立回滚；释放记录追加型，不重写历史。
- Declare/Start：保留旧 `Dispatch` 仅作测试对照，不保留生产双实现；回滚去旧版本代码。

## 20. 待裁决

建议默认值如下，确认后不再阻塞：

1. `team_message` 默认等待 30 秒；超时返回 handle。
2. 所有对话都通过 ConversationSession；teammate 的窗口是 WorkItem Session，item 必填。
3. 每个 WorkItem Session 使用独立 durable transcript，保存与恢复策略和 main/subagent 一致。
4. `max_teammates` 保留“最多创建 6 个 role”；新增独立 `max_jobs = 6`；旧配置继续可读。
5. 里程碑 WorkItem 条目固定展示前置依赖、工作内容、负责 teammate、工作状态；保留现有里程碑页签。
6. done milestone 的追加工作必须新建 milestone 或 replan，不允许 reopen。
7. main workspace 回退必须逐次审批，不提供全局永久放行。
8. teammate/subagent 路由级并发默认不额外限制，支持全局启用并设置上限。
9. WorkItem 工具失败返回默认关闭；启用后连续失败达到配置阈值时，把控制权与错误原因返回 leader，不自动回滚代码。

## 21. 推荐执行顺序

```text
第 1 批（可并行）
  T00
  S01
  L01

第 2 批（三条安全并行车道）
  A: W01 → X01 → W02 → A01
  B: S02 + S03 → S04 → U01/U02 → U03 + S05
  E: L02 → F01

第 3 批（依赖前两批）
  C01 → C02
  M01 → B01

第 4 批（集成）
  C03
  Q01
  E01
```

不建议先做 UI 再改后端语义：当前 `team_accept`、worktree 降级和 job 上限的文字都与目标不一致，先做界面会把错误语义固化到更多测试里。
