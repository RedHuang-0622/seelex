# 步骤③ 未决项第二批落地记录（状态词表三格 + U5 残②）

- 上游：`docs/2026-10-06-workunit-jobs-port/step-3-u-items-delivery.md` §9 登记的第 1、2、3 条
  （① 后台作业状态词 / ② 计划批次结果状态词 / U5 残② `Attach` 失败不进退化判据）。
- 基线：`6b04e49`（第一批交付记录的提交）。顺序：先门禁（红）→ 三格词表收口（绿）→ U5 残②（红→绿）。
- 口径不变：一份判据 = 一处函数；只并本质重复（"为什么像却不并"要写得出）；证据是"只剩一份实现 /
  门禁或编译器钉住"；**两张作业表一动没动**。

## 1. 改动清单

| 文件 | 改动 | 对应 |
|---|---|---|
| `e2e/subagent_status_vocabulary_gate_test.go` | 门禁从"一格里四形态"扩成"三格声明式 + 五形态"：每格声明写方/读方清单与取值面；新增形态 ⑤ **状态常量声明**（第二份定义本人的长相，前四形态都抓不到）；白名单只留三处**逐条写了理由**的"另一张表" | U6-②③ |
| `application/contract/dto/async_run.go` | `AsyncStateRunning` 单常量扩成后台作业状态词表（`running/done/failed/killed`），字面量只此一处 | U6-① |
| `seelebridge/tools/async_exec.go` | 四个状态常量改为转调 dto；补 dto import | U6-① |
| `application/core/async_completion.go` | 触发口径的两个终态常量改为转调 dto | U6-① |
| `application/core/work_table_async.go` | `case "done":` → `case dto.AsyncStateDone:` | U6-① |
| `application/contract/dto/plan_run.go`（新） | plan_run 结果状态词表（`completed/failed/aborted`）+ 边界说明（节点状态词不在这里） | U6-② |
| `seelebridge/plan/tool_provider.go` | `planRunResultJSON` 的四处 status 字面量 → `dto.PlanRunStatus*`；补 dto import | U6-② |
| `application/core/plan_tools.go` | 两处把结果 status 折成计划状态的 switch（各三/两个分支）+ `planRunFailure` 的一处比较 → `dto.PlanRunStatus*` | U6-② |
| `seelebridge/security/process_tree_windows.go` | `ProcessTree` 加 `attachFailed`；`Attach` 两条失败路径置位；`Degraded() = job == 0 \|\| attachFailed`；三处注释同步改口径 | U5 残② |
| `seelebridge/security/process_tree_other.go` | 注释写清平台差异：POSIX 恒 false 是因为没有"挂不上"这一形态 | U5 残② |
| `seelebridge/security/process_tree_attach_test.go`（新） | 失败必报退化 + 活进程挂上必不报退化 | U5 残② |
| `docs/arch/workunit-duplication-inventory.md` | §四 U5/U6 两条结论刷新（第二批已落地 / 第三批在册） | 文档 |

新文件合计约 4KB；无文件删除（这一批全是"字面量 → 契约一处"，删除的是**同一份定义的副本**，
见下）。

## 2. 删除清单（旧 `文件:行` → 去向 → 新唯一位置）

| 旧位置 | 旧内容 | 去向 | 新唯一位置 |
|---|---|---|---|
| `tools/async_exec.go:32-34,36` | `asyncStateRunning/Done/Failed/Killed = "running"/"done"/"failed"/"killed"` | 删除字面量（转调） | `dto/async_run.go` 的 `AsyncState*` |
| `application/core/async_completion.go:47-48` | `asyncStateDone/Failed = "done"/"failed"` | 删除字面量（转调） | 同上 |
| `application/core/work_table_async.go:136` | `case "done":` | 删除字面量（转调） | 同上 |
| `plan/tool_provider.go:442,447,450,452` | `status := "completed"` / `= "failed"` / `= "aborted"` / `= "failed"` | 删除字面量（转调） | `dto/plan_run.go` 的 `PlanRunStatus*` |
| `application/core/plan_tools.go:99,102,104` | `case "completed"/"failed"/"aborted":` | 删除字面量（转调） | 同上 |
| `application/core/plan_tools.go:455,458` | `case "completed":` / `case "aborted":` | 同上 | 同上 |
| `application/core/plan_tools.go:645` | `result.Status != "failed"` | 删除字面量（转调） | 同上 |
| `security/process_tree_windows.go`（Attach 两条失败路径） | `Degraded()` 不认"进程没挂进 Job" | 判据扩到两种形态 | 同文件 `Degraded()` 一处（`job == 0 \|\| attachFailed`） |

## 3. 红 → 绿原文

### 3.1 门禁先红（14 处命中，其中 2 处是新发现的）

```
--- FAIL: TestSubagentStatusVocabularyGate (0.01s)
    [后台作业状态] application/core/async_completion.go:47 [状态常量声明] "done"：asyncStateDone   = "done"
    [后台作业状态] application/core/async_completion.go:48 [状态常量声明] "failed"：asyncStateFailed = "failed"
    [后台作业状态] application/core/work_table_async.go:136 [switch 状态分支] "done"：case "done":
    [后台作业状态] seelebridge/tools/async_exec.go:32 [状态常量声明] "running"：asyncStateRunning = "running"
    [后台作业状态] seelebridge/tools/async_exec.go:33 [状态常量声明] "done"：asyncStateDone    = "done"
    [后台作业状态] seelebridge/tools/async_exec.go:34 [状态常量声明] "failed"：asyncStateFailed  = "failed"
    [后台作业状态] seelebridge/tools/async_exec.go:36 [状态常量声明] "killed"：asyncStateKilled = "killed"
    [后台作业状态] seelebridge/tools/async_exec.go:1029 [状态字段] "killed"：Status: "killed", Handle: run.handle, …State: asyncStateRunning,
    [计划批次结果状态] seelebridge/plan/tool_provider.go:442 [状态赋值] "completed"：status := "completed"
    [计划批次结果状态] seelebridge/plan/tool_provider.go:447 [状态赋值] "failed"：status = "failed"
    [计划批次结果状态] seelebridge/plan/tool_provider.go:450 [状态赋值] "aborted"：status = "aborted"
    [计划批次结果状态] seelebridge/plan/tool_provider.go:452 [状态赋值] "failed"：status = "failed"
    [计划批次结果状态] seelebridge/plan/tool_provider.go:515 [switch 状态分支] "failed"：case "failed":
    [计划批次结果状态] seelebridge/plan/tool_provider.go:524 [switch 状态分支] "completed"：case "completed":
FAIL	github.com/RedHuang-0622/seelex/e2e	0.199s
```

两处**新发现**（我上一批没点到的）：

- `async_exec.go:1029` 的 `Status: "killed"` —— 同一结构里 `State:` 才是状态机那一格，`Status:` 是
  **回执状态**（`observed|killed|already_finished|finished`）。它不是本格的词，进白名单并写明理由；
  回执词表的收口要连 `job_manage` 的四种 op 一起做，登记为第三批。
- `tool_provider.go:515/524` 的 `switch nr.Status` —— 框架 workplan 的**节点状态**词
  （`NodeBase.Status`），同样进白名单并写明理由。

改完：

```
ok  	github.com/RedHuang-0622/seelex/e2e	0.246s
```

### 3.2 U5 残②先红（"挂不上"被瞒下）

```
--- FAIL: TestAttachFailureMarksTreeDegraded (0.00s)
    process_tree_attach_test.go:37: 进程没挂进 Job，树却不是退化态（err=OpenProcess(2147483632): The parameter is incorrect.）：终止只及直接子进程这件事被瞒下了
=== RUN   TestAttachSuccessKeepsTreeHealthy
--- PASS: TestAttachSuccessKeepsTreeHealthy (0.04s)
FAIL	github.com/RedHuang-0622/seelex/seelebridge/security	0.922s
```

改完（整个 security 包，含既有的 `TestNewProcessTreeNotDegraded`）：

```
--- PASS: TestAttachFailureMarksTreeDegraded (0.00s)
--- PASS: TestAttachSuccessKeepsTreeHealthy (0.02s)
--- PASS: TestProcessTreeFlagsSurviveRepeatedConfigure (0.00s)
--- PASS: TestNewProcessTreeNotDegraded (0.00s)
ok  	github.com/RedHuang-0622/seelex/seelebridge/security	1.571s
```

## 4. 命令原始读数

| 命令 | 读数 |
|---|---|
| `gofmt -l`（改动 + 新增文件） | 无输出 |
| `go build ./...` | 无输出 |
| `go test ./e2e/ -run TestSubagentStatusVocabularyGate -count=1` | `ok 0.246s`（先红 14 处 → 绿） |
| `go test ./seelebridge/security/ -count=1` | `ok 1.571s` |
| `go test ./seelebridge/... ./application/... -count=1` | 54 行全 `ok`（含 `application/core` 27.4s、`seelebridge/tools` 23.4s、`seelebridge/plan` 2.3s、`application/contract/dto` 1.3s） |

## 5. 性能量级

这一批新增的运行期代价只有一处：`Degraded()` 多读一个 bool（`job == 0 || attachFailed`），
其余全是"字面量换常量"。同口径读数（改动后）：

```
BenchmarkNoteProcessTreeDegradedHealthy-8    90838897   13.08 ns/op   0 B/op   0 allocs/op   （每条同步命令一次的那条判据）
BenchmarkNoteProcessTreeDegradedReported-8   68557324   18.56 ns/op   0 B/op   0 allocs/op
```

判据仍在纳秒档、零分配；计划状态的 `switch` 与作业状态的比较都只把字面量换成常量，指令数不变。

## 6. 门禁的边界（这一批新增的部分）

- 三格声明：**记录状态**（8 个文件）、**后台作业状态**（9 个文件）、**计划批次结果状态**（1 个文件）。
- 为什么 `plan_tools.go` 不在计划批次那一格里：同一文件里既读**批次结果** status、又读**框架节点**
  status（`running|completed|failed|skipped|canceled|aborted|panicked`）。两者形状相同、语义不同，
  按形状分不开——所以清单只收写侧，读侧的三处改成引契约常量由编译器钉住，边界写进门禁头注释。
- 五处白名单只留了三处（`async_exec.go` 的回执 `killed`、`tool_provider.go` 的两个节点状态词），
  每条都写了"它属于哪张表、该收去哪"；**过期条目也会红**（改完这三处或哪天真收口了，条目必须删）。

## 7. 未决项（本轮之后）

1. **第三批（工具事件/工具调用状态词）**：`session/tool_events.go`（running|success|error）与
   `application/core/tool_hooks.go` + `application/core/subagent_view/coordinator.go`
   （running|completed|failed）两张表叠在三处，**收口前先把读方点清点**（本批没做，是因为
   光靠形状无法把两张表分开，先清点才不会再造一个假门禁）。
2. **回执状态词**（`observed|killed|already_finished|finished`）：与 `job_manage` 的四种 op 一起收。
3. **U5 残①**：同步链退化读数只到后端诊断口，进不进工具结果（改对外形状）仍是独立决定。
4. 统一事件摘要状态词（`events_unified.go:89`）：Seele 框架 `SummaryEvent.Status` 的词，
   **不归我们的契约**——这条建议长期保留在"像却不并"的名单里。
5. 前批遗留不变：②U2（teammate 时间字段恒零值）、②U6（统一实时事件粒度）、
   `worktree/README.md:97` 文档债、CRLF 幻影脏本体。
