# 运行中会话影响空闲会话的输入框提交：输入框正文按会话归属（复现与修法，2026-09-20）

> 日期: 2026-09-20 | 范围: `gui/frontend/dist`（composer 归属规则 + 接线）、
> `application/core`（复现用例固化）、`CHANGELOG.md`、`docs/gui/CHANGELOG.md`、
> `docs/gui/modules/shell-and-interactions.md`
> 回归: `gui/frontend/dist/composer-input.test.mjs`（8 例新增）、
> `application/core/session_running_idle_submit_test.go`
> （`TestIdleSessionSubmitWhileOtherRunningLandsInViewSession`，新）
> 承接: [2026-09-20-frontend-detail-motion-and-explicit-session-submit.md](2026-09-20-frontend-detail-motion-and-explicit-session-submit.md)（提交**路由**）、
> [2026-09-20-running-session-vs-idle-composer-submit.md](2026-09-20-running-session-vs-idle-composer-submit.md)（草稿门 / 引擎别名）

## 1. 现象与来路

用户报告原文（两轮同一句话）：

> 一个运行中的会话依然会污染到输入框内容的发送，场景是一个会话在运行中然后我想发送消息到
> 一个没有运行中的会话。

前两轮各修了三处：**提交路由**（`composerSubmitPlan` → `SubmitToSession(视图会话 ID)`）、
**草稿门**（`materializeDraftForSubmit`）、**引擎别名**（`BeginNewSession` 按会话路由）。
本轮先问：这三处修完之后，"运行中会话影响没运行会话的输入框内容提交"还剩哪条机制？

## 2. 复现（两条，一条"干净"、一条 RED）

### 2.1 应用层：这条链路已经是干净的（复现为"无污染"，固化成回归）

先按用户描述把应用层走一遍：A 运行中 → 切到空闲会话 B（已驻留 → 热挂载）→ 在 B 提交。
提交走**两条入口**（普通输入显式钉会话、sigil 走 ambient 路由器），探针实测：

```text
PROBE1 view=sess-b status=idle chat.running=false queued=0 conv=0
PROBE2 view=sess-b chat.running=true queuedA=0 queuedB=0 conv(now)=[user:msg for idle B assistant:]
PROBE2 engine: A calls=1 hist=[{user task A}{assistant answer}] | B calls=1 hist=[{user msg for idle B}{assistant answer}]
PROBE3（ambient Submit 从空闲视图会话提交）engine: A calls=1 | B calls=1 hist=[{user ordinary text from idle B view}]
```

即：视图快照的运行态属于 B（不被后台运行中的 A 带偏）、引擎调用与可见会话各归各的、
A 的队列为空。**后端不是剩余污染的来源**——这条判据固化成
`TestIdleSessionSubmitWhileOtherRunningLandsInViewSession`（断言引擎调用归属、A 的
队列、视图运行态、两侧可见会话四个面）。

### 2.2 前端：输入框正文不按会话归属（RED，实测）

剩余机制在**壳层状态**：输入框正文（`elements.prompt.value`）与脏位（`composerDirty`）
都是全进程一份，切会话时既不留存也不清空。于是：

```
A 运行中，用户在 A 的输入框里写了"给 A 的插话"（还没发）
  → 点空闲会话 B（视图切走，后端一切正常）
  → 输入框里仍是"给 A 的插话"
  → 按 Enter：composerSubmitPlan 只认「当前视图会话」= B
  → A 的字被当成 B 的内容提交出去      ← 报告的那句话
```

同一处脏位还制造第二种症状：`restoreComposerDraft` 经 `shouldRestoreDraft` 要求
「非脏、未聚焦」才回填，而脏位是从 A 带过来的 → **B 自己的草稿正文被挡在门外**，
用户看到 B 的输入框里是 A 的字。

复现方式（RED 现场保留）：把归属规则写成纯函数 `composerViewSwitch` 的**现状版**
（只认"会话 ID 变了"，正文与脏位原样保留 = 修复前 app.js 的实际行为），先跑新用例：

```text
$ node --test --test-reporter=tap gui/frontend/dist/composer-input.test.mjs
not ok 13 - 写在一个会话里的未发送正文不会跟着视图切到另一个会话
    AssertionError: 切到空闲会话 B 后输入框必须是空的（A 的字不能留给 B 提交）
    '给 A 的插话' !== ''
not ok 14 - 切到空闲会话后提交的只能是该会话自己的内容
    AssertionError: 切换后的空输入框：回车是 no-op，不会把 A 的字发给 B
    '给 A 的插话' !== ''
not ok 16 - 首次挂载（还没有前置会话）保留已敲进去的内容，不吞字
not ok 19 - 留存表有上限（LRU）：正文只是未发送的本地草稿，不随会话数无限长
# pass 16
# fail 4
```

（夹具 `switchOf` 按真实调用序列驱动：起点"已在 from 会话、框里是 current"→ 切到 to。）

## 3. 修法

| # | 位置 | 改动 |
|---|---|---|
| 1 | `gui/frontend/dist/composer-input.js` | 新增纯函数 `composerViewSwitch({fromSessionID, toSessionID, current, dirty, stash})` → `{stash, text, dirty, switched}`：离开会话的未发送正文按会话 ID 留存（空串不占位、重写刷新 LRU 序、上限 24）；进入会话有自己的留存就用它并置脏，没有则正文置空 + **脏位置假**（留出草稿回填位）。ID 未变 / 目标为空 → `switched=false`，不动输入框 |
| 2 | `gui/frontend/dist/app.js` | 新增壳层状态 `composerStash` / `composerSessionID` 与 `syncComposerSession(snapshot)`，在 `render()` 里**先于** `restoreComposerDraft` 调用：会话 ID 真的变了才动输入框（正文、脏位、内联建议），切回草稿会话且有本地留存时补一次防抖落盘 |

刻意不改的部分：**不**把切走当成"丢字"处理——正文按会话留存，切回原会话时还在；
`SaveComposerDraft` 的后端契约（只认视图会话 + `Status=draft`）不动，因此"落盘写谁"
仍由后端判定，壳层不复制这条规则。

## 4. 验证（红 → 绿）

```text
# GREEN（composer 规则）
$ node --test --test-reporter=tap gui/frontend/dist/composer-input.test.mjs
    # pass 21 / # fail 0

# 全量前端
$ node --test gui/frontend/dist/*.test.mjs
    tests 394 / pass 394 / fail 0        （本轮新增 8 例：12 → 20 → 21）
$ node --check gui/frontend/dist/*.js gui/frontend/dist/*.mjs
    ok（全部文件）

# 应用层（含本轮固化用例 + 上一轮三例）
$ go test ./application/core/ -run 'TestIdleSessionSubmitWhileOtherRunningLandsInViewSession|TestSubmitToSessionMaterializes|TestBeginNewSessionDoesNotSerialize|TestSubmitDuringColdRestore|TestSubmitToSessionDefers' -count=1 -v
    --- PASS: TestIdleSessionSubmitWhileOtherRunningLandsInViewSession (0.06s)
    --- PASS: TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns (0.04s)
    --- PASS: TestSubmitToSessionMaterializesIdleDraft (0.01s)
    --- PASS: TestBeginNewSessionDoesNotSerializeBehindRunningSession (0.01s)
    --- PASS: TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder (0.00s)
    --- PASS: TestSubmitToSessionDefersUntilRestoreCompletes (0.32s)
$ gofmt -l application/core/      → 空
```

新增用例（`composer-input.test.mjs`，8 例）覆盖的边界：写在一个会话里的未发送正文不跟着
视图切走；切回原会话时正文回来且是"本地内容"（脏位真）；切到空闲会话后提交只能是该会话
自己的内容（空框 → 回车 no-op）；同一会话重复渲染不动输入框；首次挂载（无前置会话）不
吞已敲的字；目标会话 ID 缺失时不动输入框；空正文不留占位；留存表 LRU 上限。
另有两条 app.js 源码级断言（仓库既有口径）：必须经 `composerViewSwitch`、且
`syncComposerSession` 在 `restoreComposerDraft` **之前**。

## 5. 未取到的证据 / 未做（如实记录）

- **实机未取证**：本机没有运行中的 Seelex GUI 实例（webview 也无法脚本化驱动），
  因此"切会话后输入框清空"这一帧没有截图证据。判据落在确定性的纯函数用例 + app.js
  源码级断言上（与 2026-09-17/2026-09-20 两轮同一口径）。
- **在途防抖落盘**：`scheduleComposerSave` 的定时器在触发时读"当前视图会话是不是草稿"，
  因此"在草稿会话里敲了字、300ms 内切走"这一种时序仍可能不落盘（切回时正文靠本地留存
  还在，重启后才会丢）。修它需要按会话 ID 落盘的 API（`SaveComposerDraft(sessionID, text)`），
  属契约面变更，本轮不动；本轮已在"切回草稿会话且正文来自本地留存"时补一次落盘。
- **多页签各持 composer** 的目标形态（见
  `docs/2026-09-02-session-subsystem-remediation/target-design.md`）落地时，本地留存表
  与草稿槽位要一起分布化——那时的判据是"每个页签各自的输入框"，本轮的
  `composerViewSwitch` 正是它的单实例版。
