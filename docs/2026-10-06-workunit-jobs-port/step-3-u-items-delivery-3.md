# 步骤③ 状态枚举化落地记录（用户口径：枚举 + iota，不用零散字符串硬编码比较）

- 口径（用户原话）：**"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**；跨边界
  （JSON / 工具结果 / 看板文本）仍以**对外词**出现——词只在一处（枚举的 `words` 表 + `String()`），
  转换点写在边界上。
- 上游：`step-3-u-items-delivery-2.md`（上一批刚把这两格的词收成"契约里的一处常量"）。
  那一批只解决"字面量只剩一处"；**无类型字符串仍能被任意字面量直接比较**（`record.State == "running"`
  照样编译过）。本批把"一处常量"升级成"**类型**"。
- 基线：`2332bf9`。与两张作业表的关系：**只换类型的形状，不动表**（登记表/表机制/取回工具一律没动）。

## 1. 改动清单

### 1.1 契约（词表升级成枚举）

| 文件 | 改动 |
|---|---|
| `application/contract/dto/async_run.go` | 后台作业状态从四个无类型字符串常量 → **`type AsyncState uint8` + iota**（`Unknown/Running/Done/Failed/Killed`）+ `asyncStateWords` 一张对照表 + `String()` / `ParseAsyncState` / `MarshalJSON` / `UnmarshalJSON`；`AsyncRunRecord.State` 字段类型跟着换成 `AsyncState` |
| `application/contract/dto/plan_run.go` | 计划批次结果状态同样升级成 `type PlanRunStatus uint8` + iota（`Unknown/Completed/Failed/Aborted`）+ 同一套方法 |
| `application/contract/dto/teamwork_jobs.go` | `TeamworkJobCompletionRecord.State` 从 `string` → `AsyncState`（词来自框架 `jobs.State*`，在 seelebridge 投影处折一次，见下） |
| `application/contract/dto/state_enum_test.go`（新） | 枚举的 wire 形状 / 拒绝未知词 / 整条记录 round-trip / 计划枚举与"节点状态词不属于这一格" |

### 1.2 写方与读方（全部改成类型，不再有字符串比较）

| 文件 | 改动 |
|---|---|
| `seelebridge/tools/async_exec.go` | `asyncRun.state` → `dto.AsyncState`；`asyncPayload.State`（工具结果 JSON 字段）→ `dto.AsyncState`；`noteRetiredLocked` / `summarizeLog` 的状态参数 → `dto.AsyncState`；两处边界（退休表、摘要行）用 `.String()` |
| `seelebridge/tools/async_probe.go` | 探针读数 `State` → `dto.AsyncState` |
| `seelebridge/tools/job_contract.go` | 摘要行 `run.state.String()`（边界） |
| `seelebridge/tools/job_tools.go` | 回执行 `State` → `dto.AsyncState`；回执 map 的 `"state"` 用 `.String()`（边界） |
| `seelebridge/tools/job_subagent.go` | `CompleteJob(handle string, state dto.AsyncState)`；`switch strings.ToLower(strings.TrimSpace(state))` → **`switch state`**（枚举没有"大小写/空格"这一层，规范化由类型保证） |
| `seelebridge/fork/types.go` | 子代理作业终态端口：`Complete(handle string, state dto.AsyncState)` |
| `seelebridge/fork/tool.go` | 四处 `"failed"` / `"done"` / `"killed"` 字面量 → 枚举；`batchState` / `state` 变量类型跟着换 |
| `seelebridge/runtime_plan.go` | 适配器 `Complete(handle string, state dto.AsyncState)` |
| `seelebridge/runtime_teamwork_jobs.go` | 新增 `asyncStateFromJobs(jobs.State) dto.AsyncState`：框架词 → 契约枚举**只在这里折一次**（折不动的落 `Unknown`，投影照搬、判定留给消费方） |
| `application/core/async_completion.go` | `asyncCompletionTriggers(state dto.AsyncState)` |
| `application/core/work_table_async.go` | `asyncWorkStatus(state dto.AsyncState)`；两条行文本用 `.String()`（边界） |
| `application/core/plan_tools.go` | 三处结果 status 字段/比较 → `dto.PlanRunStatus` |
| `seelebridge/plan/tool_provider.go` | 结果结构体 `Status dto.PlanRunStatus`（wire 由 `MarshalJSON` 说出对外词） |

### 1.3 既有用例的**机械适配**（类型换了，断言语义一条没动）

| 文件:行 | 旧写法 | 新写法 | 为什么不动语义 |
|---|---|---|---|
| `seelebridge/tools/job_contract_test.go:318/321` | `CompleteJob(handle, "killed")` | `CompleteJob(handle, dto.AsyncStateKilled)` | 断言仍是"合成终态成功/幂等" |
| `seelebridge/fork_job_helpers_test.go:146/178/200` | `= record.State` / `!= "running"` / `return record.State` | `.String()` / `!= dto.AsyncStateRunning` / `.String()` | 断言仍是"等到没有在跑的作业"与"读一条作业的状态" |
| `seelebridge/node_first_person_live_smoke_test.go:102` | `!= "running"` | `!= dto.AsyncStateRunning` | 同上 |
| `seelebridge/runtime_async_test.go:18` | `State: "failed"` | `State: dto.AsyncStateFailed` | 造样本记录，断言仍是投影内容 |
| `seelebridge/runtime_teamwork_jobs_test.go:90/134` | `!= string(jobs.StateFailed/Running)` | `!= dto.AsyncStateFailed/Running` | 断言仍是"框架词在投影处折成契约词"，折法就是本批新增的那个函数 |
| `seelebridge/subagent_job_contract_test.go:113` | `== "running"` | `== dto.AsyncStateRunning` | 同上 |
| `application/core/async_completion_trigger_test.go:64/66/113/124` | 助手参数 `state string`、`asyncCompletionTriggers("killed")`、`completedRecord("a10","killed")` | 换类型 / 枚举常量 | 断言仍是"done/failed 触发、killed 与 running 不触发" |
| `application/core/teamwork_completion_trigger_test.go:38/88` | 助手参数 `state string`、`("ab10","killed")` | 换类型 / 枚举常量 | 同上（第二张表同一口径） |
| `application/core/work_table_async_test.go:68/98/113/115/143/160/161/177/178` | `point.Status != dto.AsyncStateRunning`、`base.State = "done"` 等 | `.String()` / 枚举常量 | 断言仍是"done→completed、failed/killed→failed"与行文本内容 |
| `application/core/README-service.md:433` | `completedTeammateRecord(handle, state string)` | 新签名 | 文档跟着签名走 |

**没有**改任何断言的期望值、没有删任何用例、没有放松任何断言（逐条对照上表）。

## 2. 删除清单（旧写法 → 去向 → 新唯一位置）

| 旧位置 | 旧写法 | 去向 | 新唯一位置 |
|---|---|---|---|
| `dto/async_run.go`（本批前） | 四个**无类型**字符串常量 `AsyncStateRunning = "running"` … | 删除（升级成类型） | 同文件 `AsyncState` + `asyncStateWords` |
| `dto/plan_run.go`（本批前） | 三个无类型字符串常量 | 删除（升级成类型） | 同文件 `PlanRunStatus` + `planRunStatusWords` |
| `fork/tool.go:164,242,249` | `Complete(..., "failed" / "done")` | 删除字面量 | `dto.AsyncStateFailed/Done` |
| `fork/tool.go:203-215` | `batchState := "done"` / `= "killed"` / `= "failed"`、`state == "done"` | 删除字面量 | `dto.AsyncState*` |
| `tools/job_subagent.go:117` | `switch strings.ToLower(strings.TrimSpace(state))` | 删除（枚举自带规范化） | 同文件 `switch state` |
| `runtime_teamwork_jobs.go` | `State: string(record.State)`（框架词直接透传成字符串） | 删除（换成一次折叠） | 同文件 `asyncStateFromJobs` |
| 各写方/读方 | `record.State == "running"` 等字符串比较 | 删除 | 枚举比较（**编译期**挡住写错词） |
| 边界行文本 | 直接把状态当字符串拼进文本 | 保留但显式转换 | `.String()`（本批 6 处：退休表、摘要行、行文本 ×2、工具结果 map、探针文本） |

## 3. 红 → 绿

### 3.1 先红：用例先写，编译不过（比"用例跑红"更硬的一层）

```
$ go test ./application/contract/dto/ -run "Enum|Wire|Rejects" -count=1
application\contract\dto\state_enum_test.go:21:9: undefined: AsyncState
application\contract\dto\state_enum_test.go:28:4: undefined: AsyncStateUnknown
application\contract\dto\state_enum_test.go:41:17: undefined: ParseAsyncState
application\contract\dto\state_enum_test.go:87:10: undefined: PlanRunStatus
application\contract\dto\state_enum_test.go:106:17: undefined: ParsePlanRunStatus
```

同时把字段类型换掉后，**全仓编译错误就是"还有哪些地方在拿字符串比状态"的清单**（这正是枚举化要的
效果：漏一处就编译不过）：

```
$ go build ./...
seelebridge\tools\async_exec.go:303:53: invalid operation: run.state == asyncStateRunning (mismatched types string and dto.AsyncState)
seelebridge\tools\async_exec.go:327:16: cannot use asyncStateRunning (constant 1 of uint8 type dto.AsyncState) as string value in struct literal
… （迁移面共 20 余处，逐个换类型/补 String()）
```

### 3.2 后绿

```
$ go test ./application/contract/dto/ -run "Enum|Wire|Rejects" -count=1
--- PASS: TestAsyncStateEnumKeepsWireShape (0.00s)
--- PASS: TestAsyncStateRejectsUnknownWord (0.00s)
--- PASS: TestAsyncRunRecordWireRoundTrip (0.00s)
--- PASS: TestPlanRunStatusEnumKeepsWireShape (0.00s)
$ go test ./application/contract/dto/ -count=1
ok  	github.com/RedHuang-0622/seelex/application/contract/dto	0.604s
```

期间另一处真红（用例写错时暴露的设计问题）：`TestAsyncStateRejectsUnknownWord` 第一版断言"报错后值变成
Unknown"，实际实现是"报错时**原值不动**"——按"不留半个值"的真实语义改断言（不是改实现）。

## 4. 命令原始读数

| 命令 | 读数 |
|---|---|
| `gofmt -l`（改动 + 新增文件） | 无输出 |
| `go build ./...` | 无输出 |
| `go vet ./...` | 无输出（测试文件也一并编译过） |
| `go test ./seelebridge/... ./application/... ./e2e/ -count=1` | 55 行：**48 `ok` / 0 `FAIL`**（其余 7 行是 `[no test files]`），含 `e2e 0.840s`、`application/core 27s`、`seelebridge/tools 22s`、`seelebridge 56s` |
| `go test ./e2e/ -run TestSubagentStatusVocabularyGate -count=1` | `ok … 0.340s`（上一批的源码门禁在新形状下仍绿：词只在枚举的 `words` 表里出现一次） |

性能量级（本批没有新增热路径工作量；改的是"字符串比较 → 整数比较"和边界上的 `.String()`）：

```
BenchmarkTeamworkBoardSnapshot-8   7707   164161 ns/op   167514 B/op   1067 allocs/op
```

这条读数覆盖 teammate 作业投影（含本批新增的 `asyncStateFromJobs` 折叠）；它的量级由 jobs.Manager
快照与 DTO 组装决定——**不从这条读数主张"更快/更慢"**（没有做改动前的同口径对比）。

## 5. wire 兼容（枚举化的最大风险，逐条钉住）

| 风险 | 钉法 |
|---|---|
| 枚举的**整数值漏到 JSON** | `MarshalJSON` 走 `String()`；用例断言 `json.Marshal(AsyncStateKilled) == "killed"`（不是 `4`） |
| 记录整条 JSON 形状变化 | `TestAsyncRunRecordWireRoundTrip` 断言 `"state":"running"` 仍是字符串，并能读回 |
| 认不得的词被静默吞掉 | `ParseAsyncState` 返回 `(Unknown,false)`；`UnmarshalJSON` **报错**且**不动原值**（用例钉住三条） |
| 计划批次那一格把框架节点词读进来 | `ParsePlanRunStatus("panicked")` 必须判否（用例钉住） |

## 6. 边界（写清"为什么像却不并"）

- **框架的词**：teammate 作业状态来自 Seele `jobs.State*`（`running|done|failed|killed`，与本表同形但不
  由我们定义）；本轮在 seelebridge 投影处**折一次**（`asyncStateFromJobs`），application 侧从此只认类型。
- **框架的节点状态**：`NodeBase.Status`（`completed|failed|skipped|…`）不属这一格，`plan_tools.go` 里读节点
  状态的分支仍是字符串（在门禁里以白名单条目形式登记了理由）。
- **开放取值字段**：`dto/teamwork_board.go:190` 的 `State`（看板投影，注释写明"开放取值"）本批保持 `string`
  ——它不是判定面，是展示投影。
- **落盘**：后台作业登记表**不落盘**（不变量 I-21），所以本批不涉及旧文件读回；`jobs` 那一侧的快照也不落盘。
  **记录状态那一格**（`dto.SubAgentNodeStatus`，落盘 + 恢复 + GUI）**本批没做**——它要额外回答"旧文件里
  认不得的词，是报错还是折成 Unknown"，见未决项 ⑥。

## 7. 未决项

1. **记录状态那一格**（`dto.SubAgentNodeStatus`：现为 `type X string` + 四常量，字段仍写 `string`）：枚举化要
   连落盘恢复一起过，且要先定"未知词"的取舍（报错 vs `Unknown`）。面最大，单独一批。
2. **工具事件状态 / 工具调用状态**（running|success|error 与 running|completed|failed 两张表在三处交织）：
   收口前先点清读方。
3. **回执状态词**（`observed|killed|already_finished|finished|retired`）：与 `job_manage` 的四种 op 一起收。
4. 框架词的直读点（`NodeBase.Status`、`SummaryEvent.Status`）：不在我们契约内，保持字符串 + 白名单理由。
5. 仓库里既有 `TaskStatus` / `SubAgentNodeStatus` / `TodoItemStatus` 是 `type X string` 型枚举。本批按用户
   口径走 **int + iota**；两套风格并存。若要把它们统一成 iota（= 未决项 1 的延伸），需要一次专门的形状批。
6. 前批遗留不变：②U2（teammate 时间字段恒零值）、②U6（统一实时事件粒度）、U5 残①（同步链读数进不进工具
   结果）、`worktree/README.md:97` 文档债、CRLF 幻影脏本体、GUI 手工冒烟。
