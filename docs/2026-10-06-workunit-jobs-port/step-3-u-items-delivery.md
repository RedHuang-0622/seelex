# 步骤③ 未决项 U6 / U5 / U2 落地记录（第二批）

- 上游：`docs/2026-10-06-workunit-jobs-port/step-3-goal.md` §7 H（U2 / U5 / U6 逐条），清单在
  `docs/arch/workunit-duplication-inventory.md` §四。
- 基线：`240b5ba`（前一批交付报告的提交）。本轮**串行**做，一步一提交，顺序 U6 → U5 → U2。
- 口径没变：一份判据 = 一处函数；只并本质重复（"为什么像却不并"要写得出）；证据是"只剩一份实现 /
  门禁或编译器钉住"，不是"用例全绿"；**两张作业表一动没动**。

## 1. 改动清单

| 文件 | 改动 | 对应 |
|---|---|---|
| `e2e/subagent_status_vocabulary_gate_test.go`（新，11.3KB） | 记录状态词表门禁：四种形态扫描（状态比较/赋值/字段/switch 分支）+ 阴性对照 + "白名单过期也红" | U6 |
| `seelebridge/workunit_team.go` | `teamUnitStatusDone/Failed` 由字面量改为 `string(dto.SubAgentDone/Failed)`；补 dto import | U6 |
| `seelebridge/runtime_subagent_resume.go` | `subagentNodeStatusDone/Failed` 同上（本层不再自写字面量） | U6 |
| `seelebridge/node/coordinator.go` | 写会话记录终态的两处字面量 → `string(dto.SubAgentDone/Failed)` | U6 |
| `seelebridge/session/subagent_sessions.go` | 兜底终态 `status: "done"` → 同包别名 `SubAgentDone` | U6 |
| `seelebridge/session/subagent_tree.go` | 恢复判终态的两处字面量 → 同包别名 `SubAgentDone/Failed` | U6 |
| `seelebridge/stage_preview_judgment_test.go` | **既有互锁用例扩表**（teammate 侧两常量 + `session` 包四个再导出）；未删、未放宽任何既有断言 | U6 |
| `seelebridge/tools/router.go` | `newExecProcessTree` 建树工厂（用例注入点）+ `noteProcessTreeDegraded`（同步链唯一退化读数）+ 两处起命令点各调一次 + `bashProcessDegradedStage` 常量 | U5 |
| `seelebridge/tools/process_tree_degraded_reading_test.go`（新） | 退化必报 + 健康必静默 | U5 |
| `seelebridge/tools/process_tree_degraded_bench_test.go`（新） | 那条判据的量级读数 | U5 |
| `seelebridge/worktree/worktree_manager.go` | `Restore`：已在册的 nodeID 不再被记录覆盖（与 `Adopt`/`beginNamed` 同一条判据） | U2 |
| `seelebridge/worktree/worktree_registration_sources_test.go`（新，7.7KB） | 两来源两种顺序 + 活登记不被降级 + 重启仍能重建 + 防幽灵仍在 | U2 |
| `seelebridge/worktree/worktree_registration_bench_test.go`（新） | `Restore` 两条路的量级读数 | U2 |
| `docs/arch/workunit-duplication-inventory.md` | §一 U2 表尾 + §四 U2/U5/U6 三条结论、锚点、第二批清单 | 文档 |

`git diff --stat`（不含新增文件）：9 files changed, 90 insertions(+), 18 deletions(-)。

## 2. 删除清单（旧 `文件:行` → 去向 → 新唯一位置）

| 旧位置 | 旧内容 | 去向 | 新唯一位置 |
|---|---|---|---|
| `node/coordinator.go:132` | `status := "done"` | 删除字面量（转调） | `application/contract/dto/subagent.go:11` `SubAgentDone` |
| `node/coordinator.go:135` | `status = "failed"` | 同上下 | 同上下 `:12` `SubAgentFailed` |
| `session/subagent_sessions.go:208` | `subagentOutcome{status: "done"}` | 删除字面量（同包别名） | `session/subagent_tree.go:46`（源头 `dto:11`） |
| `session/subagent_tree.go:422` | `case status == "done"` | 同上下 | `session/subagent_tree.go:46` |
| `session/subagent_tree.go:424` | `case status == "failed"` | 同上下 | `session/subagent_tree.go:47` |
| `workunit_team.go:36` | `teamUnitStatusDone = "done"` | 删除字面量（转调契约） | `dto.SubAgentDone` |
| `workunit_team.go:37` | `teamUnitStatusFailed = "failed"` | 同上下 | `dto.SubAgentFailed` |
| `runtime_subagent_resume.go:353` | `subagentNodeStatusDone = "done"` | 同上下 | `dto.SubAgentDone` |
| `runtime_subagent_resume.go:354` | `subagentNodeStatusFailed = "failed"` | 同上下 | `dto.SubAgentFailed` |
| `worktree_manager.go:624`（Restore 循环内） | 无条件 `w.worktrees[record.NodeID] = …` | 删除**覆盖**语义（改首判） | 同函数开头新增 `if w.worktrees[record.NodeID] != nil { continue }`（与 `:287` / `:354` 同判据） |

U5 是**新增读数**（旧位置 = 无），不是删除：同步链过去一个读数都没有。

## 3. 红 → 绿原文（真跑出来的）

### 3.1 U6 门禁先红（5 处命中）

```
--- FAIL: TestSubagentStatusVocabularyGate (0.01s)
    subagent_status_vocabulary_gate_test.go:126: seelebridge/node/coordinator.go:132 [状态赋值] 状态词字面量 "done"：status := "done"
    subagent_status_vocabulary_gate_test.go:126: seelebridge/node/coordinator.go:135 [状态赋值] 状态词字面量 "failed"：status = "failed"
    subagent_status_vocabulary_gate_test.go:126: seelebridge/session/subagent_sessions.go:208 [状态字段] 状态词字面量 "done"：s.outcomes[cmd.nodeID] = subagentOutcome{status: "done"}
    subagent_status_vocabulary_gate_test.go:126: seelebridge/session/subagent_tree.go:422 [状态比较] 状态词字面量 "done"：case status == "done":
    subagent_status_vocabulary_gate_test.go:126: seelebridge/session/subagent_tree.go:424 [状态比较] 状态词字面量 "failed"：case status == "failed":
FAIL	github.com/RedHuang-0622/seelex/e2e	0.178s
```

改完（同步跑）：

```
ok  	github.com/RedHuang-0622/seelex/e2e	0.217s
```

### 3.2 U5 先红（同步链一个退化读数都没有）

```
--- FAIL: TestSyncChainReportsDegradedProcessTree (0.25s)
    process_tree_degraded_reading_test.go:76: 同步链必须报出「进程树挂不上」这条读数，实际阶段 = [bash.resolve.start bash.resolve.done bash.command.prepared bash.process.starting bash.process.started bash.process.exited bash.handler.return]
FAIL	github.com/RedHuang-0622/seelex/seelebridge/tools	1.444s
```

改完（含既有阶段序列用例与装配用例）：

```
--- PASS: TestProcessTreeAssemblyIsSingleAcrossChains (0.00s)
--- PASS: TestSyncChainReportsDegradedProcessTree (0.25s)
--- PASS: TestSyncChainStaysSilentWhenTreeIsHealthy (0.24s)
--- PASS: TestScopedBashPublishesDiagnosticStages (0.30s)
ok  	github.com/RedHuang-0622/seelex/seelebridge/tools	1.789s
```

### 3.3 U2 先红（记录里缺的栏位把活登记降级了）

```
=== RUN   TestSceneRegistrationSourcesAgreeInBothOrders
    --- PASS: TestSceneRegistrationSourcesAgreeInBothOrders/先认领后恢复 (0.82s)
    --- PASS: TestSceneRegistrationSourcesAgreeInBothOrders/先恢复后认领 (0.70s)
=== RUN   TestRestoreDoesNotDowngradeLiveRegistration
    worktree_registration_sources_test.go:120: 记录里缺的栏位不得覆盖在册事实：MainBranch got "" want "main"（收尾会静默改成「合进当前 HEAD」）
--- FAIL: TestRestoreDoesNotDowngradeLiveRegistration (0.59s)
=== RUN   TestRestoreStillRebuildsRegistrationAfterRestart
--- PASS: TestRestoreStillRebuildsRegistrationAfterRestart (0.59s)
FAIL	github.com/RedHuang-0622/seelex/seelebridge/worktree	3.468s
```

改完（整个 worktree 包）：

```
ok  	github.com/RedHuang-0622/seelex/seelebridge/worktree	22.814s
```

## 4. 命令原始读数

| 命令 | 读数 |
|---|---|
| `gofmt -l`（改动文件 + 新增文件） | 无输出 |
| `go build ./...` | 无输出（exit 0） |
| `go vet ./seelebridge/...` | 无输出（exit 0） |
| `go test ./e2e/ -count=1` | `ok github.com/RedHuang-0622/seelex/e2e 0.402s` |
| `go test ./seelebridge/... -count=1` | 30 包全 `ok`（`seelebridge` 40.051s、`node` 1.784s、`session` 0.525s、`tools` 17.447s、`worktree` 29.363s、`workunit` 1.304s……） |

## 5. 「只剩一份实现」的读数（门禁 / 编译器钉住）

- **U6**：门禁 `TestSubagentStatusVocabularyGate` 扫**记录状态那一格的写方与读方**八个文件
  （`recordStatusChainFiles` 逐文件写了角色），四种形态零命中；白名单 `allowedRecordStatusLiterals`
  目前**为空**且"过期条目也红"。跨包互锁在 `TestSubagentStatusVocabularyAgreesWithTheWire`
  （`dto` ↔ `workunit` ↔ teammate 常量 ↔ `session` 再导出，任一改词立刻红）。
- **U5**：同步链的退化判据只有一处——`Router.noteProcessTreeDegraded`（`tree.Degraded()`），
  两处起命令点（`executeScopedBash` 主体与 docker 重试）都调它；后台链读的是同一棵树上的同一
  事实（`async_probe.go` 的探针），两条链的**装配**仍是同一份 `newProcessTreeCommand`。
  不退化时零事件 ⇒ 既有"诊断阶段逐个相等"用例（`router_test.go:39`）原样通过。
- **U2**：`Restore` / `Adopt` / `beginNamed` 三条入口同一条判据（已在册即复用、不覆盖、不重建）；
  注册表写点仍是 `register`（`:403`）与 `Restore`（首判之后）两处，键都是 nodeID，现场名只有
  `sceneDirName` 一处拼法。

## 6. 两张作业表「一动没动」

```
git diff --stat -- seelebridge/tools/async_exec.go seelebridge/tools/job_contract.go \
  seelebridge/tools/job_run.go seelebridge/tools/job_tools.go seelebridge/tools/job_subagent.go \
  seelebridge/workunit/contract.go seelebridge/workunit_parent.go seelebridge/workunit_assembly.go
```

→ **空**。口径不变：不并表、不迁 Seele `jobs.Manager`、不反向合一（`teamwork-leader-worker-architecture.md` §12.4）。

## 7. 性能量级（改动后的读数）

```
BenchmarkSceneRegistrationRestore/已在册（首判跳过）-8      1781496    659.2 ns/op      0 B/op    0 allocs/op
BenchmarkSceneRegistrationRestore/空注册表（重建）-8           1736   651215 ns/op  15240 B/op  105 allocs/op
BenchmarkNoteProcessTreeDegradedHealthy-8                90838897     13.08 ns/op      0 B/op    0 allocs/op
BenchmarkNoteProcessTreeDegradedReported-8               68557324     18.56 ns/op      0 B/op    0 allocs/op
（同口径的既有判据）pathDirtyWith 233.6 ns / worktreePathEqual 460.4 ns / parseWorktreeList 1884 ns / cleanupWorktreeWith 440.2 µs
```

读法：

- **U2 的判据本身**：32 条记录全在册时一次 `Restore` 659 ns（≈20 ns/记录，0 分配）；空注册表重建
  651 µs，几乎全是每条记录的 `os.Stat`（真开销在文件系统，不在判据）。
- **U5 的判据本身**：健康路径 13 ns / 0 分配（每条同步命令多这一次），退化路径 18.6 ns。同步命令
  本身要起一个 shell 进程（毫秒档），这条判据是它的 **10⁻⁵ 量级**——补读数没有给 bash 加开销。
- 结论：本轮两处改动都落在 ns 档，没有改变任何热点的量级。

## 8. 三层测试证据里"没做"的那层（如实记）

- **全局冒烟（computer use）未做**：桌面纪律要求不抢焦点、不合成键鼠输入，本机前台是用户自己的
  Seelex 窗口，因此没有逐入口手点；替代证据是 e2e 全绿（0.402s）与 seelebridge 30 包全绿。
- **真机团队链路未跑**：本轮是单代理串行改动，团队作业面未使用（上批已 `team_close`）。

## 9. 未决项（本轮之后仍在册）

1. **U6 第二批（形态相同、属另一张词表）**——门禁不越界判它们，已在清单里逐条登记：
   ① 后台作业状态词（`tools/async_exec.go:32-34` 与 `application/core/async_completion.go:47-48`
   各一份，而 `dto` 只有 `AsyncStateRunning`）；② 计划批次结果状态词（写 `plan/tool_provider.go:442-452`、
   读 `application/core/plan_tools.go:645`）；③ 工具事件状态词（`session/tool_events.go:101/110`）；
   ④ 统一事件摘要状态词（`events_unified.go:89`）。
2. **U5 残①**：同步链这条退化读数目前只到**后端诊断口**（`application/console` 的 `LogBashEvent`）；
   要不要把退化事实带进**工具结果**（`scopedBashResult` 加栏位 / 产出尾部注记）是改对外形状的
   **独立决定**，要自己的红灯用例与契约口径。
3. **U5 残②**：`Degraded()` 判的是"Job 没建成（`job == 0`）"；`Attach` 失败（进程没挂进 Job）这一
   形态**不进**这条判据——要收的话是 `security` 侧的判据变更（`Attach` 失败后该不该标记退化）。
4. **U6 的"另一张记录形状"**：`node/agent_node.go:305/307` 给 `NodeSemanticResult.Status` 写的是
   `failed`/`completed`，而会话记录写 `done`/`failed`——同一次节点失败两个词。它不在本门禁的四形态里
   （`return` 形态），且消费方不同，本轮**只登记不并**：一并就要改 `merge_back_concurrency_test.go`
   钉住的既有事实。
5. 前一批留下的未决项不变：③U5（当时的"同步链无退化读数"已由本轮收敛）、②U2（teammate 时间字段恒
   零值）、②U6（统一实时事件粒度，需先写事件载荷字段表）、`worktree/README.md:97` 的文档债、
   CRLF 幻影脏本体。
