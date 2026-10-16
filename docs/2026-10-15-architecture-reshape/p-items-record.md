# 迁移后提案的执行记录（P-items）

日期：2026-10-16。本文件记录 [post-migration-proposals.md](post-migration-proposals.md) 里
**已裁决并已执行**的提案条目（"已做"的事实、删除清单、证据、验证），与该文件的
"提案 + 裁决"分工对应：提案表只写决策，执行细节写在这里，避免把规划写成既成事实。

---

## P11 —— 节点预算上限守卫（覆盖缺口）

提交：见本条记录的 commit（`test(plan): ...`）。

**先更正原判断（重要）**

提案表原写"`docs/arch/ARCHITECTURE_REVIEW.md` 宣称的 `plan_input_fuzz_test.go` 在现行树**不存在**"。
复核后更正为：

| 事实 | 证据 |
|---|---|
| `backend/engine/assemble/plan_input_fuzz_test.go` **存在**（1059 字节，随 `seelebridge` 根包落位） | `Get-ChildItem -Recurse -Filter plan_input_fuzz_test.go` 命中 |
| 它只 fuzz `plan.NormalizePlanLoadArguments`（规范化幂等 + 输出仍是合法 JSON） | 该文件全文 38 行 |
| 它**不触及** `ValidatePolicyLoad` 的 `MaxNodeLoops`/`MaxNodeOutputTokens` 分支 | `ValidatePolicyLoad` 在该文件里没有被调用 |

即：**覆盖缺口是真的，但"文件不存在"这个理由是错的**——原文档把一个"规范化 fuzz 用例"
当成了"预算上限守卫"，属于**张冠李戴的引述**（比单纯漏写更糟：它让后来人以为有覆盖）。

**做法**：在判据所在的包补边界用例（而不是把文档改成"无覆盖"——判据有可测入口，
`ValidatePolicyLoad(policy, argsJSON)` 是纯函数，补用例成本低于留缺口）。

**新增**：`backend/tool/plan/policy_test.go`（原来该文件不存在）。

| 用例 | 钉住的判据面 |
|---|---|
| `TestValidatePolicyLoadNodeBudgetGuard/上限值本身放行（边界内）` | `max_loops=limit`、`max_output_tokens=limit` **放行**（不是 `>=` 而是 `>`） |
| `.../max_loops 越界一档` | `max_loops=limit+1` 拒绝，错误含 `budget max_loops=6 exceeds limit 5` |
| `.../max_output_tokens 越界一档` | `max_output_tokens=limit+1` 拒绝 |
| `.../两者都越界时 loops 先判` | 两处比较的**先后顺序**（loops 在前） |
| `.../多节点中任意一个越界即拒` | 遍历全部节点，不短路在第一个 |
| `.../节点载荷不是 JSON 对象` | 节点 `budget` 解包失败的错误面 |
| `TestValidatePolicyLoadZeroBudgetLimitsDoNotEnforce` | `0 = 不限` 的口径 |
| `TestValidatePolicyLoadMaxNodesBoundary` | `MaxNodes` 的 limit-1 / limit / limit+1 三档 + 文案里的节点计数 |
| `TestValidatePolicyLoadMalformedArgs` | 非法 JSON 拒绝面（返回 0 节点 + 可读错误） |
| `TestValidatePolicyLoadRequireSerial` | 串行链放行 / 分叉拒绝 |

**门禁式证据（变异验证）**：把实现里的 `payload.Budget.MaxLoops > policy.MaxNodeLoops`
临时改成 `>=`，新用例**立刻红**（3 个子用例失败，含"上限值本身放行"），改回即绿——
证明这些用例真的钉在判据上，而不是"跟着实现写"的镜像断言。

**文档同步**：`docs/arch/ARCHITECTURE_REVIEW.md` 的"Plan 节点 budget 上限"一行改为指向
真实用例，并写明原引用文件存在但覆盖的是另一个函数。

**未改动**：`docs/arch/plan-executor.md` 引用的 `plan_kernel_test.go` / `plan_test.go` /
`plan_input_fuzz_test.go` **三个文件都存在**（前者在 `backend/engine/assemble/`），引述无误，不动。
`docs/2026-08-12-module-split/baseline-architecture.md` 的文件清单是**当时的事实快照**，按
"历史记录不改写"的口径保持原样。

**验证**：`go test ./backend/tool/plan/ -count=1` 绿（含新增 10 条子用例）；
`go vet ./backend/tool/plan/` 绿；`gofmt -l .` 空。
