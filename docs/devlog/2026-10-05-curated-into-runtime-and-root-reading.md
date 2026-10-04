# 精选目录进运行期 + 根读数进 UI 面（A/B 修复）

> 日期：2026-10-05 · 角色：shipper（work item `wi-fix-wire` / milestone `m-fix`）
> 一轮修复，两个缺口（复核实读结论）：
> **A** 运行期没人读 `plugins/curated.yaml`（`LoadCuratedFromRoot` / `ActivateFromCatalog` 当时只出现在测试里）；
> **B** 根读数（`logPluginRoots`）只写终端日志，UI 面（`main.go` 的 `startupWarnings` 通路）里没有它。

## 1. 改了什么（file:line）

### A · 精选目录：从"声明面"变成"装配面"

| 位置 | 内容 |
|---|---|
| `plugin/curated.go:287` | `CuratedRead`：一次读数的**完整**形状（Catalog + 实读页 `Path` + 找过的根 `Roots` + `Err`）。为什么不是只交 `CuratedCatalog`：拒绝文案要落到一页可核的文件上，读不到时要说清找过哪些根。 |
| `plugin/curated.go:299` | `Page()`：实读页（或找过的根）。 |
| `plugin/curated.go:321` | `Judge(name, installed)`：名字判定的**唯一文案生产者**。四分支：目录没读到 → 拒并说清找过哪些根；在 `entries` 里而进程没有它 → 拒（名字漂了/被撤过）；在 `presets[].pending` 里 → 拒并点名 pending + 上游来源 + 实读页 + 证据档（复用 `pendingRejection`）；谁都不认识 → 拒并写清"不在已装插件、也不在精选目录"，两边名单都写。 |
| `main.go:236` | `resolveCuratedRead(roots, loaded)`：**启动时**从已解析的插件根读一次。多根 first-wins（链上第一个带 `curated.yaml` 的根说话；坏的那份当场报，不退让到下一个根）；交叉校验用**本次真正加载出来**的插件名（多根并集）；缺失/解析失败留成 `Err`（绝不当空目录）。 |
| `main.go:262` | `curatedAssemblyJudge(read)`：把读数包成判决函数（判定与文案全在 `CuratedRead.Judge`，这里不复制）。 |
| `main.go:881` | `initPluginSystem`：读一次 → `runtime.SetPluginUnassembledReason(...)` 接进装配校验；读不到时终端出声 + 返回启动警告（见 B）。 |
| `seelebridge/plugin/plugin.go:48/120/137/147/161` | 桥这一侧只掌握"本进程定义了哪些名字"：`Names()` / `SetUnassembledReason` / `HasUnassembledReason` / `UnassembledReason`（注入 nil = 回落既有口径；持锁不调外部函数）。 |
| `seelebridge/ports.go:658` | `Runtime.SetPluginUnassembledReason`（插件域端口；不改任何已装插件的 include/exclude）。 |
| `seelebridge/runtime_teamwork_curated.go:33` | `rejectUnassembledMemberPlugins`：逐成员判**未定义**的名字；**已装插件不问**（`Defined` 为真即原路径）；超限成员跳过（语法先于语义）。 |
| `seelebridge/runtime_teamwork.go:287` | 校验入口：在 `validateMemberPlugins` **之前**调用上面的闸门——早于 `coordinatorFor`/`SetPlan`，所以被拒的计划不落盘。 |

### B · 根读数进 UI 面

| 位置 | 内容 |
|---|---|
| `main.go:183` | `pluginRootReport(roots, loaded)`：**一条读数**——责任链每个根各供了几个（0 也写出来）+ 每个插件的来源根（多根 first-wins 下只算胜出的那个）。 |
| `main.go:220` | `logPluginRoots`:返回同一条读数的字节（不再自己拼一份），写终端日志。 |
| `main.go:480-484` | `run()`：同一条读数进 UI 面（`app.AddNotice`）；精选目录读不到时另发一条 `⚠ 启动配置警告`。**不是"广播"**：它是"启动读数"，终端与 UI 两处同源同字节。 |

## 2. 两条测试（各自独立，都有判别力）

1. `plugin_curated_runtime_test.go`（根包，A 的读侧 + B 的读数）：
   `TestResolveCuratedReadReadsFromResolvedRootsAndJudges`（实读页 / pending 文案 / 两边名单 / entries 有而进程没有）、
   `TestResolveCuratedReadMissIsLoud`（缺失与解析失败都不得当空目录，且点名找过的根）、
   `TestResolveCuratedReadFirstRootWins`（链首说话；链首坏了当场报）、
   `TestResolveCuratedReadCrossValidatesLoadedSet`（目录列了未落盘的插件 → 红）、
   `TestPluginRootReadingIsSharedAndReachesTheUIFace`（根读数逐根计数 + first-wins 来源；`app.AddNotice` 通路）。
2. `seelebridge/runtime_teamwork_curated_test.go`（运行期链，B 的入口）：
   `TestTeamPlanRejectsPendingPluginWithSource`（**pending 那条：必拒 + 点名来源 + 实读页 + 不落盘**）、
   `TestTeamPlanRejectsUnknownPluginAgainstBothLists`、
   `TestTeamPlanRejectsWhenCuratedCatalogIsUnreadable`、
   `TestTeamPlanKeepsInstalledPluginsOnTheOldPath`（判决函数**零调用**：已装走原路径，计划照落）、
   `TestTeamPlanWithoutCuratedJudgeKeepsTheOldMessage`（未注入 = 既有"未定义"口径）、
   `TestTeamPlanGrammarOutranksCuratedSemantics`（超限报上限，不被 pending 抢话）。
   两条"不落盘"断言走 `assemblyChainPlanStore` 的 `empty`（读回落盘面，而不是只信 err）。

## 3. 命令输出（本轮实测）

```
> go test ./seelebridge/... ./e2e/ ./plugin/... -count=1
ok  github.com/RedHuang-0622/seelex/seelebridge        36.182s
ok  github.com/RedHuang-0622/seelex/seelebridge/teamwork  2.641s
ok  github.com/RedHuang-0622/seelex/e2e                 0.914s
ok  github.com/RedHuang-0622/seelex/plugin              1.425s
（其余 seelebridge 子包全 ok）

> go test ./plugin/... ./e2e/ . -count=1        # 交付要求的那条
ok  github.com/RedHuang-0622/seelex/plugin  2.037s
ok  github.com/RedHuang-0622/seelex/e2e     1.776s
ok  github.com/RedHuang-0622/seelex        36.861s

> go build ./...
（无输出，退出码 0）
```

## 4. 顺手修掉的邻域（必须说明）

`initPluginSystem` 多返回一个启动读数 ⇒ 三个既有测试调用点同步加一个 `_`：
`plugin_self_tools_test.go:77`、`repro_hot_attach_background_tool_order_test.go:68`、`tool_full_chain_test.go:246`。
另：`main.go` 的 `teamPlanHandler` 里有一段**逐字重复**的注释（同一个"两道显式拒绝"写了两遍）已删掉一份。

## 5. 未做到 / 未验证（诚实标注）

- **未做 `e2e` 级"启动 → UI 面真有这条通知"的实测**：UI 面的断言是"读数进了 `AddNotice` 调用点 + 读数只有一处生成"（源码级守卫，`git grep` 可核），没有起 GUI 去看通知。
- **未验证 `plugin.Manager` 之外的第二条装配入口**：`AssemblePreset` / `ActivateFromCatalog`（切插件那条全局单选路径）走的仍是 `plugin` 包自己的拒绝文案，本轮没有把 `CuratedRead.Judge` 的"实读页"口径并进去——两处文案因此不完全同形。
- **`installed` 名单的时点**：判决文案里的"本进程已定义插件"取的是**调用时刻**的定义表（`plugins_reload` 后会变），而目录读数取的是启动时刻的 entries；两者漂移时 `Judge` 的第 2 分支（entries 有而进程没有）会报出来，但"目录 entries 陈旧"本身没有单独的读数。
- **`-race` 已跑**（本轮改动没有新增跨调用的共享可变状态；`Manager.unassembled` 与其它字段同锁、启动期写一次）：
  `go test -race ./seelebridge/plugin/ ./plugin/ -count=1` → `ok`；
  `go test -race ./seelebridge/ -run "TestTeamPlan|TestAssemblyChain" -count=1` → `ok 1.700s`。
