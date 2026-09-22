# 会话页输入框草稿（composer draft）的页面可见性与生命周期收敛（2026-09-22）

> 日期: 2026-09-22 | 范围: `application/core`（composer 草稿的落盘/清空收敛）、
> `gui/frontend/dist`（新增草稿生命周期纯函数 + 接线说明）、本日志
> 回归: `application/core/composer_draft_test.go`
> （`TestComposerDraftSessionRowVisible`、`TestComposerWorkspaceRebindConvergesOnMaterialize`，新）
> `gui/frontend/dist/draft-lifecycle.test.mjs`（14 例，新）
> 承接: [2026-09-20-idle-composer-content-session-scope.md](2026-09-20-idle-composer-content-session-scope.md)（正文按会话归属）、
> [2026-09-20-running-session-vs-idle-composer-submit.md](2026-09-20-running-session-vs-idle-composer-submit.md)（草稿门 / 物化）

## 1. 事实侦察（草稿相关真实代码面，文件:行）

后端：

| 位置 | 事实 |
|---|---|
| `application/core/composer_draft.go:29` `SaveComposerDraft` | 只认**当前视图的草稿会话**（`Snapshot.Session.Draft && Session.ID != ""`），非草稿会话一律报错；写内存单元 + `Snapshot.Session.Composer` 后落盘 |
| `application/core/composer_draft.go:70` `draftWorkspaceID` | 草稿 record 的项目键 = 草稿槽的 `Workspace`，否则 = `Snapshot.CurrentWorkspace`——**键可以在草稿存续期间漂移** |
| `application/core/composer_draft.go:87` `persistComposerDraftIn` | 写一条**只有** Version/ID/Composer/UpdatedAt/Status 的 record；`text != ""` 才标 `Status=draft` |
| `application/core/composer_draft.go:127` `clearComposerDraft`（改前）| 物化成功后清 composer：**只在内存单元还有正文（`hadText`）时**才把清空写回**唯一一个** `projectID` 键 |
| `application/core/session_draft.go:219` | `clearComposerDraft(newID, clearProjectID)` 只由 `materializeDraftSession` 调用（一轮完成 = 草稿唯一终点；取消不碰草稿） |
| `application/core/composer_draft.go:182` `restorePersistedDraft` | 冷启动（`initialDraft`）跨项目找 `DraftCandidates` 恢复草稿；**引擎已带会话时根本不调用**（`service_assembler.go:218` 的条件装配） |
| `application/core/session_runtime/scope.go:228` `DraftCandidates` | 候选判据 = `Status == draft`，跨**全部项目**枚举 |
| `application/core/session_snapshot.go:26/63/78-85` | 目录行的草稿身份**只由草稿槽位判定**：槽位存在才补草稿行、该行恒为 `draft`；其余行按单元运行态叠加（草稿单元空闲 → `idle`） |
| `application/core/service_state.go:34/95` | `draft *draftSlot`：草稿槽位是"新建会话草稿"的持有责任者 |

前端：

| 位置 | 事实 |
|---|---|
| `gui/frontend/dist/app.js:563` `scheduleComposerSave` | 草稿会话输入后 300ms 防抖 `SaveComposerDraft(value)`（触发时重读"当前视图是不是草稿"） |
| `gui/frontend/dist/app.js:593` `syncComposerSession` → `gui/frontend/dist/composer-input.js:78` `composerViewSwitch` | 输入框正文按会话归属（LRU 留存 24） |
| `gui/frontend/dist/app.js:621` `restoreComposerDraft` → `composer-input.js:125` `shouldRestoreDraft` | 整份快照回填后端草稿正文（聚焦 / 脏位时拒绝） |
| `gui/frontend/dist/app.js:634-638` | `render` 顺序：先 `syncComposerSession`、后 `restoreComposerDraft` |
| `gui/frontend/dist/app.js:1467` `sessionRow` | `id === "" && status === "draft"` 的"新建会话草稿"行是**死分支**：早分配 SID 后草稿行恒有 ID（后端 `Snapshot` 补的行），走的是普通行 + `session-status is-draft` 芯片 |
| `gui/frontend/dist/app.js:3305` `beginNewSession` / `3315` `bindWorkspaceAndStart` | 「在该工作区新建会话」= `BeginNewSession`（草稿态幂等）+ `BindWorkspace`：**同一份草稿改绑项目** |

页面 context 的现状：会话页只渲染 `snapshot.conversation`（既定 message），本会话当前**未发送**的草稿只活在输入框里——页面里看不到它，也无从与既定消息区分。

## 2. 复现（红 → 绿，实测）

三个候选缺口，全部先用现有夹具（`newDraftRecordStore` / `newWorkspaceDraftStore` / `mustDraftService` / `newFakeWorkspace`）写成会失败的用例。

### 2.1 草稿会话在会话树里"看不见"（RED → 已修）

```text
$ go test ./application/core/ -run TestComposerDraftSessionRowVisible -count=1 -v   # 改前
--- FAIL: TestComposerDraftSessionRowVisible (0.03s)
    composer_draft_test.go:167: draft row status = "idle", want draft
        (row={ID:draft_1790073103846285100_1 Name:新会话 ... Status:idle ...})
```

根因：`SaveComposerDraft` 只写 record（`Status=draft`）与内存单元，**不建草稿槽位**；
而"草稿行恒为 draft"的规则（`service_snapshot.go:78-85`）只对槽位行生效，其余行按单元
运行态叠加 → 这份草稿会话在目录里显示成 `idle`。用户口径"草稿会话在页面上看不到"正落在
这一格：record 与输入框里都有内容，会话树里那一行却没有草稿身份。

### 2.2 物化后残留幽灵草稿（RED → 已修）

```text
$ go test ./application/core/ -run TestComposerWorkspaceRebindConvergesOnMaterialize -count=1 -v   # 改前
--- FAIL: TestComposerWorkspaceRebindConvergesOnMaterialize (0.00s)
    composer_draft_test.go:215: draft residue after materialize: map[project-1:{Version:3 ID:draft_... Status:draft
        Composer:{Text:改绑前写下的未发送正文 ...}}]
```

复现路径（都是真实前端路径）：草稿绑 `project-1` → 写未发送正文（record 落 `project-1`）→
「在该工作区新建会话」把这份草稿改绑到 `project-2`（`app.js:3315` 的 `bindWorkspaceAndStart`
幂等 + `BindWorkspace`）→ 首次发送物化。物化按 `Snapshot.CurrentWorkspace` 只清
`project-2`，`project-1` 那份 `Status=draft` 的 record 原样留下。

后果链（重启后）：`DraftCandidates`（跨项目）仍把它当候选 → `restorePersistedDraft` 把
**这份已物化会话**当草稿槽恢复回页面（`Name=新会话`、`Draft=true`、正文是旧的未发送文字）
→ 页面 context 变成"草稿正文 + 看不到既定消息"，与"页面要同时看到既定消息与草稿且可区分"
正好相反。

### 2.3 候选缺口（未修，如实记录）

**冷启动的空白草稿没有草稿槽位**：装配器在 `initialDraft` 时早分配 SID（`service_assembler.go`
`initialDraft` 分支）但**不建槽位**，`restorePersistedDraft` 又只在找到持久候选时才建槽。
于是"启动即草稿"的会话不在 `Snapshot().Sessions` 里（前端只能按 `current` 合成一行、拿不到
`draft` 状态）。

```text
$ go test ./application/core/ -run TestProbeColdStartDraftRowVisible -count=1 -v   # 临时探针（已删）
--- FAIL: ... PROBE1 RED: draft session "draft_1790072652964971600_1" missing from directory rows []
```

试过在装配/恢复期补槽位（`registerViewDraftSlot`），但 `TestResidentLimitEvictsLeastRecentlyUsedIdle`
（`resident_lru_test.go:81`：`snapshot rows after settle = 4, want 3`）**依赖"启动期目录里
没有草稿行"**这条现状。该测试不在本轮白名单内、也不该为一个空白草稿改口径，因此本轮改为
**只在首份未发送正文落盘时**登记槽位（§2.1 的修法）——空白草稿（用户还没敲字）仍不进会话树，
与 `session_lifecycle.go:174`（卸载后空白草稿"不写槽位、不进入会话树"）的既有语义一致。

另一条候选缺口（**已修**，见 [2026-09-22-composer-draft-restore-across-restart.md](2026-09-22-composer-draft-restore-across-restart.md)）：**引擎已带会话时（`initialDraft=false`）持久化草稿不被恢复**
（`service_assembler.go:218` 只在 `initialDraft` 时调用 `restorePersistedDraft`）。此时那份
草稿的目录行会被 `enrichDirectoryRowsLocked` 叠成 `idle`（槽位不存在），而重启后它既不是
视图会话、也不在草稿槽里 → 草稿正文无处可见。修它要引入"草稿行与视图会话可分离"的目录
语义（多草稿并行），属设计面变更，本轮不动。

> 后续（2026-09-22 晚）：该缺口已收口，且根因比这条记录更深——草稿正文此前写的是
> **已退役的 record 通道**（v8 下正文被丢弃、目录行也不标 `draft`），因此连"草稿候选"
> 都找不到。现在草稿正文走 sessionstore 的 lifecycle 草稿通道（`input/draft.json`，
> 与消息通道分离），目录行的 `draft` 身份就是这份文件的存在性；`initialDraft=false`
> 的启动形状由"目录收敛后的后台判断"补装（装配期仍不读会话目录）。详见
> [2026-09-22-composer-draft-restore-across-restart.md](2026-09-22-composer-draft-restore-across-restart.md)。

## 3. 修法（后端，最小改动）

`application/core/composer_draft.go`：

| # | 改动 | 说明 |
|---|---|---|
| 1 | 新增 `registerDraftSlotLocked(sessionID)`，在 `SaveComposerDraft` 写正文时（`text != ""`）调用 | 首份未发送正文落盘即登记草稿槽位 → 会话树里这一行以 `draft` 出现（与 record 的 `Status=draft` 判据同源：都只在正文非空时成立）；不覆盖已有槽位 |
| 2 | 新增 `draftResidueProjects(sessionID)`；`clearComposerDraft` 改为按**残留状态**收敛：清掉所有仍以 `Status=draft` 标记该会话的项目键（另加 `hadText` 时的物化项目键） | 物化是草稿唯一终点，清空必须清到"磁盘上所有还在冒充草稿的键"，而不是"内存里记得的那个键"；目录不支持分项目枚举（非 `SessionGranularPort`）时退化为原行为（该端口没有项目维度） |

未动：`SaveComposerDraft` 的契约（只认视图草稿会话）、`persistComposerDraftIn` 的写入形状、
取消路径（一轮被取消**不该**动草稿——取消不是草稿的终点）。

## 4. 前端：草稿生命周期纯函数（`gui/frontend/dist/draft-lifecycle.js`）

不碰 DOM、不碰 Bridge；`app.js` 只把事件接上去。

| 导出 | 语义 |
|---|---|
| `draftLifecycleFromSnapshot({sessionID, draft, composer})` | 由权威快照派生 `{sessionID, attached, text, phase}`；`attached` = 挂在"未物化草稿会话"上（已物化会话不构成草稿归属） |
| `draftLifecycle(state, event)` | 迁移唯一入口：`append` → `submit`（`remaining` 由 `clearSubmittedText` 算好传入）→ **`materialize`**（一轮完成：已发送的正文归消息、未发送的剩余仍是草稿）\| **`cancel`**（一轮被取消：正文按未发送保留）→ `clear`（权威清空）；`sending` 期间继续敲字不打断轮次 |
| `composerDraftRows({conversation, state})` | 页面行 = 既定 message + 末尾一条 `kind:"draft"`（`draft:true`/`unsent:true`）的未发送草稿行；空草稿不出行 |
| `draftRoundEvent({wasRunning, isRunning, cancelled})` | 一轮结束 → 收敛事件（`materialize` / `cancel` / `""`）；`cancelled` 由壳层给出（用户点了停止/取消） |

## 5. app.js 接线说明（本轮**不改** app.js，由主 Agent 接线）

1. **import**（`gui/frontend/dist/app.js:4` 一带，与 `composer-input.js` 同处）：
   ```js
   import { draftLifecycle, draftLifecycleFromSnapshot, composerDraftRows, draftRoundEvent } from "./draft-lifecycle.js";
   ```
2. **壳层状态**（`app.js:500-511` 的 `composerDirty/composerStash/composerSessionID` 旁）：
   ```js
   let composerDraft = draftLifecycleFromSnapshot(); // {sessionID, attached, text, phase}
   let composerRoundRunning = false;                 // 上一次渲染的 chat.running
   let composerCancelled = false;                    // 壳层知道这次结束是不是用户取消的
   ```
3. **`render(snapshot)` 里**（`app.js:634` `render()` 开头，**在** `syncComposerSession` /
   `restoreComposerDraft` **之后**，参数用渲染后的输入框正文作为权威本地正文）：
   ```js
   const roundEvent = draftRoundEvent({
     wasRunning: composerRoundRunning,
     isRunning: Boolean(snapshot.chat?.running),
     cancelled: composerCancelled
   });
   composerCancelled = false;
   composerRoundRunning = Boolean(snapshot.chat?.running);
   if (roundEvent) composerDraft = draftLifecycle(composerDraft, { type: roundEvent });
   // 权威起点：快照说这份会话还是未物化草稿时，attached 与正文以后端为准（本地未落盘
   // 输入仍由 composerDirty 挡住回填，见 shouldRestoreDraft）。
   composerDraft = draftLifecycle({ ...composerDraft, sessionID: snapshot.session?.id || "" }, { type: "append", text: elements.prompt.value });
   ```
   （最小接法：每个整份渲染都把"当前框内正文 + 当前会话 ID"喂给 `append`，`attached` 由
   `draftLifecycleFromSnapshot` 的返回值在切会话时重取；`composerDraft.sessionID` 变化时
   改用 `draftLifecycleFromSnapshot({sessionID, draft: snapshot.session?.draft, composer: ""})`
   重置，避免把上一个会话的字算成这个会话的草稿。）
4. **输入事件**（`app.js:3208` `markComposerEdited` 旁、以及 `app.js:3117` 程序化写入后）：
   ```js
   composerDraft = draftLifecycle(composerDraft, { type: "append", text: elements.prompt.value });
   ```
5. **提交**（`app.js:3151-3172` submit 处理器里，算出 `clearSubmittedText(text, sent)` 之后）：
   ```js
   composerDraft = draftLifecycle(composerDraft, { type: "submit", remaining: <clearSubmittedText 的返回值> });
   ```
6. **取消**（停止按钮 `app.js:3213` 的 click 处理器里、`invoke("CancelChat", "")` 之前）：
   ```js
   composerCancelled = true;   // 让下一次渲染的 draftRoundEvent 发 cancel（正文按未发送保留）
   composerDraft = draftLifecycle(composerDraft, { type: "cancel" });
   ```
7. **会话页渲染草稿行**（`app.js` 会话区渲染处，用 `composerDraftRows` 替代直接渲染
   `snapshot.conversation`）：
   ```js
   const rows = composerDraftRows({ conversation: snapshot.conversation || [], state: composerDraft });
   // rows 里 kind === "draft" 的那条渲染成草稿行（加 is-draft 样式 + "未发送"标记），
   // 其余按既有消息渲染；两者同页可见、按 kind 区分。
   ```
   渲染草稿行时**不要**写回 `elements.prompt`（输入框正文是本地事实源，草稿行只是页面投影）。

## 6. 验证（红 → 绿，实测输出）

```text
# 改前（stash 掉 composer_draft.go 的修复）
$ go test ./application/core/ -run 'TestComposerDraftSessionRowVisible|TestComposerWorkspaceRebindConvergesOnMaterialize' -count=1
--- FAIL: TestComposerDraftSessionRowVisible (0.03s)
    composer_draft_test.go:167: draft row status = "idle", want draft
--- FAIL: TestComposerWorkspaceRebindConvergesOnMaterialize (0.00s)
    composer_draft_test.go:215: draft residue after materialize: map[project-1:{... Status:draft Composer:{Text:改绑前写下的未发送正文 ...}}]
FAIL	github.com/RedHuang-0622/seelex/application/core

# 改后
$ go test ./application/core/ -run 'TestComposer' -count=1 -v
--- PASS: TestComposerDraftPersistsAndRestoresAcrossRestart (0.03s)
--- PASS: TestComposerDraftSessionRowVisible (0.00s)
--- PASS: TestComposerWorkspaceRebindConvergesOnMaterialize (0.00s)
--- PASS: TestComposerWorkspaceDraftBindingPersistsAcrossRestart (0.00s)
ok  	github.com/RedHuang-0622/seelex/application/core	0.412s

$ go test ./application/core/... -count=1
ok  	.../application/core	10.491s（+ agentteam/chat/context_control/context_runtime/goal/govern/input_router/
     prompt_layer/resume/session_runtime/subagent_view/task_context/worktable 全 ok）
$ gofmt -l application/core/   → 空

$ node --test gui/frontend/dist/draft-lifecycle.test.mjs
ℹ tests 14 / pass 14 / fail 0
$ node --test gui/frontend/dist/composer-input.test.mjs gui/frontend/dist/draft-lifecycle.test.mjs
ℹ tests 35 / pass 35 / fail 0
$ node --test gui/frontend/dist/*.test.mjs
ℹ tests 417 / pass 417 / fail 0
$ node --check gui/frontend/dist/draft-lifecycle.js   → ok
```

## 7. 未完成 / 有争议

- **实机未取证**：本机没有运行中的 Seelex GUI 实例（webview 也无法脚本化驱动），
  "页面上看到草稿行"没有截图证据；判据落在确定性纯函数用例 + 后端目录行断言上。
- **app.js 未接线**（白名单禁止）：页面渲染草稿行、轮次结束收敛事件都还只存在于
  `draft-lifecycle.js`；接线前用户看不到草稿行，§5 是精确接法。
- **冷启动空白草稿仍不进会话树**（§2.3 候选缺口 1，附 RED 证据）：修它要动
  `TestResidentLimitEvictsLeastRecentlyUsedIdle` 依赖的现状，本轮只修"有未发送正文时
  草稿行必须有草稿身份"。
- **引擎已带会话时的持久化草稿不可见**（§2.3 候选缺口 2）：需要"目录里的草稿行与视图
  会话可分离"的设计，本轮不动。
- **草稿正文只对未物化草稿会话落盘**（`SaveComposerDraft` 契约）：已物化会话里的未发送
  正文仍只活在本地留存表（`composerStash`）+ 输入框，重启即丢。改它要动 record 契约
  （给已物化会话写 `Composer` 而不覆盖其 `Conversation`/`Title`），与
  `session_runtime/archive.go:56-62` 的"落盘重建继承既有记录"路径有耦合，留待专门一轮。
- **`application/core/README-composer.md` 未同步**（白名单未授权，留给主 Agent 一条命令）：
  本轮在 `composer_draft.go` 新增了 `draftResidueProjects` / `containsProjectID` /
  `registerDraftSlotLocked` 三个函数，该文件的「文件与函数索引」由
  `python scripts/gen_core_readme_index.py` 生成，需要重跑一次（会同时刷新 `application/core`
  下其它分卷，若其它卷本就漂移请一并 review）。散文部分（生态位/边界）不受影响。
- `clearComposerDraft` 的残留扫描是**物化时一次**的目录枚举（项目数量级），不进热路径；
  若将来项目数很多，可在草稿槽位上记"上次落盘的项目键"以省掉扫描。
