# 2026-09-17 冷恢复抢跑：修法、锁竞争数据流（现状 → 修复后）

接续 [2026-09-17-submit-during-cold-restore-repro.md](2026-09-17-submit-during-cold-restore-repro.md)
（该文只做复现与定位，未改行为）。本文落地其 §5 的建议修法，并给出**争用发生时的
数据流**与**修复后的缩争用数据流**。

范围：`application/core`（后端门 + 延后挂载）、`gui/frontend/dist`（输入区锁）、
回归用例（Go 红→绿、前端红→绿）。

---

## 1. 判据不变式

> 会话处于 `restoring` 时，**应用层不得接受任何对话输入**（拒绝或延后），
> 不得依赖前端 `prompt.disabled` 作为唯一防线。

前端输入区锁只是**呈现**；正确性由应用层保证。

## 2. 修法（三处）

| # | 位置 | 改动 | 作用 |
|---|---|---|---|
| 1 | `application/core/service_input.go` | `submitConversation` / `submitConversationFor` 在 `Core.ViewMu` 临界区内查 `isRestoringLocked(sessionID)`：命中时**既不拒绝也不阻塞**，`deferSubmitUntilRestored(ctx, sessionID, input)` 后立即 `return nil` | 治本：把「前端锁」升级为「后端不变量」，且**不丢输入**——延后到装载完成点、再在**同一目标会话**上启动 |
| 2 | `application/core/session_scope.go` + `service_snapshot.go` | `deferSubmitUntilRestored` / `awaitRestore`：挂到新增状态 `restoreSig`（`clearRestoringLocked` 广播）上，收到信号重读集合复判（一个信号可能对应别的会话）；`SubmitToSession` 命中同一条恢复门（经 `submitConversationFor`） | 保留 `SubmitToSession` 的**非阻塞后台启动**契约（立即受理、调用方不阻塞），同时满足不变式（延后语义） |
| 3 | `application/core/chat.go` | `startChatFor` 保留 `isRestoringLocked(sessionID)` → `ErrSessionRestoring` 的**兜底** | 收口 submit 释放 `ViewMu` 到启动之间那一段 TOCTOU；正常路径不会走到（submit 已延后），故它为纯防御 |
| 4 | `gui/frontend/dist/chat-view.js` + `app.js` | `renderControls(snapshot, switching)`：`switching` 时**只锁输入**（prompt/send/文案/placeholder），不动 stop-button 与 history-bar；`restoring` 仍是完整只读壳。`resumeSessionFromList` 置/清 flag 时**显式重渲** composer | 覆盖「后端已切、前端还没渲染 restoring」的窗口，且不越界锁掉停止与历史回看 |

新增状态：`Service.restoreSig chan struct{}`（`Core.ViewMu` 保护）——「restoring 集合
有变化」的广播信号，`awaitRestore` 收到后重读集合复判（多会话并发装载时一个信号可能
对应别的会话）。

## 3. 现状数据流：争用（两个写者抢同一份可见会话）

锁/判定点只有一处、且在**前端的一次渲染**上；后端 submit 没有对应门。

```text
用户：A 运行中，切到冷会话 B
────────────────────────────────────────────────────────────────────────────────
[前端] 点击 B 行
   ├─ state.resumingSessionID = B          ← 只 disable 列表行按钮
   │  composer-status = "正在恢复会话…"      （prompt / send 仍可用）
   └─ invoke("ResumeSession", B) ─────────────────────────────┐
                                                              ▼
[后端] resumeSession(B)：hot=false、running=true ⇒ 异步冷加载
   ├─ beginAsyncRestore(B)
   │    Core.Snapshot.Session = {B, restoring}   ← 视图指针先于内容移动
   │    publishSessionChanged                     （前端尚未渲染到）
   └─ go resumeSessionCold(B) ──► LoadHistory(B) ⏳（大会话：慢 / 可能失败）

[前端] 渲染线程仍停在旧快照 A ⇒ composer 显示「就绪」，prompt.disabled=false
   │
   │   ★ 争用窗口：后端视图指针 = B，前端渲染 = A
   ▼
用户 Enter「typed while restoring」
   └─ invoke("Submit") ──► Submit
        └─ submitConversation
             ├─ 路由只看 Snapshot.Session.ID(= B) 与 ChatState().Running(= false)
             ├─ ✗ 不查 isRestoringLocked(B)          ⇒ err = nil（被接受）
             └─ startChatFor(B) ⇒ 在 restoring 空壳 B 上开真回合
   │
   └─ 此后 B 的「可见会话」有两个写者：
        写者① 用户抢跑回合（写 B 的 Conversation / 引擎历史）
        写者② 后台装载的恢复基线（resumeSessionCold 完成 → SetSessionView）
        ⇒ 守卫「后台装载只在目标可见会话仍为空时才安装」判定 B 已非空
        ⇒ 写者② 被**静默丢弃**，基线不安装
        ⇒ 症状：新消息在前、被恢复的历史挂在后面（repro 实测 conversation[2]）
```

**争用面小结（现状）**

| 争用对象 | 写者 | 现行同步手段 | 结果 |
|---|---|---|---|
| B 的可见会话 | 抢跑回合 / 恢复基线 | 仅「仍为空才安装」守卫 | 守卫失效 → 静默丢基线 |
| 提交是否合法 | 前端渲染 vs 后端状态 | 仅前端 `prompt.disabled` | TOCTOU：窗口内提交被接受 |
| 全局 composer | 所有会话共用一个 | 无（锁是全局的） | 切换期把输入区整块串行 |

## 4. 修复后数据流：缩争用（单一判定点前移 + 写者串行化）

```text
用户：A 运行中，切到冷会话 B
────────────────────────────────────────────────────────────────────────────────
[前端] 点击 B 行
   ├─ state.resumingSessionID = B
   ├─ chatView.renderControls(latest, switching=true)   ← 置位即显式重渲
   │     prompt.disabled = true / send.disabled = true
   │     stop-button、history-bar 不受影响（不越界）
   └─ invoke("ResumeSession", B) ─────────────────────────────┐
                                                              ▼
[后端] beginAsyncRestore(B)：Snapshot={B, restoring}；go resumeSessionCold(B) ⏳
   │
   ├─(A) 交互路径 Submit：submitConversation
   │       Core.ViewMu 临界区内 isRestoringLocked(B) == true
   │         ⇒ deferSubmitUntilRestored(ctx, B, input); return nil
   │             挂到 restoreSig 等装载完成（不丢输入、不报错、不阻塞）
   │       （startChatFor 内再兜一次 ErrSessionRestoring：关掉释放锁→启动之间
   │        那一段 TOCTOU；正常路径不会走到）
   │
   ├─(B) 显式后台路径 SubmitToSession(B)
   │       ActivateSession 返回 → isRestoring(B) == true
   │         ⇒ deferSubmitUntilRestored(ctx, B)
   │              挂到 restoreSig；立即 return nil（非阻塞受理）
   │
   └─(C) 装载完成 resumeSessionCold(B) → clearRestoringLocked(B)
          ├─ signalRestoreLocked() 广播
          ├─ B 的可见会话**仍为空** ⇒ 恢复基线正常安装（守卫成立）
          └─ 唤醒 (B) 的挂起提交 → submitConversationFor(B) → 此刻才开回合
   │
[前端] refresh → 快照 {B, idle/running} → renderControls(latest)
        switching 已清 ∧ restoring 已清 ⇒ 解锁（单调 OR：任一未清则继续锁）
```

**争用面小结（修复后）**

| 争用对象 | 写者 | 修复后的同步手段 | 结果 |
|---|---|---|---|
| B 的可见会话 | 恢复基线（唯一写者） | 抢跑路径已被门挡住 | 守卫恒成立，基线必装 |
| 提交是否合法 | 后端单点判定 | `isRestoringLocked` 与 restoring 写入同持 `Core.ViewMu` | 无 TOCTOU |
| 全局 composer | 只锁输入 | 按目标会话的切换/恢复态判定；不锁 stop/history | 输入区锁语义收敛，不越界 |

**缩争用要点**

1. **写者数 2 → 1**：B 的可见会话在恢复期间只由恢复基线写；抢跑回合被拒/延后，
   守卫的前提（仍为空）不再可能被破坏。
2. **判定点前移进临界区**：`isRestoringLocked` 与 `set/clearRestoringLocked` 同持
   `Core.ViewMu`，判定与状态变更不可交错 ⇒ 原来「前端渲染滞后一个快照」的 TOCTOU 消失。
3. **统一延后、按目标会话分派**：交互路径与显式后台路径都走同一条恢复门——命中即
   `deferSubmitUntilRestored`（挂到 `restoreSig`，调用方立即拿到 `nil`）。输入不丢、
   不阻塞；`ErrSessionRestoring` 退居 `startChatFor` 的纯兜底。因此不再存在「在
   restoring 空壳上开回合」的路径。前端「正在切换会话…」的禁用只是**呈现**，不是判据。
4. **锁的射程收敛**：`switching` 只锁输入，`stop-button`/`history-bar` 仍按真实运行态
   与历史窗口渲染 ⇒ 切换窗口里当前会话的取消与回看不被连带锁死。

## 5. 验证（红 → 绿）

后端（`application/core`）——本仓库口径：先证明用例能红，再证明修后为绿。红由
**临时短路恢复门**（`service_input.go` ×2、`chat.go` ×1 的 `isRestoringLocked` 判定），
复现后原样回滚（回滚后 `git diff --stat` 与改动前逐文件一致）：

```text
# RED（短路三处恢复门）：
$ go test ./application/core/ -run 'TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder|TestSubmitToSessionDefersUntilRestoreCompletes' -v
    session_submit_restoring_test.go:136: 顺序被颠倒（新消息在前、历史在后）：baseline@2 typed@0
        [… "typed while restoring" … "cold b content" …]     ← repro §3 的症状原样重现
    session_submit_restoring_test.go:190: B streamCalls = 1 while restoring, want 0（提交须挂到装载完成点）
    --- FAIL: TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder
    --- FAIL: TestSubmitToSessionDefersUntilRestoreCompletes

# GREEN（恢复门生效）：
$ go test ./application/core/ -run 'TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder|TestSubmitToSessionDefersUntilRestoreCompletes' -v
    --- PASS: TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder (0.04s)
    --- PASS: TestSubmitToSessionDefersUntilRestoreCompletes (0.32s)
```

前端（`gui/frontend/dist`）：

```text
# GREEN：
$ node --test gui/frontend/dist/chat-view-input-lock.test.mjs
    7 pass / 0 fail
    （restoring 整块只读壳 / switching 只锁输入 / 单调 OR 不提前解锁 /
      app.js 每个渲染落点都带 switching 且清位在 finally / render 不闪空态）

# RED（把 app.js 整份渲染的 switching 实参摘掉一处）：
    AssertionError: 整份渲染必须把 resumingSessionID 作为 switching 传入
    fail 1   ← 该断言不是空转
```

全量门禁（本次实测）：

```text
$ go build ./...                                     → ok
$ gofmt -l application/core/                         → 空
$ go test -race ./application/... ./gui/... -count=1 → all ok（20 包带测试）
$ node --check（dist 下全部 .js/.mjs）                → ok
$ node --test gui/frontend/dist/*.test.mjs           → 349 pass / 0 fail
$ go build -tags "gui,desktop,production" .          → ok（见 §7，GUI 二进制可构建）
```

回归用例：

- `application/core/session_submit_restoring_test.go`
  - `TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder`：A 运行中切冷 B、装载上门闩 →
    restoring 窗口内 `Submit` **受理（nil）**但 B 的 `streamCalls == 0`、A 不变；放行装载后
    被恢复的历史**在前**、延后提交**在后**（顺序不颠倒），且输入不丢失。
  - `TestSubmitToSessionDefersUntilRestoreCompletes`：`SubmitToSession` 立即受理（2s 内返回）、
    装载前 300ms 窗口内 `streamCalls == 0`、放行后自动启动。
- `gui/frontend/dist/chat-view-input-lock.test.mjs`（7 例）：`restoring` 锁整块 composer；
  `switching` 只锁输入（不隐藏 stop / history-bar）；两者同时成立时不提前解锁（单调 OR）；
  `render` 在 switching 窗口不闪「暂无消息」空态；`app.js` 每个 composer 渲染落点都带
  `switching`、清位在 `finally`。

## 6. 未纳入本次范围（已知）

- **同族粘滞路径**：`handleColdRestoreFailure` 的异步视图回滚已有修复与回归
  （`gui/bridge_view_drift_test.go`）；`forkSessionFromList` 同样会「部分切换视图」，
  但其 composer-status 文案未接到 `switching` flag（覆盖缺口，见 repro §5 建议 1 的射程）。
- **`resumeSessionCold` 基地安装守卫**的「合并 / 补插」改造未做——抢跑已被挡住后，该守卫
  不再被触发，属可选的健壮性增强。

## 7. GUI 侧验证（构建 + 实机走查）

修的是前端呈现与后端判据，两者都要在真机上看过一遍：

```text
# 1) 构建（含 embedded 前端资源；GUI 构建标签）：
$ go build -tags "gui,desktop,production" -ldflags "-s -w \
    -X .../internal/buildinfo.Version=verify -X .../internal/buildinfo.DefaultFrontend=gui" \
    -o <tmp>/seelex-gui-verify.exe .
    → exit 0（30.5 MB）

# 2) 实机走查（computer use）：把该二进制放到**独立数据根**（复制一份 config/ + plugins/
#    + .seelex/，清掉 .lock / lock.owner）后启动，避免与正在运行的开发实例抢数据根锁。
```

实机观察（截图存档于会话）：

| 步骤 | 观察 | 结论 |
|---|---|---|
| 启动新构建 | 侧栏列出 3 个 workspace / 17 个会话，顶栏显示本构建版本号 `verify` | 新前端资源已打进二进制，界面正常 |
| 点击一个**冷**会话（330k tokens） | 该行瞬时变为「恢复中…」，随后恢复为会话名并渲染出完整历史 | `resumeSessionFromList` 的置位 → 后端异步冷加载 → 装载完成这条链在真机跑通 |
| 恢复完成后 | composer 回到「就绪」态、placeholder 为待发文案、发送键可用 | 解锁路径正常（锁不会粘住） |

**边界（未取到的证据）**：本机这次冷装载是**亚秒级**完成，而 computer-use 一次
「点击 → 截图」往返本身就要 1~2 s——**锁的窗口比观测通道的采样间隔还短**，因此没有
截到 composer 处于「正在切换会话… / 正在恢复会话…」禁用态的那一帧。该断言的证据
落在确定性的用例上（`chat-view-input-lock.test.mjs` 7 例 + `app.js` 渲染落点/清位断言），
实机只对「新构建能跑、切换链路通、锁能解除」负责。

