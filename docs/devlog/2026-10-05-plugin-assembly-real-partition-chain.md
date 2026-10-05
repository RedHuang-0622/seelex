# 按当前 plugin 划分实跑装配链路：真划分链路用例 + 真装配冒烟

> 日期：2026-10-05 · 角色：leader（本轮不做实现改动，只做**用例与实测**）
> 对象：`seelebridge/runtime_role_plugins.go` 的按会话装配链路（上一轮 94f7ae4 落地）
> 交付物：`seelebridge/assembly_chain_real_partition_test.go`（新增，4 用例）+
> `teamwork_plugin_assembly_headless_test.go`（新增，4 阶段真装配冒烟）+
> `teamwork_headless_smoke_test.go`（改：worker 回合判定放宽 + 采样）
> **未提交**（HEAD 仍是 `94f7ae4`；用户没发话就不落 commit）

---

## 0. 一句话结论

按**当前插件划分**（`plugins/` 根下 `default` / `freecad` / `impeccable` 三份 manifest）把装配链路
从"函数对不对"验到"真回合的 system prompt 里有没有"：

1. **装配落到员工面**：装 `impeccable` 的员工回合，system prompt 1308 字节、带它自己的技能目录段
   （含 `cannot switch plugins` 那行口径纠正）；**不装配**的对照 574 字节、无目录段——同一条链、
   同一支团队，只差 `members[].plugins` 这一格。
2. **"不装配 = 默认 `default`" 在工具面成立、在技能目录不成立**（刻意的不对称）：真机回执里
   `inherit-host` 成员的 `plugin_face_tools` = `total_tools` = 57（= 启动基线 `default` 的面，
   它的 include/exclude 皆空），但目录段 0 字节。用户口径里那句"默认 default"要按这两半分着读。
3. **当前划分下"装配"能改变的工具面几乎为零**：`default` 与 `impeccable` 的 `include`/`exclude`
   都是空 ⇒ 装与不装的工具面**逐元素相同**；只有 `freecad` 真收窄（探针面 15→9）。
   ⇒ 现在这一版划分的装配是**能力轴（技能）**，不是权限/工具裁剪轴。
4. **一个必须先知道的事实**：本会话所在的 GUI 进程是 `94f7ae4`（02:23）**之前**的构建
   （实测见 §5 F1）——在它里面 `members[].plugins` 被静默忽略，回执也没有读数。
   要看装配生效，得**重启**到新构建。

---

## 1. 当前划分：三方同一份事实

| 读法 | 结果 |
|---|---|
| 活体 `plugins_list`（本会话进程） | `default`（active，11 skills，include/exclude 缺省）· `freecad`（include 9 条：`switch_plugin,switch_mode,get_time,read_file,grep_search,glob,write*,edit*,bash`；7 skills；MCP `freecad`）· `impeccable`（1 skill） |
| `plugins/curated.yaml:51,60,69` | `entries` = default / freecad / impeccable；`presets` = baseline / cad / design（design 带 7 条 `pending` 路线图） |
| 运行时已定义集合（真划分用例里读 `Manager.Names()`） | 与上面两份逐元素一致（`plugin.ValidateCuratedCatalog` 通过） |

划分本身**没动**（本轮 out of scope）：`freecad` 的 include、`impeccable` 的空 include 都是原样。

---

## 2. 交付物 A：真划分链路用例（`seelebridge/assembly_chain_real_partition_test.go`）

与 `assembly_chain_test.go`（6 用例，用 `cad`/`docs`/`ops` 三个**合成**插件钉链路形状）互补：
本文件把同一条链接到**真插件根**上——`plugin.NewLoader("../plugins")` + `plugin.Manager.Load()`
（生产同一条路）→ `Manager.Activate("default")`（与 `main.go:917` 的 `activateDefaultPlugin` 同一条
启动基线），工具面用**探针注册**（freecad include 名单的内外都有样本，收窄才可判别）。

| 用例 | 钉什么 | 排掉的错 |
|---|---|---|
| `...RealPartitionFreecadNarrowsToolFaceAndAddsSkills` | 探针面 15 → freecad 面 **9**（名单外的 `bash_bg/bash_read/web_search/computer_screenshot/team_plan/job_manage` 被硬拆）；7 份 `cad-*` 全进目录段；default 的技能（`code-aesthetics`/`cli-design`/`teamwork`）一条不来；全局激活态不变 | 装配只进 ctx 没落到工具面；目录段读的是宿主技能；装配动全局单选 |
| `...RealPartitionImpeccableKeepsToolFaceAddsOneSkill` | include/exclude 皆空 ⇒ 可见面与宿主面**逐元素相同**，但目录段 = `impeccable` 那一份（工具面同、能力面不同） | 把"装配"误读成"必然收窄"；也排掉"工具面差异被整体忽略"的退化实现 |
| `...RealPartitionEmptyMeansDefaultBaseline` | ① 启动基线 `ActivePlugin()=="default"`；② 空集面 = 宿主面 = **显式声明 `["default"]` 的面**（这就是"默认 default"的可判定形式）；③ system 字节逐字不变、目录段为空；④ **显式装 `default` 的目录段有那 11 份、空集没有**（不对称显式钉住）；⑤ 阴性对照：freecad 面 ≠ 空集面 | 把"没声明"实现成"装配了空集"；把"默认 default"读成"技能也继承 default" |
| `...RealPartitionReceiptMatchesShippedPartition` | 真根 `curated.yaml` entries ↔ 运行时集合 ↔ 期望三方一致；`team_plan` 回执逐成员读数（freecad：`replace` / skill 7 / 面 < 全量；impeccable：`replace` / skill 1 / 面 = 全量；不装配：`inherit-host` / skill 0 / 面 = 全量且与 impeccable 相同）；真划分上的错别字（`impeccables`）显式拒绝且**不落盘** | 划分三方漂移；回执读数与运行面两套判据；路线图/错别字被静默吞掉 |

`total` 在用例里是**读出来的**（`len(AllTools())`，本基座 28 个 = 15 探针 + 13 个 `team_*`/`jobs_manage`），
只有 `freecad` 的 9 是字面量（来自 manifest 的 include 名单）——这样新增内置工具不会误红。

**证据**

```text
$ gofmt -l seelebridge/assembly_chain_real_partition_test.go     # 无输出
$ go vet ./seelebridge/                                          # 无输出
$ go test ./seelebridge/ -run 'AssemblyChain' -count=1 -v
--- PASS: TestAssemblyChainRealPartitionFreecadNarrowsToolFaceAndAddsSkills (0.02s)
--- PASS: TestAssemblyChainRealPartitionImpeccableKeepsToolFaceAddsOneSkill (0.00s)
--- PASS: TestAssemblyChainRealPartitionEmptyMeansDefaultBaseline (0.01s)
--- PASS: TestAssemblyChainRealPartitionReceiptMatchesShippedPartition (0.01s)
--- PASS: TestAssemblyChainTeammateSeesOwnPluginsToolsAndSkills (0.00s)   ← 上一轮 6 个仍在绿
--- PASS: TestAssemblyChainConcurrentTeammatesDoNotCrossTalk (0.00s)
--- PASS: TestAssemblyChainEmptySetIsByteIdenticalToToday (0.00s)
--- PASS: TestAssemblyChainPendingPluginCannotBeAssembled (0.00s)
--- PASS: TestAssemblyChainRejectionsSpellOutTheReason (0.00s)
--- PASS: TestAssemblyChainReceiptCarriesCatalogReadingsAndYellow (0.00s)
ok  github.com/RedHuang-0622/seelex/seelebridge  0.273s
    assembly_chain_real_partition_test.go:341: 真机读数：全量 28 个工具、宿主（default）面 28 个、
        freecad 面 9 个、impeccable 面 28 个

$ go test ./seelebridge/... ./plugin/ ./skill/ ./seelexctx/ -count=1     # 相关包全量回归
（32 个包全 ok，exit=0；含 seelebridge 26.433s / seelebridge/tools 17.984s / sessionstore 不在本批）
```

---

## 3. 交付物 B：真装配冒烟（`teamwork_plugin_assembly_headless_test.go`）

复用组合根级 harness（`tool_full_chain_test.go` 的 `newFullChainHarnessWithLimits`：真 store /
真 workspace / **真插件根**（同 `pluginRoots()` 责任链 + `initPluginSystem`）/ 真 role session /
真 worktree / 真计划落盘），模型侧用脚本化 provider（**不真调模型**）。装配的落点在 wire 上核：
provider 收下**员工那一轮的请求**，取第一条 `system` 消息原文。

| 阶段 | 探针读数（实测） |
|---|---|
| A1 计划与回执读数 | `上限=3 · ui-eng=replace/[impeccable] 技能=1 目录=585 字节 面=57/57 · plain-eng=inherit-host 面=57/57`；落盘计划里 `Members[0].Plugins=["impeccable"]`、`Members[1].Plugins` 空 |
| A2 装 impeccable 的 teammate | 作业 `a1`（工作项 `wi-ui`）`state=done exit=0`；**ui-eng 的 system prompt 1308 字节，带目录段**（`impeccable` + `cannot switch plugins`），且不含 default 的技能 |
| A3 不装配的对照 | 作业 `a2`（`wi-plain`）`exit=0`；**plain-eng 的 system prompt 574 字节、无目录段** |
| A4 当前划分上的错别字 | `team_plan` 两次调用状态 `[success error]`（第二次 `plugins:["ghost"]` 被显式拒绝）；落盘计划仍是 `plugin-assembly-team`；**拒绝原文在可见会话里不可见**（呈现层换成通用文案，见 F5） |

```text
$ go test . -run 'TestTeamworkPluginAssemblyHeadlessSmoke' -count=1 -v
[teammate-smoke] A1 计划与回执读数   上限=3 · ui-eng=replace/[impeccable] 技能=1 目录=585 字节 面=57/57 · plain-eng=inherit-host 面=57/57
[teammate-smoke] A2 teammate 作业    handle=a1 state=done exit=0 工作项=wi-ui
[teammate-smoke] A2 teammate 作业    handle=a2 state=done exit=0 工作项=wi-plain
[teammate-smoke] A2 装配落到真回合   ui-eng 的 system prompt 1308 字节，带目录段（impeccable + 口径纠正）
[teammate-smoke] A3 不装配的对照     plain-eng 的 system prompt 574 字节，无目录段（默认 default 面、目录不注入）
[teammate-smoke] A4 错别字被拒       team_plan 状态=[success error] · 落盘计划仍是 plugin-assembly-team · 拒绝原文可见=false
--- PASS: TestTeamworkPluginAssemblyHeadlessSmoke (2.33s)
ok  github.com/RedHuang-0622/seelex  2.571s
```

插件根读数也在日志里（证明 harness 走的就是生产那条根链）：

```text
plugin: 根 …\go-build…\plugins=0, …\go-build…\plugins=0, plugins=3（共加载 3 个插件）；
来源 default←plugins\default, freecad←plugins\freecad, impeccable←plugins\impeccable
```

### 3.1 为了跑通这条冒烟，改了两处（都不是产品改动）

1. `teamwork_headless_smoke_test.go`：worker 回合判定从"写死 `以 exec 的身份`"放宽为
   **语义判定**（`<round_input>` + ` 的身份完成这一轮`，组装点是 `runtime_teamwork.go:844` 的
   `workerRoundInput`），并采样本轮 `system` 原文。写死角色名会让**换角色**的冒烟把 worker 回合
   误判成 leader 回合、脚本游标被吃掉。既有 `TestTeamworkHeadlessSmoke` 复跑仍 PASS
   （`worker 回合=2`、P1–P7 全绿）。
2. 本用例自己补一跳 `attachRealSkills`：员工技能目录读的是 `Runtime.skills`
   （`runtime_role_plugins.go` 的 `skillRegistryFor`），组合根在 `main.go:609` 经
   `ApplyDeps(RuntimeDeps{SkillRegistry})` 装上；全链路 harness 只把 registry 交给插件系统与应用、
   **没装上 Runtime**。第一跑就是因为缺这一跳，`skill_count=0 / 目录 0 字节`（A1 断言当场红），
   补上后为 1 / 585 字节。没有去改公共 harness——那会把别的全链路用例的提示词字节一起改掉。

---

## 4. 这一版划分下的装配语义（把"能改什么"说清）

```
工具面 = 权限面 ∩ 插件面                      ← 插件只做减法（相对权限面）
插件面 = 装配集合的 include 并集 / exclude 并集
  default    include=[] exclude=[]  ⇒ 收窄 = 恒等（全量）
  impeccable include=[] exclude=[]  ⇒ 收窄 = 恒等（全量）
  freecad    include=[9 条]         ⇒ 15 探针里只留 9 个（bash_bg/bash_read/… 被硬拆）
技能面 = 装配集合的技能目录段（**只有显式声明才注入**；空集不注入）
```

所以**当前划分**上：

- 装 `impeccable` 与"不装配"的差别**只**在能力面（技能目录段 + 属于它的纪律），工具面完全一样；
- 装 `freecad` 才有工具面收窄（+ 7 份 CAD 技能）；
- `members[].plugins` 不是"权限开关"：它拿不到权限面之外的东西（`seelebridge/tools/policy.go:88-96`
  的收口顺序权限面在前），也**不会**把 MCP 带进来（`plugin/manager.go` 的 `attachLocked` 只在
  全局 `Activate` / `Reload` 路径被调用，装配链只写 ctx）。

---

## 5. 发现（逐条标注证据强度）

**F1【Confirmed，影响你眼下怎么用】本会话所在进程是 `94f7ae4` 之前的构建。**
证据三条：
① 我这次拿到的 `team_plan` 工具 schema 里 `members` 没有 `plugins` 这一格（`runtime_teamwork_schema.go:34-37` 是 94f7ae4 加的）；
② 实测 `team_plan {"members":[{"role":"probe","plugins":["ghost"]}]}` **被受理**（新代码里 `validateMemberPlugins`
会报"插件 `ghost` 未定义（…显式拒绝，不静默忽略）"）⇒ 该进程根本没解析这一格；
③ 同一次回执里**没有** `plugin_limit_per_teammate` / `assemblies`（`runtime_teamwork.go:298-302` 是无条件写的）。
进程侧：`Get-Process seelex-gui` → `Path=…\dist\seelex-gui-dev\seelex-gui.exe`，`StartTime=2026/10/5 0:16:19`，
而 commit `94f7ae4` 的时间是 `2026-10-05 02:23:11`。
⇒ **在这个 GUI 里点不出装配效果**；要验，请重启到新构建（`dist/seelex-gui-dev` 的 exe 已是 02:23 之后那份）。

**F2【Confirmed】"装 vs 不装"的工具面差异在当前划分下为零。**
真机读数：`impeccable` 面 57/57 = `plain` 面 57/57 = `default` 面 57/57（harness 全量 57 个工具）；
真划分用例里 `impeccable` 面也等于宿主面，只有 `freecad` 15→9。根因是两份 manifest 的
`include`/`exclude` 都空（`plugins/default/plugin.md`、`plugins/impeccable/plugin.md`）。
这与上一轮复核 F 的口径一致：**插件只相对权限面收窄，不相对宿主装配**。

**F3【Confirmed】"不装配 = 默认 `default`" 要分两半读。**
工具面：成立（空集早退到 `r.plugins.Filter`，启动基线就是 `default`；用例里还钉了"空集面 == 显式 `["default"]` 面"）。
技能目录：**不成立**（空集不注入目录——那是裁决 3 的刻意不对称）。真装配冒烟里 574 B vs 1308 B 就是这一半。

**F4【Confirmed 事实 + 有据推论】`dist/seelex-gui-dev/plugins/` 的快照是 10/3 的，且缺 `curated.yaml`。**
实测 `ls dist/seelex-gui-dev/plugins` → 只有 `README.md`（1893 B，比仓库那份旧）+ `default/ freecad/ impeccable/`。
新代码启动期从**已解析的插件根**读精选目录（`main.go:initPluginSystem` 的 `resolveCuratedRead`），
读不到会出声（终端 + UI 启动警告）且未定义名一律显式拒绝。⇒ 重启到新构建前，
建议把该包的 `plugins/` 刷新（或 `-plugins <repo>/plugins`），否则会看到那条启动警告。

**F5【Confirmed，继承的旧发现】工具错误的"为什么"在可见会话里被换掉。**
A4 实测 `拒绝原文可见=false`（`team_plan` 第二次调用状态是 `error`，但会话里读不到 `未定义/显式拒绝`）。
既有 `TestTeamworkHeadlessSmoke` 的 P3 记过同一条（"拒绝原因被通用文案替换"）。装配面因此有
"线上看到拒绝、看不到理由"的可用性缺口（本仓库既有口径，非本轮引入）。

**F6【Confirmed】目录面有一个"静默为零"的前置条件。**
员工技能目录读 `Runtime.skills`，只在 `ApplyDeps(SkillRegistry)` 之后存在（`main.go:609` 生产已接）。
缺这一跳时装配回执是 `skill_count=0 / 目录 0 字节`——**看起来像"这个插件没技能"，而不是报"读面没装"**。
本轮的冒烟第一跑就撞上它（见 §3.1）。建议（未做，属他人领地）：装配回执里把"技能目录 actor 未装配"
写成一条读数，别让 0 承担两种含义。

---

## 6. 未验证 / 边界（诚实标注）

- 真装配冒烟用的是**脚本化 provider**：装配落点（system prompt）是真的，模型的"自觉"不是——
  "员工真的按纪律去用这份技能"没验，也不该由这一层验。
- **真模型 + 真 GUI 的端到端仍未做**：需要重启到新构建（F1）。本轮把"真进程"这一半做到了
  （真 Runtime / 真 role session / 真 worktree / 真计划），差的是"真模型 + 真前端"那一半。
- 真划分用例的工具面是**探针注册**（15 个），不是 `RegisterBuiltins` 的全量面；真机全量面的读数
  在冒烟里（57 个工具）。两处数字不同是**基座不同**，不是矛盾。
- 只在 REPO 的 `plugins/` 根上跑过；`dist/*` 的插件快照没有参与（F4 是读出来的事实，不是跑出来的行为）。
- 未改任何产品代码；`git status`：`M teamwork_headless_smoke_test.go`、`?? seelebridge/assembly_chain_real_partition_test.go`、
  `?? teamwork_plugin_assembly_headless_test.go`，HEAD 仍 `94f7ae4`。

---

## 7. 证据索引（哈希 / 命令）

```text
HEAD                                  94f7ae49dc70677186451a554ff5f6e0bec45033
seelebridge/assembly_chain_real_partition_test.go   65b957ab91e934a975f62fe8b1d06d71306dce06
teamwork_plugin_assembly_headless_test.go           d57d00c7f0edb20d7950ef74f33071e3ecab22c9
teamwork_headless_smoke_test.go（改）                ef51872a42140ca2fae999381d4bb57d842f3c37

$ go test ./seelebridge/ -run 'AssemblyChain' -count=1 -v          # 10/10 PASS
$ go test ./seelebridge/... ./plugin/ ./skill/ ./seelexctx/ -count=1   # 32 包全 ok（后台作业 a78，exit=0）
$ go test . -run 'TestTeamworkPluginAssemblyHeadlessSmoke' -count=1 -v  # PASS 2.33s
$ go test . -run 'TestTeamworkHeadlessSmoke$' -count=1 -v               # 既有冒烟仍 PASS（worker 回合=2）
$ gofmt -l <本轮三个文件> ; go vet ./seelebridge/ ; go vet .            # 均无输出
$ ls dist/seelex-gui-dev/plugins  # README.md + default/ freecad/ impeccable/（无 curated.yaml）
$ Get-Process seelex-gui          # StartTime 2026/10/5 0:16:19 · …\dist\seelex-gui-dev\seelex-gui.exe
```
