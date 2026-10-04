# 压缩处厚摘要：缺省即开 + 帧兜底措辞改认「推帧失败」这条事实

- 日期：2026-10-04
- 范围：`seelexctx/limits.go`(+`limits_test.go`)、
  `application/core/context_runtime/compaction_frame.go`(+`compaction_frame_test.go`、
  该模块 README 的自动索引)、`config/seelex.yaml`（+`internal/bootseed/assets/config/`
  同步副本）、`config/README.md`、`seelebridge/runtime_compaction_switch_test.go`（注释）。
  历史留痕（CHANGELOG、docs/devlog、docs/2026-*、docs/research）不改。
- 现场（用户原话）：
  1. 「本次没有模型生成的读后感：压缩摘要开关未开启，压缩摘要开关怎么开启」；
  2. 「设置成默认开启的」。

## 1. 现场证据：同一帧里两句话互相矛盾

用户读到的那句话出自**压缩帧正文**（`compactionFrameBody` 的「压缩材料」段）。同一帧的
JSON 元数据块里，`readback.note` 写的是另一件事：

```text
"readback": { "note": "本次没有压缩栈帧（推帧失败：compaction index: 会话上下文存储未绑定
（压缩栈不可用）），因此没有 read_compressed_turn 入口；原始轮次仍在会话存储里，可用
search_history 检索。" }

## 压缩材料 (Folded Material)
（本次没有模型生成的读后感：压缩摘要开关未开启，或前缀重放失败已回退本地压缩。…）
```

证据位置：dev GUI 数据根里那条帧回读记录
（`dist/seelex-gui-dev/.seelex/sessions-json/**/big_tool_result/*.result.json`，
帧 `at: 2026-10-04T00:58:51+08:00` / `reason: context_budget_autonomous` / `injected: true`）。

**两句话指的是两种不同的东西**：`readback.note` 说的是**接线**（那一跳没有可用的会话
上下文存储，帧没推成），Markdown 正文说的是**配置**（开关未开启 / 前缀重放失败）。
而配置那一侧当时并不成立：该实例真正读的配置（`main.go` 的 `runtimeConfigChain`——
`<CWD>/config/seelex.yaml` → `<CWD>/seelex.yaml` → `<exe>/config/seelex.yaml`，存在即读）
里 `limits.context_compaction_summary.enabled` **一直是 `true`**（2026-09-29 起出厂档就是
打开）。所以"压缩摘要开关未开启"是**兜底措辞**，不是读数。

这句话的来路是 `localCompactReason()`：只在"这次没有模型读后感"且**帧里没有任何降级
原因**时输出。它的前半句把一个并不存在的配置事故当成了默认解释，于是用户按它去查一个
不存在的开关。

## 2. 两件事，两种处置

### 2.1 帧兜底措辞：先认帧里已经记着的事实

`localCompactReason()` 现在按**证据强度**依次取解释：

| 顺序 | 依据 | 输出 |
|---|---|---|
| 1 | `SummaryNote`（压缩 DAG 记下的开关状态 / 重放失败的真实报错） | 原样带出（不变） |
| 2 | `IndexError`（推帧失败的真实错误） | `推帧失败（<err>），本次压缩没有留下可读的栈帧。` |
| 3 | `IndexSkipped`（索引面就绪、这次没有可归档区间） | `本次压缩没有折出任何完整协议单元，没有留下可读的栈帧。` |
| 4 | 都没有（更早版本写的帧、也没推帧事实） | 保留原兜底措辞（口径由既有用例钉住，不编原因） |

第 2、3 条原本就在同一帧里（`readback.note` 用的就是它），只是 Markdown 一半没用上——
"同一事实两处不换口径"在这次是**反着**破的。

### 2.2 开关缺省语义：缺省即开（用户口径）

`limits.context_compaction_summary` 的缺省（整块缺失、或块在但没写 `enabled` 键）从
2026-10-04 起按**打开**处理，与出厂档一致；**只有显式 `enabled: false` 才关**（这一位
就是回滚臂）。判定落在配置入口而不是字段默认：

- `LoadLimits`：`limits` 段的 YAML 键就在手边，`compactionSummaryDefaultsOpen` 只回答
  "写没写这个键"（`enabled` 键在 = 显式值，取值仍由 `Decode` 负责，不复制取值逻辑）；
- `DefaultLimits`：连配置文件都没有时同样按缺省（开）；
- `WithDefaults` **不补这一位**：补了就把 `enabled: false` 吞掉，回滚臂失效；
- 直接构造 `Limits{}` 的宿主/测试保持"零值 = 关"的旧行为。

为什么缺省放"开"一侧：关掉时帧退化成"元数据投影 + 一句本地兜底措辞"，而这次退化的
现场极难归因（本文 §1 就是）。这与 `async_exec` 的"缺省 = 关"**刻意相反**，因为那边
关闭时能力根本不可实施、拒绝是安全侧；两处口径的理由都写在各自的结构体注释里。

**代价（如实）**：配置里没写这一块的实例，升级后会在每次装配层压缩多一次**无人值守的
付费调用**；出厂档与显式值都不变，回滚是 `enabled: false` 一个键。

## 3. 先红后绿

| 用例 | 改前（红） | 改后 |
|---|---|---|
| `TestCompactionFrameBodyBlamesTheRecordedPushFailure`（`context_runtime`） | 帧 JSON 写着 `推帧失败：…会话上下文存储未绑定…`，正文仍断言开关问题 | 正文带出真实推帧失败原因、不再出现"开关未开启"；`IndexSkipped` 一臂同理；`readback.note` 口径不变 |
| `TestLimitsCompactionSummaryDefaultsOpen`（`seelexctx`） | 缺 limits 段 / 缺整块 / 块在但没写 `enabled` / 空块 四臂都读到 `false`，`DefaultLimits()` 也是 `false` | 四臂都读到 `true`；显式 `false` 仍为 `false`、显式 `true` 为 `true`；显式关不带动其它字段 |

两条各自先跑出红灯再改代码（见 §5 的复跑命令）。既有口径用例一枚未删：兜底措辞那条
（`TestCompactionFrameBodyWritesWhyNoModelSummary` 的"没有原因时保留兜底措辞"）继续钉住
第 4 种情形；`TestCompactionSummarySwitchClosedKeepsLocalCompact` 继续钉住字段层
"关就是关"；`TestShippedCompactionSummarySwitchShipsOpen` 继续钉住出厂档那一行。

## 4. 回归证据

```text
gofmt -l application/core/context_runtime seelexctx seelebridge internal/bootseed
        → 改动文件干净（其余 seelebridge/teamwork 文件的既有漂移不在本轮范围）
go build ./...                     → exit 0
go test ./seelexctx/... ./application/core/context_runtime/... -count=1   → 全绿
go test ./seelebridge/ -run 'TestCompactionSummarySwitch|TestShippedCompactionSummarySwitchShipsOpen|TestCompactionFrame' -count=1 → ok
go test ./internal/bootseed/ -count=1 → ok（内嵌默认档与 config/seelex.yaml 逐字节相同）
go test ./... -count=1 -timeout=300s → 全绿：63 个包 ok、无 FAIL（exit=0；含 application/core、
                            seelebridge、seelexctx、sessionstore、workspace、gui、tui、mcpstack）
```

复跑红灯的命令（想自己验一遍）：

```text
go test ./application/core/context_runtime/ -run TestCompactionFrameBodyBlamesTheRecordedPushFailure -count=1
go test ./seelexctx/ -run TestLimitsCompactionSummaryDefaultsOpen -count=1
```

## 5. 未做 / 边界（如实）

1. **那一帧真正没有读后感的根因本轮没修**：`readback.note` 已经把成因指出来了——
   推帧失败的原因是"会话上下文存储未绑定"。生产接线里 `AttachSessionContextStore` 只在
   会话激活路径上被调用（`internal/adapters/session_workspace_ports.go`），哪些会话/哪条
   链路会走到"没绑定"（teammate 会话？headless 冒烟？冷恢复？）**本轮未定位**。本轮只做
   到"帧如实自答"，不假装那条接线已经修好。要接着查，入口是
   `seelebridge/runtime_compaction_index.go` 的 `PushCompactionFrame`（错误原文）与
   `CompactionSummaryAvailable` / `ReadbackCompactionSummary`（同一条 store 依赖）。
2. **配置改动对运行中的实例不生效**：`main.go` 的 `initRuntime` 启动期读一次配置；改完
   `enabled` 必须重启进程。用户现场那台 dev GUI 本来就读到 `true`，因此这条不构成他们
   遇到的症状。
3. **模块索引生成器的既有漂移**（不是本轮引入）：`scripts/gen_core_readme_index.py` 在当前
   HEAD 上会重写 `application/core/context_runtime` 与 `application/core/task_context` 两份
   README 的索引（context_runtime 那份含 `fold_history.go` → `session_history.go` 的旧名），
   并在最后的分卷覆盖自检报两个未归属文件
   （`teamwork_board_session_switch_test.go`、`teamwork_completion_trigger_test.go`）后退出
   非零。本轮只保留 `context_runtime` 的重生成（新增用例要进索引），把 `task_context` 那行
   无关漂移回退，避免混入无关改动。
4. **提交**：按仓库规范不自动提交；改动留在工作区等用户点头。
