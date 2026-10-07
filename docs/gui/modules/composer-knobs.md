# composer 运行参数拉条（推理强度 / 循环次数）详细设计

> 状态：已实现
> 总体架构：[`../architecture.md`](../architecture.md)

## 1. 目标与边界

composer 底部有**两条独立拉条**：推理强度（"想多深"）与循环次数（"跑多少轮"）。
每条只改自己那一项，另一项一个字都不动。它们负责取值展示、拖动预览、提交状态、
失败回滚、可访问性与视觉反馈；不定义 wire 词表、循环上界或各档策略。

业务真值仍在 Core：`Service.SetReasoningEffort` / `Service.SetLoopLimit`，
调用路径 `Bridge.SetReasoningEffort` / `Bridge.SetLoopLimit →
Service.*`（`gui/bridge.go`、`application/core/service_knobs.go`）。
前端只有一次操作中的临时预览状态，下一份 Runtime Snapshot 始终可以覆盖它。

主要实现：

- 语义挂载点：`gui/frontend/dist/index.html` 的 `.composer-knobs` 行里
  `id="reasoning-effort-control"` / `id="loop-limit-control"`（composer，独立于 Runtime modal）；
- 组件：`gui/frontend/dist/knob-slider.js`（滑杆行为的**唯一实现**）；
- 两条拉条的参数化：`gui/frontend/dist/composer-knobs.js`；
- 装配与 Bridge 端口：`gui/frontend/dist/app.js`（`createComposerKnobs`）；
- 视觉状态：`gui/frontend/dist/styles.css` 的 `.effort-control` / `.syringe-*` 段
  与 `.composer-knobs` 段；
- 命中区归位：`styles.css` 的 `12.9 Pico 的 role 组件归位` 段（见第 6 节）。

## 2. 两条拉条的取值模型

| 拉条 | 挂载点 | 停位表 | 值域 | 落点 | 后端入口 |
|---|---|---|---|---|---|
| 推理强度 | `#reasoning-effort-control` | `discreteScale(["low","medium","high","max"])` | wire 词表四档（不含 `minimal`） | 账号池思考强度（wire `reasoning_effort`） | `SetReasoningEffort` |
| 循环次数 | `#loop-limit-control` | `linearScale({min:1,max:9999,step:1})` | 连续整数 `1..9999`，步长恒为 1 | 引擎单回合循环上限 / 账号池 | `SetLoopLimit` |

range 的 `value` 一律是 **index** 而不是值本身：两条停位表的长度不同，只有统一成
"位置 0..N 均匀"才能共用同一份命中和渲染逻辑；`value ↔ index` 的双向映射由停位表
负责（`indexOf` 对越界/非整数/不在步长上的读数返回 `-1`，调用方据此忽略不可信读数，
例如冷加载中的空 runtime）。

循环次数的上界是 `limits.input_loop_limit`（默认 9999）：它就是 goal skill 用的
"≈无上限"哨兵，拉条不允许推得比它更远——否则那个哨兵不再表达"无上限"。

## 3. 交互状态机

实现位置：`gui/frontend/dist/knob-slider.js`。

```text
Committed(value)
  ├─ input(index)  ──→ Preview(index)          只更新视觉，不调用 Bridge
  ├─ change(same)  ──→ Committed(value)        无远程调用
  └─ change(new)   ───→ Pending(value)
                          ├─ resolve ─→ Committed(后端返回的生效值)
                          └─ reject  ─→ Committed(old) + onError

Runtime Snapshot ──→ set(readout) ──→ Committed(value)    （值不可识别时忽略，不打回 0）
```

选择 `change` 而不是每次 `input` 调用 Bridge：拖动期间连发请求会把引擎与 prompt 栈
打成一串中间态。Pending 期间 input 被禁用并显示等待光标，重复的 change 由 pending
守卫忽略。

## 4. 权威状态与失败恢复

`app.js` 注入两个端口（`setReasoningEffort` / `setLoopLimit`），端口依次调用 Bridge
和 Snapshot refresh，并返回后端**真正生效的值**；Controller 以返回值渲染，而不是
假定请求值生效（后端可能拒绝或归一化，UI 必须显示真的那个）。

`renderRuntime` 每次收到全量 Snapshot 都调用 `setReasoningEffort` /
`setLoopLimit`，因此 `/effort` 命令、TUI 或未来远端控制导致的变化也能同步到拉条。
Controller 不持久化业务状态。

## 5. 视觉状态与动效克制

控件是一枚紧凑滑轨（起点标签 + 细轨填充 + 终点标签 + 数值），视觉原则与全局设计
系统一致：克制、无装饰：

- 每条只切换填充色：推理强度 `low` 绿 / `medium` 青 / `high` 琥珀 / `max` 紫；
  循环次数走默认色；不使用渐变、辉光、流光或呼吸动画；
- 已移除针筒玻璃反光、刻度点与 `effort-max-aura`，不产生常驻 GPU 动画；
- `--effort-progress` 与 `data-effort` 是 Controller 的派生视图，不影响提交逻辑；
- `prefers-reduced-motion: reduce` 关闭全部过渡与动画，颜色状态保留。

色值与进度映射只允许出现在 `:root` 的语义色 token 与两张停位表中，组件不得硬编码。

## 6. 布局与可访问性

- 两条拉条都在 composer，独立于 Runtime modal；嵌入资源契约在
  `gui/bridge_test.go` 的嵌入布局用例里固定（两条拉条都先于 `id="runtime-modal"`，
  modal 内不得出现它们的 range），并同时钉住"档位滑杆不许再回到界面上"
  （`id="effort-control"` 归零）。
- **列数必须等于拉条根元素的直接子元素个数**：每个拉条 5 个子元素
  （标签 / 起点 / 滑轨 / 终点 / 数值），`.composer-knobs .knob-slider` 因此必须声明
  5 列；列数少一个，多出来的子元素会被自动排到第二行，滑轨落进 `auto` 列后宽度塌成
  0 —— 界面上只剩两行小字，**既看不到滑轨也没有可拖的命中区**（2026-10-08 的现场，
  见 `bridge_test.go::TestEmbeddedComposerKnobGridMatchesChildren`）。
- 780px 以下两端标签 `display: none`，子元素回到 3 个（标签 / 滑轨 / 数值），
  列数必须一起从 5 收回到 3。
- 实测（无头 Chrome 1440x900，真实 `index.html` + 注入的自报几何脚本）：5 列时两条拉条
  都是一行 29px、轨宽 **281px / 286px**，隐形 range 与 `.effort-syringe` 同起同止
  （`365,729,281x29`），轨内 20% / 50% 处 `elementFromPoint` 都命中 range；把列数改回 3
  列后滑轨只剩 **2px**（贴在最右）、终点标签与数值被挤到第 2 行（y 762 vs 729）。
- **命中区 = 显示区**：`.syringe-input` 是铺满 `.syringe-barrel` 的绝对定位覆盖层
  （`inset: 0` / `100%`），活塞的百分比也按同一条轨的量。这条覆盖关系必须由
  `styles.css` 的 12.9 段显式压过 `vendor/pico.min.css`：Pico 把 `[role=group]` 当
  "相邻输入组"，对组里的 input 写 `position:relative; flex:1 1 auto`（0-2-1）与
  `margin-left:-1px`（0-3-1），而 `.syringe-input` 只有 0-1-0。压不过的后果不是
  "手感差一点"：隐形 range 会变成排在滑轨**右侧**的 flex 项（只占滑轨一半宽，实测
  1440x900：输入盒 `[961.7,987.1]` vs 滑轨 `[936.3,962.7]`）——可见滑轨整条点不到，
  唯一能拖的感应带落在滑轨右侧，于是"拖动的位置总比填充偏右"。改这条链时必须同时
  复核：输入盒与滑轨盒同起同止、轨内任意处单击都能改成对应的值。
- 命中尺与填充尺是同一把（实测档位边界 17.1% / 50.6% / 86%，理想 16.7% / 50% /
  83.3%）。离散停位拖动时活塞只在停位上落位，因此**停位中心处偏差为 0、一条停位带内
  最大偏差 = 带宽的一半**（轨道 50.83px 时实测全轨最大 10.8px）；那是离散吸附的固有
  量，不是错位。要让填充沿始终贴着鼠标，只能改成连续填充或延长轨道（改前请先确认口径）。
- 每个拉条 `role=group` + `aria-label` 描述用途；range 用 `aria-valuetext` 暴露可读
  值（`Low/Medium/High/Max` 或整数），而不是裸 index。
- 数值文本由 Controller 同步（`#reasoning-effort-value` / `#loop-limit-value`），
  取值因此不只靠滑轨长度表达；颜色走 `--effort-tone`。
- 键盘可使用方向键和 Home/End 操作原生 range。

## 7. 错误与边界策略

| 场景 | 处理 |
|------|------|
| 快照读数未知/空 | 忽略（`indexOf` 返回 `-1`），不打回 0，也不发请求 |
| 拖动但未松开 | 只预览，不修改 Core |
| 重复选择当前值 | 直接恢复 committed 视图，不调用 Bridge |
| Bridge 尚未就绪/调用失败 | 回滚到最后一份权威值、解除 disabled、统一错误回调 |
| 提交中收到重复 change | pending guard 忽略 |
| 视图会话 running | 后端按 G0b/INV-G7 拒绝；前端 `setEnabled(false)` 置灰并给出原因 |
| 用户要求减少动态效果 | 全局 `prefers-reduced-motion` 关闭过渡与动画，颜色状态保留 |

## 8. 自动化证据

- `knob-slider.test.mjs`：线性刻度的 index↔value 与非法读数、四档停位、预览/提交一次、
  采纳权威值、失败回滚、运行中上锁、快照覆盖不打回 0、构造期 range 量纲；
- `composer-knobs.test.mjs`：拖一条不动另一条、快照把两条一起铺下、运行中一起上锁、
  后端归一化时按返回值渲染；
- `gui/bridge_test.go::TestEmbeddedComposerKnobGridMatchesChildren`：列数 == 子元素数
  （宽 5 / 窄 3）；把列数改回 3 即红——这正是"看不到也拖不到"的那个现场；
- `gui/bridge_test.go::TestEmbeddedKnobSliderHitAreaOverlayOnRail`：隐形 range 的归位
  规则（0-4-1）与它压过的 Pico 规则、覆盖层几何仍写在 `.syringe-input` 自己身上、标记
  仍是 `input.syringe-input`；删掉归位规则即红；
- `gui/bridge_test.go` 嵌入布局用例：两条拉条都在 modal 之外、`id="effort-control"` 归零；
- `application/core/service_knobs_test.go`：各改一项、范围校验、词表校验、运行中拒绝且
  不触碰引擎/账号池；
- `application/core/service_knobs_smoke_test.go`（**全链路冒烟**）：装配期播种 → 拖循环
  次数 → 拖推理强度 → **强制一次 runtime 投影重建**（读数必须还在）→ 切档位 = 两条一起
  落 → 档位重放不吃掉用户值 → 运行中被拒；
- `application/core/service_interaction_guard_test.go::TestEffortCommandUsesGuardedServicePath`：
  `/effort` 命令必须走 `Service.SwitchEffort`（同一道运行守卫）；
- `gui/bridge_test.go`：Bridge 对 `SetReasoningEffort` / `SetLoopLimit` / `SwitchEffort`
  的透传（含整数参数类型）。

第 6 节的几何数字来自一次性探针，方法：静态服务 `gui/frontend/dist` + Bridge mock，
无头 Chrome/Edge 经 CDP 连续拖动扫过整条滑轨，逐步读回 `input.value` 与活塞
`getBoundingClientRect()`。它**不进仓库也不进 CI**（CI 无浏览器）；需要复核 12.9 的
归位或改动滑轨结构时照上面这四步重跑即可。

## 9. 审查清单

- 两条拉条是否仍各自只改一项（拖一条不许动另一条）？
- `input` 是否保持纯预览，只有 `change` 才调用 Bridge？
- 失败是否回滚到最后一份权威 committed 状态？成功后是否按后端返回值渲染？
- 读数是否反哺 runtime 投影（不反哺 = 下一次投影把拉条打回默认值）？
- 循环次数上界是否仍与 `limits.input_loop_limit` 同源？
- 隐形 range 是否仍是**铺满滑轨**的覆盖层（改动 §6 的 12.9 归位规则后，轨内任意处
  单击都要能改值；Pico 的 `[role=group]` 会把它排成滑轨右侧的 flex 项）？
- `.composer-knobs .knob-slider` 的列数是否仍等于拉条的子元素个数（宽 5 / 窄 3）？
- 视觉是否保持克制（无辉光/流光/常驻动画）并支持 reduced-motion？
- 两条拉条是否仍可在不打开任何 modal 的情况下看到和操作？

## 10. 档位（Effort 等级）为什么不再有界面控件

Effort 档位（`lite / medium / high / max`）一次同时改写 prompt 层、plan 策略与
ReAct 预算三项，是**多件事的预设**，不是"某一项读数"。把它做成 composer 的一条
滑杆，界面上就出现"一个滑杆同时控制思考强度和循环次数"的错觉，而它又和两条独立
读数拉条共享同一片空间与同一道运行守卫——2026-10-08 用户报告"前端有且只有一个同时
控制思考强度和循环次数的滑杆"，说的就是这一条。

口径（2026-10-08 用户裁决）：**档位从 composer 下线，不再有界面入口**；它只经
`/effort` 命令切换（CLI/TUI，`application/core/command.go` → `Service.SwitchEffort`，
同一道 G0b/INV-G7 运行守卫），界面上的运行参数只剩那两条各自独立的拉条。
切档位仍会把两条读数一起带进该档（快照里 `runtime.reasoning_effort` /
`runtime.loop_limit` 随之更新），所以两条拉条会跟着动——但那是"命令改了读数"，
不是"界面又长出一条能同时改两项的滑杆"。

`Bridge.SwitchEffort` 与 `Service.SwitchEffort` 都保留：前者仍是 GUI 侧命令面板/
远端控制的入口，后者是命令与 TUI 的唯一路径；下线的只是那条滑杆。
