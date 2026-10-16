# M3 实施记录：store 归位与粒度确认

日期：2026-10-16
执行者：`/root/m3_store`（架构重排长任务的 M3 执行子代理）
上游依据：`plan.md` §1.7、§2.9、§2.11.3、§2.12.1/2、§4 M3、§5；
`docs/2026-10-15-architecture-reshape/m3-store-design.md`（设计稿 A，492 行）；
`tmp/reshape/m3-design.md`（设计稿 B，631 行，输出者 = `m2c4_exec`）

两份设计稿独立得出同一结论：plan §2.9 的"`sessionstore` 切 8 个子包"**不可能靠搬文件完成**。
本记录第 2 节把它登记为 **D-M3-1**，并写明 M3 的实际落地范围。

## 0. 基线与验收口径（改动前实测）

| 项 | 实测（2026-10-16，工作区） | 命令/方法 |
|---|---|---|
| `sessionstore/` 生产文件 | **57** | `Get-ChildItem sessionstore -File -Filter *.go` 排除 `*_test.go` |
| `sessionstore/` 生产行数 | **15,936** | `Get-Content <file> \| Measure-Object -Line` 求和 |
| `sessionstore/` 测试文件 | **83** | 同上（设计稿 A 记 84，见 D-M3-3） |
| `workspace/` 生产文件 | **8**（1,616 行） | 与 plan §2.9 的 8 文件 / 1,616 行**逐字吻合** |
| `workspace/` 测试文件 | 8 | —— |
| `sessionstore` 的全部 Seelex 内依赖 | **只有 `seelexctx/lifecycle` 一条** | `go list -deps ./sessionstore` |
| import 了 `seelex/sessionstore` 的文件 | **174** | `rg -l 'seelex/sessionstore' --glob '!vendor/**' --glob '!tmp/**' --glob '!docs/**'` |
| 门面闭包 ratchet 上限 | `facadeClosureAllowListMax = 69` | `e2e/facade_boundary_test.go` |
| 契约层闭包 ratchet 上限 | `contractClosureAllowListMax = 28` | 同上 |

**验收硬指标**（plan §4 M3 退出条件）：持久化格式、数据根路径、锁文件、module head 语义
**字节级不变**；会话恢复 / 并发 / 切换类回归全绿；`store` 无向上依赖；写放大有可复现对照；
未提交批次的崩溃语义有测试钉住。

## 1. 阶段拆分与本轮状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| **S1** | 分组登记纠偏（只改记录：本文件） | **已完成**（§2 偏差登记） |
| **S2** | `store/root` 抽取（真机制下沉为纯标准库包 + 保形别名） | **已完成**，证据 §4.1 |
| **S3** | 边界门禁 `e2e/store_boundary_test.go`（I6：store 无向上依赖） | **已完成**，含 3 条变异验证，证据 §4.2 |
| **S4** | 删掉唯一一条向上依赖（`sessionstore → seelexctx/lifecycle`） | **已完成**（改为断言整条上移消费侧），证据 §4.3 |
| **S5** | 路径归位：`sessionstore/` → `store/`（单包）、`workspace/` → `store/workspace/`；两条既有闸同步 | **已完成**，证据 §4.4 |
| **S6** | 回归 + 文档（`store/README.md` 分卷 + 10 卷、`store/root/README.md`、现行文档路径） | **已完成**，证据 §4.5 |

## 2. 偏差登记

### D-M3-1（需复核者点头）：plan §2.9 的"8 路分包"在 M3 不可执行

**证据（实跑）**：三个"上帝类型"的方法声明散在同包多个文件里，Go 规定**方法必须与类型同包**：

| 类型 | 声明其方法的文件数 | 方法数 |
|---|---|---|
| `Router` | —— | 131 |
| `jsonRepository` | —— | 82 |
| `storeEngine` | —— | 182 |
| 合计 | 33 / 57 生产文件 | **395** |

再加"root 组 205 个未导出顶层声明里，有 57 个被其它文件组直接引用"（`role_session.go` 用
`storeEngine` 构造嵌套存储、`media.go` 同时挂 `storeEngine`/`jsonRepository`、`node_session_store.go`
全部方法挂在 `jsonRepository` 上…）。把 8 组拆成 8 个 Go 包 = **导出这 57 个内部符号 + 重建跨包
引擎/锁句柄**，属"改内部 API 形状"，不是物理归位。

**本轮落地范围**：抽 `store/root`（真正无域的机制：数据根锁 / 进程存活 / 共享读 / 原子写 /
崩溃尾 / JSONL IO / Settings / hash·randomID），其余保持**单包**。8 路分包的**前置是拆三个类型**
（395 个方法改归属 + 跨 110 个外部生产文件调用点），列为后续里程碑，不在 M3。

### D-M3-2：`store/` 单包（方案 A），不改 8 子包；README 用分卷承接

依据同上，外加门面闭包 ratchet 规则（`e2e/facade_boundary_test.go`）：单包是**一条出、一条进**
的等量替换，规模不变；8 子包会让清单净增 7 条以上并触发上限调整。README 按
`docs/arch/readme-spec.md` 的"大包按文件前缀分卷"处理。

### D-M3-3：plan §2.9 的文件清单与工作区不符（登记纠偏）

| 项 | plan | 实测 |
|---|---|---|
| `sessionstore/session_storage.go` | 列为存在 | **仓库里不存在**（`rg --files -g 'session_storage.go'` 只命中 `application/core/session_storage.go`） |
| `sessionstore/tool_record.go` | 未列 | **存在**（2026-10-15 新增的工具记录落点） |
| 测试文件数 | 未列 | 83 |

**结论**：文件级落点表一律按工作区生成，不照抄 plan §2.9。

### D-M3-4（需复核者点头）：工具记录**保留独立 JSONL**，不改为走 module head

plan §2.12.2 的裁决是"落点 = `store/session/` 下的一个 module（走 `publishModuleHead`）"。
现状是会话目录内的独立 append-only JSONL（`<sessionRoot>/tool-record.jsonl`，**一行 = 一批整份快照**，
残尾/坏行跳过）。该通道只有**一个写者、一个读者、每批一行**，"最后一行即真相"不需要水位；
head 机制的价值在"多处追加的通道要被读者一致地看到"。改通道语义属于行为面改动，
与 plan §5.1"只做物理归位"冲突，因此 M3 保留现状并在此登记；若复核者要求统一，
列为 M4 的 `store/record/` 项一起做。

### D-M3-5：`store/root` 的包名（`root`）与局部变量 `root` 遮蔽 —— 用 import 别名规避

`store/` 内有大量局部变量/字段叫 `root`（`storeEngine.root`、`jsonRepository.root`、
`acquireDataRootLock(root string, …)`）。因此 `store/*.go` 一律用别名
`storeroot "…/seelex/store/root"` 引用该包。包路径保留 plan §2.9 的 `store/root`。

### D-M3-6：`store/root` 的实际导出面按**跨包使用方**实测，不等于设计稿 B 的 13 项

设计稿 B §2.3 给了 13 项。实测（`rg -w` 全仓调用点）里，"留在 `store` 的文件 / 测试"仍在调用
`resolveStorageSettings`(11)、`defaultStorageSettings`(13)、`validateStorageSettings`、
`mergeStorageSettings`、`shardRows`、`applyWireBudget` —— 这些都必须导出，否则跨包引用不通。
**最终清单以本轮实测的编译期错误为准，逐条登记在本文件 §4。**

### D-M3-7：`sessionstore/` → `store/` 的目录改名放在 M3（设计稿 B 曾建议推迟）

设计稿 B §0.2 建议保留目录名 `sessionstore/`，把改名留到类型分解那步（"一次一主题"）。
本任务书明确要求 M3 做到 plan §1.1 的底座形态 `store/`，因此按任务书执行：
S2（抽 `store/root`）与 S5（目录改名）**分两个独立可回退的批次**，不叠在同一步里；
改名批次只做"路径 + 包名 + import"，不夹带任何行为改动。

## 3. V4 / V5 / V10 结论

- **V4（`store/{board,team,role,node}` 与 `store/session` 的读侧边界）**：**不做第二刀**。
  读侧与写侧同包、共用受锁保护的 module head 读面；栈通道的读投影是写路径发布的
  不可变 `atomic.Pointer[stackView]`（读者不取锁、不开句柄）。拆 `store/reads` 会制造第二份读判定。
- **V5（`SessionGranularStore` 与 `Router` 的并发关系）**：**`Router` 今天仍是唯一实际写者**。
  `SessionGranularStore`（46 方法）与 `SessionContextStore`（29 方法）都是它的包装
  （`SaveCommit`→`Router.SaveCommitWorkspace` 等）。因此"M3 迁移写路径"没有可做的搬迁，
  它是类型分解阶段的语义收敛项。
- **V10（工具记录看板读数范围）**：实现已是**会话粒度台账 + 进程内实时读数**（即事实上的方案 (b)），
  但 `contract/dto/tool_record.go` 的包级 doc 仍写"进程级全局台账 / 不进 Snapshot"——文档漂移，
  M3 按实现对齐注释；"是否进 Snapshot"仍待用户裁决，M3 不改快照协议。

## 4. 各步证据（实跑输出）

### 4.1 S2 `store/root` 抽取

落点：新包 `store/root`，9 个 `.go`：`doc.go`、`atomic.go`（`WriteAtomic`/`Hash`/`RandomID`/
`RemoveAllWithBackoff`/`renameBackoff`）、`data_root_lock.go`、`jsonl_io.go`、
`process_alive_{unix,windows}.go`、`file_shared_read_{other,windows}.go`、`storage_settings.go`。

**"只换包与导出名"的结构证据**（脚本 `tmp/reshape/m3s2_identity_check.py`：把 HEAD 版本按同一
改名映射归一化后逐行 diff）：

| 文件 | diff 行数 |
|---|---|
| `data_root_lock.go`、`process_alive_*.go`、`file_shared_read_*.go`、`jsonl_io.go` | **0**（归一化后与 HEAD 逐字相同） |
| `storage_settings.go` | 3 处 hunk：doc 注释搬到 `Settings` 上、`RetentionModeManual` 常量搬入、`applyWireBudget` 切出 |

**结构证据**：

```text
$ go list -deps ./store/root | 非 stdlib 包
github.com/RedHuang-0622/seelex/store/root        # 只有它自己（闭包 = 纯标准库）
$ rg -l 'seelex/store/root'                       # 除 store/** 外无引用者
```

**保形别名**（`store/root_aliases.go`）：`Settings`/`storageSettings`（类型）、
`retentionModeManual`（常量）、`NewSettings`、`ErrDataRootLocked`/`ErrDataRootStaleLock`、
`DataRootLockedBy`/`DataRootLockedByWith`（同一值的转发）——因此 `main.go` 与既有调用点
**零改动**；实现内部一律直接调 `storeroot.X`。

**验证**：`go build ./...` 通过；`go vet ./store/...` 通过；`go test ./store/ -count=1`
（原 `sessionstore` 的全部用例）**75.8s ok**。

### 4.2 S3 I6 门禁（`e2e/store_boundary_test.go`）

两条判据：① 底座闭包（`go list -deps`）里的 Seelex 包只允许是自身/`store/**`/登记条目；
② **测试文件**的直接 import 同规则（`go list -f {{.TestImports}}{{.XTestImports}}`）。
允许清单是 ratchet（当前**为空**，上限 0）。

**变异验证**（脚本 `tmp/reshape/m3s3_mutation.py`，每次改动后哈希校验已还原）：

| 变异 | 期望 | 实测 |
|---|---|---|
| A `store/root` 里 `import _ "…/seelexctx/tokens"` | 红 | ✅ 红（`store_boundary_test.go:77`） |
| B `store/` 的测试文件 `import _ "…/seelexctx/search"` | 红 | ✅ 红（第二条判据） |
| C 把待清理条目从允许清单删掉（清单不能是摆设） | 红 | ✅ 红 |

三条都咬；还原后两条判据重新全绿。

### 4.3 S4 唯一一条向上依赖的删除

- `store/router_storage.go`：删掉 `seelexctx/lifecycle` 的 import 与 `var _ lifecycle.Storage[…]`；
  方法集不变（`Append`/`ReadRange`/`Count`），doc 说明依赖方向与原因。
- 断言整条**上移到消费侧**：新增 `seelexctx/router_storage_conformance.go`
  （`var _ lifecycle.Storage[types.Message] = (*store.RouterStorage)(nil)`，生产代码、
  编译器钉住）；集成用例 `seelexctx/router_storage_conformance_test.go`（原
  `store/router_storage_test.go` 的 `TestRouterStorageWithContextActor`，改为用
  `store.NewRouter` 公开构造）。
- **为什么不是设计稿 A 的"契约层通用端口"**：`contract/ports.go` import
  `seelexctx/search` → `seelexctx/memory` → `store`，因此 `store → contract` 会立刻成环。
  实测证据（临时探针 `store/zz_cycle_probe.go`，已删除）：

  ```text
  imports github.com/RedHuang-0622/seelex/contract from zz_cycle_probe.go
  imports github.com/RedHuang-0622/seelex/seelexctx/search from ports.go
  imports github.com/RedHuang-0622/seelex/seelexctx/memory from search.go
  imports github.com/RedHuang-0622/seelex/sessionstore from memory.go: import cycle not allowed
  ```

- 变异验证：把 lifecycle 断言加回 `store/router_storage.go` → I6 **红**（脚本
  `tmp/reshape/m3s4_probe.py`，已还原）。
- I6 的 `store`（原 `sessionstore`）scope 允许清单**归零**。

### 4.4 S5 路径归位（改名）

- **搬迁**：`sessionstore/`（134 个 `.go` + `README.md`）→ `store/`；
  `workspace/`（16 个 `.go` + `README.md`）→ `store/workspace/`。包名：`sessionstore` ⇒ `store`；
  `workspace` 包名不变。
- **引用改写**（脚本 `tmp/reshape/m3s5_rename.py`）：import 路径 + 限定符 + 现行文档；
  实测 270 个 Go 文件 / 2036 行、59 个文档文件；改写后 `rg -l 'seelex/sessionstore'` 与
  `rg '\bsessionstore\b'` 仅剩下面两条刻意保留的文案。
- **遮蔽处理**（新包名 `store` 与既有的局部变量/字段同名，共 51 个文件）：把该标识符机械改名
  为 `st`（315 处标识符 + 34 处字段访问 + 106 处小写选择器），全部由编译器逐轮钉住
  （脚本 `tmp/reshape/m3s5_local_rename.py`、`m3s5_local_selectors.py`、`m3s5_fix_errors.py`，
  后者按 `go vet` 报错位置改写并迭代到零错）。
  **静默歧义检查**：`store` 包内"既是包级标识符又是方法名"的名字只有
  `Config`/`Router`/`ToolResult`/`jsonRepository`/`stackJournal`/`stackStats` 6 个
  （脚本 `tmp/reshape/m3s5_overlap.py`）；外层文件里这 6 个名字只出现在类型位置
  （`*store.Router`、`[]store.ToolResult` 等），无一处是"局部变量上的同名方法"。
- **闸门同步**：`e2e/facade_boundary_test.go` 与 `e2e/store_boundary_test.go` 的清单/scope
  等量替换（`sessionstore` → `store`）；`e2e/layout_test.go` 的 `repositoryModules`
  （`sessionstore` → `store`，`workspace` → `store/workspace`）；`e2e/workunit_ports_test.go`
  的 `forbiddenPackages`/`forbiddenQualifiers`、`subagent_status_vocabulary_gate_test.go`
  的文件清单、`state_enum_cast_gate_test.go` 的扫描目录。
- **验证**：`gofmt -l .` 干净（除 `tmp/`、`vendor/`）；`go build ./...`、`go vet ./...`、
  `go build -tags "gui,desktop,production" ./...` 全部通过；`go test ./e2e/ -count=1` **ok**；
  全量 `go test ./... -count=1` 失败集合 = 基线 3 例（`application/core`）+ 已知偶发 1 条
  （`seelebridge/tools.TestJobHandlesCarryTheirTableToken`）；前端
  `node --test gui/frontend/dist/*.test.mjs` **778/778**。

### 4.5 S6 文档

- `store/README.md`：保留原有散文，新增「分卷 README（根包按文件前缀）」导航表；
  新增 10 个分卷 `store/README-{session,message,compact,stack,retention,board,team,media,storage,misc}.md`
  （各含生态位 + 文件与函数索引），由 `scripts/gen_store_readme_volumes.py` 生成，
  提取逻辑复用 `scripts/gen_core_readme_index.py`；覆盖自检 **134/134 个 `.go`**（含测试），
  漏归属即报错。
- `store/root/README.md`：按 `AGENTS.md` §3 十项 + 生成索引（9 个文件）。
- 现行文档路径同步：`AGENTS.md`、`README.md`、`README_EN.md`、`MEMORY.md`、`CONTRIBUTING.md`、
  各模块 README、`config/README.md` 与 `config/seelex.yaml` 注释、
  `internal/bootseed/assets/config/seelex.yaml`、`docs/arch/**`、`docs/gui/**`。
- 历史文档**不动**：`CHANGELOG.md`、`docs/<日期>-*/**`、`docs/devlog/**`、`docs/research/**`
  （AGENTS.md §2 的历史口径）。

## 5. 删除清单（原位置 → 新唯一位置）

包级：`sessionstore/`（54 个生产文件的归位 + 84 个测试随包）→ `store/`；`workspace/` → `store/workspace/`。
下列逐文件给出"唯一实现"的新落点（同一份代码，未产生第二份实现）：

| 原位置 | 新唯一位置 |
|---|---|
| `sessionstore/data_root_lock.go` | `store/root/data_root_lock.go`（`AcquireDataRootLock`/`ReleaseDataRootLock`/`DataRootLockedBy*`/`OwnerRecord`/`NewOwnerRecord`/`ReadOwnerRecord`/`WriteOwnerRecord`/`DataRootLockFile`/哨兵错误） |
| `sessionstore/process_alive_unix.go`、`process_alive_windows.go` | `store/root/process_alive_*.go`（`ProcessAlive`） |
| `sessionstore/file_shared_read_other.go`、`file_shared_read_windows.go` | `store/root/file_shared_read_*.go`（`OpenSharedRead`/`ReadSharedFile`） |
| `sessionstore/jsonl_io.go` | `store/root/jsonl_io.go`（`TruncateCrashTail`） |
| `sessionstore/storage_settings.go` | `store/root/storage_settings.go`（`Settings`/`NewSettings`/`DefaultSettings`/`ResolveSettings`/`MergeSettings`/`ValidateSettings`/`ShardRows`/`WireBudget`/`RetentionModeManual`） |
| `sessionstore/sessionstore.go` 的 `hash`/`randomID`/`removeAllWithBackoff`/`writeAtomic`/`renameBackoff` | `store/root/atomic.go`（`Hash`/`RandomID`/`RemoveAllWithBackoff`/`WriteAtomic`）+ 未导出的 `renameBackoff` |
| `sessionstore/storage_settings.go` 的 `applyWireBudget`（接收者方法） | `store/wire_assembler.go` 的自由函数 `applyWireBudget(settings, base)`（`wireParams` 属装配域，不随 `Settings` 下沉） |
| `sessionstore/router_storage.go` 的 `lifecycle.Storage` 编译期断言 | `seelexctx/router_storage_conformance.go`（消费侧） |
| `sessionstore/router_storage_test.go` 的 `TestRouterStorageWithContextActor` | `seelexctx/router_storage_conformance_test.go` |
| 根 `README.md` 的 `workspace/README.md` 链接 | `store/workspace/README.md` |
| `e2e/layout_test.go` 的 `repositoryModules` 条目 | `store`、`store/workspace`（`sessionstore`、`workspace` 两条消失） |

## 6. 新增偏差登记（S2–S6 实施期发现）

| ID | 偏差 | 需要谁点头 |
|---|---|---|
| D-M3-8 | `store/root` 比设计稿 B 的 13 项多导出 6 项：`OwnerRecord`/`NewOwnerRecord`/`ReadOwnerRecord`/`WriteOwnerRecord`/`DataRootLockFile`/`ProcessAlive`。理由：数据根锁的 3 个白盒用例（在 `store/` 侧）要伪造/replay 锁文件与探测存活，**导出同一实现**比让用例复制锁文件格式更符合 AGENTS §8.1（复制格式才是第二份实现） | 复核者 |
| D-M3-9 | 设计稿 B 要求 `Settings.ApplyWireBudget` 成为导出方法；实测两个调用点都在 `store` 包内，且它操作的是本包的 `wireParams` ⇒ 改为包内自由函数（接收者改第一个参数），判据与文案逐字未变 | 复核者 |
| D-M3-10 | `store/root` 是"新模块 +1"，两条 ratchet 上限各 +1：`facadeClosureAllowListMax` 69→70、`contractClosureAllowListMax` 28→29（契约层闭包本身没有新增真实依赖，只是既有污染链 `contract → seelexctx/search → store` 上多了一个纯 stdlib 叶子的名字） | 复核者 |
| D-M3-11 | S4 未采用设计稿 A §2.4 的"契约层通用端口"路线：`contract → seelexctx/search → seelexctx/memory → store` 会使 `store → contract` 成环（实测 `import cycle not allowed`）。改用"断言整条上移消费侧"，不新增契约面 | 复核者 |
| D-M3-12 | `seelebridge/events_unified.go` 的两处错误文案 `"unified events: sessionstore router is not attached"` **逐字保留**（错误文案属行为面，改名不追文案）。若要求一致，可单独一个 commit 改文案并同步用例 | 用户 |
| D-M3-13 | `gui/**` 未改（plan §2.1「前端本轮零改动」），因此 `gui/frontend/dist/*.js|*.mjs` 注释里的旧路径 `sessionstore/...` 仍是残留；`gui/README.md` 同理 | 复核者（M5 前端收口时一起改） |

## 7. 未达成 / 残留

1. **`store/root` 没有自己的测试文件**：其机制由 `store/` 侧的白盒用例覆盖（搬迁前后这些
   用例逐字未改，见 `store/data_root_lock*_test.go`、`file_shared_read_windows_test.go`、
   `atomic_publish_windows_test.go`、`retention_search_blob_test.go` 等）。理由：这些用例同时
   验证"Router 打开数据根 → 锁行为"的集成面，搬走会丢覆盖；复制一份则违反 §8.1。
2. **写放大对照（M3 退出条件之一）**：写路径已在 2026-10-15 改为"批次落盘"（结论见 §0.5 与
   `store/tool_record.go`），本轮搬迁未触碰该通道；"同一回合 N 次调用 ⇒ 1 次落盘"的
   **可复现 A/B 数字**尚未补齐（既有的 `application/core/tool_record_test.go` 与
   `internal/toolrecord/accumulator_test.go` 只钉行为，不打印计数对照）→ 列为 M3 收尾后的
   独立小项（不影响本轮"字节级不变"验收）。
3. **V10 的"是否进 Snapshot"** 仍待用户裁决（§3），本轮未改快照协议。
4. `plan.md` §2.9 的**文件清单本身**未按工作区更新（plan 是方案文档，漂移已在 §2 登记；
   按 plan §5 的口径，记录以工作区实测为准）。
