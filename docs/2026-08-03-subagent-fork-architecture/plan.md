# 子代理架构调整详细设计 — 模型自由层（todolist + fork）与固化层（goal + plan）分层

> 状态：**全部切片已实施**（2026-08-03，v2 版本：切片 1-3 = A1 工具面 / A2 skill / A3 预算 / C3 收尾契约；切片 4-10 = C1 worktree / C2 fork / A5 todolist / A6 plan 归位 / B1 详情 / D1 窗口加载 / C4 沙箱端口。`go build/vet/test ./...`、GUI 构建、真实 LLM 冒烟、真实账号 plan 冒烟全绿）。切片 4-10 的实施差异见 §12。
> 适用范围：子代理入口分层（plan DAG / fork / todolist）、worktree 生命周期工作流、子代理与主代理能力对齐（工具集/skill/预算）、前端子代理详情查看、主代理滑动窗口加载、沙箱
> 关联架构：[`docs/arch/seele-v2-runtime-architecture.md`](../arch/seele-v2-runtime-architecture.md)（§4.4 Plan→subagent）、[`docs/research/coding-agent-harness-comparison.md`](../research/coding-agent-harness-comparison.md)（差距矩阵 §3.7）、[`docs/plan/subagent-detail-architecture.md`](../plan/subagent-detail-architecture.md)（前端详设，本设计 §8 实施）
> 已确认决策（2026-08-03 用户）：① worktree 合并回主工作区前必须用户审批；② 变基由子代理自己执行，框架检测失败兜底；③ 非 git 项目/只读节点跳过 worktree，共享工作区；④ plan 工具从主工具面移出，随 goal skill 激活注入
> 2026-08-04 增量：子代理节点生命周期和内部工具活动已形成 Runtime → Application/EventHub → GUI Bridge → frontend reducer 的实时事件链，并由整体 mock 覆盖。

## 1. 背景与动机

### 1.1 现状与实证问题

子代理唯一入口是 plan DAG：模型必须经 `plan_load` 现场构造 `entry/nodes/edges` 规范 JSON，再 `plan_run` 执行（`seelebridge/plan_tool_provider.go`）。对强模型可行，但实测暴露四类问题：

| 问题 | 实证 | 根因 |
|---|---|---|
| 弱模型不会构造 plan JSON | minimax 无法产出合法 DAG，被 preflight 契约反复拒绝 | 让模型"一次想清楚整个图"违反模型能力曲线：自然语言规划强、结构化 JSON 生成弱 |
| 弱模型不知道如何结束子代理/提交任务 | minimax 跑完节点后不执行 `task_complete`/收尾 | 计划/任务终止契约面向强模型，无显式收尾流程引导 |
| 弱模型乱写 txt 文件 | minimax 习惯写一堆无意义文件"表达" | 无沙箱/无物理隔离，写文件是它唯一可靠的"动作"出口 |
| 子代理工具集硬编码 | `nodeScopeToolVisible` 白名单 6 工具（`registry.go:129`） | 子代理与主代理能力不对等 |

### 1.2 设计原则（用户确认）

1. **子代理与主代理除模型供应商、上下文长度不同外，能力完全一致**：同一工具面（skill 工具/MCP/plan/task/上下文工具）、同一 skill 目录、同一权限策略、同一收尾契约。
2. **入口分层**：模型自由规划走 todolist + 自主 fork；人为调优的确定性流程（goal）才用 plan DAG；两层共享同一 workplan 内核。
3. **worktree 生命周期由框架强制编排**，不依赖模型自觉——开 worktree → 干活 → 变基 → 合并，均为确定性串行工作流。

### 1.3 目标

- 子代理获得与主代理一致的能力面（A1-A4）；
- 新增 `fork_subagents` 工具：程序化 DAG，弱模型只要会"调一个工具、传子代理列表"；
- 新增 todolist 工具族：无结构负担的模型自管理清单；
- plan 工具面归位 goal（A6）；
- 子代理 worktree 生命周期工作流（C1）落地；
- 前端子代理详情查看（B1）、主代理滑动窗口加载（D1）、沙箱（C4）。

## 2. 目标架构（三层）

```text
┌─ 模型自由层（默认工具面）────────────────┐
│  todolist（轻量清单，模型自管理）         │
│  fork_subagents（按需派活，程序化 DAG）   │
│  + 文件/搜索/bash/skill/MCP/上下文工具     │
├─ 固化层（goal skill 激活时注入）─────────┤
│  goal skill → plan_load/plan_run/…      │
│  （DAG 节点 kind:agent 复用同一种子代理）  │
├─ 共享底层（两层复用，不重复建设）─────────┤
│  workplan runner / NodeScope /           │
│  worktree 生命周期（§3）/ merge-back      │
│  事件投影 / 节点预算 / 合并审批 / 沙箱     │
└──────────────────────────────────────────┘
```

不变式：**fork 的子代理与 plan DAG 的 agent 节点执行同一套 worktree 生命周期**。固化层与自由层只有"入口"不同，执行路径共享。

## 3. 子代理 worktree 生命周期工作流

### 3.1 节点生命周期状态机

```text
pending → worktree-creating → running（cwd = worktree 根）
       → rebasing → merging → completed
       └→ failed（worktree 保留现场 + 报告主代理）
```

状态经 planEventSink 事件投影（`NodeStatus` 扩展 `worktree_creating/rebasing/merging`），GUI 节点详情可见当前阶段。

### 3.2 节点编排（`SeelexAgentNode.Run` 改造，agent_node.go:74）

```text
1. 开：git worktree add <sibling>/seelex-<nodeID> -b seelex/<nodeID>
     （复用 plugins/git/worktree/git-worktree.ps1 的 Add-PlanSubAgentWorktree -NodeId）
2. 切：NodeScope.WorkspaceID = worktree 路径 → scoped_tools.go 的
     ResolveRead/ResolveWrite 与 bash 的 cwd 均按节点根解析（§3.7 接缝）
3. 干：子代理 Session 在 worktree 内执行（收尾提示词见 §7.5）
4. 变基：子代理自己把分支 rebase 到主分支最新（§3.4）
5. 合：合并前审批（§3.3）→ merge 回主工作区 → 清理 worktree
6. 失败：worktree 保留现场（供排查），节点 failed，报告主代理
```

### 3.3 合并审批（已确认）

- 节点完成且检测到 worktree 有提交 → merge 前经 `approve.ApprovalGate`（`plan_factory.go:150` approvalGateNode 同款）向用户弹审批，附 **diff 摘要**（changed files + 行数统计，不展开全文）；
- 决策：approve → merge + 清理；reject → 不合并，节点 failed，worktree 保留；
- 只读节点（无提交）跳过审批直接完成；
- 非 git 项目/跳过 worktree 的节点（§3.5）不产生合并，无需审批。

### 3.4 变基策略（已确认）

- 由子代理自己执行：收尾提示词要求"干活结束、commit 完成后 rebase 到主分支最新"（§7.5 第 2 步），变基冲突由子代理自己解决；
- 框架兜底：节点结束后检测 worktree 分支与主分支的 merge-base，若落后主分支且子代理未完成 rebase → 尝试 `git rebase`；冲突 → 节点 failed + 现场保留（不自动解冲突，避免框架瞎改用户代码）。

### 3.5 降级策略（已确认）

| 条件 | 行为 |
|---|---|
| 非 git 仓库 | 跳过 worktree，共享工作区（现状路径门禁语义不变） |
| 只读节点（预计无写操作） | 跳过 worktree 减少开销；以节点目标内容判定或配置显式指定 |
| worktree 创建失败（路径/权限） | 降级共享工作区 + 事件投影警告 |

### 3.6 冲突处理

- 并行子代理各自在独立 worktree，文件冲突不会发生在干活阶段；
- 冲突只可能出现在 rebase/merge 阶段：`git rebase` 冲突 → 子代理自己解（§3.4）；`git merge` 冲突（主工作区有新提交改动同文件）→ abort merge，节点 failed，现场保留，报告主代理"冲突文件清单"。

### 3.7 接缝（字段已预留，只需接线）

| 接缝 | 现状 | 本设计 |
|---|---|---|
| `PlanBranchBinding.WorkspaceID`（branch.go:56-64） | 冻结值，无消费者 | plan_run 时绑定主工作区；节点 worktree 路径作为节点级覆盖 |
| `NodeScope.WorkspaceID`（node_scope.go:16-21） | 空转 | = worktree 路径，`scoped_tools.go` 按节点分根 |
| `git-worktree.ps1`（plugins/git/worktree） | 插件存在，子代理工具面拿不到 | worktree 工具进子代理工具面（A1 后）+ 框架编排直接调用 |
| `approvalGateNode`（plan_factory.go:150） | 仅 kind:approve 节点 | 复用为 merge 审批门（§3.3） |
| workplan runner 事件投影 | queued/running/completed + 心跳 | 扩展 worktree 阶段事件（§3.1） |

## 4. fork 工具（模型自由层入口）

### 4.1 契约

```
fork_subagents(subagents: [{id, goal}...])
  → 程序化构造 all-parallel DAG（N 个 agent 节点 + 1 个汇总节点）
  → workplan.NewFromPlan + runner.Run（复用 plan_run 全链路）
  → 返回各节点结果汇总 JSON
```

- 参数只有 `id` + `goal`（自然语言目标）——**弱模型不需要 DAG 知识**；
- 每个子代理走 §3 的 worktree 生命周期；
- 汇总节点 = 确定性节点（`productNode` 同族），输出 = 各节点结果拼接，供主代理同轮可见；
- 护栏：fork 数量上限（PlanPolicy 校验，`plan_policy.go`）、每节点预算（§7.3）、合并审批（§3.3）。

### 4.2 与 plan 的关系

| 入口 | 何时用 | DAG 来源 |
|---|---|---|
| `fork_subagents` | 模型自由层：无依赖的并行派活 | 程序生成（本工具内部） |
| `plan_load/plan_run` | 固化层：goal 等调优流程、有依赖图的任务 | 模型构造（goal skill 引导） |

共用：workplan runner、NodeScope、worktree 生命周期、事件投影、merge-back、预算、审批。fork 是 plan 的无依赖特例，**不需要独立执行器**。

## 5. todolist 工具族

### 5.1 契约

```
todolist_init(items: [string])          → 建立清单（≤ 上限 N 项，PlanPolicy 校验）
todolist_add(item) / todolist_done(i)   → 增/勾选
todolist_status()                        → 当前清单快照
```

- 无 DAG 校验、无节点类型、无拓扑约束——模型自然语言维护；
- 与 plan 的区别：todolist 是模型自己的待办组织（无执行器、无证据回传），plan 是确定性 DAG（有执行器、有节点证据）。

### 5.2 状态与投影

- 清单状态入 `SessionContextRecord` 的 SkillStack 同族栈帧或独立帧（`now using todolist` = 栈顶语义复用）；
- 事件投影 → GUI 清单面板（勾选实时可见）；
- 全部 done → 触发任务终态语义（衔接 `task_complete` 投影校验，`task_terminal.go`）。

### 5.3 与 Task/Plan 的关系

| 机制 | 角色 |
|---|---|
| TaskStack | 任务生命周期（开始/终态），不变 |
| todolist | 模型自由层的计划组织，无执行语义 |
| PlanStack | 固化层的确定性 DAG，执行语义在 workplan runner |

todo 全 done 与 plan 全节点完成都走同一 `task_complete` 投影校验路径（§4.5 架构文档 Task 机制）。

## 6. plan 工具面归位（已确认决策 ④）

- `plan_load/plan_clear/plan_run/plan_status/plan_export/plan_validate` 从主工具面**移出**（`planToolProvider.Tools()` 不再恒返回全部）；
- 随 goal skill 激活注入：复用 `activateTaskSkillsLocked`（application/core/chat.go:48）skill 激活机制——goal skill 激活时把 plan 工具集并入可见集；
- 模型默认工具面 = todolist + fork + 文件/搜索/bash/skill/MCP/上下文工具；
- replan/计划恢复机制保留（goal 流程用）；`plan_run` 不再需要模型默认可见。

## 7. 子代理对齐 mainagent

### 7.1 工具集去硬编码（A1，已实施）

- 删除 `nodeScopeToolVisible` 6 工具特判，子代理继承 mainagent 完整工具面（skill/MCP/普通工具全可见）；
- 可见性只由插件 include/exclude 策略（`seelexVisibilityPolicy`，registry.go:94）与权限门控驱动；
- 例外（已实施，`nodeScopeExcludedTool`）：**操作全局状态的工具仍对子代理不可见**——plan 工具族（plan_load/plan_clear/plan_run/plan_status/plan_export/plan_validate）+ task 终态工具（task_complete/task_failed/task_needs_user_decision）。理由：这些工具操作 runtime/会话级单例状态（`planToolProvider.loaded`、TaskStack 终态），并发子代理调用造成语义冲突（子代理 plan_run 递归嵌套 DAG、task_complete 错误终结主任务）。这属于"状态所有权"而非"能力"差异；
- 安全语义不变：权限门控（`SetPermissionConfig`）对主/子代理同等生效；路径门禁（pathgate.go）不变。

### 7.2 skill 能力（A2，已实施）

- **skill 是 actor 包装的资源**（2026-08-03 用户澄清）：`skill.Registry` 内部自锁，读写即消息进出（All/Get 读、Register/Reload 写）；seelebridge 直接持有引用（`Runtime.skills`，装配一次性写入、运行期只读消费），**不加外层锁**（与 filesystem actor 同构）；
- 接线：main.go `runtime.SetSkillRegistry(skillRegistry)`；
- 注入：节点 PromptBlocks 追加两块——
  - `node-skill-catalog`：全部可见 skill 的名称 + 描述（目录，始终注入）；
  - `node-skill-active`：与节点目标匹配（名称分词/描述词出现在节点 input）的 skill **完整指令**；
- 未装配 registry（nil）→ 无块，行为降级；
- 与 mainagent SkillStack 的关系：目录块 = "读取 skill 目录"的等价能力；完整指令注入 = 激活。mainagent 的当前激活 skill 继承语义（跨层传值）留待后续。

### 7.3 预算参数注入（A3，已实施）

- `SeelexNodeInput` 增可选字段（plan_factory.go，schema 已入 plan_load 契约）：

```json
{"id": "x", "input": "...", "budget": {"max_loops": 8, "max_output_tokens": 4000}}
```

- 缺省回退 `limits.PlanNodeMaxLoops`（当前默认 15，limits.go:63 保留为缺省而非唯一入口）；
- PlanPolicy 增上限字段 `MaxNodeLoops`/`MaxNodeOutputTokens`（0 = 不限），超限 plan_load 拒绝；effort 档位已设默认上限（high=48、max=96）；
- 生效点：`nodeBudget(input)` 优先节点值（agent_node.go）→ 节点预算 PromptBlock + **节点会话 `SetMaxLoops` 动态覆盖**（Session 动态方法，agent_node.go Run 内类型断言后调用）。

### 7.4 上下文长度按节点账号推导（A4）

- 节点窗口/压缩阈值按 role+branch hash 路由到的账号（`resolvePlanBranchAccount`，branch.go:95）context window 推导；
- `nodeBudget.MaxOutputTokens` 已按账号 limits 读（agent_node.go:206-209），窗口策略（`WindowPolicy`）补节点侧实例化。

### 7.5 子代理收尾提示词契约（C3，已实施）

节点 PromptBlocks 新增固定块 `node-finish-protocol`（agent_node.go nodePromptBlocks）：

```
## 任务结束流程 (Finish Protocol)
1. 完成标准：任务可验证（检查项/测试通过）才算完成。
2. 收尾序列（按序执行）：
   a. 若有文件改动：git add -A && git commit -m "seelex/<nodeID>: <摘要>"
   b. 变基：git rebase <主分支>（合并最新变更，冲突自行解决）
3. 明确禁止：不 merge、不 checkout 主分支、不触碰主工作区（合并是框架的事）。
4. 最终回复：给出结构化 findings（结论/改动文件/验证结果），供 merge-back。
```

- fork 子代理与 plan DAG 节点共用同一契约；
- 非 git/降级节点：跳过 a/b，直接执行 1/4。

## 8. 前端：子代理详情查看（B1）

按 [`docs/plan/subagent-detail-architecture.md`](../plan/subagent-detail-architecture.md) 实施（现状只有 lifecycle 事件 + 最终输出弹窗）：

1. Runtime 节点会话注册表：`NodeSessionConversation(nodeID)`（运行中读子会话 History——子代理 actor 独立锁，安全；结束读 `lastNodeConversations` 快照）；
2. 遥测活动投影：llm/tool 事件按 nodeSessionID 过滤 → `SubagentActivity`；
3. application：`SubagentSessionDetail`（对话适配 + 活动 + 截断 ≤50 条/节点）；
4. GUI：详情弹窗"会话记录"标签（目标/对话流/工具调用）+ 运行中 2s 轮询。

## 9. 主代理性能：滑动窗口加载区间（D1）

- 现状：`DurableHistory.Load` 全量加载（sessionstore/durable_history.go:46），`ReadEventTail`/`ReadRange` 窗口读能力闲置（sessionstore.go:257-279 已实现）；
- 改造：会话加载/恢复改走窗口读尾（token + unit 预算），窗口外由 CompactStack 摘要承接（架构文档 §4.8.3）；
- 验收：长会话请求只装载窗口区间；`LoadEventTail` 有真实调用方；token 账本可审计。

## 10. 沙箱（C4）

- 按 `docs/2026-07-28-project-session-scope/sandbox-research.md` 落地执行隔离（Linux bubblewrap / macOS seatbelt / Windows AppContainer 或 isobox 端口）；
- bash 与写操作在沙箱内执行验证；worktree（§3）与沙箱互补：物理隔离（并行）+ 执行隔离（命令）；
- 主代理与子代理共用同一沙箱机制（子代理与主代理能力一致原则）。

## 11. 弱模型兼容（E1/E2）

| 问题 | 本设计解法 |
|---|---|
| 不会构造 plan JSON | 默认路径改为 todolist + fork（§4/§5），plan 从弱模型路径上消失（§6） |
| 不知道怎么结束子代理 | 收尾提示词契约（§7.5）+ harness 自动终态兜底（自然停止已存在） |
| 乱写 txt 文件 | worktree 物理隔离（§3）+ 沙箱（§10）+ 合并前审批（§3.3）三层兜底 |

## 12. 实施步骤（切片）

每个切片保持 `go build ./...`、`go vet ./...`、测试通过。

| 切片 | 内容 | 状态 |
|---|---|---|
| 1 | A1 移除硬编码工具白名单 + 插件策略统一 + 收尾提示词契约（§7.1/§7.5） | ✅ 已实施（v1） |
| 2 | A2 skill 接入节点会话（§7.2，actor 化：直接持有 Registry，无外层锁） | ✅ 已实施（v1） |
| 3 | A3 预算参数注入（§7.3，含 SetMaxLoops 动态覆盖）；**A4 节点窗口按账号推导未实施**（nodeBudget.MaxOutputTokens 已按账号，窗口策略仍 runtime 级） | ✅ A3（v1）；A4 遗留 |
| 4 | C1 worktree 生命周期（§3）：开/切/变基兜底/合并审批/降级/冲突（worktree.go）；降级 = 非 git/创建失败共享工作区；合并审批复用 approvalGate（diff stat 摘要）；失败保留现场 | ✅ 已实施 |
| 5 | C2 fork 工具（§4）：fork_tool.go；程序化 DAG start→N×agent→summary（forkSummaryNode 拼接 PrevResults）；fork_subagents 对子代理不可见（递归护栏） | ✅ 已实施 |
| 6 | A5 todolist 工具族（§5）：todo_tool.go；actor 状态（todoState）+ limits.todo_max_items；全 done 提示 task_complete；会话级持久化未接（留 SessionContextRecord 后续） | ✅ 已实施 |
| 7 | A6 plan 工具面随 goal skill 注入（§6）：seelexVisibilityPolicy 按 SetGoalSkillProvider 判定（main 从 app.ActiveSkillIDs 注入）；默认面 = todolist + fork；replan 恢复路径测试需 goal 激活 | ✅ 已实施 |
| 8 | B1 前端子代理详情（§8）：节点会话注册表（NodeSessionConversation 实时/快照）+ application SubagentSessionDetail（截断）+ GUI 弹窗三标签（会话记录/时间线/输出）+ 运行中 2s 轮询；遥测活动投影（SubagentActivity）未做（会话记录已覆盖主要诉求） | ✅ 已实施 |
| 9 | D1 滑动窗口加载区间（§9）：DurableHistory.SetTailBudget + Load 窗口读尾（eventsToMessages）+ runtime 主会话装配（ctxStore 接线后生效）；会话恢复流程本身未完全接线（架构文档 slice 6+ 遗留） | ✅ 已实施 |
| 10 | C4 沙箱（§10）：CommandSandbox 端口 + native 实现（cwd 门禁 + 凭据环境变量清洗 + 能力报告 + fail-fast）；isobox/agentbox 适配留接口，OS 级隔离待成熟后接入 | ✅ 已实施（端口版） |

## 13. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| worktree merge 冲突（主工作区新提交） | 节点失败 | abort + 现场保留 + 冲突文件清单报告（§3.6） |
| fork 泛滥（token/时间成本） | 开销失控 | 数量上限（PlanPolicy）+ 节点预算（§7.3）+ 合并审批 |
| 合并自动进用户工作区 | 意外改动 | 合并前审批 + diff 摘要（§3.3） |
| 子代理在 worktree 内忘记 commit | rebase/merge 无法进行 | 收尾提示词强约束 + 框架检测"worktree 脏且未 commit"报错 |
| Windows 路径长度/worktree 位置 | 创建失败 | sibling 目录短名 + 降级共享工作区（§3.5） |
| plan 移出后模型失去结构化能力 | goal 场景仍要 plan | 工具面随 goal skill 注入，不丢失能力只挪入口（§6） |
| 窗口读截断导致信息缺失 | 模型上下文不足 | 窗口 N 由 WindowPolicy 推导 + CompactStack 摘要承接（§9） |

## 14. 验证

- 切片级测试：fork 程序化 DAG 等价性（与手写 plan JSON 同结果）、worktree 生命周期状态机（含失败/降级/审批路径）、预算参数覆盖、skill 块注入、todolist 终态衔接；
- 集成验收（确定性 completer）：`fork_subagents → 并行 worktree 子代理 → 合并审批 → 汇总回传` 全链路；`goal skill 激活 → plan 工具可见` 投影验证；
- 弱模型（minimax）实测：能完成"todolist 建清单 → fork 派活 → 收尾回传"闭环；不再乱写 txt（沙箱 + worktree 兜底）；
- GUI 构建（`-tags gui` + ldflags）通过；子代理详情弹窗显示会话记录与实时活动。

## 15. 2026-08-04 事件链补强

- Runtime middleware 只识别 `RoleSubAgent`，发布工具 `running/success/error`；主代理工具继续走原 ToolHook，避免重复事件。
- worktree 创建、rebase、merge 分别投影为 `worktree_creating`、`rebasing`、`merging`，持久事件携带 `agent.runtime/session_id` Location。
- Application 递归定位嵌套 Plan 节点，按工具调用 ID upsert 有界 `tool_events`，发布 `subagent.changed`、`subagent.tool.started`、`subagent.tool.completed`。
- GUI Bridge 原样 relay 到 `seelex:event`；前端 reducer 深拷贝 Plan 树并递归更新，详情页展示会话、生命周期时间线和工具输入/结果/错误。
- `gui/frontend/dist/event-chain.test.mjs` mock Wails `seelex:event`，验证事件不触发 Snapshot reload 且最终工具状态在前端节点可见；Go bridge relay 和 Runtime/Application 分层测试共同覆盖整条链路。
