# 定时任务的执行参数（思考强度 / 循环次数）+ plan_node_max_loops 默认 384

日期：2026-10-08
范围：`seelexctx` / `config/seelex.yaml`（含 bootseed 资产）、`application/contract/dto`、
`application/core`、`application/prompt`、`seelebridge/{scheduler,fork,node,runtime_plan.go}`、
`internal/adapters`、`gui/frontend/dist`

## 0. 一句话

同一天的第二批：①`limits.plan_node_max_loops` 的默认值 **15 → 384**（上一批解耦之后，
它是"子代理缺省跑多少轮"的唯一来源）；②**定时任务可以声明它那一次运行的两条执行参数
——思考强度与循环次数——缺省落成默认档位的两条读数**，两条各自走原来那条链的按会话落点：
次数钉在触发那个会话的引擎上，强度在该会话的回合起点按会话下发一次。

## 1. `plan_node_max_loops` 默认 15 → 384

**为什么调**：2026-10-08 的第一批（[composer-knobs-and-unit-params.md](./2026-10-08-composer-knobs-and-unit-params.md)）
把"子代理缺省轮数"的唯一来源还给了这条配置（档位不再参与）。还回来之后它的默认值仍是解耦
之前的 15——而 high/max 档下子代理本来跑 48/96，15 是偏小的。**384 = 默认档位 high 的循环
次数**：于是"没声明 `max_loops` 的子代理"与"主会话默认档位"落在同一量级，配置项的名字
（子代理节点循环上限）与它给出的量级也终于对得上。

改了哪些地方（默认值一处 + 注释/断言同步，不留旧数字）：

| 文件 | 改动 |
| --- | --- |
| `seelexctx/limits.go` | `DefaultLimits().PlanNodeMaxLoops` 15 → **384**；字段注释补"（默认 384）" |
| `config/seelex.yaml`、`internal/bootseed/assets/config/seelex.yaml` | `plan_node_max_loops: 384`，注释写明"默认 384；与默认档位 high 的循环次数一致" |
| `seelexctx/limits_test.go` | 默认值断言 15 → 384 |
| `seelebridge/fork/tool.go`、`types.go` | 注释里的"默认 15" → "默认 384" |
| `seelebridge/node/coordinator.go` | `Budget` 的注释同上 |
| `seelebridge/runtime_plan.go` | fork schema 里 `max_loops` 的描述同上（模型读到的是这句） |
| `seelebridge/agent_node_test.go` | `TestNodeBudgetPrefersNodeInput` 的缺省期望值 15 → 384 |

逃生口不变：单条子代理可以自己写 `max_loops`（上限 = 进程级天花板 `limits.input_loop_limit`），
或直接上调这条配置。

## 2. 定时任务的两条执行参数

### 2.1 契约、默认参数与它们住哪儿

- 契约加两个字段：`dto.ScheduledTaskSpec.ReasoningEffort`（provider wire 词表）与
  `.LoopLimit`（轮数）；快照加 `reasoning_effort` / `loop_limit`（面板数据源）。
  任务定义是整份 spec 落进全局 JSONL 的，所以这两个字段**随定义持久化**，重启原样回来。
- **空 / 0 = 未声明**。默认参数 = **默认档位的那两条读数**：`prompt.DefaultEffortLevel`
  （新常量，值 `high`，`NewEffortManager` 也改用它）经 `ReasoningEffortFor` / `MaxLoops`
  读出来，即 wire `high` + `384` 轮。数字与词表因此仍只有 `effortProfiles` 一份，
  别处不再抄一遍。
- 默认在**创建/编辑那一刻就落进定义**（权限档位"空 = 默认 full"的同一套口径）：面板上读到
  的值就是触发时真正要用的值。为什么不选"缺省跟随当前档位/拉条"——定时任务在后台跑，
  它的行为不该随用户此刻把拉条拖到哪儿而漂移；落进定义则跨重启也稳定。
- 分工：**契约层只判不落**（`seelebridge/scheduler` 归一 + 校验：词表走
  `dto.NormalizeWireReasoningEffort`，负次数显式拒；默认值不在这里，因为档位表属于
  application/prompt，不属于调度器这一域）；**默认值由装配层落**（application/core 的
  `scheduledRunParams` 是"声明优先 → 否则默认"的唯一实现，创建/编辑入口与触发入口共用，
  于是冷启动读回的老定义（没有这两格）在触发时按同一份判据兜得住）。

### 2.2 两条落点：为什么必须是两条不同的机制

**循环次数 → 触发那个会话自己的引擎。** 进程级那一格（`EngineBase.SetMaxLoops`，就是
composer"循环次数"拉条的读数）是**所有会话共用的默认值**，一次触发不能顺手改掉用户正在用的
会话。所以这一格按会话住在引擎端口上：

- 新窄端口 `contract.SessionLoopLimitPort`（`SetSessionMaxLoopsFor(sessionID, loops)`），
  实现是 `internal/adapters.EnginePort`（端口本来就按会话持有引擎）；
- 钉值落在那一个会话上，该会话的引擎**重建**（历史替换 / 冷装载）后继续生效；用户在会话里
  主动改循环次数 = 对该会话的显式选择，**撤掉钉值**（否则那次选择会在引擎重建时被钉值盖掉）；
  会话引擎卸载（LRU 驱逐 / 卸载）时钉值随引擎离场——端口对已卸载的会话不留状态；
- 应用侧把它当**硬依赖**：宿主给不出这一格时显式报错，不静默退回进程默认值（那正是
  "面板说 48 轮、实际按 384 轮跑"的读数与行为相反）。

**思考强度 → 按会话记一笔租约，在该会话的回合起点下发一次。** 账号池是**进程级共享**的：
一条 wire 值落在 `*api.ChatClient` 上，没有"这个会话用自己的强度"这种格子。于是：

- 装配时（`AssembleScheduledRun`）把任务声明的那条按会话记一笔（`borrowSessionReasoningEffort`）；
- 该会话的**回合起点**取走并下发一次（`syncSessionReasoningEffortFor`，与
  `syncPlanPolicyFor` / `syncFullAccessFor` 同一个同步点），**下发即消掉**——它属于那一次
  运行，不是这个会话的长期设置；用户随后在该会话拖拉条、切档位照旧生效（不会出现"拉条被
  一个看不见的东西压住"）；
- 顺带修的一件事：在此之前，只有"切档位 / 拖拉条"会下发强度，**别的会话的回合起点不会按
  自己该用的那条下发**。现在每个回合起点都按本会话的读数下发（普通会话 = 拉条读数；空值
  不下发，"没选过"保持 provider 默认）。账号池"后切者生效"因此不再跨回合遗留成
  "用着用着变了强度"。

### 2.3 前端

- 弹窗加两格：思考强度（下拉）与循环次数（数字框）。下拉的停位与文案**复用 composer 那条
  拉条的同一份清单**（`REASONING_EFFORT_LEVELS` / 新导出的 `REASONING_EFFORT_LABELS`）——
  拉条与任务声明停的是同一组 wire 值，界面不该有第二套词。
- **留空 = 不声明**，前端不替后端猜默认值（判据与默认值都在后端一份；`normalizeLoopLimit`
  只做"数字控件也可能是任意字符串"这一层清洗）。
- 表格的装配 chip 多一段执行参数读数（`强度 high · 循环 384`）；编辑腿把这两格原样带回表单。

### 2.4 删除 / 替换清单（同一件事不留第二份实现）

| 原来的第二份 | 现在的唯一实现 |
| --- | --- |
| `application/core.validWireReasoningEffort` 的 switch 字面量 | 委托 `dto.NormalizeWireReasoningEffort`（判据本体进契约层；拉条只额外补"不收空值"一条） |
| `application/core.reasoningEffortChoices()` 的清单字面量 | `dto.WireReasoningEffortVocabulary()` |
| `SetLoopLimit` 内联的区间判断 | `validateLoopLimit`（拉条与定时任务共用同一份） |
| `engine_port.go` 五处 `port.maxLoops > 0 { SetMaxLoops(...) }` | `loopsForLocked(sessionID)`（"这个会话该用多少轮"的唯一读法） |
| `NewEffortManager` 里的 `current: "high"` 字面量 | `prompt.DefaultEffortLevel`（默认来源一处） |

## 3. 回归钉子

- `seelexctx.TestLoadLimitsDefaults`：默认值断言（含 `PlanNodeMaxLoops == 384`）。
- `seelebridge.TestNodeBudgetPrefersNodeInput`：节点预算缺省回退 384。
- `seelebridge/scheduler.TestScheduledTaskRunParamsNormalization`：空/0 = 未声明（调度器不落
  默认值）；声明值归一进定义与快照；未识别强度（含 `session`）与负次数显式拒。
- `seelebridge/scheduler.TestScheduledTaskAssemblySurvivesRestart`：两条执行参数随定义持久化，
  冷启动恢复；没声明的任务恢复后仍是"未声明"。
- `application/core.TestScheduledRunParamsDefaultsAndValidation`：创建时缺省落成 `(high, 384)`；
  声明值不被默认盖掉；非法值当场拒且不惊动调度器。
- `application/core.TestAssembleScheduledRunAppliesTierAndPlugins`：装配时次数钉到那次会话
  （别的会话不受影响）、强度记一笔且**只被取走一次**；未声明的裸 spec 按默认档位兜底。
- `application/core.TestKnobChainSmoke`：全链路冒烟里补一条——回合起点按会话下发一次强度
  （下发序列 `[max low low]`），被拒的调用一个字都不下发。
- `internal/adapters.TestEnginePortPinsLoopsPerSession` / `TestEnginePortSessionLoopPinLifecycle`：
  钉值只落在那个会话（别的会话拿进程默认值）、引擎重建后仍在、用户显式改次数撤掉钉值、
  卸载随引擎离场。
- `scheduled-tasks-view.test.mjs`：载荷带上两格 / 留空即未声明 / `normalizeLoopLimit` 的清洗 /
  chip 渲染 / 编辑来回。

## 4. 行为变化要知道 + 已知边界

- **fork 子代理缺省轮数 15 → 384**（§1）。要少就在单条子代理上写 `max_loops`，或下调配置。
- 每个回合起点都会按会话下发一次思考强度（跟随会话的账号各一次 `SetReasoningEffort`，开销
  极轻；空读数不下发）。这是"账号池进程级共享"下唯一能保证"每个回合按自己的声明跑"的位置。
- 定时任务新建的会话把**它自己的**循环次数带在身边：面板上看到的（任务的 `loop_limit`）与
  那次会话真正跑的上限一致；该会话引擎被卸载后重开按进程默认值跑。
- **仍未按会话分的那一格**：账号池的 wire 强度是进程级的。这一笔下发之后、下一个会话在自己
  的回合起点说自己的强度之前，别的会话的请求也走这条强度。要彻底按会话分，需要 Seele 侧
  提供按会话的 ChatClient（未做）——本批只把"每个回合起点按自己的声明下发一次"这件事做实。
- 老定时任务（定义里没有这两格）：触发时按默认档位兜底（行为正确），面板上暂不显示 chip；
  编辑保存一次即落成显式值（快照带 `reasoning_effort` / `loop_limit`）。
