# 工具权责模型：主体 × 路由组 × 位（Linux 式）

> 稳定口径：**谁在调用（主体）× 调用哪一族工具（路由组）× 持有什么位（rwx）**，
> 用 Linux 的 user / group / mode / sudo 语义统一 agent 的权限判定。
> 本文只写**长期有效的架构事实**；一次性设计与框架需求单见
> [`docs/2026-09-15-agent-permission-routing-groups/`](../2026-09-15-agent-permission-routing-groups/README.md)，
> 当次接入的落地差异见该目录的 `seele-integration-status.md`。

日期：2026-09-15 · 状态：**当前实现**（§7 明确列出目标设计中仍未落地的部分）

---

## 1. 生态位与代码落点

| 层 | 落点 | 职责 |
| --- | --- | --- |
| 框架（Seele） | `tools/permission/{checker,middleware,approval}.go` | 组/位/规则/审批的**判定与错误语义**；`Gate` 是唯一判定入口 |
| 框架 | `tools/tools.go` `ToolMeta{Kind,Groups,Bits,Resource}` | 工具自报门第（**可选**，见 §7） |
| 产品（Seelex） | `seelebridge/tools/permission_policy.go` | 授权表（路由组 + 主体位）、主体类解析、主体类策略（Enforcer） |
| 产品 | `seelebridge/tools/permission_tiers.go` | **主会话权限档位**的覆盖实现（`ApplyTier`：只剪 ask，不碰 deny） |
| 产品 | `seelebridge/tools/registry_state.go` | 把框架 `Gate` 接进工具调度链；会话级档位选表 + `full` 档短路 |
| 组合根 | `main.go`（`setupPermissionGate` / `mergePermissionConfig` / `newPermissionBridge` / `SetRoleSessionPolicyResolver` / `SetRoleSessionOwnerResolver`） | 装配：默认表 + `config/seele.yaml` 覆盖 + 审批桥 + 员工 ToolsPolicy 读面 + 审批归属读面 |
| 配置 | `config/seele.yaml` | **只写覆盖**（`missing_bit` + 最细粒度 `rules`） |

分工的一句话：**框架管"位语义"，产品管"位落在哪片资源、以及各主体的默认授权"**。

## 2. 模型

### 2.1 主体（谁在调用）—— Linux 的 user 维度

| 主体 id | 来源（`ClassForNodeScope` / `roleSessionClass`） | Linux 类比 |
| --- | --- | --- |
| `root` | 无节点作用域 / 主代理 / plan entry / goalplan 节点 / 未登记会话 | uid 0（sudo） |
| `sub` | 调度 ctx 带 `NodeScope{NodeID, Role: RoleSubAgent}`（`node/agent_node.go` 注入） | 普通用户 |
| `emp_ro` | 角色会话（员工）+ `RoleSpec.ToolsPolicy = readonly` | 受限用户（只读） |
| `emp_rw` | 角色会话（员工）+ `RoleSpec.ToolsPolicy = readwrite` | 受限用户（可写项目） |

主体写在调度 ctx 上（`WithEngine` + `WithSessionID`），**不落在共享字段**：并行子代理 /
并行会话同时调工具时不会互相污染。

#### 2.1.1 审批归属：呈现面与判定面解耦

主体（`WithEngine`）决定**按谁判**；会话归属（`WithSessionID`）只决定**审批弹在哪**。
两者刻意分开：

- **判定面**读 `SessionFromContext` 的**原始**调度会话（角色会话号）+ ctx 里的员工身份；
- **呈现面**在写 ctx 前先按 `PermissionGate.RoleSessionOwner` 折算：角色会话
  （`goal-a2a-<role>` / `advisor:<main>`）→ **宿主主会话**（`app.RoleSessionOwner` 反查索引，
  与 `SetRoleSessionPolicyResolver` 同一份事实）。

为什么必须折算：角色会话不是用户视图里的会话，`observeInteraction` 只把"视图会话（或空
归属）"的待批镜像进单格 `Snapshot.Interaction`，且角色会话没有会话单元承载目录的
`awaiting_approval`——不折算 = 员工越权提权对宿主完全不可见（只能等审批超时被拒）。
折算后，现有面板的三条读面（单格弹窗 / 目录徽标 / `SessionSnapshot.Approvals`）原样复用。
nil / 未命中的读面保持旧的按会话归属（后台会话待批仍归自己，不冒充别人）。

### 2.2 路由组（哪一族）—— Linux 的 group 维度

按工具名 glob 路由，LMRW（最后匹配胜出，组之间不重叠）：

| 组 | mode | resource | 默认动作 | 语义判据 |
| --- | --- | --- | --- | --- |
| `ro` | `r--` (4) | — | allow | 不改任何共享状态（含桌面**观察**） |
| `rw` | `rw-` (6) | `project` | allow | 写项目文件 / 自有工作台 |
| `rw_session` | `rw-` (6) | `session` | allow | 写本会话可变 transcript（`compact_context`） |
| `rw_desktop` | `rw-` (6) | `desktop` | ask | 写**共享外设**（一块桌面，所有并行代理共用） |
| `ctl` | `--x` (1) | — | allow | 改变 agent 循环的控制流（结束/挂起/派生/装载执行结构） |
| `adm` | `rwx` (7) | — | ask | 改变**能力面本身**（换插件/Skill/reload/MCP） |

两个刻意的归组口径（与设计稿的差异，实现为准）：

- `goal_begin/goal_update/goal_propose_finish` 归 `ctl` 而不是 `rw`：goal 栈是主代理的
  治理状态，现有 `policy.go` 对子代理整族不可见，跟着 `ctl` 断位更贴合既有口径。
- `computer_wait` 归 `ro` 且 allow（纯节流无副作用）；`computer_screenshot/windows` 是
  `ro` 但默认 ask（画面进上下文）。

### 2.3 位与缺位 —— 主体 × 组的授权表

| 主体 | `ro` | `rw`(project) | `rw_session` | `rw_desktop` | `ctl` | `adm` |
| --- | --- | --- | --- | --- | --- | --- |
| `root` | 4 | 6 | 6 | 6 | 1 | 7 |
| `sub` | 4 | 6 | 0 | 0 | 0 | 0 |
| `emp_ro` | 4 | 0 | 0 | 0 | 0 | 0 |
| `emp_rw` | 4 | 6 | 0 | 0 | 0 | 0 |

- **位齐** → 按组的默认动作（allow / ask），再被 `rules` 覆盖。
- **位缺** → 该工具对**该主体不可路由（= 不在 PATH）**，不是"调用时报错"。缺位在配置里
  由 `permission.missing_bit` 表达（当前 = `ask`），子代理 / 员工侧由主体类策略改判（§4）。
- `root` 全位：位这一层对主代理是**无新增限制**的（等价旧行为），判定差异只来自
  §2.2 的组默认与 §5 的规则。

### 2.4 最细粒度覆盖（`rules`）

`rules` 是 args 级的最后一道覆盖（LMRW），顺序即语义：**先兜底 ask，再安全 allow，
最后危险 deny**。命令行类工具（`bash`）的 `patterns` 匹配的是**命令本体**，不是工具
入参 JSON —— 由 `seelebridge/tools/registry_state.go:policyArgsFor` 归一化，
否则"能力白名单"整块失效。

`config/seele.yaml` **不写** `tool: "*"` 的兜底 ask：那会把所有组的默认动作覆盖掉。

### 2.5 权限档位（主 agent 的会话粒度覆盖）

档位（tier）是 **root 主体在本会话的自动度**，不是新判定机制：它是把 §2.4 的 `rules`
按档位**剪掉若干 `ask`** 的一层声明式覆盖（`ApplyTier`），**从不新增 allow、从不触碰
deny**（危险命令段在任何档位下都硬拦）。

| 档位 id | 覆盖 | 语义 |
| --- | --- | --- |
| `manual`（默认） | 无 | 完全按权责表问/放 |
| `edit` | 剪 `write_file`/`edit_file` 的 ask | 项目文件写不再打断 |
| `auto` | 再剪 `bash` 的全部 ask | 任意命令直跑（危险 deny 仍在） |
| `full` | 执行门短路 | 本会话全部放行（= 旧 `full_access`） |

- 词表（id/标签/说明）的唯一事实在 `application/contract/dto`（前端按它渲染列表）；
  覆盖实现在 `seelebridge/tools/permission_tiers.go`。
- **只对 root 生效**：`gate()` 按"主体类 + 会话档位"选表——非 root（`sub`/`emp_*`）
  一律用 base 表；`full` 档的短路条件收紧为 `class == root`。因此档位只改主 agent 的
  "问不问"，不改任何主体的"有没有位"（员工越权照旧审批提权）。
- **会话粒度**：档位选择落在 `SessionUnit` 槽（`PermissionTier/SetPermissionTier`），
  执行门按调度 ctx 的会话解析；A 会话切档不替 B 放行，B 的起点同步也不关掉 A。
- 前端：composer chip 就地显示当前档短名（位置 = 原"全权"chip），运行状态弹窗里的
  「权限档位（本会话）」列表是权威选择入口；档位目录由后端（`RuntimeState.PermissionTiers`）下发。
- `SetFullAccess(true/false)` 保留为兼容壳（⇔ `full`/`manual` 档）。

## 3. 一次调用的求值顺序

```
Middleware(ctx, name, meta)                 # seelebridge/tools/registry_state.go
  ├─ 主体解析：节点作用域（sub）→ 角色会话登记（emp_*）→ 否则 root
  ├─ ctx 注入 WithEngine(subject) + WithSessionID(session)
  └─ Gate.Decide(ctx, name, meta, args)     # 框架
       ├─ 1. Enforcer.Enforce(...)          # 产品挂载点（位与沙箱/路径的交界）
       │      ├─ class == root 且 full 档 → allow（短路；员工/子代理不享，见 §2.5/§4）
       │      └─ 主体类策略（sub / emp_*，见 §4）；root 非 full 档 → ok=false 落回框架
       ├─ 2. meta.Kind == control && subject != root → 不可见（目标设计，§7）
       ├─ 3. Checker.DecideForMeta：route(name) → 位与/资源 → 组默认 → rules
       │      （root 用**本会话档位**的 checker；sub/员工用 base checker）
       └─ 4. allow → 执行；deny → 拒绝；ask → 执行选择页面（审批）
```

`root` 的判定与接线前的产品口径**逐条一致**（由
`permission_config_test.go:TestMainAgentToolDecisions` 钉住），差异只有三处、
且都是有意收口：

1. `ctl` 簇与 `ro/rw` 常规工具：从旧配置的 `"*" → ask` 兜底变成组默认 allow（少弹窗）；
2. `mcp_load`：从显式 allow **收紧**为 `adm` 组默认 ask（连 MCP = 扩能力面）；
3. `bash` 的 args 级白名单/危险模式真正生效（`git status` 直跑 / `python x.py` 问人 /
   `rm -rf /` 直接拒）。

## 4. 三条产品口径（不是可选配置）

| 主体 | 口径 | 实现 |
| --- | --- | --- |
| `sub` | **没有人类在环**：任何"需要问人"的调用（组默认 ask / 规则 ask / 缺位 / 未分封）一律**直接拒绝**，绝不挂起等一个不会有人回答的页面；位齐 → 放行（它的作用域另有 worktree / ProjectScope 收窄）；显式 `deny` 仍然硬 | `permission_policy.go:enforceSubAgent`；审批处理器对 sub 置 nil |
| `emp_*` | **有宿主人类**：位缺（违权）→ 走**执行选择页面提权**（人类的选择页 = sudo 口令）；位齐 → 交回框架（组默认/规则）；显式 `deny` 不因提权页面变软 | `permission_policy.go:enforceEmployee` |
| `root` | 完全落回框架（组默认 + rules），位这一层只多一道"位必须齐"而 root 全位 | `Enforce` 返回 `ok=false` |

会话级 `full` 档降级为 `Enforcer` 里的**短路**（`PermissionGate.Enforce`）：
它是"本次会话不做位与规则判定"的用户显式决定，**不越会话传播**，**不把一个无权主体
（sub/员工）变成有权主体**（短路条件 = `class == root`），也是 §2.5 档位表的最右一档。

## 5. 错误语义（调用方与前端可依赖）

| 语义 | 判定 | 错误 | 归类 |
| --- | --- | --- | --- |
| 该主体**不可路由**（位缺 / 对 sub 的隐藏） | `visible == false` | `ErrToolNotVisible` | "这工具不在你的 PATH 上" |
| 有权但被策略拒（显式 `deny`、页面拒绝） | `ResultDeny` / 页面 deny | `ErrPermissionDenied` | EPERM |
| 需要人类口令 | `ask`（有审批处理器） | 阻塞在选择页面 | 人类放行/拒绝 |

`DenyWithoutPrompt: true` 是 seelex 的显式选择：配置里的 `deny` 维持"立即拒绝"，
只有 `ask` 才呈现选择页面。要采纳框架默认的"拒绝也走选择页面"，去掉这一个开关即可。

## 6. 装配与配置

```
默认权责表（代码）  DefaultPermissionConfig() = Groups + Subjects + MissingBit + Rules
        ↓ 叠加（只覆盖显式给出的字段，缺失保持默认）
config/seele.yaml  permission.{missing_bit, rules}      # 只写覆盖
        ↓
PermissionGate.Set(cfg, handler)                        # 重建 checker + 保留 cfg 快照
        ↓
运行时选项：会话级权限档位（-permission manual|edit|auto|full，旧别名 full_access → full；
             GUI/CLI 按会话切换；full = 执行门短路）
员工读面：SetRoleSessionPolicyResolver(角色会话 → ToolsPolicy)，TTL 缓存 5s
```

「只覆盖不替换」很重要：分组表与主体授权表是**产品口径**（谁能用哪一族工具），
不该因为一份只写了规则的旧配置就整块消失——那会让每个主体都变成"无授权"。

## 7. 当前实现 vs 目标设计（未落地部分）

| 项 | 现状 | 影响 |
| --- | --- | --- |
| **工具未登记 `ToolMeta`** | 所有真实工具都是零值 `ToolMeta`（注册点未加 meta） | 路由走的是 §2.2 的**名字 glob 分封**（已生效）；"工具自报门第"与 `Map` 到簇属的能力未启用 |
| `meta.Kind == ToolKindControl → 非 root 不可见` | 闸门存在但**无工具声明 Kind**，当前是死的 | 登记 control 类工具前必须先处理主体映射（`root` 之外的 subject 会让它变不可见） |
| `seelebridge/tools/policy.go` 两张硬编码名单 | `nodeScopeExcludedTool` / `isComputerInputTool` 仍在，与位模型**并存** | 双份事实，存在漂移风险；设计稿要求"名单改由工具元数据生成→删硬编码" |
| **员工执行面** | 只有"读面"接线（`main.go:SetRoleSessionPolicyResolver`）；员工回合的执行面尚未落到 framework Session | `emp_ro/emp_rw` 目前只有单测覆盖，**生产路径上还没有真实调用者** |
| 提权台账 | 审批响应 `Scope` 为空 = `once`，不持久 | "记住此工具 / 整会话"的台账与审计回调未做 |
| 可见性面 | 位缺表现为 dispatch 时的 `ErrToolNotVisible` | 设计目标是"断位即不在工具清单里"（需要 provider 侧按主体过滤） |
| `replace` 本地路径 | `go.mod` 指向 `G:/Program/go/Seele` | 发布前必须移除或改版本号依赖 |

## 8. 验证（可复现）

```powershell
go vet ./...                                            # 退出码 0
go test ./... -count=1                                  # 全包 ok
go test -race ./seelebridge/tools/... ./seelebridge/session/... ./application/core/goal/... ./seelexctx/... -count=1

# 确定性用例（不需凭据）
go test ./seelebridge/tools/ -run "Subagent|Employee|Bash|Subject|Route" -v -count=1
go test . -run "TestEveryRegisteredToolIsRouted|TestMainAgentToolDecisions|TestMergedConfigSubagentAndEmployee" -v -count=1
go test ./seelebridge/session/ -run "LifecycleRelease" -v -count=1

# 真实 API 冒烟（opt-in，需凭据；走生产审批桥）
$env:SEELEX_SMOKE_ACCOUNTS='<repo>\config\accounts.yaml'
go test -tags manualsmoke2 . -run TestRealAPIPermissionSmoke -count=1 -v -timeout 1500s
```

确定性用例钉住的口径：子代理违权（ctl/adm/共享外设/会话 transcript）→ 直接拒绝且
**零审批请求**；子代理位齐 → 放行；`rules` 的 deny 对子代理同样硬；员工缺位 → 走一次
选择页面、页面拒绝则调用被拒；菜单类的每个注册工具都能被机械归组（无"未分封"漏点）。

## 9. 新增工具时要做什么（机械清单）

1. 在**工具注册点**决定它属于哪一族（§2.2 判据），把它加进
   `DefaultPermissionGroupList()` 对应组的 `Match`；
2. 同步 `permission_config_test.go:permissionToolNames`（该表会断言"每个注册工具都能
   被归组、组 mode 非 0"）；
3. 若它需要逐次确认或必须拒绝，在 `DefaultPermissionRules()` 里加最细粒度规则
   （命令行类工具写**命令模式**）；
4. 若它改变循环控制流，归 `ctl`；若它改变能力面本身，归 `adm`——这两族对 `sub`
   是断位的（子代理不能叫停/派生主循环，也不能换能力面）。
