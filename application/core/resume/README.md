# core/resume

## 生态位

`application/core/resume` 是「终止前未完成工作」的**领域无关恢复模板**：把
「定位 → 判定 → 补历史 → 重建现场 → 注入说明 → 同键重跑 → 收敛」这条流水线
固化成唯一权威顺序，具体领域（subagent、role draft、后续其它派发单元）只
提供 `Port` 实现。主要调用方：`application/core/session_runtime` 与
`seelebridge` 的子代理恢复路径；设计依据见
[docs/arch/a2a-agent-team-factory.md](../../../docs/arch/a2a-agent-team-factory.md)
的 §5.1 与 §9（AT10）。

## 职责与非职责

职责：

- 定义七步顺序（`Order()`）并保证调用方不能自排；
- 按幂等键（`Unit.Key`）去重、串行领取（`claim`/`release`），同一 Key 不并发重跑；
- 只对 `active` 单元执行「重建 → 注入 → 重跑」，已终结（done/failed）单元只补历史；
- 产出可审计的 `Report`（Resumed/Skipped/Failed，稳定排序）。

非职责：

- 不认识 subagent / goal / role draft 或任何具体存储（只认识 `Unit` 与 `Port`）；
- 不决定「未完成」的判据（由 `Port.Locate` 给出）；
- 不做持久化：结论跟随被恢复单元的父级写回路径（`Port.Converge`）。

## 步骤流水线

```text
Locate 定位 → Decide 判定 → RepairParent 补历史 → RestoreScene 重建现场
  → InjectNote 注入说明 → Reexecute 同键重跑 → Converge 收敛
```

「补历史」写的是 provider-only 占位，保证父侧 assistant/tool 调用成对合法；
「注入说明」只进被执行单元自己的上下文；「收敛」删除临时现场并把结论按正常
路径写回父级。

## 依赖方向

只依赖标准库（`context`/`errors`/`fmt`/`sort`/`sync`/`time`）。禁止反向依赖
`core` 根包或任何领域包；领域侧通过 `Port` 注入。

## 并发、错误与幂等语义

- `Runner` 持 `sync` 在途集合：同一 `Key` 重复调用返回 `Skipped` 而不是并发重跑；
- 失败可重试：`release` 在收敛或失败后都释放，`ExecuteKey` 是定点重试入口；
- 未装配端口（`port == nil`）时 `Execute` 显式返回 `ErrPortNotAssembled`，
  不静默空转；`Port.Locate` 报错视为阻塞错误。

## 扩展方式

新增恢复领域：实现 `Port` 的七个方法（各自保证幂等）并在组合根装配；不要为
新领域在模板内加 `if kind == ...` 分支，也不要在模板里重排步骤。

## Review 指南

- 步骤顺序是否仍由 `Order()` 唯一权威决定，调用方没有自排；
- 同一 `Key` 在任何恢复轮次里是否仍是同一个幂等键（否则会产生第二条结论）；
- 已终结单元是否只补历史、没有被重启；
- `Port` 方法自身的幂等是否仍成立。

## 测试与验证

```text
go test ./application/core/resume -count=1
```

`resume_test.go` 覆盖步骤顺序稳定、active/terminal 分派、按 Key 去重与空键
忽略、失败可重试、在途 Key 跳过、未装配端口与 `Locate` 报错。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### resume.go

- `func Order() []Step` — Order 返回模板的标准步骤顺序（唯一权威，调用方不得自排）。
- `func (u Unit) Active() bool` — Active 报告单元是否仍需重启续跑。
- `func (r UnitResult) Failed() bool` — Failed 报告本轮是否失败。
- `func (r Report) Resumed() []string` — Resumed 返回本轮真正重启的单元键（稳定排序）。
- `func (r Report) Skipped() []string` — Skipped 返回本轮未重启的单元键（稳定排序）。
- `func (r Report) Failed() []UnitResult` — Failed 返回本轮失败的单元结果（稳定排序）。
- `func NewRunner(port Port) *Runner` — NewRunner 构造恢复执行器；port 为 nil 时 Execute 返回 ErrPortNotAssembled。
- `func (r *Runner) Execute(ctx context.Context) (Report, error)` — Execute 执行一轮恢复：定位 → 判定 → 补历史 → 重建 → 注入 → 重跑 → 收敛。
- `func (r *Runner) ExecuteKey(ctx context.Context, key string) (UnitResult, error)` — ExecuteKey 只恢复指定幂等键的单元（定点重试入口；未定位到 → Skipped）。
- `func (r *Runner) executeUnit(ctx context.Context, unit Unit) UnitResult` — executeUnit 跑完一个单元的全部适用步骤。
- `func (r *Runner) claim(key string) bool` — claim 标记 Key 在途（幂等：同一 Key 不并发重跑）。
- `func (r *Runner) release(key string)` — release 释放在途标记（收敛或失败后都释放，失败可重试）。
- `func dedupeUnits(units []Unit) []Unit` — dedupeUnits 按 Key 去重并稳定排序（幂等键唯一，重复定位取首个）。

### resume_test.go

- `func (p *fakePort) Locate(context.Context) ([]Unit, error)`
- `func (p *fakePort) RepairParent(_ context.Context, unit Unit) error`
- `func (p *fakePort) RestoreScene(_ context.Context, unit Unit) (Scene, error)`
- `func (p *fakePort) InjectNote(_ context.Context, unit Unit, _ Scene) error`
- `func (p *fakePort) Reexecute(_ context.Context, unit Unit, _ Scene) (Outcome, error)`
- `func (p *fakePort) Converge(_ context.Context, unit Unit, _ Outcome) error`
- `func (p *fakePort) calls() (repairs, restores, injects, reexecs, converges []string)`
- `func TestOrderIsStable(t *testing.T)`
- `func TestExecuteActiveUnitRunsAllSteps(t *testing.T)`
- `func TestExecuteTerminalUnitRepairsHistoryOnly(t *testing.T)`
- `func TestExecuteDeduplicatesByKeyAndIgnoresEmptyKeys(t *testing.T)`
- `func TestExecuteFailureIsReportedAndRetryable(t *testing.T)`
- `func TestExecuteSkipsKeyAlreadyInFlight(t *testing.T)`
- `func TestExecuteKeyReportsUnknownUnit(t *testing.T)`
- `func TestExecuteRequiresPort(t *testing.T)`
- `func TestExecuteLocateErrorIsBlocking(t *testing.T)`
- `func joinSteps(steps []Step) string`

