# ClaudeTeamwork 运行方式调研（A4，2026-10-01）

> **口径**：本文是**用例 5（切换对话视图到员工）的前置调研**的实测记录。
> 用 `browser_*`（Playwright）实际打开 `github.com/dperegolise/claude-teamwork` 并读取
> 其 README / `teamwork/commands/status.md` / `teamwork/CLAUDE.md` 原文，逐条给出证据，
> 再映射到 Seelex 的落点。本文不改代码；结论供 E（前端）与 C（会话粒度）使用。

---

## 1. 它是什么（README 原文证据）

- 「A self-contained Claude Code plugin that runs an autonomous, role-specialized engineering
  team using a multi-agent, role-specialized, **worktree-isolated** architecture. No external
  services, no MCP, no databases.」——编排跑在 Claude Code **原生 Agent Teams 原语**
  （`SendMessage` + 共享 task list + 自动 idle 通知）上；`.teamwork/` 只装**持久证据与 resume 底料**。
- 角色：Sentinel（lead，只编排不写码）→ Orchestrator（拆里程碑、派活）→
  Explorer / Worker / Reviewer / Critic / Auditor / Integrator。
- **isolation 与并行是同一件事**：lead 建 `.claude/worktrees/<id>`（分支 `<id>`）、
  `cd` 进去再 spawn——teammate 继承 lead 的 cwd，**开局就在 worktree 里**；
  Orchestrator 把独立里程碑（无共享依赖、文件不重叠）fan-out 成并行 Worker。
- **强制审计门**：`Stop` hook 物理阻断 run 完成，直到 Auditor 写出
  `.teamwork/reports/audit-PASS`（静态分析查"作弊"：硬编码测试输出、永远通过的 mock 门面）。
- **崩溃可恢复**：`SessionStart` hook 检测在途 run，新 lead 从磁盘 + worktree 分支 rehydrate。

## 2. 「看一个员工在干什么」= 什么（`/teamwork:status` 原文）

`teamwork/commands/status.md` 逐条列出 lead 观察面的**六个读数**（原样引用）：

1. 读 `.teamwork/state/run-state.json` → status、succession index、`next_action`；
2. `TaskList` → 按状态汇总里程碑（**共享 task list 是进度的唯一事实来源**；无 live team
   时回落到 `.teamwork/handoff/latest.json`）；
3. `git worktree list` → 有哪些里程碑分支/worktree 存在；
4. 列 `.teamwork/heartbeats/` → **哪些 teammate 活着、各自最后一次活动时间**；
5. `.teamwork/reports/audit-PASS` 是否存在 + 列 `.teamwork/reports/`（worker/review/critic/
   integrate/audit 产物）；
6. 今日 `.teamwork/logs/events-*.jsonl` 的**最后 ~10 行**。

**关键**：它**没有**「把对话视图切到某个 teammate 会话」这种能力——teammate 是 Claude Code
原生 teammate，人类观察 teammate 的方式是**split pane（tmux）看它的实时输出**，
而 `/teamwork:status` 给的是**汇总读数**（存活 / 最后活动 / 它在哪个 worktree / 它的报告产物）。

## 3. 映射到 Seelex（E 的设计口径）

| ClaudeTeamwork | Seelex 落点 | 说明 |
|---|---|---|
| `TaskList` 里程碑状态 | team plan（`sessionstore.TeamworkPlan` 的 stages/milestones）+ `jobs` 表 | 已有（`team_plan`/`jobs_manage`） |
| `git worktree list` | `seelebridge/worktree`（`WorktreeManager.Info/List`）+ 前端 `worktree-view.js` | 已有 |
| `.teamwork/heartbeats/`（存活 + 最后活动） | **不引入心跳**：改用 `jobs` 的观察面（`jobs_manage(op=observe)`）× 事件驱动热更新 + 手动刷新键 | 用户已定（E3「无心跳」） |
| `.teamwork/reports/*` 产物 | 员工 role draft / role session 备份行（`RoleSnapshot`） | 已有读面，缺刷新 |
| split pane 看 teammate 实时输出 | **切换对话视图到员工**：把会话视图切到该员工自己的 `role_session_id` 会话 | 用例 5 的正解 |
| `run-state.json.next_action` | team plan 的 `State`（stage / jobs 句柄投影） | 已有 |

**结论（用例 5 从"待调研"到"有解"）**：

1. 「切到员工视图」= 切到**该员工自己的角色会话**——Seelex 已经有 `role_session_id`
   这条一等身份（`RoleSnapshotWorkspace` 能读出它以角色视角看到的行），所以这不是新造
   概念，而是**把已存在的会话视图的目标换成员工会话**。
2. 「员工运行详情」= 三块读数之和：**作业读数**（`jobs_manage` observe：这个员工名下
   在跑/刚结束的 worker 作业）+ **工作区现场**（它的 worktree 路径 + 未提交改动）+
   **它自己的会话内容**（role draft / backup rows）。ClaudeTeamwork 的
   `status` 正是把这三种读数拼成一句话。
3. **无心跳**：ClaudeTeamwork 靠文件心跳判存活；Seelex 的作业表本身就是存活事实
   （running/done/killed 是状态机的终态，不需要"最后心跳时间"再推一层）。因此 E3
   用**事件驱动 + 手动刷新键**，而不是加心跳。
