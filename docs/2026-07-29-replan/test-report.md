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
