# 真机复跑记录（2026-10-06 晚）：子代理 worktree 链 ✅ / teammate 接线档 ❌（探针陈旧）

> 本文档含**两轮**复跑：
> - 第一轮（§1–§4）：基线 `bdbfeba`，跑子代理链 + teammate 接线档；
> - 第二轮（§5）：基线 `809e6a3`（记录身份/恢复分链那一笔之后），子代理链 + 整包真机档 + teammate 装配档。
> 第二轮多出的两条真机红（fork / 第一视角）**不在第一轮范围内**，是"整包在真机门下跑"才暴露出来的。

## 0. 第一轮（基线 `bdbfeba`）

基线：`main` 头 `bdbfeba`，工作区干净（`git status --porcelain` 空）；`config/accounts.yaml` 在（provider: openai / bochaai）。
目标二进制：`tmp/bin/seelex-headless.exe`（本次由 `go build -o tmp/bin/seelex-headless.exe .` 重建，与 HEAD 同源）。
原始日志（未加工）：`_logs/live_r2_subagent_chain.txt`、`_logs/live_r2_team_work.txt`
（`>` 重定向经本机控制台代码页写入，长中文正文有替换字符；下面的**关键读数**都是 ASCII/短中文，逐字抄自原文。）

---

## 1. 真机 A：`seelebridge` 子代理 worktree 链 —— PASS

命令：

```powershell
$env:SEELEX_LIVE_SMOKE='1'
go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m
```

读数（原文）：

- `=== plan_run 耗时 14.6808881s ===`；`plan_run 输出：{"status":"completed","node_count":1,...}`（模型自己按收尾协议 `git add` / `git commit`，提交 `1ca91d4`）
- `现场在册：true  路径=<TEMP>\TestLiveSubagentWorktreeChain…\001-seelex-impl  采样=58`（现场落点**在主工作区之外**）
- `产出落在现场：true`
- `合并审批：[merge-impl] 子代理 impl 的改动将合并进主工作区（main） / live-acceptance.txt | 1 + / 1 file changed, 1 insertion(+)`
- `主工作区 git log：1ca91d4 live acceptance SEELEX-LIVE-286342 / 98c208a base`
- `--- PASS: TestLiveSubagentWorktreeChain (15.27s)`；`ok github.com/Red-Huang…/seelebridge 15.528s`，作业 `exit=0`

即：派发 → 真发现场 → 现场上真干活 → 过合并审批 → rebase/merge 回主工作区 → 成功路径析构，在 HEAD 上**一次运行里全部可达**。

## 2. 真机 B：`gui` teammate 接线档 —— FAIL（1.50s）

命令：

```powershell
$env:SMOKE_TEAM_WORK_LIVE='1'
$env:SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED='1'
go test ./gui -run TestRealAPITeamWorkContentLiveProbe -v -count=1 -timeout 20m
```

读数（原文）：

```
=== RUN   TestRealAPITeamWorkContentLiveProbe
    team_workcontent_live_probe_test.go:111: goal 创建后未见 tl 成员（自动装配未生效）：[]
--- FAIL: TestRealAPITeamWorkContentLiveProbe (1.50s)
FAIL	github.com/RedHuang-0622/seelex/gui	1.692s
```

### 2.1 根因（Confirmed）：探针前提已退场，不是产品回归

探针第 2 步的口径是「`goal.begin` → **隐式装配** `goal-a2a`（tl 成员 + `order_policy=goal_loop`）」
（`gui/team_workcontent_live_probe_test.go:95-113`）。**这条自动装配早已按设计删除**：

1. **真机观测**：`team.view` 返回 `members=[]` —— 主会话已物化（`main_session_id` 非空），团队确实没被装配。
2. **2026-10-01 提交 `970122c`**（`goal` 席位轮转退场 + 旧 goal 残余整肃）删掉 `ensureGoalAgentTeam`：
   `application/core/goal_service.go:46` 注释逐字写着「**不在 goal 上线时自动装配团队**（2026-10-01 删除
   `ensureGoalAgentTeam`）」，理由之一是「开始一个 goal 会把用户手工加的 worker/reviewer 一起冲成模板那三个人。
   这不是自动化的边界，是事故」。回归哨兵在 `application/core/goal_team_wiring_test.go:207` 附近
   （goal 上线**不得**改写/新建会话团队）与真机档 `gui/goal_team_wiring_live_probe_test.go`（文档头明说
   「自动装配随形态目录一起删除后，这条探针守的是『别再把它接回来』」）。
3. **探针的另一半前提也退了**：它第 4 步要在**普通工作回合之后**读到 tl 角色会话的 `role_context`（ADVISOR 回合原文）。
   席位轮转（回合尾自动推一轮 ADVISOR）在 2026-10-03 整条退场：`docs/devlog/2026-10-03-seat-rotation-retired.md`
   §2/§3 载明「goal 从此**没有任何座位概念**」，ADVISOR 只在**显式入口**跑真实 TL 回合
   （`goal_propose_finish` 的终态 gate / 审批预筛）；`application/core/agentteam/README.md:144` 记
   `Advance()`（经 `Runtime.Next`）**没有生产调用者**。

### 2.2 同族陈旧（同一前提，未跑）

- `gui/team_work_computer_use_live_probe_test.go:107-113`：同一句
  `goal 创建后未见 tl 成员（自动装配未生效）` —— 同一枚红，**本次未运行**（该档会把当前屏幕画面随提问发给 provider）。
- 文案残留：`application/core/goal/README.md:10` 仍写「接线现状：`GoalBeginFor` 在 goal 落栈成功后自动装配
  `goal-a2a` 团队（`ensureGoalAgentTeam` → `MaterializeAgentTeamPreset`），因此 **goal 上线即拉起 TL 团队**」——
  与同一子系统的 `goal_service.go:46` 直接冲突（陈旧句，待改）。

### 2.3 探针问的那个问题本身还没死

「EXEC 的工作正文是否进入 teammate 的输入」这条**机制**没退场：`AdvanceAfterChat` 仍登记本轮工作正文摘要
（`NoteWorkDetail` → 团队前缀 `SetPrefix`），确定性覆盖在
`application/core/goal_work_summary_test.go:112 TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor`。
死掉的只是**触发它自动跑一轮 ADVISOR 的那条链**。

### 2.4 当前架构下这条档要怎么才成立（两条路，待用户裁决）

- **A 退场**：删掉 `gui/team_workcontent_live_probe_test.go` 与 `gui/team_work_computer_use_live_probe_test.go`
  （判据随架构退场），并在文档里留一句退场理由；顺手修 `application/core/goal/README.md:10` 的陈旧句。
- **B 改写到当前口径**：显式 `team.materialize`（照 `gui/goal_team_wiring_live_probe_test.go` 的作法）装配 tl，
  再用现成的 `goal.propose_finish` RPC（`gui/headless.go:521`）**显式驱动一轮真实 TL 回合**，然后照原样读
  `role.snapshot(tl)` 的 `role_context`，验「工作正文 marker + `work.progress` 帧进 TL 输入」。
  这样这条真机档在当前架构下重新有意义（= `TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor` 的真机版）。

## 3. 复跑命令（原样）

```powershell
go build -o tmp/bin/seelex-headless.exe .
$env:SEELEX_LIVE_SMOKE='1'; go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m
$env:SMOKE_TEAM_WORK_LIVE='1'; $env:SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED='1'; go test ./gui -run TestRealAPITeamWorkContentLiveProbe -v -count=1 -timeout 20m
```

## 4. 未做 / 未决

- 未跑 `SMOKE_TEAM_WORK_COMPUTER_LIVE`（同前提陈旧；且会把当前屏幕发给 provider）。
- 未跑 `SMOKE_FORK_LIVE` / `SMOKE_SUBAGENT_LIVE` / `SMOKE_TEAM_LIVE` / `SMOKE_ROLE_LIVE` 等其余真机档。
- 未改任何代码/用例：本轮只跑真机 + 勘定。
- 未验证：当前架构下「leader 派活 → teammate 回合」的真机档是否已有覆盖（确定性覆盖在
  `seelebridge/teamwork_worktree_lifecycle_test.go` + `seelebridge/teamwork_headless_smoke_test.go`）。

---

## 5. 第二轮复跑（2026-10-06 22:58–23:05，基线 `809e6a3`）

基线：`main` 头 `809e6a3`（"记录快照记身份 + 恢复链分策略 + 写侧责任链"那一笔），工作区仅剩未跟踪的本文件；
`config/accounts.yaml` 在（`provider: openai` / `bochaai`）。目标二进制由本机重建：
`go build -o tmp/bin/seelex-headless.exe .` → `BUILD_EXIT=0`（与 HEAD 同源，供真进程探针用）。

原始日志（未加工，UTF-8）：`_logs/live_r3_subagent_chain.txt`、`_logs/live_r3_seelebridge_all.txt`、
`_logs/live_r3_team_probe.txt`、`_logs/live_r3_root_gates.txt`。

### 5.1 结论速览

| 档 | 命令门 | 结果 | 关键读数 |
|---|---|---|---|
| A 子代理 worktree 全链（`seelebridge`） | `SEELEX_LIVE_SMOKE=1` | ✅ PASS（14.55s / 再跑 6.96s） | 现场在册 `true` 采样 55；产出落在现场 `true`；过合并审批 `[merge-impl]`；主工作区 log 出现 `e4e17bb live acceptance SEELEX-LIVE-298717` |
| A′ 同一档在全包真机门下再跑一次 | 同上 | ✅ PASS（6.96s） | 采样 25，其余读数同形 |
| B 整包真机档（`seelebridge`，全包 `-v`） | `SEELEX_LIVE_SMOKE=1` | ⚠️ **2 FAIL / 其余绿** | `TestCustomRoleLiveProbe` ✅0.19s、`TestSystemPositionLiveProbe` ✅0.89s、`TestLiveSubagentWorktreeChain` ✅6.96s；`TestForkSubagentsLiveSmoke` ❌65.76s、`TestNodeFirstPersonLiveSmoke` ❌30.19s；4 条 docker 档 SKIP |
| C teammate 装配档（`gui` 真进程） | `SMOKE_TEAM_LIVE=1` | ❌ FAIL（1.82s） | `链路出现不符合设计稿的偏差: [本团队（review-team）暂无可执行者：reviewer、auditor …]` |
| D 其余真进程档 | `SMOKE_TEAM_WORK_LIVE` / `SMOKE_SUBAGENT_LIVE` / `SMOKE_FORK_LIVE` / `SMOKE_TEAM_WORK_COMPUTER_LIVE` | 未跑 | 理由见 §5.4 |

### 5.2 A：子代理 worktree 全链（本轮要验的那条）—— PASS

```
$env:SEELEX_LIVE_SMOKE='1'; go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m
```

读数（原文，`_logs/live_r3_subagent_chain.txt`）：

- `[config] parsed: agent=1 subagent=1 goalplan=1`
- `=== plan_run 耗时 13.8822614s ===`；`{"status":"completed","node_count":1,...}`，模型自报
  `已创建 live-acceptance.txt（内容一行 SEELEX-LIVE-298717）并以 commit e4e17bb 提交，工作区干净`
- `现场在册：true  路径=C:\Users\redre\AppData\Local\Temp\TestLiveSubagentWorktreeChain4109428525\001-seelex-impl  采样=55`
- `产出落在现场：true`
- `合并审批：[merge-impl] … live-acceptance.txt | 1 + / 1 file changed, 1 insertion(+)`
- `主工作区 git log：e4e17bb live acceptance SEELEX-LIVE-298717 / ac7863a base`
- `--- PASS: TestLiveSubagentWorktreeChain (14.55s)`；`ok github.com/Red-Huang-0622/seelex/seelebridge 14.763s`

即：派发 → 真发现场（主工作区之外）→ 现场上真干活 → 过合并审批 → rebase/merge 回主工作区 → 成功路径析构，
在 `809e6a3` 上**仍然一次运行内全部可达**（第一轮是 `bdbfeba`）。全包真机门下又跑了一次（`6.96s` PASS，采样 25），
两跑同形。

### 5.3 C：teammate 装配档 —— FAIL，根因是判据/文案陈旧（**不是链路回归**）

```
$env:SMOKE_TEAM_LIVE='1'; go test ./gui -run TestRealAPIAgentTeamLiveProbe -v -count=1 -timeout 20m
```

读数（原文，`_logs/live_r3_team_probe.txt`；报告 `tmp/headless-smoke/reports/team-live-20261006-225930.json`）：

```
=== RUN   TestRealAPIAgentTeamLiveProbe
    team_live_probe_test.go:245: 链路出现不符合设计稿的偏差: [本团队（review-team）暂无可执行者：reviewer、auditor 目前只有注册配置与角色会话，装配后不会自动产生回合（需要宿主为它接执行者）]
--- FAIL: TestRealAPIAgentTeamLiveProbe (1.82s)
```

**Confirmed 事实链**（都在当前 HEAD 上可核对）：

1. 红的是**探针自己的判据**：`gui/team_live_probe_test.go:159-160` 把 `team.set_order` 回执里的
   `DesignNotice` 一律收进 `notice`，末尾 `:243` 只要 `len(notice) != 0` 就判"链路偏差"。这一段
   `set_order` 在 **review-team** 上跑（前一步 `:120` materialize 的是 `teamLiveReviewSpec()`）。
2. 那条 notice 是**设计事实**，不是偏差：`application/core/agentteam/factory.go:445` 由
   `unexecutedRoles(orderRoles)` 与静态事实表 `RolesWithExecutor`（同文件底部：`user`/`main`/`tl`）生成；
   `application/core/agentteam/team_view_test.go:138/151/166` **要求** review/research 团队必须声明它、
   goal-a2a 必须没有它；README.md:412 与 `application/core/agentteam/README.md:141`、devlog
   `docs/devlog/2026-09-14-teamwork-wiring-fixes.md` §5（"修复 ④"）都把它记成既定口径。
3. 该档最后一次留下 PASS 记录是 **2026-09-10**（`docs/test/REPORT-a2a-agentteam-subagent-recovery-2026-09-10.md`，
   `design_notice=[]`），而这条 notice 生于 **2026-09-14**、探针在 **2026-10-01**（`7b0db19` 删内置形态目录）
   被改写——中间**没有**这条档的 PASS 记录。⇒ 这条红是"判据没跟上设计"，不是本轮或近期产品回归。

**但顺带牵出一个真的产品问题（本轮只登记，未动）**：notice 的后半句"（需要宿主为它接执行者）"**已经陈旧**——
今天宿主对**任何被派发的角色**都会跑真回合：`team_dispatch` → 作业 → `seelebridge/runtime_teamwork.go:646
RunWorker` → `r.runRoleRound(ctx, spec)`（`request.Role` 不限 tl/exec）。所以"装配后不会自动产生回合"仍真，
"宿主没有它的执行者"已不真——真实口径应是"装配≠干活；leader 派活才会跑"。事实表 `RolesWithExecutor`
仍是静态三人表，UI/面板会照它说"暂无可执行者"，而用户此时恰恰是打算派活的。

**两条路（待裁决）**：

- **路 A（只改判据）**：探针改成"goal-a2a 不得有 notice；review-team 的 notice 必须**就是**那条设计声明（逐字断言），
  其余 notice 才算偏差"。产品不动。→ 冒烟立刻转绿，但面板仍会对可派活的角色说"无执行者"。
- **路 B（判据 + 文案一起修）**：把 `RolesWithExecutor` 改成**宿主提供的动态事实**（或把这条 notice 的措辞改成
  "装配后不会自动产生回合；leader 派活（`team_dispatch`）才会跑"），探针按新口径断言。→ 面板不再说错话，
  代价是 `agentteam` 这一格加一个 Port 与一组回执断言（`team_view_test.go` 要同步）。

### 5.4 B：整包真机档暴露的两条红（越出本工作流，登记待查）

本轮把 `seelebridge` 包**整包**放进真机门（`SEELEX_LIVE_SMOKE=1 go test ./seelebridge -count=1 -v -timeout 40m`），
于是两条**从未在真机门下跑过**的档（wave1–wave3 文档里一直记作"未覆盖缺口"）被跑起来了，两条都红：

**B1 `TestForkSubagentsLiveSmoke`（FAIL 65.76s）** ——

```
fork_live_smoke_test.go:73: === 真实 API fork 冒烟（耗时 1m5.7554605s）===
fork_live_smoke_test.go:76: 取回的产出必须覆盖 live_file: ## 结论（直读答案）
```

判据是 `seelebridge/fork_live_smoke_test.go:74-77`：逐个要求 `live_time` / `live_file` 这两个**子代理 id**
出现在 `job_manage(op=fetch)` 取回的正文里。本次取回的是子代理的**最终答复本身**（首行即模型自己的
`## 结论（直读答案）`）；`live_time` 侥幸通过是因为模型在答复里提到了自己那条分支
（`…当前分支 seelex/live_time，由框架处理合并…`），`live_file` 那篇 README 总结里则一个字都没提自己的 id。
⇒ **判据依赖模型措辞**（脆弱判据），不是"fork 没跑通"：两条子代理都真跑了、真产出了正文。

**B2 `TestNodeFirstPersonLiveSmoke`（FAIL 30.19s）** ——

```
node_first_person_live_smoke_test.go:273: history replay tools = 0, want >= 5
```

即认证 E（历史回放）红：fork 跑完后重新 `SubscribeSubagentLive(nodeID)`，**阶段**事件回放得到（`len(history) >= len(liveStages)` 那条断言过了），
**工具**事件回放为 0，而实时流里收到过 5 条以上 `status=success` 的工具事件（`[即时] … 工具 bash_read status=success …`）。
代码路径上两者是**同一条漏斗**（`runtime_live.go`：工具事件回调 `liveCh <- toolLiveEvent(event)` → 广播循环
`broadcastLive` → 写 `liveHistory[nodeID]`），按此不应出现"只回放阶段"。**根因已于 23:11 补测定位
（Confirmed，见 §5.8）**：不是工具事件没进回放，而是回放缓冲是**有上限的滚动尾部窗口**，被上千条正文增量滚过去了。

### 5.5 本轮改了什么

**只改了本文档**：没有动任何产品代码、没有动用例。A 绿、B/C 的红都停在"读数 + 根因/待查"上，交裁决。

### 5.6 复跑命令（第二轮，原样）

```powershell
go build -o tmp/bin/seelex-headless.exe .
$env:SEELEX_LIVE_SMOKE='1'
go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m   # A：要验的那条链
go test ./seelebridge -count=1 -v -timeout 40m                                      # B：整包真机档（会顺带跑 fork / 第一视角）
$env:SMOKE_TEAM_LIVE='1'
go test ./gui -run TestRealAPIAgentTeamLiveProbe -v -count=1 -timeout 20m           # C：teammate 装配档
```

### 5.7 两个必须知道的现场事实

1. **`seelebridge` 的 `fork` / `node_first_person` 两条真机档把"\<本地仓库根\>"绑成项目根**
   （`filepath.Abs(filepath.Join(".."))`），运行期会在**真实仓库**里建现场与分支（模型自己在正文里提到
   `当前分支 seelex/live_time`）。本轮跑后核查：`git status --porcelain` 仅本文件、`git branch -a` 无
   `seelex/live_*`、`.git/worktrees` 目录不存在（现场按"0 提交"路径清掉了），**未留残留**。
   复跑前请知悉；要完全隔离就先把仓库复制到临时目录再跑。
2. **`gui/*_live_probe_test.go` 一族**（`SMOKE_SUBAGENT_LIVE` / `SMOKE_FORK_LIVE` / `SMOKE_TEAM_WORK_LIVE` /
   `SMOKE_TEAM_WORK_COMPUTER_LIVE`）用 `forkLiveSpawn` 起真进程，`cmd.Dir = repoRoot` —— 同样是本地仓库根；
   本轮因此**未跑**（§5.1 的 D）。其中 `SMOKE_TEAM_WORK_LIVE` 那一档的前提已在第一轮判定退场（§2），
   `SMOKE_TEAM_WORK_COMPUTER_LIVE` 会把**当前屏幕画面**发给 provider（本机桌面纪律：不主动做这个）。


### 5.8 B2 根因定位（补测，2026-10-06 23:11，基线仍是 `809e6a3`）

补测方式：临时加一条**一次性**诊断档（`seelebridge/zz_tmp_first_person_probe_test.go`，跑完即删；跑后 `git status`
已核干净），在真机门下 fork 一个与 B2 同形的节点，打印**实时面**与**回放面**的 `Kind` 直方图（一次真实 API 运行，19.6s）：

```
LIVE   kinds=map[assistant:1013 stage:11 tool:10]
REPLAY len=512 kinds=map[assistant:511 stage:1]
REPLAY cap=512；最后一个 tool 事件的下标=-1（其后事件数=512）
```

**Confirmed 根因**：`liveHistory` 是**每节点上限 512 条、超出淘汰最旧的滚动尾部窗口**
（`seelebridge/runtime_live.go:18-20` `subagentLiveHistoryCap = 512`、`:120-124` 的
`history = history[len(history)-subagentLiveHistoryCap:]`）。真机一轮流式会产出**上千条 `assistant` 正文增量**
（本次 1013 条，来源 `seelebridge/node/agent_node.go` 的 `sess.ChatStream` onChunk → `RecordNodeAssistant`），
于是窗口滚过早期事件后，回放里只剩"最后的 511 条正文增量 + 1 条 stage(result)"，**11 条 stage 与 10 条 tool
全部被挤出窗口** → `history replay tools = 0`。

- **实时面没有坏**：同一次运行 live 收到 `stage:11 tool:10`（其中 `status=success` 的工具事件 5 条），
  B2 的认证 A（即时输出）本来就是过的；工具事件确实经同一条漏斗进了 `liveHistory`，只是**被上限淘汰**。
- 认证 E 的前一条断言 `len(history) < len(liveStages)` **只是被凑巧满足**（512 ≥ 14），它证明不了"回放覆盖了阶段流"。
- ⇒ 不是产品链回归，也不存在"工具没进回放"的漏斗分叉：**用例写下的期望（"从 subagent start 到最新的完整事件流
  （阶段 + 工具）"）比实现提供的保证（有界滚动窗口）更强**。
- 收口两条路（待裁决）：**①** 改用例口径——把回放断言限定为"窗口内的最近 N 条"，或把 cap 拉到覆盖整轮；
  **②** 改产品——把 stage/tool 事件与正文增量**分桶**，让正文增量不参与淘汰 stage/tool 的那个窗口
  （面板"第一视角"要看的是"走了哪几步"，不是每一个 token）。

## 6. 第三轮（2026-10-07 凌晨，主干 `fd3ff13`）：上面几条红的收口读数

> 本轮在主工作区执行；§2/§5 记的三条红逐条落到下面三笔提交上（现场先按 `note` 手工并入主干）。

| 事 | 落地 | 读数 |
|---|---|---|
| ③A fork 取回判据 | `05ce6b9`（判据改产物哈希对照 + 确定性对照用例）、`9bb67c4`（裁断"基准面被产品侧上限截断"的成因 + 钉住截断分支的用例） | `TestForkProductHashJudgmentDeterministic` PASS（`017b5a856820` / `b6a24597d291`，交叉配对必不同）；真机 `TestForkSubagentsLiveSmoke` **PASS 29.56s**（1478 / 1806 字节）。第一次红的原因不是 fork 链路：基准面自己就被 `seelebridge/node/agent_node.go` 的 `nodeOutputMax`(2000 字节) 截断（基准 len=2003 / 取回 len=2075、两侧 head 同源）——判据已收敛到 `forkProductMatches`：未截断走逐字节哈希，截断时只钉"取回侧同长度前缀"**并明说降级** |
| ③B 第一视角分页 | `a1e13a4`（有界窗口 + 分页：subagent 按节点、teammate 按会话） | 真机 `TestNodeFirstPersonLiveSmoke` **PASS 35.76s**（`SubagentLiveWindow: 4096` + 逐页读，逐页合计 = total） |
| ③C teammate 装配档 | `fd3ff13`（判据分桶 + notice 文案改真实口径） | 真机 `TestRealAPIAgentTeamLiveProbe`：**1.82s ❌ → PASS 1.24s**；报告 `design_notice_declared=[本团队（review-team）暂无可执行者：reviewer、auditor …（leader 用 team_dispatch 派活才会跑真回合）]`、`design_notice=[]`（无偏差）、`real_turn={assistant_rows:1,user_rows:1}`、`race_clean=true`、`healthz=200` |

全仓读数（主干 `9bb67c4` / `fd3ff13` 之上）：`go vet ./...` = 0；`go test ./... -count=1` = 0；
`node --test gui/frontend/dist/*.test.mjs` = `tests 652 / pass 652 / fail 0`。

仍开放（不在本轮范围，登记备查）：
- **恢复链的收尾判据**：老现场登记缺 `MainBranch` 栏 ⇒ 自动合并一律失败、要 leader 手工合。现场保留、不猜，行为是对的，但"恢复后的收尾必然要人推一把"值得单独收口。
- **路 B-heavy**：`RolesWithExecutor` 由静态事实表换成宿主提供的动态事实（新 Port + 回执断言）；本轮只在文案与注释上消除那句假话，事实表形状未动。
- 第一条团队线遗留的四个 interrupted 作业（工作表上仍可见）。

## 7. 第四轮（2026-10-07 凌晨，主干 `701d363`）：C 链路的**语义**修完（不是把判据放松）

§6 的 ③C 当时只做了一半：判据分桶 + 文案改成"装配本身不会产生回合"，但**符号与 wire 仍在说"没有执行者"**，
而"暂无可执行者"这个说法本身就是假话——用户在真机上恰恰是**打算派活**。

这一轮把一件事拆成两件（这才是"修语义"）：

| | 是什么 | 谁 |
|---|---|---|
| **自动回合** | 没人派活时宿主自己产生的回合 | `user`（输入）/ `main`（主会话 ChatStream）/ `tl`（goal 治理 ADVISOR 回合） |
| **派活回合** | leader 用 `team_dispatch` 派活 → 作业 → `Runtime.RunWorker` → `runRoleRound` | **任何在编角色**（角色不限 tl/exec） |

改名与改写（23 文件，+127/−112，提交 `701d363`）：

- 符号：`RolesWithExecutor` → `AutomaticTurnRoles`；`unexecutedRoles`/`UnexecutedRoles` →
  `rolesWithoutAutomaticTurn`/`RolesWithoutAutomaticTurn`；`StopNoExecutor` → `StopNoAutomaticTurn`；
- wire：`TeamSchedule.Unexecuted` → `NoAutomaticTurn`（`json:"no_automatic_turn"`）、停止原因词
  `no_executor` → `no_automatic_turn`（dto/TUI/前端标签）、前端徽标"无执行者" → "无自动回合"；
- 文案：入职回执与成员表声明 = 「…没有自动回合（装配本身不产生回合）；要 leader 派活
  （team_dispatch）才会跑真回合」。

判据（`gui/team_live_probe_test.go`）**收紧**：① 声明必须恰一条且点明 reviewer/auditor；② 声明里不许出现
「暂无可执行者 / 没有执行者 / 无执行者 / 只读成员」（`noExecutorClaimIn`）。

读数：

- 真机 `TestRealAPIAgentTeamLiveProbe` **PASS 1.77s**（`tmp/headless-smoke/reports/team-live-20261007-003430.json`）：
  `design_notice_declared=["本团队（review-team）：reviewer、auditor 没有自动回合（只有注册配置与角色会话，装配本身不产生回合）；要 leader 派活（team_dispatch）才会跑真回合"]`、
  `design_notice=[]`、`real_turn={assistant_rows:1,user_rows:1}`、`race_clean=true`、`healthz=200`。
- `go build ./...` = 0、`go vet ./...` = 0、`go test ./... -count=1` = **73 ok / 0 FAIL / exit 0**、
  `node --test gui/frontend/dist/*.test.mjs` = tests 652 / pass 652 / fail 0。
- 一处**不稳定读数**（登记）：同树首次全量跑（作业 a24）尾部报了 `FAIL` 但失败包名未取回；同树复跑
  （作业 a25）73 ok / 0 FAIL。不是语义回归，但成因未见，若再现需追。

仍开放：

- `dto.RoleInstantiation.Executor` 仍是"自动回合的推手"（`user`/`main`/`tl` = 角色名、timer = `scheduler`、
  其余为空 + 声明）；"谁跑它"这件事今天对所有角色都有答案（leader 派活），字段形状未动。
- 状态机总表 `docs/arch/state-machine-inventory.md` 未登记这一格（停止原因词是 agentteam 内的字符串常量表，
  不在 §1 统一面内）——要不要收编进枚举，待定。
