# `TurnScheduler` 的 channel 投递与顺序编辑退场（2026-10-01 · M4 §9 的 #3）

> **口径**：本文记 **M4 清场**里 #3 的退场——`application/core/agentteam/scheduler.go` 的
> `TurnScheduler` 那几组**没有生产消费者**的接口。每条给**引用事实**（全仓 `.go` 调用点，
> 已排除 `_tmp` / `_scratch` / `vendor` / `dist`）与**退场后的形状**。
> 结论分 **Confirmed**（有代码 / 用例 / 命令证据）与 **Hypothesis**（待验证）。

---

## 1. 修前事实（Confirmed）

`scheduler.` 字段访问在全仓 `.go` 里只落在四个文件：`application/core/agentteam/runtime.go`
（生产）、`scheduler.go`（定义）、`scheduler_test.go` / `scheduler_wiring_test.go`（用例）。
逐条列成员：

| # | 退场对象 | 修前引用事实 | 结论 |
|---|---|---|---|
| 1 | `Requests()` / `Request()` / `Next()` | 只有 `scheduler_test.go` 调 `Request` / `Next`；`Requests()` 连用例都没有（**零调用点**）。文件头自述投递方 = "每个角色自己的 agent loop"，并写明"**这条投递方目前仍缺**" | 没有生产消费者 → 退场 |
| 2 | `Move()` / `Remove()` / `Restore()` | 只有 `scheduler_test.go`。前端拖拽调序那条手势已在 2026-10-01 退场（面板只读 team 快照）；Go 侧顺序写入口是 `Registry.SetOrder` → `lifecycle` → `Runtime.SyncOrder` → `SetOrder` | 没有生产消费者 → 退场 |
| 3 | `Prefix()`（只读 getter） | 只有 `scheduler_test.go`。前缀对外的读数是 `Runtime.Prefix()` / `Snapshot().Prefix` | 没有生产消费者 → 退场 |
| 4 | `requests chan TurnRequest` 字段、`NewTurnScheduler` 的 `buffer` 参数、`RuntimeOptions.Buffer` | `buffer` 只喂 `requests`；`RuntimeOptions.Buffer` **生产从未设置**（`application/core/agentteam_runtime.go` 的 `teamRuntimeFor` 只给 `RoundLimit` / `NoProgressLimit`） | 随 1 一起死 → 退场 |
| 5 | `orderLocked()` / `indexOfRole()` | 只被 `Move` / `Remove` / `Restore` 调用 | 随 2 一起死 → 退场 |
| 6 | `TurnRequest.RoundID` | 只有 `scheduler_test.go` 构造时给值（生产构造点 `Runtime.peekNext` 不给） | 随 1 一起死 → 退场 |
| 7 | `advanceLocked(roleName)` 的 `roleName` 分支 | 唯一传非空 `roleName` 的调用方是 `Next`（见 1）；`Advance` 恒传 `""` | 分支随 1 一起死 → 简化成无参 `advanceLocked()` |

生产**实际消费**的只有四件（`runtime.go`）：`SetOrder`（`SyncOrder` 整表替换）、
`Order()`（座位存在性 / 投影）、`SetPrefix`（team work 前缀载体）、`sessionsLocked()`
（`Snapshot` 的"下一个谁发言"投射会话号）。

**对上一份清单的一处更正**：`docs/devlog/2026-10-01-m4-deadcode-inventory.md` 的 #3 把
`Advance()` 记成"经 `Runtime.Next` 的生产消费"。按当轮核实**不成立**——`Runtime.Next()` 的调用点
只有 `runtime_test.go` / `prefix_test.go`：没有生产推进者，`peekNext` 的"游标恒为 nil"也正因此
（README 的接线现状段本来就写着"`Next()`/`Advance()` 没有生产消费者"）。本笔**只按清单退场**
（上一份清单点名的四件 + 同证据的 `Move` / `Prefix` / 通道），`Advance` / `Runtime.Next` 留着，
理由见 §3。

## 2. 改法

- `scheduler.go`：删 `Requests` / `Request` / `Next` / `Move` / `Remove` / `Restore` / `Prefix`、
  `requests` 字段、`TurnRequest.RoundID`、`orderLocked` / `indexOfRole`；`advanceLocked` 去掉
  `roleName` 分支（空链表返回 nil，否则前进一格、表尾回表头）。
- `scheduler.go` 文件头重写为"**一份顺序事实 + 一条推进路径**"：写清生产消费的四件、
  退场的四组（含"已退场"字样）与 `Advance` / `Runtime.Next` 的现状。
- `runtime.go`：删 `RuntimeOptions.Buffer` 与 `NewRuntime` 里的 buffer 兜底（通道没了，缓冲无主）。
- `scheduler_test.go`：`TestTurnSchedulerChainsAndAdvances` → `TestTurnSchedulerChainOrderAndPrefixHandoff`，
  只钉留下的那份原语：初始顺序、闭链推进（会话号取自链表绑定）、`SetOrder` 整表替换后下一次推进
  落到新顺序、`skip` 的跳过与"一圈全跳过 = 收束"、前缀交接、空环与 nil 接收者。
- `scheduler_wiring_test.go`：两条**可证伪**断言形状不变，口径改为 `Advance()`（经 `Runtime.Next`）
  没有生产消费者；**新增一条**：退场这件事必须被记下来（README 与 `scheduler.go` 都要出现
  "已退场"）——否则下一个人只会看到一张没人消费的接口表。
- 文档：本包 README 的「接线现状」表 + 「结论口径」段 + 文件结构行；`docs/arch/teamwork-leader-worker-architecture.md`
  §9 的"team 环与逃生"行（原写 `TurnScheduler` **未接线**，与事实不符——它已接线，只是推进路径
  没人消费）与 §11 的 M4 行；清单 #3 行改记退场结果。

## 3. 边界（不假装的地方）

- **`Advance` / `Runtime.Next` 不动**：`Runtime.Next` 是逃生路径 ③`no_executor` / ④`empty_ring` 的
  **唯一计算点**（`stopLocked` 在那里被调用），`Advance` 是它的推进原语。删它们不是"清死代码"，
  是**删行为**（两个已公布到前端 `TeamView.schedule` 的停止原因会变成永不触发）。因此本笔只做
  "同证据、且不改变任何可观察行为"的那一组；`Advance`/`Next` 的取舍等到 team plan 成为唯一顺序
  事实那一笔（它们的行为用例 `runtime_test.go` / `prefix_test.go` 一字未改，继续钉着现状）。
- **顺序编辑只退 Go 侧原语**：`team.set_order`（registry → `lifecycle` → `SyncOrder` → `SetOrder`）
  仍是活路径，环本身仍 live（逃生记账），退场条件是"team plan 成为唯一顺序事实"。
- **前端零改动**：`Snapshot()` / `TeamView.schedule` 的字段一个没动。

## 4. 回归证据

```
go build ./...                                  # exit 0
go vet ./application/... ./seelebridge/... ./tui/... ./gui/... .   # exit 0（含测试编译）
go test ./application/core/agentteam/ -count=1  # ok（含 scheduler_wiring_test.go 三条断言）
go test ./... -count=1                          # 全绿（无 FAIL 包）
python scripts/gen_core_readme_index.py          # 索引随源码注释刷新
```

用例侧的**规格变更**（不是删覆盖）：

- `scheduler_test.go` 旧用例钉的三组形状（channel 投递、顺序编辑、`Prefix()` getter）随对象一起
  退场；留下的那一条只钉存活原语，判据一条不少（顺序 / 会话号绑定 / skip / 收束 / 前缀交接 /
  空环 / nil 接收者）。
- `runtime_test.go` / `prefix_test.go` **未改**：`Runtime.Next`、`Runtime.Prefix`、逃生记账、
  环成员不含 user 全部照旧。
