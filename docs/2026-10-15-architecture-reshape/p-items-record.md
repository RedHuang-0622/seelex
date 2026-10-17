# 迁移后提案的执行记录（P-items）

日期：2026-10-16。本文件记录 [post-migration-proposals.md](post-migration-proposals.md) 里
**已裁决并已执行**的提案条目（"已做"的事实、删除清单、证据、验证），与该文件的
"提案 + 裁决"分工对应：提案表只写决策，执行细节写在这里，避免把规划写成既成事实。

---

## P1 —— 路径门前移（M2-C5）

提交：`aabb311d`。

**改前**：事件 → 权限判定（`ask` 就地弹审批）→ 诊断 → handler；路径越界只在 handler
里由 ProjectScope 发现。越界路径会先弹一次完全没用的审批，批准后仍然被拒。

**改后**：事件 → **路径门** → 权限门 → 诊断 → handler。越界路径直接拒，不弹审批。

| 面 | 落点 | 说明 |
|---|---|---|
| 中间件 | `backend/tool/registry/path_gate.go` 的 `PathGate` | `NewRegistryState` 的 Meta 链顺序显式写成 事件 → 路径门 → 权限门 |
| 路径根解析 | `router.go` 的 `resolveScopedPath` | 唯一实现：handler 与路径门共用；NodeScope worktree 根优先，否则按会话键取项目根 |
| 入参表 | `pathGateTargets` | `path`（read/grep/glob/write/edit）、`read_batch.paths[]`、`bash`/`bash_bg` 的 `workdir`；坏 JSON 不在此报错（交给原参数校验，避免两种文案） |
| 装配 | `assemble/runtime.go` | 用 `r.projectScope` + `internal/telemetry.SessionIDFromContext` 构造 |

**行为差异（有意）**：越界路径的"谁先拒"变了——不再有审批事件；错误正文仍是
ProjectScope 的原文（`... bound project`）。界内路径的审批与执行顺序不变。

**证据**：

- `TestPathGateRejectsEscapeBeforePermissionAsk`：越界路径 `approvals=0 / ran=0`；
  同一门内界内路径 `approvals=1 / ran=1`（顺序是判据，不只是"会拒"）。
- `TestPathGateUsesNodeScopeRoot`：worktree 内放行、worktree 外拒绝。
- `TestPathGateCoversBatchAndBashWorkdir`：批量读任意一条越界即拒；bash workdir 同门。
- `go test ./backend/tool/registry/ ./backend/engine/assemble/ ./gui/ ./e2e/` 全绿。

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

---

## P9 —— 统一事件错误文案（`sessionstore` → `store`）

提交：见本条记录的 commit（`fix(events): ...`）。

**先更正位置**：提案表写的位置是 `backend/engine/obs`，`rg` 实测两处文案在
`backend/engine/assemble/events_unified.go:209,232`（`obs` 包不含该字符串）。

**改动（判据归一，不是简单改字）**

原实现是**同一句文案写两份**（`UnifiedEvents` 与 `UnifiedEventsRange` 各一处）。本次没有只替换
两个字符串，而是把它收成**一处**：

```go
var errUnifiedEventsRouterMissing = errors.New("unified events: store router is not attached")
```

两个入口共用该变量——按 AGENTS §8.3「同一判据一处」，两处各写一份下次改名还会漏一处（这正是
D-M3-12 的成因：M3 改名 `sessionstore` → `store` 时文案没跟着走）。

**用例钉住**（不是"删掉断言让它绿"）：`backend/engine/assemble/events_unified_test.go` 的
`TestUnifiedEventsRequiresRouter` 由"只判 err != nil"加强为：

- 两个入口**分别**断言错误逐字含 `unified events: store router is not attached`；
- 并断言错误里**不含** `sessionstore`（防止旧包名回潮）。

**验证**：`go test ./backend/engine/assemble/ -count=1 -run TestUnifiedEvents` 绿；
`gofmt -l backend/engine/assemble/` 空；`rg 'sessionstore router is not attached' -g '*.go'` 无命中；
`rg 'sessionstore'` 在现行生产代码里的残留另见 P8 清单（本片只负责这一句文案）。

---

## P10 —— 「会话不存在」判据漂移收口（较高优先级 bug）

提交：见本条记录的 commit（`fix(store): ...`）。

**执行中发现漂移面比提案更宽**：提案写"两份实现"，实测是 **7 处、4 个包**：

| 位置 | 修复前形态 | 语义 |
|---|---|---|
| `store/durable_history.go` | `func isSessionNotFound` | 只看 `fs.ErrNotExist` |
| `seelexctx/search/search.go` | `func isSessionNotFound` | `fs.ErrNotExist \|\| sql.ErrNoRows` |
| `application/core/session_runtime/archive.go` | **内联**判据 | `fs \|\| sql` |
| `wiring/adapters/session_workspace_ports.go` | **内联** 4 处 | `fs \|\| sql` |

**落点与语义的选择（有依据，不是随手取一份）**

- 落点 = **`store`**：它是"未找到"这个错误的**编码者**（JSON 布局把未找到编码为 `fs.ErrNotExist`），
  判据应贴着编码者放，消费方调用它——判据放消费方会随消费方数量分裂。
- 语义 = **只看 `fs.ErrNotExist`**（不是并集）：`store/sessionstore.go` 的 `ErrBackendRetired`
  说明 SQLite / PostgreSQL / Redis 只是保留枚举、`Open` 直接拒绝，**今天没有任何代码路径能产生
  `sql.ErrNoRows`**；保留它等于让"同一份输入"在两条路径上给两种答案（正是本 bug 的形态）。
- 导出 `store.IsSessionNotFound`，函数 doc 里写清"新增后端若换编码只改这一处"，把这条约束钉在代码里。

**机械门禁（防回潮，不是"用例全绿"）**：新增 `e2e/session_not_found_single_criterion_test.go`

1. `TestSessionNotFoundCriterionIsDeclaredExactlyOnce`：扫全仓 Go 源码（含用例）的函数声明，
   按**名字集合**（`IsSessionNotFound`/`isSessionNotFound`/`sessionNotFound`/`SessionNotFound`/
   `isSessionAbsent`）判"判据只许一处，且必须是 `store/durable_history.go:IsSessionNotFound`"；
2. `TestRetiredSQLNoRowsIsReferencedOnlyByListedTestData`：`database/sql` 与 `sql.ErrNoRows`
   只允许出现在**白色清单**登记的文件里（今天只允许红灯用例把 `sql.ErrNoRows` 当**输入数据**喂进去），
   清单还有**规模上限**（只许收缩）与**过期条目检测**（登记了却不再引用就报错）。

**门禁的齿（复核者独立做的变异验证）**：临时放入一份 `sessionNotFound(err)` + `sql.ErrNoRows` 的
探针文件后，**两条门禁同时报红**且分别指名道姓（"实测 2 处"、"已退役的 SQL 后端错误码被代码引用"）；
删除探针即绿。证明门禁钉在判据上，而不是"跟着实现写"。

**红先行的证据**：`seelexctx/search/session_not_found_test.go` 把五种输入（`fs.ErrNotExist`、
`sql.ErrNoRows`、被 `fmt.Errorf("%w")` 包装的 `fs.ErrNotExist`、普通错误、`nil`）喂给唯一判据，
钉住"未找到 = 只看 fs"与"该包已经**不存在**本地副本"（用例里直接扫本包声明）。

**诚实边界**（写进用例头注释，不夸大覆盖）：机械手段只能证明"判据函数唯一 + 退役错误码不再被代码引用"；
一个**新的内联** `errors.Is(err, fs.ErrNotExist)` 副本在语法上与合法用法不可区分，抓不到，那部分靠 review。

**验证**：`go test ./e2e/ -count=1 -run 'TestSessionNotFound|TestRetiredSQLNoRows'` 绿；
`go test ./store/ ./seelexctx/search/ -count=1` 绿；`gofmt -l .` 空。
