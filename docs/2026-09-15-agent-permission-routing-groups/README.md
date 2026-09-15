# 2026-09-15 · Agent 权限分封 — 路由组式 Linux 权限模型

> 把基础工具面按「路由组（route group）」分封，用 Linux 的
> **主体（user/group/other）× 位（rwx）× sudo** 语义统一 agent 权限；
> main agent = root（sudo）。
>
> 本文件分两部分：**Part A = 分封设计（产品层可落地）**，
> **Part B = 对 Seele 框架的需求单（R1–R8，供框架侧更新）**。

日期：2026-09-15 · 状态：设计 + 框架需求单 · 依据：见 §1 取证表

---

## 0. 一句话基线

> 现在「谁能用哪个工具」被**三套互不相识的机制**分别决定：
> ① `config/seele.yaml` 的扁平 `permission.rules`（allow/ask/deny，按 tool 名 + args glob）；
> ② `seelebridge/tools/policy.go` 里**硬编码的两张子代理黑名单**（`nodeScopeExcludedTool` / `isComputerInputTool`）；
> ③ `PermissionGate` 的**会话级 full_access 布尔**（一刀切整会话免审）。
>
> 本方案把它们收敛为**一套 Linux 权限模型**：工具先被**分封到路由组**（`ro`/`rw`/`ctl`/`adm`），
> 组带**位（rwx）**；主体（main / subagent / node / user）按组**授权位**；
> 位齐 → 按组默认动作（allow/ask）；位缺 → 工具对该主体**不可路由**（= 不在 PATH）；
> `adm` 组仅 main（sudo / uid 0）可达；人类持 sudo 口令（= 审批弹窗）。

---

## 1. 取证：现状（可复现的事实）

| # | 事实 | 证据 |
|---|---|---|
| E1 | 规则模型是扁平的 `{tool, patterns, action}`，无主体、无分组、无位 | `Seele@v0.2.0/tools/permission/types.go`：`PermissionRule{ToolName, Patterns, Action}`、`PermissionConfig{Mode, Rules}` |
| E2 | 检查器签名不含主体：`Check(toolName, argsJSON)`，LMRW + glob | 同上 `checker.go`：`func (pc *PermissionChecker) Check(toolName, argsJSON string) CheckResult` |
| E3 | 「子代理无某类工具」是**硬编码 switch 名单**，不在权限模型内 | `seelebridge/tools/policy.go`：`nodeScopeExcludedTool()`（plan/task 终态/fork/switch_plugin/skill_activate/compact_context）、`isComputerInputTool()` |
| E4 | 越权面分两种语义：`deny` → middleware 返回 `permission denied by policy`；隐藏 → `ErrToolNotVisible` | `seelebridge/tools/registry_state.go`（Middleware / deny 分支）vs `policy.go` 注释「Dispatch 侧由 RegistryRuntime 复核同一策略，隐藏工具返回 ErrToolNotVisible」 |
| E5 | 提权是**整会话布尔**，无一次一授权、无理由、无审计粒度 | `registry_state.go`：`SetFullAccessFor(sessionID, on)`；middleware 首行短路 |
| E6 | 产品层只做工具名 glob 匹配，工具名要**逐个枚举**（含大量别名） | `main.go:defaultManualRules()`：`todolist_*` 与 `todo_*` 各写一遍；`git_status/git_log/git_diff` 被 allow 但**从未注册**（幽灵白名单，见 E7） |
| E7 | 幽灵规则：默认白名单/`seele.yaml` 里的 `git_*`、`create_file`、`edit`、`delete` 均无对应注册工具 | 对比 `main.go`（`RegisterTool` 全量）与 `config/seele.yaml`（rules 全量） |
| E8 | 工具条目只有 name/description/schema，无 kind/group/位 元数据 | `Seele@v0.2.0/tools`：`ToolEntry{Definition, Handler}`；`types.Tool{Type, Function{Name, Description, Parameters}}` |
| E9 | 主代理当前可见基础工具面 = **52 个**（default plugin，include/exclude 空）；另有 `plan_*`(6) 与 `goal_update/status/propose_finish` 因门控当前不可见 | 运行时 `## Tools` 清单 + `plugins/default/plugin.md`（`include: []`、`exclude: []`）+ `policy.go` 的 `isPlanTool`/`isGoalTool` |

---

## 2. 模型：主体 / 路由组 / 位 / sudo

### 2.1 主体（谁在调用）—— Linux 的 user / group / other

| 主体 | Linux 类比 | 说明 |
|---|---|---|
| `user`（人类） | **真 root** | 唯一持 sudo 口令者。口令 = GUI/TUI 审批弹窗 |
| `main`（主代理） | **uid 0 / sudo** | 持全部位；`-permission full_access` = 免密 sudo |
| `entry`（Plan entry 节点） | 与 main 同 uid | 现有语义即「entry 节点同主代理」（`policy.go`） |
| `sub`（子代理 / kind:agent 节点） | **普通用户** | 只在 worktree 作用域内，`umask` 收紧 |

### 2.2 路由组（哪一族）—— 用户要的三簇 + 属主簇

「路由组」= **按名字前缀/模式把工具路由进一个簇**，再对簇套位。位用 r=4 / w=2 / x=1。

| 组 | Linux 类比 | 对象 mode | 语义（判据） | 缺位后果 |
|---|---|---|---|---|
| `ro` 读簇 | `r--` | `4`（r） | 不改任何共享状态；产出只进自己的上下文 | 不可读 |
| `rw` 写簇 | `rw-` | `6`（rw） | 改**项目文件**或**本会话工作台**状态 | 不可写 → ask/deny |
| `ctl` **叫停 loop 簇** | `--x` + 信号(Term/Stop/Chld) | `1`（x） | 决定**循环是否继续**：正常停机 / 异常停机 / 挂起转人工 / 派生子循环 / 装载执行结构 | 不可执行（工具不在 PATH） |
| `adm` 属主簇 | `rwx` + setuid + `CAP_*` | `7` + owner | 改变 **harness 本身**（能力面）：换插件、激活 skill、reload、连 MCP | 仅 uid 0 可达 |

**判据（分封的判决规则，可机械执行）：**

```
ro  ⇔ 不触碰项目文件、不触碰会话可变状态、不触碰桌面输入
rw  ⇔ 写项目文件 / 写会话工作台（todo·task·goal 栈·transcript·项目知识缓存）
ctl ⇔ 该工具的"成功执行"会改变 agent 循环的控制流（结束/挂起/派生/装载）
adm ⇔ 该工具的"成功执行"会改变**工具面本身**（哪些工具存在/可见）
```

分界线落点：**"写完是否生效"**
- 只写盘、不生效（`plugin_create` / `skill_create`）→ `rw`
- 使变更**生效**（`plugins_reload` / `switch_plugin` / `skill_activate` / `mcp_load`）→ `adm`
- 例外：`mcp_create` 归 `adm`——它写盘的是**将来会被 spawn 的任意命令/URL**（不是声明式 manifest），构成预先扩权，比 `plugin_create` 重。

### 2.3 授权（谁能拿哪些位）—— 主体 × 组

| 主体 | `ro` | `rw` | `ctl` | `adm` | sudo |
|---|---|---|---|---|---|
| `user` | 4 | 6 | 1 | 7 | ✅（口令） |
| `main` / `entry` | 4 | 6 | 1 | 7 | ✅（NOPASSWD 可配） |
| `sub` | 4 | 6（限 worktree） | **0** | **0** | ❌ |

> `sub` 拿满 `rw` 但 `ctl=0 / adm=0` —— 这正是 `policy.go` 两张硬编码名单在表达的语义：
> 子代理改文件可以，**叫停/派生主循环、换能力面不行**（并行子代理会互相污染循环状态 / 共享桌面）。

### 2.4 位 → 动作（allow / ask / deny）

位齐不等于静默放行。沿用最小惊喜：**位齐 → 组默认动作**，动作可被最细粒度的 args 规则覆盖。

| 组 | main/entry 默认 | sub 默认 |
|---|---|---|
| `ro` | allow | allow（截屏/窗口枚举这类「读但画面进上下文」→ ask） |
| `rw` | allow（`bash`/`plugin_create`/`skill_create` → ask） | allow（作用域已由 worktree 收窄） |
| `ctl` | allow | 不可路由 |
| `adm` | ask（人类 sudo 口令） | 不可路由 |

### 2.5 共享资源位（外设）—— rw 的 resource 限定

`rw` 位必须再标一个 **resource**，因为「写自己 worktree 的文件」和「写用户桌面」是两种授权：

| resource | 含义 | `sub` 是否可达 | 对应工具 |
|---|---|---|---|
| `project` | 会话绑定的项目根（已被 ProjectScope 收窄） | ✅ | `write_file` `edit_file` `bash` |
| `desktop` | **共享外设**（一块桌面，所有并行子代理共用） | ❌ | `computer_click` `computer_move` `computer_drag` `computer_scroll` `computer_type` `computer_keys` `computer_focus` |

> 这条正是现规则「子代理不可见会改变桌面的工具」的位化表述（`policy.go:isComputerInputTool`）。
> 观察类桌面工具（`computer_screenshot`/`computer_windows`/`computer_wait`）是 `ro`，`sub` 可达。

---

## 3. 分封表（52 个可见基础工具 + 门控中的 `plan_*`/`goal_*` → 组）

### 3.1 `ro` 读簇（`r--`）

| 工具 | 备注 |
|---|---|
| `read_file` `grep_search` `glob` | 项目只读（已受 ProjectScope 约束） |
| `get_time` `web_search` | 外部只读 |
| `read_tool_result` `read_plan` `read_compressed_turn` `search_history` | 会话/归档引用回读（无损） |
| `todo_status` `todolist_status` `plan_status` `goal_status` | 状态读 |
| `plugins_list` `skills_list` `mcp_list` | 枚举 |
| `plan_validate` `plan_export` | 校验/导出（不改进程状态） |
| `computer_screenshot` `computer_windows` | 桌面**观察**；画面进上下文 → 默认 ask（现规则已如此） |
| `computer_wait` | 纯节流，无副作用 → allow（现规则已如此） |

### 3.2 `rw` 写簇（`rw-`）

| 工具 | 备注 |
|---|---|
| `write_file` `edit_file` | 项目文件写（已过 FileSystem actor 串行） |
| `bash` | 项目内执行；args 级模式 = **能力白名单**（见 §4 `cap`） |
| `todo_init` `todo_add` `todo_done`（含 `todolist_*` 别名） | 自有工作台 |
| `task_add`（含 `taskadd` 别名） | 工作台登记（幂等） |
| `goal_begin` `goal_update` | 目标栈推进（未收口） |
| `compact_context` | 改写本会话可变 transcript（子代理不可见 = `sub` 无位） |
| `project_refresh` | 重建项目知识缓存 |
| `plugin_create` `skill_create` | 写盘脚手架，**未生效** |
| `computer_click` `computer_move` `computer_drag` `computer_scroll` `computer_type` `computer_keys` `computer_focus` | `rw` 但 **resource = desktop**（共享外设，见 §2.5）；逐次 ask；`sub` 无 desktop 位 |

### 3.3 `ctl` 叫停 loop 簇（`--x` / 信号）

| 工具 | 信号类比 | 控制流语义 |
|---|---|---|
| `task_complete` | `SIGTERM`（正常） | 结束循环：成功收口 |
| `task_failed` | `SIGTERM`（异常） | 结束循环：带界失败 |
| `task_needs_user_decision` | `SIGSTOP` | 挂起循环 → 转人工 |
| `task_check_node` | `SIGCHLD` | 在途打点（不结束循环） |
| `ask_approve` | 阻塞读 | 把循环挂起等人类答复 |
| `goal_propose_finish` | `SIGTERM` 请求 | 提议收口（终裁在 ADVISOR） |
| `fork_subagents` | `fork(2)` | 派生并行子循环 |
| `plan_load` `plan_clear` `plan_run` | `exec(2)` | 装载/清空/执行执行结构 |

> 「**之上的叫停 loop 工具簇**」= 本组。它是唯一能改变「循环是否继续」的组，
> 因此也是唯一必须与 `adm` 一起对 `sub` 断位的组。

### 3.4 `adm` 属主簇（`rwx` + setuid）

| 工具 | 备注 |
|---|---|
| `switch_plugin` `switch_mode` | 换整个工具面/提示面/Skill/MCP（换内核） |
| `skill_activate` | 注入 Trusted Skill 指令面 |
| `plugins_reload` | 事务式应用能力变更 |
| `mcp_create` | 登记将被 spawn 的任意命令/URL（预先扩权） |
| `mcp_load` | 连接 MCP，动态新增工具面 |

---

## 4. 配置形态（`config/seele.yaml` 升格）

保持向后兼容：`groups` + `subjects` 是新维度，`rules` 原样保留为**最细粒度覆盖层**。

```yaml
permission:
  mode: manual

  # 主体（Linux user/group/other）。rw 可限定 resource：project | desktop
  subjects:
    user: { bits: {ro: 4, rw: "6@project+desktop", ctl: 1, adm: 7}, sudo: password }
    main: { bits: {ro: 4, rw: "6@project+desktop", ctl: 1, adm: 7}, sudo: nopasswd }
    sub:  { bits: {ro: 4, rw: "6@project",          ctl: 0, adm: 0}, sudo: none }

  # 路由组（工具簇）：name 前缀 / glob → 组 + 对象 mode + 默认动作
  groups:
    - name: ro
      match: ["read_*", "grep*", "glob", "get_time", "web_search", "search_history",
              "*_status", "*_list", "plan_validate", "plan_export",
              "computer_screenshot", "computer_windows", "computer_wait"]
      mode: r
      default: allow
    - name: rw
      match: ["write_file", "edit_file", "bash", "todo_*", "todolist_*",
              "task_add", "taskadd", "goal_begin", "goal_update",
              "compact_context", "project_refresh", "plugin_create", "skill_create"]
      mode: rw
      resource: project
      default: allow
    - name: rw
      match: ["computer_click", "computer_move", "computer_drag", "computer_scroll",
              "computer_type", "computer_keys", "computer_focus"]
      mode: rw
      resource: desktop        # 共享外设：sub 无位（保留现「子代理不可见」语义）
      default: ask
    - name: ctl
      match: ["task_complete", "task_failed", "task_needs_user_decision", "task_check_node",
              "ask_approve", "goal_propose_finish", "fork_subagents",
              "plan_load", "plan_clear", "plan_run"]
      mode: x
      default: allow
    - name: adm
      match: ["switch_plugin", "switch_mode", "skill_activate", "plugins_reload",
              "mcp_create", "mcp_load"]
      mode: rwx
      default: ask

  # 最细粒度覆盖（能力白名单：args 级）。LMRW = 最后匹配胜出，故顺序为
  # 「先兜底 ask → 再安全 allow → 最后危险 deny」
  rules:
    - tool: "computer_screenshot"   # 读，但画面进上下文
      action: ask
    - tool: "plugin_create"         # 写盘脚手架：生效前需人类过目
      action: ask
    - tool: "skill_create"
      action: ask
    - tool: "bash"                  # 兜底：未命中白名单的命令 → ask
      action: ask
    - tool: "bash"                  # 安全命令白名单（cap）→ allow
      patterns: ["git *", "ls *", "cat *", "go test *"]
      action: allow
    - tool: "bash"                  # 危险命令 → deny
      patterns: ["rm -rf /*", "dd if=* of=*"]
      action: deny
```

**求值顺序（一次调用）：**
```
route(tool) → group          # 名字 glob → 组
grant(subject, group) & mode  # 位与：main 全通；sub 在 ctl/adm 断位 → 不可路由
├─ 断位   → 工具对该主体不可见（= 不在 PATH），dispatch 报 ErrToolNotVisible
├─ 位齐   → 组 default（allow → 放行；ask → 人类 sudo 口令）
└─ rules  # 最细粒度覆盖，最后匹配胜出（覆盖组 default）
```

---

## 5. 与现有机制的关系（迁移映射）

| 现有机制 | 新模型中的角色 | 迁移动作 |
|---|---|---|
| `policy.go:nodeScopeExcludedTool` / `isComputerInputTool` | `sub` 在 `ctl`/`adm` 断位 | 名单改由**工具元数据**（§Part B R2）生成，删硬编码 |
| `config/seele.yaml:permission.rules` | `groups` 的 `default` + 最细粒度 `rules` | 加 `groups`/`subjects`，`rules` 保留 |
| `PermissionGate`（middleware） | 位→动作的执行点 | 增「先 route→位判定，再 rules」两级 |
| `full_access`（会话布尔） | 免密 sudo（NOPASSWD） | 降级为可选；主路径改 per-call `elevate`（Part B R5） |
| `ErrToolNotVisible` vs `deny error` | 统一为 EPERM/EACCES 语义 | Part B R7 |

---

# Part B · 对 Seele 框架的需求单（R1–R8）

> 下面每条都对齐框架**当前真实符号**，可直接改。按「必改 / 建议」标注。
> 目标：让产品层不用再维护旁路名单，就能表达上面的模型。

### R1（必改）主体维度进 Checker
- **现状**：`PermissionChecker.Check(toolName, argsJSON string) CheckResult`——**无主体参数**（E2）。产品层只能靠 `policy.go` 硬编码名单 + 会话布尔绕开。
- **需求**：`Check(subject Subject, toolName, argsJSON)`；或 `PermissionConfig` 支持按主体分规则集，`Registry.WithSubjectResolver(func(ctx) Subject)` 供 middleware 取主体。
- **理由**：`sub`/`main`/`entry` 的正交授权无法在无主体签名下表达。

### R2（必改）工具元数据：kind / group / 位 / 可见主体
- **现状**：`ToolEntry{Definition, Handler}` 只有 name/description/schema（E8）。
- **需求**：加可选 `Meta{ Kind: read|write|control|admin; Groups []string; Bits uint8; Visibility []Role }`，随 `ToolProvider.Register` 一起声明。
- **理由**：让工具**自报门第**，gate 按元数据路由；`seelex` 侧删掉 `nodeScopeExcludedTool` / `isComputerInputTool` 两张 switch 名单（每加一个工具都要改产品层，是持续漏点）。

### R3（必改）组（group）作为规则一等维度
- **现状**：`PermissionConfig.Rules []PermissionRule` 扁平，靠调用方把每个工具名枚举一遍，还催生别名双写与幽灵规则（E6/E7）。
- **需求**：`PermissionConfig.Groups []PermissionGroup{Name, Match []string, Mode uint8, Default Action, Bits}`；Checker 先 `route(name)→group` 再套位。
- **理由**：`todolist_*`/`todo_*`、`git_*` 这类枚举+别名问题在组模型下自然消失。

### R4（必改）位（bits）与动作（action）分离
- **现状**：rule 直接写 allow/ask/deny 三值，没有 rwx 位，也没有「缺位怎么办」的策略（E1）。
- **需求**：`PermissionGroup.Mode uint8`（r=4/w=2/x=1） + `PermissionConfig.MissingBit Action`（缺位 → ask 还是 deny/隐藏）；`PermissionRule` 增可选 `Bits *uint8` 做位级覆盖。
- **理由**：「组给 mode、主体给 grant、缺位按策略」三者正交后，权限是可组合的，不再逐条硬编码。

### R5（必改）升级（sudo / elevate）原语
- **现状**：提权只有 `SetFullAccessFor(sessionID, on)` 整会话布尔（E5），无「一次一授权 / 理由 / 审计 / 时限」。
- **需求**：任一即可——
  - a) `ApprovalResponse` 增加 `Scope string`（`once|tool|session|args`），产品层据此实现「一次放行 / 记住此工具 / 整会话」；
  - b) 或框架提供 `Elevation.Elevate(subject, group, reason) (grant Grant, revoke func())` + 审计回调。
- **理由**：Linux sudo 的价值在**每次提权可见、可审计、可时限**；当前整会话 NOPASSWD 粒度太粗。

### R6（建议）控制类工具的「信号」语义
- **现状**：`task_complete`/`task_failed`/`task_needs_user_decision` 只是**名字约定**，停机由 application handler 触发，框架 loop 内核不认。
- **需求**：若采纳 R2，`Meta.Kind=control` 即可让 gate 统一「control 仅 root 路由」；进一步可加 `Meta.Signal: term|stop|chld|pause`，让 loop 内核据信号决定 停机 / 挂起 / 续跑 / 派生。
- **理由**：把「叫停 loop」从命名约定升格为内核可执行语义。

### R7（建议）两类「不可用」语义统一
- **现状**：`deny` → middleware 返回 error；隐藏 → `ErrToolNotVisible`（E4）。产品层要区分「没权限」和「工具不存在」，两条路。
- **需求**：统一为「未授权位 ⇒ 不可路由（等价 `ErrToolNotVisible`）」「有权但被策略拒 ⇒ EPERM」两层，并给 dispatch 稳定错误类型。
- **理由**：调用方与前端都需一致的错误语义。

### R8（建议）位与沙箱/路径的挂载点
- **现状**：产品层已有 `ProjectScope`/`PathGate` 做读/写路径分域，位（rw）判定若在框架内做，会与产品层路径判定各写一遍。
- **需求**：框架允许注入 `BitEnforcer` 回调（产品层决定「r/w 只作用于项目根、x 只作用于命令白名单」），框架只负责位比较与路由。
- **理由**：职责不重叠：框架管"位语义"，产品管"位落在哪片路径"。

---

## 6. 产品层待办（seelex 侧，非框架）

1. `config/seele.yaml` 升格为 `subjects` + `groups` + `rules`（§4 形态），并**清理幽灵规则**（`git_*`/`create_file`/`edit`/`delete`，E7）。
2. `seelebridge/tools/policy.go` 两张硬编码名单改为吃工具元数据（依赖 R2）。
3. 工具注册点补齐元数据（`Router.Register`、`task/tools.go`、`task/terminal.go`、plan provider、`register_goal_tools.go`、`main.go` 三个 `register*Tools`）。
4. UI：审批弹窗区分「sudo 口令（adm/ctl 提权）」与「普通确认（rw）」；提权写 append-only 审计。
5. `sub` 的 `umask`：显式断言 `ctl=0, adm=0`，把「子代理不可见名单」变成**位断言测试**（现有 `policy_test.go` 升级）。

## 7. 验收

- [ ] 任一工具都能被机械归组：按 §2.2 判据不产生歧义（新工具加入时只改注册点元数据，不改 gate）。
- [ ] `sub` 对 `ctl`/`adm` 全组断位，且**断位表现为不可见**（非调用时报错）。
- [ ] `main` 全位可达；`adm` 每次都过人类 sudo 口令（并可选择 remember 范围）。
- [ ] 删除 `policy.go` 硬编码名单后，`policy_test.go` 的等价断言全绿。
- [ ] 幽灵规则清零：白名单里每个工具名都能在 `RegisterTool` 全量里找到。
