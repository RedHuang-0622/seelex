# backend/limit/loop

## 生态位

`loop` 是 ReAct 循环上限的**唯一判据处**：档位（effort）对应的循环轮数与工具预算、
"会话钉住值 vs 进程默认值"的解析、以及子代理自己声明的循环次数校验，都只在这里各有一份。
它是 limit 板块里最"数字"的一层——没有任何状态、没有 IO，只有判据。

主要调用方：

- `application/prompt`（effort 档位表取这里的读数，`MaxLoops` / `ReActBudgetFor` 转发到此处）；
- `internal/adapters`（会话级引擎端口解析"这次用多少轮"）；
- `seelebridge/fork`（`fork_subagents` 的子代理 `max_loops` 入口校验）。

## 职责与非职责

- 做：登记档位数字、给出预算读数、解析有效上限、校验子代理声明的上限。
- 不做：不持有任何运行期状态（会话钉住值的存储、当前档位、引擎句柄都在消费方）；
  不做配置读取（进程级天花板 `limits.input_loop_limit` 由 `backend/limit` 提供，
  这里只接收它的数值）；不做提示词或 plan 策略（那些在 `application/prompt`）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`loop.go`](loop.go) | `Profile`/`Budget` 类型、档位数字表、`MaxLoops`/`BudgetFor`/`Levels`、`Effective`、`ValidateSubagentMaxLoops` |
| [`loop_test.go`](loop_test.go) | 档位数字的精确钉住、解析口径、子代理校验的边界与错误文案 |

## 核心实现

```text
For(level) → Profile{Level, MaxLoops, Budget}
MaxLoops(level)      → 未识别档位返回 0（不猜档位）
BudgetFor(level)     → 未识别档位兜底 lite（迁移前的既有口径）
Effective(pin, fallback) → pin > 0 ? pin : fallback
ValidateSubagentMaxLoops(id, maxLoops, ceiling) → 负数 / 越天花板各一条错误
```

档位数字（迁移前取自 `application/prompt/effortProfiles`，逐字未改）：

| 档位 | 循环轮数 | 工具预算（轮 / 调用 / 无进展轮） |
|---|---|---|
| `lite` | 15 | 15 / 30 / 6 |
| `medium` | 48 | 48 / 96 / 10 |
| `high` | 384 | 384 / 768 / 24 |
| `max` | 768 | 768 / 1536 / 48 |

刻意保留的既有口径（不是 bug，别"顺手修正"）：

- `Effective` 的钉住值 **`<= 0` 表示"这一格没被钉住"**（撤掉这一格），不是"零轮"；
- 子代理校验里 **`0 = 没声明`**（放行，留给本批默认），只有负数才非法；
- 天花板 **`<= 0 = 不限`**：配置层没给数就不许自造一个；
- 越界判据是 `maxLoops > ceiling`（等于天花板**放行**）；
- 错误文案逐字保留（`max_loops must be >= 1` 这条是对负数说的既有口径）。

`effort 档位的节点上限`（`PlanPolicy.MaxNodeLoops`）**不在这里**：那是主代理自己
`plan_load` 节点的判据，拿来卡子代理等于"改一下 reasoning effort，子代理的次数跟着变"
（2026-10-08 口径）。这条口径由 `seelebridge/fork` 的工具入口用例守着。

## 数据流

```text
config/seelex.yaml limits.input_loop_limit ─┐
                                            ├─→ backend/limit（进程级上限/快照）
界面上的会话驻留窗口/账号窗口 ──────────────┘
                                            │
application/prompt 的档位表 ──→ loop.For(level) ──→ MaxLoops / Budget
                                            │
internal/adapters（会话钉住值）──→ loop.Effective(pin, 进程默认) ──→ engine.SetMaxLoops
                                            │
seelebridge/fork（子代理声明）──→ loop.ValidateSubagentMaxLoops(…, ceiling)
```

## 依赖方向

- 允许：标准库（只有 `fmt`、`strings`）。
- 禁止：`application/*`、`seelebridge/*`、`seelexctx`、`backend/limit` 之外的任何本仓包。
  判据层不许回头依赖上层；消费方反过来依赖它才是对方向。可用
  `go list -deps ./backend/limit/loop` 机械核对（当前非 stdlib 依赖：无）。

## 并发、存储与错误语义

- 本包**无状态、无锁、无 IO**：所有函数都是纯函数，可并发调用。
- 错误语义：`ValidateSubagentMaxLoops` 只返回"声明不合法"的错误（文案即用户可见面），
  不做兜底修正；调用方拿到错误就拒绝这次调用，不静默改成合法值。

## 扩展方式

- 新增档位：在 `profiles` 里加一条，并同步 `application/prompt` 的档位表
  （一个档位一条记录，提示词/planPolicy/reasoningEffort 与循环读数一起登记）；
  两处档位名不一致会被 `application/prompt` 的一致性用例和初始化期检查抓住。
- 新增判据：先在这里加函数 + 用例，再让消费方改调用，不要在消费方保留第二份实现。
- 调整数字：改 `profiles` 与 `loop_test.go` 的期望值（两处必须同改，测试会逼你）。

## Review 指南

- 有没有第二份数字/规则偷偷长出来（消费方又抄了一遍 384、又写了一遍 `pin > 0`）？
- 既有口径有没有被"顺手修正"：钉住值 `<= 0`、子代理 `0 = 未声明`、天花板 `<= 0 = 不限`、
  `> ceiling` 才拒、错误文案逐字。
- 是否误把 `PlanPolicy.MaxNodeLoops` 当成子代理次数的判据（那是另一条口径）。

## 测试与验证

```text
go test ./backend/limit/loop/... -count=1
go test ./application/prompt/... -count=1      # 跨入口一致性用例在这里
go test ./internal/adapters/... ./backend/tool/fork/... -count=1
```

关键用例：`TestTierTableExactValues`（唯一数字表）、`TestEffectiveResolution`、
`TestValidateSubagentMaxLoopsUsesProcessCeiling`、`…MessagesUnchanged`；
`application/prompt` 的 `TestEffortLoopReadingsComeFromSingleJudgment` 是跨实现一致性用例
（同一档位名，两个入口必须给同一判定）。

## 文件与函数索引

### loop.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `normalize` | `func normalize(level string) string` | 档位名解析口径（大小写与首尾空白不敏感） |
| `For` | `func For(level string) (Profile, bool)` | 返回档位的循环读数；未识别档位返回 false |
| `MaxLoops` | `func MaxLoops(level string) int` | 返回档位的循环上限；未识别档位返回 0 |
| `BudgetFor` | `func BudgetFor(level string) Budget` | 返回档位的工具预算；未识别档位兜底 lite |
| `Levels` | `func Levels() []string` | 返回全部已登记档位名（顺序不保证） |
| `Effective` | `func Effective(pinned, fallback int) int` | 会话钉住值优先，否则回退进程默认值 |
| `ValidateSubagentMaxLoops` | `func ValidateSubagentMaxLoops(id string, maxLoops, ceiling int) error` | 校验子代理声明的循环次数（负数、越天花板） |

### loop_test.go

| 测试 | 钉住什么 |
|---|---|
| `TestTierTableExactValues` | 档位数字的唯一期望值 + 两个入口读数一致 |
| `TestUnknownLevelDoesNotGuess` | 未识别档位：`MaxLoops`=0、`BudgetFor` 兜底 lite |
| `TestLevelNameNormalization` | 档位名大小写/空白口径 |
| `TestEffectiveResolution` | 有效上限解析的 5 种输入 |
| `TestValidateSubagentMaxLoopsUsesProcessCeiling` | 子代理校验的 6 个边界（含等于天花板放行） |
| `TestValidateSubagentMaxLoopsMessagesUnchanged` | 两条错误文案逐字不变 |
