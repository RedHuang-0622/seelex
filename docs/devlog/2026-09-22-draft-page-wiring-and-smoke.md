# 草稿接线与草稿冒烟：页面 context 两半合流 + 三件事的实测（2026-09-22）

> 日期: 2026-09-22 | 范围: `gui/frontend/dist`（`app.js` / `components.js` /
> `chat-view.js` / `styles.css` 接线；草稿状态机在 `draft-lifecycle.js`，由并行方向的
> 提交提供）、`composer_draft_live_smoke_test.go`（新，`-tags draftsmoke`）、本日志
> 承接: [2026-09-22-composer-draft-lifecycle.md](2026-09-22-composer-draft-lifecycle.md)
> （草稿生命周期纯函数与后端清空收敛）、
> [2026-09-22-explorer-subpages-refresh.md](2026-09-22-explorer-subpages-refresh.md)、
> [2026-09-22-team-role-draft.md](2026-09-22-team-role-draft.md)

## 1. 为什么还要一轮接线

三条并行方向（资源管理器子页 / 会话页草稿 / Agent Team 角色草稿）都各自把规则落到
了可单测的纯函数上，但**壳层（`app.js` 等 DOM 绑定脚本）留给了主 Agent**：方向 C
的白名单明确不含 `app.js`，方向 A 只改了资源管理器区域，方向 B 的白名单不含
`styles.css`。于是：

- `draft-lifecycle.js` 存在但没有被 `app.js` 引用（页面看不到草稿行）；
- `agent-team-view.js` 只加了 `is-draft` 类与 title，没有样式（草稿区与已发布行同色）；
- `components.js` 没有 `refresh` 图标，资源管理器三个刷新按钮退化成 `recall` 图标。

## 2. 本轮接线（文件级合并 + 壳层收敛）

| 位置 | 改动 |
|---|---|
| `app.js:5` | 引入 `DRAFT_ROW_KIND / composerDraftRows / draftLifecycle / draftLifecycleFromSnapshot / draftRoundEvent` |
| `app.js:512-520` | 壳层草稿状态：`composerDraft` / `composerRoundRunning` / `composerCancelled`（规则不在这里，只接线） |
| `app.js:640` `syncComposerDraftState` | 唯一写入点：会话换了按权威快照重取归属；同一会话把"轮次收尾"翻成 `materialize`/`cancel`；最后用框内正文 `append`（幂等） |
| `app.js:660` `composerDraftPageText` | 页面草稿正文 = `composerDraftRows` 里那条 `draft` 行（空草稿不出行；判据不复制） |
| `app.js` `render` / `renderIncremental` / `runViewActivation` | 三条会话行渲染落点都带草稿正文；增量通道也同步草稿（轮次收尾常走增量） |
| `app.js` `markComposerEdited` / 提交 / 停止按钮 | `append`（含程序化写入/召回）/ `submit`（`remaining` 由 `clearSubmittedText` 算好）/ 记下"这次结束是取消" |
| `components.js` `renderConversationModel(messages, chat, draft)` | 末尾插一条 `key="chat:draft"`、`meta.kind="draft"` 的投影行（`is-draft` + `data-draft/unsent` + markdown + escape），`DRAFT_ROW_KEY` 导出给测试 |
| `chat-view.js` `renderConversation(..., draft)` | 有草稿时这一页不算空态（草稿会话此前显示成空页）；`render(snapshot, scrollMode, switching, draft)` 透传 |
| `styles.css` | `.draft-message`（会话页草稿行）+ `.role-session-row.is-draft` / `.role-record-draft-head` / `.role-record-cell.is-draft` / `.chip.is-draft`（Agent Team 草稿区，补方向 B 的白名单缺口） |
| `index.html` + `components.js` `ICONS.refresh` | 三个刷新按钮换成真正的 `refresh` 图标（不再借 `recall`） |
| `docs/gui/modules/right-sidebar.md` / `gui/frontend/README.md` | Agent Team 草稿区与资源管理器三子页的口径同步（方向 B / A 的白名单缺口） |
| `application/core/README-composer.md`、`README-goal.md` | 重跑 `python scripts/gen_core_readme_index.py`（方向 C / B 新增函数与测试进索引） |

顺序约束只有一条，且被源码级断言钉住：`render` 里 **先** `syncComposerSession`
（输入框正文按会话归属对齐）**再** `restoreComposerDraft`（草稿回填）**最后**
`syncComposerDraftState`（草稿状态读"对齐并回填之后"的框内正文）。

回归：`node --test gui/frontend/dist/*.test.mjs`（439 pass / 0 fail，含 3 例
`components.test.mjs` 草稿行、2 例 `chat-view-input-lock.test.mjs` 草稿落点断言）、
`go test ./gui/... ./application/core/... -count=1` 全绿。

## 3. 草稿冒烟（headless，逐条 wire 证据）

新增 `composer_draft_live_smoke_test.go`（`-tags draftsmoke`，默认 `go test` 不带）：

```
$ go test -tags draftsmoke . -run TestComposerDraftHeadlessSmoke -count=1 -v -timeout 10m
--- PASS 前缀匹配：未发送草稿不进 provider 请求，跨轮前缀不变量成立
--- PASS 历史的留存：重启后既有消息仍在，草稿归属可观测
--- PASS 中断会话恢复：取消在途一轮后历史不被吞、重启后仍可继续
--- PASS 草稿的留存（观测）：重启后未发送草稿还能不能回来
```

它只把 provider 换成本地记录型 mock（可"扣住"请求制造中断窗口），装配 / 会话单元 /
草稿槽 / rollout 落盘 / wire 组装都是生产路径。四件事的实测结论：

1. **前缀匹配（如约）**：草稿哨兵正文（`SaveComposerDraft` 的未发送内容）没有出现在
   任何一次 provider 请求里；从草稿发出去的第一轮物化后草稿槽清空、目录行不再标
   `draft`；相邻请求保持"前一次请求是后一次请求的完整前缀"（`req#2 n=3 user assistant user`）。
2. **历史的留存（如约）**：3 轮真实回合 → 关停 → 同一 store 再启动 → `ResumeSession`，
   6 条可见消息逐条相同（`compareWirePrefix` 空差异）。真实 API 版同样跑通：
   `-tags manualsmoke2` 的 `TestRealAPISessionRestoreSmoke` PASS。
3. **中断会话恢复（如约，有一处如实记录）**：扣住 provider → 请求在途时 `CancelChat` →
   会话回到 idle，**已定稿的 2 条消息不被改写**，被中断那一轮的 user 正文留在会话里
   （未被吞），重启后这 3 条仍在，继续提交第三轮拿到回答（3 → 5 条）。
   如实记录：中断会把取消原因写成可见回合错误
   （`chat.error = "…: context canceled"`），并留下一条空的 assistant 占位；重启恢复时
   这条空占位不被保留（空内容不构成历史），所以"逐条相同"的判据按"有正文的 user/assistant"
   取。
4. **草稿的跨重启留存（未如约，属已记录的候选缺口）**：同一 store 再启动后，
   **未发送草稿正文没有回来**——视图会话是另一条 `sess_…`（`draft=false`、
   `composer=""`），草稿会话连目录行都不出现（`存在=false`）。
   这与方向 C 记录的"引擎已带会话时（`initialDraft=false`）持久化草稿不被恢复"一致：
   它要的是"目录里的草稿行与视图会话可分离（多草稿并行）"的设计，本轮不动，
   冒烟把它记成观测而不是硬断言。

真实 API 前缀冒烟（既有 opt-in 用例）同样重跑：

```
$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
go test -tags manualsmoke . -run TestManualSmokeRealAccountPrefixInvariant -count=1 -v
--- PASS (9.84s)
report=tmp/prefix_smoke_report.txt requests=9 tool_round_messages=22 failed_tool_messages=17 violations=0 presented_leaks=0
```

报告里 9 次请求两两"全前缀一致"，真实 provider 计费侧缓存命中 92.5%–96.8%
（`prompt_cache_hit_tokens`），即前缀匹配与提示缓存前提都成立。

## 4. 本轮不做

- **草稿行的视觉复核**：本机没有可脚本化驱动的 GUI 实例（webview），页面草稿行与
  Agent Team 草稿区的"好不好看"只有纯函数用例 + 源码级落点断言，没有截图证据。
- **中断那轮的空 assistant 占位是否该进恢复投影**：属"历史投影口径"，要有明确口径
  才能定断言，本轮按"有正文才算历史"记录。
- **方向 B 的两条遗留**（已发布角色行仍在 main 车道、半同步窗口的极端重放）保持不动，
  见 `docs/devlog/2026-09-22-team-role-draft.md` §6。
