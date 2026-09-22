# Agent Team 角色未同步草稿：独立成区 + 生命周期口径（2026-09-22）

> 日期: 2026-09-22 | 范围: `gui/frontend/dist/agent-team-view.js`、
> `gui/frontend/dist/agent-team-view.test.mjs`（新增 4 例 + 1 例既有断言收紧）、
> `sessionstore/role_session_test.go`（新增 1 例）、`application/core/goal_team_recorder_test.go`（新，4 例）
> 后端：**零行为改动**（只新增回归测试；结论与证据见 §4）
> 承接: [2026-09-16-team-work-record-dataflow](../2026-09-16-team-work-record-dataflow/README.md)（team work 记录 → 传输 → 渲染链路）

## 1. 现象与用户口径

角色会话详情（员工行「查看」→ `Bridge.AgentTeamRoleSnapshot` → `renderRoleSessionDetail`）
把 `snapshot.draft_rows` 直接并进 `role_rows` 当普通行渲染：**用户看不出哪一行还没同步**，
也看不到"这个角色现在有几条草稿"。用户要的 context 是「既定的 message + 各个 draft 下的内容」：
已发布行与未同步草稿各自可见、来源清楚。

生命周期口径（本轮不新造，只是照着后端已有语义呈现）：

| 时点 | 权威动作 | 前端看到什么 |
| --- | --- | --- |
| 本轮进行中／上一次同步失败 | 行只在 `draft/<role>.jsonl`（sequencer 的 WAL） | `draft_rows` 非空 → 「未同步草稿」区 |
| 本轮完成（裁决已出） | `RecordTLRound` append 后立即 `SyncRoleDraft`：原子发布 head+floor 再删 draft 文件 | `draft_rows` 变空，同样的行成为 message 行 |
| 本轮取消（评估阶段被取消） | 走不到记录器 → 什么都不写 | 无草稿、无悬空行 |

前端只渲染后端权威投影，**不推演**"同步后应该长什么样"。

## 2. 复现（先红，再改）

最小复现用真实数据形状（`application/core/goal_team_recorder.go:RecordTLRound`
一次 `AppendRoleDraft` 落 `role_context` + `tl_directive` 两行；seq 由 sequencer 在 sync 时分配，
所以草稿行的 `event.seq` 恒为 0），写在
`gui/frontend/dist/agent-team-view.test.mjs`：

```bash
node --test gui/frontend/dist/agent-team-view.test.mjs
# 修复前：✖ unsynced role drafts get their own region …
#         ✖ role record table keeps draft cells but marks them is-draft, never as seq 0
#         ✖ draft content, kind and identity are escaped like any other row
#            ℹ tests 29 / pass 26 / fail 3
```

修复前 `renderRoleSessionDetail` 的实际输出片段（红证据）：

```html
<thead><tr class="excel-head-row"><th class="role-record-lane-head">车道</th><th>0</th><th>8</th></tr></thead>
<tr class="role-record-lane is-own" data-lane="tl">
  <td class="role-record-cell is-own" data-seq="0" title="ADVISOR 自己的回合">本轮裁决原文</td>
  <td class="role-record-cell is-own" data-seq="8" title="ADVISOR 自己的回合">已发布的裁决</td></tr>
<article class="role-session-row">… seq 0 … 本轮送给 ADVISOR 的原文</article>
<article class="role-session-row">… seq 0 … 本轮裁决原文</article>
```

四条症状一次读全：

1. 列头凭空多出 `0` —— 草稿没有 seq，却按 `seq=0` 冒充一个回合；
2. `ownBySeq` 是 `Map` 以 seq 为键，**同回合多行草稿互相覆盖**：`role_context`
   那一行在表格里被 `tl_directive` 吞掉（上面只剩「本轮裁决原文」）；
3. 草稿行与已发布行 `class` 完全相同（`role-session-row`），看不出草稿身份；
4. 没有条数徽标／待同步文案；`data-record-own` 还把草稿算进"自己的行"（`3`）。

## 3. 修复（只动前端渲染）

`gui/frontend/dist/agent-team-view.js`：

- `normalizeRoleDrafts`（新，L876）+ `draftIDOf`（L865）：草稿行的身份是
  `round_id / unit_seq / kind`（外层 sequencer 凭据优先、内层 `event` 兜底），
  **不再借 seq**；`normalizeRoleRows` 拆出 `normalizeRoleRow`（L840）/ `isRenderableRoleRow`（L854）
  以复用同一套字段归一化与过滤口径。
- `renderRoleSessionDetail`（L912）：已发布行走 `renderPublishedRoleRow`（L942）保持原样，
  草稿走新的 `renderRoleDraftBlock`（L958）——独立成区：`data-role-drafts` 条数、
  `section-title`+`badge` 计数、每行 `class="role-session-row is-draft"`
  （`data-draft-index / -kind / -round / -unit`）、`chip is-draft` 的「待同步」文案、
  以及"本轮还没结束…同步成功后才出现在上面的 message 行中"的说明。整块无草稿时不渲染。
- `renderRoleRecordTable`（L1001）：`ownRows = roleRows`（已发布）、`draftRows` 单列；
  草稿排在右端「草稿N」列（`role-record-draft-head`，title 说明"同步后才分配 seq"），
  main 车道在这些列是 `is-empty` 占位，自身车道是
  `class="role-record-cell is-own is-draft" data-draft=N`（title「未同步草稿：本轮结束
  （完成或取消）同步后才写进 message」）；`data-record-own` 只数已发布行，另加
  `data-record-draft` 数草稿，legend 补一句口径。全部文本经 `escapeHtml`
  （内容、kind、身份串、tool 名/参数都逃逸）。

## 4. 后端结论：不需要改（只补回归测试）

触发点与取消路径逐条核对：

- **本轮完成即同步**：`application/core/goal_team_recorder.go:59-62`（`RecordTLRound`
  append 后立刻 `SyncRoleDraft`）、`:91-94`（`RecordMainTurn`）、`:134-137`
  （`ArchiveTLHistory`，逃生收口归档）。生产侧**只有这三处**写 role draft，
  没有"只 append 不 sync"的生产者。
- **取消不丢内容、也不留悬空草稿**：`application/core/goal/techleader.go:409` 的
  `runRoundLocked` 里，记录器在第 501-513 行、**在 `Evaluate` 成功之后**才调用；
  评估阶段被取消 → `Evaluate` 报错直接返回 → 记录器不执行 → 什么都没写。
  已出裁决的回合即使 ctx 取消也照落照同步（`goal_team_recorder.go` 的
  `RecordTLRound(_ context.Context, …)` 刻意忽略 ctx）——两种情况都不会出现
  "取消丢内容"或"取消后草稿挂着"。
- **存储侧收敛唯一入口**：`sessionstore/role_session.go:371` 的 `syncRoleDraft`：
  读 draft → `sortRoleDraftRows` → 幂等判定（`head.LastCommitID == commitID` 时
  只删 draft、不重复 append，L386-390）→ `messageCommitSync` 原子发布 head+floor
  （L399）→ 删除 draft 文件（L403）。**`deleteRoleDraft` 只在这里被调用**
  （定义 L304，调用 L388/L403）：草稿不可能在没发布的情况下被丢掉。
- **投影是权威**：`sessionstore/role_session.go:419` 的 `readRoleSnapshot` 把
  `draft_rows`（L444-447）与 `main_rows/role_rows` 分列返回，经
  `internal/adapters/session_workspace_ports.go:747` 映射成 DTO；
  既有 live 探针 `gui/team_workcontent_live_probe_test.go:272-288` 就是按"行可能落在
  main 文档（sync 之后）或 draft（未 sync）"读的——这正是前端分两区渲染的依据。

回归测试（正式、可复跑）：

- `sessionstore/role_session_test.go:TestRoleSnapshotProjectsPendingDraftUntilSync`（新）：
  同步前 `draft_rows=2 / role_rows=0 / main_rows=1` 且草稿行 `event.seq==0`、
  带 round/role/unit 凭据；sync 后 `draft_rows` 清空、同样的两行带 seq 进 message；
  半同步窗口重放 → `AlreadySynced`、不重复 append、快照无草稿。
- `application/core/goal_team_recorder_test.go`（新，4 例）：`RecordTLRound` /
  `RecordMainTurn` 一次 append 紧跟一次 sync（顺序与排序键=会话顺序）、unit_seq 递增、
  草稿不带 seq、结束后无 pending；ctx 已取消仍落盘；未装配 team 的会话不写 draft。

## 5. 验证

```bash
node --test gui/frontend/dist/agent-team-view.test.mjs      # 29 pass / 0 fail（4 例新增 + 1 例收紧）
node --test gui/frontend/dist/*.test.mjs                    # 407 pass / 0 fail
go test ./application/core/... ./sessionstore/... -count=1   # 全 ok（core 15.2s / sessionstore 59.9s）
gofmt -l application/core sessionstore                      # 空
```

## 6. 遗留与口径提醒（不在本轮改动范围）

1. **已发布角色行仍在 main 车道**：`readRoleSnapshot`
   （`sessionstore/role_session.go:419`）的 `role_rows` 来自角色会话自己的备份文档，
   生产侧只有 headless 探针 `AppendRoleSessionRows`（`gui/headless.go:421`）会写它；
   tl 的行 sync 后落进**主会话** message（`main_rows`，带 `role_name=tl`），
   所以在记录表里显示为 main 车道的 `is-main` 格、在自身车道显示为 `is-shared`。
   这与 `docs/2026-09-16-team-work-record-dataflow/README.md` §5 的车道模型一致，
   但与用户"来源清楚"的期待仍有距离（`main_rows[].role_name` 已经带得出作者）。
   若要修，建议独立一轮：按 `role_name/role_session_id` 把已发布角色行标成
   `is-own`（并同步改那份文档）——本轮刻意不动车道语义，避免与草稿呈现混在一个 diff。
2. **视觉区分未落样式**：本轮只加了 `is-draft` 类与 title 文案；
   `gui/frontend/dist/styles.css` 不在本轮白名单内，所以「未同步草稿」区目前与已发布行
   同色同框。建议由收敛样式的一方补 `.role-session-row.is-draft` /
   `.role-record-cell.is-draft` / `.role-record-draft-head` 的强调色。
3. **半同步窗口 + 跨角色交错的极端串扰**：若"发布成功但删 draft 失败"后又有别的角色
   sync（`head.LastCommitID` 变成别人的 commit），残留 draft 重放会被当成新草稿再
   append 一次（重复行）。当前只在一处注释里记录，未修：需要 storage 侧保留"最近 N 个
   commit"判定，属于另一个方向的改动。
