# 对抗复核 C / D / G 三条「本特性自定失败模式」的修复与证据（2026-10-05）

工作项：`wi-*`「修复：读数同判据 / 重复显式拒 / 三跳钉子」（里程碑 `m-fix`，团队
`teamwork-*`）。三条缺陷全部由**对抗复核**提出，file:line 已逐条自行核实（不采信转述），
下面每条给「修前复现 → 修法 → 修后证据 → 判别力」。

## 0. file:line 清单（修前 → 修后）

| # | 修前的位置 | 事实 | 修后 |
|---|---|---|---|
| C | `seelebridge/runtime_role_plugins.go:250-266`（`assemblyViews`） | `len(plugins) > 0` 才算读数 ⇒ `inherit-host` 成员报 `0/0` | 读数与运行面共用 `pluginFaceJudgement` |
| C | `seelebridge/runtime_role_plugins.go:255` `defs, _ := r.pluginDefs(...)` | 吞掉 `missing` + `seelebridge/plugin/plugin.go:174` 对空 defs 返回 `true` ⇒ 插件被撤销后**回执报满面、运行面为空** | 失灵独立成第 3 种形态（`Faulted`），读数 0 + `plugin_face_note` |
| C | `seelebridge/runtime.go:418-433`（`PolicyDeps.PluginFace`） | 收口自己算一遍（与读数两套判据） | 收口与读数同判据（`pluginFaceJudgement(...).face(tools)`） |
| C | `seelebridge/runtime_teamwork.go:305`（`teamDispatchHandler`） | 派发回执**没有**装配读数 ⇒ 失灵不可观察（plan 的回执表达不了它：未知名在 plan 那一步已被拒） | 派发回执带 `plugin_face`（失灵时说清「声明 X，现已失灵，工具面为空」） |
| D | `application/contract/dto/plugin_assembly.go:34`（`NormalizePlugins`） | 静默去重 ⇒ 唯一生效口径是「静默合并」 | 重复**显式拒绝** |
| D | `sessionstore/teamwork.go:288` 附近 | `_ = pluginNames` 死代码；那条「重复（显式拒绝，不静默去重）」**不可达** | 去死代码；两处口径一致（本层是落盘前最后一道） |
| D | `application/core/agentteam/spec.go:118` | 写入侧同样静默去重（与 plan 自述相反） | 同上（共用 `dto.NormalizePlugins`） |
| D | `seelebridge/tools/role_plugins.go:22`、`runtime_role_plugins.go`（`normalizePluginNames`）、`runtime_teamwork_curated.go` 注释 | 把读侧/传输侧的去重写成"口径" | 注释写清「声明侧的重复在入口被拒，这里只是规范化已校验的事实」 |
| G | `seelebridge/teamwork/coordinator.go:160` | `Dispatch` 载荷的 `Plugins` **零用例** | `teamwork/items_plugins_test.go` 钉住载荷 |
| G | `seelebridge/teamwork/items.go:374` | `DispatchItem` 载荷的 `Plugins` **零用例** | 同上 |
| G | `seelebridge/runtime_teamwork.go:629` | `workerRoleRoundSpec` 的 `Plugins: request.Plugins` **零用例** | `runtime_role_plugins_fault_test.go` 端到端钉住（到本轮 ctx + 工具面） |
| F | `seelebridge/runtime_role_plugins.go:18-20` / `runtime_teamwork_schema.go:36` | 「插件只能收窄工具面」口径不准 | 改成「只相对**权限面**成立，不相对**宿主装配**」 |

## 1. C：读数与运行事实同一判据

### 修前复现（临时探针，跑在修复前的代码上）

```
① inherit-host 读数 PluginFaceTools=0 TotalTools=0；运行面=[cad_draw]（1 个）
② 撤销前 读数=2 运行面=2
② 撤销后 读数 PluginFaceTools=5 TotalTools=5；运行面=[]（0 个）
```

第 ② 行就是复核说的那条：**回执报满面（5/5）、运行面为空（0）**——两套判据给出的
结论相反。第 ① 行是 `inherit-host` 报 `0/0` 的占位。

### 修法

`pluginFaceJudgement`（`runtime_role_plugins.go`）成为**唯一**判据，三种形态：

- `Defs` 有值 → 按集合收窄（`plugin.Face` / `plugin.VisibleName`）；
- `Defs` 为空 → 一律可见（「没装配」与「宿主没激活插件」同解，与旧 `Filter` 逐条等价：
  单元素集合下 `plugin.Face([activeDef], tools)` ≡ `Manager.Filter(tools)`）；
- `Faulted` → **失灵**（声明还在、定义没了）：运行面返回 `nil`，读数记 0 并带
  `plugin_face_missing` / `plugin_face_note`。

运行面（`runtime.go` 的 `PluginFace`）与读数（`assemblyViews`）都走它；顺带删掉了
那个吞 `missing` 的 `pluginDefs`。

失灵**可观察**的落点是**派发回执**（`team_dispatch` 的 `plugin_face`）：`team_plan`
的回执永远表达不了失灵（未知名在那一步就被显式拒绝），而失灵的后果落在派发之后的运行面。

### 修后（同一个探针）

```
① inherit-host 读数 PluginFaceTools=1 TotalTools=5；运行面=[cad_draw]（1 个）
② 撤销前 读数=2 运行面=2
② 撤销后 读数 PluginFaceTools=0 TotalTools=5；运行面=[]（0 个）
```

## 2. D：重复声明显式拒绝（口径统一到一处）

### 修前复现

```
重复声明的 team_plan：err=<nil> 计划未落盘=false      ← 被受理（静默合并）
RoleSpec 重复声明：err=<nil> plugins=[cad docs]        ← 静默合并
```

而 `sessionstore.ValidateTeamworkPlan` 里那条「插件 %q 重复（显式拒绝，不静默去重）」
**不可达**（上游已经把重复吃掉了），旁边还留着 `_ = pluginNames` 死代码。

### 修法

- `dto.NormalizePlugins`：重复 → `插件 %q 重复声明（显式拒绝，不静默去重）`；
- `sessionstore.ValidateTeamworkPlan`：删死代码，保留「落盘前最后一道」的重复拒绝；
- 读侧/传输侧的规整（`normalizePluginNames` / `tools.WithRolePlugins`）在注释里写清
  「这不是声明侧口径」——它面对的是已经过入口校验的事实。

### 修后

```
重复声明的 team_plan：
  err = team_plan: 成员 "exec" 的插件装配非法: 插件 "docs" 重复声明（显式拒绝，不静默去重）
  计划未落盘 = true
```

阴性对照：`[" docs "]` 单次声明仍受理；`["  "]` 清完为空 = 不覆盖（不是「装配了空集」）。

## 3. G：派发三跳钉子

三处 `Plugins:` 透传点各自补了用例，**断言的是载荷本身与本轮 ctx**，不是回执：

- `seelebridge/teamwork/items_plugins_test.go`
  （`Dispatch` / `DispatchItem` 的 `WorkerRequest.Plugins`、未声明 = 空、
  `SetPlan` 改写后取新声明）；
- `seelebridge/runtime_role_plugins_fault_test.go`
  （`team_plan → team_work → team_dispatch(item) → RunWorker → 本轮 ctx + 工具面`，
  并顺带把「撤销之后再用它」的失灵读数钉在同一个用例里）。

### 判别力（变异检验：临时把那一跳删掉，用例必须红）

```
# coordinator.go:160 删掉 Plugins → 
Dispatch 的载荷丢了装配声明：Plugins=[]，want [docs]（派发时带了、执行时丢了）
# items.go:374 删掉 Plugins →
DispatchItem 的载荷丢了装配声明：Plugins=[]，want [test]（派发时带了、执行时丢了）
# runtime_teamwork.go:629 删掉 Plugins →
这一轮的 ctx 装配 = []，want [docs]（三跳里有一跳把 Plugins 丢了）
```

三次都在改回之后复跑通过，工作区里没有留下变异。

## 4. F：口径写准（不改行为）

`runtime_role_plugins.go` 的三条边界与 `runtime_teamwork_schema.go` 的 `plugins` 描述
改成：

> 插件只收窄**权限面**（与权限面相交后只会更小），**不相对宿主装配**——装配集合替换
> 宿主那一份收窄，可以比它更宽（`plugins/default/plugin.md` 的 include/exclude 皆空 ⇒
> 声明 `plugins:["default"]` 就等于拿到全工具面），也可以比它更窄。

## 5. 证据（命令与输出）

```
go build ./...                                        → 通过（无输出）
go test ./application/... ./sessionstore/... ./seelebridge/... -count=1
  ok application/core 31.4s / sessionstore 67.5s / seelebridge 34.7s / seelebridge/teamwork 2.5s / …
go test ./plugin/... ./seelexctx/... . -count=1        → 全 ok
go vet ./seelebridge/                                  → 干净
```

## 6. 未覆盖 / 残留（诚实清单）

- **C 的宿主面读数是时间点事实**：`inherit-host` 的读数取「那一刻」的全局激活插件；
  全局 `switch_plugin` 之后旧读数会过时（读数只在它被生成的那一刻承诺与运行面一致）。
- **失灵只在派发与计划回执里可见**：执行体内部（`PluginFace` 收口返回空面）没有独立
  日志面——本包没有 logger 端口，谁在跑那一轮的失灵只能从派发回执与工具面为空的后果看出来。
- **D 未覆盖 `main.go` 的精选装配路径**（边界约定不碰）：那条路的名字来自
  `plugin.CuratedCatalog`，它自己的校验已经拒绝 preset 内的重复
  （`plugin/curated.go:489-495`）。
