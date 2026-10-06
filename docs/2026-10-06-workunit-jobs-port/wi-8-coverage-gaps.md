# wi-8：审计 §6 两条空白用例的填补（AdjustItem 正向 / teammate 每 Session 进度详情）

- 角色：`resume-note-unify`（work item `wi-8`，里程碑 `m2-audit-open`）
- 基线：main 头 `0adef97`（本 worktree 干净起步）
- 上承：`docs/2026-10-06-workunit-jobs-port/step-3-audit.md` §6 九条登记表的 **#7 / #8**
- 环境：Windows + PowerShell；`bash_read` 拒收含 `|` / `&&` / `>` 的命令，一律改写 `git grep` / `Select-String`
- 纪律：**未改任何生产代码**（两次 mutation 探针均已还原，`git diff` 空）；未改既有断言；未读 `config/accounts.yaml` / `*.local.yaml`

---

## 0. 交付一句话

- **#8（AdjustItem 正向路径）：填上了。** 新增 3 条用例，覆盖五项字段全改 + `team_items` 读回一致 + 新依赖生效（显式拒收 → 依赖 done 后放行）+ 负向边界不回归（running / review / done）。
- **#7（teammate 每 Session 进度详情）：填了一半（读路存在、已补测），另一半仍开放。**

---

## 1. #8 AdjustItem 正向路径（新增文件 `seelebridge/teamwork/items_adjust_test.go`）

既有只有负向：`seelebridge/teamwork/items_test.go:593 TestAdjustItemRefusesStartedAndFinishedWork`；
实现入口 `seelebridge/teamwork/items.go:250 AdjustItem`。**本文件逐字未动 `items_test.go`。**

| 用例 | 覆盖 |
|---|---|
| `TestAdjustItemRewritesPendingWorkAndReadsBack` | pending 的五项一次改齐（role/name/description/goal/depends_on）；`Coordinator.Items()`（= `team_items` 插口，`seelebridge/runtime_teamwork.go:545`）读回逐字段一致；没提到的格（id/milestone/status/session/worktree/handle）一个不动；只改一项时其余留空=不动；改成不在编角色 → 拒（"不在编"）；改不存在的工作项 → 报错（"没有工作项"） |
| `TestAdjustItemDependencyIsAHardGate` | 把 `wi-test` 的 `depends_on` 从 `[wi-impl]` 改成另一件**未完成**的 `[wi-req]` → 派发被**显式拒收**（正文含"还没完成"+`wi-req`）；被拒后不留会话/现场/时间戳；依赖 dispatch→review→accept(done) 后才放行，放行后 `status=running` 且落上自己的 `SessionID`/`Worktree` |
| `TestAdjustItemStillRefusesRunningReviewAndDone` | 负向不回归：running / **review**（既有那条只覆盖 running+done）/ done 一律"既定的" |

**红→绿**：这条读路本来就在，用例**到即绿**。为证明用例不是空转，做了一次**临时 mutation 探针**（把 `AdjustItem` 里 `Goal`/`DependsOn` 两处赋值短路），红原文见 `_logs/wi8_red_mutation.txt`：

```
--- FAIL: TestAdjustItemRewritesPendingWorkAndReadsBack
    items_adjust_test.go:62: 改目标没生效：goal = ""
--- FAIL: TestAdjustItemDependencyIsAHardGate
    items_adjust_test.go:118: 拒收正文必须说清是哪件依赖没完成，得到 teamwork: 工作项 "wi-test" 依赖的 "wi-impl" 还没完成（状态 pending）…（仍读旧依赖）
```

探针随后**还原**（`git diff` 空）。

## 2. #7 teammate 每 Session 进度详情（新增文件 `seelebridge/runtime_teamwork_work_item_progress_test.go`）

### 2.1 勘定：teammate 侧有**两条**读面，各带一半

| 读面 | 入口 | 实际读数（原始字段面） |
|---|---|---|
| (a) 会话详情面 | `Service.TeammateSessionLiveFor(sessionID)`（`application/core/teamwork_service.go:38` → `seelebridge/runtime_teammate_session_live.go:35`） | `dto.TeammateSessionLiveView`（`application/contract/dto/teammate_session_live.go:21`）字段面 = `session_id / role / live / running / messages / truncated`（键表由 `e2e/teammate_session_live_wire_test.go:44` 钉住）⇒ 带 **session id + 对话正文 + 在不在跑**；**不带 worktree、不带状态词/阶段** |
| (b) 看板投影面 | `Service.TeamworkBoardViewFor(sessionID)`（`application/core/teamwork_service.go:20` → `seelebridge/runtime_teamwork_board.go:102`） | `dto.TeamworkBoardView.WorkItems[]`（`dto.TeamworkWorkItemView`，`application/contract/dto/teamwork_board.go:167`；搬运点 `seelebridge/runtime_teamwork_board.go:354 teamworkWorkItemViews`）字段面 = `id/milestone/role/name/description/goal/depends_on/status/session_id/worktree/handle/note/started_at/finished_at/live/interrupted` ⇒ **session id + worktree + 状态词 + 阶段（所属里程碑）齐全** |

结论：题面要的那组「session id + worktree + 状态词/阶段」**只在 (b) 读得到**——即**读路存在，缺的是用例**。
（子代理侧一份 `SubagentDetail` 就齐 `Status`+`Worktree`+`Context`，见 `application/core/subagent_detail_test.go:27/:59`；teammate 侧没有这样的单载荷读面。）

### 2.2 补测（钉 (b)）

`TestTeamworkBoardReadsWorkItemProgressDetail`：造一份"`wi-impl` 正在跑"的真计划（`SessionID=s-team-exec-wi-wi-impl`、`Worktree=seelex/exec-wi-impl`、绑定账本一行未释放），取 `TeamworkBoardSnapshot("s-team")`，**按这件事自己的会话号定位**它，断言四件一起成立：`SessionID` / `Worktree` / `Status`（=`running`）/ `Milestone`（=`m-impl`），且阶段名 `实现` 能在同一份投影里解析、执行者 `exec` 在、`Live=true`（现场在册）、`Interrupted=true`（状态说在跑但本进程查无句柄）；反向钉一条：**角色会话号**（`s-team-exec`）定位不到这"件工作"。

**红→绿**：同 #8，读路存在 ⇒ 到即绿。mutation 探针（把 `teamworkWorkItemViews` 的 `Worktree: item.Worktree` 短路）红原文见 `_logs/wi8_red_board_worktree.txt`：

```
--- FAIL: TestTeamworkBoardReadsWorkItemProgressDetail
    runtime_teamwork_work_item_progress_test.go:103: 进度详情必须带现场（worktree）：""
```

探针已还原。

### 2.3 仍开放的那一半（**不改产品**，如实标）

「**一条**读面同时给出 会话正文 + worktree + 阶段」——即让 teammate 的进度详情达到子代理详情那种**单载荷**口径——**需要动产品**（给 `TeammateSessionLiveView` 加 `worktree`/状态词，或给看板行加 `stage`/`preview`），本轮**停手**。锚点与缘由：`docs/arch/workunit-progress-read-surface.md` §1.5（"看板行没有进度字段……'跑到哪'看不到"）、§3.1（前端"实时进度"是两个不同的 hook）。**需要什么**：① 定 `TeammateSessionLiveView` 是否收编 worktree/status（涉及 wire 契约 → 需同步 `e2e/teammate_session_live_wire_test.go` 与前端 `renderTeammateLiveSession`）；或 ② 给 `TeamworkWorkItemView` 加 `stage`/`preview`（`docs/arch/workunit-progress-read-surface.md` §3.2 列的可选面）。

---

## 3. 命令读数（原文）

```powershell
gofmt -l seelebridge/teamwork/items_adjust_test.go seelebridge/runtime_teamwork_work_item_progress_test.go
#   → 空
go vet ./seelebridge/ ./seelebridge/teamwork/
#   → 空
go build ./...
#   → BUILD_EXIT=0
go test ./seelebridge/teamwork/ -count=1
#   → ok  github.com/RedHuang-0622/seelex/seelebridge/teamwork  1.184s   TEAMWORK_EXIT=0
go test ./seelebridge/ -count=1          # 第 2 半择定的包
#   → ok  github.com/RedHuang-0622/seelex/seelebridge  28.179s  （FAIL 行 0 条）
go test ./seelebridge/teamwork/ -count=1 -run Adjust -v
#   → 4 条 PASS（含既有 TestAdjustItemRefusesStartedAndFinishedWork）
go test ./seelebridge/ -count=1 -run WorkItemProgress -v
#   → --- PASS: TestTeamworkBoardReadsWorkItemProgressDetail (0.03s)
```

日志留档：`_logs/wi8_red_mutation.txt`、`_logs/wi8_red_board_worktree.txt`、`_logs/wi8_seelebridge_test.txt`。

## 4. 结论

- **#8：是**（填上了）——依据：3 条新用例 + mutation 红原文证明断言非空转。
- **#7：一半**——「能从读面/投影读出 session id + worktree + 状态词/阶段」= **是**（看板投影面，已补测 + mutation 红原文）；「一条读面（会话详情）同时带 worktree/阶段」= **仍开放**（需动产品，本轮停手）。

## 5. 未做 / 边界

1. 未改任何生产代码；两次 mutation 探针均已还原（`git status --porcelain` 只列两个新 `?? .go` 文件）。
2. 未更新 `step-3-audit.md` §6 的 #7/#8 状态（那是 `wi-4` audit 线的产物）；本条交付即可供其刷新。
3. 未跑真机档（`gui/team_workcontent_live_probe_test.go` 等需 `SMOKE_TEAM_WORK_LIVE=1` + 额度）；本项目标就是**非真机档**读数。
4. 未跑全量 `go test ./...`：本轮只动两个测试文件，影响面 = `seelebridge`（已跑）与 `seelebridge/teamwork`（已跑）。
