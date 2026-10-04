# teammate 会话查看：为什么"总是看到主代理的会话"，以及两处缺陷的现场

- 日期：2026-10-04
- 主题：修「teamwork 的 teammate 会话查看 = 主代理的会话」这条用户现场（上一轮做了
  `TeammateSessionLive` 之后**仍然**复现）
- 证据：两条红灯复现用例（会话读面 + wire 契约）+ headless 冒烟新增 P6b 探针（全绿）
- 相关锚点：`docs/devlog/2026-10-04-teamwork-scope-sweep-and-auto-return.md`（上一轮：三条链收口）、
  `application/contract/dto/teammate_session_live.go`、`gui/frontend/dist/team-board-view.js`

---

## 0. 结论先行（两个缺陷，**不是同一个**）

用户看到的是"主代理的会话"，但链子上有两处独立缺陷，缺一不可地让这句话成立：

| # | 缺陷 | 后果（用户可见） | 复现 |
|---|---|---|---|
| A | `dto.TeammateSessionLiveView` 的字段**没有 `json tag`** | 「这件事的会话」这条读面在 GUI 上**从来读不到**：Wails 把 Go 字段名出成 JSON 键（`Running`/`SessionID`/…），前端按 snake_case 读 → `view.running` 为 undefined → 永远走"执行面不在本进程"的分支 | `e2e/teammate_session_live_wire_test.go`（修前 6 个键全缺） |
| B | 没有"这件事自己的会话"时，看板 teammate 行**回退到员工的长期角色会话** | 打开的是**群聊车道读面**：它的 main 车道就是**主会话整段**，而 teammate 自己那条车道在存储里**从来没有行**（worker 回合是进程内执行面，不写存储）→ 用户看到的就是主代理的会话 | `sessionstore/role_snapshot_teammate_repro_test.go` + `gui/frontend/dist/team-board-view.test.mjs`（修前红灯） |

**为什么上一轮"冒烟全绿"却没修好**：冒烟直接调 `runtime.TeammateSessionLive`（读 Go 结构体），
前端 .mjs 用例喂的是 snake_case 夹具——**两端各自都绿，契约在中间断了**。这一次补的探针
（P6b）取的是**应用层那一面再序列化成 JSON**，正是 GUI 拿东西的那一跳。

---

## 1. 缺陷 B 的现场：teammate 的"会话读面"里只有主代理的行

`sessionstore/role_snapshot_teammate_repro_test.go` 钉住的事实（本机输出）：

```text
=== RUN   TestRoleSnapshotForTeammateCarriesMainRowsWithNoOwnRows
    role_snapshot_teammate_repro_test.go:48: 确认：teammate 的”会话读面“里，唯一的内容就是主代理的行（seq=2 role=assistant）
--- PASS
```

读法与结论：

- teammate 的角色会话号是 `(主会话, team_id, role)` 派生出来的，但**角色子树从未被创建**
  ——`seelebridge.runRoleRound` 造引擎时刻意不接 DurableHistory，也不调 `CreateRoleSessionWorkspace`；
- `RoleSnapshot(main, role, role_session_id)` 于是给出 `role_rows=[] / draft_rows=[]`，而
  `main_rows` = **主会话整段**（这条读面本来是为"群聊车道"设计的：main 行是**共享上下文**，
  对真有自己行的角色才有意义）；
- 前端 `renderRoleRecordTable` 把 `main_rows` 画进主车道、自身车道留"共享/占位"——
  于是一份**内容全是主代理**的表被当成了"这位 teammate 的会话"。

修法（前端，一条口径）：**入口只指向"这件事自己的会话"**。`renderTeamQueue` 不再把
`member.role_session_id` 当兜底目标；没有 `current_session_id` 时**不挂入口**（纯文本 +
`title` 说清为什么），而不是给一个内容错的入口。

两条入口共用**同一份判定**（`gui/frontend/dist/team-board-view.js` 的
`teammateSessionEntry`，纯函数、有用例）：

```text
live  —— 这位是这份团队计划里的 teammate，且有"这件事自己的会话" → 开实时读面
none  —— 是 teammate 但此刻没有自己的会话（还没派活 / 已验收销项） → 不打开（如实说一句）
role  —— 不是 teammate（goal-a2a 的 tl 等真有自己角色会话的角色） → 照旧走角色会话
```

看板的 teammate 行是这条判定的渲染面；`app.js` 的 `openRoleSessionDetail`（Agent Team 面板
成员行的「查看」）也走同一条判定——那条入口只带角色名与"员工的长期角色会话号"，此前直接
落到角色会话读面（= 主代理的会话）。`role` 与 `none` 的分界就是"这位在不在**团队计划**里"，
所以只有拿得到最近一帧看板计划时才启用这条判定（拿不到 = 老路径，行为不变）。

红灯 → 绿灯：

```text
# 修前（renderTeamQueue，pm 没有自己的工作项）
<button ... data-team-role-open="pm" data-team-role-session="s-v-model-pm" data-team-item=""
        data-tip="打开 pm 的会话（这位此刻没有在跑的工作项，这是它的角色会话）">pm</button>
# 修后
<span class="team-member-role" title="pm 这一位此刻没有自己的会话：teammate 的每一件事各自一套
      Session（一 Work Item 一套），没有在跑或待验收的工作项就没有可看的正文">pm</span>
```

---

## 2. 缺陷 A 的现场：这条读面在 GUI 上从来没到过前端

`e2e/teammate_session_live_wire_test.go`（按 JSON 边界读同一份载荷）修前输出：

```text
--- FAIL: TestTeammateSessionLiveWireShapeMatchesFrontend
    实时读面缺前端要读的键 "session_id"（前面的键：[Live Messages Role Running SessionID Truncated]）
    实时读面缺前端要读的键 "role" / "live" / "running" / "messages" / "truncated"
    messages 不是前端能读的数组：<nil>
```

修法：给 `TeammateSessionLiveView` 补 `json` tag（`session_id/role/live/running/messages/truncated`），
并把这一族字段同时加进 `e2e/dto_boundary_test.go` 的 wire 契约守卫（那是这个仓
"探测一个就必须转发一个"的同款做法：**契约要有守卫，不能只靠两端的用例各自的夹具**）。

---

## 3. 同源的那一跳（headless = 有前端的路径）

冒烟新增 **P6b**：拿**应用层**读数（`app.TeammateSessionLiveFor`，走窄端口那一跳）序列化成
JSON，断言前端要读的键都在、`running` 为真。本机输出（`go test -run TestTeamworkHeadlessSmoke -v -count=1 .`）：

```text
[teammate-smoke] P6 当前 teammate 会话      session=…-smoke-team-exec-wi-wi-impl role=exec 行数=2（含本轮工作正文）
[teammate-smoke] P6b 实时读数的 wire 形状    应用层读数（543 字节）带前端要读的 snake_case 键
[teammate-smoke] P7 验收与闸门              accept ok · wi-impl=done · 下游 wi-check 受理回执={…"item":"wi-check"…}
```

为什么必须是"应用层那一面"而不是 runtime 直接调用：GUI 走的是
`Service.TeammateSessionLiveFor` → 窄端口类型断言（`contract.TeammateSessionProjection`）。
上一轮的缺陷 A（作业终态链）就是**窄端口漏转发**导致整条链静默不存在；P6b 把这条跳纳入探针。

---

## 4. 落点表

| 层 | 文件 | 改了什么 |
|---|---|---|
| DTO（wire 契约） | `application/contract/dto/teammate_session_live.go` | 补 `json` tag（6 个字段）+ 把"字段名是 wire 契约"写成注释 |
| 守卫 | `e2e/teammate_session_live_wire_test.go`（新）、`e2e/dto_boundary_test.go` | JSON 边界断言 + wire 字段守卫清单加入这一族 |
| 证据用例 | `sessionstore/role_snapshot_teammate_repro_test.go`（新） | 钉住"teammate 的角色会话读面恒带 main 行、自身行为空"——缺陷 B 的根 |
| 前端（渲染件） | `gui/frontend/dist/team-board-view.js` | teammate 行只指向"这件事自己的会话"；新增纯函数 `teammateSessionEntry`（live/none/role 三分支，两条入口共用）+ 头注写清读面事实 |
| 前端（接线） | `gui/frontend/dist/app.js` | `renderTeam` 记住最近一帧计划；`openRoleSessionDetail` 用 `teammateSessionEntry` 判定：live 走实时读面、none 不开（如实提示）、role 走角色会话 |
| 前端（用例） | `gui/frontend/dist/team-board-view.test.mjs` | ③ 条目改口径 + 新用例「入口只指向这件事自己的会话」+ `teammateSessionEntry` 三分支用例 |
| 探针 | `teamwork_headless_smoke_test.go` | 新增 P6b（应用层读数的 wire 形状） |

---

## 5. 已知缺口（未修，留给下一轮）

1. **headless 控制面没有这条读面的 RPC**：`gui/headless_team.go` 只有 `team.*` 的角色管理，
   "这件事的会话"目前只能在**进程内**（冒烟）或 GUI 上读。要让外部进程也能验，需要加
   `team.teammate_session` 之类的透传口（本轮的 P6b 在进程内覆盖了同一跳）。
2. **入口没有"上一件事"的回看**：验收通过后 `item.session_id` 被清掉，teammate 行就不再是入口
   （这是诚实的选择：引擎槽已被 `ResetSession` 清掉，读也只会读到"执行面不在本进程"）。
   要"回看这件事的最后正文"，正确的落点是**作业输出文件**（`team_context` 的
   `Evicted/LatestJobOutputPath` 那条残边），不是会话读面。
3. **角色会话读面本身没改**：它对**真有自己行**的角色（goal TL / agentteam 员工）仍然正确；
   本轮改的是"谁被允许拿它当 teammate 的会话"。

---

## 6. 复跑命令

```text
go test ./e2e -run TestTeammateSessionLiveWireShapeMatchesFrontend -v -count=1
go test ./sessionstore -run TestRoleSnapshotForTeammateCarriesMainRowsWithNoOwnRows -v -count=1
node --test gui/frontend/dist/team-board-view.test.mjs
go test -run TestTeamworkHeadlessSmoke -v -count=1 .        # P0–P7 + P6b
```
