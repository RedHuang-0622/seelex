# 草稿跨重启恢复收口：草稿正文改走 lifecycle 草稿通道 + 装配期补装（2026-09-22 晚）

> 日期: 2026-09-22 | 范围: `sessionstore`（lifecycle 草稿通道的路由/粒度面）、
> `internal/adapters`（`SessionPort` 草稿能力）、`application/core`（草稿持久化与
> 装配期恢复）、`composer_draft_live_smoke_test.go`（第 4 条升硬断言）
> 承接: [2026-09-22-composer-draft-lifecycle.md](2026-09-22-composer-draft-lifecycle.md)
> （草稿槽位/清空收敛）、[2026-09-22-draft-page-wiring-and-smoke.md](2026-09-22-draft-page-wiring-and-smoke.md)
> §3.4（把"重启后未发送草稿还能不能回来"记成观测的那一条）

## 1. 现象

冒烟第 4 条（重启后未发送草稿能不能回来）实测：视图会话是另一条 `sess_…`
（`draft=false`、`composer=""`），草稿会话连目录行都不出现（`存在=false`）。

## 2. 根因（两层，第二层是此前没记到的）

**第一层（真正的根因）：草稿正文写在了 v8 已退役的 record 通道上。**
`persistComposerDraftIn` 用 `SaveSessionRecordWorkspace`（`state`/record 通道）落
`Composer.Text`；但 v8 布局里这条通道已退役——`SessionGranularStore.SaveRecordRaw`
只做 `EnsureIndexed` + 标题/归档写穿，**record 正文被直接丢弃**，`LoadRecordRaw`
按既有通道派生一个骨架 record。于是：

- 草稿正文根本不在磁盘上（重启后无从恢复）；
- 目录行的 `draft` 身份由**派生 record** 给出，判据是 lifecycle 草稿文件是否存在
  （`jsonRepository.hasDraft` → `derivedRecord`/`derivedRecordPayload` 的
  `status=draft`，§2.5.4），而草稿从未写过这份文件 → 行恒为 `idle`。

探针实测（`SessionsOf("")`，重启后）：

```
PROBE row id= draft_1790077391770211100_1  status= idle  name=
```

`DraftCandidates()` 只认 `Status=draft`，因此**恒为空** → `restorePersistedDraft`
即使被调用也找不到候选。此前记录的"引擎已带会话时（`initialDraft=false`）不恢复"
是第二层：

**第二层：装配器只在 `initialDraft`（引擎 `SessionID()==""`，即生产冷启动形状）
时调用 `restorePersistedDraft`。** 宿主若在装配前把引擎预置成一条**没有内容**的
会话（冒烟 harness 就是这种形状），这条恢复路径直接跳过。

## 3. 修法

### 3.1 草稿正文改走 lifecycle 草稿通道（draft 与 message 分开）

| 层 | 改动 |
|---|---|
| `sessionstore/lifecycle.go` | 新增 `setComposerDraft(key, content)`（空正文 = `state.Draft=nil` → `publishLifecycleLocked` **删除** `input/draft.json`）与 `lifecycleDraft(key)`（读回正文） |
| `sessionstore/sessionstore.go` | 新增 `Router.SaveComposerDraftWorkspace(projectID, sessionID, content)` / `ComposerDraftWorkspace(...)`（沿用 archived 的 `withRepositoryAt` + `*jsonRepository.layout` 派发，`handled=false` = 该后端没有草稿通道） |
| `sessionstore/session_granular.go` | `SessionGranularStore.SaveComposerDraft` / `LoadComposerDraft` 包装（粒度入口统一） |
| `internal/adapters/session_workspace_ports.go` | `SessionPort.SaveComposerDraftWorkspace` / `LoadComposerDraftWorkspace` |
| `application/core/composer_draft.go` | 新增可选端口 `composerDraftPort`；`persistComposerDraftIn` 在 record 写之后**再写草稿通道**（清空同理，真删文件 → 目录行不留幽灵草稿）；新增 `draftTextFor` 读回时**优先草稿通道**、回退 `record.Composer`（非 v8/旧数据） |

这条通道正是"draft 与 message 分开、降低 message 的 IO 密度"的落点：草稿编辑只
重写一份 `input/draft.json`，不进消息日志；而目录枚举的 `draft` 身份与它同源。

### 3.2 装配期补装（含"引擎已带会话"的形状）

- 冷启动形状（`initialDraft`）：`restorePersistedDraft(true)` 仍在装配期同步做
  （与既有行为一致）。
- 宿主预置引擎会话的形状：**装配期不读会话目录**（目录 IO 只由目录 worker 承担，
  见 `TestSnapshotDoesNotReadBlockedSessionCatalog` / `...ShutdownDoesNotWait...`），
  改为 `scheduleShellDraftRestore` 在**目录收敛后的后台判断**里补装：
  - `shellViewUntouched`：视图仍停在那个空壳会话上、启动窗口内没有用户动作
    （`viewEpoch == 0`、无草稿槽、未关闭/未排空）；
  - `claimShellView`：再要求那条引擎会话**没有用户可见内容**（`user/assistant/tool`；
    system 提示词不算——预置的空会话常常已带一条 system 提示）。有内容时草稿
    **不抢视图**，只登记草稿槽位并把正文回读进该会话单元（切回这份草稿时输入框
    拿得回正文）；
  - 等待上限 `shellDraftRestoreBudget = 5s`，超时即放弃本次补装（草稿仍在磁盘上）。
- 恢复落点的口径与 `/new`、`ResumeSession` 对齐：`Runtime.SwitchSessionTasks(草稿ID,
  record.Tasks)` 把该会话自己的 task 台账装回运行时注册表，再 `refreshWorkTableFromSources()`
  重建/发布工作表格投影——恢复后工作表格内容同样可见。

## 4. 测试与证据

单测（`application/core/composer_draft_restore_test.go`，四条判据；修复前 RED 见 §5）：

- `TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession`：预置空会话 → 补装回视图
  （draft + composer + 目录行 `draft`）；
- `TestComposerDraftKeepsViewOfContentfulEngineSession`：预置**有内容**的会话 → 视图不
  被抢、草稿槽登记、正文回读进单元；
- `TestComposerDraftRestoreRehydratesTaskLedger`：`record.Tasks` 随会话落盘 → 恢复后装回
  运行时注册表并出现在工作表格投影里；
- `TestComposerDraftTextSurvivesRetiredRecordChannel`：v8 形状（record 通道退役）下草稿
  正文只在草稿通道 → 跨重启读回、物化后通道清空。

存储层（`sessionstore/composer_draft_channel_test.go`）：草稿通道 → 目录 `status=draft`；
清空 → 回 `idle`；正文不进退役 record 通道；跨 Router 重开存活。

冒烟第 4 条从"观测"升成硬断言：

```
$ go test -tags draftsmoke . -run TestComposerDraftHeadlessSmoke -count=1 -v -timeout 10m
--- PASS TestComposerDraftHeadlessSmoke/草稿的留存：重启后未发送草稿装回视图
    草稿留存通过：视图会话="draft_…_1" draft=true composer=30 字；草稿行 status=draft；工作表格行=1
```

回归：`go test ./... -count=1`（67 包全绿，含 `application/core` 的阻塞目录两例与
`gui/...`）；`go test ./application/core/... -count=1` 全绿。

## 5. 修复前 RED（同一断言）

```
--- FAIL: TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession
    restored draft over precreated empty engine session = {ID:sess_host_precreated Draft:false Status:idle Composer:}
--- FAIL: TestComposerDraftRestoreRehydratesTaskLedger
    restored task ledger = [], want the drafted session's todo:0
--- FAIL: .../草稿的留存（观测）   → 重启后未发送草稿没有回到视图：session={ID:sess_… Draft:false Status:idle Composer:}
```

## 6. 本轮不做 / 遗留

- **目录行与视图会话可分离（多草稿并行）** 仍未做：同一时刻仍只有"一个新建会话草稿"
  槽位（`enrichDirectoryRowsLocked` 的幂等防重口径不变）。宿主预置**有内容**会话时
  草稿只登记槽位、不进视图——这正是那档设计要解决的部分。
- **补装是异步的**：预置引擎会话的宿主，首个快照可能仍显示空壳（随后被补装切到草稿）。
  生产冷启动（引擎为 nil）不受影响，仍是装配期同步恢复。
- **草稿行的视觉复核**：本机没有可脚本化驱动的 GUI 实例（webview），页面草稿行
  （draft 行在既有 message 之后、排队输入之前）仍只有纯函数用例 + 源码级落点断言。
