# 步骤③ 目标书：现场 / 进程树层的重复收口（只并本质重复）

状态：**本轮唯一口径文档**（leader 撰写；实现与本文件冲突 = 先回来改本文件，不许在实现里私下另立一套）。
适用范围：`seelebridge/worktree`、`seelebridge/tools`、`seelebridge/workunit`（分类那一格）+ `e2e/workunit_ports_test.go`（门禁）。
配套（事实源，按序读）：`workunit-duplication-inventory.md`（现状盘点）→
`workunit-single-lifecycle-one-implementation.md`（§5 证据口径 / §7 纪律 / §10 红灯）→
`workunit-ports-and-assembly.md`（§2 端口 / §5 三步计划）→ 本文件（本轮判据与派活）。

## 0. 基线与上游

- 基线：main 头 `8ddebaa`（含 ① 落地 `a6f8f2b`、② 口径纠正 `2323874`）；开工前 `git log --oneline -10` 核对，工作区干净。
- **② 尚未落地**：`2323874` 只是把 ② 收窄为「并判据 / 读法 / 容器」并写了交接提示词
  （`step-2-handoff-prompt.md`），代码未动。② 与 ③ 的交叠只有 §二 #1（恢复名单登记，本轮**不做**），
  因此 ③ 的 A–E **全部可以本轮做**。
- 环境：Windows + PowerShell；`bash_read` 会拒收含 `|` / `&&` / `cd` 的命令（改用 `bash` / `grep_search`）。

## 1. 目标（一句话）

把「现场清理 / 工作区脏判定 / 进程树装配 / 现场在册路径比较 / 契约包对 worktree 的错误依赖」这
五处**同一判据的第二份**收成一份；其中两处**已经真的伤过人**的缺陷先写红灯再修。
判据不是"用例全绿"，而是**只剩一份实现 + 删除清单**（AGENTS.md §8）。

## 2. 在更大目标里的位置

```
总目标：收紧重复实现 ──▶ 在基类上做多态（lifecycleHost 是唯一基类，见 §5）
  ├── ① 端口 / 装配 / 读面 / 机械门禁      ✅ 已落地 a6f8f2b
  ├── ② 并「判据 / 读法 / 容器」（折算三份、阶段编解码、恢复说明容器）  ⏳ 未落地
  ├── ③ 本文件：现场 / 进程树层的重复收口 + 两个红灯先行          ← 本轮
  └── ④ 之后（本轮只登记，不做）：#1 恢复名单登记、#7 teammate 恢复续跑、
          teamwork 九条测试与「并发合并 / 提交树分叉」回归、基类多态
```

## 3. 本轮做什么：A–E（锚点**已复核**，行号以现状为准）

> ⚠ 清单 `workunit-duplication-inventory.md` 写作时 main 头在 `746b00e` 一带，`worktree_manager.go`
> 的锚点整体**偏小 32–43 行**。下表是 2026-10-06 复核后的现状锚点，**先按本表改清单、再动手**。

| 项 | 现状（几份 × 锚点） | 并到哪 | 硬约束 |
|---|---|---|---|
| **A** 现场清理（删目录 + 删分支） | **2 份**：`worktree_manager.go:822 cleanup`（非幂等）／`:865 CleanupWorktree`（幂等） | 一份**幂等**实现（另一处变薄包装或删除） | 二处内部调用点 `:486`（无提交且干净）／`:510`（合并成功后）；外部调用点 `runtime_teamwork.go:820`、`runtime_teamwork_items.go:122`、`worktree_test.go:220` 全走同一份。**红灯先行**：连续两次清理不报错 + 「目录 / 分支 / git 登记任一不在 = 已释放」 |
| **B** 「工作区是否脏」判定 | **3 份**：`:731 pathDirty`／`:792 worktreeDirty`（转发 `wt.Path`）／`runtime_teamwork.go:860 worktreeDirty`（包级，调用点 `:809`） | 一份（`pathDirty` 唯一实现），其余转调 | CRLF 幻影脏的**唯一修复点 = `pathDirty` 里那一行 `git status --porcelain`**（唯一，必须写清行号） |
| **C** 进程树装配序列 | **2 份**：`tools/async_run.go:244 startAsync`／`tools/router.go:554 newScopedCommand` | 一份装配助手（`NewProcessTree` + `ConfigureProcessTree` + `cmd.Cancel` + `Attach`） | **只收树的装配**；超时 / 取消策略各留各的（后台 `WithoutCancel` + `asyncHardCap` vs 同步 `scopedToolTimeout`），并写清"为什么像却不并" |
| **D** 「现场是否在册」路径比较 + 命名 | **2 份口径**：`:581 Restore`（只用 `os.Stat`，不比较）／`:620 sceneRegistered`（走 `worktreePathEqual:921`）；命名两处拼 `:371 scenePath` / `:725 isManagedPath`；另一份 worktree 清单解析 `:898 gitWorktreeRegistered`（人读格式）与 `:695 listWorktrees`（porcelain） | 一份 `worktreePathEqual` + 一个命名常量 | **保留「目录不存在不登记」的防幽灵语义**（`:581` 的 `os.Stat` 判据不许被顺手删掉）；`Restore` 的路径**必须**改成规范化比较 |
| **E** 契约包对 worktree 的错误依赖 | `workunit/classify.go:38,40` 用 `worktree.IsUncommittedChanges` / `worktree.IsMergeBlockedByMain`；门禁唯一例外项（`e2e/workunit_ports_test.go` 的 `worktreeDependentsInWorkunit`） | 哨兵 + 两个判据搬进契约包 `seelebridge/workunit`；`worktree` 侧留**别名 / 薄包装**（公共 API 不变）；撤掉门禁例外项 | 不许改 `worktree` 的公共 API：只允许「移定义 + 留别名（`var ErrX = workunit.ErrX` / 薄包装函数）」这一种等价形态；既有用例（`workunit/contract_test.go`、`worktree_merge_serial_test.go` 等引用 `worktree.ErrUncommittedChanges`）**一条不动** |

## 4. 非目标（做了即越界）

- **#1** 恢复名单登记（与 ② 交叠，"认领先于 `Prune`"是硬顺序）；**#7** teammate 恢复续跑（要过 leader 闸门/屏障/在编校验）；
  **#8** 作业面合表（`teamwork-leader-worker-architecture.md` §12.4 已裁决关闭）。
- 不碰前端读面渲染；不改 Seele `jobs` 包；不动 `seelebridge/worktree/**` 里与 A–D 无关的行为语义；
  不给基类加端口、加分支、加具体类型（见 §5）。
- 不改既有用例（只允许新增）；不 `commit` 到主分支以外的分支策略、不 `push`（提交纪律见 §9）。

## 5. 基类体检（防上帝）——本轮必须回答

**基类 = `seelebridge/workunit_parent.go` 的 `lifecycleHost`**（唯一一份生命周期实现，装配处 `workunit_assembly.go`）。

现状读数（2026-10-06 复核）：

| 读数 | 值 |
|---|---|
| 端口字段 | **3 格**，全部不导出：`scenes sceneFace` / `units unitFace` / `records recordFace` |
| 方法面 | 契约 6 个（`Begin` / `AlreadySettled` / `Finish` / `Reclaim` / `Recover` / `Notice`）+ `teamOwned` + `newLifecycleHost` |
| 具体类型 | 文件内 0 个（`teamwork` / `worktree` / `sessionstore` / `session` 都不出现，机械门禁钉住） |
| 按层分支 | 0 个（唯一"按层判断"是**归属读数** `teamOwned`） |

**本轮判据（可机械查）**：③ 一个字节都不许进基类——

```
git diff --stat 8ddebaa..HEAD -- seelebridge/workunit_parent.go seelebridge/workunit_assembly.go
```

必须**空输出**。非空 = 派活越界，回退重做。

**预警阈值（登记，本轮不处理；触到就停手向用户报告，另开一波拆）**：基类出现第 4 格端口 /
任何 `if kind ==` / `if role ==` / 单文件 > 400 行 / 端口字段被导出。

**多态路线的先决条件（登记）**：基类方法面稳定 + 端口可换（换一个 `Lifecycle` 实现 = 改装配处一行 +
编译期断言先红）。本轮**不做多态**，只保证基类不被撑大——③ 的 A–E 全在具体层，与基类多态正交。

## 6. 编排（milestone / work item / teammate）

```
M1（并行）                                     M2（屏障后）
  wi-1 worktree 内部收口（A+B+D）──┐
  wi-2 工具链进程树装配收口（C）   ├─▶ 复核通过 ─▶ wi-3 契约依赖撤例外（E）
  wi-4 审计：U2/U5/U6 + 锚点刷新  ─┘
```

| 里程碑 | work item | teammate | 交付物 | 依赖 |
|---|---|---|---|---|
| `m1-scene-tools-dedup` | `wi-1-worktree-dedup` | `worktree-dedup` | A+B+D 一份实现 + 红灯用例 + 删除清单 | — |
| 同上 | `wi-2-tools-proctree` | `tools-proctree` | C 一份装配助手 + 不并超时的理由 + U5 证据 | — |
| 同上 | `wi-4-audit-u2u5u6` | `audit-u2u5u6` | U2/U5/U6 逐条结论 + 清单锚点刷新 | — |
| `m2-sentinel-to-contract` | `wi-3-sentinel` | `sentinel-mover` | E：哨兵搬入契约包 + 别名 + 门禁撤例外 | `m1` |

**文件独占**（同一文件不许两个 work item 同时动，防止合并冲突）：

| work item | 独占文件 |
|---|---|
| wi-1 | `seelebridge/worktree/worktree_manager.go`、`seelebridge/worktree/README.md`、新增 `seelebridge/worktree/*_test.go` |
| wi-2 | `seelebridge/tools/async_run.go`、`seelebridge/tools/router.go`、`seelebridge/tools/README.md`、新增 `seelebridge/tools/*_test.go` |
| wi-3 | `seelebridge/workunit/*.go`、`seelebridge/workunit/README.md`、`e2e/workunit_ports_test.go`、`docs/arch/workunit-ports-and-assembly.md`、`worktree_manager.go` 的**别名块**（仅 A–E 的哨兵那几个符号） |
| wi-4 | `docs/arch/workunit-duplication-inventory.md`、新增 `docs/2026-10-06-workunit-jobs-port/step-3-audit.md` |

复核与收口由 leader 本人做（无 review 里程碑、无 `tl` 席位）；`team_close` 是唯一回收点。

## 7. 交付判定 A–H（缺一条不算完成）

- **A** 现场清理：只有一份实现（唯一位置 + 旧位置去向）；红灯用例在仓——连续两次清理不报错，
  且「目录 / 分支 / 登记任一不在 = 已释放」，给**先红后绿**的红灯原文。
- **B** 脏判定：3 → 1 的删除清单；CRLF 幻影脏的唯一修复点在**哪一行**。
- **C** 进程树：2 → 1 的删除清单；写清**为什么不把超时策略也并进去**。
- **D** 在册判定：2 → 1 的口径统一；**保留「目录不存在不登记」的防幽灵语义**。
- **E** `grep -rn "worktree\." seelebridge/workunit/` 只剩契约自己声明的记录形状与注释；门禁例外项撤掉。
- **F** 原始读数：`gofmt -l`（改动文件）/ `go build ./...` / `go vet ./seelebridge/...` /
  `go test ./e2e/ -count=1` / `go test ./seelebridge/worktree/ ./seelebridge/tools/ ./seelebridge/teamwork/ ./seelebridge/... -count=1`。
- **G** **删除清单**（文件:行 → 新唯一位置；旧位置去向：删除 / 薄包装）——只加不改的交付一律退回。
- **H** 未决项：U2（teammate 现场是否被重复登记）/ U5（进程树退化路径两条链对外主张是否一致）/
  U6（在跑状态字面量残留点）逐条回答或标记"仍开放"。

## 8. 红灯先行（两处已经伤过人的缺陷）

1. **缺陷 A：清理不幂等**（`exit status 128 / '<path>' is not a working tree`，原始记载在
   `worktree_manager.go:852-864` 的 `CleanupWorktree` doc 注释里）——先写会红的用例：
   **连续两次释放同一份现场不许报错**，且三种「已释放」形态（目录不在 / 分支不在 / git 登记不在）
   各一条；再改实现；再绿。旧用例 `worktree_vanished_scene_repro_test.go` 已钉住"现场被对端收走时
   `Finish` 必须报错"——**这一条不许被幂等口径抹掉**（幂等只覆盖"释放动作"，不覆盖"收尾段拿不到现场"）。
2. **在册比较口径**（`:581 Restore` 只用 `os.Stat`，与 `sceneRegistered` 的规范化比较不一致，
   是"恢复出来的现场仍被 `Prune` 当孤儿删掉"的第二个成因）——先写会红的用例（同一现场路径的两种
   写法：`/` 与 `\`、盘符大小写不同），再统一到 `worktreePathEqual`。

## 9. 纪律（派活逐条带上）

- AGENTS §0 危险操作铁律：先读 `MEMORY.md`、先中文预警、先确认；不得读取或提交
  `config/accounts.yaml` 与 `*.local.yaml`；不得执行 `make clean/release`、`git clean/reset --hard`、
  删除 `dist/` 或 `.seelex/` 的命令。
- AGENTS §8：一份判据 = 一处函数；只并本质重复（写得清"为什么像却不并"）；交付必须列删除清单；
  证据是"只剩一份实现 / 编译器钉住"，不是"用例全绿"。
- MEMORY.md「错误修复纪律」：先复现（会红的用例）→ 只改根因相关代码 → 重跑复现（红→绿）→ 沉淀回归用例。
- 既有用例一条不动（动了就是越界）；不混入无关文件；不顺手格式化基线未 `gofmt` 的文件。
- 桌面纪律：不置顶 / 不覆盖窗口、不抢前台、不合成键鼠；作业输出一次性取全（销项后日志会被删）。
- 提交：在自己 worktree 内按主题 `git add -A && git commit`（合并由框架收尾时自动做），**不 push**、
  不切主分支、不动主工作区；发现红线（要改公共 API 才能并 / 锚点与现状冲突）→ 停手写清并回报，
  不要硬做。

## 11. 并行波：步骤②（并"判据 / 读法 / 容器"，**不并两张作业表**）

③ 与 ② 的文件面几乎不交叠（③ = `worktree/**`、`tools/**`、`workunit/classify.go`+门禁；② = `workunit/progress.go`、
`session/subagent_sessions.go`、`workunit_team_records.go`、`runtime_subagent_resume.go`、`workunit_team.go`），
因此 ② 与 ③-M1 **并行**；唯一共享文件是 `seelebridge/workunit/README.md`（② 改读面那一行、③-E 撤例外段），
所以 ② 排在 ③-E **之前**（里程碑屏障：`m2-sentinel-to-contract` depends_on `m1` **与** `m3`）。

### 11.1 裁决（先接受，否则白干一轮）

`docs/arch/teamwork-leader-worker-architecture.md` **§12.4**：把异步面/作业面迁到 Seele `jobs.Manager`
**已走过并认定失败**（`event.Sink` 必须在 `jobs.New` 构造期定死；框架 `event.Recorder` 是单例 + 全局序号，
而 Seelex 会话事件库按会话追加/排序 → 作业事件 append 不到会话事件流尾部）。原文结论：
「本步不再开工，也不必等 §12.3 的契约增补；§12.3 降级为留档，不构成待办。」
因此 ② 的口径是：**不并表、不迁 Seele、不反向合一**（反向合一 = 让 teammate 搬离 `jobs.Manager`，
会掏空框架的 `jobs_manage`，同属重新裁决）。两张表只登记为**分层事实**（`workunit/README.md` 依赖方向段），
不再当缺陷。

### 11.2 范围与判据（任务书 §5② 四行）

| # | 重复 | 今天几份 | 并到哪 | 硬约束 |
|---|---|---|---|---|
| ① | 「记录 → 进度」折算 | 3 份手写（`session/subagent_sessions.go` 写侧 `buildRecordLocked` / 读侧 `restoreLocked`、`workunit_team_records.go`） | `workunit.ProgressOf`（① 已定形，**今天没有生产调用者**） | 改的是落盘/回读路径 = 行为迁移：**先写红灯**（跨层一致性：同一份收尾脚本在 subagent 与 teammate 上跑出同一份 `Stage` 序列） |
| ② | 阶段 JSON 编解码 | 2 份 | `workunit.EncodeStages`·`DecodeStages` | 同上 |
| ③ | teammate 阶段打点恒单元素 | 1 处语义缺口（`workunit_team_records.go` 的 `teamUnitStages`） | 走同一套 `Stage` | 会改回灌文案（勘定 U3/U4 要复验）；或写清为什么保留 + 判据 |
| ④ | 恢复说明容器 | 2 份同形状 map（`runtime_subagent_resume.go` / `workunit_team.go`） | `workunit.RecoveryNote`（已有） | 只收容器（`set` / `consume` / `clear`）；构建器已经是唯一一份 |

判定（缺一条不算完成）：**A** `ProgressOf`/`EncodeStages`/`DecodeStages` 有生产调用点（给文件:行），
三处旧体删除或转调；**B** 跨层一致性用例在仓 + **先红后绿**原文；**C** teammate 阶段打点不再恒单元素
（或写清为什么保留 + 判据，U3/U4 正面回答）；**D** 恢复说明容器只剩一处；**E** 既有用例一条不改；
**F** 原始读数（`gofmt -l` / `go build ./...` / `go vet ./seelebridge/...` / `go test ./e2e/ -count=1` /
`go test ./seelebridge/... -count=1`）；**G** **两张作业表一动没动**（`git diff --stat` 里没有它们，
作业端口形状也没被改成"为合表服务"）；**H** 勘定 U2/U3/U4/U6 逐条回答或标"仍开放"。

非目标：不合表 / 不迁 Seele / 不反向合一；不碰前端读面渲染；不碰调度闸门；**不动 `seelebridge/worktree/**`**；
不改 Seele 的 `jobs` 包；**不顺手改** `workunit/classify.go` 的 `worktree` 依赖（那是 ③-E 的活，排在 ② 之后）。

### 11.3 编排（并行波，与 m1 同时开工）

| 里程碑 | work item | teammate | 交付物 | 依赖 |
|---|---|---|---|---|
| `m3-step2-read-surface` | `wi-5-progress-unify` | `progress-unify` | 折算 3→1 + 编解码 2→1 + teammate 打点恒单元素 + 跨层一致性红灯用例（② 的 ①②③） | — |
| 同上 | `wi-7-resume-note-unify` | `resume-note-unify` | 恢复说明容器 2→1（② 的 ④） | `wi-5`（同文件同区域，必须串行） |
| `m2-sentinel-to-contract` | `wi-3-sentinel` | `sentinel-mover` | ③-E | `m1` **与** `m3` |

文件独占（2026-10-06 复核后改正——初版把 ④ 划成"与 wi-5 无交叠"是错的）：

- `wi-5` = `seelebridge/workunit/progress.go`、`seelebridge/workunit/README.md`、`session/subagent_sessions.go`、
  `seelebridge/workunit_team_records.go`（折算 + 阶段打点部分）、新增用例。
- `wi-7` = `seelebridge/runtime_subagent_resume.go`、`seelebridge/workunit_team_records.go`（**只动 193–244
  的恢复说明容器段与 :319 的写入调用**）、`seelebridge/runtime.go`（只动 :226/:230 两个字段声明）、新增用例。
- 交叠点 = `seelebridge/workunit_team_records.go`：`teamResumeState` 与它的 set/consume/clear 就住在
  该文件 193–244，而 `:319` 的写入点落在 wi-5 要改的 `saveTeamUnitRecord` 区域 → **同一文件同一区域，
  只能一个写者**，故 ④ 排在 wi-5 之后（不是"顺手并行"）。
- `wi-3`（③-E）排在 `m1` 与 `m3` 之后，因此 `seelebridge/workunit/README.md` 同一时刻也只有一个写者。
- 跨波约束：`seelebridge/runtime_teamwork.go` 归 ③ 的 `wi-1`；② 侧不许改它——代价是
  `SubagentResumeNote` / `consumeTeamResumeNote` / `clearTeamResumeNote` **方法名必须保留**
  （:698/:848 的调用点因此一个字不用改）。

### 11.4 并行性裁定（② 与 ③ 能不能并行）

**结论：③-M1 与 ② 可以并行；③-E 与 ② 不能并行（已用屏障排开）。**

1. **机械面（文件级，已 grep 复核）**：③-M1 = `seelebridge/worktree/**` + `seelebridge/runtime_teamwork.go`
   （`worktreeDirty` 两处）+ `seelebridge/tools/**`；② = `seelebridge/workunit/progress.go` +
   `session/subagent_sessions.go` + `seelebridge/workunit_team_records.go` + `seelebridge/runtime.go` +
   `seelebridge/runtime_subagent_resume.go`。两者**没有同一个文件**（初版以为 `runtime_subagent_resume.go`
   与 `workunit_team.go` 装 ④ 的容器——实测容器在 `workunit_team_records.go:195–244`，
   故 ② 内部必须串行，见 §11.3）。
2. **语义面**：② 要的契约（`ProgressOf` / `EncodeStages` / `DecodeStages` / `RecoveryNote`）在步骤① 已落地并有
   编译期断言，② 不消费 ③ 的任何产物；③-E 改的是 `workunit/classify.go` 的哨兵依赖（同包不同文件），
   不触碰 ② 用的符号。→ 无逻辑依赖，纯文件面约束。
3. **不能并的那一段**：③-E 与 ② 同在 `package workunit`，且要改 `workunit/README.md` 与 e2e 门禁
   （② 新增的用例也要过那道门禁）→ 屏障排在两者都 done 之后。
4. **并行不等于可以不验合并面**：② 与 ③ 都在"恢复/回灌"这条路上（③ = `worktree.Restore` 在册口径 +
   脏判定；② = 记录→`Stage` 折算 + 恢复说明容器）。**各自分支绿 ≠ 合并后绿**，所以右腿验证必须跑在
   合并后的 main 上（`go test ./e2e/ ./seelebridge/... -count=1` + 我本人复核），这一步是 leader 的活。

## 12. 交付格式（leader 汇总时用）

1) 改动文件清单；2) **删除清单**（文件:行 → 新唯一位置；旧位置去向）；3) 每条的红→绿原文（A 必须有）；
4) 命令原始读数；5) 「只剩一份实现」的证据（每条：唯一位置 + `grep` 读数）；6) 未决项（U2/U5/U6 逐条）。
