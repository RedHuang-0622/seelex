# 定时任务：修掉「提交就报解析错」、给周期加开始时间锚点、把勾选框画成人样

- 日期：2026-10-07
- 起因（用户报告，三件事一起给）：
  1. 「error parsing arguments: parsing time "" as "2006-01-02T15:04:05Z07:00": cannot parse "" as "2006"」；
  2. 「你打开新建定时任务那个面板看看，那个创建后立即使用的按钮是人写的吗？」；
  3. 「以及需要做出链路的真 api 冒烟，周期可以是 1 分钟看看，同时任务类型这个可以去掉了，
     只需要提示词的就够了。时间可以说是一天的几点开始的周期，同样的一周的星期几开始的
     几点开始的也可以添加限制条件，如果没明说那么需要勾选一个每个周期的当前时间」。
- 结论：三条都在**同一批**里落地——① 载荷契约收进视图模块并把 `runAt` 写成 `null`；
  ② 勾选框改回原生控件 + `accent-color`（无窗口量测给出修前/修后尺寸）；③ 弹窗只留提示词，
  周期加「开始时间 / 每周星期几」锚点，不给锚点就必须勾「每个周期按当前时间」，
  另加 1 分钟周期（冒烟与自测能用）。真 API 链路冒烟见 §6。

---

## 1. 现场一：建任务必报解析错（根因不在 Go）

报错原文 `error parsing arguments: parsing time "" …` 里的 `error parsing arguments` 前缀
只有一处会写：Wails 的调用分发器（`vendor/github.com/wailsapp/wails/v2/internal/frontend/
dispatcher/calls.go:41` → `BoundMethod.ParseArgs`）。它把前端 `invoke(name, args…)` 的参数
交给 `encoding/json` 反序列化到方法签名上的 Go 类型。我们的签名是
`Bridge.ScheduleTask(spec seelebridge.ScheduledTaskSpec)`，而 DTO 里 `RunAt` 是 `time.Time`：

```go
// gui/frontend/dist/app.js（修前）
runAt = "";                       // 周期模式：留空
const spec = { …, runAt, … };     // 空串照样发出去
```

`time.Time` 的 `UnmarshalJSON` 只认 RFC3339 或 `null`，`""` 当场报
`parsing time "" as "2006-01-02T15:04:05Z07:00"`。**Go 侧一行都执行不到**，所以调度器、
application、seelebridge 全都无辜——错误发生在第 0 步（参数入栈）。

这条同时解释了两件事：为什么错误文本里带着 Go 的时间布局串（用户看到的不是我们写的任何文案），
以及为什么"周期/一次性两种模式"里只有周期模式炸（一次性模式恰好发了 RFC3339）。

## 2. 现场二：那个勾选框（无窗口取证）

`_tmp/sched-panel/` 是这次的量测台：起一个本地静态服务器 → 用 `git show HEAD:…` 取**修前**的
`index.html` / `styles.css`，与工作区当前文件各拼一个预览页（同一份量测脚本）→
`chrome --headless=new --dump-dom` 拿读数、`--screenshot` 留图（全程无窗口，不碰桌面）。
标记直接抽自 `index.html` 的 `#scheduled-task-modal`，不复制不改写。

「创建后立即启用」这一行，修前/修后（1000×1000，device-scale-factor=1，动效关掉）：

| 判据 | 修前（HEAD） | 修后 |
|---|---|---|
| 行宽 / 行高 | **120.3 × 54** px | **454 × 18** px（同卡片里其它 `.settings-field` 一致） |
| 文案行数 | **3 行**（「创建后」/「立即启」/「用」，每行 36px 宽） | **1 行**（84px） |
| 勾选框 | **67.3 × 32** px，`appearance: none`，1px `#e7e7e4` 边框，底 `#f5f4f0` | **15 × 15** px，`appearance: auto`，`accent-color: #1f2328` |
| 勾的样子 | `:checked` 的 `background-image` 是 Pico 的 `--pico-icon-checkbox`——**白色** SVG，`background-size: .75em`，画在 `--code-bg` 浅底上：勾在不在都看不出来 | 截图里量到 15×15 的实心块（`(31,35,40)`）+ 白色勾；未选是白底灰框 |
| 全库 checkbox 数量 | `index.html` 里 `type="checkbox"` **只有这一个** | 两个（新增「每个周期按当前时间」） |

根因三条，都能在 CSS 里对上：

1. **行被压成内容宽**：Pico 写了 `label:has([type="checkbox"], [type="radio"]) { width: fit-content }`
   （(0,1,1)）。`.settings-field.sched-toggle` 只有 `display: flex`，压不过它。
2. **勾选框长成文本输入框**：Pico 的 `[type=checkbox] { width: 1.25em; height: 1.25em; appearance: none }`
   被 Seelex 自己的 `.settings-field input { width: 100%; padding: 9px 10px; border; border-radius;
   background: var(--code-bg) }`（(0,1,1)）+ `.app-shell .settings-field input { min-height: 32px }`
   （(0,2,1)）盖掉；旧的那条 `.sched-toggle input { width: auto }` 与 `.settings-field input`
   **同权重且更靠前**，从来没生效过（顺序决定胜负）。
3. **勾看不见**：`[type=checkbox]:checked { background-image: var(--pico-icon-checkbox) }` (0,2,0)
   虽然赢了 `background` 简写（逐属性比），但那是一张给"填色方块"配的**白**勾，落在 `--code-bg`
   的浅底上等于没画。

所以那个控件的真实身份是：**全库唯一的 checkbox，套着文本输入框的皮，勾用一张看不见的白 SVG**。
答案是"不是人写的"——是没人看过的默认拼装。

修法（`styles.css`）：行钉回整宽（`.settings-field.sched-toggle { … width: auto }`，(0,2,0) 压
`label:has` 的 (0,1,1)）；勾选框改回**原生控件 + `accent-color: var(--accent)`**，勾交给浏览器画——
两种皮肤（light/dark 的 `--accent`/`--on-accent` 正好相反）都清楚，也不依赖某张固定颜色的 SVG。

## 3. 改法：弹窗只剩提示词，周期带锚点

| 处 | 修前 | 修后 |
|---|---|---|
| `任务类型` 下拉 | `command` / `prompt` 二选一（命令类在本机根本没有白名单项，见 §7） | **删除**（类型恒为 prompt） |
| `命令` 下拉 | 白名单键选择 | **删除** |
| `提示词内容` | 随类型切换显隐 | **常驻** |
| 周期单位 | hour / day / week / month | 加 **minute**（1 分钟粒度能在一次冒烟里看到一次真实触发） |
| 周期锚点 | 无（周期 = 创建时刻起算） | `开始时间`（`HH:MM`）+ `周周期` 多一个 `星期几`；不给就**必须**勾 `每个周期按当前时间` |
| 表内周期文案 | `每 1 天` | `每 1 天 09:00` / `每 1 周 周一 09:00` / `每 2 天（按创建时间）` |
| 载荷组装 | `app.js` 就地拼对象 | 收到 `scheduled-tasks-view.js` 的 `buildScheduledTaskSpec()`（纯函数，node 用例钉得住） |

锚点语义（`seelebridge/scheduler`）：**锚点算"严格晚于 now 的第一个锚点时刻"**，所以
「每天 09:00」在 08:00 建 → 今天 09:00，在 10:00 建 → 明天 09:00；周锚点同理
（「每周一 09:00」在周三建 → 下周一 09:00）；月锚点保留原日历语义（同日 + 月末钳制）。
不给锚点 = 老口径：`now + 周期`，即"每个周期按当前时间"。

锚点与周期的搭配**显式拒绝**，不静默降级（静默吞掉锚点会让「每天 09:00」悄悄变成
「创建时刻起每 24 小时」，用户看不到任何提示）：

| 组合 | 结果 |
|---|---|
| minute / hour + 开始时间 | 拒绝（子日周期没有"几点开始"） |
| 固定间隔（Interval）+ 开始时间或星期 | 拒绝 |
| day / month + 星期 | 拒绝（星期只归周周期） |
| 周 + 星期但没给开始时间 | 拒绝 |
| 一次性任务（RunAt）+ 开始时间或星期 | 拒绝（它的时刻就是 RunAt，锚点只会被无声忽略） |
| 开始时间不是 `HH:MM`（`9点半` / `25:00` / `09:60` / `09:00:00`） | 拒绝 |

`9:00` 这类**短写被接受并归一**成 `09:00`（Go 的 `time.Parse("15:04")` 本就收紧放），
快照里只留一种写法。

## 4. 载荷契约（这次的真根因，单独一条）

```
前端字段 → buildScheduledTaskSpec → Bridge.ScheduleTask(spec) → application → seelebridge
                                  ↑ Wails 用 encoding/json 反序列化，runAt 是 time.Time
```

- 周期模式：`runAt: null`（JSON `null` 对 `time.Time` 是 no-op；空串是**致命**的）。
- 一次性模式：`runAt: "2026-10-08T09:30:00.000Z"`（RFC3339）。
- 其余锚点字段是 string/int，空值天然安全（`startClock: ""`、`startWeekday: 0`）。

钉子两头都有：前端（`scheduled-tasks-view.test.mjs` 断言 `spec.runAt === null` 且序列化里
不出现 `"runAt":""`）+ Go（`gui/bridge_test.go` 的 `TestScheduledTaskRunAtWireContract`
用同一次 `json.Unmarshal` 证明 `null` 过、`""` 炸）。

## 5. 钉子与门禁

| 处 | 内容 |
|---|---|
| `seelebridge/scheduler/scheduler_test.go` | `TestNextScheduledAtAnchoredPeriods`（15 例表驱动，`now` 钉在 2026-10-07 星期三，期望写成字面日历）+ `TestScheduledAnchorContractValidation`（11 种非法搭配）+ `TestScheduledAnchoredTaskCarriesAnchor`（锚点进快照 + 简写归一） |
| `gui/bridge_test.go` | `TestEmbeddedScheduledFormPromptOnly`（弹窗不许再有类型/命令控件，锚点三件必须齐）、`TestEmbeddedScheduledPayloadDelegatedToView`（`runAt: null` 在视图模块里、`app.js` 不得自己拼 `runAt:`）、`TestScheduledTaskRunAtWireContract`、`TestEmbeddedScheduledToggleHasOwnSkin`（勾选框自己的皮 + 旧规则不许回来） |
| `gui/frontend/dist/scheduled-tasks-view.test.mjs` | 5 条载荷用例（日/周/分钟/一次性/非法输入）+ 1 条锚点文案用例 |
| 真机（无窗口） | `_tmp/sched-panel/`：修前/修后同页量测 + 截图 + 像素核对（勾选框是实心块还是白底灰框） |

> 顺带修掉：`manualsmoke` 这个 opt-in 标签**整块编译不过**（具名枚举改造后没人再编过它）——
> `job_backfill_live_smoke_test.go` 与 `manual_smoke_test.go` 里三处拿无类型字符串比枚举，
> 以及 `initPluginSystem` 少接一个返回值。真 API 冒烟要跑就得先让这族文件编译，改动都是
> 一行的类型口径对齐（`dto.AsyncStateDone` / `model.PlanCompleted` / `Status.String()`）。

## 6. 真 API 链路冒烟（1 分钟周期）

`TestManualSmokeRealAccountScheduledPromptChain`（`scheduled_task_live_smoke_test.go`，
`manualsmoke` 标签，opt-in）：GUI `Bridge`（前端 `invoke("ScheduleTask", spec)` 的同一落点）
创建 → application 端口 → 调度器排期 → 注入的 prompt 执行器（`main.scheduledPromptExecutor`，
测试基座装的是同一份）→ 真实 provider 一轮对话 → 回到快照/面板数据源。

判据（全部来自真实 wire 行为）：创建即排期（`interval_seconds=60`、`next_run_at ≈ now+60s`、
面板数据源里看得到）→ 一个周期后真的触发（`run_count>=1`、`last_status=ok`）→ 提示词真的进了
会话并被模型回答 → 跑完后 `next_run_at` 晚于 `last_run_at`（周期在滚）→ 取消后从面板消失。
1 分钟是调度器最小周期（30s）之上、能在一次冒烟里看到"触发 + 真实回答"的最小粒度；
日历锚点（每天 09:00 / 每周一 09:00）不可能在冒烟里等一天，交给 §5 的表驱动用例。

```
$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
go test -tags manualsmoke . -run TestManualSmokeRealAccountScheduledPromptChain -count=1 -v -timeout=8m
```

## 7. 未修（如实记，不擅自扩口径）

1. **命令白名单仍然只有 `auto_get_jobs` 一项，且 dev 包里为空**（`resolveAutoGetJobsDir` 按
   CWD / exe 目录找脚本，dev 包两处都落空）。这次把**弹窗入口**收成只做提示词任务，但
   `scheduler` 的 command 分支、白名单登记与相关用例**原样保留**——它们是 API 级能力，
   删掉属于另一个决定。若确定"命令类任务不要了"，下一批可以整块摘掉
   （`RegisterScheduledCommand` + `runCommand` + `main.registerScheduledTaskCapability`）。
2. **定时任务仍是纯进程内状态**（重启即清空），本批未改。
3. `_tmp/sched-panel/` 是这次的量测台（含 `styles-under-test.css` 等中间产物），不入库。
