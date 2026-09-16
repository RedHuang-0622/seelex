# 主会话权限档位（Session Permission Tier）方案

> 需求（用户第 4 点）：**主会话的权限设置要做出不同挡位；挡位做成列表放在相关面板里
> 让用户选；且这是主 agent 在这个 session 的粒度。**
>
> 本文只给**方案 + 落地路径 + 决策点**。日期：2026-09-16。
> **状态：已落地**（同日）——落地方案与验证证据见
> [`docs/devlog/2026-09-16-session-permission-tiers.md`](../../devlog/2026-09-16-session-permission-tiers.md)；
> 决策按 §10 默认建议执行（chip 就地放在原"全权"位置 = D3「打开列表」）。
> 前置事实链：`docs/arch/agent-permission-subjects.md`（权责模型长期口径）、
> `docs/2026-09-16-ring-escape-permission-bearing/CHANGES.md` §3（权限拦截承重面）、
> `docs/devlog/2026-09-16-employee-inherit-approval-panel.md`（员工继承 → 审批面板）。

---

## 0. 一句话

> 把现在的**二元 `full_access` 布尔**升格成**有序的权限档位表**（`manual / edit / auto / full`），
> 档位是**对主 agent 主体（root）的声明式覆盖**（剪掉哪些 `ask`），
> **会话级归属**（沿用 `SessionUnit` 槽 + `PermissionGate` 按会话解析），
> 在**运行状态面板**里以列表呈现可选，**只对 main（root）生效**——
> 员工主体与子代理主体的判定链一字不改（因此"主会话全权只管主会话，员工越权照旧走审批提权"）。

---

## 1. 现状取证（可复核的事实）

| # | 事实 | 证据（真实符号） |
|---|------|------------------|
| E1 | 权限只有**两档**：`-permission manual | full_access` | `main.go:parsePermissionMode`（只接受 `manual`/`full_access`）、`setupPermissionGate` |
| E2 | 运行期"全权"是**一个布尔**，且已经是**会话级** | `application/model/state.go:208` `RuntimeState.FullAccess`、`:647` `SessionRuntime.FullAccess`；`session/runtime_slot.go:52-72` `FullAccessMode/SetFullAccessMode`；`session/ports.go:228-232` |
| E3 | 会话级解析链路完整：Unit 槽 → Service → 执行门按会话短路 | `application/core/session_scope.go:153` `fullAccessForSession`、`:178` `syncFullAccessFor`；`application/core/service_interaction.go:237` `Service.SetFullAccess`；`seelebridge/tools/registry_state.go` `sessionFullAccess`/`SetFullAccessFor`/`FullAccessFor` |
| E4 | "全权"在判定链里是**短路**，不是一档策略：非 sub 主体直接 `ActionAllow` | `registry_state.go:PermissionGate.Enforce`：`if class != SubjectClassSub && FullAccessFor(...) { return ActionAllow, true }` |
| E5 | 工具体系**已经分组**、且规则**已经有最细粒度覆盖层** | `seelebridge/tools/permission_policy.go:DefaultPermissionGroupList`（ro / rw / rw_session / rw_desktop / ctl / adm）、`DefaultPermissionRules`（LMRW：兜底 ask → 白名单 allow → 危险 deny） |
| E6 | 前端只有**一枚二元 chip**，没有列表 | `gui/frontend/dist/index.html:78` `<button id="perm-toggle" …>全权</button>`；`app.js:1389` `renderRuntime` 渲染、`app.js:~3277` toggle → `invoke("SetFullAccess", next)` |
| E7 | 行业已收敛到"多档自动度"，本项目已把它标为 **P0** | `docs/research/2026-09-11-seelex-harness-source-review-and-comparison.md:27`（Claude Code 六档）、`:202`（P0 权限只有两档） |
| E8 | 当前 `full_access` **溢出到员工**：短路条件只排除 `sub` | E4 的 `class != SubjectClassSub`；`docs/devlog/2026-09-16-employee-inherit-approval-panel.md` §5.3 自述"全权语义外溢"是已知风险 |

**结论**：E5 说明"档位"不需要新机制——它就是**对已有 分组表 + 规则表 的一层声明式覆盖**；
E2/E3 说明"会话粒度"的管道**已经建好**，档位顺着同一条管道走即可；
E4/E8 说明必须把"短路"从"非 sub"收紧为"**仅 root**"，才满足"全权只管网主会话"。

---

## 2. 模型：一个档位 = 对 root 的一层覆盖

```text
档位（tier） = 有序预设，只作用在 root（主 agent）主体上，对 base 权责表做两件事之一：
  A. BypassAll = true          → 会话级短路（= 今天的 full_access，等价"免审 sudo"）
  B. 剪掉若干 baseline ask 规则 → 该工具落回它所在路由组的默认动作（rw 组默认 allow）
```

**关键不变量：档位只"剪 ask"，从不"加 allow 在危险 deny 之后"。**
`DefaultPermissionRules()` 的顺序即语义（LMRW 最后匹配胜出）：`write/edit/plugin/skill` ask →
`bash` 兜底 ask → `bash` 白名单 allow → `bash` 危险 ask → `bash` 危险 **deny**（最后）。
档位覆盖 = 从这份表里**删除**要放开的 `ask` 规则；危险 deny 段永不被覆盖。
这样"放开"只可能发生在 deny 之前，`rm -rf /`、`dd if=* of=*`、`mkfs*` 在任何档位下都硬拦。

---

## 3. 档位表（面板列出的那个有序列表）

默认 `manual`；升序 = 自动度递增。

| # | id | 面板标签 | 覆盖（对 base 权责表） | 用户体感 |
|---|----|----------|------------------------|----------|
| 0 | `manual` | 手动（默认） | 无 | 现状：写文件/命令/桌面/能力面都问人 |
| 1 | `edit` | 自动改文件 | 剪掉 `write_file` / `edit_file` 的 ask | 项目文件写不再打断；命令仍按白名单，桌面/能力面仍问人 |
| 2 | `auto` | 自动执行 | 再剪掉 `bash` 的兜底 ask + 危险 ask（保留危险 deny） | 文件写 + 任意命令直跑（除危险命令）；桌面/能力面仍问人 |
| 3 | `full` | 全权（免审） | `BypassAll`（= 今天的 `full_access`） | 全部放行 |

**刻意不放进中档的两族**（都留在"问人"，只有 `full` 才放开）：

- `rw_desktop`（共享外设）：所有并行代理共用**一块物理桌面**，逐次确认是安全底线；
- `adm`（能力面：换插件/skill/reload/MCP）：改变的是"有哪些工具"，等于预先扩权。

可选第 5 档（同机制，一行数据即可加）：`readonly` 计划/只读——把 `rw` 组默认从 allow 压成 ask
（ctl 不动，否则 `plan_*`/`task_*` 会挂住循环）。

> 档位表放在 `application/contract/dto/permission.go`（与 `PermissionGroupNames`/`PermissionBit*`
> 同一份跨层词表的既有惯例）：**id/标签/说明是 dto 的唯一事实**（前端要按它渲染列表），
> **覆盖实现（剪哪条规则）在 `seelebridge/tools`**（它拥有 `DefaultPermissionRules`）。
> 与"组名的唯一事实在 dto、组的分封在 seelebridge"完全同构。

---

## 4. 判定链与主体边界（本方案的核心收口）

一次工具调用的求值顺序（在现有 `Middleware` → `Gate.Decide` 上只加"选哪张表"）：

```text
Middleware(ctx, name, meta)                    # seelebridge/tools/registry_state.go
  ├─ 主体解析：节点作用域(sub) → 角色会话(emp_*) → 否则 root      （不变）
  └─ Gate.Decide(ctx, name, meta, args)
       ├─ 1. Enforcer.Enforce(...)             # PermissionGate
       │      ├─ class == root 且 tier.BypassAll → allow（短路）   ← 收紧：原来是 class != sub
       │      ├─ class == sub      → 子代理口径（不变）
       │      ├─ class == emp_*    → 员工口径（位缺 → 审批提权；不变）
       │      └─ 其余（root、非 full 档）→ ok=false 落回框架
       ├─ 3. Checker.DecideForMeta：
       │      checker = (class == root) ? tierChecker(session) : baseChecker   ← 新增：按主体选表
       └─ 4. allow 执行 / deny 拒绝 / ask 执行选择页面
```

两条**必须成立**的边界：

1. **档位只改 root 的"问不问"，不改任何主体的"有没有位"。**
   工具的**可见面**（`ToolFaceForContext` / `ToolFaceForSubject`，按位算）与档位无关——
   `edit`/`auto` 不会把员工的写工具摆到面上，也不会把某个工具从谁的面上去掉。
2. **非 root 主体一律用 base 表。** 这是"全权只管网主会话"的落点，也是修掉 E8 的钥匙：
   若员工的判定落回 tierChecker，`auto`/`edit` 会让 `emp_rw` 的 `write_file` 从"审批提权"
   静默变成"直接放行"——正是用户第 3 点禁止的。因此 `gate()` 按 class 选表：
   `class == root → tierChecker(session)`，其余 → `baseChecker`。

**子代理（sub）完全不受影响**：档位不改变它的 `ctl=0/adm=0` 断位，也不改变"没有人类在环 →
ask 一律直接拒绝"的口径（`enforceSubAgent` 不变）。

---

## 5. 数据面与接口（会话粒度的管道，顺着现有链路走）

```text
dto.PermissionTierInfo{ID,Label,Short,Description}   +  dto.PermissionTiers()   # 跨层词表（新）
        │
        ├─ seelebridge/tools: DefaultPermissionTiers() + ApplyTier(base, tier)  # 覆盖实现（新）
        │        └─ PermissionGate.tierCheckers map[string]*Checker             # Set 时预构建（新）
        │           按会话解析 tier → 选表（gate() 内）
        │
        ├─ session.SessionUnit: PermissionTier()/SetPermissionTier(tier)        # 会话槽（新，与 Effort/FullAccess 同层）
        │
        ├─ application/contract.RuntimePort: PermissionTier()/SetPermissionTierFor(sid,tier)（新）
        │        └─ application/core.Service: SetPermissionTier(tier) (string, error)  # 视图会话
        │            + session_scope.syncPermissionTierFor(sid)                 # chat 起点按会话同步
        │
        ├─ application/model: RuntimeState.PermissionTier + SessionRuntime.PermissionTier（新）
        │        （FullAccess 保留为派生：tier=="full"，保前端与诊断兼容）
        │
        └─ gui.Bridge: SetPermissionTier(tier) (string, error)                  # 返回真正生效值
                 └─ 前端 app.js：运行状态面板「会话权限档」列表 + composer chip
```

要点：

- **单一事实**：生效档位 = `SessionUnit` 槽（会话选择）→ 未选择回退**进程默认档**
  （`-permission` 映射）。与 `fullAccessForSession` 完全同源，不引入第二套解析。
- **Set 时预构建** `tierCheckers`：`PermissionGate.Set` 已经重建 checker，顺手为每个档位构建
  一份（base + ApplyTier），运行时只做 map 命中 → O(1)，不引入每调用的规则重算。
  运行期 `SetEmployeePermissions`/`ensureEmployeeSubject` 重建 checker 时，**subject 面变、规则面不变**，
  因此按同一份 tier overlay 重建所有档位的 checker。
- **进程默认档**：`-permission manual|full_access` 继续可用（映射 `manual`/`full`），
  扩展接受 `edit|auto`（`parsePermissionMode` 放宽为"档位 id 或旧别名"）。
- **`SetFullAccess*` 保留为兼容壳**：`true → tier full`、`false → tier manual`。
  现有 GUI chip、CLI、以及 `permission_session_isolation_test.go` 等用例的语义不变。

---

## 6. 前端（"列表放在相关面板里"）

### 6.1 主位：运行状态面板（`#runtime-modal`）

在「运行状态」弹窗里新增一节「**会话权限档**」：按 `dto.PermissionTiers()` 的**有序列表**渲染
可选项（label + 一行 description + 当前档标记），点击 → `invoke("SetPermissionTier", id)` →
按返回的生效值渲染 → `refresh()`。

- 数据来源是**本会话**的 `runtime.permission_tier`（`SessionSnapshot.Runtime` 是会话粒度，
  切会话即换档位显示）——这正是"主 agent 在这个 session 的粒度"。
- 列表文案由**后端目录**下发（不是前端硬编码），避免前后端各写一套档位名漂移。

### 6.2 次位：composer 的 `#perm-toggle` chip 改为"档位指示器"

- 渲染当前档短名（`手动 / 自动改文件 / 自动执行 / 全权`），全权档保留 ✓ 记号。
- 交互二选一（决策点 D3）：**点击循环切档**（manual→edit→auto→full→…，最快）
  或**点击打开上面的列表**（最不易误触）。
- 现有"返回真正生效值再渲染"的做法保留（`app.js:3277` 的 `renderFullAccessChip` → 改名
  `renderPermissionChip(tier)`），避免"点了没生效"的滞后观感。

> 面板落点备选：右栏「状态」子页（与 `full_access` 同为会话事实，只读镜像一行）。
> 建议先只在运行状态面板做（一处权威、一处入口），状态子页作为可选镜像。

---

## 7. 与需求 1–3 的关系（同一条链）

| 用户点 | 本方案如何承接 |
|--------|----------------|
| 1 员工权限来自全局员工数据（可继承） | 不动：员工主体 `emp_<角色>` 的位来自装配表/员工库（`EmployeePermission`/`PermissionGroups`）。档位不产生员工条目。 |
| 2 前端标明"哪个员工用了工具" | 不动（另一件事）；档位只决定 root 的问/放，工具归属标注读的是调用主体。 |
| 3 **主会话全权只管网主会话；员工越权照旧审批提权** | **本方案的核心收口**：`Enforce` 短路条件 `class != sub` → `class == root`；且非 root 一律用 base 表（§4）。员工越权仍走 `enforceEmployee` → 审批页（归属折算到宿主主会话，已是既有能力）。 |
| 4 主会话权限档位 + 面板列表 + 会话粒度 | §2–§6。 |

> 顺带修掉 E8 的既有外溢：今天 `full_access` 会让员工的越权请求被 broker 的会话级自动放行；
> 收紧后 `full` 档仍只作用于 root，员工越权回到"人类点头"。

---

## 8. 边界与非目标

- **不改框架（Seele）**：只用现有 `permission.Gate`/`Checker`/`BitEnforcer`/`PermissionConfig`
  能力；档位是产品侧的组合，不进框架语义（框架侧 `R5 elevate` 是更远的"一次一授权/审计"，
  与本档位正交，本方案不依赖它）。
- **不改工具可见面语义**：可见性仍由"位"决定；档位不增删任何主体的位。
- **不改子代理**。
- **不落盘（与现状一致）**：`effort`/`full_access` 今天都是会话槽内存态（只有 `Composer` 落盘，
  见 `session/ports.go:228` 附近与 `composer_draft.go`），档位沿用同一口径；跨重启记忆列为决策点 D5。
- **不做 OS/容器隔离**：档位是"问不问人"，不是沙箱。

---

## 9. 验收与测试

**seelebridge/tools（判定矩阵，table-driven）**

- root × 4 档 × 代表工具：`manual` 与**今天的 base 判定逐条一致**（复用/对齐
  `permission_config_test.go:TestMainAgentToolDecisions` 作为回归钉）；
  `edit`：`write_file`=allow、`plugin_create`=still ask、`bash "rm -rf /"`=deny；
  `auto`：`bash "python x.py"`=allow、`bash "make build"`=allow、`bash "rm -rf /"`=**deny**、
  `computer_click`=ask、`mcp_load`=ask；`full`=短路 allow。
- **主体边界**：给定任意档位，`sub` 与 `emp_rw` 的判定结果**与档位无关**（`emp_rw` 的
  `write_file` 在 `auto` 下仍走审批提权，不得静默放行）——这一条直接钉住需求 3。
- **会话隔离**：A 会话设 `full`，B 会话仍是 `manual`；B 的 chat 起点同步不得改 A 的档位
  （对齐 `permission_session_isolation_test.go`）。

**application/core**

- `SetPermissionTier` 只写**视图会话**槽 + 投影 `Runtime.PermissionTier`/派生 `FullAccess`；
  切会话读到各自档位；非法档位 id → 报错且不改变当前档（对齐 `NormalizePermissionGroups` 的写入侧校验口径）。
- `SetFullAccess(true/false)` ⇔ tier `full`/`manual`（兼容壳的等价断言）。

**gui**

- Bridge `SetPermissionTier` 转发 + 返回生效值；
- 前端契约：目录列表按序渲染、当前档标记、点击提交后按返回值渲染（`node --test` 用例）。

**可复现命令（计划）**

```powershell
go vet ./... ; go build ./...
go test ./seelebridge/tools/ -run "Tier" -v -count=1
go test . -run "TestMainAgentToolDecisions|TestEveryRegisteredToolIsRouted" -count=1
go test ./application/core/ -run "Tier|FullAccess" -count=1
go test ./gui/ -run "PermissionTier" -count=1
```

---

## 10. 决策点（需你拍板，方案已给默认建议）

| # | 问题 | 建议 |
|---|------|------|
| D1 | 档位集合与命名：4 档（`manual/edit/auto/full`）是否够？ | 先用 4 档；`readonly` 留作可选第 5 档，同机制随时加 |
| D2 | `auto` 是否也放开**共享桌面**（`rw_desktop`）？ | **否**：桌面与 `adm` 只在 `full` 放开（共享外设的安全底线） |
| D3 | composer chip 行为：循环切档 vs 打开列表？ | 打开列表（列表是权威入口，chip 只显示当前档 + 一个入口）——若你偏好"最快"，则循环切档 |
| D4 | 列表放在哪个面板？ | 运行状态（`#runtime-modal`）主位；右栏「状态」子页可选镜像 |
| D5 | 档位是否跨重启记忆？ | 先与 `effort/full_access` 一致**不落盘**；要记忆则改会话 record（照 `Composer` 的持久化路径） |
| D6 | 需求 3 的收紧（`full` 不再对员工短路）确认为**行为变更** | 确认按需求收紧（今天 `full_access` 会连带放行员工越权） |

---

## 11. 改动清单（预估，按落地顺序）

| 层 | 文件 | 改动 |
|----|------|------|
| 词表 | `application/contract/dto/permission.go` | `PermissionTier*` 常量 + `PermissionTiers()` 目录 |
| 判定 | `seelebridge/tools/permission_tiers.go`（新） | `DefaultPermissionTiers()` + `ApplyTier()` + 按档构建 checker |
| 判定 | `seelebridge/tools/registry_state.go` | `PermissionGate.tierCheckers`；`Set` 预构建；`gate()` 按 class 选表；`Enforce` 短路收紧为 `class == root`；`SetPermissionTierFor/For` 与 `SetFullAccess*` 兼容壳 |
| 会话 | `session/runtime_slot.go`、`session/ports.go` | `PermissionTier/SetPermissionTier` 槽（`FullAccessMode` 转派生） |
| 应用 | `application/contract/ports.go`、`application/core/{service_interaction,session_scope,chat}.go`、`application/model/state.go` | Port/Service/投影/chat 起点同步 |
| bridge | `seelebridge/runtime_tools.go` | 透传 |
| 组合根 | `main.go` | `parsePermissionMode` 放宽为档位 id；默认档 = manual |
| 前端 | `gui/frontend/dist/{index.html,app.js,styles.css}` + `*.test.mjs` | 运行状态面板列表 + chip 改造 |
