# 运行中会话影响空闲会话的输入框提交：两条机制的复现与修复（2026-09-20）

> 日期: 2026-09-20 | 范围: `application/core`（草稿物化门、引擎读写路由）、
> `CHANGELOG.md`、`application/core/README-session.md`
> 回归: `application/core/session_running_idle_submit_test.go`（新，3 例）

## 1. 现象与来路

2026-09-17 的报告原文是「在一个会话运行中，从另外一个不在运行中的会话发送消息，
会因为一些问题发不出去」。那一轮只治了 `restoring` 抢跑（见
[2026-09-17-submit-during-cold-restore-fix.md](2026-09-17-submit-during-cold-restore-fix.md)），
复现文 §4 明确留了一句「**普通路径不丢字，报告里说的『有时候』不是每次都发生**」——
即间歇性没有解释完。本轮把剩下的两条机制各自做成确定性复现并修掉。

触发前提是上一批（未提交）的前端改动：普通输入不再走 ambient `Submit`，而是一律
`SubmitToSession(当前视图会话 ID, text)`（`gui/frontend/dist/composer-input.js`
`composerSubmitPlan`，目的正是让运行中会话不再"吸"走别人的输入）。**后端不知道这条
路由变化**：草稿的早分配 SID 也会被显式带进来。

## 2. 复现（确定性，RED 实测）

夹具用会话路由引擎（`multiSessionEngine` / `aliasBusyEngine`）+ 支持 record 读写的
会话端口，即生产形状；三例都在 `application/core/session_running_idle_submit_test.go`。

### 2.1 草稿被当成"未加载的冷会话"回读

```text
A 运行中 → BeginNewSession（视图=早分配草稿）→ SaveComposerDraft("新会话第一条")
→ SubmitToSession(draftID, "新会话第一条")

session_running_idle_submit_test.go:123: 草稿被当成冷会话回读
  （conv[0]="已恢复会话: draft_1789839778959514300_2"）；首条提交必须走物化：
  [{Role:system Content:已恢复会话: draft_…} {Role:user Content:新会话第一条} {Role:assistant Content:}]
--- FAIL: TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns (0.12s)
```

对照组（无会话运行中）同一个判据也红，且红在同一处：

```text
session_running_idle_submit_test.go:176: 草稿被当成冷会话回读
  （conv[0]="已恢复会话: draft_1789839829854542200_2"）
--- FAIL: TestSubmitToSessionMaterializesIdleDraft (0.04s)
```

链路（文件:行）：`session_scope.go` `SubmitToSession` → `!sessionLoaded(draftID)`
（草稿阶段刻意不建引擎 bundle）→ `ActivateSession` → `session_history.go`
`resumeSession` → 未驻留分支：无会话运行 → `resumeSessionCold(sid, 0)`；有会话运行 →
`beginAsyncRestore` + 后台 `resumeSessionCold`。而 `resumeSessionCold` 里
`hasRecord=true`（那条 record 是 `SaveComposerDraft` 写的 `Status=draft`），于是
`:459` 的恢复标记行被装进一个**新会话**，`materializeDraftSession` 整条路径
（按 SID 建束、绑项目、起标题、`clearComposerDraft`）一步没走。

`restoring` 分支只是它的一个变体：提交被 `deferSubmitUntilRestored` 挂到装载完成点，
用户看到的是"回车没反应"，之后仍然带着恢复标记起回合。**运行中会话在这里的作用是把
症状从"内容不对"升级成"看起来没发出去"**——不是根因，根因是草稿没有门。

后果清单（都实测/可推导）：

| 面 | 后果 |
|---|---|
| 新会话正文 | 首行是「已恢复会话: draft_…」系统行 |
| 草稿槽位 | `service.draft` 不消费 → 下次 `/new` 复用同一条草稿身份 |
| 持久化 | 草稿 record 保留已发送正文与 `Status=draft` → **重启后已发送的话又回到输入框** |
| 视图 | 经 `restoring` 空壳，提交延后（A 运行中时） |

### 2.2 新建会话排在运行中会话的引擎别名后面

```text
A 运行中（后台）；视图已热挂载到空闲会话 B；点"新建会话"

session_running_idle_submit_test.go:287: BeginNewSession 排在运行中会话的引擎别名后面
  （History 调用 2 次 / ClearHistory 0 次）；空闲会话的输入准备被运行中会话挡住
--- FAIL: TestBeginNewSessionDoesNotSerializeBehindRunningSession (0.50s)
```

链路：`session_draft.go` `BeginNewSession` 用 `Engine.History()` 判定"当前会话有没有
历史要先落盘"、用 `Engine.ClearHistory()` 清空"离开的那个会话"。这两个都是
**进程级活跃别名**（`internal/adapters/engine_port.go:675` 起一系列方法读
`port.engine`），别名指向最后被激活/建束的那个会话——这里仍是后台运行中的 A。
Seele `session.ChatStream` 从进函数持 `Session.mu` 到出函数（v0.3.0 `session/chat.go:273`），
而 `History()`（`:155`）/`ClearHistory()`（`:207`）取同一把锁。

`aliasBusyEngine` 就是照这份锁语义写的测试替身（别名那一会话在 ChatStream 中时，
`History()`/`ClearHistory()` 必须排队），所以红的不是"慢"，而是"结构上排在后面"。

放大效应：`BeginNewSession` 全程持有 `transitionView()`；生产宿主
`seelebridge/runtime.go:716` `PerSessionExecution() == false` ⇒
`transitionForSession` 一律回退视图 key（`session_scope.go:37`），那是**进程唯一**的
过渡锁——所有会话的 `resumeSession`/`UnloadSession` 与 ambient `Submit` 都在它后面排队。
于是一次"新建会话"点击能冻住整个界面到 A 那一轮结束；解除阻塞后被清空的还是 A 的
工作历史。这正是"有时候发不出去"的另一种形状：**不是丢字，是排队**。

## 3. 修法

| # | 位置 | 改动 |
|---|---|---|
| 1 | `application/core/session_draft.go` | 新增 `materializeDraftForSubmit`（廉价预判 `isUnmaterializedDraftTarget` → 视图过渡锁内复判归属 → `materializeDraftSession`）。归属不符返回新增的 `ErrDraftNotInView`（`service.go`）：草稿槽是进程单例，物化会切共享视图镜像，拿着过期快照提交时明确失败比投给别的会话安全 |
| 2 | `application/core/session_scope.go` | `SubmitToSession` 在"目标未加载 → ActivateSession"判定**之前**过这道草稿门；物化后 bundle 已在，后面的加载判定自然为真，不再出现草稿走冷加载 |
| 3 | `application/core/session_draft.go` | `BeginNewSession` 的引擎读写改为按会话路由：`engineHistoryFor(sessionID)` / `clearEngineHistoryFor(sessionID)` |
| 4 | `application/core/service_input.go` | 在 `engineHistoryFor` 同族补 `clearEngineHistoryFor`（路由宿主 `ClearHistoryFor`，非路由宿主退化），doc 注释点明为什么禁止用别名版本 |

不改的部分（刻意）：前端 `composerSubmitPlan` 的显式路由是正确方向，本轮**不**回退成
ambient `Submit`；`materializeDraftSession` 仍只有一份实现，两条提交路径共用。

## 4. 验证（红 → 绿）

```text
# GREEN（三例 + 上一轮 restoring 两例）
$ go test ./application/core -run 'TestSubmitToSessionMaterializes|TestBeginNewSessionDoesNotSerializeBehindRunningSession|TestSubmitDuringColdRestore|TestSubmitToSessionDefersUntilRestoreCompletes' -count=1 -v
    --- PASS: TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns (0.04s)
    --- PASS: TestSubmitToSessionMaterializesIdleDraft (0.01s)
    --- PASS: TestBeginNewSessionDoesNotSerializeBehindRunningSession (0.00s)

# 全量（本机 CGO_ENABLED=1，带 -race）
$ go test -race ./application/... ./gui/... ./session/... ./internal/... -count=1 -timeout=900s
    → exit 0，24 包 ok，无 FAIL
$ go vet ./...                                        → ok
$ go build ./...                                      → ok
$ go build -tags "gui,desktop,production" ./...       → ok
$ gofmt -l application/core/                          → 空
$ python scripts/gen_core_readme_index.py             → 刷新 README-service/session
```

## 5. 未做 / 风险

- **草稿物化仍要求视图停在草稿上**（`ErrDraftNotInView`）。多页签各持 composer 的
  目标形态落地时，草稿槽位要随之一同分布化，本判断据届时重写。
- **`PerSessionExecution() == false` 没动**（`seelebridge/runtime.go:709` 注释：worktree
  与 PathGuard 仍读进程级 project root）。本轮只是把 `BeginNewSession` 移出别名路径；
  视图 key 仍是生产唯一的过渡 key，跨会话并行的生命周期串行度不变。
- `service_interaction.go:189,198` 仍在用别名 `Engine.ClearHistory()`（`/clear` 一类
  视图命令）。同一族风险，但不在"输入框提交"这条链上，本轮未动。
- **实机未取证**：与 2026-09-17 同样受限于 webview 采样间隔（冷加载亚秒级、别名阻塞
  只在长回合里可见）。证据全部落在确定性用例上；`aliasBusyEngine` 是按 Seele v0.3.0
  真实锁纪律写的替身，不是自证式桩。
