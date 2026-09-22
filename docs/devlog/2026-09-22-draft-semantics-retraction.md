# 撤回：未发送输入草稿（composer draft）不是 draft

> 日期: 2026-09-22 | 范围: `application/core`、`sessionstore`、`internal/adapters`、`gui/frontend/dist`
> 触发: 用户验收 —— "draft 需要做的是一个未完成的会话……而不是我输入框未输入完的内容"

## 1. 为什么要撤回

此前把 draft 实现成**输入框里未发送的正文**：

- `BeginNewSession` 早分配一个 `draft_<nanos>_<seq>` 会话 ID；
- 用户一打字，`SaveComposerDraft` 就把这份正文写到会话目录的 lifecycle
  草稿通道（`input/draft.json`）并登记一个 record（`Status=draft`）；
- 目录枚举的 `Status=draft` 判据是**草稿文件的存在性**，于是会话树/会话侧栏在
  用户输入时就多出一行"草稿会话"；
- 重启时 `restorePersistedDraft` 把最近一份非空草稿重新装回视图。

这与产品的 draft 语义相反。产品的 draft 指的是**未完成的会话**：一个已经发出的
任务执行到一半，因为关机 / API 欠费中断，重启或充值后应当能**续跑原来的会话**。
输入框里没写完的内容不是 draft，也不该成为一个会话身份。

副作用（用户同时报告的 subagent 判定问题）也来自这套"早分配草稿 SID"：以
`draft_` 开头的会话 ID 与子代理记录的落盘键（`(projectID, mainSessionID)`）绑在
一起，一旦 ID 在物化前后不一致，重启恢复就认不出这些子代理。

## 2. 本次撤回了什么

| 层 | 撤回内容 |
|---|---|
| core | `composer_draft.go` 整体（`SaveComposerDraft` / `persistComposerDraft*` / `clearComposerDraft` / `persistedDraftCandidate` / `draftTextFor` / `restorePersistedDraft` / `scheduleShellDraftRestore` / `claimShellView` / `shellViewUntouched` / `engineSessionCarriesContent` / `registerDraftSlotLocked`）；装配器里的草稿恢复与后台补装调用点；物化路径的 `clearComposerDraft` 调用 |
| sessionstore | `SessionGranularStore.SaveComposerDraft` / `LoadComposerDraft`、`Router.SaveComposerDraftWorkspace` / `ComposerDraftWorkspace`、`storeEngine.setComposerDraft` / `lifecycleDraft` |
| adapters | `SessionPort.SaveComposerDraftWorkspace` / `LoadComposerDraftWorkspace`、`EnsureSessionIndexed`（仅草稿索引使用） |
| gui | `Bridge.SaveComposerDraft` 及其接口声明、`draft-lifecycle.js` / `draft-lifecycle.test.mjs`、`app.js` 的草稿生命周期接线（`composerDraft` 状态 / `syncComposerDraftState` / `scheduleComposerSave` / 侧栏草稿行）；`restoreComposerDraft` 保留但恒为 no-op（后端不再回填正文）|
| 文档/测试 | 4 份 composer-draft devlog、`composer_draft*_test.go`、`composer_draft_live_smoke_test.go`、`sessionstore/composer_draft_channel_test.go`、`session_running_idle_submit_test.go` |

保留：
- `lifecycle` 的 **queue / draft 迁移**机制（队列语义，与 composer 无关）；
- `SessionRecord.Composer` / `SessionState.Composer` / `model.ComposerDraft`——
  仅作落盘兼容的惰性字段，当前恒为空；
- 会话列表里**排队消息**的 `queued-message … is-draft` 渲染与 Agent Team 的
  **角色未同步草稿**（`draft_rows`）——两者是另外的语义，不在撤回范围。

## 3. 现在的行为

- 输入框输入**不会**产生任何会话行、不会落盘、也不会在重启后回填；
- `composerDraftPageText()` 恒返回空串（保留函数只为调用点与渲染签名稳定）；
- 无 ID 的会话行直接不渲染。

## 4. 待办（下一步）

1. **draft = 未完成会话**：定义"已发出但中断"的判据（本轮未收尾 / `status` ∈
   `{running, queued, restoring}` 且进程重启时无结论），并据此给会话打 draft
   状态、支持续跑；输入框内容与之无关。
2. **subagent 判定**：`(projectID, mainSessionID)` 落盘键在"草稿 ID → 物化 ID"
   漂移时会把子代理记录留在旧键下。需要 mock 全链路复现（打印草稿 ID / 物化 ID
   / 落盘键 / 恢复键对撞），再定修复（稳定 ID 或键别名迁移）。

## 5. 验证

- `go build ./...`、`go vet ./...` 干净；
- `go test ./application/... ./sessionstore/... ./gui/... ./internal/...` 全绿；
- `node --test gui/frontend/dist/*.test.mjs` 425/425 通过（含新增的撤回回归断言：
  `app.js` 不再引用 `draft-lifecycle.js`，且 `composerDraftPageText` 恒空）。
