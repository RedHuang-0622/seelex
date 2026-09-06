# 切换失败后“输入发不出去/发错会话”：链路复现与修复（跟进）

日期：2026-09-06
性质：接续「热会话切换时而灵时不灵」的 follow-up bugfix
关联：[code-changes.md](code-changes.md)（上一处：跨会话迟到事件污染订阅水位）

## 1. 现象

在切换失败（ResumeSession 返回错误）之后想继续在输入框输入并修正当前会话时，
输入内容“发送不出去 / 发到了看不到的会话”：用户在界面上仍看到会话 A（旧
视图），实际后端可能已经把视图切到目标 B（部分切换），输入被路由进 B；或输入
后没有任何可见反应，表现为多个操作后输入框内容出不去。

## 2. 链路（本处缺陷所在段）

```
Bridge.ResumeSession(B) ──> core.resumeSession(B)          gui/bridge.go / session_history.go
  ├─ 同步冷加载（无运行中会话）：resumeSessionCold(B)
  │    ① 装载历史/引擎 B（内存副作用）
  │    ② ViewMu 段：视图指针切到 B + 发布基线
  │    ③ 迟到失败点：AttachSessionContext(B) 出错 ── 发生在 ② 之后
  │       → resumeSession 返回错误，但视图已 = B
  ├─（异步冷加载路径已含回滚 handleColdRestoreFailure，无此问题）
  ▼
前端 session-button 点击：catch → 只提示“恢复会话失败”，不刷新权威快照
  → 界面仍显示 A、输入区可输入；用户输入修正
  ▼
backend 当前视图 = B → Submit 路由进 B（用户看不到/不是要修正的会话）
```

前端错误路径只 `renderSessions(...)` 用旧快照重画列表，不拉权威快照；后端
同步冷加载的“迟到失败”又没有回滚——两边都不收敛 → 输入路由到用户看不到的
会话。

## 3. 根因（已复现确认）

1. **core 不变量缺失**：`resumeSession` 同步冷加载失败时不回滚视图。
   `resumeSessionCold` 的失败点 `AttachSessionContext` 位于视图激活（②）之后；
   返回错误时视图已经切到 B。后台冷加载路径（handleColdRestoreFailure）有
   回滚，同步路径没有——两条失败路径语义不一致。
2. **前端错误路径不收敛**：`ResumeSession`/`ForkSessionLatest` 失败后只重画
   列表（基于旧 client.current()），不拉权威快照；若后端已部分切换，界面与
   后端视图指针永久不一致。

## 4. 复现（RED → GREEN）

`application/core/session_switch_failed_resume_test.go`
`TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt`：

- 会话 A 驻留且空闲（视图 = A）；目标 B 冷且其 `AttachSessionContext` 失败
  （模拟迟到失败）；
- 步骤 2 断言 **ResumeSession(B) 返回错误后视图仍 = A**（修复前 RED：视图
  变成 B，“输入会路由进 B 而用户看着 A”）；
- 修复后补充：随后在 A 提交“correction for A”必须路由到 A 并完成一轮对话。

修复前失败输出：`RED REPRO: after failed ResumeSession(B) view = "sess-b",
want still "<aID>"`。

## 5. 修复

1. `application/core/session_history.go`：同步冷加载失败时调用新增
   `rollbackSyncResumeFailure(sessionID, previous)`：
   - 视图已被目标激活（迟到失败）→ 目标 previous 驻留则 `hotAttachSession`
     回退，否则 `resetViewToDraftAfterRestoreFailure`（与后台失败路径同语义）；
   - 视图尚未切换（早失败）→ previous 驻留时热挂载一次，把可能已被目标
     workspace 占用的全局项目根/写作用域切回 previous。
   由此建立不变量：**ResumeSession 返回错误 ⇒ 视图停留在切换前会话**。
2. `gui/frontend/dist/app.js`：ResumeSession / ForkSessionLatest 失败 catch 后
   立即 `await refresh({ scroll: "preserve" })` 拉一次权威快照收敛，使会话/
   聊天/输入区状态与后端一致（快照重拉失败不影响 toast 提示），避免后续输入
   路由到用户看不到的会话。

## 6. 回归沉淀与验证

- 回归测试：`application/core/session_switch_failed_resume_test.go`（新增，
  先红后绿）。
- 验证：
  - `go test ./application/core -count=1` → ok（含新回归，先红后绿确认）；
  - `go test -race ./application/core ./gui -count=1` → ok；
  - `go build ./...` → ok；`gofmt` 干净；`node --check gui/frontend/dist/app.js` → ok。

## 7. 改动文件

- `application/core/session_history.go`（同步冷加载失败回滚 + 不变量注释）
- `application/core/session_switch_failed_resume_test.go`（新增回归）
- `gui/frontend/dist/app.js`（切换失败后权威快照收敛）
