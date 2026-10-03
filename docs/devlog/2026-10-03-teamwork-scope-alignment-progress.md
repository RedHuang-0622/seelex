# Teamwork 口径对齐：进展打点与待改清单

- 日期：2026-10-03
- 主题：团队作业面从「阶段制（`stages[]`）」迁到「里程碑屏障 + 里程碑内 Work Item DAG」
- 状态：口径代码已落地（`41d6330`），skill/文档侧与两处运行时缺陷仍在清单上
- 相关锚点：`docs/devlog/2026-10-03-workitem-milestone-refactor.md`、`plugins/default/teamwork/SKILL.md`、`docs/arch/teamwork-leader-worker-architecture.md`

---

## 0. 目标

把 Seelex 团队作业面从「阶段制」口径彻底迁到「里程碑屏障 + 里程碑内 Work Item DAG」口径，
并让**所有对外口径一致**（工具 schema/description、skill 正文、文档、运行中的 dev 包）；
同时用一条真实的 V 模型流水线把 leader 面（派发不等待 / 尾插自动返回 / accept 释放 / 屏障闸门）跑通并留证据。

完成条件（逐条可核对）：

1. `team_plan` / `team_work` / `team_dispatch` / `team_accept` 的工具 schema 与 description
   只讲里程碑 + Work Item，`stages` 不再列为必需字段；
2. 仓库内 skill 正文（`plugins/default/teamwork`、`plugins/default/goal`）与文档锚点同口径；
3. 运行中的 dev 包（`dist/seelex-gui-dev`）读到的是新口径（文件层面 + 注入层面各一次证据）；
4. 一条真实流水线：exec 与 verify 两件事**各自一套 Session + git worktree**，verify 只读权责；
   里程碑屏障把 `m-docs` 挡在 `m-contract` 之后（负路径有拒绝原文）；
5. 全过程证据（提交 sha / 工作项状态 / 拒绝信息 / 缺陷复现）在本文件与团队看板可回读。

---

## 1. 口径基线（当前真口径）

- **顺序的唯一事实**：`milestones[].depends_on`（屏障，里程碑之间串行）
  + 里程碑内 `work_items[].depends_on`（DAG）；`stages` 只是历史字段。
- **执行隔离**：一 Work Item = 一 Session + 一 git worktree；一个 teammate 在一个里程碑里可承担多个 Work Item。
- **派发即返回**：`team_dispatch` 拿 handle 不等待；作业完成 → 自动合并该 worktree →
  往消息队列**尾插**有界回执 → leader 在**回合边界**读；`team_join` 只用在真实强依赖点。
- **取回产出**：优先 `team_context`（非消费读，不推进游标）；`jobs_manage(op=fetch)` 取尽即销项。
- **释放隔离**：发生在 `team_accept`（工作项 → done，同时打开下游依赖闸门）；
  `team_fail` 保留现场与会话，重派复用同一会话号。
- **分里程碑排活**：`team_work` 一次只排一个里程碑；依赖里程碑未 done 会被拒收。

---

## 2. 已做（带证据）

| # | 事情 | 证据 |
|---|---|---|
| 1 | 团队面 Work Item 里程碑重构 + 收口面（`team_close`/`goal_done`）落地 | 提交 `34605d2`、`b9b2303` |
| 2 | dev 包 `plugins/` 同步修正：只覆盖不删除（此前「skill 口径在运行中的 GUI 上看起来没生效」） | 提交 `e7c9aab`、`aad4923`；`plugins/default/teamwork/SKILL.md` 与 `dist/seelex-gui-dev/plugins/default/teamwork/SKILL.md` **同为 10928B**（源 23:41:00 / 包 23:57:01） |
| 3 | 计划：在编成员 + 两道里程碑屏障 `m-contract` → `m-docs` | `team_work(milestone="m-docs")` 拒绝原文：「里程碑 "m-docs" 依赖的里程碑 "m-contract" 还没完成（状态 "active"）」 |
| 4 | 负路径取证：① 下游未 done 不可派发；② 屏障未开给 `m-docs` 排活被拒 | 同上；`m-skill` 探针返回「计划里没有里程碑 "m-skill"」，反证 `m-docs` 是计划内 id |
| 5 | `wi-schema`（exec，独立 worktree `seelex/exec-wi-schema`）：`team_plan` 的 description 与 schema 改成里程碑口径，`stages` 移出 required，新增 `runtime_teamwork_schema_test.go` | 提交 `41d6330`（schema +34/−9、新增用例 +89），**已在 main**；工作项 `done` |
| 6 | 真实缺陷 A 复现 + 现场修复（见 §3.1） | `team_accept` 报 `exit status 128 / not a working tree`；`git worktree add --detach` 重新登记后 accept 通过 |
| 7 | `wi-schema-check`（verify，只读，独立 worktree `seelex/verify-wi-schema-check`）已跑完 | handle `a2`：`started_at 1791043455` → `finished_at 1791043534`，`status=review`、`note=跑完待验收` |
| 8 | 「做完是否自动返回」实验（进行中，未定论） | 见 §3.2 |

---

## 3. 已确认的缺陷与现象

### 3.1 缺陷 A：accept 释放 与「完成后自动合并」重复动手（团队面 bug）

- **现象**：`team_accept(wi-schema)` 失败 ——
  `worktree remove --force G:\Program\go\seelex-seelex-exec-wi-schema`: **exit status 128**。
- **现场事实**：
  - `git worktree list` 只剩主工作区 `G:/Program/go/seelex`（无该绑定）；
  - 分支 `seelex/exec-wi-schema` 不存在，`.git/worktrees` 不存在；
  - 手动执行同样的 `git worktree remove --force <path>` → `fatal: '<path>' is not a working tree`；
  - 而 **main HEAD = `41d6330`（正是 exec 的那次提交）**，`git reflog` 记为 `commit:`。
- **结论（已确认）**：作业完成时框架**自动合并了该 worktree**（改动确实落到 main），
  但账本里的 worktree 绑定没有同步释放；`team_accept` 的释放步骤对同一份绑定**再动手一次**，
  必然 128 → **工作项无法验收、下游依赖闸门被卡死**。
- **本次绕过**：`git worktree add --detach G:/Program/go/seelex-seelex-exec-wi-schema 41d6330`
  把该路径按原样重新登记，再 `team_accept` 即通过。
- **待改**：合并成功即同步释放绑定；或让释放步骤**幂等**（路径不存在 ⇒ 视为已释放）。

### 3.2 现象 B：「做完自动返回」——尚未观察到

- **实验口径**：verify 作业 `a2` 全程**未被** `team_join` / `jobs_manage(op=fetch)` 触碰，
  看它跑完是否自动回到 leader 上下文。
- **事实**：`a2` 已 `finished_at=1791043534`、`status=review`、`note=跑完待验收`（ledger 知道它完成了）；
  但到本回合开始，leader 上下文里**还没有出现**该作业的尾插回执。
- **判定**：**尚未观察到自动返回**（待下一次回合边界再确认一次）。
  若仍不出现，则按 handle `a2` 主动取回，并把「未自动返回」作为实证记回本文件与 `$teamwork` 口径。
- **后续（2026-10-04 已定论）**：不是"回合边界还没到"，而是那条链子有**四处静默断点**——
  窄可选端口没转发（`adapters.RuntimePort` 缺 `TeamworkJobCompletion`）/ 装配顺序（消费者
  在 `application.New` 里就把信号口读走）/ 全局 limits 未应用（开关默认关）/ 看板缓存没在
  **自动尾插**这条非工具写路径失效。四处都已修，逐阶段证据见
  `docs/devlog/2026-10-04-teamwork-scope-sweep-and-auto-return.md`。

### 3.3 现象 C：技能正文更新对「进行中的任务」不可见

- **现象**：本会话被注入的 `teamwork` 正文是**阶段制旧版**（`# Teamwork：V 模型 leader 工具面`），
  而磁盘上的 `plugins/default/teamwork/SKILL.md` 与 dev 包副本都是里程碑新版。
- **事实**：
  - 旧版正文只存在于 `stage/seelex-v0.1.1-*/plugins/...`（构建暂存，2026-10-02）与**本会话的 transcript 快照**；
  - `plugins_reload` 返回 `added/removed/updated` 全 `null` → **进程内注册表已是新版**（不是注册表滞留）。
- **代码锚点**：`skill_activate_tool.go:19-21,44-48`（激活时从进程内 `skill.Registry` 取正文）；
  `application/core/task_context/task_context_state.go:34-45,48-93`
  （正文作为 append-only internal user 事件落进 transcript；**同任务内同名不再补写**，
  去重基准是任务自身的 `ActiveSkills`，压缩裁掉后也不补写）。
- **结论（已确认）**：**改文件 ≠ 改口径生效**。进行中的任务会一直带着那份旧正文。
- **待决/待改**：
  ① 接受「口径改动对**新任务**生效」并把它写进口径（最低成本）；
  ② 或按 content hash 判定，正文变了就补写一条新事件（需先明确与 append-only 稳定前缀缓存的关系）。

---

## 4. 接下来要改什么（按顺序）

1. **收 `m-contract`**：读 `a2` 的三条复核结论（① milestones 是否含 `depends_on`/`name`；
   ② `required` 是否已不含 `stages`；③ 用例是否真钉得住）→ 证据够就
   `team_accept(wi-schema-check)`；不够就 `team_fail` 重派（复用同一会话号）。
2. **`m-docs` 排活**（屏障一开）：已备好工作项 `wi-skill-scope`（exec，见 §5），
   外加一条只读复核项（verify）。
3. **缺陷 A 修复**（团队面）：自动合并 ↔ accept 释放不得重复动手；建议落一条用例：
   绑定已释放（路径/分支已不存在）时 accept 必须通过而不是 128。
4. **技能正文刷新语义**（§3.3）：二选一决策 + 写进口径。
5. **文档口径**：`docs/arch/teamwork-leader-worker-architecture.md` 若还残留 `stages` 现行口径，
   先 grep 定界再一并对齐。
6. **收口**：全部里程碑 done → `team_close` → goal 收口 → 结论与证据写回看板与最终回复。

---

## 5. `plugins/default/goal/SKILL.md` 的漂移清单（`m-docs` 的活）

| 位置 | 现文（旧口径） | 真口径 / 改法 |
|---|---|---|
| §2 | 「顺序写进 `team_plan` 的 `stages[].depends_on`（**唯一事实**）」 | `milestones[].depends_on`（屏障）+ `work_items[].depends_on`（DAG） |
| §2 | 「先算人再**铺阶段**」「要加角色就先裁阶段」 | 分里程碑排活（`team_work` 一次一个里程碑），先算人再定里程碑/工作项 |
| §2 | 引用 `$teamwork` 的「V 模型阶段模板」 | `$teamwork` 现在给的是「里程碑 + 工作项」模板 |
| §4.1 | 「取回产出：`jobs_manage(op=fetch)`」 | 尾插自动 + `team_context` 非消费读（`fetch` 取尽即销项，最不该在这里用） |
| §4.5 | 「`team_retire(role)`（释放 worktree + 清上下文）」 | 释放发生在 `team_accept`；工作项在飞时 `team_retire` 会被拒 |
| §4.6 | 「推进 active seq 并派下一阶段」 | 依赖边满足即可派（`work_items[].depends_on` / 屏障） |

---

## 6. 未决与风险

- 计划**没有只读入口**：里程碑 id 靠 `team_items` + 拒绝原文间接确认（本次如此确认 `m-docs`）。
- `wi-schema-check` 未验收期间，其 worktree `seelex/verify-wi-schema-check` 不应被触碰。
- 若自动返回最终不发生，leader 的「回合边界读回执」要退化为主动 `fetch`/`team_context`，
  这会连带改写 `$teamwork` §3 与 `$goal` §4 的描述。
