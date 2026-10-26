# effort

## 生态位

`kernel/limit/effort/` 是 **effort 档位策略的唯一判据处**：每个档位对应哪条
reasoning 强度、哪组循环读数、哪份 plan 约束，以及"未声明执行参数时用哪一档"。
主要调用方：`application/prompt`（宿主侧 `EffortManager`）、`application/core`
（定时任务执行参数默认值）与 wiring 的档位装配。

## 职责与非职责

- 做：档位策略表、默认档位、`ReasoningEffortFor`、`PlanningPolicy`、`Levels`、
  `DefaultRunParams`。
- 不做：不驱动 PromptStack、不调用引擎、不读配置。`EffortManager` 的应用动作
  留在 `application/prompt`。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `effort.go` | 策略表与四个取数入口（含定时任务默认参数） |
| `loop_consistency_test.go` | 与 `kernel/limit/loop` 的跨实现一致性 |
| `reasoning_effort_test.go` | 档位 → provider 词表映射 + 每档必填强度 |

## 核心实现

- `Profile`：一条档位一条记录（`Level` / `Loops` / `SubagentPolicy` / `ReasoningEffort`），
  防止新增档位只拿到其中一半。
- `For` / `ReasoningEffortFor` / `PlanningPolicy` / `Levels` / `OrderedLevels`。
- `DefaultRunParams`：定时任务的"声明优先、未声明落默认"唯一判据；只有 0 算
  未声明，负值留给校验层显式拒绝。

## 数据流

调用方按档位名查 `Profile` → 取 reasoning 强度 / 循环读数 / plan 约束；
循环数字本身来自 `kernel/limit/loop`，本包不抄数字。

## 依赖方向

`effort → loop + contract/dto`（+ stdlib）。禁止 import `application/*`、
`backend/engine/*` 或 `store`。

## 并发与安全

策略表是包级只读数据，构造后不再写；并发读安全。

## 扩展方式

新增档位：在 `profiles` 加一条 + 在 `loop` 的档位表加同名条目 + 决定是否加入
`OrderedLevels`。两表不一致由一致性用例在测试期打红。

## Review 指南

- 是否有人又在 prompt/core 里抄了一份数字或 reasoning 词表；
- 新档位是否同时有循环读数与 plan 约束；
- 默认参数是否仍只在 `DefaultRunParams` 一处。

## 测试与验证

```text
go test ./kernel/limit/effort/ -count=1
go test ./application/prompt/ -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### effort.go

- `func mustLoopProfile(level string) loop.Profile`
- `func For(level string) (Profile, bool)` — For 返回档位策略；未识别返回 false（不猜档位）。
- `func ReasoningEffortFor(level string) string` — ReasoningEffortFor 返回 provider wire 词表里的思考强度；未识别返回空串
- `func SubagentPolicyFor(level string) dto.SubagentPolicy` — SubagentPolicyFor 返回子代理执行的硬约束；未识别兜底 lite（迁移前口径）。
- `func Levels() []string` — Levels 返回全部已登记档位（顺序不保证；需要顺序用 OrderedLevels）。
- `func DefaultRunParams(reasoningEffort string, loopLimit int) (string, int)` — DefaultRunParams 把一次定时任务的执行参数落成显式值：声明优先，未声明

### loop_consistency_test.go

- `func TestEffortLoopReadingsComeFromSingleJudgment(t *testing.T)` — TestEffortLoopReadingsComeFromSingleJudgment 是跨实现一致性用例：档位策略表

### reasoning_effort_test.go

- `func TestReasoningEffortForMapsLevelsToWireVocabulary(t *testing.T)` — TestReasoningEffortForMapsLevelsToWireVocabulary 钉住"档位 → provider 词表"的
- `func TestEffortProfilesCarryReasoningEffort(t *testing.T)` — TestEffortProfilesCarryReasoningEffort 钉住"每一档都必须有思考强度"：

