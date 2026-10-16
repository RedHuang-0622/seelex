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
| R2 toolrecord → `store/record` | 未开始 |
| R5/R6 `application/core` 根文件按域切分（view/switch/residency） | 未开始（需 §2 两段式切分） |
| R7/R8 core 的 engine/tool/multiagent/limit 域归位 | 未开始（同上） |
| R9 `plugin` / `skill` / `mcpstack` → `backend/engine/*` | 未开始 |
| R10 `aliases.go` 删除 + 门面五组收窄 + I5 | 未开始 |
| R11 `assemble/ports.go` 删除 + `Runtime` 按域下沉 | 未开始 |

**Windows 偶发基线（本轮实测，不是回归）**：全量 `go test ./...` 在并行负载下会出现
`backend/tool/registry`（`TestJobHandlesCarryTheirTableToken` 的 TempDir 清理、进程句柄未放）、
`gui/terminal`（`TestManagerRealShellRoundTrip`）、`store`（`TestCommitNotBlockedByFullHistoryRead`）
三类超时/清理失败；**逐条单跑必过**，与环境负载相关，与本次搬迁无关。
