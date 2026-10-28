# Store 领域驱动拆分 · 进度台账

本文件是 M0–M11 的唯一进度台账，每完成一个里程碑追加一节并刷新顶部状态表。
方案事实源仍是 [`plan.md`](plan.md) 与 [`milestones.md`](milestones.md)；
本文件只记录**已跑出来的证据**，不重复设计论证。

提交纪律：从 2026-10-28 起，每个里程碑完成门禁后创建一个只含该里程碑的本地
commit；不 push。M7 外部调用点窄化已获得 commit 授权，作为当前 checkpoint
先落库，后续里程碑继续遵循“测试 → commit”。

当前已落库 checkpoint：

- `cf119696`：M7 provider wire 与压缩栈窄读面。
- `3c23ebf4`：`PromptStackSnapshot` 内层投影。

快照时间：2026-10-28（M7 完成）。

## 1. 状态总览

| 里程碑 | 能力面 | 状态 | 门禁证据 |
|---|---|---|---|
| M0 | 基线门禁 | 已完成 | 见 §2 |
| M1 | Board 试点 | 已完成 | 见 §2.5 |
| M2 | layout 内核 | 已完成 | 见 §2.6 |
| M3 | Artifact | 已完成 | 见 §2.7 |
| M4 | Commit/Turn UoW | 已完成 | 见 §2.8 |
| M5 | Session 核心 | 已完成 | 见 §2.9 |
| M6 | Session 状态机 | 已完成 | 见 §2.10 |
| M7 | Context | 已完成 | 见 §2.11，含外部批次重放与回滚演练 |
| M8 | Teamwork | 已完成 | 见 §2.12 |
| M9 | Role/WorkUnit | 已完成 | 见 §2.13 |
| M10 | Facade 清零 | 已完成 | 见 §2.14 |
| M11 | 全仓验收 | 未开始 | — |

## 2. M0：基线门禁（已完成）

### 2.1 逐项交付

`milestones.md` §4 的七项交付，逐项落到可执行断言：

| # | 交付 | 落地文件 | 断言 | 结果 |
|---|---|---|---|---|
| 1 | message/event/compact/retention/lifecycle/tool-result/media 物理布局 golden | `store/layout_golden_test.go` | `TestSessionLayoutGolden` | PASS |
| 2 | `Commit` 写序：artifact → refs → message → compaction | `store/commit_write_order_test.go` | `TestCommitWritesArtifactBeforeMessage` | PASS |
| 3 | `Commit` 半失败语义 | 同上 | `TestCommitPublishesMessageBeforeCompactionFailure`、`TestCommitRetryWithExplicitSeqIsIdempotent` | PASS |
| 4 | 锁身份：同模块同锁、不同模块不同锁、未知模块显式失败 | `store/module_lock_identity_test.go`（M0 前已存在，本里程碑只登记不改写） | `TestModuleLocksAreDistinct`、`TestUnmappedModuleLockPanicsInsteadOfAliasing`、`TestMediaReadNotBlockedByHeldMessageLock` | PASS |
| 5 | `Router`/`jsonRepository`/`storeEngine` 方法面 ratchet | `store/god_type_ratchet_test.go` + `store/testdata/god_type_methods.json` | `TestGodTypeMethodSurfaceOnlyShrinks`、`TestGodTypeMethodScannerSeesAdditionsAndSkipsTests` | PASS |
| 6 | `./store/...` 全量 I6 import 门禁 | `e2e/store_boundary_test.go`（本次由人肉清单改为枚举） | `TestStoreDependencyClosureHasNoSeelexUpwardDependency`、`TestStoreTestFilesImportNothingUpward`、`TestStoreBoundaryEnumeratesEveryStorePackage` | PASS |
| 7 | 代表性会话 fixture，可从当前版本冷读 | `store/cold_read_fixture_test.go` + `store/testdata/coldread/` | `TestColdReadFixture`（9 个子测试）、`TestColdReadFixtureRegenerate` | PASS |

冷读 fixture 覆盖 9 个读路径：消息行、会话范围读（`ReadRange` 与
`ReadConversationRange` 两种语义）、工具结果、压缩记录、压缩帧、草稿与队列、
媒体、retention 水位、结构事件。fixture 由 `SEELEX_COLDREAD_REGENERATE=1`
显式重建，落盘只含 `project-<hash>/`，不含数据根锁。

### 2.2 基线测量（M0 之后所有里程碑只许与它比较）

**方法面 ratchet**（`store/testdata/god_type_methods.json`，按方法名集合精确相等判定）：

| 类型 | 方法数 |
|---|---|
| `Router` | 130 |
| `jsonRepository` | 90 |
| `storeEngine` | 183 |
| `SessionGranularStore` | 46 |

**底座向上依赖**（`go list -deps` 实测，I6 登记表当前例外）：

| 包 | 允许的 Seelex 内向上依赖 | 归零条件 |
|---|---|---|
| `store` | `kernel/event` | 事件端口下沉 `contract`（M4 前后） |
| `store/root` | 无（闭包 = 纯标准库） | 已归零 |
| `store/schedule` | `kernel/event` | 同上 |
| `store/workspace` | `internal/winhide` | 机制叶子，位在 store 之下，长期保留 |

`store/workspace → internal/winhide` 不是待清理项：`winhide` 自身闭包为零 Seelex
依赖，登记它而不是放过整个 `internal/` 前缀，是为了让后续新增的 `internal/*`
依赖照样必须显式登记。

**直接 import 根 `store` 的文件**（统计口径：`grep -rl '"github.com/RedHuang-0622/seelex/store"' --include='*.go' .`，
排除 `store/` 自身与 `tmp/reshape/*` 独立模块快照）：

- 合计 **232** 个文件：生产 **86**、测试 **146**。
- 按目录统计（生产 / 测试 / 合计）：

| 目录 | 生产 | 测试 | 合计 |
|---|---|---|---|
| `backend/engine/assemble` | 22 | 35 | 57 |
| `backend/multiagent/teamwork` | 16 | 23 | 39 |
| `backend/engine/context`（含 `seelectx/{memory,search}`） | 10 | 15 | 25 |
| `wiring/adapters` | 8 | 11 | 19 |
| `test` | 0 | 11 | 11 |
| `application/assembly` | 0 | 11 | 11 |
| `backend/multiagent/subagent` | 5 | 3 | 8 |
| `kernel/session` | 4 | 3 | 7 |
| `backend/workunit` | 3 | 4 | 7 |
| `kernel/goal` | 3 | 3 | 6 |
| `kernel/context` | 2 | 3 | 5 |
| `backend/tool/worktree` | 1 | 4 | 5 |
| `application`（根包） | 0 | 5 | 5 |
| `kernel/board` | 0 | 4 | 4 |
| `backend/tool/io`（`media`/`multimodal`） | 2 | 2 | 4 |
| `backend/multiagent/team` | 2 | 2 | 4 |
| `wiring/bootstrap` | 3 | 0 | 3 |
| `backend/engine/obs` | 2 | 1 | 3 |
| `e2e` | 0 | 3 | 3 |
| `backend/multiagent/agentsession` | 1 | 1 | 2 |
| `wiring/registry` | 1 | 0 | 1 |
| `kernel/board/boardtest` | 1 | 0 | 1 |
| `application/session` | 0 | 1 | 1 |
| `application/goal` | 0 | 1 | 1 |

生产文件的完整清单见 §4。这张表是 M10「Facade 清零」的收缩基线：
每迁移一个领域，对应包的生产文件应当从清单里消失，而不是改写 import。

### 2.3 零路径 / 零格式 / 零数据迁移

M0 只新增测试与 testdata，**没有改动任何生产代码**：

- 新增：`store/layout_golden_test.go`、`store/commit_write_order_test.go`、
  `store/god_type_ratchet_test.go`、`store/cold_read_fixture_test.go`、
  `store/testdata/god_type_methods.json`、`store/testdata/coldread/`。
- 修改：`e2e/store_boundary_test.go`（I6 从人肉清单改为枚举 + 只许收缩登记表）。
- 未触碰：`.seelex` 用户数据、`config/*`、`dist/`、任何落盘路径或 JSON 字段。

### 2.4 验收

```text
go test ./store/... ./e2e/... -count=1   # 2026-10-27 PASS（store 137.1s，e2e 56.7s）
go test ./test/ -count=1                 # 2026-10-27 PASS（102.0s）
git diff --check
```

格式检查 `gofmt -l` 对本里程碑新增测试无输出；`git diff --check` 通过。

## 2.5 M1：Board 垂直试点（已完成）

### 2.5.1 领域归属与契约

项目看板追加、折叠、压实、路径校验、按物理路径分片锁、聚合读、会话回收与
`BOARD.md` 渲染统一归 `store/board`。`contract/dto` 继续拥有行、作用域与读选项的
跨层身份；根 `store` 只保留同身份 alias 和 Router 单层转发。看板仍是派生投影，
不是 task/teamwork 的事实源。

### 2.5.2 红测与 checkpoint

首次失败：

```text
go test ./e2e -run TestStoreDDDMBoardOwnershipGate -count=1
--- FAIL: TestStoreDDDMBoardOwnershipGate
    store/board 尚未成为看板 owner: The system cannot find the file specified.
```

隔离 worktree：`C:\Users\redre\.codex\worktrees\store-ddd-m1\seelex`

- branch：`codex/store-ddd-M1`
- base：`6c0545fdbb1e007bff4e914431ac7c7eb54fb716`
- checkpoint：`22b608804c574d949c43f4d68557bdc957663033`
- 外部影响清单：`impact/M1-external-deps.{json,md}`
- 脚本 dry-run 与 apply：通过；journal：`%TEMP%\seelex-store-ddd-M1-40456.json`

### 2.5.3 删除/合并清单

| 原位置 | 新唯一位置 | 处理 |
|---|---|---|
| `store/project_board.go`：路径、锁、追加、压实、聚合读、回收 | `store/board/board.go` | 删除根实现，根 Router 改单层转发 |
| `store/project_board_markdown.go`：Markdown 渲染 | `store/board/markdown.go` | 删除根实现 |
| `store/project_board_test.go` / `store/project_board_landing_test.go` | `store/board/board_test.go` + `store/project_board_compat_test.go` | 领域行为随 owner；根测试只测兼容与跨模块锁身份 |
| `store/project_board.go` 的 `sanitizeKnownText` 调用 | `store/internal/textsanitize/sanitize.go` | 项目摘要与看板共用唯一清洗判据 |
| `wiring/registry` 的具体 `*store.Router` Board 参数 | `kernel/board.ProjectBoardReader` + `dto.ProjectBoardRow` | 消费方窄端口 |
| `kernel/board/boardtest` 的根 `store.FoldProjectBoardRows` | `store/board.Fold` | 测试替身复用领域唯一折叠实现 |

根 `project_board_compat.go` 不含 JSON 解码、文件打开、排序、锁表或残尾修复实现；
`e2e/store_ddd_board_gate_test.go` 机械禁止这些实现重新回流。

### 2.5.4 门禁结果

```text
go test ./store/... ./e2e/... -count=1   PASS
go test ./kernel/board ./wiring/registry -count=1   PASS
go test ./test -run ProjectBoard -count=1   PASS
go test ./test/ -count=1   PASS
go build ./...   PASS
go build -tags "gui,desktop,production" ./...   PASS
go vet ./...   PASS
git diff --check   PASS
```

`./store/...` 的 I6 枚举自动纳入 `store/board` 与
`store/internal/textsanitize`，两者没有根 `store` 或上层反向依赖。

### 2.5.5 结构指标

| 类型 | M0 | M1 |
|---|---:|---:|
| `Router` | 130 | 130 |
| `jsonRepository` | 90 | 90 |
| `storeEngine` | 183 | 183 |
| `SessionGranularStore` | 46 | 46 |

M1 按计划保留根兼容方法，God 方法面没有增长。直接 import 根 `store` 的生产文件
从 86 降到 85；测试文件仍为 146。`wiring/registry` 仍因其它工具 import 根
`store`，但项目看板读取面已不接收具体 Router。

### 2.5.6 数据兼容与回滚

- 路径、JSON tag、schema、head、watermark：零变化。
- 用户数据：未读取、未迁移、未清理；M1 测试只写 `t.TempDir()`。
- Board fixture 继续验证残尾、幂等、tombstone、压实、会话串写拒绝、Markdown 字节稳定。
- 回滚面：反向补丁恢复两个根实现、删除 `store/board` 装配并恢复消费方类型；
  磁盘上的 `worktable.jsonl`、`teamwork.jsonl`、`BOARD.md` 格式不变，无需数据回滚。

## 2.6 M2：Layout 内核（已完成）

### 2.6.1 领域归属

`store/internal/layout` 成为 JSON v8 布局机制 owner：

- `project-<hash>/session-<hash>/metadata/<module>.json` 路径派生；
- guide 幂等创建与固定字段；
- module head 信封、checksum、原子发布、严格/宽容读；
- 按 `(Scope, ModuleID)` 一模块一锁的动态身份表。

message/event/compact/stack 等 head 的**领域重建**仍留在根 `storeEngine`；layout
不知道 payload 类型，也不按模块写 switch。

### 2.6.2 红测

首次失败：

```text
go test ./e2e -run TestStoreDDDMLayoutOwnershipGate -count=1
--- FAIL: store/internal/layout 尚未成为布局机制 owner:
The system cannot find the file specified.
```

通过后的定向门禁：

```text
go test ./store/internal/layout -count=1   PASS
go test ./store -run 'Head|Lock|Atomic|Pending|Guide' -count=1   PASS
go test ./e2e -run TestStoreDDDMLayoutOwnershipGate -count=1   PASS
```

### 2.6.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/module_heads.go` sessionRoot/metadataDir/modulePath/guidePath | `store/internal/layout.Layout.{SessionRoot,MetadataDir,ModulePath,GuidePath}` |
| `store/module_heads.go` `ensureLayoutGuide` | `store/internal/layout.Layout.EnsureGuide` |
| `store/module_heads.go` `headChecksum` / `publishModuleHead` | `store/internal/layout.{Checksum,Layout.Publish}` |
| `store/module_heads.go` strict/lenient head 读取 | `store/internal/layout.Layout.{ReadHead,ReadLenientHead}` |
| `sessionModuleLocks` 的 18 个模块锁字段 + `mutexFor` switch | `store/internal/layout.Layout.Lock` 的动态 `(session, module)` 锁表 |
| `json_layout.go` 的 `toolRefsMu` 字段使用 | layout 的 `moduleToolRefs` 锁身份 |

根 `validStorageModule` 仍显式拒绝未登记模块；新增模块不再修改锁字段集合。

### 2.6.4 结构指标与数据兼容

- God 方法面：`Router 130` / `jsonRepository 90` / `storeEngine 183` /
  `SessionGranularStore 46`，均未增长。
- 根 `store` 生产 importer：85，未增长。
- 路径、metadata 文件名、guide/head JSON 字段、checksum 算法、锁粒度和原子 rename：
  零变化。
- `store/internal/layout` 只依赖标准库与 `store/root`，自动受 I6 覆盖。

## 2.7 M3：Artifact（已完成）

### 2.7.1 领域归属

`store/artifact` 成为不可变内容寻址资产与派生搜索索引 owner：

- `big_tool_result/` 文本 blob、硬限/软限/会话配额与 GC；
- immutable tool-result JSON 文件；
- `meta/<sha256>/` 媒体去重、名称清洗、独立配额、索引发布与 GC；
- `metadata-index/search.json` 的 token 化、构建和查询。

根 `storeEngine/jsonRepository` 保留迁移期 Router/Repository 能力接口与数据投影
适配；message/compact 的读取及 tool-result refs head 发布仍由 Session/Commit 用例
编排，artifact 不读取 transcript。

### 2.7.2 红测与结构门禁

首次失败：

```text
go test ./e2e -run TestStoreDDDMArtifactOwnershipGate -count=1
--- FAIL: store/artifact 缺少模块 README:
The system cannot find the file specified.
```

领域测试与定向回归：

```text
go test ./store/artifact -count=1   PASS
go test ./store -run 'Media|Blob|Search|ToolResult|Commit' -count=1   PASS
go test ./e2e -run TestStoreDDDMArtifactOwnershipGate -count=1   PASS
```

### 2.7.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/big_tool_result.go` 的限额、截断、blob 文件、GC | `store/artifact/blob.go` |
| `store/media.go` 的内容摘要、名称清洗、配额、元数据、索引、媒体 GC | `store/artifact/media.go` |
| `store/search_index.go` 的 token 化、hit 合并、排序与索引文件 | `store/artifact/search.go` |
| `jsonRepository.writeToolResultLocked/readToolResult` 的文件实现 | `artifact.Store.WriteToolResult/ReadToolResult` |
| `jsonRepository` 四个 media 方法的锁内实现 | `artifact.Store` 媒体方法，根只转发 |

根文件只保留类型/错误 alias、`MediaStore` 能力面、Router 适配和既有包内测试所需的
薄路径转发；`e2e/store_ddd_artifact_gate_test.go` 禁止内容寻址/配额/索引实现回流。

### 2.7.4 结构指标与数据兼容

- `storeEngine` 方法面 `183 -> 180`，删除 `mediaUsageBytes`、`readMediaMetaFile`、
  `writeMediaMetaFile`；`Router/jsonRepository/SessionGranularStore` 未增长。
- 路径保持 `big_tool_result/*.result.json`、`big_tool_result/*.jsonl`、
  `meta/<sha256>/`、`metadata-index/search.json`、`metadata/media.json` 不变。
- 文本软限/硬限、媒体硬限、Tombstone 等价的内容寻址、GC dry-run 与 JSON 字段不变。
- artifact 复用 M2 的同一 layout 实例；media 锁身份仍为 `media`，未创建第二套锁表。

## 2.8 M4：Commit/Turn UoW（已完成）

### 2.8.1 领域归属

- `application/conversation.TurnStoreCommitApplication` 拥有跨聚合写序：
  Artifact → Session(message/head) → Compaction。
- `store.CommitWriter` 只暴露 `WriteArtifacts`、`WriteMessages`、
  `WriteCompactions` 三个单聚合写口，不再承载跨聚合 UoW。
- `store.Commit` / `Repository.WriteCommit` 保留迁移期兼容 adapter；生产 snapshot
  持久化已经改走 application UoW + wiring adapter。

### 2.8.2 红测

首次失败：

```text
go test ./e2e -run TestStoreDDDMCommitOwnershipGate -count=1
--- FAIL: application TurnStoreCommit 尚未落位:
The system cannot find the file specified.
```

应用层单测同时钉住：

- 正常顺序严格为 `artifact, message, compaction`；
- message 失败后不得继续 compaction；
- M0 的 compaction 半失败可见性由既有 store 特征测试继续覆盖。

### 2.8.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `json_layout.writeCommitLayout` 内 artifact 写序 | `application/conversation.TurnStoreCommitApplication.Commit` |
| `json_layout.writeCommitLayout` 内 message/compaction 顺序 | 同上 |
| 根顺序实现调用的三段逻辑 | `store/commit_writer.go` 三个 free phase 函数 |
| wiring snapshot 直接构造 `store.Commit` | `turnStoreCommitAdapter` → application UoW → store 单聚合写口 |

### 2.8.4 外部批次与验证

- 分支：`codex/store-ddd-M4`
- checkpoint：`ba80a33f948358620cdaf1e75557333a77e16f64`
- manifest：`impact/M4-external-deps.{json,md}`
- script：`scripts/store-ddd/M4-external-migrate.py`；dry-run/apply 通过
- journal：`%TEMP%\seelex-store-ddd-M4-16576.json`

已验证：

```text
go test ./application/conversation ./application/assembly ./wiring/adapters ./store -count=1
go test ./test -count=1
go build ./...
go vet ./...
go test ./e2e -run TestStoreDDDMCommitOwnershipGate -count=1
```

### 2.8.5 数据兼容

- 路径、JSON tag、head、watermark、refs 索引与 compaction 水位零变化。
- ProviderHistory/State 继续不落盘；fork 后栈重建顺序保持在快照提交之后。
- 没有引入跨通道原子事务；message 已发布后 compaction 失败仍保留 M0 语义。

## 2.9 M5：Session 核心（已完成）

### 2.9.1 已完成切片

`store/session` 已承接：

- message/event 分片文件名的唯一格式与解析；
- `PendingTailReport` 的 wire/状态词表；
- 草稿尾连续性与重复 seq 分类。
- `MessageHead` / `ShardInfo` / `Floor` 的 JSON 身份与空 head 构造；
- 唯一 seq 分配器 `AssignRowSeqs`（显式 Seq 幂等窗口与严格递增校验）。
- EVENT 行、kind 词表、event head/shard JSON 与确定性 commit identity；
- message/EVENT JSONL 的共享读、崩溃残尾截断、追加、sync 与解码纯存储原语；
- head 有界自愈序列（严格读 → 重读 → 可选模块锁 → 领域 repair → 再读）
  下沉 layout；根只提供 payload 重建回调；
- message/EVENT typed head 的发布入口下沉 `store/session`，根生产写路径不再
  直接调用通用 `publishModuleHead`；
- message commit identity、pending-tail head 推进计算与草稿发布后的 head 字段
  更新下沉 Session；根只负责文件枚举、锁和缓存协同；
- message 分片目录枚举与草稿物理末行序号基准下沉 Session；
- message 已追加后的 head/commit/token/meta 状态转换下沉 Session；

根 `message_rows.go`、`structural_events.go`、`pending_tail.go` 对文件名、分类、
head 类型、JSONL 读写、seq 分配、head publish/heal 与 draft-tail 计算只保留
alias/单层转发。kernel 消费者使用 `SessionSnapshotPort`、
`SessionTranscriptPort`、`SessionConversationRangePort` 等窄端口，wiring adapter
负责映射到根存储。`Event` 继续是同身份 `protocol.TranscriptEvent` alias。

完成门禁：完整 `store/e2e`、M0 冷读/写序/半失败、draft/pending-tail、并发读、
重建和 fork 回归均通过；路径、JSON tag、head、watermark 零变化。

### 2.9.2 红测

```text
go test ./e2e -run TestStoreDDDMSessionOwnershipGate -count=1
--- FAIL: store/session/session.go 尚未落位
```

通过后：

```text
go test ./store/session -count=1
go test ./store -run 'Pending|Draft|Event|Message|Head' -count=1
go test ./e2e -run TestStoreDDDMSessionOwnershipGate -count=1
```

### 2.9.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/pending_tail.go` 的 `PendingTailReport` 与状态常量 | `store/session.PendingTailReport` 与常量 |
| `store/pending_tail.go` 的连续性/重复 seq 判据 | `store/session.ClassifyPendingTail` |
| `store/message_rows.go` 的 message 分片名生成/解析 | `store/session.MessageShardName/ParseMessageShardName` |
| `store/structural_events.go` 的 event 分片名生成/解析 | `store/session.EventShardName/ParseEventShardName` |
| `store/message_rows.go` 的 message head/shard/floor JSON 类型 | `store/session.MessageHead/ShardInfo/Floor` |
| `store/message_rows.go` 的 `assignRowSeqs` | `store/session.AssignRowSeqs` |
| `store/structural_events.go` 的 EVENT kind/row/head/shard 类型 | `store/session.StructuralEventKind/StructuralEvent/EventHead/EventShardInfo` |
| `store/structural_events.go` 的 commit identity 推导 | `store/session.StructuralEventsCommitID` |
| `store/message_rows.go` / `structural_events.go` 的 JSONL 读写与残尾修复 | `store/session/storage.go` |
| `store/module_heads.go` 的 head 自愈重试/锁顺序 | `store/internal/layout.Layout.ReadWithHeal` |
| message/EVENT production head 发布调用 | `store/session.PublishMessageHead/PublishEventHead` |
| `store/message_rows.go` message commit identity | `store/session.MessageRowsCommitID` |
| `store/pending_tail.go` 发布 head 字段计算 | `store/session.ApplyPendingTailHead` |
| `store/pending_tail.go` 分片枚举与物理末行基准 | `store/session.ListMessageShardFiles/DraftBaseSeq` |
| `store/message_rows.go` append 后 head 字段推进 | `store/session.ApplyCommittedMessageHead` |

数据路径与 JSON tag 零变化。

## 2.10 M6：Session 状态机（已完成）

### 2.10.1 已完成切片

`store/session` 已承接跨状态机共享的稳定形状与判据：

- draft/queue/lifecycle head 的 wire 类型和 queue 状态词表；
- queue head 状态；
- retention head、manual 确认错误与 watermark 错误；
- compact frame/head、边界词表与“不得越过已发布 message head”校验；
- fork 的 watermark、子会话身份与已存在冲突校验。
- retention 连续前缀校验；
- queue 入队幂等、draft→queue、直发、发送态、确认出队、失败回草稿；

根 lifecycle/retention/compact/fork 文件保留 alias、I/O 与锁协调；状态词表、
状态转移、watermark/prefix、compact 边界和 fork 冲突判据均由 `store/session`
唯一拥有。队列 FIFO、失败回草稿、LRU、compact、fork 的既有特征测试全绿。

### 2.10.2 门禁

```text
go test ./store/session -count=1
go test ./store -run 'Lifecycle|Queue|Retention|Compact|Fork' -count=1
```

两者以及完整 `store/e2e` 通过；路径、JSON tag、head、watermark 与锁粒度未变化。

## 2.11 M7：Context（已完成）

### 2.11.1 已完成切片

`store/context` 已承接：

- `<session>/context.json` 路径与 opaque payload 原子读写；
- workplan checkpoint ID → `checkpoint-<hash>` 的安全键映射。
- `SessionContextRecord`、Plan/Task/Goal/Skill/Compact 帧、Evidence、Revision
  与 GoalAudit 的稳定 JSON wire；根包改为同身份 alias。
- checkpoint payload 与 SystemPrompt snapshot 的稳定 JSON wire。
- StackKind/StackItemInput/StackItemRecord/Watermark/ModuleHead/Mutation 的
  wire 与终态词表；根栈通道改为 alias/纯函数转发。
- Compact 栈的范围校验、request 索引与前驱链校验；
- wire message/result 形状、兜底预算、软阈值缩放与参数归一化；
- Plan/Task/Goal 入栈输入、Goal 栈顶校验、Skill 压/弹栈纯状态操作；
- provider wire 的纯装配状态机：compact frame、尾行过滤、孤儿工具行、
  open/修复占位、软预算边界、retry attempt 锚点、digest 与前缀比较。
- 压缩栈窄读面：`SessionContextStore.CompactStackSnapshot` 只返回
  `CompactFrame` 深拷贝；seelectx controller/search、相关记忆选取与装配侧
  不再通过万能 `SessionContextRecord` 读取压缩栈。
- `PromptStackSnapshot`：prompt 装配与本地压缩获得只含 Plan/Task/Skill/Compact
  四栈的内层投影；seelectx、kernel context 与装配侧 consumer 已直接切换，
  不再通过 `SessionContextRecord` 读取 prompt/storage 混合字段。
- `store/context.ForkState`：父 context payload → 独立子会话 payload 的纯重写；
  session fork 只负责读取、包装错误与写回，不再拥有 `SessionContextRecord`
  JSON 变换、Skill 过滤或 compact 帧裁剪。

根 `wire_assembler.go` 只把 `Event` 与 attempt cache 映射为 `store/context`
输入，不再持有装配 switch；`context_state.go` 与 `checkpoint_store.go` 只保留
repository lock、opaque 序列化与 Router 适配。四类栈消费方投影、
`SessionContextRecord` 收缩与 compact bridge 归位仍待后续切片，因此 M7
保持进行中。

### 2.11.2 红测与门禁

```text
go test ./e2e -run TestStoreDDDMContextOwnershipGate -count=1
--- FAIL: store/context/context.go 尚未落位
```

通过后：

```text
go test ./store/context ./store -run 'Context|Checkpoint' -count=1
go test ./e2e -run 'TestStoreDDDMContextOwnershipGate|TestFacadeDependencyClosureRatchet' -count=1
```

路径、`context.json` 文件名、opaque 内容和 checkpoint key 均未变化。

provider wire 纯装配切片首次为红灯：

```text
go test ./store -run 'Wire|Lifecycle|Queue|Retention|Compact|Fork' -count=1
FAIL store/wire_assembler.go: imported and not used: crypto/sha256, encoding/hex,
encoding/json, strings
```

修正后：

```text
go test ./store/context -count=1
go test ./store -run 'Wire|Lifecycle|Queue|Retention|Compact|Fork' -count=1
go test ./e2e -run 'TestStoreDDDMContextOwnershipGate|TestFacadeDependencyClosureRatchet' -count=1
go test ./store/... -count=1
go test ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
```

以上均 PASS。`go test ./store/...` 首次运行时 `TestCommitNotBlockedByFullHistoryRead`
在并发负载下超时 125.7ms；该用例单独重跑 PASS，未改测试或锁实现，记为
环境负载型波动；随后单独串行重跑完整 `go test ./store/... -count=1` PASS。

压缩栈窄读面门禁首次为红灯：

```text
go test ./e2e -run '^TestStoreDDDMCompactStackReadFaceGate$' -count=1
FAIL: CompactStackSnapshot missing in store/session_context.go,
seelectx/controller.go, seelectx/search.go and assemble/runtime.go;
controller/search/runtime still expose Snapshot() store.SessionContextRecord
```

切换后：

```text
go test ./e2e -run '^TestStoreDDDMCompactStackReadFaceGate$' -count=1   PASS
go test ./store/... -count=1   PASS
go test ./backend/engine/context/... ./kernel/context ./backend/engine/assemble -count=1   PASS
go test ./test/ -count=1   PASS
go build ./...   PASS
go build -tags "gui,desktop,production" ./...   PASS
go vet ./...   PASS
```

Prompt 窄投影切换首次编译为红灯：seelectx frame/gap/dag、kernel context
compaction 与 application probe 仍把 `SessionContextRecord` 传给
`PromptStackSnapshot` 参数。切换后以下门禁 PASS：

```text
go test ./backend/engine/context/... ./kernel/context ./backend/engine/assemble -count=1
go test ./application/assembly -run 'Context|Strategy|Cache|Goal|Stack' -count=1
go test ./e2e -run 'TestStoreDDDMPromptStackReadFaceGate|TestStoreDDDMCompactStackReadFaceGate|TestFacadeDependencyClosureRatchet' -count=1
go test ./store/... -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
```

全量并发门禁中 `TestCommitNotBlockedByFullHistoryRead` 再次因 build/test 同时
跑而超时；不调整阈值，串行单测与完整 `store/...` 重跑 PASS。

session fork 所有权切片：

```text
go test ./store/context ./kernel/session ./e2e -run 'Fork|ContextForkOwnership' -count=1   PASS
go build ./...   PASS
go vet ./...   PASS
```

M7 正式验收命令已于 2026-10-28 01:55 整体 PASS：

```text
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
```

四条上下文所有权门禁、普通/tagged build 与全仓 vet 也均 PASS。外部 consumer
批次已从 checkpoint `8fbae4e8` 在隔离 replay worktree 完成 dry-run、apply、
领域测试、tree 比对和 rollback 演练：

- track 代码应用后与 `b50612ea` 完全一致；
- 新建的 `store/context/fork.go`、`fork_test.go` 与主工作树 SHA-256 一致；
- rollback 后代码 diff 为 0，随后重新 apply 成功；
- manifest、可重放 patch 与脚本分别位于 `impact/M7-external-deps.{json,md}`、
  `impact/M7-*.patch` 与 `scripts/store-ddd/M7-external-migrate.py`。

`CompactStackSnapshot` 的深拷贝由 `TestSessionContextStoreCompactStackSnapshotIsDeepCopy`
钉住。Plan/Task 仍由 `GapCoverageDeps.Record` 提供给本地压缩摘要；该次要投影
尚未收缩，不能把本切片误报为 M7 完成。

### 2.11.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/wire_assembler.go` 的 `assembleWireRows` 纯装配核心 | `store/context.AssembleWireRows` |
| `store/wire_assembler.go` 的 `wireState`、行分类、预算、open/孤儿修复 | `store/context/wire_engine.go` 私有状态机 |
| `store/wire_assembler.go` 的 `wireDigest` / `wirePrefixEqual` | `store/context.WireDigest` / `WirePrefixEqual` |
| `store/wire_assembler.go` 直接消费 `Event` / `attemptCache` | 根包 `contextWireRows` / `rootWireAttemptProvider` 单层映射 |
| `seelectx.CompactStackStore.Snapshot() SessionContextRecord` | `CompactStackSnapshot() []store.CompactFrame` |
| search `StackSource.Snapshot() SessionContextRecord` | `CompactStackSnapshot() []store.CompactFrame` |
| `RelatedMemoryBlocks(record, query)` | `RelatedMemoryBlocks(frames, query)` |
| `seelectx.Render*Blocks(record SessionContextRecord)` | `Render*Blocks(PromptStackSnapshot)` |
| `CompactionInput.Record / GapCoverageOptions.Record / LocalCompactOptions.Record` | `PromptStackSnapshot` |
| `GapCoverageDeps.Record func() SessionContextRecord` | `func() PromptStackSnapshot` |
| `kernel/session.forkContextRecord` 解码/清栈/过滤 Skill/裁剪 Compact | `store/context.ForkState` |
| `kernel/session.forkContextSkillFrames` | `store/context.forkSkillFrames`（私有） |
| `kernel/session.rewriteForkCompactStack` | `store/context.RewriteForkCompactStack` |

本切片没有新增 God receiver 方法：`Router`、`jsonRepository`、`storeEngine`、
`SessionGranularStore` 方法集合均未增长。路径、JSON tag、head、watermark、
attempt cache 与 provider wire wire 字节均零变化；未读写或迁移用户数据。

压缩窄读切片的直接改点（未提交）：

```text
store/session_context.go
store/session_context_test.go
backend/engine/context/seelectx/{controller.go,controller_test.go,
controller_loop_contract_test.go,contention_gate_test.go,gap_test.go}
backend/engine/context/seelectx/search/{search.go,search_test.go}
backend/engine/assemble/{runtime.go,runtime_context.go}
kernel/context/{wiring.go,wiring_test.go}
e2e/store_ddd_context_gate_test.go
```

这是外部依赖批次。专用 branch/worktree 已满足；checkpoint、manifest、脚本
dry-run/apply 仍待用户授权本地 commit 后补做，当前不能宣告该批次完成。

## 2.12 M8：Teamwork（完成）

### 2.12.1 切片一：纯模型 / 校验 / folding（`af2bbaab`）

- 新建 `store/teamwork`，唯一拥有 Plan/Member/Milestone/WorkItem、Event、
  Binding 的稳定形状与词表。
- Plan/Work Item 校验、里程碑与工作项 DAG 环检测、binding 最后一行折叠
  已下沉；根 `store` 改为同身份 alias 与单层纯函数转发。

### 2.12.2 切片二：I/O owner（本轮）

`store/teamwork/store.go` 成为 teamwork 落盘的**唯一**实现：

- plan = 模块 head `<sessionRoot>/metadata/teamwork.json`，`ValidatePlan` →
  `layout.Commit` 整份替换；
- 审计 = `<sessionRoot>/teamwork/events.jsonl`，`O_APPEND` 单次写一行；
- 绑定 = `<sessionRoot>/teamwork/worktrees.jsonl`，同上（KV 折叠仍在
  `model.go` 的 `Bindings`）；
- 两条流水共用 `readRows` 读取：缺失 = 空切片，**只**跳过最后一行的解析失败；
- 作业输出目录 `<sessionRoot>/teamwork/jobs` 由 `JobOutputDir` 确保存在。

同时把 head 整份替换的**写序**下沉到机制层：`store/internal/layout` 新增
`Commit`（ensure guide → 推导 commit id → 模块锁 → publish）与
`PayloadCommitID`；`storeEngine.commitModuleHead`、`modulePayloadCommitID`
改为单层转调，不再各自维护一份写序。组合根 `storeEngine` 装配
`teamwork` / `roles` 两个领域面（共享同一个 `layout`），`moduleTeamwork`
改为 `storeteamwork.ModuleID` 的同身份 alias。

### 2.12.3 切片三：team registry → `store/role`（本轮）

先给领域归属证据，再落代码：

1. **内容是角色身份与策略**：`RoleSpec` 描述"这批会话有哪些角色、各自的
   system prompt / 模型策略 / 工具范围 / 顺序优先级"，不是 teamwork 的
   "谁、什么顺序"硬编排，也不属于会话消息或生命周期。
2. **物理子树就是角色域**：`<sessionRoot>/team/roles.json` 与 M9 的 role
   draft、角色会话子树、permission、floor/message lifecycle 同处
   `<sessionRoot>/team`；交给 teamwork 会把角色身份拆到两个限界上下文。
3. **读写语义独立**：整份原子替换、无 head 水位、不写 message、不碰
   sequencer——与 teamwork 的"head + 两条追加流水"是三套机制。

`store/role/registry.go` 因此持有 `SchemaVersion`、`RoleSpec` / `Registry`、
`Normalize` 与 `Store{Path,Read,Write,Remove}`。根 `team_registry.go` 只剩三个
`*Workspace` 单层转发 + `registryScope`；角色会话、floor、lifecycle 转发
留着，等 M9 迁走。

### 2.12.4 红测与门禁

先写所有权门禁 `e2e/store_ddd_teamwork_io_ownership_gate_test.go`：要求
`store/teamwork/store.go` 持有 `os.OpenFile` / `O_APPEND` /
`storeroot.ReadSharedFile` / `layout.Commit` / `ModuleID` 且不 import 根
`store`；根 `teamwork.go`、`teamwork_items.go`、`team_registry.go` 不含
`json.Marshal` / `json.Unmarshal` / `os.OpenFile` / `os.MkdirAll` /
`os.ReadFile` / `storeroot.ReadSharedFile` / `storeroot.WriteAtomic` /
`sync.Map` / `sort.SliceStable` / `normalizeTeamRegistry`；`store/role/registry.go`
持有整份原子替换与规范化。

红（实现落位前的首次运行在 `store/teamwork/store.go` 与
`store/role/registry.go` 的读取处直接失败；实现完成后剩下的最后一条红灯）：

```text
go test ./e2e -run '^TestStoreDDDMTeamworkIOOwnershipGate$' -count=1
FAIL: store/role 缺少模块 README：GetFileAttributesEx …store\role\README.md: The system cannot find the file specified.
```

补齐 `store/role` / `store/teamwork` 模块 README 后转绿：

```text
go test ./e2e -run '^TestStoreDDDMTeamworkIOOwnershipGate$' -count=1
ok  github.com/RedHuang-0622/seelex/e2e 2.986s
```

### 2.12.5 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/teamwork.go` Event 类型与 kind 常量 | `store/teamwork.Event` 与常量 |
| `store/teamwork.go` plan 校验实现 | `store/teamwork.ValidatePlan` |
| `store/teamwork.go` `teamworkDir` / `teamworkEventsPath` / `teamworkJobDir` | `store/teamwork.Store.Dir` / `EventPath` / `JobsDir` |
| `store/teamwork.go` `WriteTeamworkPlan` / `ReadTeamworkPlan` 的 head 发布与读 | `store/teamwork.Store.WritePlan` / `ReadPlan` |
| `store/teamwork.go` `AppendTeamworkEvent` / `ReadTeamworkEvents` 的 JSONL 追加与残尾读 | `store/teamwork.Store.AppendEvent` / `ReadEvents` |
| `store/teamwork.go` `TeamworkJobOutputDir` | `store/teamwork.Store.JobOutputDir` |
| `store/teamwork_items.go` WorkItem/Binding 类型 | `store/teamwork.WorkItem/Binding` |
| `store/teamwork_items.go` DAG 校验 | `store/teamwork/validation.go` |
| `store/teamwork_items.go` `TeamworkBindings` 折叠 | `store/teamwork.Bindings` |
| `store/teamwork_items.go` `teamworkBindingsPath` + `AppendTeamworkBinding` / `ReadTeamworkBindings` 的 I/O | `store/teamwork.Store.BindingPath` / `AppendBinding` / `ReadBindings` |
| `store/team_registry.go` `teamRegistryLock` / `teamRegistryPath` / `readTeamRegistry` / `writeTeamRegistry` / `removeTeamRegistry` | `store/role.Store.Path` / `Read` / `Write` / `Remove` |
| `store/team_registry.go` `normalizeTeamRoleSpec` / `normalizeStringSet` / `normalizeTeamRegistry` | `store/role.Normalize` |
| `store/module_heads.go` `commitModuleHead` 内联 ensure → commit id → 锁 → publish | `store/internal/layout.Layout.Commit` |
| `store/module_heads.go` `modulePayloadCommitID` 的 JSON + hash | `store/internal/layout.PayloadCommitID` |
| `store/module_heads.go` `moduleTeamwork storageModule = "teamwork"` 字面量 | `store/teamwork.ModuleID`（同身份 alias） |

### 2.12.6 God 方法数与 import 边

- `storeEngine` 180 → 172（删 8 个：`readTeamRegistry`、`writeTeamRegistry`、
  `removeTeamRegistry`、`teamRegistryPath`、`teamworkDir`、`teamworkEventsPath`、
  `teamworkJobDir`、`teamworkBindingsPath`）；`Router`、`jsonRepository`、
  `SessionGranularStore` 方法集合不变。`layout.Commit` / `PayloadCommitID` 是
  机制层新增，不是 God receiver 方法。
- import 边：`store/teamwork` → `contract/dto`、`store/root`、
  `store/internal/layout`；`store/role` → `store/root`、`store/internal/layout`；
  根 `store` → 两个领域包（组合根装配 + 单层转发）。领域包对根 `store` 的
  反向边为零，由所有权门禁钉住。

### 2.12.7 数据兼容

- 路径零迁移：`metadata/teamwork.json`、`teamwork/events.jsonl`、
  `teamwork/worktrees.jsonl`、`teamwork/jobs`、`team/roles.json` 逐字不变。
- JSON tag 零变化；commit id 推导算法逐字搬到 `layout.PayloadCommitID`
  （`head-<module>-<hash>`），旧 head 的水位语义不变。
- 未读写、未迁移用户数据；未触碰 `.seelex`、config、dist。

### 2.12.8 测试

新增 `store/teamwork/store_test.go`（13 例）：`TestStorePhysicalPathsAreUnchanged`、
`TestWritePlanReplacesWholePayload`、`TestReadPlanMissingIsEmpty`、
`TestWritePlanValidatesBeforeWriting`、`TestReadPlanColdStart`、
`TestEventsAreAppendOnlyAndOrdered`、`TestAppendEventRequiresKind`、
`TestReadEventsMissingIsEmpty`、`TestReadBindingsMissingIsEmpty`、
`TestCrashTailIsSkipped`、`TestBindingsFoldLastRowAfterColdRead`、
`TestConcurrentAppendsKeepWholeLines`、`TestJobOutputDirIsSessionScoped`。

新增 `store/role/registry_test.go`（11 例）：路径身份、缺失即未配置、
规范化幂等与非法输入、整份替换、非法输入不落盘、幂等删除、损坏文件显式报错、
旧 schema 冷读、并发写整份性。

### 2.12.9 门禁结果

```text
go test ./store/... ./store/teamwork/... ./backend/multiagent/teamwork/... ./test/ -count=1
go build ./...
go vet ./...
```

实测（`GOCACHE=G:\go-build-cache\seelex`、`GOTMPDIR=G:\go-build-tmp\seelex`）：

```text
go test ./store/... ./store/teamwork/... ./backend/multiagent/teamwork/... ./test/ -count=1
  exit 0；13 个包 ok，0 条 FAIL
go test ./e2e -run 'TestStoreDDD' -count=1
  ok github.com/RedHuang-0622/seelex/e2e 5.338s
go build ./...   exit 0（空输出）
go vet ./...     exit 0（空输出）
gofmt -l store e2e  空输出
```

**路径拼写注意事项**：首轮用正斜杠 `GOTMPDIR='G:/go-build-tmp/seelex'` 跑出 3 个红，
全部是同一种假象——测试把 `filepath.Join` 产生的反斜杠路径与正斜杠拼写的临时根做
`strings.HasPrefix` 比较：

```text
store/TestProjectKnownPathFollowsProjectRoot
backend/multiagent/teamwork/TestOutputStoreSanitizesRoleName
backend/multiagent/teamwork/TestOutputStoreAllocatesDistinctPathsPerDispatch
test/TestProjectBoardWriteEndToEndFromTodoTool
```

用 `GOTMPDIR='G:\go-build-tmp\seelex'`（与 `filepath.Join` 输出同拼写）重跑即全绿，
单测 `-run TestProjectKnownPathFollowsProjectRoot` 亦 ok。这不是 M8 回归，测试未被修改。

M8 里程碑本地单提交，不 push。

### 2.12.10 下一步

进入 M9：`store/role` 继续承接 role draft / 角色会话子树 / permission /
floor-message lifecycle，新建 `store/workunit`；sequencer 仍是主会话 message 的
唯一写者。

```text
go test ./store/... ./store/role/... ./store/workunit/... ./backend/multiagent/... ./test/ -count=1
```

## 2.13 M9：Role / WorkUnit（已完成）

### 2.13.1 已完成切片

- `store/role` 承接 team registry、role draft WAL、permission 登记、角色会话
  路径与提交身份。
- Role draft sync 由 store/role 的 `SyncDraft` 唯一判定：排序 → main message
  原子提交 head/floor → 成功后才删 draft；commit/head 失败均保留 draft。
- `store/workunit` 承接 node/subagent/teammate 记录形状、`subagents/` 布局、
  原子写、枚举、删除、终态更新与写侧责任链。
- 根 `storeEngine` 删除 `roleStore`、`createRoleSession`、`newSubagent`、
  `appendRoleSessionRows`、`syncRoleDraft` 等 10 个领域方法；根 `jsonRepository`
  删除 node session 与 role permission 的 7 个落盘方法。
- 子树统一由 `newSubsessionEngine` 装配，只含 layout/message/event/lifecycle
  机制，不再递归装配 artifact/context/teamwork/role 同型子引擎。
- transcript EventKind 判据唯一化到 `store/session.EventKindOf`，根包只留旧 API
  转发与 contract protocol 常量。

### 2.13.2 红测与门禁

首次 M9 所有权门禁在 owner 文件缺失时失败；Qoder 中途实现后仍存在生产编译红灯：

```text
undefined: appendRoleDraft
undefined: readRoleDraft
layout.layout.createRoleSession undefined
root tests still reference removed roleStore/newSubagent/NodeSession internals
```

接管后修复 root 到新 owner 的单层转发、role draft 的会话根、permission 窄端口，
并把旧测试迁到领域 API。完整验收：

```text
go test ./store/... ./store/role/... ./store/workunit/... ./backend/multiagent/... ./test/ -count=1
go test ./e2e -run 'TestStoreDDDM9RoleWorkUnitOwnershipGate|TestStoreDDDMTeamworkIOOwnershipGate|TestStoreDDDMTeamworkModelOwnershipGate' -count=1
go build ./...
go vet ./...
```

全部 PASS。

### 2.13.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `store/role_session.go` draft 文件锁/追加/残尾/排序/commit-id/sync | `store/role/draft.go` |
| `store/role_session.go` 角色子树路径与提交身份 | `store/role/session_path.go` |
| `store/role_permission.go` 权限文件形状、规范化、缓存、锁序 | `store/role/permission.go` |
| `store/node_session_store.go` 记录形状与 JSON 文件 I/O | `store/workunit/record.go` / `store/workunit/store.go` |
| `store/unit_record_writer.go` 写侧责任链 | `store/workunit.Writer` |
| 根 `store.sessionstore.go` EventKind fallback | `store/session.EventKindOf` |
| 根 `storeEngine` 上角色/子代理领域方法 | `newSubsessionEngine` + `store/role` / `store/workunit` free functions |

### 2.13.4 结构指标与数据兼容

- `storeEngine` 172 → 162；`jsonRepository` 90 → 83；`Router` 与
  `SessionGranularStore` 未增长。ratchet 基线已同步收紧。
- `store/role` 与 `store/workunit` 均不 import 根 `store`；根包只保留 alias、
  单层转发与组合根 lifecycle。
- 路径零迁移：`subagents/`、role draft、`role-permissions.json`、角色会话
  `goal_*` / `role_*` 目录名逐字不变。
- JSON tag、schema、floor/head 发布顺序与冷恢复语义不变；未触碰用户数据。

## 2.14 M10：Facade 清零（已完成）

### 2.14.1 已完成切片

- 新增 typed `store.Stores`：一次打开数据根，显式持有 Router、Artifact、Context、
  Board、Teamwork、Role 与 ToolRecord owner；bootstrap 只注入具体 owner 或窄端口。
- 删除 `Router.BoardsFor`、`BoardsForSession`、`TeamworkFor`、`ToolRecordFor`
  与自由函数 `Boards`、`Teamwork`、`ToolRecord`、`MediaStoreOf`。
- goal/team 会话看板形状下沉 `contract/dto/session_board.go`；校验、module ID、
  head 发布与缺失读语义唯一实现迁入 `store/board/session.go`。
- 工具记录 JSONL 追加、最后完整快照、残尾/坏行读取唯一实现迁入
  `store/record/store.go`；根 `tool_record.go` 只留 `ToolRecordFile` alias。
- `backend/multiagent/teamwork` 直接消费 `store/teamwork.Store` 的窄接口；
  `teamwork_items.go` 根转发文件删除。
- goal 看板绑定改由 Runtime 持有会话级 `board.BoundStore`，wiring adapter 不再
  回头调用 Router 做能力探测。
- 新增 M10 Facade Gate、God 方法 ratchet 收紧与 79 文件根 import allowlist。

### 2.14.2 红测与外部批次

首次失败：

```text
go test ./e2e -run '^TestStoreDDDM10FacadeGate$' -count=1
FAIL: 4 个能力发现方法、4 个自由函数仍在；board/record/stores owner 缺失；
      根 tool_record.go 仍有 I/O；根 store 生产 importer=85（上限 80）
```

外部批次：

- checkpoint：`f49f4ec791957d8e581d3f3874580511c84848c1`
- manifest：`impact/M10-external-deps.{json,md}`
- replay branch：`codex/store-ddd-M10-replay`
- script：`scripts/store-ddd/M10-external-migrate.py`
- patch：`scripts/store-ddd/M10-external.patch.b64`
- 61 个目标 dry-run/apply 通过；隔离 worktree `go build ./...` 与结构门禁通过。
- rollback 后 replay 工作树零差异；main/replay 规范化内容 parity = ok。
- 最终 apply journal：`%TEMP%\seelex-store-ddd-M10-43bfjp6f\journal.json`

### 2.14.3 删除/合并清单

| 原位置 | 新唯一位置 |
|---|---|
| `Router.BoardsFor*`、根 `BoardRepository` 能力发现 | `store.Stores.Boards` + `store/board.SessionStore.Bind` |
| `store/board.go` 的 goal/team 存档校验与 I/O | `store/board/session.go` + `contract/dto/session_board.go` |
| `Router.TeamworkFor`、根 `TeamworkRepository` 适配 | `store/teamwork.Store` + 消费方 `planStoreRepository` |
| `store/teamwork_items.go` | `store/teamwork/model.go` / `store.go`；文件删除 |
| `Router.ToolRecordFor`、根 `jsonRepository` 工具记录 I/O | `store/record/store.go` + bootstrap `toolRecordStore` 窄端口 |
| `MediaStoreOf` | 消费方 `multimodal.MediaReader` / 装配期直接注入 |
| 根 God receiver 的 4 个 Board、7 个 Teamwork、2 个 ToolRecord 领域方法 | 各领域 owner；根只留同身份 alias |

### 2.14.4 结构指标

| 类型 | M9 | M10 |
|---|---:|---:|
| `Router` | 130 | 126 |
| `jsonRepository` | 83 | 70 |
| `storeEngine` | 162 | 161 |
| `SessionGranularStore` | 46 | 46 |

- 根 `store` 生产 importer：85 文件 / 19 包 → 79 文件 / 17 包；逐文件 allowlist
  ratchet 只许收缩。
- `store/board`、`store/record`、`store/teamwork` 均不 import 根 `store`。
- wiring 不再执行能力探测；`Store` 只是装配期 typed owner bundle。

### 2.14.5 数据兼容与验证

- 路径、JSON tag、schema、head、watermark、teamwork 文件布局零变化。
- 未读写、迁移或清理用户数据；全部测试使用 `t.TempDir()`。
- M10 门禁与全量验证通过：

```text
go build ./...
go test ./application/... ./middle/... ./backend/... ./store/... ./wiring/... ./e2e/... -count=1
go vet ./...
gofmt -l .
git diff --check
```

本里程碑最终本地 commit SHA 由交接报告记录；不 push。

## 3. 未决风险

1. **冷读语义必须先裁决再改**。当前 `ReadRange(ctx, key, offset, limit)` 的
   `limit <= 0` 是「只取总数」而不是「取全部」；provider 消息只来自**完整事件单元**
   （孤儿 tool 行不进 provider 上下文）；`ReadConversationRange` 是派生视图
   （assistant 的每个 tool_call 与 tool 行各展开一条）。M4 把写序提升为
   Application `CommitTurn` 时，这三条属于**兼容要求**，不允许用「新实现更合理」直接改。
2. **I6 还有 3 条例外**。`store` 与 `store/schedule` 的 `kernel/event` 边要等事件端口
   下沉 `contract` 才能归零；在那之前 `max: 1` 的 ratchet 不允许加第二条。
3. **新领域包默认零允许**。`store/<domain>` 一旦出现在 `go list ./store/...`，
   就自动受 I6 约束且没有例外额度；需要例外的包必须在
   `storeBoundaryRegisteredScopes` 里登记并说明归零条件，否则视为越界。
4. **`tmp/reshape/{r5check,r6verify,r7verify,verify-wt}` 是历史快照副本**（各自带
   `go.mod`，不参与 `go list ./...`，其中 2145 个 `.go` 文件同样 import 根 `store`）。
   它们会让 grep 类统计严重失真，但属于工作区数据，未经用户确认不删除；
   统计时一律排除。
5. **并发会话正在改同一批目录**。`application/*`、`backend/engine/context/seelectx/*`、
   `store/README-session.md` 等有另一会话的未提交改动，本里程碑不触碰这些文件。
6. **计划中的 `middle/view/board` 已不是当前物理路径**。实际看板消费方在
   `kernel/board`，其生产端口已经使用 `contract/dto`；M1 按实际依赖面验证。
   M10 前需统一 plan/milestones 的历史路径措辞，但不能为路径名再复制一套消费方。
7. **M7 外部批次已闭环**。checkpoint `8fbae4e8`、两个 patch、manifest、
   replay dry-run/apply、测试、tree parity 与 rollback 均有证据；后续批次继续
   沿用相同流程。

## 4. 附录：直接 import 根 `store` 的生产文件（86）

```text
backend/engine/assemble/runtime.go
backend/engine/assemble/runtime_agent_conversation.go
backend/engine/assemble/runtime_context.go
backend/engine/assemble/runtime_deps.go
backend/engine/assemble/runtime_project_board.go
backend/engine/assemble/runtime_role_plugins.go
backend/engine/assemble/runtime_session.go
backend/engine/assemble/runtime_subagent.go
backend/engine/assemble/runtime_subagent_recovery.go
backend/engine/assemble/runtime_subagent_resume.go
backend/engine/assemble/runtime_teammate_inspection.go
backend/engine/assemble/runtime_teamwork.go
backend/engine/assemble/runtime_teamwork_board.go
backend/engine/assemble/runtime_teamwork_board_archive.go
backend/engine/assemble/runtime_teamwork_board_close.go
backend/engine/assemble/runtime_teamwork_context.go
backend/engine/assemble/runtime_teamwork_curated.go
backend/engine/assemble/runtime_teamwork_scene.go
backend/engine/assemble/runtime_teamwork_scope.go
backend/engine/assemble/runtime_unit_recovery.go
backend/engine/assemble/workunit_assembly.go
backend/engine/assemble/workunit_team_records.go
backend/engine/context/seelectx/assembler.go
backend/engine/context/seelectx/controller.go
backend/engine/context/seelectx/dag.go
backend/engine/context/seelectx/frame.go
backend/engine/context/seelectx/gap.go
backend/engine/context/seelectx/memory/memory.go
backend/engine/context/seelectx/replay.go
backend/engine/context/seelectx/replay_material.go
backend/engine/context/seelectx/router_storage_conformance.go
backend/engine/context/seelectx/search/search.go
backend/engine/obs/event_correlation.go
backend/engine/obs/unified_events.go
backend/multiagent/agentsession/factory.go
backend/multiagent/subagent/conclusion.go
backend/multiagent/subagent/resume_units.go
backend/multiagent/subagent/session_factory.go
backend/multiagent/subagent/subagent_sessions.go
backend/multiagent/subagent/subagent_tree.go
backend/multiagent/team/plugin_assembly.go
backend/multiagent/team/plugin_reject.go
backend/multiagent/teamwork/board_archive.go
backend/multiagent/teamwork/board_view.go
backend/multiagent/teamwork/context_view.go
backend/multiagent/teamwork/coordinator.go
backend/multiagent/teamwork/items.go
backend/multiagent/teamwork/items_gate.go
backend/multiagent/teamwork/milestone_lifecycle.go
backend/multiagent/teamwork/output_store.go
backend/multiagent/teamwork/plan_query.go
backend/multiagent/teamwork/planlock.go
backend/multiagent/teamwork/scene.go
backend/multiagent/teamwork/store.go
backend/multiagent/teamwork/teamwork.go
backend/multiagent/teamwork/tool_requests.go
backend/multiagent/teamwork/unit_record.go
backend/multiagent/teamwork/unit_recovery.go
backend/tool/io/media/media.go
backend/tool/io/multimodal/content.go
backend/tool/worktree/worktree_manager.go
backend/workunit/progress.go
backend/workunit/recovery.go
backend/workunit/session.go
kernel/board/boardtest/store.go
kernel/context/compaction_stack.go
kernel/context/wiring.go
kernel/goal/audit.go
kernel/goal/board_archive.go
kernel/goal/sessionstore_store.go
kernel/session/archive.go
kernel/session/fork.go
kernel/session/ports.go
kernel/session/transcript_range.go
wiring/adapters/compressed_turn_store.go
wiring/adapters/goal_coordinator.go
wiring/adapters/role_permission_port.go
wiring/adapters/runtime_narrow_ports.go
wiring/adapters/session_manager.go
wiring/adapters/session_ssh_state_port.go
wiring/adapters/session_status_record.go
wiring/adapters/session_workspace_ports.go
wiring/bootstrap/application.go
wiring/bootstrap/bootstrap.go
wiring/bootstrap/runtime.go
wiring/registry/registry.go
```

重算命令（排除独立模块快照）：

```bash
grep -rl --include='*.go' '"github.com/RedHuang-0622/seelex/store"' . \
  | sed 's#^\./##' | grep -v -e '^store/' -e '^tmp/' | sort
```

## 5. 下一步

进入 M10 Facade 清零：先建立当前 Router / jsonRepository / storeEngine /
SessionGranularStore 与生产 import 根 store 的精确基线，逐个把调用方切到消费方
窄端口，再删除 `*For` 能力发现口和领域转发。每批完成全仓门禁并创建本地 commit，
不 push。
