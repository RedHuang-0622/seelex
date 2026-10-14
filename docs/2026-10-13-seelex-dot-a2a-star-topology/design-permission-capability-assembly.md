# 权限重构设计草案：从「分层位表」到「能力条目勾选装配」

日期：2026-10-13 · 状态：**设计草案（未裁决、未改代码）** · 记录人：agent
关联：本目录 `README.md` §1.8（"从分层权限改成装配权限"）、§4 Q4/Q5/Q6；
`docs/arch/agent-permission-subjects.md`（今天的权威口径）；
`docs/2026-09-15-agent-permission-routing-groups/README.md`（长期口径）

> 本文回答四件事（用户原话的四问）：
> **① 目前现状**（§1，逐条带 file:line）；**② 开发文档**（§2 目标模型 + §4 伪代码）；
> **③ 改动涉及的模块与文件**（§3）；**④ 相关伪代码**（§4）。
> 需要用户拍的裁决点集中在 §5；证据索引在 §6。事实纪律：代码与测试为"已实现"的唯一依据，
> 规划/草案一律标注；未实测的推断标 **Hypothesis**。

---

## 0. 结论摘要

1. **需求**：R / W / Desktop 作为**三个独立条目**、由用户**自由勾选组合**（不预先提供组合选项），
   外加 **delete 作为独立条目**。
2. **现状能不能表达**：**不能**，而且卡点不是"面板上少几个勾选框"，是**粒度与耦合**被焊死在三处：
   - **焊点 A（位向量）**：路由组的 `Mode` 把 r/w 焊成一根 —— `rw` 组 mode=`rw`，
     主体位必须**覆盖** mode 才在面上（`bits & mode == mode`），于是"D 只写不读"、
     "D 只要 Desktop 不要 R/W"在模型里根本不存在。
   - **焊点 B（组是路由最小单位）**：一个工具按名字 glob 落进**恰好一个**组（LMRW），
     勾选粒度 = 组，**不可能**细到"要 write_file 但不要 bash"。
   - **焊点 C（delete 不存在）**：模型里没有任何"删除"条目。删除今天只以
     **bash 规则里的命令模式**存在（`rm *` → ask / `rm -rf /*` → deny），
     以及 `write_file` 覆盖既有文件这条隐性路径。
3. **目标模型**：把"主体 × 组 × 位"换成两条集合的**包含判定**：
   - 工具侧：**每个工具声明它需要哪些能力**（可以多于一个，例如 `edit_file` 需要 R+W）；
   - 主体侧：**用户勾选的能力集合**（自由组合，逐条独立）；
   - 判定：`need(tool,args) ⊆ have(subject)`。**组合不预设，需求由工具自身给出**。
4. **最大的一块新工程是 delete**：它必须同时落成
   （a）一等公民工具 `delete_file`/`delete_dir`（今天**不存在**，`seelebridge/tools/router.go:92-107` 只注册 read/grep/glob/write/edit/bash 族），
   （b）**bash 命令的服务端能力分类**（复用既有的 `security.ClassifyCommand` idiom，`seelebridge/security/command_class.go:90`），
   （c）一条诚实的口径说明：**W 拦不住"把文件清空"**，工具层做不到"不可毁灭"，
   真正的执法点在 **git 快照 / 保护分支 / 凭据**（本目录 `README.md` §4 Q4）。
5. **工程策略**：**不改 vendor 里的 Seele 框架**。能力条目是新的唯一事实，
   框架的 `PermissionConfig{Groups,Subjects,Bits,Rules}` 降级为**从能力集合投影出来的低层管道**
   （§2.7），并配一条"投影不得静默放权"的同型守卫（对齐 `sanitizeMeta` 的教训，
   `seelebridge/tools/permission_policy.go:495`）。

---

## 1. 目前现状（逐条带证据）

### 1.1 四层结构（Linux 式）

实现是"分层权限"的完整落地，长期口径见 `docs/arch/agent-permission-subjects.md` §2：

```
主体（谁在调用）  root / sub / emp_ro / emp_rw / emp_<角色名>
路由组（哪一族）  ro / rw / rw_session / rw_desktop / ctl / adm     （按工具名 glob，LMRW）
位（rwx）         组给 Mode，主体给 Grant.Bits；位齐 → 组默认动作；位缺 → 不可路由
动作              组默认 allow/ask；rules 做最细粒度覆盖（LMRW，最后匹配胜出）
```

落点（`seelebridge/tools/permission_policy.go`）：`DefaultPermissionGroupList()` `:91`、
`DefaultPermissionSubjects()` `:194`、`DefaultPermissionRules()` `:234`、
`DefaultPermissionConfig()` `:289`（`MissingBit: ActionAsk` `:294`）。

### 1.2 词表（`application/contract/dto/permission.go`）

| 项 | 内容 | 位置 |
| --- | --- | --- |
| 路由组名 | `ro` / `rw` / `rw_session` / `rw_desktop` / `ctl` / `adm` | `:17-24`（`PermissionGroupNames()` `:36`） |
| 位值 | `r=4` / `w=2` / `x=1`，`All=7` | `:27-33` |
| 写入侧校验 | `ValidPermissionGroup()` `:49`、`NormalizePermissionGroups()` `:68`（未知组名报错，空 map = "没显式装配"，`{group:0}` = "这一族明确不开"） |
| 会话档位（tier） | `manual/edit/auto/full` | `:100`、`PermissionTiers()` `:117`、`NormalizePermissionTier()` `:153` |

跨层唯一事实的分工很清楚：**组名与位值的字面量在 dto**，
`seelebridge/tools` 的 `Group*` / `bit*` 是别名（`permission_policy.go:62-84`）。

### 1.3 路由组表（6 组，工具名 glob，LMRW）

| 组 | Mode | Resource | 组默认 | 该组名下的工具（现状全表） | 位置 |
| --- | --- | --- | --- | --- | --- |
| `ro` | `r--`(4) | — | allow | `read_file` `read_plan` `read_tool_result` `read_compressed_turn` `search_history` `grep_search` `glob` `bash_read` `read_batch` `get_time` `web_search` `todo_status` `todolist_status` `plan_status` `goal_status` `plugins_list` `skills_list` `mcp_list` `plan_validate` `plan_export` `show_files` `computer_screenshot` `computer_windows` `computer_scroll_targets` `computer_wait` | `:94-121` |
| `rw` | `rw-`(6) | `project` | allow | `write_file` `edit_file` `bash` `bash_bg` `job_manage` `jobs_manage` `todo_init` `todo_add` `todo_done` `todolist_init` `todolist_add` `todolist_done` `task_add` `taskadd` `project_refresh` `plugin_create` `skill_create` | `:122-141` |
| `rw_session` | `rw-`(6) | `session` | allow | `compact_context` | `:142-148` |
| `rw_desktop` | `rw-`(6) | `desktop` | **ask** | `computer_click` `computer_move` `computer_drag` `computer_scroll` `computer_type` `computer_keys` `computer_focus` | `:149-158` |
| `ctl` | `--x`(1) | — | allow | `task_complete` `task_failed` `task_needs_user_decision` `task_check_node` `ask_approve` `fork_subagents` `plan_load` `plan_clear` `plan_run` `goal_begin` `goal_update` `goal_propose_finish` `goal_done` `team_plan` `team_dispatch` `team_join` `team_milestone` `team_close` `team_context` `team_work` `team_item` `team_accept` `team_fail` `team_recover` `team_items` | `:159-178` |
| `adm` | `rwx`(7) | — | **ask** | `switch_plugin` `switch_mode` `skill_activate` `plugins_reload` `mcp_create` `mcp_load` | `:179-193` |

**注意两处归组的语义分歧（现状就有）**：
`computer_wait` 归 `ro`（纯节流无副作用），`computer_screenshot/windows/scroll_targets`
也是 `ro` 但由 rules 改成 ask（画面进上下文）——即**"观察类桌面"跨了两个组**，
这正是"Desktop 想做成一个条目"时最先撞上的既有事实。

### 1.4 主体授权表（位矩阵）

`DefaultPermissionSubjects()` `:194-232`：

| 主体 | `ro` | `rw`(project) | `rw_session` | `rw_desktop` | `ctl` | `adm` |
| --- | --- | --- | --- | --- | --- | --- |
| `root` | 4 | 6 | 6 | 6 | 1 | 7 |
| `sub` | 4 | 6 | **0** | **0** | **0** | **0** |
| `emp_ro` | 4 | **0** | 0 | 0 | 0 | 0 |
| `emp_rw` | 4 | 6 | 0 | 0 | 0 | 0 |

判据：**位齐** → 组默认动作，再被 rules 覆盖；**位缺** → 该工具对该主体**不可路由（= 不在 PATH 上）**，
配置里由 `permission.missing_bit` 表达（当前 `ask`，`:294`）。

### 1.5 求值链（一次调用）

```
Middleware（seelebridge/tools/registry_state.go:479）
  ├ 主体解析 classFor：ctx 员工身份 → 节点作用域(sub) → 角色会话登记(emp_*) → root
  │   （permission_policy.go:417 / :430；员工执行面走 WithEmployeeGrant :868）
  ├ sanitizeMeta :495（声明的组不在本次配置里 → 声明作废，退回按名字路由）
  ├ ctx 注入 WithEngine(subject)（框架据此查授权表）
  ├ 会话 ID 按 RoleSessionOwner 折算成宿主主会话（只影响审批呈现归属 :372）
  └ gate().Decide(...) :546
      ├ Enforce（执行门，本包 :576）
      │   ├ class==root && full 档 → ActionAllow 短路
      │   └ enforceClass :517
      │       ├ sub  → enforceSubAgent :540（显式 deny 硬；位缺/未分封 → 交回框架 → 直接拒绝，Approval=nil）
      │       └ emp_* → enforceEmployee :565（显式 deny 硬；位缺/未分封 → **执行选择页提权**；位齐 → 直接放行）
      └ Checker.DecideForMeta（route → 位 → resource → 组默认 → rules）
          命中 ask → ApprovalBroker（DenyWithoutPrompt: true 于 :546-560）
```

### 1.6 工具面（列工具时）

唯一折算点 `ToolFaceForSubject`（`permission_policy.go:625`）/ `ToolFaceForContext`（`:654`）：

```go
group, routed := RoutePermissionGroup(cfg.Groups, toolName)   // :633
bits := grant.Bits[group.Name].Bits
return bits&group.Mode == group.Mode                          // 覆盖全部所需位 = 在面上
```

接线（产线已通）：`seelebridge/tools/policy.go:11-27`（`PolicyDeps.ToolFace`）→ `:82`
（`Filter` 里"看不见就不给模型看"）→ `seelebridge/runtime.go:439-440`（闭包注入）。

### 1.7 员工装配（一员工一主体 `emp_<角色名>`）

- 主体名归一化 `EmployeeSubjectName` `:693`（非 ASCII 角色名追加 FNV 短哈希，避免两个员工撞同一主体）；
- 装配条目 `EmployeePermission{RoleName, Policy, Groups}` `:745`；
- 档位派生 `EmployeeGroupsForPolicy` `:753`（readonly → 读位；readwrite → 读位+项目写位）；
- 装配期写入 `Runtime.AssignEmployeePermissions`（`seelebridge/runtime_tools.go:80`）→
  `PermissionGate.SetEmployeePermissions`（`registry_state.go:293`）；
- 运行时补条目 `ensureEmployeeSubject` `:943`（会话冷启动反查不到时按"显式格子优先、档位其次"补）；
- 角色回合起手把身份按构造带进 ctx：`seelebridge/runtime_role_turn.go:281`
  （`WithEmployeeGrant(ctx, roleName, spec.ToolsPolicy, spec.PermissionGroups)`）；
- **装配字段**：`dto.RoleSpec.ToolsPolicy :92 / PermissionGroups :96`、
  `dto.TeamMember.ToolsPolicy :150 / PermissionGroups :156`、
  `sessionstore.TeamworkMember.Permission map[string]int (json:permission_groups)` `:111-114`、
  `teamwork.WorkerRequest.PermissionGroups`（`seelebridge/teamwork/teamwork.go:77-81`）→
  `memberPermissionGroups()` `:348-361`（位值范围校验 `map[string]uint8`）。

### 1.8 两套"档"（都叠在三层模型之上）

| 轴 | 位置 | 作用对象 | 作用 | 粒度 |
| --- | --- | --- | --- | --- |
| **档位 tier** `manual/edit/auto/full` | dto `:100-130`；覆盖实现 `seelebridge/tools/permission_tiers.go`（`ApplyTier`） | **只 root** | **只剪 ask**，从不新增 allow、从不碰 deny；`full` 是执行门短路 | **会话** |
| **员工 ToolsPolicy 档** `readonly/readwrite/(空=inherit)` | `ClassForToolsPolicy` `:315`；`EmployeeGroupsForPolicy` `:753` | 员工 | 装配期"没单独分配时"的默认位 | 装配期 |

### 1.9 配置面 / 写面 / 读面（今天已有的入口）

| 面 | 位置 | 说明 |
| --- | --- | --- |
| 配置文件 | `config/seele.yaml:21 permission:`（`missing_bit` + `rules`） | 只写"覆盖"，字段缺失 = 保持代码默认（`main.go:1786 mergePermissionConfig`、`:1845 loadPermissionConfig`） |
| CLI/TUI | `/permission <manual\|edit\|auto\|full>`（`application/core/command.go:201`） | 切换**会话档位** |
| GUI 写面 | `gui/bridge.go:64-67`（接口）、`:920 SetFullAccess`、`:927 SetPermissionTier` | 只有档位写面，**没有能力写面** |
| GUI 读面 | `application/model/state.go:356 PermissionTier` / `:359 PermissionTiers []dto.PermissionTierInfo` | 目录由后端下发（前端不复制档位名） |
| 前端 | `gui/frontend/dist/permission-tier.js`（纯解析/展示）、`index.html:94 perm-picker` + `:286-288 permission-tier-list`（运行状态弹窗里的档位列表）、`app.js:2034-2065,4269-4625` | **没有能力勾选面板**（`permission_groups` 在 dist 里零命中，已 grep 全量 js/mjs/html） |
| 团队装配 | `team_plan` 的 member 字段 → `dto.TeamMember.PermissionGroups` | 今天的"逐格下发"入口 |

### 1.10 现状里"等价于预设组合"的三处焊点 + delete 不存在

| # | 需求（用户原话） | 现状能否表达 | 证据 |
| --- | --- | --- | --- |
| 1 | **W 独立于 R**（只写不读） | **不能**：`rw` 组 mode=`rw`，`bits&mode==mode` 要求 R 与 W 同时具备 | `permission_policy.go:122-124`、`:633` |
| 2 | **Desktop 独立于 R/W** | **不能**：`rw_desktop` 是**一个组**、mode 也是 `rw`；勾选粒度是"整格 r+w+desktop" | `:149-151` |
| 3 | **勾选细到工具**（要 `write_file` 不要 `bash`） | **不能**：组是路由的最小单位，一个工具恰好落一组 | `RoutePermissionGroup` `:338`、`matchToolGlob` `:353` |
| 4 | **delete 独立条目** | **不存在**：无 `delete` 组、无位、无工具；删除只体现为 bash 规则模式 `"rm *"`/`"mv *"` → ask（`:263-272`）、`"rm -rf /*"` → deny（`:273-282`） | `:268-270`、`:283-287` |
| 5 | **不做"预先提供所有权限组合选项"** | 现状**没有** 2^N 组合枚举表；但有两类"预设"：**tier 4 档**（自动度预设）与 **ToolsPolicy 3 档**（能力预设），它们正是"不可见的口径" | dto `:117-130`、`ClassForToolsPolicy` `:315` |
| 6 | 平权后**桌面稀缺性**（原靠"位缺 + 默认 ask"表达） | 需要**运行时互斥**替代（本目录 `README.md` §4 Q6） | 今天无互斥实现（`isComputerInputTool` 只做"子代理不许注入输入"，`policy.go:164`） |
| 7 | 能力落**凭据层**（拦得住 bash 推分支） | **没有**（今天"位缺 = 不在 PATH 上"，拦不住有凭据的 `bash`） | 本目录 `README.md` §4 Q4 |

---

## 2. 目标模型（开发文档）

### 2.1 能力条目词表（建议 7 条，其中前 4 条是用户点名）

| id | 名称 | scope | 语义 | 覆盖工具（投影自现状） | 可逆性 |
| --- | --- | --- | --- | --- | --- |
| `read` | **R 读取** | project / session | 读文件、搜索、回读制品与历史；不改任何共享状态 | 现 `ro` 组的文件/搜索/回读类（去掉 desktop 观察类） | 可逆 |
| `write` | **W 写入** | project | 写/改项目文件与工作台（todo/task）；`bash`（执行面）；作业面（`job_manage`/`jobs_manage`/`read_batch`） | 现 `rw` 组 + 写类 rules | 可逆（除非覆盖） |
| `delete` | **D 删除** | project | 让一个路径**不再存在**（unlink/rmdir/recursive/移出项目/远端删除） | **新增** `delete_file`/`delete_dir` + `bash` 的删除命令（args 级） | **不可逆** |
| `desktop` | **Desktop 桌面** | desktop | 看屏幕 + 注入鼠标键盘（含 `computer_wait`） | 现 `rw_desktop` 组 + `ro` 组的 `computer_screenshot/windows/scroll_targets` | 可逆（对外部状态可逆性另说） |
| `session` | 会话工作台 | session | 压缩/改写本会话可变 transcript | `compact_context` | 可逆 |
| `control` | 循环控制 | loop | 结束/挂起/派生/装载执行结构/编排团队 | 现 `ctl` 组全部 | 可逆 |
| `admin` | 能力面 | ability | **改能力面本身**（换插件/Skill/MCP/reload） | 现 `adm` 组全部 | 可逆，但**自举**（见 §2.8） |

**可选细化（不建议进第一版）**：把 `bash` 从 `write` 里拆成独立 `exec` 条目；
把作业面拆成独立 `jobs`。理由：今天的口径就是"bash 在 `rw` 组（需 r+w）"，
第一版保持"`bash` 需要 `write`"可以**零语义漂移**地把老配置映射过来（§3.5）。

### 2.2 三条语义定律

**L1 · 条目独立、自由组合、不预设组合。**
装配面只呈现"条目 + 复选 + 说明"。用户勾哪几条就是哪几条，`2^7` 种组合里**没有一条被产品预先命名**。
"预设"最多作为**一键填充草稿**的便利（如"只读草稿 = 勾 read"），且必须**当场把勾好的条目显示出来**，
不能作为一个不可见的口径存在——判定与存储的**唯一事实永远是条目集合**。

**L2 · 判定 = 需求集 ⊆ 勾选集（工具声明它需要什么，不是它属于哪一组）。**
"组合不是预设"不等于"工具没有必需条件"：**工具自身的必需条件由工具给出**，可能多于一个，例如：

| 工具 | 需求集 | 为什么 |
| --- | --- | --- |
| `read_file` / `grep_search` / `glob` / `read_*` | `{read}` | 只读 |
| `write_file` | `{write}` | 纯写（覆盖既有文件仍是 write，见 §2.4） |
| `edit_file` | `{read, write}` | 它按 `old_string` **匹配文件内容**（`router.go:420-435`），没有 read 做不成 |
| `bash` | `{write}` ∪ `args(命令)` | 执行面本身需要写位（沿用今天 `rw` 组口径）；删除命令由分类器加 `{delete}`（§2.4） |
| `bash_read` | `{read}` | 服务端只读分类守卫（`router.go:533` → `security.ClassifyCommand`） |
| `computer_click` 等输入类 | `{desktop}` | 桌面 |
| `computer_screenshot` 等观察类 | `{desktop}` | **今天跨 ro/rw_desktop 两组**，平权后统一到 `desktop` 一条（顺带删掉"观察类靠 rules 补 ask"的跨组特例） |
| `delete_file` / `delete_dir` | `{delete}` | 新工具 |
| `compact_context` | `{session}` | 会话工作台 |
| `task_*` / `plan_*` / `goal_*` / `team_*` / `fork_subagents` | `{control}` | 循环控制流 |
| `switch_plugin` / `mcp_*` / `plugins_reload` / `skill_activate` | `{admin}` | 能力面 |

**L3 · 未勾 = 不存在；"问不问"是另一根轴。**
勾选决定**有没有**（工具不在模型面上、调用不可路由/被拒）；审批决定**问不问**（rules/tier/审批页）。
今天这两件事被 `MissingBit=ask` + 主体类策略混在一条链上（§1.5），能力模型把它们**显式分开**
（这正是 §5-Q1 要拍板的根问题，见 §5）。

### 2.3 需求集从哪来（静态 + args 级）

两层，**并集**（不是 LMRW）：

```
need(tool, args) = static(tool) ∪ argsLevel(tool, args)
```

- `static(tool)`：能力规范表（唯一事实，见 §4.2）。**并集**语义是刻意的：组路由用"最后匹配胜出"，
  而"需求"必须**失败朝安全侧**——一个工具能删又能写，就要求两条；
- `argsLevel`：命令行工具的**命令本体分类**（`bash`/`bash_bg`）。这是 delete 的真正落点。
  `policyArgsFor`（`registry_state.go:515`）今天已把 `{"command":...}` 抽成命令本体交给规则匹配，
  能力判定复用同一条抽法。

### 2.4 delete 条目（重点，也是最大的一块）

#### (1) 定义与边界（必须钉死，否则判据漂）

`delete` = **命名空间的移除**：某个路径/引用在这次调用后**不再存在**。明确边界：

| 形态 | 需要 | 理由 |
| --- | --- | --- |
| `rm a.txt`、`rm -rf dir`、`rmdir dir`、`del`、`erase`、`Remove-Item`、`rd` | `D`(+`W` 由 bash 静态需求给出) | 路径消失 |
| `git rm`、`git clean -fd`、`git reset --hard`（丢弃未提交改动）、`git worktree remove`、`git branch -D` | `D` | 内容/引用消失 |
| `mv a b` / `Move-Item` / `rename` | `D` + `W` | 源路径消失、目标路径出现 |
| `find … -delete`、`--delete`、`-delete` | `D` | 参数级删除（`writeTokens` 里已收录这些 token：`security/command_class.go:83-88`） |
| `truncate -s 0 f`、`> f`（重定向）、`write_file` 覆盖既有文件 | **`W`（不算 D）** | 这是"改内容"，不是"移除路径" |
| `docker rm`、`docker system prune` 等 | `D`（建议额外走 rules 的 ask） | 删的是外部资源 |

**诚实边界（必须写进文档，不许藏）**：只勾 `W` 的能力**仍然可以造成内容损失**——
把文件清空（`truncate` / `> f` / 覆盖）在文件系统层面就是"写"。
所以**工具层的 delete 条目做不到"不可毁灭"**；要真正的"删不掉"，执法点只能在
**git 快照 / 保护分支 / 凭据**（本目录 `README.md` §4 Q4）。本文把这条标为
**Confirmed 的模型局限**（可由上面的形态表证明），而不是缺陷。

#### (2) 三条可达路径

1. **一等公民工具**（新增）：`delete_file`（文件）、`delete_dir`（目录，需 `recursive:true`）。
   落在 `seelebridge/tools/router.go:92` 的注册表里，路径解析复用 `resolveNodePath(ctx, path, forWrite=true)`
   （`router.go:60-84`，内含 `security.ResolveInside` 的越界拒绝），并复用既有原子/审计写入面。
   有了它，模型有条**明路**，不必"为了删一个文件去写 bash"。
2. **bash 命令分类**（新增分类器）：`seelebridge/security/command_caps.go`。
   与 `ClassifyCommand` 同族、同保守默认：**判不出来 = 按最严处理**（要求 `{write, delete}`，见 §4.3）。
3. **其它工具的参数级形态**（加固）：`write_file` 若入参带 `truncate:true` 之类的显式语义，
   应显式要求 `D`；`project_refresh` 这类"重建/清理"类工具若会删目录，同样要标。第一版只做前两条。

#### (3) 为什么必须今天就一起做

delete 是**唯一一条"不勾也没有别的路径拦住"**的能力（其余能力今天各有代理：
`desktop` 有默认 ask、`admin` 有 root 限制）。只要模型还能跑 `bash`，
"没有 delete 条目"就等于**删除能力永远隐藏在 write 里**——这就是用户说的
"把 delete 单独设置出来"的实质价值。

### 2.5 desktop 条目（平权后的稀缺性从哪来）

现状靠"独立一格 + 默认 ask + 子代理位缺"表达"一块桌面是共享外设"。
平权后（`desktop` 与其他条目同形），稀缺性必须有**运行时替代**：

- **桌面租约**（建议）：`DesktopLease`，键 = 会话/主体，`Acquire` 失败 → **不静默排队**，
  而是落回审批页（"有人正在用这块桌面，是否抢占？"）或直接拒绝（子代理无人类在环时）；
- 与 `isComputerInputTool`（`policy.go:164`，"子代理不许注入输入"）的关系：
  租约是**运行时**答案，硬编码名单是**静态**答案 —— 目标态应由能力表派生（见 §3.2），
  消除 `docs/arch/agent-permission-subjects.md` §7 已记录的"双份事实漂移风险"。

### 2.6 与 tier（会话档位）的关系：两轴正交

| 轴 | 回答 | 粒度 | 谁 |
| --- | --- | --- | --- |
| **能力条目**（本文） | 有没有 | 主体（员工/角色/队友；root 默认全勾） | 装配期 + 面板勾选 |
| **tier**（现状保留） | 问不问 | **会话** | 用户随时切 |

保留两轴的理由：把它们合成一条会让"用户想少被打断"变成"用户想多要权限"，
而今天的产品口径恰恰是**档位只剪 ask、从不新增 allow**（`permission_tiers.go`）。
**可选强化（§5-Q3）**：每条目三态 `off / ask / on`，把两轴合成一张表 —— 表达力更强，
但改动面翻倍（每个主体 × 每条目一个三态，且 tier 的会话粒度无处安放）。

### 2.7 与框架 `PermissionConfig` 的关系（工程决策）

**不动 vendor**。三件事：

1. **能力集合是新的唯一事实**：`PermissionGate` 增 `map[Subject]CapabilitySet`；
2. **投影**：`projectPermissionConfig(caps) → toolspermission.PermissionConfig`
   把能力集合折成 `Groups/Subjects/Rules`，供框架 gate 继续承担
   "执行门 + 审批页 + args 级 rules"；
3. **同型守卫（防线）**：投影必须**只收窄不放宽**，并配一条断言用例
   （每个注册工具都能被投影覆盖；任何"未分封/投影缺失"的工具**不得**因投影而变成 allow）。
   这条断言就是 `sanitizeMeta`（`:495`）那条教训的机械化——它记的是
   "组名对不上 ⇒ required 退化成 0 ⇒ 静默放权"。

### 2.8 `admin` 条目的自举问题（安全底线）

勾选模型里，`admin` = "改能力面本身" = **可以给自己勾上任何其它条目**（写配置文件/换插件）。
所以：

- `admin` **不允许**作为普通条目自由勾给非 root 主体（UI 只读 + 后端拒绝），
  或勾选它需要**二次口令**（人类的选择页）；
- `control` 对 `sub` 同样要谨慎：子代理能编排/叫停主循环 = 自我提权链。
  建议沿用今天"ctl/adm 对 sub 断位"的口径，在能力模型里表达为**条目级禁勾**（`Blocked`）。

### 2.9 存储与传输

| 层 | 现状 | 目标 |
| --- | --- | --- |
| 角色定义 | `dto.RoleSpec.PermissionGroups map[string]uint8 :96` | 增 `Capabilities []string`（迁移期**两者并存，能力优先**） |
| 团队计划 | `dto.TeamMember.PermissionGroups :156` | 同上 |
| 团队落盘 | `sessionstore.TeamworkMember.Permission map[string]int :114` | 增 `capabilities`（json），双读 |
| 装配请求 | `teamwork.WorkerRequest.PermissionGroups :77-81` | 增 `Capabilities` |
| 运行时 ctx | `WithEmployeeGrant(..., groups)` `permission_policy.go:868` | `WithEmployeeCapabilities(ctx, roleName, caps)`（同构造携带，理由同现有注释：冷启动反查不到时不能退化成档位默认） |
| 配置文件 | `config/seele.yaml permission.rules/missing_bit` | 增 `permission.capabilities.<subject>: [read, write, …]`；`groups/subjects` 保留为 legacy 覆盖 |

### 2.10 读写 API

- 写：`Bridge.SetSubjectCapabilities(subject string, caps []string) (view, error)`（对照 `bridge.go:927`）；
  `Runtime.SetRoleCapabilities(roleName string, caps []string) error`；
  `Runtime.AssignEmployeeCapabilities(roles []dto.RoleSpec) error`（替换 `runtime_tools.go:80`）。
  写入侧**显式失败**：未知条目名报错（对照 `dto.NormalizePermissionGroups` 的既有纪律）。
- 读：`RuntimeState` 增 `PermissionCapabilities []dto.PermissionCapabilityInfo`
  （对照 `state.go:359`）+ 当前主体的 `Checked []string`；
  角色读面挂在 `AgentTeamRoleSnapshot`（`bridge.go:995`）→ `dto.RoleSnapshot`（`dto/rolesession.go:97`）。
- 目录里每条目带 **`contains_tools`**（勾上后会拿到哪些工具，由后端算）
  与 **`blocked_reason`**（为什么不能勾）——这是"不预设组合"能用的前提：
  用户看得见自己在买什么，而不是先背组合表。

### 2.11 前端（勾选面板）

- 新纯函数模块 `gui/frontend/dist/permission-capabilities.js` + `*.test.mjs`
  （与 `permission-tier.js` 同型：目录来自后端、前端不复制条目名、缺失时退化成"只显示 id"）；
- 面板：条目行 = `[✓] R 读取` + 一句话说明 + "勾上将获得：read_file, grep_search, …"；
  `delete` 行**标红/加不可逆标识**；`admin` 行在非 root 主体上**禁用并给原因**；
- **不做组合预设**：不出现"只读员工/开发员工/发布员工"三个按钮；
  允许"一键填充草稿"，但填充后必须**逐条显示为已勾选项**（§2.2 L1）；
- composer 芯片仍显示 tier（另一根轴），旁挂一句"能力面：R W D"之类的摘要。

---

## 3. 改动涉及的模块与文件

### 3.1 新增文件

| 文件 | 内容 | 规模 |
| --- | --- | --- |
| `application/contract/dto/permission_capability.go` | 条目词表 + 目录（id/label/short/desc/scope/irreversible/contains_tools）+ `Valid/Normalize` + legacy 映射 + 错误类型 | M |
| `seelebridge/tools/permission_capabilities.go` | 能力规范表（条目→工具 glob）+ `RequiresFor` + `CapabilitySet` + 判定 + 投影 `projectPermissionConfig` + 主体集合管理 | L |
| `seelebridge/security/command_caps.go`（+ `_test.go`） | 命令 → 能力需求集（**delete 分类器**），与 `command_class.go` 同族 | L |
| `seelebridge/tools/delete_tools.go`（或并入 `router.go`） | `delete_file` / `delete_dir` 工具与 schema | M |
| `seelebridge/tools/permission_capability_test.go`、`permission_delete_test.go` | 需求表/判定矩阵/删除形态/投影守卫 | L |
| `gui/frontend/dist/permission-capabilities.js` + `.test.mjs` | 勾选面板纯函数 | M |
| `docs/arch/permission-capability-assembly.md` | **拍板后**升级为架构事实（本文是草案） | S |

### 3.2 修改文件

| 文件 | 位置 | 改什么 | 风险 |
| --- | --- | --- | --- |
| `application/contract/dto/permission.go` | 全局 | 组词表**保留**（legacy 投影源）；加 `CapabilitiesFromLegacyGroups` | 低 |
| `application/contract/dto/agentteam.go` | `:92-96`、`:150-156`、`:270/341` | `RoleSpec/TeamMember/TeamLibraryEntry` 增 `Capabilities []string` | 低（可加不可删） |
| `application/contract/ports.go` | `EmployeePermissionPort` 附近 | 增能力分配/读取方法（或改签名） | 中（跨层契约） |
| `application/core/agentteam/spec.go` | `NormalizeRole :100-112` | 校验能力名（未知报错） | 低 |
| `application/model/state.go` | `:356-360`、`:838-860` | `RuntimeState`/`SessionRuntime` 增能力目录与勾选态 | 低 |
| `seelebridge/tools/permission_policy.go` | `:91-232`、`:495`、`:517-578`、`:616-671`、`:745-968` | 组/位表降级为投影源；`enforceSubAgent/enforceEmployee` 改走能力判定；`ToolFaceFor*` 改按需求集 ⊆ 勾选集；`EmployeePermission.Groups` → `Capabilities` | **高**（判定核心，必须配全量回归） |
| `seelebridge/tools/registry_state.go` | `:189-243`、`:266`、`:293-345`、`:456`、`:479-515`、`:546-583` | `PermissionGate` 增 `capabilitySets`；`SetEmployeeCapabilities`；`rebuildLocked` 投影重建；`Middleware` 接 args 级需求解析；`Enforce` 接能力判定 | **高** |
| `seelebridge/tools/tool_meta.go` | `:26-49` | `DeclaredToolMeta` 的唯一事实从"组表"换成"能力表"，`Meta.Groups` 变投影产物 | 中 |
| `seelebridge/tools/policy.go` | `:11-27`、`:145-176` | `ToolFace` 口径改能力集；`nodeScopeExcludedTool`/`isComputerInputTool` 名单改由能力表派生（消除双份事实） | 中 |
| `seelebridge/tools/permission_tiers.go` | 全局 | `tierStopAskingTools` 若保留则按新条目名对齐；`full` 短路条件保持 `class==root` | 中 |
| `seelebridge/tools/router.go` | `:92-107`、`:533` | 注册 `delete_file/delete_dir`；`bash`/`bash_bg` handler 前置"命令能力分类"守卫（与 `bash_read` 同型） | 中 |
| `seelebridge/runtime.go` | `:439-440` | `ToolFace` 闭包改指能力版 | 低 |
| `seelebridge/runtime_tools.go` | `:37`、`:80` | `SetPermissionConfig`（投影）、`AssignEmployeePermissions` → 能力版 | 中 |
| `seelebridge/runtime_role_turn.go` | `:281` | `WithEmployeeGrant` → `WithEmployeeCapabilities` | 中 |
| `seelebridge/teamwork/teamwork.go` | `:77-81`、`:348-361` | `WorkerRequest`/`memberPermissionGroups` → 能力数组 | 中 |
| `sessionstore/teamwork.go` | `:111-114` | `TeamworkMember` 增 `capabilities`（双读迁移） | 中（落盘兼容） |
| `seelebridge/runtime_teamwork.go` | `team_plan` handler | member 能力字段透传 | 低 |
| `main.go` | `:1764-1884` | `setupPermissionGate`/`mergePermissionConfig`/`loadPermissionConfig` 支持 `permission.capabilities` | 中 |
| `config/seele.yaml` | `:21` | 加能力段与新注释口径 | 低 |
| `gui/bridge.go` | `:64-67`、`:920-930`、`:995` | 能力读写 API + 角色快照读面 | 中 |
| `gui/frontend/dist/index.html` | `:94`、`:286-288` | 新增能力勾选面板（档位列表保留） | 中 |
| `gui/frontend/dist/app.js` | 档位 UI 附近 | 面板渲染/写回/警告 | 中 |
| `gui/frontend/dist/team-board-view.js` | `tools_policy` chip | 改成能力 chips | 低 |
| `application/core/command.go` | `:201` | `/permission` 旁增 `/capabilities`（读/写；CLI/TUI 面） | 低 |
| `permission_config_test.go` | `permissionToolNames` | 断言改为"每个注册工具都**在能力表里**且需求集非空" | 低但关键 |
| `seelebridge/tools/permission_employee_grant_test.go` 等 8 个权限用例 | 全局 | 语义迁移到能力集（保留 legacy 映射回归） | 中 |

### 3.3 明确不动

- `vendor/github.com/RedHuang-0622/Seele/**`（框架 `PermissionConfig` 形状、`Gate`/`Checker` 语义）；
- `seelebridge/security/project_scope.go`（**作用域**照旧由 ProjectScope 收窄：能力说"能做什么"，作用域说"能对哪个根做"）；
- teamwork / worktree / `emp_<role>` 的主体命名与作业模型；
- tier 的"只剪 ask、只 root、会话粒度"三条边界。

### 3.4 分期与工作量（**Hypothesis，未实测**）

| 期 | 内容 | 相对规模 | 风险 |
| --- | --- | --- | --- |
| **P0** | §5 的裁决（尤其 Q1 未勾语义、Q2 条目集、Q4 delete 边界） | — | 不改代码 |
| **P1** | 词表 + 能力表 + 需求判定 + 投影 + 迁移映射 + 全量回归 | **XL** | 判定核心，回归用例必须先于实现 |
| **P2** | **delete**：`delete_file/delete_dir` + 命令分类器 + 规则 + 用例 | **L** | 分类器误判（漏判=漏拦删除；误判=功能缺失），保守默认 + 可疑命令走 ask |
| **P3** | 读写 API（Bridge/RuntimeState/CLI）+ 角色/团队字段迁移 | **L** | 落盘双读、老数据兼容 |
| **P4** | 前端勾选面板 + 团队看板/装配面 | **M** | 纯展示与写回 |
| **P5** | 桌面租约（Q6）+ 凭据层（Q4，跨到 git/系统） | **XL** | 另立方案；凭据层不在本仓库单独可解 |

一句话：**P1+P2 是这个需求的主体工作量**，也是"庞大"的来源——不是加一个复选框，
而是把"权限的原子"从"组位"换成"能力条目"，并把原本不存在的能力（delete）从命令分类里**造出来**。

### 3.5 迁移与兼容

**legacy 组+位 → 能力集合**（单向、无损到能力侧，用于零漂移迁移）：

```
ro: r        → {read}
rw: r | w    → {read, write}          # 注意：老模型里 W 必然带 R
rw: w        → {write}                # 老模型表达不出，但新模型支持
rw_session:* → {session}
rw_desktop:* → {desktop}
ctl: x       → {control}
adm: rwx     → {admin, read, write}
```

- **反方向（能力 → 组位）可能有损**（`{write}` 无 `read`、`{desktop}` 无读写），
  因此**投影只用于"喂给框架的管道"**，不作为读回口径；读回一律给能力集合。
- `ToolsPolicy=readonly/readwrite` 降级为**装配期草稿默认**（现状已是这个角色：`EmployeeGroupsForPolicy :753`）；
  `inherit/full/空` 维持"不写条目 = 继承宿主"（`runtime_tools.go:80-105` 的既有口径，别改）。
- `permission.groups/subjects`（yaml）保留为 legacy 覆盖：**存在则参与投影，且与能力段冲突时以能力段为准 + 告警**。

---

## 4. 相关伪代码

> 命名与现有代码同族：`dto.Permission*`、`toolspermission.*`、`state.*`。所有函数名都落在
> §3 的改动表里，不是凭空发明的 API。

### 4.1 dto：能力词表与规整

```go
// application/contract/dto/permission_capability.go
package dto

// 能力条目 id（唯一事实；前端不复制这一份）。
const (
	CapRead    = "read"
	CapWrite   = "write"
	CapDelete  = "delete"
	CapDesktop = "desktop"
	CapSession = "session"
	CapControl = "control"
	CapAdmin   = "admin"
)

type PermissionCapabilityInfo struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`          // "读取" / "写入" / "删除" / "桌面" …
	Short         string   `json:"short"`          // "R" / "W" / "D" / "桌面"
	Description   string   `json:"description"`
	Scope         string   `json:"scope"`          // project|session|desktop|loop|ability
	Irreversible  bool     `json:"irreversible,omitempty"`   // delete=true：UI 标不可逆
	ContainsTools []string `json:"contains_tools"`           // 勾上后拿到的工具（说明面，后端算）
	AdminOnly     bool     `json:"admin_only,omitempty"`     // admin：仅 root 可勾（§2.8）
}

func PermissionCapabilities() []PermissionCapabilityInfo // 规范序；返回新切片
func PermissionCapabilityIDs() []string
func ValidPermissionCapability(id string) bool

// NormalizeCapabilities 规整一份勾选集合：去重、规范化、未知条目**报错**。
// 空切片 = "没有显式勾选"（装配期按草稿默认）；`[]string{}` 与 nil 同类（沿用
// NormalizePermissionGroups 的既有纪律，dto/permission.go:68）。
func NormalizeCapabilities(ids []string) ([]string, error)

// CapabilitiesFromLegacyGroups 是迁移映射（§3.5），供老配置/老落盘数据零漂移过渡。
func CapabilitiesFromLegacyGroups(groups map[string]uint8) ([]string, error)

type PermissionCapabilityError struct{ Capability, Reason string }
func (e *PermissionCapabilityError) Error() string
```

### 4.2 能力规范表（工具 → 需求集，唯一事实）

```go
// seelebridge/tools/permission_capabilities.go
package tools

// CapabilitySet 是能力集合。用集合而不是位向量：条目数会增长（未来 exec/jobs），
// 而位向量的语义会随"位宽"漂移；判定是集合包含，两行代码就够，不需要位运算。
type CapabilitySet map[string]bool

func (s CapabilitySet) Has(id string) bool
func (s CapabilitySet) Union(other CapabilitySet) CapabilitySet
func (s CapabilitySet) SubsetOf(other CapabilitySet) bool   // 判定：need ⊆ have
func (s CapabilitySet) Sorted() []string                    // 稳定序（写配置/下发前端）

// capabilityRule 是"一条能力条目覆盖哪些工具"。工具名走 `*` glob（与组表同一套
// matchToolGlob，permission_policy.go:353）。
type capabilityRule struct {
	Capability string
	Tools      []string
}

// capabilityStatic 是**静态需求表**：唯一事实，取代"工具属于哪个组"。
// 并集语义（见 §2.3）：同一个工具可以命中多条规则，需求取并集。
var capabilityStatic = []capabilityRule{
	{CapRead, []string{
		"read_file", "read_plan", "read_tool_result", "read_compressed_turn",
		"search_history", "grep_search", "glob", "bash_read", "show_files",
		"get_time", "web_search", "plan_validate", "plan_export",
		"todo_status", "todolist_status", "plan_status", "goal_status",
		"plugins_list", "skills_list", "mcp_list",
	}},
	{CapWrite, []string{
		"write_file", "edit_file", "project_refresh", "plugin_create", "skill_create",
		"todo_init", "todo_add", "todo_done", "todolist_init", "todolist_add", "todolist_done",
		"task_add", "taskadd",
		"bash", "bash_bg", // 执行面沿用今天"在 rw 组"的口径（§2.1）
		// 作业面与 bash_bg 同族：**沿用今天 rw 组的口径**——既有注释明确写了
		// "放 CTL 会让 sub/员工断位，它们自己派发的后台作业就谁也取不回、杀不掉"
		// （permission_policy.go:126-131）。能力模型不得在这里比现状更严。
		"job_manage", "jobs_manage", "read_batch",
	}},
	// edit_file 需要 read 才能按 old_string 匹配内容（router.go:420-435）：
	// 这不是"预设组合"，是工具自身的必需条件（§2.2 L2）。
	{CapRead, []string{"edit_file"}},

	{CapDelete, []string{"delete_file", "delete_dir"}}, // 新工具
	{CapDesktop, []string{
		"computer_click", "computer_move", "computer_drag", "computer_scroll",
		"computer_type", "computer_keys", "computer_focus",
		"computer_screenshot", "computer_windows", "computer_scroll_targets", "computer_wait",
	}},
	{CapSession, []string{"compact_context"}},
	{CapControl, []string{
		"task_complete", "task_failed", "task_needs_user_decision", "task_check_node",
		"ask_approve", "fork_subagents", "plan_load", "plan_clear", "plan_run",
		"goal_begin", "goal_update", "goal_propose_finish", "goal_done",
		"team_plan", "team_dispatch", "team_join", "team_milestone", "team_close", "team_context",
		"team_work", "team_item", "team_accept", "team_fail", "team_recover", "team_items",
	}},
	{CapAdmin, []string{
		"switch_plugin", "switch_mode", "skill_activate",
		"plugins_reload", "mcp_create", "mcp_load",
	}},
}

// StaticRequirement 返回工具的静态需求集（args 无关）。
// 未命中任何规则 → 返回 nil + false；调用方必须把它当**未分封**处理（最严），
// 绝不允许"需求为空 ⇒ 判定放行"（对齐 sanitizeMeta 的教训）。
func StaticRequirement(toolName string) (CapabilitySet, bool)

// RequiresFor 返回"这一次调用"的完整需求集：静态 ∪ args 级。
func RequiresFor(toolName, argsJSON string) (CapabilitySet, bool) {
	need, routed := StaticRequirement(toolName)
	if !routed {
		return nil, false
	}
	if extra, ok := argsLevelRequirement(toolName, argsJSON); ok {
		need = need.Union(extra)
	}
	return need, true
}
```

### 4.3 bash 命令 → 能力需求（**delete 分类器**）

```go
// seelebridge/security/command_caps.go
package security

// CapabilityOfCommand 判定一条命令需要哪些能力。与 ClassifyCommand（命令分类的
// 服务端守卫，command_class.go:90）同族、同保守默认：
//
//	ok=false ⇒ 无法安全分类（复合/替换/未知首词）→ 调用方必须按**最严**处理，
//	          即要求 {write, delete}；绝不允许"判不出来就少要一条能力"。
//
// 语义边界见设计草案 §2.4(1)：本函数判的是"路径/引用是否消失"，
// 覆盖/清空属 write，不属 delete。
func CapabilityOfCommand(command string) (CapabilitySet, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" { return nil, false }

	// 1) 复合/重定向/替换：整串拒绝分类（沿用 ClassifyCommand 的口径：
	//    `a | b` 里 a 只读不代表整条只读，`$(...)` 完全不可见）。
	for _, token := range shellSyntaxTokens { // 复用 command_class.go 的同一份 token 表
		if strings.Contains(trimmed, token) { return nil, false }
	}

	// 2) 只读命令：复用既有白名单（bash_read 的同一份判据，不新造第二份）。
	if ClassifyCommand(trimmed) { return CapabilitySet{CapRead: true}, true }

	fields := splitCommandWords(trimmed) // 复用 command_class.go:138
	if len(fields) == 0 { return nil, false }
	first := strings.ToLower(fields[0])
	sub := "" ; if len(fields) > 1 { sub = strings.ToLower(fields[1]) }

	caps := CapabilitySet{}
	// 3) 执行面本身需要 write（沿用"bash 在 rw 组"的既有口径）。
	caps.Add(CapWrite)

	// 4) 删除首词表（跨平台：POSIX + Windows PowerShell/CMD 形态）。
	if deleteFirstWords[first] { caps.Add(CapDelete) }
	// 5) 两词族的删除/破坏性子命令（只列判据，不追求穷举；漏判由 ok=false 兜）。
	if set, ok := deleteSubcommands[first]; ok && set[sub] { caps.Add(CapDelete) }
	// 6) 参数级删除 token（`find -delete`、`--delete`；writeTokens 已收录一部分）。
	for _, f := range fields[1:] {
		for _, t := range deleteArgTokens {
			if strings.Contains(strings.ToLower(f), t) { caps.Add(CapDelete) }
		}
	}
	// 7) 搬移 = 源路径消失（delete）+ 目标路径出现（write）。
	if moveFirstWords[first] { caps.Add(CapDelete); caps.Add(CapWrite) }

	// 8) 首词不在任何已知表里：保守（无法分类 → 最严）。
	if !knownFirstWords[first] { return nil, false }
	return caps, true
}

var deleteFirstWords = map[string]bool{
	"rm": true, "rmdir": true, "unlink": true, "shred": true,
	"del": true, "erase": true, "rd": true, "remove-item": true, "ri": true,
}
var deleteSubcommands = map[string]map[string]bool{
	"git":    {"rm": true, "clean": true, "worktree": true, "reset": true /* --hard */, "push": true /* --delete */},
	"docker": {"rm": true, "rmi": true, "prune": true},
}
var deleteArgTokens = []string{"-delete", "--delete", "--hard", "--prune"}
var moveFirstWords  = map[string]bool{"mv": true, "move": true, "move-item": true, "rename": true, "ren": true}
```

调用侧（与 `bash_read` 的守卫同型，`router.go:533`）：

```go
// seelebridge/tools/permission_capabilities.go
func argsLevelRequirement(name, argsJSON string) (CapabilitySet, bool) {
	switch name {
	case "bash", "bash_bg":
		command := commandOf(argsJSON) // 复用 registry_state.go:515 policyArgsFor 的抽法
		caps, ok := security.CapabilityOfCommand(command)
		if !ok {
			// 判不出来 = 最严：写 + 删都要（宁可多要一条能力，也不放过一条删除）。
			return CapabilitySet{CapWrite: true, CapDelete: true}, true
		}
		return caps, true
	default:
		return nil, false
	}
}
```

### 4.4 判定（`need ⊆ have`）

```go
// seelebridge/tools/permission_capabilities.go
// decideByCapabilities 是主体类策略的能力版：返回 ok=true 表示"由能力判定定案"，
// 不再落回框架的组/位；ok=false 表示"能力齐、动作交回问不问那一轴"。
func (state *PermissionGate) decideByCapabilities(
	ctx context.Context, subject toolspermission.Subject, name, argsJSON string,
) (toolspermission.Action, bool) {
	have := state.capabilitiesForSubject(ctx, subject) // 装配条目 → ctx 身份 → 共享默认
	need, routed := RequiresFor(name, argsJSON)

	if !routed {
		// 未分封（动态 MCP / 第三方插件工具）：不猜。
		// 子代理/员工：不在面上、不可路由（今天口径，permission_policy.go:540/565）。
		// root：交回框架规则（今天口径）。
		if class := state.classFor(ctx); class != SubjectClassRoot {
			return state.missingAction(class), true
		}
		return "", false
	}
	if need.SubsetOf(have) {
		// 能力齐 ⇒ 有没有这一轴结束；问不问交给 rules/tier（今天的分工，原样保留）。
		return "", false
	}
	return state.missingAction(state.classFor(ctx)), true
}

// missingAction 是 §5-Q1 的裁决落点（**今天是硬编码在这里的一行**）。
func (state *PermissionGate) missingAction(class SubjectClass) toolspermission.Action {
	switch class {
	case SubjectClassSub:
		return toolspermission.ActionDeny // 无人类在环：缺能力 = 不存在，零审批
	case SubjectClassEmployeeRO, SubjectClassEmployeeRW:
		return toolspermission.ActionAsk // 有宿主人类：执行选择页 = sudo（今天口径）
	default:
		return toolspermission.ActionDeny // root 默认全勾；被主动收回的能力视为不存在
	}
}

// capabilitiesForSubject 的解析顺序（与今天的 classFor/resolveEmployeeSubject 同源）：
//   1. ctx 身份（WithEmployeeCapabilities：角色回合按构造携带的勾选集合）
//   2. 装配期条目（SetSubjectCapabilities / AssignEmployeeCapabilities 落下的 map[Subject]）
//   3. 共享默认（root = 全条目；sub/emp_ro/emp_rw = 按 §3.5 的 legacy 映射）
// 都查不到 ⇒ 返回"空集"而不是"全集"：缺配置必须收窄，绝不放宽。
```

### 4.5 工具面（列工具时）

```go
// ToolFaceForContext 的能力版：静态需求集 ⊆ 勾选集。
//
// 与判定（4.4）的分工：这里 args 未知，用**静态需求集** —— 因此勾了 write 的
// 员工看得见 `bash`（它静态只要 write），但真要跑 `rm -rf x` 时，
// args 级需求把 delete 加上去 → 调用时被拦（或提权）。两处必须成对，缺一即错。
func (state *PermissionGate) ToolFaceForContext(ctx context.Context, toolName string) bool {
	if state == nil { return true }
	need, routed := StaticRequirement(toolName)
	if !routed { return false } // 未分封：员工面上不摆（今天口径，permission_policy.go:625-641）
	return need.SubsetOf(state.capabilitiesForSubject(ctx, state.subjectFor(ctx, toolName)))
}
```

### 4.6 投影到框架配置（不动 vendor 的代价与防线）

```go
// projectPermissionConfig 把"能力集合 → 框架的低层管道"折出来：
//   - Groups：由 capabilityStatic 反推（每个工具名 → 它所属的旧组名），保证框架的
//     routeLocked 仍能按名字路由到组；
//   - Subjects：能力集合 → 位（仅用于让框架的位检查与 rules 生效）；
//   - 能力集合**缺失**的条目一律折成 0 位（= 不可路由），绝不折成全位。
func projectPermissionConfig(caps map[toolspermission.Subject]CapabilitySet) toolspermission.PermissionConfig

// 防线（必须有用例）：投影后**不得**出现"未分封/投影缺失 ⇒ required 退化成 0 ⇒ allow"。
// 这条断言就是 sanitizeMeta（permission_policy.go:495）那条教训的机械化。
func assertNoSilentAllow(cfg toolspermission.PermissionConfig, registered []string) error
```

### 4.7 写 / 读 API

```go
// seelebridge/tools/permission_capabilities.go
func (state *PermissionGate) SetSubjectCapabilities(subject toolspermission.Subject, caps []string) error {
	normalized, err := dto.NormalizeCapabilities(caps) // 未知条目 → 报错，不静默
	if err != nil { return err }
	state.mu.Lock(); defer state.mu.Unlock()
	state.capabilitySets[subject] = toSet(normalized)
	published := state.cfg
	published.Subjects, published.Groups = projectSubjects(state.capabilitySets), published.Groups
	state.cfg = published
	state.rebuildLocked() // 唯一落点：任何改 cfg 的路径都必须走它（registry_state.go:266 的既有纪律）
	return nil
}

// gui/bridge.go —— 读面（前端"勾选面板"的唯一数据源）
type PermissionCatalogView struct {
	Subject string                  `json:"subject"`
	Items   []PermissionCapabilityView `json:"items"`
	Notes   []string                `json:"notes,omitempty"` // 语义提示（未勾=不存在；D 不拦覆盖…）
}
type PermissionCapabilityView struct {
	Info           dto.PermissionCapabilityInfo `json:"info"`
	Checked        bool                         `json:"checked"`
	EffectiveTools []string                     `json:"effective_tools"` // 勾上即得（说明面）
	BlockedReason  string                       `json:"blocked_reason,omitempty"` // admin 仅 root 等
}

func (bridge *Bridge) PermissionCatalog(subject string) (PermissionCatalogView, error)
func (bridge *Bridge) SetSubjectCapabilities(subject string, caps []string) (PermissionCatalogView, error)
```

### 4.8 前端勾选面板（纯函数 + 可测）

```js
// gui/frontend/dist/permission-capabilities.js
// 与 permission-tier.js 同型：目录唯一来源是后端（runtime.permission_capabilities），
// 前端不复制条目名、不预置组合。
export function capabilityCatalog(runtime)          // 过滤掉无 id 的条目
export function capabilityChecklist(catalog, draft) // → [{id,label,short,desc,checked,irreversible,tools,blocked}]
export function capabilityDiff(before, after)       // 勾选变化 → {added:[],removed:[]}（写请求载荷）
export function capabilityWarnings(items)           // ["单独勾 W 不阻止清空文件内容", "D 不可逆", "桌面为独占资源"]
export function capabilitySummary(runtime)          // composer 芯片旁的摘要："R·W·D"
```

### 4.9 桌面租约（Q6 的运行时替代）

```go
// seelebridge/tools/desktop_lease.go
type DesktopLease struct {
	mu    sync.Mutex
	owner map[string]string // 会话键/租约键 → 持有者（主体名）
}
// Acquire：已被别人持有 → 返回 errDesktopBusy（**不静默排队**）。
// 调用侧把 errDesktopBusy 折成 ask（有人的主体：审批页问"是否抢占"）或 deny（子代理）。
func (l *DesktopLease) Acquire(key, holder string) (release func(), err error)
```

### 4.10 迁移映射（一次性，可测）

```go
func CapabilitiesFromLegacyGroups(groups map[string]uint8) ([]string, error) {
	out := make([]string, 0, len(groups))
	for group, bits := range groups {
		if !dto.ValidPermissionGroup(group) {
			return nil, &dto.PermissionGroupError{Group: group, Reason: "未知路由组"}
		}
		if bits == 0 { continue } // `{rw:0}` = 这一族明确不开 → 映射成"不勾"
		switch group {
		case dto.PermissionGroupRO:
			out = append(out, dto.CapRead)
		case dto.PermissionGroupRW:
			if bits&dto.PermissionBitRead != 0  { out = append(out, dto.CapRead) }
			if bits&dto.PermissionBitWrite != 0 { out = append(out, dto.CapWrite) }
		case dto.PermissionGroupRWSession:
			out = append(out, dto.CapSession)
		case dto.PermissionGroupRWDesktop:
			out = append(out, dto.CapDesktop)
		case dto.PermissionGroupCTL:
			out = append(out, dto.CapControl)
		case dto.PermissionGroupADM:
			out = append(out, dto.CapAdmin, dto.CapRead, dto.CapWrite)
		}
	}
	return dto.NormalizeCapabilities(out)
}
```

### 4.11 用例清单（写测试时按这张表逐条钉）

| # | 断言 | 类型 |
| --- | --- | --- |
| 1 | **每个注册工具**都在 `capabilityStatic` 里、需求集非空（沿用 `permissionToolNames` 的机械清单纪律） | 表驱动 |
| 2 | `edit_file` 需求 = `{read, write}`；`write_file` 需求 = `{write}`（证明"单写不读"可表达） | 表驱动 |
| 3 | `{write}` 的主体：`write_file` 放行、`bash` 可见、`bash`+`rm -rf x` → 需求含 delete → 按未勾语义处理 | 判定矩阵 |
| 4 | `{desktop}` 的主体：`computer_click` 放行、`write_file` 不在面上（平权的直接证据） | 判定矩阵 |
| 5 | delete 形态表（§2.4）逐行：`rm` / `git rm` / `Remove-Item` / `mv` / `find -delete` → 需求含 `delete` | 表驱动 |
| 6 | 复合命令（`a && rm -rf b`、`$(…)`、`|`）→ **无法分类 → 最严**（要求 write+delete） | 表驱动 |
| 7 | 投影防线：未分封工具在投影后**不得**变成 allow（`assertNoSilentAllow`） | 守卫 |
| 8 | legacy 回归：老 yaml / 老 `permission_groups` 落盘 → 能力集合 → 判定与今天**逐条一致** | 回归 |
| 9 | 装配面：`emp_<角色>` 的 `{write}` 与 `{desktop}` 两个员工能力面不同（沿用 `permission_employee_grant_test.go` 形态） | 集成 |
| 10 | **零审批**：子代理缺能力 → 拒绝且**零审批请求**（沿用 `permission_subject_test.go` 口径） | 集成 |
| 11 | 前端纯函数：勾选 → 写请求载荷 → 回执刷新（`node --test`） | 单元 |

---

## 5. 需要拍板的裁决点

| # | 问题 | 选项 | 我的建议 |
| --- | --- | --- | --- |
| **Q1**（根问题） | **未勾的能力是"不存在"还是"可提权"？**（本目录 `README.md` §4 Q5） | (a) 不存在（deny）(b) 可提权（ask）(c) 每条目三态 `off/ask/on` | **(a) 为默认，(b) 仅对"有宿主人类的员工"保留**（= 今天的 sudo 口径原样保住），(c) 作为后续可选强化：改动面翻倍且 tier 的会话粒度无处安放 |
| **Q2** | 条目集取几条？ | 4 条（R/W/D/Desktop，其余保留组）/ 7 条（本文）/ 9 条（+exec/jobs） | **7 条**：ctl/adm/session 必须有条目安放，否则"装不进去"；exec/jobs 拆出来收益小、迁移风险大 |
| **Q3** | tier 与能力两轴是否合并？ | 保留两轴（本文）/ 合并成三态 | **保留两轴**（档位只剪 ask 已是既定产品边界） |
| **Q4** | `delete` 的边界（覆盖/清空算不算删除） | 算 / 不算 | **不算**（§2.4 边界表），并**明说**"W 拦不住清空"；真正的不可毁灭只能在 git/凭据层 |
| **Q5** | `bash` 的静态需求 | `{write}`（沿用今天口） / 新立 `{exec}` | 第一版 `{write}`（零漂移迁移）；`exec` 列为可选细化 |
| **Q6** | 桌面稀缺性（平权后） | 运行时租约 / 继续依赖逐次审批 | **运行时租约**（§4.9）；但它是 P5，别塞进 P1 |
| **Q7** | 勾选面覆盖哪些主体？ | 只员工/角色（`emp_<role>`）/ 全体主体（root 也可取消） | 第一版 = **员工/角色/队友**（张力最小、正是"平权"的落点）；root 面板只读显示"全勾"，其"问不问"仍由 tier 管 |
| **Q8** | 能力是否也落**凭据层**（Q4 原文） | 是 / 否 | **是，但另立方案**：拦得住 `bash` 的只有凭据（只读 remote、只能推自己的 branch），本仓库单独解不了 |

---

## 6. 证据清单（file:line）

| # | 事实 | 证据 | 级别 |
| --- | --- | --- | --- |
| 1 | 组表 6 组 + 工具归组全表 | `seelebridge/tools/permission_policy.go:91-193` | Confirmed |
| 2 | 主体位矩阵 | `permission_policy.go:194-232` | Confirmed |
| 3 | 缺位 = ask（`MissingBit`） | `permission_policy.go:294` | Confirmed |
| 4 | 位齐判据 `bits&mode==mode` | `permission_policy.go:633` | Confirmed |
| 5 | 组路由 = LMRW 名字 glob | `permission_policy.go:338-388` | Confirmed |
| 6 | sub 断位 → 直接拒绝（Approval=nil，零审批） | `permission_policy.go:540-563`；`registry_state.go:546-560` | Confirmed |
| 7 | 员工缺位 → 执行选择页提权；位齐 → 直接放行 | `permission_policy.go:565-578` | Confirmed |
| 8 | 工具面唯一折算点 | `permission_policy.go:616-671` | Confirmed |
| 9 | 员工一主体 `emp_<角色>` + 装配条目 | `permission_policy.go:673-748`、`:927-968`；`seelebridge/runtime_tools.go:80-105` | Confirmed |
| 10 | 角色回合按构造带身份进 ctx | `seelebridge/runtime_role_turn.go:281` | Confirmed |
| 11 | 档位只剪 ask / 只 root / 会话粒度 | `seelebridge/tools/permission_tiers.go`；`registry_state.go:442-478`、`:576-583` | Confirmed |
| 12 | 会话档位读面下发前端 | `application/model/state.go:356-359`；`gui/frontend/dist/permission-tier.js` | Confirmed |
| 13 | GUI 只有档位写面，无能力写面 | `gui/bridge.go:64-67`、`:920-930`；dist 全量 js/mjs/html 中 `permission_groups` 零命中 | Confirmed |
| 14 | **无 delete 工具/条目**：删除只存在于 bash 规则模式 | `permission_policy.go:263-272`（ask）、`:273-282`（deny）；`seelebridge/tools/router.go:92-107`（注册面） | Confirmed |
| 15 | `write_file`/`edit_file` 形态 | `seelebridge/tools/router.go:394-435` | Confirmed |
| 16 | 只读命令分类器（前置 idiom） | `seelebridge/security/command_class.go:31-136`；handler 守卫 `router.go:533` | Confirmed |
| 17 | 命令本体抽取（args 级判定的落点） | `registry_state.go:515-533`（`policyArgsFor`） | Confirmed |
| 18 | 簇属声明由组表派生；投影型静默放权的教训 | `seelebridge/tools/tool_meta.go:26-49`；`permission_policy.go:495-516` | Confirmed |
| 19 | 双份名单（子代理可见性 / 桌面输入）仍在 | `seelebridge/tools/policy.go:145-176`；`docs/arch/agent-permission-subjects.md` §7 | Confirmed |
| 20 | 员工装配字段链路（dto → sessionstore → teamwork） | `dto/agentteam.go:92-96`、`:150-156`；`sessionstore/teamwork.go:111-114`；`seelebridge/teamwork/teamwork.go:77-81`、`:348-361` | Confirmed |
| 21 | yaml 只写覆盖层 | `config/seele.yaml:1-21`；`main.go:1764-1884` | Confirmed |
| 22 | CLI 只切档位 | `application/core/command.go:198-215` | Confirmed |
| 23 | 桌面"共享外设"是物理冲突、需运行时互斥 | 本目录 `README.md` §4 Q6 | 设计判断（Q6 原文） |
| 24 | 能力须落凭据层 | 本目录 `README.md` §4 Q4 | 设计判断（Q4 原文） |
| 25 | "P1+P2 是主体工作量"、分期规模 | 本文 §3.4 | **Hypothesis**（未实测） |
| 26 | "工具层做不到不可毁灭"（W 可清空） | 本文 §2.4 形态表（可由 `truncate -s 0` / `> f` / `write_file` 证明） | Confirmed（模型层） |

## 7. 非目标

- 不改 vendor 里的 Seele 框架权限内核；
- 不做凭据层与远端发布（Q4 的落地另立方案）；
- 不做桌面租约实现（P5；本轮只写接口）；
- 不删 `ToolsPolicy` 三档、不删 tier 四档（降级为草稿默认 / 另一根轴）；
- 不追求"工具级细到参数"的完整能力代数（例如 per-path 的 R/W 分级）——那是下一个数量级的工程。
