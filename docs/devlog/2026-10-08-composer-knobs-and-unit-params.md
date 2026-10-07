# Composer 两条独立拉条（推理强度 / 循环次数）+ 工作单元执行参数

日期：2026-10-08
范围：`application/`（core / model / contract 边界）、`gui/`（bridge + 前端）、
`seelebridge/fork`、`seelebridge/teamwork`、`sessionstore`、`docs/gui/modules/effort-control.md`

## 0. 一句话

composer 底部多了**两条独立拉条**：推理强度（wire 词表 `low/medium/high/max`）与循环
次数（连续整数 `1..9999`，**步长恒为 1**）。原有的 Effort 档位拉条退化成"**同时设置
这两条的预设**"。同一套参数在**工作单元**层面也开了口：`fork_subagents` 的每条子代理
可自带 `max_loops`，`team_plan` 的每个成员可自带 `max_turns`。

## 1. 为什么要拆（问题本身）

Effort 档位一个值携带两样东西（`application/prompt/effort.go` 的 `effortProfiles`）：

| 档位 | reasoningEffort（wire） | maxLoops | ReAct 预算 | planPolicy |
|---|---|---|---|---|
| lite | low | 15 | 15 轮 / 30 调用 | Effort=lite |
| medium | medium | 48 | 48 / 96 | +MaxNodes=4, RequireSerial |
| high | high | 384 | 384 / 768 | +MaxNodeLoops=48 |
| max | max | 768 | 768 / 1536 | +MaxNodeLoops=96 |

两条轴本来就走**两条完全不同的链**：循环上限写在**引擎**上，思考强度写在**账号客户
端**上（`contract.ReasoningEffortPort`）。钉在同一档上意味着"384 轮 + low 思考"这种
组合必须改代码。

## 2. 口径：档位是预设，两条拉条各自独立

- 拖一条拉条**只**改它那一项，另一项一个字都不动；
- 切档位 = **同时设置两条拉条**（快照的 `runtime.reasoning_effort` / `loop_limit` 随之
  更新，界面上看得见"档位改了什么"）；
- 因此不再需要"覆盖 vs 未覆盖"的粘滞状态：档位不是第三个事实源，它是一组预设。

新入口（`application/core/service_knobs.go`）：

| 入口 | 落点 | 校验 |
|---|---|---|
| `SetReasoningEffort(ctx, value)` | `ReasoningEffortPort.SetSessionReasoningEffort`（只改"跟随会话"的账号） | provider 词表（**不收** `minimal`？收；**不收** `session`——那是"没有覆盖"的同义词） |
| `SetLoopLimit(ctx, n)` | `Engine.SetMaxLoops` | `1..limits.input_loop_limit`（默认 9999，就是 goal 用的"≈无上限"哨兵） |

**运行守卫与档位同一条**（G0b/INV-G7）：视图会话 running 时三个入口一律拒绝
（`ensureViewIdle`）。前端置灰只是体验，后端才是那道门。

## 3. 最容易踩的坑：读数必须在 runtime 投影里反哺

`Snapshot.Runtime` 每个回合尾 / 每次工具边界都会被**整份重建**
（`view_state.CollectRuntimeProjectionFor` 造一个新的 `model.RuntimeState`，**不带**
`Plan/Account` 之外的旧字段）。所以两条读数各自有一份进程级权威值：

- `serviceState.knobLoopLimit` / `knobReasoningEffort`，由 `knobMu` 保护；
- 投影经 `view_state.Deps.CurrentReasoningEffort` / `CurrentLoopLimit` 取回；
- 装配期（`service_assembler`）播成默认档派生值，否则首个快照里两条拉条会显示
  Low / 1，而引擎实际在跑 high（384）。

加锁次序固定为 **ViewMu → knobMu**（单向）。为什么不用 ViewMu 一把锁：投影的**收集**
在锁外、**应用**在锁内，用同一把锁会有重入风险。

冒烟用例 `TestKnobChainSmoke` 专门盯这一环：拖完两条之后**强制**一次
`collectRuntimeProjection + applyRuntimeProjectionLocked`，读数必须还在。

另有一处被顺手修掉：`reapplyEffortAfterPluginSwitch` 会重放档位（`applyLocked` 里的
`SetMaxLoops(档位默认)`）——用户拧到 600 的循环上限会被这次重放吃掉。现在重放之后补回
"当前生效的循环上限"（`currentLoopLimit()`）。退栈 Skill / 任务边界恢复循环上限的两处
也统一改读 `currentLoopLimit()`，不再直接取档位默认值。

## 4. 前端：滑杆组件复用

`gui/frontend/dist/knob-slider.js` 是三条拉条的**唯一实现**：

- 行为（拖动只预览 / 松手才提交一次 / pending 守护 / 失败回滚到权威值 / 快照覆盖 /
  运行中上锁 / 采纳后端返回的"真正生效的值"）只写一遍；
- 差异只由参数表达：`scale` + `dataKey` + `progressVar`；
- 刻度两族：`discreteScale(values, {labels, progress})` 与
  `linearScale({min,max,step})`（index↔value 双向映射，非法读数返回 `-1` 让调用方忽略
  而不是打回 0）；
- `effort-control.js` 退化成"这份实现 + 一张四档停位表"，**对外 API 与行为不变**——
  原有 `effort-control.test.mjs` 一行未改仍然全绿；
- 两条新拉条在 `composer-knobs.js`，挂载点 `#reasoning-effort-control` /
  `#loop-limit-control`（`index.html` 的 `.composer-knobs` 行，贴在输入框底部）；
- CSS 也复用：直接用 `.effort-control` / `.syringe-*`（含 `styles.css` 12.9 段那条把
  隐形 range 铺满滑轨的归位规则），只补 `.composer-knobs` 的排布与窄屏降级。

## 5. 工作单元执行参数（subagent / teammate）

用户口径是"在二者的基类 workunit 上做参数配置和链路"。落法：

| 层 | 入参（工具面） | 链路 | 状态 |
|---|---|---|---|
| subagent | `fork_subagents` 每条 `max_loops` | `SubagentSpec.MaxLoops` → `buildForkPlan` 的 `node.budget.max_loops` → `node.Coordinator.Budget` → `session.SessionConfig.MaxLoops` | 通 |
| teammate | `team_plan` 每个成员 `max_turns` | `sessionstore.TeamworkMember.MaxTurns` → `WorkerRequest.MaxTurns`（两个派发口：coordinator.Dispatch / items.DispatchItem）→ `roleRoundSpec.MaxLoops` → `session.SessionConfig.MaxLoops` | 通 |

两条都在**工具入口**就判上限（subagent 的判据是**进程级循环天花板**
`limits.input_loop_limit`，与循环次数拉条同一份哨兵；teammate `0 = 没声明`，回退编排层
默认再回退装配默认 `roleTurnMaxLoops`）。工具**描述**同步更新：
`fork.SubagentsContractDescription` 与 `teamworkPlanDescription` 各自讲清这个参数，
schema（`runtime_plan.go` 的 fork 注册 + `runtime_teamwork_schema.go`）同步加字段。

刻意**不做**的一件事：**单元级思考强度**。执行参数必须"说了就真生效"，而思考强度今天
没有单元级落点——账号池是进程级共享的（一个账号一个 ChatClient，由 P2C 租约分配，不按
会话/单元分格），`subagent` / `goalplan` 的账号还各自**钉死**了自己的强度。真要做得先
在账号租约面加"租期内覆盖 + 归还还原"，那是另一件事。把它写进工具 schema 会得到一份
"声明了却不生效"的契约——正是 `workunit` 包一开始要消灭的那种东西。这条留给用户拍板。

## 6. 验收读数

- `go build ./...` / `go vet ./...` 干净；
- `go test ./... -count=1` 全绿（含本轮新增：`knob-slider.test.mjs`、
  `composer-knobs.test.mjs`、`service_knobs_test.go`、`service_knobs_smoke_test.go`、
  `fork/per_subagent_params_test.go`、`teamwork/items_max_turns_test.go`、
  `runtime_teamwork_schema_test.go` 的成员参数用例）；
- 前端 `node --test`（含未改动的 `effort-control.test.mjs` 与 `element-registry.test.mjs`）
  全绿——后者保证新 id 既注册进 `app.js` 又真的在 `index.html` 里。

## 7. 边界

- 两条拉条是**进程级**读数（引擎只有一个、账号池只有一个）。会话化（代码注释里的
  "刀 4"）没做之前，"每会话一份"在界面上写得出来、后端做不到，所以刻意按进程级投影。
- `minimal` 不在推理强度拉条上：它只在部分模型可用（GPT-5 系），摆到界面上等于给大多
  数模型一个会被 provider 拒绝的合法按钮。要回到"跟随档位"，切一次档位即可。
- 循环次数是**线性**刻度（用户明确要求步长 1）：默认值 384 落在 9999 的 3.8% 处，低段
  很密。要改成对数刻度得先确认口径（那会让"步长 1"不再成立）。

## 8. 口径修正：effort 不决定工作单元的循环次数（同日，用户拍板）

用户澄清原意："**设置了 reasoning effort，就不要让 effort 对 subagent / teammate 的
loop 次数造成污染**。"核对三条路径后确认（证据在下表），拉条那条链本来就是干净的，但
fork 子代理那条链不干净——本轮把它拆掉。

| 路径 | 是否改变工作单元的循环次数 | 证据 |
|---|---|---|
| 推理强度拉条 `SetReasoningEffort` | **否**（只写账号池 + 读数） | `application/core/service_knobs.go:87-99` |
| 档位 `SwitchEffort`（切一次档 = 同时设两条拉条） | **曾经会**：写 `SetPlanPolicy(档位策略)`，fork 子代理节点循环预算读的就是它（high=48 / max=96），声明的 `max_loops` 还被它当上限拒 | `application/core/service_interaction.go:123-167`、`seelebridge/fork/tool.go`（旧 `forkNodeLoops`） |
| teammate 回合上限 | **否**（成员的 `max_turns` → 否则 `roleTurnMaxLoops=12` 常量；装配根没给 `TeamworkBackend.MaxTurns`） | `main.go:537-548`、`seelebridge/runtime_teamwork.go:654-658` |

改动（`seelebridge/fork`）：

- 删掉 `forkNodeLoops()` —— 那个"复用 effort 节点循环数、不做独立常量"的读法就是污染源；
- 节点循环预算改为 **"这一条自己声明的 `max_loops` → 否则配置 `limits.plan_node_max_loops`
  （默认 15）"**，两点与档位无关。这也把配置项的名字还给了它自己：`config/seelex.yaml`
  里写的是"子代理节点循环上限（默认 15）"，此前在 high/max 档下被 48/96 悄悄盖掉；
- `max_loops` 的上限判据从"档位节点上限"换成 **进程级循环天花板**
  `limits.input_loop_limit`（拉条同一份哨兵），`validateSubagentLoops` 是这唯一判据；
- `PlanPolicy.MaxNodeLoops` 保留给**主代理自己的 plan_load 节点**（那是 plan 的判据，
  不是工作单元的判据），fork 只继续用它的 `MaxNodes`（数量）与并发推导。

回归钉子（`seelebridge/fork`）：

- `TestForkPlanNodeLoopsIgnoreEffortPolicy`：high(48) / max(96) / medium / lite 四个档位
  下，缺省一律取配置 15——档位换个遍，子代理的次数不变；
- `TestForkPlanNodeLoopsHonourDeclaredOverride`：声明 6 / 60 在两个档位下都是 6 / 60
  （60 越过 high 的 48 不再被拦）；
- `TestValidateSubagentLoopsUsesProcessCeiling`：越天花板与负数拒；60 放行；0 = 没声明；
  天花板未配置（0）不判上限；
- `TestForkToolHandleRejectsSubagentLoopsAboveProcessCeiling`：同一条判据在工具入口生效。

**行为变化要知道**：档位 high/max 下 fork 子代理的缺省循环数由 48/96 变为配置的 15。
需要更多轮的批次请在 `limits.plan_node_max_loops` 上调，或给那条子代理自己写
`max_loops`（模型侧现在也可以直接声明，且不受档位拦）。
