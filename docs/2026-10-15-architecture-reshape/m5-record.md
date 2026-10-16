# M5 记录：thin main（启动项分包 + 集成用例归位）

- 工作包：`docs/2026-10-15-architecture-reshape/`
- 分支：`refactor/architecture-reshape`（`main` 未动，未 push）
- 时间：2026-10-16
- 事实源：`git log`、本文件列出的实跑命令输出、各包 README

## 0. 范围声明（先说清"完成到哪一半"）

plan 的 M5「中间层切割与装配瘦身」有四组动作：

| 动作 | 状态 |
|---|---|
| (c) `main.go` 的注册段迁 `wiring/registry` | **已完成**（S2） |
| 装配根变薄：启动参数 / 装配顺序 / 结构体归位 | **已完成**（S1/S3：`main.go` 48 行） |
| 根目录集成用例归位（用户追加要求） | **已完成**（S4） |
| 触发脚本与规范同步（用户追加要求） | **已完成**（S5） |
| (a) §2.4 中间层归位（`middle/view/board`、`middle/view/record`） | **未开始** |
| (b) `application/core` 剩余文件按域切分（先 `kernel/state`，再域） | **未开始** |
| (d) `Service` 门面收窄为五组窄接口 + I5 校验 | **未开始** |

因此本记录只主张"**装配瘦身与入口分包**这一半完成"，M5 的退出条件（门面五组收窄且每组建模
测试通过、中间层不写会话事实、`middle` 与 `backend` 无环）**尚未验证**。

## 1. 目标与验收

这一半的目标是**入口变薄**，让"同一套装配服务多个入口"成立：

| 验收项 | 目标 | 现在 |
|---|---|---|
| `main.go` 行数 | < 400 | **48** |
| 启动参数归属 | 一处声明、可复用 | `wiring/config.Flags` + `BindFlags` |
| 装配顺序归属 | 一处实现、可复用 | `wiring/bootstrap.Run` / `App` |
| 工具注册归属 | 一处实现 | `wiring/registry` |
| 仓库根测试文件 | 0（用例进专门包） | **0**（57 个 → `test/`） |
| 行为面 | 只搬位置，不改行为 | 见 §3 保真证据 |

## 2. 阶段与提交

| 片 | 提交 | 内容 | 结构证据 |
|---|---|---|---|
| S0 索引卫生 | `a6917253` | 生成器改 LF + 刷新 `application/core` 分卷索引（既有漂移） | `wiring/config/README.md` 等新产物不再 CRLF |
| S1 配置与参数 | `5e7811c5` | `wiring/config`：25 个函数 + flag 表（`main.go` 2350 → 1861） | `go list -deps ./wiring/config` 不含 Seelebridge/application |
| S2 注册段 | `04a4e83a` | `wiring/registry`：19 函数 + 2 整文件（→ 1008 行） | `register_goal_tools.go`/`skill_activate_tool.go` 整文件消失 |
| S3 装配段 | `aa896974` | `wiring/bootstrap`：`Run`/`App`/`Init*`（→ 48 行） | 入口只剩 5 步；`App.Shutdown` 逆序清理栈 |
| S4 用例归位 | `53d9fb87` | 57 个 `*_test.go` → `test/` 包（+ 11 卷索引文档） | 根 `*_test.go` = 0；`test/*.go` = 59 |
| S5 触发脚本 | 本提交 | 钩子路径判据收紧 + 工具链解析 + 自测 + 记忆/规范同步 | `bash scripts/hooks-selftest.sh` 13/13 |

## 3. 保真证据（"只搬位置"是怎么证明的）

1. **逐函数对账**：S1/S2/S3 每一步都用脚本把新位置与 HEAD 的函数体做归一化 diff
   （`tmp/reshape/m5_s2_fidelity*.py`、`m5_s3_fidelity.py`）——残余差异只有
   `config.`/`registry.` 前缀、导出名与既定的三类显式化改写。
2. **语句序列对账**（S3）：旧 `run()` 238 条语句 vs 新 `Run` 241 条，89 行差异**逐条**归入
   "前置段入参校验 + `App` 字段 / `defer` → `addCleanup` / `return err` → `fail` / 三个显式参数 /
   结尾字段赋值"。装配链中段（runtime → … → 定时任务恢复）**零差异**，即顺序未动。
3. **关机顺序**：`App.Shutdown` 反向执行清理栈，末位关 backend 诊断输出——与旧 `defer` 链的
   LIFO 逐条相同（`closeProjectBoard` → `application.Shutdown` → `store.Close` → `runtime.Shutdown`
   → 输出关闭）。
4. **调用点改写只改前缀**：根测试 41 + 10 + 16 个文件分三片改指 `config.`/`registry.`/`bootstrap.`，
   断言一字未动（唯一"形"的变化是 `SetupPermissionGate` 由"改包级 flag 再调用"变成显式收档位）。
5. **全量测试失败集合**：与改动前的既有基线一致（`application/core` 3 例 + `backend/tool/registry`
   1 例 Windows 敏感 flake），另有一次并行负载下 `backend/engine/assemble` 的 fork best-effort
   用例超时（单跑 0.38s 必过）。

## 4. 删除清单（重复实现 → 唯一位置）

| 删掉的第二份实现 | 唯一位置 | 为什么它是重复 |
|---|---|---|
| `main_unix.go` / `main_windows.go` 的 `processExists`（+ `main.go` 末尾指向它的注释） | `store/root.ProcessAlive` | 同一个"PID 还活着吗"判据；主包那份在 Windows 上把"已终结但句柄未释放"判成活，正是 `store/root` 注释里记着的 2026-09-18 事故。`wiring/config` 新增 `TestProcessLivenessHasOneImplementation` 机械钉住"只有一处"。 |
| `main.go` 里 `projectBoardReadRows` / `projectBoardHookMode`（搬迁时短暂留下的副本） | `wiring/registry`（包内私有） | Go 不报"未使用的函数"，只有调用点被搬走时两份都会编译——已删副本，`main.go` 只留一处 `registry.ProjectBoardSinkFor(...)` 调用。 |
| `main.go` 的 `Version` / `DefaultFrontend` | `internal/buildinfo` | 构建期注入点本来就在那里（`-X .../internal/buildinfo.Version`）；入口再存一份就是第二个名字。 |
| 根目录 57 个 `*_test.go` | `test/` 包 | 用例与入口同层时无法被单独点名、也无法在换入口后复用。 |
| `wiring/config` 的第二份"拼 `<exe>/config`" | `RuntimeConfigChain` / `PromptDirChain` | 责任链只有一条；`test/plugin_roots_test.go` 把"拼插件根只有一处"升级成**全仓调用点计数**。 |

## 5. 新落点与依赖方向

```text
main.go（48 行：BindFlags → Parse → pprof → -version → Run → Serve → Shutdown）
  └─ wiring/config     启动参数 + 路径/配置责任链（零 Seelex 依赖，只剩 stdlib/overlay/bootseed/promptassets/buildinfo/plugin/dto/store-root）
  └─ wiring/bootstrap  装配顺序 + 逆序关机（依赖 application/backend/gui/tui/registry/config）
  └─ wiring/registry   工具与能力注册（依赖 application/assemble/backend-tool/config）
test/                  仓库级集成用例（只依赖公开 API；TestMain 把 CWD 切到仓库根）
```

三条命令可复现依赖检查：

```text
go list -deps ./wiring/config | grep seelex     # 只应出现 backend/limit/overlay 等低层
go list -deps ./wiring/registry | grep seelex
go build ./... && go vet ./...
```

## 6. 偏差登记

| 编号 | 偏差 | 理由与代价 |
|---|---|---|
| D-M5-S1 | 数据根锁的"进程存活"判据由主包版本换成 `store/root.ProcessAlive`（更强：Windows 上"已终结但句柄未释放"判死） | 判据归一（§8.3）；行为差异只出现在旧实现会误判为"活"的场景，代价是 `resolveStorePath` 在该场景由避让 `_1` 变为复用基路径。 |
| D-M5-S2 | `wiring/registry` 的 `InitPluginSystem` / `InitStore` / `InitWorkspaceRepo` / `InitApplication` / `SetupPermissionGate` 增加显式参数（`pluginsPaths` / `storePath` / `mode`） | 装配段不再读包级 flag，"第二个入口"才能换值；代价是测试调用点补参数。 |
| D-M5-S3 | `test/` 的 `TestMain` 把工作目录切到仓库根 | 57 个用例按仓库相对路径读夹具（`gui/icons`、`scripts/`、`.claude/`、`plugins/`、源码文本门禁）；改路径=重写用例。代价：用例内的相对路径语义依赖这条口径，README 与 doc.go 都写明。 |
| D-M5-S4 | `test/` 的源码文本门禁改靶（`main.go` → `wiring/bootstrap/*.go`） | 门禁原本断言"装配段把根读数带进 UI 面"，装配段已换文件；判定内容未变。 |
| D-M5-S5 | `.githooks` 收紧 post-commit 的构建判据（纯用例提交不重建）+ 工具链解析（找不到 gofmt 时大声警告、pre-push 找不到 go 时拒绝推送） | 前者是搬迁带来的新能力（用例与源码在路径上分开了）；后者是自测发现的**既有缺陷**（精简 PATH 下格式检查一直空转）。 |

## 7. 未完成项

### 7.1 M5 剩下的三组动作（plan §M5 原文）

1. **(a) 中间层归位**：`middle/view/board`、`middle/view/record`（§2.4）。当前 `backend/engine/assemble`
   里的 `runtime_project_board*` 等仍归装配侧。
2. **(b) `application/core` 按域切分**：先 `kernel/state`，再按域；退出条件是门面五组窄接口成型。
3. **(d) 门面收窄 + I5 校验**：`Service` 导出方法面按五组收窄，每组建模测试；并把"中间层不写会话
   事实""`middle` 与 `backend` 无环"变成断言。现有 `e2e/facade_boundary_test.go` 的 ratchet 已就位，
   收窄时应当**只减不增**（允许清单上限随之下降）。

### 7.2 M4 的尾巴（M5 之前就欠着）

1. **`backend/engine/assemble` 的按域下沉**（M4-J2 续做）：`Runtime` 仍是跨文件单类型；
   `runtime_teamwork*.go`（11 文件）等域尚未下沉。出口条件（M4 记录）：方法按域分到各板块且无跨板块回指。
2. **K 片**：`assemble/ports.go` 的 ~1020 行逐行委托 + 兼容别名待删（删掉后契约层闭包更干净）。
3. **`seelebridge/` 目录里的运行数据**：代码已清空（`go list` 里不再有该包），目录内只剩
   `.seelex/mcp-traces/all.json`（940 B，运行期 trace，非会话数据）与空 `workunit/`。删除它属于
   "移动/删除用户数据"，按 `MEMORY.md` 需要用户确认后再动。

### 7.3 已裁决但未实现的增量能力

1. **M2-D（loop 预警）**：用户已裁决"延后到 M5 之后讨论"；`Verdict`（Ok/Warn/Exhausted）+ 总结正文
   走 `workunit.RecoveryNote` 的增量能力尚未实现。
2. **V10（工具记录看板）**：读数不进 `Snapshot`、钩子直接与 JSONL 交互——待设计。
3. **M2-C5（中间件定序）**：`§1.4` 里唯一真正的换序（路径门前移到权限判定之前）尚未实施，
   用户要求先说明运行时行为变化（已说明：越界路径不再先弹提权，改为直接以作用域错误失败）。

## 8. 验证（本机实跑，M5 收尾时）

```text
gofmt -l main.go wiring test e2e scripts   → 空
go build ./...                              → 无输出
go build -tags "gui,desktop,production" ./... → 无输出
go vet ./...                                → 无输出
go test ./test/ -count=1 -timeout=900s      → ok（50.2s 单独 / 107.5s 全量并行）
go test ./e2e/ -count=1                     → ok（含模块 README、链接、facade ratchet 门禁）
go test ./... -count=1                      → 失败集合 = 既有基线（见 §3.5）
bash scripts/hooks-selftest.sh              → pass=13 fail=0
```

结构证据（可直接复跑）：

```text
(Get-ChildItem . -File -Filter *_test.go).Count   # 0
(Get-ChildItem test -Filter *.go).Count           # 59
python scripts/gen_core_readme_index.py           # 索引刷新（test/ 分卷 + 各新包）
```

## 9. 与 plan 的对应

- plan §2.5（装配文件落点）：`backend/engine/assemble` 收口后，M5 的入口侧
  （`wiring/config` / `wiring/bootstrap` / `wiring/registry`）已按"一个目录一个 package"落地；
  `main.go` 达到"只装配"的终局形态。
- plan §5.1（M1–M5 只做物理归位、行为零变化）：S1–S4 遵守（三条显式化改写与一条判据归一已登记
  为偏差）；S5 的钩子改动属工具链缺陷修复，不涉及产品行为。
- plan 的 M5 目标 `main.go < 400` 行：超额达成（48 行）。

## 10. M5 第二半：按 `m5-slices.md` 推进的切片

### R1 —— `internal/adapters/` → `wiring/adapters/`（plan §2.10，commit `853f6b31`）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 |
|---|---|
| `internal/adapters/`（23 文件 2454 行 + README） | `wiring/adapters/`（包名仍为 `adapters`，逐字节搬） |

**为什么这是"只搬位置"**：23 个文件无一字节改变（`git show --stat` 里 22 个文件 0 行差异、
1 个是 README 的路径修正）；改动**只出现在取用侧**：13 个 Go 进口方改 import 路径、
`gofmt` 因路径变长重排 import 块。

**同步的登记面（漏一个就打红 CI）**

| 面 | 改法 |
|---|---|
| `e2e/layout_test.go` 的 `repositoryModules` | `internal/adapters` 换登 `wiring/adapters`；`internal` 仍在（其余子包未动） |
| `e2e/dto_boundary_test.go` 的反向断言 | 路径是 `filepath.Join(root, "internal", "adapters", ...)` **分段式**，纯字符串替换抓不到 —— 必须单改（这是本切片唯一一次"跑起来才发现"的漏点） |
| `e2e/subagent_status_vocabulary_gate_test.go` | 写死路径表里的 `internal/adapters/session_workspace_ports.go` → `wiring/adapters/...`，判据一字未改 |
| `internal/README.md` | 删掉 `adapters/` 一节（否则链接指向已删的 `adapters/README.md`）＋架构图节点/边同步 |
| `wiring/README.md` | "规划（尚未实现）：`internal/adapters/` 将搬到 `wiring/adapters/`" 改成已落位事实；`WebSearch` 那一格仍标**规划**（唯一实现当前是 `backend/tool/search`） |
| `wiring/adapters/README.md` | 生态位/架构图/composition root 改为 `wiring/bootstrap`；`seelebridge.Runtime` 措辞改为 `backend/engine/assemble.Runtime`（该包源码里仍以局部别名 `seelebridge` 引入，别名不改行为） |

**偏差登记**

| 编号 | 偏差 | 理由 |
|---|---|---|
| D-M5-R1-1 | 未删 `internal/adapters` 之外的两个空目录（`seelebridge/workunit/`、`internal/toolmetrics/`） | 仓库 §0 铁律要求删除类操作先向用户预警并取得确认——已提给用户，不擅自执行 |
| D-M5-R1-2 | 未给 `wiring/adapters/README.md` 补「文件与函数索引」 | 该段是 readme-spec §3 要求（`wiring/{config,registry}` 已有），但本切片只做归位；补索引要扩 `scripts/gen_core_readme_index.py` 的目标包清单，登记为 R10 门面收窄时一并做 |

**验证（原始输出）**

```text
gofmt -l .（排除 tmp/ 工作树）           → 空
go build ./...                          → 无输出
go build -tags "gui,desktop,production" ./... → 无输出
go vet ./...                            → 无输出
go test ./e2e/ ./wiring/... ./contract/... ./session/... ./store/... -count=1 → 全 ok
go test ./... -count=1 -timeout=900s     → 失败集合 = 基线（application/core 3 例：
                                           TestContextBudgetSkipsCompactionWithoutMargin /
                                           TestRunningSessionAccumulatesAllRoundsInOrderControl /
                                           TestColdRestoreTailFirstRequestKeepsTranscriptOrder）
```

**过程事故**：本轮派出的两个子代理（`m5_adapters`、它再派生的 `m5_k1`）在 25 分钟内**没有产生
任何文件改动**，也未回报；按"长时间无响应就串行执行"的约定，我已中断它们并亲自完成本切片。
教训：子代理侧要有"多久没落地就交回"的硬超时，不能靠它自述状态。

### R3 —— `model` / `event` / `state` 归位（plan §2.3，commit `0853f297`）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 |
|---|---|
| `application/model/`（5 文件 1037 行 + README） | `middle/protocol/model/`（包名仍为 `model`） |
| `application/event/`（1 文件 346 行 + README） | `middle/protocol/event/`（包名仍为 `event`） |
| `application/core/internal/state/`（27 行 + README） | `kernel/state/`（包名仍为 `state`） |

`git mv` 的三组文件**零行差异**；145 个引用侧只改字符串（import 路径与散文落点）。

**同步的登记面**

| 面 | 改法 |
|---|---|
| `e2e/layout_test.go` | 模块表换登记（`middle/protocol/*`、`kernel/state`） |
| `e2e/facade_boundary_test.go` | 三张 allowlist 等量换名，**上限不动** |
| `e2e/dto_boundary_test.go` | 又一次分段式路径（`filepath.Join("application","model","state.go")` 与 `readRepoFile`）——同 R1 的教训：`filepath.Join` 的段式写法纯字符串替换抓不到 |
| `application/README.md` / `application/core/README.md` | 表行与相对链接改指 `../middle/protocol/*`、`../../kernel/state/`（否则 `TestRepositoryModuleReadmeLinks` 打红） |
| `middle/protocol/event/README.md` | 相对链接深度修正（`../../docs/gui` → `../../../docs/gui`） |
| `scripts/gen_core_readme_index.py` | 删掉已迁出的 `internal/state` 叶子循环（留着下次刷新直接 `FileNotFoundError`）；把 `kernel/state`、`middle/protocol/{model,event}`、`wiring/adapters` 登记进 `EXTRA_PACKAGES` |

**顺带补齐**：`middle/protocol/{model,event}` 与 `wiring/adapters` 此前**没有**「文件与函数索引」
（readme-spec §3 要求），本次由生成器补上——这同时销掉了 D-M5-R1-2。

### R4 —— 中间层三个叶子包归位（plan §2.4，commit `c64b37fe`）

**删除清单**

| 原位置 | 新唯一位置 |
|---|---|
| `application/core/view_state/` | `middle/view/state/`（包名仍为 `view_state`） |
| `application/core/subagent_view/` | `middle/view/subagent/`（包名仍为 `subagent_view`） |
| `application/core/worktable/` | `middle/view/worktable/`（包名仍为 `worktable`） |

`git show --stat` 里这三组是"0 行变化"的重命名；改动只在 25 个取用侧（core 内部
`service_assembler/components/state`、`work_table.go`、`chat.go`、`session_*` 等）与 e2e 门禁表。
架构图/表格里对已删目录的链接一并改指（`TestRepositoryModuleReadmeLinks` 是这里的守门人）。

### 第二半的当前进度（可复跑 `python tmp/reshape/remaining.py`）

| 切片 | 状态 |
|---|---|
| R1 adapters → `wiring/adapters` | 已提交 `853f6b31` + 记录 `72b6082e` |
| R3 model/event/state → `middle/protocol/*`、`kernel/state` | 已提交 `0853f297` |
| R4 中间层三叶子 → `middle/view/*` | 已提交 `c64b37fe` |
| R9 `plugin` / `skill` / `mcpstack` → `backend/engine/*` | 已提交 `4ab87318`（skill）、`e0a7665c`（plugin）、`fa28cbd7`（mcpstack） |
| R2 toolrecord → `store/record` | 已提交 `88d349f0` |
| R5/R6 `application/core` 根文件按域切分（view/switch/residency） | 未开始（需 §2 两段式切分） |
| R7/R8 core 的 engine/tool/multiagent/limit 域归位 | 未开始（同上） |
| R10 `aliases.go` 删除 + 门面五组收窄 + I5 | 未开始 |
| R11 `assemble/ports.go` 删除 + `Runtime` 按域下沉 | 未开始 |

**Windows 偶发基线（本轮实测，不是回归）**：全量 `go test ./...` 在并行负载下会出现
`backend/tool/registry`（`TestJobHandlesCarryTheirTableToken` 的 TempDir 清理、进程句柄未放）、
`gui/terminal`（`TestManagerRealShellRoundTrip`）、`store`（`TestCommitNotBlockedByFullHistoryRead`）
三类超时/清理失败；**逐条单跑必过**，与环境负载相关，与本次搬迁无关。

### R9 —— 顶层叶子包归位 engine 板块（plan §2.5，`4ab87318` / `e0a7665c` / `fa28cbd7`）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `skill/`（4 Go + README） | `backend/engine/skill/` | 包名不变，25 个取用侧只改 import 字符串 |
| `plugin/`（14 文件） | `backend/engine/plugin/` | 生命周期事实源，包名不变；15 个取用侧只改 import |
| `backend/engine/plugin/plugin.go` | `backend/engine/plugin/visibility/visibility.go` | 包名 `plugin` → `visibility` |
| `backend/engine/plugin/plugin_face_test.go` | `backend/engine/plugin/visibility/visibility_face_test.go` | 同上 |
| `backend/engine/plugin/README.md` | `backend/engine/plugin/visibility/README.md` | 同上 |
| `mcpstack/`（8 Go + README） | `backend/engine/mcp/mcpstack/` | 包名不变 |
| `mcpstack/config/`（2 Go + README） | `backend/engine/mcp/mcpstack/config/` | 包名不变 |

**为什么有这么两处改名（偏差登记）**

plan §2.5 把顶层 `plugin/`（生命周期）与 `seelebridge/plugin`（Runtime 可见性投影缓存）
都指向 `backend/engine/plugin`，`mcpstack` 与 `seelebridge/mcp` 同理都指向
`backend/engine/mcp`——两处都是"两个概念、一个目录名"，而一个目录只能有一个 package。
裁决：**plan 里更晚、更完整的那份概念拿裸名，另一份退一档做子包**。

- `plugin` → 生命周期事实源（plan §2.5 第 317 行）拿 `backend/engine/plugin`；
  可见性投影缓存退 `backend/engine/plugin/visibility`，包名随之 `plugin` → `visibility`
  （4 个取用侧：`assemble/runtime.go`、`assemble/runtime_role_plugins.go`、
  `registry/role_plugins_test.go`、e2e 闭包清单）。
- `mcp` → M4 已把 `seelebridge/mcp` 落在 `backend/engine/mcp`，因此 `mcpstack`
  退 `backend/engine/mcp/mcpstack`，**包名保持 `mcpstack`**（零改名）。

两处都是"同一份代码换落点"，门面闭包清单因此是等量替换：`facadeClosureAllowListMax`
保持 73 不变（只有条目内容变，条数不变）。

**顺带修掉的陈旧引用**（搬迁暴露的文档漂移，不是新行为）：
`backend/engine/mcp/mcpstack/README.md` 与它的 `config/README.md` 仍写着
`seelebridge.Runtime` / `seelebridge.MCPServer` / `main.go` / `bootseed` 与
`go test ./mcpstack`；`backend/engine/skill/README.md` 仍写 `seelebridge`；
`backend/engine/plugin/README.md` 仍写 `main.go` 的 `pluginRootChain` /
`requirePlugins`（现落在 `wiring/config`）与 `go test ./plugin/...`。逐处改指当前落点。

**验证**：`gofmt -l .` 空、`go build ./...` 绿、`go vet ./...` 绿，
`go test ./e2e/ ./backend/engine/plugin/... ./backend/engine/skill/ ./backend/engine/mcp/... ./backend/engine/assemble/ ./wiring/... ./test/ -count=1` 全绿。
`git diff -U0` 逐行抽查：Go 侧改动只有 import 字符串（+ 4 处 qualifier 改名），
其余是 e2e 模块表/闭包清单与文档。

### R2 —— 工具记录读数归位 store 板块（plan §2.10 / §2.12.2，`88d349f0`）

**删除清单**

| 原位置 | 新唯一位置 |
|---|---|
| `internal/toolrecord/accumulator.go` | `store/record/accumulator.go` |
| `internal/toolrecord/accumulator_test.go` | `store/record/accumulator_test.go` |
| `internal/toolrecord/README.md` | `store/record/README.md` |

包名 `toolrecord` → `record`（一个目录一个 package）；`Accumulator` 的方法签名、
dirty 门、原因归一、`Load` 只增不减语义逐字保留。`internal/README.md` 的子模块清单与
架构图删掉该条目（这是"目录消失"的证据面）。

**本片不含的部分（如实登记）**：plan §2.12.2 还有一条"判据
（`toolStatusFailed` / `toolRecordReason` / `recordToolOutcome`）→ `backend/tool/record/`"。
这条要切 `application/core` 的单包跨文件类型（判据与三条工具链 `tool.completed` /
`subagent.tool.completed` / `teammate.tool.completed` 耦合在同一 `Service` 上），
归属 **R7/R8 的核心切分**，不在本片范围。会话级粒度与批次落盘
（写放大 200×→1×，见本文件 §4.0 的 A/B）在此前切片已落地，本片只换落点。

**验证**：`gofmt -l .` 空、`go build ./...` 绿；
`go test ./store/... ./application/core/ ./e2e/ ./test/ -count=1` ——
`store`、`store/record`、`store/root`、`store/schedule`、`store/workspace`、`e2e`、`test` 全绿；
`application/core` 仍是基线 3 例既有红（`TestContextBudgetSkipsCompactionWithoutMargin`、
`TestRunningSessionAccumulatesAllRoundsInOrderControl`、
`TestColdRestoreTailFirstRequestKeepsTranscriptOrder`），与迁移前**逐条同名**。

### 本轮的机械化工件（可复用）

- `tmp/reshape/repath.py <old> <new>`：只改 Go **import 字符串**（要求结尾引号或斜杠，
  因此 `plugin` 不会误伤 `plugins`），跳过 `vendor/dist/tmp/_scratch` 等非源码树。
  文档一律手工改——历史/计划句里的旧路径**不跟着改**（见 R1 的教训）。
- `tmp/reshape/r9b.py`：`plugin → visibility` 这类"路径 + 限定符"双改，逐文件打印
  changed/unchanged 供复核。
- 提交信息一律落盘成 `tmp/reshape/msg-*.txt` 再 `git commit -F`：PowerShell 没有 heredoc，
  内联中文经 shell 管道会打坏文件（M4 期已出过一次事故，见 m4-record 的教训）。

## M5-R5a —— 项目看板行投影与同步器归位 `middle/view/board`（plan §2.4）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `application/core/project_board.go`（867 行，§1 映射 + §2 触发表 + §3 同步器） | `middle/view/board/projection.go`（§1+§2）与 `middle/view/board/syncer.go`（§3） | `package core` → `package board`；`WorkItem` → `model.WorkItem`；`Limits()` → `limit.Get()`（同一份进程级快照，core 的那个门面取数口 → `backend/limit`） |
| `application/core/project_board.go`（§4 Service 侧只读取数口 + `taskRecordsForSession`） | `application/core/project_board_read.go` | **留在应用层**（偏差登记见下）；`TeamworkPlanSource` → `board.TeamworkPlanSource` |
| `application/core/project_board_test.go`（1073 行） | 见下三处 | 夹具与纯用例/取数口用例按"被测对象在哪"拆开 |
| 同上（映射/触发表/同步器用例） | `middle/view/board/projection_test.go`、`syncer_test.go`、`trigger_test.go` | 用例体逐字保留，只换限定符 |
| 同上（两侧共用的夹具） | `middle/view/board/boardtest/{store.go,rows.go}` | 内存存储面 + 行助手；**刻意不 import 本包**（否则本包内部用例 import 它 = 测试环） |
| 同上（Service 侧取数口/钩子接线用例） | `application/core/project_board_read_test.go` | 与 `project_board.go` 的 §4 同落点 |
| `application/core/project_board_hook_modes_test.go` | `middle/view/board/trigger_test.go` | 判据一词不动（档名词表 = 触发表键集） |
| `application/core/work_table.go` 的 `truncateWorkEvidence`（未导出） | `middle/view/worktable.TruncateWorkEvidence` | **一条判据只能有一份**：看板映射也要用它（`EvidenceChars` 截断 + `…`），而 `middle` 不能反向 import `application/core`；调用点 5 处改引用（core 4 文件 + board 映射） |

**偏差登记（未按 plan 字面做的部分）**

1. `project_board.go` 的 §4（`WorkTableRowsFor` / `ProjectRootFor` / `ProjectBoardTeamPlanFor` /
   `ProjectBoardTeamStateFor` / `taskRecordsForSession`）**留在 `application/core`**，不随文件搬入
   `middle/view/board`。理由：这五个方法读的是**服务状态**（视图锁、`Snapshot.Session.ID`、
   子代理树、task 注册表 scope、`Deps.Workspace`/`Deps.Runtime` 端口），要搬必须先给 board 定一个
   只能由 `core` 实现的 `Host` 端口（plan §2 的两段式第 4–6 步）。本片先把**无状态**的两半
   （映射 + 同步器，794 行非空行）搬干净、行为零变化；`Host` 注入版取数口是本条的**续做步**
   （见"下一步"）。**偏差的代价是零**：门面签名与装配调用点一字未动。
2. `boardtest` 是**新模块**（plan 未点名）：两侧用例（board 自己的同步器用例 + core 的接线用例）
   共用同一份内存存储面夹具——若在两侧各写一份，水位/追加语义就会漂移成两份判据。

**保真证据（不是"测试绿了"，是"只剩一份实现 + 未改一行逻辑"）**

- `python tmp/reshape/r5a_boundary_check.py`：把 HEAD 的 `project_board.go` 与三份目标文件的
  非空行做**行多重集**比对（剥掉 package/import 行，只归一化搬迁强制的限定符差异
  `model.WorkItem` / `limit.Get()` / `worktable.TruncateWorkEvidence` / `board.TeamworkPlanSource`）：

  ```text
  original non-empty lines: 794
  destination non-empty lines: 794
  lines in original but not found at destination: 0
  lines at destination that were not in the original: 0
  ```

- 门面闭包上限 73 → 74，并把 `middle/view/board` 登记进 `facadeClosureAllowList`（**一条进**的新模块：
  此前它是 `application/core` 里的一个文件，不是独立包；自身直接依赖全在清单内）。
  正负两向都验过：把清单里那一行注掉，`TestFacadeDependencyClosureRatchet` 立刻报
  "门面依赖闭包新增了未登记的 Seelex 包 … middle/view/board"。
- `e2e/layout_test.go` 模块表增 `middle/view/board`、`middle/view/board/boardtest`；
  `scripts/gen_core_readme_index.py` 的 `EXTRA_PACKAGES` 增同两项（README「文件与函数索引」由它生成）。

**验证（HEAD 上重跑）**

- `gofmt -l .` 空；`go build ./...` 绿；`go build -tags "gui,desktop,production" ./...` 绿；
  `go vet ./...` 绿；`go test ./e2e/ -count=1` 绿。
- `go test ./middle/view/board/... -count=1` 绿（搬运后的映射/同步器/词表用例）；
  `go test ./application/core/ -run 'ProjectBoard|BuildProjectBoardRows' -count=1` 绿（取数口 + 钩子接线）。
- 全量 `go test ./... -count=1 -timeout=900s` 的失败集合 = 基线 3 例
  （`application/core` 的 `TestContextBudgetSkipsCompactionWithoutMargin`、
  `TestRunningSessionAccumulatesAllRoundsInOrderControl`、
  `TestColdRestoreTailFirstRequestKeepsTranscriptOrder`）+ 2 例 Windows 环境性红
  （`backend/tool/registry.TestJobHandlesCarryTheirTableToken` 的 TempDir 清理、
  `gui/terminal.TestManagerRealShellRoundTrip`）。
  后两例**逐条取证**：`gui/terminal` 那条单跑即绿；`backend/tool/registry` 那条在
  **HEAD 的干净 checkout**（`$env:TEMP` 下的独立 worktree，无本次改动）上**同样红、同一句错误**
  （`TempDir RemoveAll cleanup: … being used by another process`），因此与本次搬迁无关。

**下一步（R5 续做，按依赖顺序）**

- R5a-2：给 board 定 `Host` 端口（视图读锁下的一致口径 + task 记录 + 异步运行 + 项目根 + teamwork 计划），
  把 §4 五个方法搬进 `middle/view/board/read.go`，`Service` 侧只留转发——门面与装配调用点不变。
- R5b：`work_table.go` + `work_table_async.go` → `middle/view/worktable/`（终态触发回合段 → `middle/switch/`），
  其中 `buildWorkTable` 等纯投影函数与 `TruncateWorkEvidence` 同落点。
- R5c：`runtime_projection.go` / `diagnostics.go` / `perf_stats.go` / `service_snapshot.go` → `middle/view/`。

**本片的教训（写进工具约定）**

- **不要用 `Set-Content -Encoding utf8` 做变异实验**：Windows PowerShell 5 会写出 BOM + CRLF，
  当场把 `e2e/facade_boundary_test.go` 变成 CRLF+BOM（本片实际踩到，已 `git checkout HEAD --` 复原后
  重新用 `apply_patch` 落改动）。变异实验一律"改 → 跑 → 立刻用 git 复原 → 用 apply_patch 重落"。
- 夹具跨包时**先想好 import 方向**：`boardtest` 若 import `board`，board 的内部用例就无法 import 它
  （Go 不允许测试环）。所以夹具刻意保持"零业务依赖 + 由使用侧断言接口满足"。

## M5-R5c —— 视图读数面归位 `middle/view`（plan §2.4）

提交：`d7ca1cc4`（`middle/view` 根包本体（`diagnostics.go` / `metrics.go` / `projection.go` /
`view_test.go` / `README.md`）+ 三处宿主缝 + 两道门禁登记）。分支仍是
`refactor/architecture-reshape`。

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `application/core/diagnostics.go` 的 `RenderDiag` 实现（82 行） | `middle/view/diagnostics.go` 的 `RenderDiag` | 宿主只留一行转发（`/diag` 命令与门面签名不变） |
| `application/core/perf_stats.go` 的派生计算（44 行） | `middle/view/metrics.go` 的 `PerfStats` | 宿主只留"锁下取数"（上限 / 归档读数 / 快照三处输入） |
| `application/core/runtime_projection.go` 的 `latestVisibleUserGoal` + `truncateRuntimeProjectionGoal`（两条判据） | `middle/view/projection.go` 的 `LatestVisibleUserGoal` + 私有 `truncateRuntimeProjectionGoal` | 宿主只留"锁下取数、锁外发布" |
| `application/core/runtime_projection.go` 对 `backend/engine/assemble` 的投影形状依赖 | `middle/view/projection.go` + `contract/dto` | **消掉一条越层边**：投影形状本已在 `contract/dto`，宿主不再经适配层取名 |

**偏差登记**

1. **锁与进程级上限不搬**。plan 只说文件搬到 `middle/view/`，但 `PerfStats` 原来整段跑在
   `service.ViewMu` 读锁里，`HistoryWindow` 读的是 `backend/limit` 的进程级快照。把锁搬进
   中间层意味着中间层要么持有宿主的互斥量、要么反向依赖 `backend/limit` —— 两条都违反
   plan §1.2 的"中间层允许依赖 = 契约 + store 读端口"。因此本片按**计算入中间层、取数留宿主**
   切：`view.PerfStats(snapshot, historyWindow, toolRefs)` 把三个宿主输入按参数收下。
   代价为零：调用点、门面签名、`Snapshot`/`PerfStats` 字段与 JSON 标签一字未动。
2. **`middle/view/state` 只借一个常量**：`SubagentContextMarker` 是"子代理产出标记"的既有唯一
   定义（`middle/view/state/coordinator.go:26`），`LatestVisibleUserGoal` 直接引用它而不是复写字符串
   ——否则标记词表就有两份，改一处会漂移成两份判据。
3. **`diagnostics.go` 仍在 `application/core` 留名**：文件里只剩一行转发。留名是为了让
   `command.go`（`/diag`）与门面 `application.RenderDiag` 的调用点零改动；plan §2 的两段式
   技术本身就要求"门面侧转发方法保留，这是门面稳定的钉子"。

**保真证据（不是"测试绿了"，是"同一份输入跑两遍得同一结果"）**

- **跨实现一致性探针**（`AGENTS.md` §8.6）：`_scratch/r5c_probe`（下划线目录，Go 工具链与
  README 门禁都当它不存在）把**迁移前的三个函数逐字保留**成 legacy 实现，与 `middle/view`
  在**同一份合成输入**上对跑（含 4 节点计划、2 插件、2 账号（1 个禁用）、5 技能（跨 `cols=4`
  折行）、6 条消息（空正文 / 子代理标记行 / 250 rune 目标 / 截断工具结果）、2 条归档引用）：

  ```text
  RenderDiag 一致（638 字节）
  PerfStats 一致（{SnapshotBytes:2982 ConversationMessages:6 ConversationChars:1114
                   LargestMessageChars:750 ToolResults:2 ArchivedBytes:123
                   TruncatedOutputs:1 HistoryWindow:37 Revision:12345}）
  LatestVisibleUserGoal 一致（201 runes）
  RuntimeVisibility 字段一致
  ParentEvidence 字段一致
  跨实现一致性探针：全部一致
  ```

  `RenderDiag` 与 `LatestVisibleUserGoal` 是**逐字节**相等（不是"结构化等价"）；`PerfStats` 是
  整结构体相等。探针本身不 commit（`_scratch/` 是本地草稿目录），命令与输出记在此处。
- **旧实现只剩一份**：`application/core` 侧只有一行 `return view.RenderDiag(snap)`；
  `latestVisibleUserGoal` / `truncateRuntimeProjectionGoal` 在 `application/core` 已无定义
  （`rg` 全仓只剩 `middle/view` 一处）。宿主的 `PerfStats` 只剩取数与转发。
- **门禁两向验过**：`facadeClosureAllowList` 增 `middle/view`、上限 74 → 75（**一条进**的新模块：
  此前它是 `application/core` 里的文件）；`e2e/layout_test.go` 模块表增 `middle/view`；
  `scripts/gen_core_readme_index.py` 的 `EXTRA_PACKAGES` 增同项，并已重跑生成器（core 分卷索引
  同步删掉两条已迁走的函数条目）。

**验证（本机实跑）**

- `gofmt -l .` 空；`go build ./...` 绿；`go build -tags "gui,desktop,production" ./...` 绿；
  `go vet ./middle/view/ ./application/core/ ./e2e/` 绿。
- `go test ./middle/view/ -count=1` 绿（新包 5 组用例）；`go test ./middle/... ./gui/... -count=1` 绿；
  `go test ./e2e/ -count=1` 绿（模块 README 门禁 / 闭包 ratchet / 链接门禁一并过）。
- `go run ./_scratch/r5c_probe` 退出码 0（上面的逐条一致）。

**下一步（R5 续做）**

- R5a-2：给 board 定 `Host` 端口，把取数口搬进 `middle/view/board`。
- R5b：`work_table.go` + `work_table_async.go` → `middle/view/worktable/`（终态触发回合段 → `middle/switch/`）。
- R5c-2：`service_snapshot.go`（会话状态富化 + 目录行富化）→ `middle/view`，需要先定
  `SnapshotHost`（`ViewMu` 读锁下的 Snapshot / 会话单元 / 草稿槽 / restoring 集合）。

## M5-R8a —— goal 域归位 `backend/multiagent/goal`（plan §2.7）

提交：`39498a1e`（63 文件改动，`42 insertions / 39 deletions`）。分支仍是
`refactor/architecture-reshape`。

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新唯一位置 | 说明 |
|---|---|---|
| `application/core/goal/`（35 文件 6180 行：`controller` / `stack` / `techleader` / `gate` / `advisor` / `a2a` / `audit` / `board_archive` / `sessionstore_store` / `record` / `tl_*` / `headless` / `escape` / `store` + 各自用例） | `backend/multiagent/goal/`（同名同包） | `git mv` 保历史；**34 个 `.go` 全部 100% 相似度**（git rename 检测），README 只有两行测试命令随落点更新 |

**取用侧（只改 import 路径，各 1 行）**

| 文件组 | 数量 | 归属 |
|---|---|---|
| `application/core/goal_*.go` 与 `goal_*_test.go`（`goaldomain` 别名） | 16 | goal 协调器 / 记录器 / 工作摘要 + 各自用例 |
| `backend/engine/assemble/runtime_goal_tl.go`、`runtime_role_turn.go`、`runtime_role_*_test.go` | 4 | 装配侧 TL 端口与角色回合 |
| `gui/headless.go`、`gui/headless_goal_test.go` | 2 | headless RPC 透传 goal 形状 |

**门禁与文档同步**

- `e2e/subagent_status_vocabulary_gate_test.go` 的"写死路径表"7 条改指新落点
  （**判据一字未动**，只换路径）。
- `e2e/layout_test.go` 模块表登记 `backend/multiagent/goal`。
- `e2e/facade_boundary_test.go` 门面闭包条目`application/core/goal` → `backend/multiagent/goal`
  （**等量替换**，上限仍是 75）。
- `application/README.md` / `application/core/README.md` 的 goal 导航链接改指新落点
  （e2e 的 `TestRepositoryModuleReadmeLinks` 先报红、后即改——这条门禁确实抓得住搬迁漏改的链接）。
- `backend/multiagent/goal/README.md` 的测试命令改指新落点；`application/core/agentteam/README.md`
  一处 `application/core/goal/techleader.go` 的文件引用改指新落点。

**顺带对齐的一处既有不一致**：`e2e/facade_boundary_test.go` 的**前端直接依赖允许清单**里本来就写着
`backend/multiagent/goal`（注释标明 D-M1-6"随 headless 拆分（M5）收口"），而 `gui/headless.go` 实际
import 的是 `application/core/goal`。本片之后两者一致——即"允许清单登记的落点"与"实际落点"重合。

**保真证据（结构性，不是"测试绿了"）**

```text
git diff --cached -M --numstat（片内）
  0	0	{application/core => backend/multiagent}/goal/a2a.go
  0	0	{application/core => backend/multiagent}/goal/advisor.go
  …（34 个 .go 全为 0/0）
  2	2	{application/core => backend/multiagent}/goal/README.md
```

即：**被搬的代码一个字节没变**（`git mv` + rename 检测直接证明），改的只有取用侧 import 路径与
登记表。历史记录（`docs/2026-*` 工作包、`CHANGELOG.md`）里的旧路径**刻意不改**——它们描述的是当时
的事实，改了就篡改历史（`_scratch/find_goal_refs.py` 用来确认残留引用只剩这些历史记录）。

**验证（本机实跑）**

- `gofmt -l .` 空（搬迁把两处 import 对齐带偏，已 `gofmt -w` 修回）；`go build ./...` 绿；
  `go build -tags "gui,desktop,production" ./...` 绿；`go vet ./...` 绿。
- `go test ./backend/multiagent/goal/... -count=1` 绿（36 个文件的用例随包搬入，全过）；
  `go test ./gui/... -count=1` 绿；`go test ./e2e/ -count=1` 绿。
- 全量 `go test ./... -count=1 -timeout=900s` 的失败集合 = **基线 3 例** `application/core`
  （`TestContextBudgetSkipsCompactionWithoutMargin`、`TestRunningSessionAccumulatesAllRoundsInOrderControl`、
  `TestColdRestoreTailFirstRequestKeepsTranscriptOrder`）+ **1 例已取证的 Windows 环境性红**
  （`backend/tool/registry.TestJobHandlesCarryTheirTableToken`，TempDir 句柄占用）。**无新增失败**。

---

## M5-R8b：`agentteam` / `resume` 归位多代理板块（commit `1db3720d`）

**删除清单（原位置 → 新唯一位置）**

| 原位置 | 新位置 | 变化 |
|---|---|---|
| `application/core/agentteam/spec.go` | `backend/multiagent/team/spec.go` | `git mv`，包名 `agentteam` → `team`；代码 0 字节改动 |
| `application/core/agentteam/spec_plugins_test.go` | `backend/multiagent/team/spec_plugins_test.go` | 同上（只改 `package` 行） |
| `application/core/agentteam/README.md` | `backend/multiagent/team/README.md` | 加"落点 / 退场时间线"说明 |
| `application/core/resume/{resume.go,resume_test.go,README.md}` | `backend/multiagent/resume/…` | `git mv`，包名不变，代码 0 字节改动 |

**取用侧（只改 import 路径或注释，不改判据）**：`application/core/goal_team_recorder.go`、
`backend/engine/assemble/runtime_subagent_resume.go`、`backend/engine/assemble/runtime_role_turn_test.go`、
`store/teamwork.go`、`backend/multiagent/teamwork/{teamwork.go,role_session_id_consistency_test.go}`、
`application/README.md`、`application/core/README.md`。

**门禁同步**：`e2e/layout_test.go` 模块表 +2 行；`e2e/facade_boundary_test.go` 闭包清单两条换成新落点
（条数不变，上限 75 不动）。

**有意不改（口径同 D-M3-12"改名不追文案"）**：`team/spec.go` 的错误文案前缀仍是 `agentteam: …`；
`role_session_id_consistency_test.go` 的失败文案仍写 `agentteam=%q`。

**保真证据**：`git diff -M --numstat` 显示被搬的 6 个文件全为 `0 0`（纯 rename）——即同一个字节的代码
换了落点，改的只有包名行、import 路径与文档。

## M5-R8c：团队看板读面归位 `backend/multiagent/teamwork`（commit `ab852d2b`）

**删除清单（原位置 → 新唯一位置）**

| 原位置:行 | 新唯一位置 |
|---|---|
| `application/core/teamwork_service.go:24` `TeamworkBoardViewFor` 的方法体 | `backend/multiagent/teamwork/board.go` `BoardReader.BoardViewFor` |
| `application/core/teamwork_service.go:46` `TeammateSessionLiveFor` 的方法体 | 同文件 `BoardReader.SessionLiveFor` |
| `application/core/teamwork_service.go:63` `TeammateSessionLivePageFor` 的方法体 | 同文件 `BoardReader.SessionLivePageFor` |
| （`application/core` 侧三条方法的空视图判定各写一遍） | 收进 `BoardReader` 一处：看板 → `nil`；live → 回显会话号 + `Running=false`；分页 → 回显会话号与分页参数 |

`application/core/teamwork_service.go` 保留三条同名门面方法，各一行转发（`service == nil` 的守卫留在门面）。

**新增用例**：`backend/multiagent/teamwork/board_test.go` 三条——未装配（`nil` 宿主）、
宿主存在但不支持该窄端口、装配后逐字透传且不缓存。第二支此前**没有覆盖**（`application/core` 侧只
覆盖了真实宿主与 nil 两支）。

**保真证据**：方法体逐字搬运；核心判据（`contract.TeamworkBoardProjection` /
`contract.TeammateSessionProjection` 的类型断言）一字未改；无行为/文案/字段变化。

**验证（本机实跑）**：`gofmt -l .` 空；`go build ./...` 与 `-tags "gui,desktop,production"` 构建绿；
`go test ./e2e/ ./backend/multiagent/teamwork/ ./application/core/ -count=1` 中 e2e 与 teamwork 绿，
`application/core` 失败集合 = **基线 3 例**（与 `tmp/reshape/baseline-test.log` 逐条同名）。

**下一步（R8 余下，均为 `application/core` 根包的接收者转移 + 分包）**

- `role_session.go`（168 行 / 17 函数）→ `backend/multiagent/team/`；
  `teamwork_service.go` 余下部分与 `teamwork_service.go` 相关的写面 → `backend/multiagent/teamwork/`。
- 工具域：`review_files.go` + `reference_tools.go` → `backend/tool/{review,reference}/`；
  `tool_hooks.go`、`plan_tools.go` → `backend/tool/`。
- limit 域：`service_knobs.go` → `backend/limit/`；`session_permission_tier.go` → `backend/limit/tier/`；
  `service_account_window.go` → `backend/limit/accountwindow/`（该文件只剩编排：校验 → 写盘 →
  改内存 → 读回生效值，落点应是 `accountwindow` 的 use case + `store`/宿主两个窄端口）。
- 这些文件全是 `*Service` 方法，按 `m5-slices.md` §2 的两段式（先同包接收者转移、再分包）施工。
- 之后按 plan §2.5 搬整包：`task_context` / `context_runtime` / `session_runtime` / `chat` /
  `prompt_layer` / `input_router` / `context_control`。

## 11. M5 第二半（续）：R7a–R7h、R5d、R-limita（2026-10-16 本轮）

本轮把 plan §2.5/§2.11.1 里**还留在 `application/core` 的独立子包**整包归位完毕，并开了第一片
**接收者转移**（root 包 `*Service` 方法下沉）。所有片都是可独立回退的单一主题提交，逐片在
**独立 worktree**（`tmp/reshape/r7verify`，`GOWORK=off`）复核。

### 11.1 整包归位（纯搬迁：包名与限定名一字未动）

| 片 | commit | 原位置 → 新唯一位置 | 规模 | 门禁同步 |
|---|---|---|---|---|
| R7a | `821fad19` | `application/core/task_context/` → `backend/engine/task/` | 20 文件 / 50 进口方 | layout 1 条、subagent 词表 11 条、facade 闭包 1 条 |
| R7b | `4e139090` | `application/core/prompt_layer/` → `backend/engine/prompt/` | 4 文件 / 7 进口方 | layout 1、facade 1 |
| R7c | `36bc7dd5` | `input_router/` → `backend/engine/input/`；`context_control/` → `.../input/contextcontrol/` | 5 文件 / 10 进口方 | layout 2、facade 2 |
| R7d | `3c1206f6` | `application/core/chat/` → `backend/engine/react/` | 4 文件 / 9 进口方 | layout 1、facade 1 |
| R7e | `65f78ae9` | `application/core/context_runtime/` → `backend/engine/context/` | 17 文件 / 34 进口方 | layout 1、subagent 词表 1、facade 1 |
| R7f | `2ee49a70` | `application/core/session_runtime/` → `backend/engine/session/` | 22 文件 / 26 进口方 | layout 1、subagent 词表 3、facade 1 |
| R7g | `d7a778d5` | `seelexctx/`（根 + 8 子包）→ `backend/engine/context/seelectx/` | 80 进口方 / 136 文件变更 | layout 6、store 边界 1、facade 1、P10 白名单 1 |
| R7h | `ebaca93b` | `internal/promptassets/` → `backend/engine/prompt/assets/` | 10 进口方 | layout（原清单未登记，无命中） |

**为什么都是"只搬位置"**：每个目标包都不依赖父包 `application/core`（`go list -deps` 实测），
所以不需要接收者转移——进口方只改 import 字符串，限定名（`task_context.X` 等）一字未动。
被搬 README 的相对链接另有专门校准（见 11.4）。

### 11.2 接收者转移第一片：R-limita（`a3fe7f9e`）

| 项 | 内容 |
|---|---|
| 判据本体 | `application/core/service_account_window.go` → `backend/limit/accountwindow/setter.go` 的 `Setter.Set` |
| 门面 | `Service.SetAccountWindow` 保留为一行转发 + nil 守卫（外部调用点零改动） |
| 端口 | `Runtime contract.AccountWindowPort`（nil = 宿主未实现）、`Store contract.AccountWindowStore`（nil = 不落盘）——**结构满足**，无适配层；两个端口本就在契约层，符合 limit 板块"只依赖契约"的允许依赖 |
| 用例 | `application/core/service_account_window_test.go` → `backend/limit/accountwindow/setter_test.go`（git 判为重命名）；门面侧新增 `service_account_window_forward_test.go` 只钉 nil 守卫与端口断言（不重复判据） |
| 保真 | 错误文案逐字、次序（校验 → 写盘 → 内存 → 回读生效值）、写盘失败内存不动、返回值是宿主回读值，全部保留 |

### 11.3 判据归位：R5d（`e0c22ca6`，plan §2.4）

- `application/core/{error_codes.go, error_presentation.go}` → `middle/view/errors/`（包名 `viewerrors`）；
- **跨层判据下沉 `contract`**（新增 `contract/errors.go`：结构化错误码、`ErrReActBudgetExceeded` 哨兵、
  provider 失败分类 `ClassifyProviderFailure` 与 `IsProviderContextExhaustion`）。理由：plan §1.1 规定
  「前端 → 门面 → 中间层 → 后端 → 底座」，中间层不得 import 后端、后端不得 import 中间层，
  而这两处的判据同时被**中间层呈现**与**后端恢复策略**消费 ⇒ 只能落契约层。新增文件只用 stdlib，
  **不改 `contract` 的依赖闭包**（契约 ratchet 未动）。
- 删除的重复实现：`classifyPresentedError` → `viewerrors.Classify`；`presentToolError` → `viewerrors.PresentToolError`；
  `isProviderContextExhaustion` / `classifyProviderFailure` / `providerFailureKind` 常量 →
  `contract.*`（`history_safety.go` 与呈现层从此共享一份）；`task_context.ErrReActBudgetExceeded` → `contract.ErrReActBudgetExceeded`。
- 用例拆分：纯呈现用例 → `middle/view/errors/presentation_test.go`；Service 级用例 → `application/core/chat_error_surface_test.go`。
- 门禁：`e2e/layout_test.go` +1；`e2e/facade_boundary_test.go` +1 且 `facadeClosureAllowListMax` 75 → 76
  （按该文件既有"新模块可 +1、必须写清多了谁"的约定）。

### 11.4 施工纪律（本轮新增，后续切片必须遵守）

1. **并行编辑隔离**：工作树里有另一个 worker 在并行改 `application/core` 的若干文件（未提交）。
   直接 `git add <file>` 会把它的在途改动夹带进本片提交（本轮 R7b 首版就踩了，已用 plumbing
   重建 blob 修复并 amend）。纪律：**只 `git add` 显式路径列表**；对同时被并行改写的文件用
   `tmp/reshape/stage_clean.py` / `stage_blob.py`（父提交内容 + 本片替换，再 gofmt）重建 blob。
2. **被搬 README 的相对链接深度必须校准**：R7g 时 `seelexctx/README.md`（一层）搬到四层后
   `../docs/...` 全部失效，门禁 `TestRepositoryModuleReadmeLinks` 抓到。`tmp/reshape/fix_rel_links.py`
   按"解析不到就补一层 `../`、最多 6 层"自动校准；历史工作包里的失效链接**不动**（它们记的是当时事实）。
3. **换名/换落点后要扫"硬编码路径"的用例**：R7h 的 `test/tool_face_prompt_consistency_test.go`
   与 R7g 的 `e2e/session_not_found_single_criterion_test.go` 白名单都写死了旧路径。
4. **临时文件禁区**：`tmp/` 下的临时 `.go` 会被 `go build ./...` 捡起来（本轮踩过一次）——
   探针一律用 `.go.txt` 后缀或放进带嵌套 `go.mod` 的目录。

### 11.5 本轮结束时的实况与剩余

- `application/core` 的子包**只剩根包文件**（250 个 `.go`，含用例）：接收者转移是剩余工作的主体。
- 已完成的接收者转移只有 1 片（R-limita）。剩余按 plan §2.11.1 逐个做：
  `service_knobs.go`/`session_permission_tier.go` → `backend/limit/*`；`plan_tools.go`/`tool_hooks.go`/
  `reference_tools.go`/`review_files.go` → `backend/tool/*`；`role_session.go`/`teamwork_service.go` →
  `backend/multiagent/*`；`context_compact*.go`/`history_safety.go` → `backend/engine/context/`；
  `chat.go` → `backend/engine/react/`；`command.go`/`completion.go`/`input.go`/`service_input.go` →
  `backend/engine/input/`；`skill_activate.go`/`skill_context.go` → `backend/engine/skill/`；
  以及 R6（常驻/切换域）、R8 余项、R10（删 `aliases.go` + 门面收窄五组 + I5）、R11（删 `ports.go`、`Runtime` 按域下沉）。
- 工作树里仍有另一个 worker 的 R5b 半成品（`middle/view/worktable/{async,projection}.go` +
  `application/core/{work_table*,async_completion,session_cold_read,session_scope,service_*}.go`，19 个文件未提交）；
  本轮的每一片都**没有碰**它的文件，但它负责的 `e2e/subagent_status_vocabulary_gate_test.go` 更新尚未落地，
  所以**主工作树**的 `go test ./e2e/` 目前会因它而红；本轮的复核一律在独立 worktree 里做（那里没有它的在途改动）。

## 12. M5 第二半（续 2）：R-limitb / R-limitc / R8d（2026-10-16 本轮）

三片都是**接收者转移**（判据与状态搬到拥有它的模块，门面只留宿主适配 + 一行转发），
每片一个提交、逐片在独立 worktree 复核（`go build ./...`、`-tags "gui,desktop,production"`、
`go vet ./...`、`go test ./e2e/`）。

| 片 | commit | 原位置 → 新唯一位置 | 门面侧留下什么 | 端口（全部结构满足） |
|---|---|---|---|---|
| R-limitb | `ca010440` | `application/core/service_knobs.go` 的判据、守卫与状态 → `backend/limit/knobs.go`（`Knobs` + `KnobsHost` + `ValidateLoopLimit` / `ValidWireReasoningEffort` / `ReasoningEffortChoices`） | `serviceKnobsHost` 适配 + `SetReasoningEffort` / `SetLoopLimit` 一行转发 | `ViewSession` / `MutateRuntime` / `PublishRuntime` / `SetEngineMaxLoops` / `CurrentEffort` / `MaxLoopsForEffort` / `ReasoningEffortSink` |
| R-limitc | `93f93705` | `application/core/session_permission_tier.go` + `session_scope.go` 的档位三处 + `service_interaction.go` 的落地 → `backend/limit/tier/tier.go`（`Deps` + `EffectiveTier` / `Persist` / `Read` / `ApplyTier` / `ApplyStored` / `RestoreFor` / `SyncViewLocked` / `SyncGateFor`） | `service.tierDeps()` + `serviceTierUnits` / `serviceTierView` 薄适配 + 8 个一行转发 | `SettingStore`（↔ `session.SessionSettingPort`）/ `Units` / `AutoApproval` / `View` / `Gate` / `DefaultTier` |
| R8d | `65be4dfc` | `application/core/role_session.go` 的端口解析判据与 14 个转发 → `backend/multiagent/team/roles.go`（`RoleSessions`） | `service.roleSessions()`（两个可选能力的类型断言）+ 14 个一行转发 | 直接持 `contract.RoleSessionPort` / `contract.SchedulePort` |

**为什么不是"加一层转发"**：三片都把**判据**（错误文案与取值规则）搬到了拥有该规则的模块，
门面侧不再有任何第二份实现；端口全部是契约端口或既有窄接口，用**结构满足**接上，没有适配层。
证据是删除清单 + 编译器（改完门面侧同义实现即编译失败），不是"用例全绿"。

**R-limitc 推翻了 M2 的"有意不搬"**：M2 记录的理由是"档位**词表**不该搬进 limit"——那条前提仍然成立
（词表仍在 `contract/dto/permission.go`，前端与 limit 共消费）；M2 同时把"Service 的持久化与快照同步"
有意延后到 M5，本片正是那一步，`backend/limit/README.md` 的沿革段已改写，避免两处结论冲突。

**R-limitb 的加锁次序**（本次最容易做错的地方）：原实现的次序是 视图锁 → 组件锁（只单向），且读数的
**收集**刻意在锁外。转移后由 `KnobsHost.MutateRuntime` / `PublishRuntime` 保证"宿主在自己锁内调用
mutate、组件在 mutate 里取自己的锁"，次序不会倒；`knobs_test.go` 有对应用例钉住。

**新增用例的分工**（不重复判据）：

| 包 | 新增用例 | 打的是什么 |
|---|---|---|
| `backend/limit` | `knobs_test.go`（6 条） | 两条拉条的**宿主端口契约**：守卫返回视图会话 ID、档位回退、改上限在宿主锁内一次写完、下发即发布、无端口 no-op、租约一次性 |
| `backend/limit/tier` | `tier_test.go`（5 条） | 生效档位解析与回退、一次落地写三处、脏值不落槽但镜像快照、非视图会话不写快照、读写降级与失败语义 |
| `backend/multiagent/team` | `roles_test.go`（2 条） | 三种"没有可用端口"文案 + 两条原样转发 |
| `application/core` | 原有门面级用例**保持不动** | 端到端路径（Service → 快照/门/审批面/引擎） |

**门禁同步**：`e2e/layout_test.go` 模块清单 +1（`backend/limit/tier`）；`e2e/facade_boundary_test.go`
闭包清单 +1 且上限 76 → 77（按该文件"新模块可 +1、必须写清多了谁"的既有约定）。

**并行编辑隔离（第 3 次踩到）**：`application/core/session_scope.go` 同时被另一个 worker 改写
（它给该文件加了 `middle/view/worktable` import）。`git add <file>` 会把它的在途编辑一起提交，
所以本批对**同时被并行改写的文件**一律用 `tmp/reshape/stage_*.py` 从父提交内容重建 blob
（只施加本片替换、再 gofmt）后暂存；它的编辑既没被夹带、也没被回退。

**复核证据（本批结束时的实况）**：`go build ./...` 0、`-tags "gui,desktop,production"` 0、
`go vet ./...` 0、`go test ./e2e/` ok、`go test ./backend/limit/... ./backend/multiagent/... ./test/` 全 ok；
`application/core` 的失败集合 = 开工前就红的**基线 3 例**，无新增。

## 13. M5 第二半（续 3）：R-tool-review（`72bd5d12`）

| 项 | 内容 |
|---|---|
| 原位置 → 新唯一位置 | `application/core/review_files.go`（129 行）的落点本体 → `backend/tool/review/review.go`（`Requests` + `Host`） |
| 门面留下什么 | `serviceReviewHost`（workspace 读面解析 / 召唤 ID 派发 / 会话归属 / 视图锁内写请求 / 快照发布）+ `RequestFileReview` / `ClearReviewRequest` 两个一行转发 |
| 判据唯一 | `ClassifyReadError`（读失败 → `dto.ReviewSkip*` 的唯一分类表）、`NormalizeKey`、超限/空清单文案、`ProbeLimit`（64 KiB）都在新包，core 侧没有第二份 |
| 通道不变 | 裁决仍走 `contract.WorkspaceFilePort.ReadFile(root, path, ProbeLimit)`——与前端点开文件行同一个 Port、同一处 root，沙盒边界仍只在 workspace 域一处实现 |
| 锁纪律 | 裁决（含磁盘读）在锁外；写请求 + `bumpLocked` 由宿主在自己的视图锁内一次完成，发布在锁外（与迁移前同序） |
| 门禁 | `e2e/layout_test.go` +1；`e2e/facade_boundary_test.go` +1、上限 77 → 78 |
| 用例 | 新包 7 条（分类表 9 格 / 去重键 / 全跳过不碰宿主 / 无会话不发布 / 回退视图会话 / workspace 失败冒泡 / Clear 归属）；`application/core/review_files_test.go` 6 条端到端用例保持不动 |
| 登记偏差 | plan §2.6 把"工具注册那一半"（`RegisterReviewFilesTool`，签名绑在 `*assemble.Runtime`）也点进本包 → 随 R11（`Runtime` 按域下沉）一起搬，本轮只搬落点 |
| 复核 | 独立 worktree：`go build ./...` 0、`-tags "gui,desktop,production"` 0、`go vet ./...` 0、`go test ./e2e/` ok、`backend/tool/review` 与 `application/core -run Review` 全 ok |

## 14. M5 第二半（续 4）：R-skill（`998793e5`）

| 项 | 内容 |
|---|---|
| 原位置 → 新唯一位置 | `application/core/skill_activate.go`（33 行）的判据 → `backend/engine/skill/activate.go`（`Activator` + `ActivateDeps{Skills, Apply}`） |
| 门面留下什么 | nil 守卫（"service is not initialized"）+ 把 `Deps.Skills` 与 `applySkill` 交给落点的一行转发；`wiring/bootstrap` 的 activate 注入不变 |
| 判据唯一 | 名字 trim → 空名 / `goal` 不可激活 / 未知技能三条文案；**被拒的名字不调用落地动作**；命中时恰好落地一次并回传技能信息；未装配技能表给明确错误 |
| 端口结构满足 | `SkillLookup{ Get(name) (model.SkillInfo, bool) }` 由 `contract.SkillPort` 直接满足，无适配层 |
| 用例 | 新包 `activate_test.go` 3 条（拒绝不落地 / 归一与落地一次 / 未装配技能表）；`application/core/skill_activate_test.go` 的端到端用例（压层 + 下一次提交信任它 + 幂等）保持不动 |
| 文档 | `backend/engine/skill/README.md` 文件结构 + 测试命令（改掉过期的 `./skill`、`./plugin` 路径）+ 新增「文件与函数索引」节（该 README 原先缺这一节，顺带补齐 skill.go / loader.go / activate.go） |
| 登记偏差 | plan 同批点名的 `skill_context.go`（128 行）**未搬**：它的类型（`chatRequest` / `PromptLayer` / `ReActBudget`）属 ReAct/chat 域，须随 `chat.go → backend/engine/react` 一起动；单独搬会把 core 的聊天请求簿记拖进 skill 包。已登记，随 react 片处理 |
| 复核 | 独立 worktree：`go build ./...` 0、`go vet ./...` 0、`go test ./e2e/` ok、`backend/engine/skill` 与 `application/core -run 'Skill|Activate'` 全 ok |

## 15. M5 第二半（续 5）：R-tool-reference（`1650a2ca`）

| 项 | 内容 |
|---|---|
| 原位置 → 新唯一位置 | `application/core/reference_tools.go` 的判据 → `backend/tool/reference/reference.go`（`NodeResultRef` / `BuildPage` / `EncodePage` + UTF-8 助手） |
| 门面留下什么 | "从哪读"的编排与工具面接线：`ReadToolResultHandler` / `ToolResultContent` / `toolResultContent`（pending → 落盘 → 引擎桥节点归档）、`resolveToolResultRefAlias`（`result:call_` 别名要读任务域映射）、`nodeToolResult` / `hasToolResultRefLocked`、`ReadPlanHandler` |
| 判据唯一 | `BuildPage` 是 **GUI 与模型面共用**的那一份（同一份归档、同一页边界）；越界 offset 由 `EncodePage` 显式报错；UTF-8 边界对齐 + "一次一字节也要前进" |
| 用例随判据走（并去掉一处重复覆盖） | 迁入 `reference_test.go`：前缀解析表、JSON 形状、分页 UTF-8 边界（新增 0/4→0/3 与末页）、contains 过滤（含越界页）、"一次一字节也要前进"（**从 `session_archive_test.go` 迁入**，避免同一判据两处断言）；core 只留两条 Service 级集成用例（引擎桥 / `result:call_` 别名） |
| 门禁 | `e2e/layout_test.go` +1；`e2e/facade_boundary_test.go` +1、上限 78 → 79 |
| 复核 | 独立 worktree（`1650a2ca`）：`go build ./...` 0、`go vet ./...` 0、`go test ./e2e/` ok、`backend/tool/reference` 与 `application/core -run 'Reference|ToolResult|Plan'` 全 ok |
| 过程教训 | 首版把 `EncodePage` 的调用点切过去后，core 测试里仍引用旧函数（`reference_tools_test.go`、`session_archive_test.go` 各一处）→ 已按"用例随判据走"处理；另有一处遗留的未用 import 被独立 worktree 的 `go vet` 抓到（主工作树的 vet 先撞上另一个 worker 的在途错误，容易漏看）——**主工作树 vet 有并行噪声时，必须以干净 worktree 的 vet 为准** |
