# Teamwork 口径整条收口：stages 退场 + 三个真实缺陷的现场与修复

- 日期：2026-10-04
- 主题：把团队作业面从「阶段制」整条铲到「里程碑屏障 + Work Item DAG」，并把
  **teammate 做完自动返回 / 看板回执可见 / 这件事自己的会话**三条链在**真实装配**上跑通
- 证据：一条 headless 逐阶段冒烟（P0–P7 全绿）+ 全量包测试 + 前端 node 用例
- 相关锚点：`docs/devlog/2026-10-03-teamwork-scope-alignment-progress.md`（问题清单）、
  `docs/arch/teamwork-leader-worker-architecture.md`、`teamwork_headless_smoke_test.go`

---

## 0. 结论先行

1. **`stages` 整条退场**：计划结构、DTO、投影、工具 schema/description、前端渲染件与接线、
   TUI 面板、skill 正文、arch 文档里都不再有它（唯一事实 = `milestones[].depends_on` 屏障
   + `work_items[].depends_on` 里程碑内 DAG）。
2. **「做完自动返回」原来在链路上断在四处**，每一处都是静默失败（不报错、只是什么都不发生）。
   修完之后，冒烟里 **leader 回合=9（含自动返回 1）· worker 回合=1**，空闲会话被起回合，
   正文自带收口姿势（`team_context` / `team_accept / team_fail`，且**不**指向 `job_manage`）。
3. **看板"跑完了看不见回执"再现**，根因这次在**投影缓存**：自动尾插这条写路径不经过工具，
   所以团队的 `team_*` 失效点覆盖不到它。修复 + 用例在 `seelebridge` 落定。
4. **剩下两个已知缺口**（未修，已留痕）：工具错误的**用户可见原文**被通用文案替换
   （`P3 拒绝原文可见=false`）；worktree 合并偶发 `git … The directory name is invalid`。

---

## 1. 逐阶段探针输出（headless 冒烟，同一条投影路径）

```text
P1 编排面装配    team_* 9 件在面 · teammate 作业信号口在
P0 会话与项目    session=sess_… git-root=…\002
P2 计划与看板    milestones=2 屏障=[m-contract] work_items=2 无 stages 键
P3 屏障负路径    调用状态=[success error] · 拒绝原文可见=false（呈现面=""）
P3 排活          work_items=2（全在 m-contract）· exec 队列=[实现]
P4 teammate 作业  handle=a1 state=done exit=0 归属=exec/wi-impl summary="exit=0 · 1 行"
P4 落盘事实       wi-impl 状态=review 句柄=a1 note="跑完待验收"
P4 尾插回执       teammate=exec 状态=free 回执="[wi-impl] 实现（里程碑 m-contract，角色 exec）：
                 跑完，改动已合并回主工作区，等待 leader 评估" 工作项状态=review
P5 自动返回       空闲会话被起回合；正文含 team_context/team_accept，未指向 job_manage
P6 当前 teammate 会话 session=…-smoke-team-exec-wi-wi-impl role=exec 行数=2（含本轮工作正文）
P7 验收与闸门     accept ok · wi-impl=done · 下游 wi-check 受理回执={"handle":"a2",…"item":"wi-check",…}
汇总             leader 回合=9（含自动返回 1）· worker 回合=1
```

跑法：

```text
go test -run TestTeamworkHeadlessSmoke -v -count=1 .
```

**为什么必须这一层**：假装载只能证明"我调用过的那些函数是对的"。这条链的每一跳都在不同层
（工具面 → 编排域 → `jobs.Manager` → 应用层触发 → 会话投影），三个现场（切会话看板消失 /
做完不自动返回 / 看 teammate 会话看到的是历史）全是**跨层错位**。P0–P7 每一步都留痕，
失败时能直接指出是哪一跳断的——本轮四次红都是这么定位的。

---

## 2. 缺陷 A：teammate 作业终态链在**窄端口**上断链（已修）

- **现象**：作业跑到终态、ledger 里 `status=done`，但空闲会话不会被起回合（"做完不自动返回"）。
- **根因**：`application/core` 侧靠**类型断言**读窄可选端口
  `runtime.(contract.TeamworkJobCompletion)`（`async_completion.go` 的 `teamworkJobEvents`）。
  生产包装 `internal/adapters.RuntimePort` **只实现了看板与会话两个窄端口**，
  没转发 `TeamworkJobCompletions/TeamworkJobEvents` → 断言失败 → 信号口读成 `nil` →
  `select` 里的 nil 通道永不触发 → **整条链静默不存在**（不报错、不打日志）。
- **修复**：`RuntimePort` 补转发，并把编译期断言补进那份"探测一个就必须转发一个"的清单：

```go
_ contract.TeamworkJobCompletion = RuntimePort{}
```

- **教训**：这个仓已经为这类"静默 false"写过防御（断言块），新加窄端口时**清单也要加**——
  断言不是文档，是编译期闸门。

## 3. 缺陷 B：装配顺序是契约的一部分（已修，测试基座侧）

- **现象**：冒烟按组合根**后置**注入 teamwork 面（先 `application.New`，再 `SetTeamworkBackend`）
  时，自动返回照样不发生。
- **根因**：应用的四个**生命周期消费者**在 `application.New` 里就把信号口读走了
  （`consumeAsyncRuns`）。后置注入 ⇒ 消费者捕获的是 `nil` ⇒ 同上，链子静默不存在。
  生产组合根的顺序是对的（`main.go`：`SetTeamworkBackend` 在 `initApplication` **之前**），
  是测试基座把顺序写错了。
- **修复**：`newFullChainHarnessWithLimits(..., installers ...fullChainBackendInstaller)` ——
  在 store/workspaces 就绪之后、`application.New` **之前**执行注入钩子；
  冒烟用这个钩子按组合根的位置装配。**顺序本身写进注释当契约**。

## 4. 缺陷 C：全局 limits 没应用，开关永远是关的（已修，测试基座侧）

- **根因**：`limits.async_exec.trigger_conversation` 的**读侧**读进程内那份生效 limits
  （`core.Limits()`，组合根用 `core.ApplyLimits` 注入），不是 `RuntimeConfig.Limits` 那一份。
  冒烟只设了后者 ⇒ 开关保持默认（关）⇒ 消费者连扫描都不做。
- **修复**：冒烟按组合根同序 `core.ApplyLimits(limits)`，用例结束后还原。
- **顺带教训**：**默认关**的开关 + 静默跳过 = 假绿（"什么都没发生"也叫通过）。
  断言必须建在"发生了"上（这里就是"空闲会话被起了回合"这条用户行）。

## 5. 缺陷 D：看板缓存没在**自动尾插**这条写路径失效（已修 + 用例）

- **现象**：作业终态、计划里已经 `review`、回执也进了消息队列，但看板一直显示
  `running` + `Messages=[]`，直到下一次 `team_*` 调用把它撞醒——用户看到的仍是
  "跑完了看不见回执"。
- **根因**：`runtime_teamwork_board.go` 的「计划 + 审计」两份文件读按会话缓存，
  失效点是**13 处 `team_*` 工具 handler**（`invalidateTeamworkBoard`）；而
  `teamwork.ItemSettler` 的尾插（状态收敛 + 回执 + settle 审计行）**不经过工具**，
  于是投影继续吐旧计划。**投影陈旧被误判成"尾插没落"**——冒烟里靠"落盘事实/投影分开读"
  的探针一次分清（落盘 `review`、投影 `running`）。
- **修复**：`Runtime.SettleWorkItem` 出口 `defer r.invalidateTeamworkBoard()`。
- **用例**：`seelebridge/runtime_teamwork_board_test.go`
  `TestSettleWorkItemInvalidatesBoardCache`（先采集灌缓存 → 直接调 `SettleWorkItem` →
  再采集必须是 `review`；缓存不失效就会停在 `running`）。

## 6. 冒烟自身的两个判定坑（都已修，值得记）

1. **不要在 wire 字符串上子串匹配**：请求体里 `<` `>` 是 `\u003c` `\u003e`，
   `strings.Contains(body, "<round_input>")` 永远不命中 ⇒ worker 回合被当成 leader 回合，
   把脚本给 leader 的回应（验收、派下游）喂给 worker，每一跳都错位（现场：P4 拿到的是
   第二个作业的 handle）。改为**解码 messages 后**再判定（`requestBodyText`）。
2. **判定只看最后一条 user 消息**：自动返回的回执作为一条 user 行留在会话里，
   按"整段里有没有"判会把此后**每一个**回合都认成自动返回回合，那些回合的脚本回应
   （验收、派下游）就永远发不出去。改为只看本轮输入。

---

## 7. 口径整条退场的落点（stages）

| 层 | 落点 |
|---|---|
| 计划结构 / 持久面 | `sessionstore.TeamworkPlan`（无 `Stages`；里程碑内嵌 `Items`）、`State` 去掉 `stage` |
| DTO | `application/contract/dto/teamwork_board.go`（无 `Stages`；`WorkItems` + 成员 `CurrentSessionID/CurrentWorkItem`） |
| 投影 | `seelebridge/runtime_teamwork_board.go`（`teamworkPlanHasOrchestration` 判据 = 有里程碑） |
| 工具 schema/description | `seelebridge/runtime_teamwork_schema.go` + `runtime_teamwork_schema_test.go`（属性整条删除、不在 required、description 不再讲阶段） |
| 前端 | `gui/frontend/dist/team-board-view.js` / `app.js` / `team-board-view.test.mjs`（无 `orderStages/jobsByStage/stagesOf`，退场判据只看里程碑与工作项） |
| TUI | `tui/goalteam.go`（里程碑 + 工作项行，无阶段行） |
| skill | `plugins/default/plugin.md`、`plugins/default/goal/SKILL.md`（§2/§3/§4 改写）、`plugins/default/teamwork/SKILL.md` |
| arch 文档 | `docs/arch/teamwork-leader-worker-architecture.md`§4.5/§4.6/§6.2、`team-board-gui-tui-contract.md`§2/§5/§6/§7、`session-board-metadata-lifecycle.md`、`seelebridge/teamwork/README.md`、`application/core/agentteam/README.md`、`dto/agentteam.go`（历史字段的"新事实"指向改口径） |

边界：`order_policy/order_roles` 是**另一件事**（历史/只读字段，退场被
`m4-deadcode-inventory` 标 blocked），本次只把它们的"新事实"指针改到里程碑口径，不动落盘取值。
`docs/arch/a2a-agent-team-factory.md` / `agent-team-seat-vs-claim.md` 里写明的**历史口径**保留。

---

## 8. 已知缺口（未修，留给下一轮）

1. **工具错误的用户可见原文被替换**（冒烟 P3 探针留痕 `拒绝原文可见=false`）：
   `team_work` 的拒绝原文（"里程碑 m-docs 依赖的里程碑 m-contract 还没完成"）在会话里
   被通用文案（"该工具未能完成本次操作…"）盖掉——模型与用户都看不到具体原因。
   这是**根因可见性**问题，不是团队面的问题，归工具执行面。
2. **worktree 合并偶发失败——已复现并定因（原样保留为例外路径的证据）**：
   `git [rev-list --count <base>..HEAD]: fork/exec …\git.exe: The directory name is invalid`。
   - **成因**：一条链上的两个动作对**同一份现场**并发动手——尾插
     （`SettleWorkItem → MergeWorkspace → WorktreeManager.Finish`，两条 git 都跑在
     `wt.Path`）与验收释放（`AcceptItem → releaseItem → ReleaseWorkspaceItem →
     CleanupWorktree → git worktree remove --force`）。目录一没，后面的 git 连子进程都
     起不来（cwd 不存在），Go 在 CreateProcess 上报 ERROR_DIRECTORY，**git 自己一句话
     都没说**。放行的判据是 `AcceptItem` 的 `case TeamworkItemReview, TeamworkItemRunning:`
     ——running = 作业还在飞 = 尾插还没跑完（`team_close` 的 `releaseAllItems` 同理，
     它释放的是**全部**活绑定，不看状态）。
   - **指纹**：报错指向哪条命令，就是对端在哪一拍动手——现场报的是第二条
     （`commitCountSince`），所以释放发生在 `branchBehindBase` 与它**之间**。
   - **复现**：`seelebridge/worktree/worktree_vanished_scene_repro_test.go`
     （真实 git，两种时序各一条，逐字复现报错原文）+
     `seelebridge/teamwork/items_accept_running_repro_test.go`（把 worker 停在半路，
     钉住"在跑的工作项被放行验收 ⇒ 现场被释放"）。
   - **后果（比报错本身更重）**：那条交错里合并被**跳过**，而 `CleanupWorktree` 释放时
     会 `git branch -D seelex/<item>` —— teammate 这一轮的提交因此只剩 reflog 可达，
     而尾插回执还在教 leader「请 leader 亲自执行合并」（那时已经没现场可合了）。
   - **已定责（2026-10-04，按用户裁决落地方案 ①）**：产出的责任链只有一条——
     **合并（尾插步 1）→ 回执（步 2）→ 状态（步 3）→ leader 审查 → 验收入账 / 重新派活 /
     整队收口**；下游动作只许在上游走完之后动手。落地：`AcceptItem` 对「在跑」拒收
     （判据沿用既有的 `running && handleAlive`——与 `DispatchItem` 的重复派发、
     看板 Interrupted 投影同一处口径；handle 已作废＝这一件事的尾插不会再跑 ⇒ 仍放行，
     保住"重启后收尾"那条路）；`team_accept` 的工具描述同步写明这条链。用例：
     `seelebridge/teamwork/items_accept_chain_gate_test.go`（在跑 ⇒ 拒且**不动现场**；
     handle 作废 ⇒ 放行）。
   - **残留窗口（未堵，如实记）**：整队收口这条路**已经**是"先合并再释放"——`retireSteps`
     步 1 的 `jobs.Reclaim` 会 cancel **并等作业体结束**，而作业体里就含尾插（合并 → 回执
     → 状态），且 store 与 `GitRunner` 都不吃 ctx 取消（取消不会把合并拦腰截断）。但
     `Reclaim` 的等待有上限：`jobs.Limits.DefaultWait` 默认 **5s**
     （vendor `jobs/options.go:41`）——合并/rebase 超过 5s，Reclaim 就带着"作业还在跑"
     返回，随后的释放仍可能抢在合并前面。要不要再收（例如释放侧复核一次 `handleAlive`）
     留待后续。
3. **`stages` 退场后没有"只读计划入口"**：确认里程碑/工作项仍靠 `team_items` + 拒绝原文。

---

## 9. 复跑命令

```text
go build ./...
go test ./application/... ./sessionstore/... ./seelebridge/... ./tui/... ./gui/... -count=1
go test -run TestTeamworkHeadlessSmoke -v -count=1 .        # 逐阶段探针
node --test gui/frontend/dist/team-board-view.test.mjs       # 前端渲染件纯函数
```
