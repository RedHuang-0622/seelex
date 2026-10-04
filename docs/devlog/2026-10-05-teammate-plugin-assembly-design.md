# teammate 的「插件装配」：接缝图与两轴契约（设计，未实现）

> 日期：2026-10-05 · 角色：arch（work item `wi-arch` / milestone `m-seam`）
> 定位：**设计文档**，不落代码。给出接缝（现文件:行）+ 七条契约裁决 + exec 最小改动清单 + 本轮不做。
> 一手输入：`docs/research/2026-10-05-teammate-plugin-assembly-external-research.md`（Playwright 实读，2026-10-05）
> 历史裁决（本稿必须与之对齐，不许悄悄推翻）：`docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md:71`（**全局保持单选**）、`:82`（全局多插件叠加不排期）；`plugins/default/README.md`（default = 启动基线）。

---

## 0. 一句话结论

装配的接缝**今天就已经存在，而且已经带 ctx**：`seelebridge/tools/policy.go:82` 的
`p.deps.PluginFilter(filtered)` 是唯一一处"按会话装配"被丢掉的地方（该函数上游 :48-76 已经按
ctx 分节点作用域与员工工具面）。把它从**无 ctx 的全局闭包**换成**按 ctx 解析会话装配**，
不需要新的执行面、不需要动 registry、不需要动权限门。两轴的分界线是：
**权限轴按会话（已有，`RoleSpec.ToolsPolicy`/`PermissionGroups`）；能力轴（插件/skill）今天仍是全局单选，
本设计把它改成"按会话的集合"，且只收窄、不放权。**

---

## 1. 接缝图（现状测绘，逐条可核对）

### 1.1 今天的插件面：全局单选，且**对 teammate 同样生效**

| 层 | 位置 | 事实 |
|---|---|---|
| 可见性执行面 | `seelebridge/plugin/plugin.go` | `Manager{defs, active string}`（`active` 在 `:32`）；`Filter(tools)` 读 `m.active`，未激活/未定义**原样返回**；文件头 `:8` 自述"多插件叠加属产品决策，**当前为单选**"。本包只是**投影缓存**，事实源在 root（`:1-11`）。 |
| 无 ctx 的接缝 | `seelebridge/tools/policy.go:16` | `PluginFilter func([]types.Tool) []types.Tool` —— **签名里没有 ctx**。 |
| 接缝调用点 | `seelebridge/tools/policy.go:81-82` | `if p.deps.PluginFilter != nil { return p.deps.PluginFilter(filtered) }` ← **唯一收口**。 |
| 上游已有 ctx | `seelebridge/tools/policy.go:48-49`（`Filter(ctx, tools)` + `NodeScopeFromContextOrEmpty(ctx)`）、`:76`（`p.deps.ToolFace(ctx, name)`） | 策略**已经**按 ctx 分主体。接缝不是"没有 ctx"，是"最后一跳把 ctx 扔了"。 |
| 注入点 | `seelebridge/runtime.go:377`（`seeltools.NewPolicy(...)`）、`:401`（`PluginFilter: r.plugins.Filter`）、`:406` 附近（`bridge.WithVisibilityPolicy(r.visibilityPolicy.Filter)`） | 全仓唯一接进 bridge 的地方；一个 Policy 实例服务**所有**会话。 |
| teammate 走的正是它 | `seelebridge/runtime_role_turn.go:378-379`（`Agent: teammateToolFace(r.agt)`） | 角色会话引擎 = `r.agt`（带同一个 Policy）。⇒ **teammate 的工具面今天被宿主的 active 插件全局收窄**。 |
| 权限轴（已按会话） | `seelebridge/runtime_role_turn.go:267`（`WithEmployeeGrant(ctx, roleName, ToolsPolicy, PermissionGroups)`）、`seelebridge/tools/permission_policy.go:410`（`classFor`）、`:414`（NodeScope）、`:647`（`ToolFaceForContext`） | 权限面按 ctx 解析、**按构造**放进 ctx（不靠反查）。这就是能力轴要照抄的"读法"。 |
| 硬收窄先例 | `seelebridge/runtime_role_turn.go:637-639`（`teammateToolFace`） | "装配即无此工具"：teammate 面硬移除 `fork_subagents`。能力轴按会话装配可复用同一姿势。 |
| 插件携带物 | `plugin/plugin.go:22` | `Plugin{Include, Exclude, Prompt, RootDir, MCPServers, Skills}` —— 三样东西：工具收窄、提示词、**进程级 MCP 连接**。 |
| 激活是全局事务 | `plugin/manager.go`（`Activate`/`Deactivate`/`attachLocked`/`detachLocked`/`Reload`） | `current` 单选；MCP attach/detach 是**进程级**连接，切换时旧插件先保持可用再拆。 |

### 1.2 今天的 skill 面：全局单选 + **teammate 侧目录为零**

| 层 | 位置 | 事实 |
|---|---|---|
| 单选状态 | `skill/skill.go:47-49` | `Registry{pluginSkills map[string]map[string]Skill, activePlugin string}`。 |
| 掩蔽语义 | `skill/skill.go:71`（`Get`）、`:93`（`All`） | `activePlugin != ""` → **只返回该插件技能，global/loaded 全被屏蔽**；空 → 返回 global。这是 default↔专用插件切换的全部机制（`plugins/default/README.md`）。 |
| 目录渲染（成本面） | `application/core/prompt_layer/skill_catalog.go:48`（`RenderSkillCatalog`）、`:22`（尾句写死 "belongs to the **active plugin**"）、`:34`（头 `## Available Skills`） | 每条一行 `- name: description`，**正文永不进目录**；纯函数、无 I/O。 |
| 目录接进主会话 | `application/core/prompt_layer/coordinator.go:140`（`SystemPromptForActiveTaskLockedFor`）→ `:164`（`skillCatalogPart()`） | 目录落在**主会话 system prompt**里，每轮都在。 |
| teammate 侧 | `seelebridge/runtime_role_turn.go:547`（`roleTurnSystemPrompt`） | 只给"已登记提示词 或 最小角色框架"；`seelebridge` 全域 grep `RenderSkillCatalog` = **0 命中**。⇒ **teammate 回合今天看不到任何技能目录**。 |
| teammate 不能自选 | `seelebridge/tools/permission_policy.go:174-178`（`GroupADM`：`switch_plugin`/`switch_mode`/`skill_activate`/`plugins_reload`）、`:195+`（默认授权表 sub/员工 `adm=0`） | teammate **没有**切插件、激活 skill 的位（连提权页面都到不了 ADM 这一族）。能力轴今天完全由宿主/leader 决定。 |

### 1.3 声明面（三处缺口）

| 面 | 位置 | 现状 |
|---|---|---|
| 角色注册 | `application/contract/dto/agentteam.go:82`（`RoleSpec`） | 有 `ToolsPolicy`（`:93`）、`PermissionGroups`（`:98`）——**权限轴已在**；**没有** plugins 字段。回读面 `TeamMember` 同缺。 |
| 角色写入侧校验 | `application/core/agentteam/spec.go:91`（`NormalizeRole`） | `ToolsPolicy` 枚举校验与权限格子校验都在这里（`:105`、`:116`），先例清晰：**写入侧拦注水**。 |
| 编排声明 | `seelebridge/runtime_teamwork_schema.go:19`（`teamworkPlanSchema`），members 属性见 `:36-46` | 只有 `role` / `role_session_id` / `worktree` / `tools_policy` / `permission_groups`。 |
| 编排落盘 | `sessionstore/teamwork.go:105`（`TeamworkMember`）、`:232`（`ValidateTeamworkPlan`） | 成员条目 + 第二道关口校验（角色重复/会话重复/里程碑图）。**注意：本层看不到插件目录**，不能在这里比名字。 |
| 编排解析 | `seelebridge/runtime_teamwork.go:256`（`teamPlanHandler`）、`:612`（`roleRoundSpec` 组装）、`seelebridge/teamwork/items.go:372` + `coordinator.go:125`（`member.*` → `WorkerRequest`，定义见 `seelebridge/teamwork/teamwork.go:68`，权限字段 `:79-80`） | 成员字段的完整透传链：`team_plan` → plan → `WorkerRequest` → `roleRoundSpec` → `runRoleRound`。**新字段必须走完这条链**，任一跳断掉就是"派发时带了、执行时丢了"。 |

---

## 2. 七条契约结论（逐条裁决，不留"看情况"）

### 契约 1 · 声明面

- **形状**：`RoleSpec.Plugins []string`（json `plugins,omitempty`）；`TeamMember.Plugins []string`（回读面）；
  `sessionstore.TeamworkMember.Plugins []string`；`teamworkPlanSchema()` 的 `members[]` 加
  `"plugins": {"type":"array","items":{"type":"string"}}`。
- **空/缺失 = 空集 = 继承宿主当前装配**（不是"禁用插件"）。语义见契约 4。
- **未知名必须显式拒绝**，不是静默忽略。先例：`seelebridge/plugin/plugin.go` 的 `Activate` 对未定义插件
  返回 `seelebridge: plugin %q is not defined`。拒绝文案统一走 `team_plan: 插件 %q 未定义（显式拒绝，不静默忽略）`。
- **校验分层（写死，别混）**：
  - **语法层**（去空、去重、单项非空、**每会话上限 3 个**）：`agentteam.NormalizeRole`（`spec.go:91`）+ `ValidateTeamworkPlan`（结构）；
  - **语义层**（名字必须存在于插件目录）：`seelebridge/runtime_teamwork.go:256 teamPlanHandler` 解析后即可比对（bridge 手里有 `r.plugins` 的 defs，`sessionstore` 没有、也不该 import 插件域）。
- **工作项不单独声明插件**：`team_work` 排的工作项继承其成员（role）的 `plugins`。

### 契约 2 · 生效面（工具可见性怎么算 / skill 怎么算）

- **工具可见性 = 权限面 ∩ 插件面（"与"，不是"或"）。插件只能收窄或保持，永不放宽。**
  理由：权即授权（`seelebridge/tools/permission_policy.go` 三条产品决定）；插件是"别人给的包"。
  计算式（多插件集合）：
  `Face(ctx) = { t ∈ 全量工具 | 既有 per-scope 规则通过 ∧ ToolFace(ctx,t) ∧ ∃p∈装配集: include_p 命中 t ∧ ∀p∈装配集: exclude_p 不命中 t }`
  —— 即 **include 取并集、exclude 取并集硬拆**，最后**与权限面相交**。include/exclude 的匹配语义沿用
  `seelebridge/plugin/plugin.go` 的 `matchToolPattern`（`path.Match`，与旧 holder 一致），不改。
  实现方向：**不新增"或"分支**，只把 `policy.go:82` 的收口换成带 ctx 的版本。
- **skill 可见性 = 按会话的集合**（`activePlugin string` → 该会话的 `pluginNames []string`），
  **但 root 路径保持单选不变**（`Registry.activePlugin` 与 `switch_plugin` 语义逐字不动）。
  新增的只是角色会话侧的一条按 ctx 读法（`Skills.ForAssembly(ctx)`）。
  掩蔽语义沿用 `skill/skill.go:93`：集合非空 → 只给这些插件的技能（global 仍被屏蔽）；集合为空 → 见契约 4。
- **注入分级**：目录（name+description，被动、每轮）与正文（Trusted Active Skill，按需）。
  本轮只做**目录**：`roleTurnSystemPrompt`（`runtime_role_turn.go:547`）末尾追加 `RenderSkillCatalog`。
- **不给 teammate 开 `skill_activate` / `switch_plugin`**（ADM 位保持 0）：能力可以装配，"选择权"不放出去。

### 契约 3 · 隔离性

- **按会话装配，不落全局态。** 解析器签名是 `func(ctx) []plugin.Def`，每轮现算，**禁止**在 `Policy` 上缓存"当前装配"
  （缓存就是并发丢失更新的另一面）。
- 身份来源：角色会话的 ctx 里**已经有** `RoleName`（`runtime_role_turn.go:267 WithEmployeeGrant`）、
  会话号（`:273` telemetry）与做工身份（`:275 withRoleRoundWorkScope`）。⇒ 装配面从 ctx 读，
  **不读** `Manager.active`。
- **并发回合**：两个 teammate 的 ctx 不同 ⇒ 面不同；`roleSessionHandle` 只有 `{id, role, engine}`，
  不含插件字段（不共享可变状态）。
- **主代理不受污染**：root 面（`NodeScope.NodeID == ""` 且非角色会话）仍走 `r.plugins.Filter`（读 `active`）；
  `switch_plugin` 仍是唯一的全局开关（`main.go` 的 `registerPluginSwitchTools`，工具注册在 `:224` 之外的生产面）。
  teammate 面是**另一条路**，两者互不写。
- **生效时机**：装配在派发/建会话那一刻定死；`team_plan` 改 `plugins` 对**已开**的角色会话
  **自下一轮生效**（工具面每轮从 ctx 重算，无需重建引擎）。这条要写进 `team_plan` 的 schema 描述里。
- 插件携带的 **MCP 与 Prompt 不进 teammate 面**（见契约 5 与 §5"本轮不做"）。

### 契约 4 · 回归（`plugins` 缺失/为空）

**逐字一致 ⇒ 空集的语义按"今天的字节"反推，而不是按直觉统一**：

- **工具面：空集 = 继承宿主当前装配**（今天 teammate 就是被宿主 active 插件收窄的）。
  实现上 `ctx` 解析不出集合即**直接早退到 `r.plugins.Filter`**（同一个闭包，不是等价实现）。
- **技能目录：空集 = 不注入**（今天 teammate 的 system prompt 里一条目录都没有，
  `roleTurnSystemPrompt` 返回什么就是什么）。
- 两者的"不一致"是**继承的事实，不是设计疏漏**：要能力必须显式声明 `plugins[]`。
  代价明写：**空集 teammate 看不到任何技能目录**；要"不受宿主收窄且无插件"，需要显式空数组语义，
  本轮不做（见 §5）。

**证法（三条，可跑）**：
1. **早退断言**：单测构造 `RoleSpec{Plugins: nil}` 的角色会话 ctx，断言插件面解析结果 == 旧闭包
   `r.plugins.Filter` 的结果（同一输入同一输出，逐元素比较）；
2. **字节断言**：同一 ctx 下 `roleTurnSystemPrompt(role)` 与改动前逐字节相等
   （`RenderSkillCatalog` 空集返回 `""`，`skill_catalog.go:52` 的既有口径）；
3. **既有用例不回归**：`seelebridge/runtime_account_test.go` 的 `ActivePlugin()`/可见工具用例、
   `skill`/`plugin` 包的 `Plugin|Skill|Layout` 用例组必须继续全绿（root 路径未动）。
   并加一条**反面用例**：装 `plugins:[a]` 的 teammate 的可见工具集 ⊆ 该 teammate 权限面（不能因插件变宽）。

### 契约 5 · 裁决——权限**不能**随插件走

**采纳外部的硬规则**（`docs/research/...-external-research.md` §1.2：从插件加载的子代理，`permissionMode`/`hooks`/`mcpServers` **一律忽略**）。

- **裁决**：插件只带能力 —— skills、提示词、工具**收窄**；`tools_policy` / `permission_groups` / MCP / hooks
  **一律不得由插件供货**，只能由宿主或 leader 在 `RoleSpec` / `team_plan` 里给。
- **理由三条**：
  1. 安全：插件是可被覆盖的补充（外部把插件定义放**最低**优先级，§1.4）。若插件能提权，"精选目录"就是提权发行渠道；
  2. 与现行架构一致：`seelebridge/plugin` 自述只是**投影缓存**、写路径只有 root 单入口；权限事实在 `PermissionGate`
     （主体 × 组 × 位）。两个域不许互相写；
  3. 可审计：权属注册表里一条可读行；若权限可由插件拼装，权限事实就要跨域拼。
- **代价（明写）**：插件作者不能自足声明"我这个能力需要写文件"，leader 必须手动把 `plugins[]` 与
  `tools_policy`/`permission_groups` **配对声明**。配错的后果是"能力在、位缺"——
  **位缺**在员工侧会落成提权页面（`permission_policy.go` 的既有兜底），**插件收窄**不会。
  这条差别要写进 leader 的 `$teamwork` 口径，否则会被读成"装了插件就有权限"。
- **反方（Record）**：若采纳"权限随插件走"，一装即提权，且与"精选可被覆盖、插件最低优先级"直接冲突。**不采纳**。

### 契约 6 · 两级优先序（写死）

**"具体覆盖泛化"，且要区分"取定义"与"取集合"两件事**：

1. **集合**（这个 teammate 用哪些插件）：
   `leader 显式声明（team_plan members[].plugins / RoleSpec.Plugins）` **替换**集合
   —— 给了就**只用**给的那些（不自动叠加角色自带的）；
   **不给（nil/空）→ 继承角色自带 → 角色自带也为空 → 继承宿主当前装配**。
2. **定义**（同名插件取哪一份 include/exclude/skills）：
   `角色自带` > `精选目录` > `宿主内建`
   （本轮只有"精选目录（本地 `plugins/`）∪ 宿主内建（`default`）"两层真实存在，第二层先只写口径、不实现新解析层）。
3. teammate **不许**改这一层：无 `plugins_reload` / `switch_plugin` 位（今天已由 ADM 断位保证，写进契约不要放开）。
4. 与历史裁决的关系：`docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md:71` 的"**全局**保持单选"
   **继续有效**——本设计只新增"按会话的集合"这条侧路，**不重开全局多插件叠加**（`:82` 仍不排期）。

### 契约 7 · 成本可见（name + description 从哪读）

两处字节都要给读数，装配界面/回执里报的是两者之和：

1. **技能目录**：直接调 `prompt_layer.RenderSkillCatalog(skills, sigil)`（`skill_catalog.go:48`，纯函数无 I/O），
   取 `len([]rune(输出))`。头尾固定（`:34` 头 + `:22` 提示语），每条 `- name: description`。
   主代理侧今天已经在算（`coordinator.go:164`），teammate 侧装上目录后**同一函数**即得。
2. **工具面**：`VisibleTools(ctx)` 的 schema 字节（name+description 每轮进请求）。
   既有读面即口径：`runtime.VisibleTools(ctx)` / `AllTools()`（`main.go` 的 `switch_plugin` 回执已用它们报
   `visible_tools`/`total_tools`），按序列化长度估算。
3. **装配闸门**：单会话装配的技能目录字节超过阈值即**显式拒绝**该次装配（对齐外部 15k token 警告的口径，
   阈值具体值由 exec 定；**口径必须有**）。每会话插件数上限 3（契约 1）也是这条的下位约束。

---

## 3. 实现拆分建议（exec 的最小改动清单）

按依赖序，`C7/C8` 是**唯一的行为改动点**，其余都是透传与校验：

| # | 文件 | 改动 |
|---|---|---|
| C1 | `application/contract/dto/agentteam.go`（`:82 RoleSpec`、`TeamMember`） | 加 `Plugins []string \`json:"plugins,omitempty"\``（两份）。 |
| C2 | `application/core/agentteam/spec.go:91` | `NormalizeRole` 里对 `Plugins` 去空、去重、单项非空、**上限 3**（语法层，**不**比对目录）。 |
| C3 | `sessionstore/teamwork.go:105 / :232` | `TeamworkMember.Plugins`；`ValidateTeamworkPlan` 只做结构校验（去重/非空/上限）。**不比对插件目录**（域边界）。 |
| C4 | `seelebridge/runtime_teamwork_schema.go:19` | `members[]` 加 `plugins`；描述里写"未知名显式拒绝 / 对已开会话自下一轮生效"。 |
| C5 | `seelebridge/runtime_teamwork.go:256` | `teamPlanHandler`：解析后逐个比对 `r.plugins` 的 defs，未定义即返回显式错误。 |
| C6 | `seelebridge/teamwork/items.go:372`、`coordinator.go:125`、`teamwork/teamwork.go:68`（`WorkerRequest`，权限字段 `:79-80`） | `Plugins` 走完透传链（派发时带了、执行时不许丢）。 |
| C7 | `seelebridge/tools/policy.go:16 / :81-82` | `PluginFilter func([]types.Tool) []types.Tool` → 带 ctx 的版本（如 `PluginFace func(ctx, tools) []types.Tool`），收口改一行；**nil 语义不变**（nil = 原样返回）。 |
| C8 | `seelebridge/runtime.go:401` | 注入改成按 ctx 的闭包：解析出非空集合 → 新面；解析不出 → **早退到 `r.plugins.Filter`**（契约 4）。 |
| C9 | `seelebridge/plugin/plugin.go` | 加**纯函数** `Face(defs []Def, tools []types.Tool) []types.Tool`（include 并集、exclude 并集硬拆）；`Filter` 保留不动（root 路径）。 |
| C10 | `seelebridge/tools/permission_policy.go:905`（`employeeFromContext`，**未导出**） | 导出一个只读访问器（如 `seeltools.EmployeeFromContext(ctx)`），供 C8 的闭包按角色解析装配（不要把 Policy 变成第二个权限门）。 |
| C11 | `seelebridge/runtime_role_turn.go:267` 一带 | 把成员的 `plugins` 也按构造放进 ctx（`withRolePlugins(ctx, names)`，与 `WithEmployeeGrant` 并列），工具面与目录都按轮现算。 |
| C12 | `seelebridge/runtime_role_turn.go:547` | `roleTurnSystemPrompt` 末尾追加装配集合的技能目录（`RenderSkillCatalog`）；**空集返回空串，system 字节逐字不变**。 |
| C13 | 用例 + 文档 | 四条：空集逐字一致 / 未知名显式拒绝 / 两个并发 teammate 装不同插件不串味 / 插件不能放宽权限面（`⊆`）。文档：`docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md` 补一行"按会话集合"的落地说明，`seelebridge/plugin/plugin.go:8` 的"当前为单选"改成新口径（全局单选 + 会话集合）。 |

---

## 4. 与历史裁决的相容性（为什么这不是推翻）

- `05-plugin-dual-track-decision.md:71` 说的是**全局激活态**保持单选 —— 本设计**不动** `Manager.current`、
  不动 `switch_plugin`、不动 `seelebridge/plugin` 的写路径（仍 root 单入口）。
- 新增的是**角色会话侧的只读投影**：按 ctx 的集合 → 收窄后的工具面 + 技能目录。
  root 侧不看它，teammate 侧不能改它（ADM 断位）。
- `:82` 的"多插件叠加不排期"仍成立：全局叠加没做；做的是"一个 teammate 的能力装配"。

---

## 5. 本轮不做（明确列出）

1. **MCP 轴**：插件携带的 MCP server 是**进程级连接**（`plugin/manager.go` 的 attach/detach、切换事务），
   按会话装 MCP 需要另一套连接生命周期与命名空间隔离 —— 本轮只登记为未做。
2. **插件 `Prompt` 与 teammate**：`main.go:1103 applyPluginPrompt` 对主会话是**整段替换 system prompt**；
   照搬到 teammate 面会覆盖员工提示词（`roleTurnSystemPrompt` 的登记提示词优先语义）。本轮不接。
3. **teammate 的 `skill_activate` / `switch_plugin`**：ADM 位保持 0，能力可装配、选择权不下放。
4. **同名插件的定义覆盖实现**：契约 6 只把优先序写死；本轮只有"精选目录 ∪ 宿主内建"真实存在，不实现新解析层。
5. **显式空数组语义**（`plugins: []` = 不受宿主收窄）：需要指针字段区分"缺失/为空"，本轮不做（契约 4 已写代价）。
6. **精选目录 / marketplace 的收集**：用户已明确"后期收集"（外部调研 §4）。
7. **前端装配界面与成本读数 UI**：本轮只定"读数从哪来"（契约 7）。
8. **不动 `sessionstore` 的写接口与单次写语义**（沿用既有裁决）。

---

## 6. 未核 / 边界（诚实标注）

- `seelebridge/plugin/plugin.go` 内 `Filter` 与 `matchToolPattern` 的具体行号未逐行核（文件 `/plugin` 关键字 glob 在本 worktree 不可用）；
  语义按整文件实读（未激活 → 原样返回；`path.Match` 通配）。
- `plugin/manager.go` 的 `Activate`/`attachLocked` 行号未逐个核；MCP attach/detach 的"进程级"性质来自整文件实读。
- 未验证：同一进程内**并发**角色回合 + 装配集合解析的实测行为（本设计只给口径，未跑用例）。
- 未验证：`r.plugins` 在 `team_plan` handler 时刻一定已 `Define` 完（启动装配顺序），C5 的拒绝路径据此成立；
  exec 落地时应用一条用例钉住"未加载插件目录时 `plugins[]` 一律显式拒绝"。
- 未做的对照组：外部（Claude Code）的 `skills` 前置全量注入 vs 目录按需激活，取舍留给产品（本设计默认目录，见契约 2）。
