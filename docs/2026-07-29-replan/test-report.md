# Replan 防风暴与幂等重试测试报告

测试日期：2026-07-29。本文记录本次变更的可复核量化结果；真实账号配置只作为不透明输入复制到临时目录，未读取、打印或纳入版本控制。

## 概览

| 范围 | 结果 | 定量结果 |
|---|:---:|---|
| 全仓库 Go 测试 | 通过 | `go test ./... -count=1 -timeout=120s`；慢路径为 `seelebridge` 91.037 s、`seelexctx` 50.318 s |
| 静态与构建 | 通过 | `go vet ./...`、普通构建、`gui,desktop,production` 构建均通过；整条验证流水线 138.7 s |
| 前端测试 | 通过 | 32/32，Node 测试总时长 638.058 ms |
| replan 重复执行 | 通过 | 相关 Runtime/guard 测试连续 10 次通过，50.179 s |
| `plan_load` 基准 | 通过 | 21,548 ns/op、8.35 MB/s、5,011 B/op、76 allocs/op |
| 真实 API A/B 冒烟 | 通过 | A/control 自发 `plan_load` 成功；B/treatment 强制 `plan_load` 成功，实际 2 次 provider 请求 |

## 幂等与风暴边界

| 保护项 | 上限 / 行为 | 覆盖测试 |
|---|---|---|
| 单 Plan 链恢复 | 最多 2 次成功 recovery replan | `TestResolvePlanFailureStopsAfterPlanChainReplanLimit` |
| 全局并发 | 最多 2 个 replan | `TestReplanGuardLimitsDuplicateConcurrencyAndRate` |
| 全局操作频率 | 每分钟最多 6 次 replan | `TestReplanGuardLimitsDuplicateConcurrencyAndRate` |
| provider 成本预算 | 每分钟最多 6 次真实 provider 请求 | `TestReplanGuardCapsProviderRequestsBeforeRetry` |
| 重复操作 | 相同 Interaction / Runtime operation key 在进行中直接拒绝 | `TestReplanGuardLimitsDuplicateConcurrencyAndRate` |
| 自动重试 | 仅 WorkPlan 替换前的 schema 或 policy 校验失败允许 1 次纠错请求 | `TestRuntimePrepareReplanForcesPlanLoadForExplicitLiteRecovery` |

纠错重试不重复执行 recovery Plan：policy 和 `plan_load` handler 都在委托 Seele WorkPlan 替换前拒绝无效 JSON。已加载的 Plan、意外工具调用、provider 执行错误和所有未知错误都不触发自动重试。

## 真实 API A/B 记录

命令：

```powershell
$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path 'config/accounts.yaml').Path
go test -tags manualsmoke . -run '^TestManualSmokeRealAccountPlan$' -count=1 -timeout=2m -v
```

| 试跑 | A/control：Lite + Plan Skill | B/treatment：`PrepareReplan` | 结论 |
|---|---|---|---|
| 1 | 自发 `plan_load` 成功 | 两次请求均未返回工具调用 | 拒绝且不执行；确认 provider 偶发不遵守 tool choice 时不会扩大副作用 |
| 2 | 自发 `plan_load` 成功 | 返回 `edges` 数组；policy 在载入前拒绝 | 暴露重试分类遗漏：此前只识别 `plan_load:` 前缀 |
| 3（修复后） | 自发 `plan_load` 成功 | 成功；provider 请求增量为 2，accepted +1，rejected +0 | 数组格式失败被安全纠正后载入；A/B 通过 |

该样本不足以量化模型规划质量的提升，但量化证明了 treatment 的系统价值：真实错误 JSON 会消耗一次受限 provider 请求，随后最多一次、无副作用的纠错请求可恢复；成本不会因重试无限放大。

## 执行命令

```text
go test ./seelebridge ./application/core ./e2e/scenario -count=1 -timeout=120s
go test ./seelebridge -run 'Test(ReplanGuard|RuntimePrepareReplan)' -count=10 -timeout=120s
go test ./seelebridge -run '^$' -bench BenchmarkPlanLoadSmoke -benchmem -benchtime=1s
go test ./... -count=1 -timeout=120s
go vet ./...
go build ./...
go build -tags "gui,desktop,production" ./...
node --test gui/frontend/dist/*.test.mjs
```

## 本机限制

- Windows 本机以 `CGO_ENABLED=0` 运行；未执行 `-race`，应由 Linux CI 的 race job 补充。
- 未运行模糊、内存泄漏或漏洞扫描；这些不在本次已执行量化结果中。

## DAG 输入适配层验证（2026-07-29）

| 范围 | 结果 | 定量结果 |
|---|:---:|---|
| 数组 DAG 规范化 | 通过 | `nodes[]`（`id`/`key`）和 `edges[]`（`from`/`source`、`to`/`target`）规范化为 canonical 对象 DAG；3 节点、2 边 direct dispatch 成功加载 |
| 边界与 UI 投影 | 通过 | 覆盖嵌套 `{ "to": "id" }`、缺少边来源的拒绝、以及 Application ToolHook 使用 canonical 参数更新 PlanState |
| 全仓验证 | 通过 | Go 测试、`go vet`、普通构建、生产 GUI 构建及前端 32/32；整条本轮验证流水线 124.8 s |
| 真实 API 兼容冒烟 | 通过 | 43.821 s；A/control 自发 `plan_load=true`，B/treatment 强制 recovery `plan_load=true`，provider 请求增量 1 |

数组适配器只解决“模型已经调用 `plan_load`、但偏好列表表示”的解析兼容性；它不会让 provider 在忽略 `tool_choice` 时自动产生工具调用。此前临时生成质量 A/B 的受控观察中，Lite 的 3/3 样本均达到结构评分 6/6，High 已完成的 2 个样本均在 preflight 阶段未产生可加载 Plan，第三个样本因 150 s 测试总时限取消。因此该适配器改善的是输入鲁棒性，尚不能宣称已经提升 High 的生成可用率。

## 适配后真实 API 生成质量 A/B（2026-07-29）

测试使用三个独立会话、相同的审计/发布/项目作用域安全任务。每个任务明确要求 `nodes[]`、`edges[]` 适配输入形式；每个样本单独限制为 50 s，总测试时长 170.096 s。结构评分为 6 分：有效入口、至少三节点、至少两边、全图可达、检查阶段、验证与报告阶段；全部样本禁止 `plan_run`。

| Effort | 有效 Plan | 成功样本结构分数 | 每成功样本 `plan_load` | `plan_run` | 未完成样本 |
|---|---:|---|---:|---:|---|
| Lite | 3/3 | audit 6/6、release 6/6、security 6/6 | 1 | 0 | 无 |
| High | 2/3 | audit 6/6、release 6/6 | 1* | 0 | security 在 50 s 单样本时限内超时 |

适配后的 High 已从此前观测到的“完成样本无可加载 Plan”恢复为 2 个可加载、结构完整的 Plan；但此小样本不能单独证明因果或统计显著性。* 原先“每样本两次 plan_load”是 UI 生命周期口径错误：同一次调用会生成一条 `role=tool` 记录和一条 `role=tool_result` 完成记录。后续统计只以 `role=tool` 记录作为实际工具调用数。

## Preflight authority live acceptance (2026-07-29)

The opt-in real-account smoke completed in 49.06 s. The account file was
copied as an opaque input into a temporary test directory; it was not read,
printed, or versioned.

| Path | Result | Actual plan_load calls | plan_run | Provider requests |
|---|---|---:|---:|---:|
| Medium + authoritative preflight marker | pass | 1 | 0 | one forced preflight; 2 nodes and 1 serial edge |
| High + authoritative preflight marker | pass | 1 | 0 | one forced preflight |
| Lite voluntary recovery control | pass (no voluntary load in this sample) | 0 | 0 | normal ReAct only |
| Explicit replan treatment | pass | 1 forced load | 0 | +1 |

The authoritative context is marked with
`<!-- seelex:plan-context:v1 authority=preflight-loaded -->`. It carries the
canonical DAG and the original user request in one current-turn envelope.
During that ReAct turn, Runtime omits Plan-mutating tools from the visible
snapshot and retains a handler guard; it restores them when ChatStream returns,
before the user-selected bounded replan path is available.

Final local verification completed in 133.4 s: go test ./..., go vet ./...,
normal and production GUI builds, and 32/32 frontend tests all passed.
BenchmarkPlanLoadSmoke measured 45,036 ns/op, 8,897 B/op, and 142 allocs/op
on Windows amd64 (Intel i7-2600).

## Medium/High follow-up real API A/B (2026-07-29)

Two further real-account runs measured the dedicated authority envelope for
both mandatory-planning levels. In both runs, Medium and High each produced
one actual role=tool plan_load record and no plan_run. Medium produced the
required serial two-node, one-edge DAG in both runs; High also produced one
loaded Plan in both runs. This confirms that the envelope is not High-only:
Medium receives the same authority lifecycle while its runtime policy still
enforces at most four serial nodes and concurrency one.

The same two runs attempted the optional explicit replan treatment. The
provider returned an invalid entry/node relationship once and no tool call once;
each was rejected after the single bounded corrective retry. These are recorded
as provider-generation failures, not as Plan execution, replacement, or
unbounded retry. They do not change the successful Medium/High preflight A/B
observations.
